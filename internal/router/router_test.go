package router

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AymanYouss/relay/internal/apitypes"
	"github.com/AymanYouss/relay/internal/config"
)

func testModels() []config.ModelConfig {
	return []config.ModelConfig{
		{Name: "cheap", Provider: "p", Tier: "cheap", Fallbacks: []string{"cheap-alt"}},
		{Name: "cheap-alt", Provider: "p", Tier: "cheap"},
		{Name: "strong", Provider: "p", Tier: "strong", Fallbacks: []string{"strong-alt"}},
		{Name: "strong-alt", Provider: "p", Tier: "strong"},
	}
}

func testRouter(strategy string) *Router {
	return New(config.RouterConfig{
		Strategy:            strategy,
		CheapModel:          "cheap",
		StrongModel:         "strong",
		ComplexityThreshold: 0.55,
	}, testModels(), NewHeuristicClassifier())
}

func msg(content string) *apitypes.ChatCompletionRequest {
	return &apitypes.ChatCompletionRequest{
		Messages: []apitypes.ChatMessage{{Role: apitypes.RoleUser, Content: content}},
	}
}

func TestRouteAutoSimplePromptUsesCheap(t *testing.T) {
	r := testRouter("auto")
	req := msg("hi there")
	req.Model = "auto"
	d := r.Resolve(req)
	assert.Equal(t, "cheap", d.Primary())
	assert.Equal(t, StrategyAuto, d.Strategy)
	assert.Less(t, d.Complexity, 0.55)
}

func TestRouteAutoComplexPromptUsesStrong(t *testing.T) {
	r := testRouter("auto")
	req := msg("Design and architect a fault-tolerant distributed consensus system, " +
		"analyze the trade-offs step by step, and prove why quorum reads are linearizable. " +
		"```go\nfunc raft() {}\n```")
	req.Model = "auto"
	d := r.Resolve(req)
	assert.Equal(t, "strong", d.Primary())
	assert.GreaterOrEqual(t, d.Complexity, 0.55)
}

func TestRoutePinnedModel(t *testing.T) {
	r := testRouter("auto")
	req := msg("anything")
	req.Model = "strong"
	d := r.Resolve(req)
	assert.Equal(t, "strong", d.Primary())
	// Chain includes the model's fallbacks.
	assert.Equal(t, []Hop{{"strong"}, {"strong-alt"}}, d.Chain)
}

func TestRouteExplicitStrategyOverride(t *testing.T) {
	r := testRouter("auto")
	req := msg("Design a complex distributed system with detailed trade-off analysis")
	req.Model = "gpt-does-not-exist"
	req.RelayRoute = "cost"
	d := r.Resolve(req)
	assert.Equal(t, "cheap", d.Primary(), "cost strategy forces the cheap model")
}

func TestRouteQualityStrategy(t *testing.T) {
	r := testRouter("auto")
	req := msg("hello")
	req.Model = "quality"
	d := r.Resolve(req)
	assert.Equal(t, "strong", d.Primary())
}

func TestRouteUnknownModelFallsBackToStrategy(t *testing.T) {
	r := testRouter("cost")
	req := msg("hello")
	req.Model = "some-unknown-model"
	d := r.Resolve(req)
	assert.Equal(t, "cheap", d.Primary())
}

func TestChainDeduplicatesAndDropsUnknown(t *testing.T) {
	models := []config.ModelConfig{
		{Name: "a", Provider: "p", Fallbacks: []string{"b", "a", "ghost"}},
		{Name: "b", Provider: "p"},
	}
	r := New(config.RouterConfig{Strategy: "quality", StrongModel: "a", CheapModel: "b"}, models, nil)
	req := msg("x")
	req.Model = "a"
	d := r.Resolve(req)
	require.Equal(t, []Hop{{"a"}, {"b"}}, d.Chain)
}
