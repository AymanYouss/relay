// Package usage handles token accounting, cost calculation, monthly budget
// tracking and the analytics that power the admin dashboard.
//
// A Recorder combines a pricing table with a pluggable Store. Two stores are
// provided: an in-memory store for tests and single-node runs, and a Redis store
// that keeps durable daily rollups and per-key monthly spend so budgets and
// dashboards work across a fleet of gateway replicas.
package usage

import (
	"context"
	"time"
)

// Pricing is the per-million-token cost of a model, split by direction.
type Pricing struct {
	InputPerM  float64
	OutputPerM float64
}

// Cost returns the USD cost of the given token counts at this price.
func (p Pricing) Cost(promptTokens, completionTokens int) float64 {
	return float64(promptTokens)/1_000_000*p.InputPerM +
		float64(completionTokens)/1_000_000*p.OutputPerM
}

// Event is a single completed request as seen by the accounting layer.
type Event struct {
	Time             time.Time
	KeyID            string
	KeyName          string
	RequestedModel   string
	Model            string
	Provider         string
	Strategy         string
	Complexity       float64
	RouteReason      string
	PromptTokens     int
	CompletionTokens int
	CostUSD          float64
	// SavedUSD is the cost that would have been incurred upstream but was avoided
	// because the response was served from the semantic cache.
	SavedUSD   float64
	CacheHit   bool
	CacheScore float64
	LatencyMS  int64
	Status     string // "ok" or "error"
	Error      string
}

// Summary aggregates a time window for the dashboard headline cards.
type Summary struct {
	Requests         int64   `json:"requests"`
	CacheHits        int64   `json:"cache_hits"`
	CacheHitRate     float64 `json:"cache_hit_rate"`
	Errors           int64   `json:"errors"`
	ErrorRate        float64 `json:"error_rate"`
	TotalCostUSD     float64 `json:"total_cost_usd"`
	CostSavedUSD     float64 `json:"cost_saved_usd"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	LatencyP50       float64 `json:"latency_p50_ms"`
	LatencyP95       float64 `json:"latency_p95_ms"`
	LatencyP99       float64 `json:"latency_p99_ms"`
	LatencyAvg       float64 `json:"latency_avg_ms"`
	WindowDays       int     `json:"window_days"`
}

// DailyPoint is one day in the time series.
type DailyPoint struct {
	Date         string  `json:"date"`
	Requests     int64   `json:"requests"`
	CacheHits    int64   `json:"cache_hits"`
	CacheHitRate float64 `json:"cache_hit_rate"`
	CostUSD      float64 `json:"cost_usd"`
	CostSavedUSD float64 `json:"cost_saved_usd"`
	LatencyP50   float64 `json:"latency_p50_ms"`
	LatencyP95   float64 `json:"latency_p95_ms"`
	LatencyP99   float64 `json:"latency_p99_ms"`
}

// ModelUsage is per-model aggregate usage.
type ModelUsage struct {
	Model    string  `json:"model"`
	Provider string  `json:"provider"`
	Requests int64   `json:"requests"`
	CostUSD  float64 `json:"cost_usd"`
	Tokens   int64   `json:"tokens"`
	Share    float64 `json:"share"`
}

// RouteBreakdown is per-strategy request counts.
type RouteBreakdown struct {
	Strategy string `json:"strategy"`
	Requests int64  `json:"requests"`
}

// KeyUsage is per-API-key month-to-date usage plus its configured limits.
type KeyUsage struct {
	KeyID         string  `json:"key_id"`
	Name          string  `json:"name"`
	Requests      int64   `json:"requests"`
	CostUSD       float64 `json:"cost_usd"`
	Tokens        int64   `json:"tokens"`
	BudgetUSD     float64 `json:"budget_usd"`
	BudgetUsedPct float64 `json:"budget_used_pct"`
	RateLimitRPM  int     `json:"rate_limit_rpm"`
}

// Dashboard is the full analytics payload consumed by the admin UI.
type Dashboard struct {
	Summary Summary          `json:"summary"`
	Series  []DailyPoint     `json:"series"`
	Models  []ModelUsage     `json:"models"`
	Routes  []RouteBreakdown `json:"routes"`
	Keys    []KeyUsage       `json:"keys"`
	Recent  []Event          `json:"recent"`
}

// Store persists events and answers analytics and budget queries.
type Store interface {
	Record(ctx context.Context, e Event) error
	// MonthSpend returns the current calendar month's spend for a key.
	MonthSpend(ctx context.Context, keyID string) (float64, error)
	// Query aggregates the last windowDays days for the dashboard.
	Query(ctx context.Context, windowDays int) (Dashboard, error)
	Close() error
}

// percentile returns the p-th percentile (0..100) of the samples, which must be
// sorted ascending. Uses nearest-rank.
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 100 {
		return sorted[len(sorted)-1]
	}
	rank := int(p/100*float64(len(sorted)-1) + 0.5)
	if rank >= len(sorted) {
		rank = len(sorted) - 1
	}
	return sorted[rank]
}

func dayKey(t time.Time) string { return t.UTC().Format("2006-01-02") }
func monthKey(t time.Time) string { return t.UTC().Format("2006-01") }
