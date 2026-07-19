// Package telemetry wires Relay's observability: OpenTelemetry distributed
// tracing (exported over OTLP) and Prometheus metrics. Both are optional and
// degrade to no-ops when unconfigured, so the gateway runs the same in a laptop
// container as it does behind a full observability stack.
package telemetry

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

// TracerName is the instrumentation scope used across the gateway.
const TracerName = "github.com/AymanYouss/relay"

// Config controls tracing setup.
type Config struct {
	ServiceName  string
	OTLPEndpoint string // host:port of an OTLP/HTTP collector; empty disables export
	Sampling     float64
	Version      string
}

// InitTracing configures the global tracer provider. The returned shutdown
// function flushes and stops the exporter and should be deferred by the caller.
// When OTLPEndpoint is empty, tracing uses a no-op provider.
func InitTracing(ctx context.Context, cfg Config) (trace.Tracer, func(context.Context) error, error) {
	if cfg.OTLPEndpoint == "" {
		return otel.Tracer(TracerName), func(context.Context) error { return nil }, nil
	}

	exporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpoint(cfg.OTLPEndpoint),
		otlptracehttp.WithInsecure(),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("otlp exporter: %w", err)
	}

	res, err := resource.Merge(resource.Default(), resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(cfg.ServiceName),
		semconv.ServiceVersion(cfg.Version),
	))
	if err != nil {
		return nil, nil, fmt.Errorf("otel resource: %w", err)
	}

	sampling := cfg.Sampling
	if sampling <= 0 {
		sampling = 1.0
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter, sdktrace.WithBatchTimeout(5*time.Second)),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(sampling))),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{},
	))

	return tp.Tracer(TracerName), tp.Shutdown, nil
}
