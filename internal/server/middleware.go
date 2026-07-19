package server

import (
	"context"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/AymanYouss/relay/internal/auth"
	"github.com/AymanYouss/relay/internal/telemetry"
)

type ctxKey int

const (
	ctxKeyPrincipal ctxKey = iota
	ctxKeyRequestID
)

// requestIDMiddleware assigns a request id, echoing an inbound one if provided.
func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = uuid.NewString()
		}
		w.Header().Set("X-Request-ID", id)
		ctx := context.WithValue(r.Context(), ctxKeyRequestID, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// otelMiddleware starts a server span per request, continuing any inbound trace.
func otelMiddleware(next http.Handler) http.Handler {
	tracer := otel.Tracer(telemetry.TracerName)
	prop := otel.GetTextMapPropagator()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := prop.Extract(r.Context(), propagation.HeaderCarrier(r.Header))
		ctx, span := tracer.Start(ctx, r.Method+" "+r.URL.Path,
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				attribute.String("http.method", r.Method),
				attribute.String("http.route", r.URL.Path),
			),
		)
		defer span.End()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// recoverMiddleware converts panics into 500s and logs the stack.
func recoverMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error("panic recovered", "panic", rec, "stack", string(debug.Stack()))
					writeError(w, http.StatusInternalServerError, "internal error", "internal_error", "")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// statusRecorder captures the response status for access logging.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// Flush propagates flushes so SSE streaming works through the wrapper.
func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func accessLog(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: 200}
			next.ServeHTTP(rec, r)
			logger.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"duration_ms", time.Since(start).Milliseconds(),
				"request_id", requestIDFrom(r.Context()),
			)
		})
	}
}

// requireAuth authenticates the API key and stores the principal in context.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := auth.BearerToken(r)
		principal, ok := s.deps.Keys.Authenticate(token)
		if !ok {
			writeError(w, http.StatusUnauthorized, "invalid or missing API key", "invalid_request_error", "invalid_api_key")
			return
		}
		if span := trace.SpanFromContext(r.Context()); span.IsRecording() {
			span.SetAttributes(attribute.String("relay.key", principal.ID))
		}
		ctx := context.WithValue(r.Context(), ctxKeyPrincipal, principal)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// rateLimit enforces the principal's per-minute request limit.
func (s *Server) rateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := principalFrom(r.Context())
		decision, err := s.deps.Limiter.Allow(r.Context(), p.ID, p.RateLimitRPM)
		if err != nil {
			s.deps.Logger.Warn("rate limiter error", "err", err)
			next.ServeHTTP(w, r) // fail open: availability over strict limiting
			return
		}
		if !decision.Allowed {
			s.deps.Metrics.RateLimited.WithLabelValues(p.ID).Inc()
			w.Header().Set("Retry-After", strconv.Itoa(int(decision.RetryAfter.Seconds())+1))
			writeError(w, http.StatusTooManyRequests, "rate limit exceeded", "rate_limit_error", "")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireAdmin guards the admin API with the configured bearer token.
func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := s.deps.Config.Server.TrustedAdminToken
		if want == "" {
			writeError(w, http.StatusServiceUnavailable, "admin API disabled: no admin token configured", "config_error", "")
			return
		}
		if auth.BearerToken(r) != want {
			writeError(w, http.StatusUnauthorized, "invalid admin token", "invalid_request_error", "")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func principalFrom(ctx context.Context) auth.Principal {
	p, _ := ctx.Value(ctxKeyPrincipal).(auth.Principal)
	return p
}

func requestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(ctxKeyRequestID).(string)
	return id
}
