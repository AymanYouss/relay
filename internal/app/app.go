// Package app wires Relay's components together from configuration and runs the
// gateway with graceful shutdown. Keeping the composition root here keeps main
// tiny and lets integration tests build a fully wired gateway in-process.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/AymanYouss/relay/internal/auth"
	"github.com/AymanYouss/relay/internal/cache"
	"github.com/AymanYouss/relay/internal/config"
	"github.com/AymanYouss/relay/internal/embed"
	"github.com/AymanYouss/relay/internal/gateway"
	"github.com/AymanYouss/relay/internal/provider"
	"github.com/AymanYouss/relay/internal/ratelimit"
	"github.com/AymanYouss/relay/internal/router"
	"github.com/AymanYouss/relay/internal/server"
	"github.com/AymanYouss/relay/internal/telemetry"
	"github.com/AymanYouss/relay/internal/usage"
)

// Version is set at build time via -ldflags.
var Version = "dev"

// Run boots the gateway from the config at path and blocks until shutdown.
func Run(path string) error {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := config.Load(path)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	logger.Info("configuration loaded",
		"providers", len(cfg.Providers),
		"models", len(cfg.Models),
		"cache_enabled", cfg.Cache.Enabled,
		"backend", cfg.Redis.Backend,
	)

	// Providers.
	registry := provider.NewRegistry()
	for _, pc := range cfg.Providers {
		switch pc.Kind {
		case "openai":
			registry.Add(provider.NewOpenAI(pc.Name, pc.BaseURL, pc.APIKey, pc.Timeout))
		case "anthropic":
			registry.Add(provider.NewAnthropic(pc.Name, pc.BaseURL, pc.APIKey, pc.Timeout))
		case "local":
			registry.Add(provider.NewLocal(pc.Name, pc.BaseURL, pc.APIKey, pc.Timeout))
		}
	}

	// Shared Redis client (RESP2 for vector-search reply parsing).
	var rdb *redis.Client
	if cfg.Redis.Backend == "redis" {
		rdb = redis.NewClient(&redis.Options{
			Addr:     cfg.Redis.Addr,
			Password: cfg.Redis.Password,
			DB:       cfg.Redis.DB,
			Protocol: 2,
		})
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := rdb.Ping(ctx).Err()
		cancel()
		if err != nil {
			return fmt.Errorf("connect redis at %s: %w", cfg.Redis.Addr, err)
		}
		logger.Info("connected to redis", "addr", cfg.Redis.Addr)
	}

	// Embedder for the semantic cache.
	embedder := buildEmbedder(cfg, registry, logger)

	// Cache.
	var semCache *cache.SemanticCache
	if cfg.Cache.Enabled {
		var store cache.VectorStore
		if rdb != nil {
			rs, err := cache.NewRedisStoreFromClient(rdb, embedder.Dimensions())
			if err != nil {
				return fmt.Errorf("init cache index: %w", err)
			}
			store = rs
		} else {
			store = cache.NewMemoryStore()
		}
		semCache = cache.New(embedder, store, cache.Options{
			Threshold:     cfg.Cache.SimilarityThreshold,
			TTL:           cfg.Cache.TTL,
			Namespace:     cfg.Cache.Namespace,
			MaxCandidates: cfg.Cache.MaxCandidates,
		})
	}

	// Router and executor.
	rtr := router.New(cfg.Router, cfg.Models, router.NewHeuristicClassifier())
	exec := router.NewExecutor(cfg.Router.MaxRetries, cfg.Router.RetryBackoff)

	// Usage accounting.
	var usageStore usage.Store
	if rdb != nil {
		usageStore = usage.NewRedisStore(rdb)
	} else {
		usageStore = usage.NewMemoryStore()
	}
	pricing := map[string]usage.Pricing{}
	for _, m := range cfg.Models {
		pricing[m.Name] = usage.Pricing{InputPerM: m.InputPricePerM, OutputPerM: m.OutputPricePerM}
	}
	recorder := usage.NewRecorder(usageStore, pricing)

	// Rate limiter.
	var limiter ratelimit.Limiter
	if rdb != nil {
		limiter = ratelimit.NewRedisLimiter(rdb)
	} else {
		limiter = ratelimit.NewMemoryLimiter()
	}

	// Telemetry.
	metrics := telemetry.NewMetrics()
	tracer, shutdownTracing, err := telemetry.InitTracing(context.Background(), telemetry.Config{
		ServiceName:  cfg.Telemetry.ServiceName,
		OTLPEndpoint: cfg.Telemetry.OTLPEndpoint,
		Sampling:     cfg.Telemetry.TraceSampling,
		Version:      Version,
	})
	if err != nil {
		return fmt.Errorf("init tracing: %w", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdownTracing(ctx)
	}()

	// Gateway.
	gw, err := gateway.New(gateway.Deps{
		Config:   cfg,
		Registry: registry,
		Router:   rtr,
		Executor: exec,
		Cache:    semCache,
		Recorder: recorder,
		Metrics:  metrics,
		Tracer:   tracer,
		Logger:   logger,
	})
	if err != nil {
		return fmt.Errorf("build gateway: %w", err)
	}

	// HTTP server.
	srv := server.New(server.Deps{
		Config:   cfg,
		Gateway:  gw,
		Keys:     auth.NewKeyStore(cfg.APIKeys),
		Limiter:  limiter,
		Recorder: recorder,
		Metrics:  metrics,
		Logger:   logger,
		Version:  Version,
	})

	// Run with graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errc := make(chan error, 1)
	go func() { errc <- srv.Start() }()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		logger.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Error("graceful shutdown failed", "err", err)
		}
		if rdb != nil {
			_ = rdb.Close()
		}
		return nil
	}
}

// buildEmbedder selects an embedding implementation. When an embedding provider
// is configured and available, it is used; otherwise the dependency-free hash
// embedder keeps the semantic cache operational.
func buildEmbedder(cfg *config.Config, registry *provider.Registry, logger *slog.Logger) embed.Embedder {
	dims := cfg.Embedding.Dimensions
	if cfg.Embedding.Provider != "" {
		if p, ok := registry.Get(cfg.Embedding.Provider); ok {
			logger.Info("using provider embedder", "provider", cfg.Embedding.Provider, "model", cfg.Embedding.Model)
			return embed.NewProviderEmbedder(p, cfg.Embedding.Model, dims)
		}
		logger.Warn("configured embedding provider not found; falling back to local embedder", "provider", cfg.Embedding.Provider)
	}
	return embed.NewHashEmbedder(dims)
}
