// Package gateway composes Relay's request pipeline: authentication limits,
// semantic caching, complexity-aware routing, provider failover, token
// accounting and telemetry all meet here. The HTTP layer stays thin by
// delegating every request to Complete (non-streaming) or Stream (SSE).
package gateway

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/AymanYouss/relay/internal/apitypes"
	"github.com/AymanYouss/relay/internal/auth"
	"github.com/AymanYouss/relay/internal/cache"
	"github.com/AymanYouss/relay/internal/config"
	"github.com/AymanYouss/relay/internal/provider"
	"github.com/AymanYouss/relay/internal/router"
	"github.com/AymanYouss/relay/internal/telemetry"
	"github.com/AymanYouss/relay/internal/usage"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Sentinel errors surfaced to the HTTP layer for status mapping.
var (
	ErrRateLimited     = errors.New("rate limit exceeded")
	ErrBudgetExceeded  = errors.New("monthly budget exceeded")
	ErrModelNotAllowed = errors.New("model not permitted for this key")
	ErrNoUpstream      = errors.New("no upstream model available")
)

// Deps are the collaborators the gateway orchestrates.
type Deps struct {
	Config   *config.Config
	Registry *provider.Registry
	Router   *router.Router
	Executor *router.Executor
	Cache    *cache.SemanticCache // nil when caching is disabled
	Recorder *usage.Recorder
	Metrics  *telemetry.Metrics
	Tracer   trace.Tracer
	Logger   *slog.Logger
}

// Gateway is the request orchestrator.
type Gateway struct {
	deps    Deps
	catalog *catalog
	cacheNS string
	cacheOn bool
}

// New constructs a Gateway, building the model catalog from configuration.
func New(deps Deps) (*Gateway, error) {
	cat, err := newCatalog(deps.Config, deps.Registry)
	if err != nil {
		return nil, err
	}
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	return &Gateway{
		deps:    deps,
		catalog: cat,
		cacheNS: deps.Config.Cache.Namespace,
		cacheOn: deps.Config.Cache.Enabled && deps.Cache != nil,
	}, nil
}

// Catalog exposes the model catalog for the HTTP models endpoint.
func (g *Gateway) Catalog() *catalog { return g.catalog }

// admission enforces rate limits and budgets and returns the request-scoped
// state shared by both the streaming and non-streaming paths.
func (g *Gateway) admit(ctx context.Context, p auth.Principal, req *apitypes.ChatCompletionRequest) error {
	if !p.ModelAllowed(req.Model) {
		return ErrModelNotAllowed
	}
	exceeded, _, err := g.deps.Recorder.BudgetExceeded(ctx, p.ID, p.MonthlyBudgetUSD)
	if err != nil {
		g.deps.Logger.Warn("budget check failed", "err", err)
	} else if exceeded {
		g.deps.Metrics.BudgetExceeded.WithLabelValues(p.ID).Inc()
		return ErrBudgetExceeded
	}
	return nil
}

// cacheEnabled reports whether caching should run for this request.
func (g *Gateway) cacheEnabled(req *apitypes.ChatCompletionRequest) bool {
	if !g.cacheOn {
		return false
	}
	if req.RelayCache != nil {
		return *req.RelayCache
	}
	return true
}

