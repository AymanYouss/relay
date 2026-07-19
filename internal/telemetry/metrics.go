package telemetry

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics is the set of Prometheus instruments Relay exposes. It is safe for
// concurrent use and is registered against a private registry so the /metrics
// endpoint is self-contained and testable.
type Metrics struct {
	registry *prometheus.Registry

	Requests        *prometheus.CounterVec
	RequestDuration *prometheus.HistogramVec
	UpstreamLatency *prometheus.HistogramVec
	CacheLookups    *prometheus.CounterVec
	TokensTotal     *prometheus.CounterVec
	CostTotal       *prometheus.CounterVec
	RateLimited     *prometheus.CounterVec
	BudgetExceeded  *prometheus.CounterVec
	Retries         prometheus.Counter
	Failovers       prometheus.Counter
	ActiveStreams   prometheus.Gauge
}

// NewMetrics constructs and registers all instruments.
func NewMetrics() *Metrics {
	reg := prometheus.NewRegistry()
	reg.MustRegister(prometheus.NewGoCollector())
	reg.MustRegister(prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))

	m := &Metrics{
		registry: reg,
		Requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "relay_requests_total",
			Help: "Total gateway requests by outcome.",
		}, []string{"model", "provider", "strategy", "cache", "status"}),
		RequestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "relay_request_duration_seconds",
			Help:    "End-to-end request latency as seen by the client.",
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 30},
		}, []string{"model", "cache"}),
		UpstreamLatency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "relay_upstream_duration_seconds",
			Help:    "Latency of upstream provider calls.",
			Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 30},
		}, []string{"provider", "model"}),
		CacheLookups: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "relay_cache_lookups_total",
			Help: "Semantic cache lookups by result.",
		}, []string{"result"}),
		TokensTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "relay_tokens_total",
			Help: "Tokens processed by direction and model.",
		}, []string{"direction", "model"}),
		CostTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "relay_cost_usd_total",
			Help: "Upstream spend in USD by model.",
		}, []string{"model"}),
		RateLimited: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "relay_rate_limited_total",
			Help: "Requests rejected by rate limiting.",
		}, []string{"key"}),
		BudgetExceeded: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "relay_budget_exceeded_total",
			Help: "Requests rejected because a key exceeded its monthly budget.",
		}, []string{"key"}),
		Retries: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "relay_retries_total",
			Help: "Upstream retry attempts across the failover chain.",
		}),
		Failovers: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "relay_failovers_total",
			Help: "Requests that succeeded only after failing over to a fallback model.",
		}),
		ActiveStreams: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "relay_active_streams",
			Help: "Currently open streaming responses.",
		}),
	}

	reg.MustRegister(
		m.Requests, m.RequestDuration, m.UpstreamLatency, m.CacheLookups,
		m.TokensTotal, m.CostTotal, m.RateLimited, m.BudgetExceeded,
		m.Retries, m.Failovers, m.ActiveStreams,
	)
	return m
}

// Handler returns an HTTP handler serving the Prometheus exposition format.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

// Registry exposes the underlying registry for tests.
func (m *Metrics) Registry() *prometheus.Registry { return m.registry }
