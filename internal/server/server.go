// Package server exposes Relay over HTTP: an OpenAI-compatible public API and a
// separate admin surface (metrics, analytics API, dashboard). Splitting the two
// onto distinct listeners keeps operational endpoints off the public port.
package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/AymanYouss/relay/internal/auth"
	"github.com/AymanYouss/relay/internal/config"
	"github.com/AymanYouss/relay/internal/gateway"
	"github.com/AymanYouss/relay/internal/ratelimit"
	"github.com/AymanYouss/relay/internal/telemetry"
	"github.com/AymanYouss/relay/internal/usage"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Deps are the server's collaborators.
type Deps struct {
	Config   *config.Config
	Gateway  *gateway.Gateway
	Keys     *auth.KeyStore
	Limiter  ratelimit.Limiter
	Recorder *usage.Recorder
	Metrics  *telemetry.Metrics
	Logger   *slog.Logger
	Version  string
}

// Server owns the public and admin HTTP listeners.
type Server struct {
	deps   Deps
	public *http.Server
	admin  *http.Server
}

// New builds the server and its routers.
func New(deps Deps) *Server {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	s := &Server{deps: deps}
	s.public = &http.Server{
		Addr:        deps.Config.Server.Addr,
		Handler:     s.publicRouter(),
		ReadTimeout: deps.Config.Server.ReadTimeout,
		// No write timeout: streaming responses can outlive it. Idle/read
		// timeouts and context cancellation bound resource use instead.
		WriteTimeout: deps.Config.Server.WriteTimeout,
		IdleTimeout:  120 * time.Second,
	}
	s.admin = &http.Server{
		Addr:         deps.Config.Server.AdminAddr,
		Handler:      s.adminRouter(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
	}
	return s
}

func (s *Server) publicRouter() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RealIP)
	r.Use(requestIDMiddleware)
	r.Use(otelMiddleware)
	r.Use(recoverMiddleware(s.deps.Logger))
	r.Use(accessLog(s.deps.Logger))

	r.Get("/healthz", s.handleHealth)
	r.Get("/readyz", s.handleReady)

	r.Route("/v1", func(r chi.Router) {
		r.Use(s.requireAuth)
		r.Use(s.rateLimit)
		r.Post("/chat/completions", s.handleChatCompletions)
		r.Post("/embeddings", s.handleEmbeddings)
		r.Get("/models", s.handleModels)
	})
	return r
}

func (s *Server) adminRouter() http.Handler {
	r := chi.NewRouter()
	r.Use(requestIDMiddleware)
	r.Use(recoverMiddleware(s.deps.Logger))

	r.Get("/healthz", s.handleHealth)
	if s.deps.Metrics != nil {
		r.Handle("/metrics", s.deps.Metrics.Handler())
	}

	r.Route("/admin/api", func(r chi.Router) {
		r.Use(s.requireAdmin)
		r.Get("/dashboard", s.handleDashboard)
		r.Get("/config", s.handleAdminConfig)
	})

	// Serve the built dashboard SPA when present.
	s.mountDashboard(r)
	return r
}

// Start launches both listeners. It blocks until one exits with an error.
func (s *Server) Start() error {
	errc := make(chan error, 2)
	go func() {
		s.deps.Logger.Info("public API listening", "addr", s.public.Addr)
		if err := s.public.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
	}()
	go func() {
		s.deps.Logger.Info("admin API listening", "addr", s.admin.Addr)
		if err := s.admin.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
	}()
	return <-errc
}

// Shutdown gracefully drains both listeners.
func (s *Server) Shutdown(ctx context.Context) error {
	err1 := s.public.Shutdown(ctx)
	err2 := s.admin.Shutdown(ctx)
	return errors.Join(err1, err2)
}

// PublicHandler exposes the public router for tests.
func (s *Server) PublicHandler() http.Handler { return s.publicRouter() }

// AdminHandler exposes the admin router for tests.
func (s *Server) AdminHandler() http.Handler { return s.adminRouter() }