// Complete runs the non-streaming pipeline and returns a fully populated
// response, including a Relay metadata block describing the gateway's decisions.
func (g *Gateway) Complete(ctx context.Context, p auth.Principal, req *apitypes.ChatCompletionRequest) (*apitypes.ChatCompletionResponse, error) {
	start := time.Now()
	ctx, span := g.deps.Tracer.Start(ctx, "gateway.Complete", trace.WithAttributes(
		attribute.String("relay.key", p.ID),
		attribute.String("relay.requested_model", req.Model),
	))
	defer span.End()

	if err := g.admit(ctx, p, req); err != nil {
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	// Semantic cache lookup.
	var cachedVector []float32
	if g.cacheEnabled(req) {
		res, err := g.lookupCache(ctx, req)
		if err != nil {
			g.deps.Logger.Warn("cache lookup failed", "err", err)
		} else if res.Hit {
			g.recordCacheHit(ctx, p, req, res, start)
			return res.Response, nil
		} else {
			cachedVector = res.Vector
		}
	}

	// Route and execute with failover.
	decision := g.deps.Router.Resolve(req)
	span.SetAttributes(
		attribute.String("relay.strategy", decision.Strategy),
		attribute.Float64("relay.complexity", decision.Complexity),
	)
	if len(decision.Chain) == 0 {
		return nil, ErrNoUpstream
	}

	var (
		resp      *apitypes.ChatCompletionResponse
		usedEntry modelEntry
		upstreamStart time.Time
		upstreamMS int64
	)
	outcome := g.deps.Executor.Run(ctx, decision.Chain, func(ctx context.Context, model string) error {
		entry, ok := g.catalog.resolve(model)
		if !ok {
			return &provider.Error{Provider: model, Message: "model not in catalog"}
		}
		upstreamStart = time.Now()
		r, err := entry.provider.ChatCompletion(ctx, provider.Request{Model: entry.upstream, Body: *req})
		upstreamMS = time.Since(upstreamStart).Milliseconds()
		if err != nil {
			return err
		}
		g.deps.Metrics.UpstreamLatency.WithLabelValues(entry.provider.Kind(), model).Observe(time.Since(upstreamStart).Seconds())
		resp = r
		usedEntry = entry
		return nil
	})
	g.recordRetries(outcome)
	if outcome.Err != nil {
		g.recordError(ctx, p, req, decision, usedEntry, start)
		span.SetStatus(codes.Error, outcome.Err.Error())
		return nil, outcome.Err
	}

	// Normalize identity fields and attach gateway metadata.
	resp.Model = usedEntry.name
	cost := g.deps.Recorder.Cost(usedEntry.name, resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
	resp.Relay = &apitypes.RelayMeta{
		CacheHit:       false,
		RoutedModel:    usedEntry.name,
		RoutedProvider: usedEntry.provider.Name(),
		RouteReason:    decision.Reason,
		UpstreamMillis: upstreamMS,
		CostUSD:        cost,
		Attempts:       outcome.Attempts,
	}

	// Persist to cache and record accounting.
	if g.cacheEnabled(req) {
		g.storeCache(ctx, req, resp, cachedVector)
	}
	g.record(ctx, usage.Event{
		Time:             time.Now(),
		KeyID:            p.ID,
		KeyName:          p.Name,
		RequestedModel:   req.Model,
		Model:            usedEntry.name,
		Provider:         usedEntry.provider.Name(),
		Strategy:         decision.Strategy,
		Complexity:       decision.Complexity,
		RouteReason:      decision.Reason,
		PromptTokens:     resp.Usage.PromptTokens,
		CompletionTokens: resp.Usage.CompletionTokens,
		CostUSD:          cost,
		LatencyMS:        time.Since(start).Milliseconds(),
		Status:           "ok",
	}, false)

	span.SetStatus(codes.Ok, "")
	return resp, nil
}

func (g *Gateway) lookupCache(ctx context.Context, req *apitypes.ChatCompletionRequest) (cache.Result, error) {
	ctx, span := g.deps.Tracer.Start(ctx, "gateway.cacheLookup")
	defer span.End()
	res, err := g.deps.Cache.Lookup(ctx, req)
	if err != nil {
		g.deps.Metrics.CacheLookups.WithLabelValues("error").Inc()
		return res, err
	}
	if res.Hit {
		g.deps.Metrics.CacheLookups.WithLabelValues("hit").Inc()
		span.SetAttributes(attribute.Bool("cache.hit", true), attribute.Float64("cache.score", res.Score))
	} else {
		g.deps.Metrics.CacheLookups.WithLabelValues("miss").Inc()
	}
	return res, nil
}

func (g *Gateway) storeCache(ctx context.Context, req *apitypes.ChatCompletionRequest, resp *apitypes.ChatCompletionResponse, vec []float32) {
	// Only cache well-formed responses.
	if len(resp.Choices) == 0 || resp.Choices[0].Message.Content == "" {
		return
	}
	if err := g.deps.Cache.Store(ctx, req, resp, vec); err != nil {
		g.deps.Logger.Warn("cache store failed", "err", err)
	}
}

func (g *Gateway) recordCacheHit(ctx context.Context, p auth.Principal, req *apitypes.ChatCompletionRequest, res cache.Result, start time.Time) {
	resp := res.Response
	// The response was produced by some model previously; value the saving at the
	// price the *requested* route would have cost.
	saved := g.deps.Recorder.Cost(resp.Model, resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
	resp.Relay = &apitypes.RelayMeta{
		CacheHit:       true,
		CacheScore:     res.Score,
		RoutedModel:    resp.Model,
		RoutedProvider: "cache",
		RouteReason:    "semantic cache hit",
		CostUSD:        0,
	}
	g.record(ctx, usage.Event{
		Time:             time.Now(),
		KeyID:            p.ID,
		KeyName:          p.Name,
		RequestedModel:   req.Model,
		Model:            resp.Model,
		Provider:         "cache",
		Strategy:         "cache",
		PromptTokens:     resp.Usage.PromptTokens,
		CompletionTokens: resp.Usage.CompletionTokens,
		CostUSD:          0,
		SavedUSD:         saved,
		CacheHit:         true,
		CacheScore:       res.Score,
		LatencyMS:        time.Since(start).Milliseconds(),
		Status:           "ok",
	}, true)
}

func (g *Gateway) recordError(ctx context.Context, p auth.Principal, req *apitypes.ChatCompletionRequest, d router.Decision, entry modelEntry, start time.Time) {
	model := d.Primary()
	if entry.name != "" {
		model = entry.name
	}
	g.record(ctx, usage.Event{
		Time:           time.Now(),
		KeyID:          p.ID,
		KeyName:        p.Name,
		RequestedModel: req.Model,
		Model:          model,
		Strategy:       d.Strategy,
		Complexity:     d.Complexity,
		LatencyMS:      time.Since(start).Milliseconds(),
		Status:         "error",
	}, false)
}

// record fans an event out to Prometheus and the usage store.
func (g *Gateway) record(ctx context.Context, e usage.Event, cacheHit bool) {
	cacheLabel := "miss"
	if cacheHit {
		cacheLabel = "hit"
	}
	g.deps.Metrics.Requests.WithLabelValues(e.Model, e.Provider, e.Strategy, cacheLabel, e.Status).Inc()
	g.deps.Metrics.RequestDuration.WithLabelValues(e.Model, cacheLabel).Observe(float64(e.LatencyMS) / 1000)
	if e.CostUSD > 0 {
		g.deps.Metrics.CostTotal.WithLabelValues(e.Model).Add(e.CostUSD)
	}
	if e.PromptTokens > 0 {
		g.deps.Metrics.TokensTotal.WithLabelValues("input", e.Model).Add(float64(e.PromptTokens))
	}
	if e.CompletionTokens > 0 {
		g.deps.Metrics.TokensTotal.WithLabelValues("output", e.Model).Add(float64(e.CompletionTokens))
	}
	// Recording must not fail the request; use a detached, bounded context so
	// accounting still completes if the client has already disconnected.
	recCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	go func() {
		defer cancel()
		if err := g.deps.Recorder.Record(recCtx, e); err != nil {
			g.deps.Logger.Warn("usage record failed", "err", err)
		}
	}()
}

func (g *Gateway) recordRetries(o router.Outcome) {
	if o.Attempts > 1 {
		g.deps.Metrics.Retries.Add(float64(o.Attempts - 1))
		if o.Err == nil {
			g.deps.Metrics.Failovers.Inc()
		}
	}
}
