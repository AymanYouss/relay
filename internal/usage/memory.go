package usage

import (
	"context"
	"sort"
	"sync"
	"time"
)

// maxMemoryEvents bounds retention for the in-memory store.
const maxMemoryEvents = 200_000

// MemoryStore keeps events in a bounded in-process ring and computes aggregates
// on demand. It is exact within its retention window and requires no external
// dependency, which suits tests, local development and demos.
type MemoryStore struct {
	mu     sync.RWMutex
	events []Event
	now    func() time.Time
}

// NewMemoryStore returns an empty in-memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{now: time.Now}
}

// Record appends an event, evicting the oldest when at capacity.
func (m *MemoryStore) Record(_ context.Context, e Event) error {
	if e.Time.IsZero() {
		e.Time = m.now()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, e)
	if len(m.events) > maxMemoryEvents {
		m.events = m.events[len(m.events)-maxMemoryEvents:]
	}
	return nil
}

// MonthSpend sums the current calendar month's cost for a key.
func (m *MemoryStore) MonthSpend(_ context.Context, keyID string) (float64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	month := monthKey(m.now())
	var total float64
	for i := range m.events {
		e := &m.events[i]
		if e.KeyID == keyID && monthKey(e.Time) == month {
			total += e.CostUSD
		}
	}
	return total, nil
}

// Query aggregates the last windowDays days.
func (m *MemoryStore) Query(_ context.Context, windowDays int) (Dashboard, error) {
	if windowDays <= 0 {
		windowDays = 7
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	cutoff := m.now().UTC().AddDate(0, 0, -windowDays+1).Truncate(24 * time.Hour)

	var (
		summary   Summary
		latencies []float64
		dayAgg    = map[string]*DailyPoint{}
		dayLat    = map[string][]float64{}
		modelAgg  = map[string]*ModelUsage{}
		routeAgg  = map[string]int64{}
		keyAgg    = map[string]*KeyUsage{}
	)
	summary.WindowDays = windowDays
	month := monthKey(m.now())

	for i := range m.events {
		e := &m.events[i]
		if e.Time.UTC().Before(cutoff) {
			continue
		}
		summary.Requests++
		summary.TotalCostUSD += e.CostUSD
		summary.CostSavedUSD += e.SavedUSD
		summary.PromptTokens += int64(e.PromptTokens)
		summary.CompletionTokens += int64(e.CompletionTokens)
		if e.CacheHit {
			summary.CacheHits++
		}
		if e.Status == "error" {
			summary.Errors++
		}
		if e.LatencyMS > 0 {
			latencies = append(latencies, float64(e.LatencyMS))
		}

		d := dayKey(e.Time)
		dp := dayAgg[d]
		if dp == nil {
			dp = &DailyPoint{Date: d}
			dayAgg[d] = dp
		}
		dp.Requests++
		dp.CostUSD += e.CostUSD
		dp.CostSavedUSD += e.SavedUSD
		if e.CacheHit {
			dp.CacheHits++
		}
		if e.LatencyMS > 0 {
			dayLat[d] = append(dayLat[d], float64(e.LatencyMS))
		}

		if e.Model != "" {
			mk := e.Model + "|" + e.Provider
			mu := modelAgg[mk]
			if mu == nil {
				mu = &ModelUsage{Model: e.Model, Provider: e.Provider}
				modelAgg[mk] = mu
			}
			mu.Requests++
			mu.CostUSD += e.CostUSD
			mu.Tokens += int64(e.PromptTokens + e.CompletionTokens)
		}
		if e.Strategy != "" {
			routeAgg[e.Strategy]++
		}
		if e.KeyID != "" {
			ku := keyAgg[e.KeyID]
			if ku == nil {
				ku = &KeyUsage{KeyID: e.KeyID, Name: e.KeyName}
				keyAgg[e.KeyID] = ku
			}
			ku.Requests++
			ku.Tokens += int64(e.PromptTokens + e.CompletionTokens)
			if monthKey(e.Time) == month {
				ku.CostUSD += e.CostUSD
			}
		}
	}

	if summary.Requests > 0 {
		summary.CacheHitRate = float64(summary.CacheHits) / float64(summary.Requests)
		summary.ErrorRate = float64(summary.Errors) / float64(summary.Requests)
	}
	if len(latencies) > 0 {
		sort.Float64s(latencies)
		summary.LatencyP50 = percentile(latencies, 50)
		summary.LatencyP95 = percentile(latencies, 95)
		summary.LatencyP99 = percentile(latencies, 99)
		var sum float64
		for _, l := range latencies {
			sum += l
		}
		summary.LatencyAvg = sum / float64(len(latencies))
	}

	dash := Dashboard{Summary: summary}

	// Fill a contiguous day series so the chart has no gaps.
	for i := windowDays - 1; i >= 0; i-- {
		d := dayKey(m.now().UTC().AddDate(0, 0, -i))
		dp := dayAgg[d]
		if dp == nil {
			dp = &DailyPoint{Date: d}
		}
		if dp.Requests > 0 {
			dp.CacheHitRate = float64(dp.CacheHits) / float64(dp.Requests)
		}
		if ls := dayLat[d]; len(ls) > 0 {
			sort.Float64s(ls)
			dp.LatencyP50 = percentile(ls, 50)
			dp.LatencyP95 = percentile(ls, 95)
			dp.LatencyP99 = percentile(ls, 99)
		}
		dash.Series = append(dash.Series, *dp)
	}

	for _, mu := range modelAgg {
		if summary.Requests > 0 {
			mu.Share = float64(mu.Requests) / float64(summary.Requests)
		}
		dash.Models = append(dash.Models, *mu)
	}
	sort.Slice(dash.Models, func(i, j int) bool { return dash.Models[i].Requests > dash.Models[j].Requests })

	for strat, n := range routeAgg {
		dash.Routes = append(dash.Routes, RouteBreakdown{Strategy: strat, Requests: n})
	}
	sort.Slice(dash.Routes, func(i, j int) bool { return dash.Routes[i].Requests > dash.Routes[j].Requests })

	for _, ku := range keyAgg {
		dash.Keys = append(dash.Keys, *ku)
	}
	sort.Slice(dash.Keys, func(i, j int) bool { return dash.Keys[i].CostUSD > dash.Keys[j].CostUSD })

	// Most recent events, newest first, capped.
	n := len(m.events)
	limit := 50
	for i := n - 1; i >= 0 && len(dash.Recent) < limit; i-- {
		dash.Recent = append(dash.Recent, m.events[i])
	}

	return dash, nil
}

// Close is a no-op for the in-memory store.
func (m *MemoryStore) Close() error { return nil }
