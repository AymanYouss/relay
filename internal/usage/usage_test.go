package usage

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPricingCost(t *testing.T) {
	p := Pricing{InputPerM: 2.50, OutputPerM: 10.00}
	// 1000 input + 500 output tokens.
	cost := p.Cost(1000, 500)
	assert.InDelta(t, 0.0025+0.005, cost, 1e-9)
}

func TestPercentile(t *testing.T) {
	s := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	assert.Equal(t, 1.0, percentile(s, 0))
	assert.Equal(t, 10.0, percentile(s, 100))
	assert.Equal(t, 10.0, percentile(s, 99))
	assert.Equal(t, 0.0, percentile(nil, 50))
}

func TestMemoryStoreRecordAndQuery(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	now := time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }

	// Two paid requests and one cache hit for the same key.
	require.NoError(t, store.Record(ctx, Event{
		Time: now, KeyID: "k1", KeyName: "team", Model: "gpt-4o", Provider: "openai",
		Strategy: "auto", PromptTokens: 100, CompletionTokens: 200, CostUSD: 0.5,
		LatencyMS: 900, Status: "ok",
	}))
	require.NoError(t, store.Record(ctx, Event{
		Time: now, KeyID: "k1", KeyName: "team", Model: "gpt-4o-mini", Provider: "openai",
		Strategy: "auto", PromptTokens: 50, CompletionTokens: 50, CostUSD: 0.01,
		LatencyMS: 300, Status: "ok",
	}))
	require.NoError(t, store.Record(ctx, Event{
		Time: now, KeyID: "k1", KeyName: "team", Model: "gpt-4o", Provider: "cache",
		Strategy: "cache", CostUSD: 0, SavedUSD: 0.5, CacheHit: true, CacheScore: 0.98,
		LatencyMS: 5, Status: "ok",
	}))

	dash, err := store.Query(ctx, 7)
	require.NoError(t, err)

	assert.Equal(t, int64(3), dash.Summary.Requests)
	assert.Equal(t, int64(1), dash.Summary.CacheHits)
	assert.InDelta(t, 1.0/3.0, dash.Summary.CacheHitRate, 1e-9)
	assert.InDelta(t, 0.51, dash.Summary.TotalCostUSD, 1e-9)
	assert.InDelta(t, 0.5, dash.Summary.CostSavedUSD, 1e-9)
	assert.Greater(t, dash.Summary.LatencyP99, 0.0)

	// Series covers the whole window with a contiguous set of days.
	assert.Len(t, dash.Series, 7)

	// Month spend for budget enforcement.
	spend, err := store.MonthSpend(ctx, "k1")
	require.NoError(t, err)
	assert.InDelta(t, 0.51, spend, 1e-9)

	// Per-key and per-model aggregates present.
	require.NotEmpty(t, dash.Keys)
	assert.Equal(t, "k1", dash.Keys[0].KeyID)
	require.NotEmpty(t, dash.Models)
	require.NotEmpty(t, dash.Routes)
}

func TestRecorderBudgetExceeded(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	rec := NewRecorder(store, map[string]Pricing{"m": {InputPerM: 1000, OutputPerM: 1000}})

	// Unlimited budget never trips.
	over, _, err := rec.BudgetExceeded(ctx, "k", 0)
	require.NoError(t, err)
	assert.False(t, over)

	require.NoError(t, rec.Record(ctx, Event{KeyID: "k", CostUSD: 30, Time: time.Now()}))
	over, spent, err := rec.BudgetExceeded(ctx, "k", 25)
	require.NoError(t, err)
	assert.True(t, over)
	assert.InDelta(t, 30, spent, 1e-9)
}

func TestRecorderCost(t *testing.T) {
	rec := NewRecorder(NewMemoryStore(), map[string]Pricing{
		"gpt-4o": {InputPerM: 2.5, OutputPerM: 10},
	})
	assert.InDelta(t, 0.0125, rec.Cost("gpt-4o", 1000, 1000), 1e-9)
	assert.Equal(t, 0.0, rec.Cost("unknown", 1000, 1000))
}
