package cache

import (
	"context"
	"testing"
	"time"

	"github.com/AymanYouss/relay/internal/apitypes"
	"github.com/AymanYouss/relay/internal/embed"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestCache(threshold float64) *SemanticCache {
	return New(embed.NewHashEmbedder(512), NewMemoryStore(), Options{
		Threshold:     threshold,
		TTL:           time.Hour,
		Namespace:     "test",
		MaxCandidates: 5,
	})
}

func chatReq(model, content string) *apitypes.ChatCompletionRequest {
	return &apitypes.ChatCompletionRequest{
		Model:    model,
		Messages: []apitypes.ChatMessage{{Role: apitypes.RoleUser, Content: content}},
	}
}

func chatResp(model, content string) *apitypes.ChatCompletionResponse {
	return &apitypes.ChatCompletionResponse{
		ID:      "resp-1",
		Model:   model,
		Choices: []apitypes.Choice{{Message: apitypes.ChatMessage{Role: apitypes.RoleAssistant, Content: content}}},
		Usage:   apitypes.Usage{PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30},
	}
}

func TestCacheMissThenHit(t *testing.T) {
	ctx := context.Background()
	c := newTestCache(0.9)

	req := chatReq("gpt-4o", "what is the capital of France")

	// First lookup: miss, but returns a vector for cheap storage.
	res, err := c.Lookup(ctx, req)
	require.NoError(t, err)
	assert.False(t, res.Hit)
	require.NotEmpty(t, res.Vector)

	require.NoError(t, c.Store(ctx, req, chatResp("gpt-4o", "Paris"), res.Vector))

	// Identical request now hits.
	res2, err := c.Lookup(ctx, req)
	require.NoError(t, err)
	require.True(t, res2.Hit)
	assert.GreaterOrEqual(t, res2.Score, 0.9)
	require.NotNil(t, res2.Response)
	assert.Equal(t, "Paris", res2.Response.Choices[0].Message.Content)
}

func TestCacheNamespaceIsolationByModel(t *testing.T) {
	ctx := context.Background()
	c := newTestCache(0.9)
	req := chatReq("gpt-4o", "hello there general")

	res, err := c.Lookup(ctx, req)
	require.NoError(t, err)
	require.NoError(t, c.Store(ctx, req, chatResp("gpt-4o", "hi"), res.Vector))

	// The same prompt for a different model must not hit the first model's cache.
	other := chatReq("gpt-4o-mini", "hello there general")
	res2, err := c.Lookup(ctx, other)
	require.NoError(t, err)
	assert.False(t, res2.Hit, "cache is scoped per requested model")
}

func TestCacheThresholdRejectsWeakMatch(t *testing.T) {
	ctx := context.Background()
	c := newTestCache(0.999) // effectively require near-identical prompts

	req := chatReq("gpt-4o", "explain how TCP congestion control works in detail")
	res, err := c.Lookup(ctx, req)
	require.NoError(t, err)
	require.NoError(t, c.Store(ctx, req, chatResp("gpt-4o", "..."), res.Vector))

	// A related but different prompt should fall below the strict threshold.
	other := chatReq("gpt-4o", "what is the weather like in Tokyo today")
	res2, err := c.Lookup(ctx, other)
	require.NoError(t, err)
	assert.False(t, res2.Hit)
}

func TestMemoryStoreTTLExpiry(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	vec := []float32{1, 0, 0}
	require.NoError(t, store.Upsert(ctx, Entry{
		ID: "1", Namespace: "n", Vector: vec, Response: []byte("{}"),
	}, time.Nanosecond))
	time.Sleep(2 * time.Millisecond)
	matches, err := store.Search(ctx, "n", vec, 5)
	require.NoError(t, err)
	assert.Empty(t, matches, "expired entries must not be returned")
}
