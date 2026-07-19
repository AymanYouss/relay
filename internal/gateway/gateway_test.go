package gateway

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AymanYouss/relay/internal/apitypes"
	"github.com/AymanYouss/relay/internal/auth"
	"github.com/AymanYouss/relay/internal/cache"
	"github.com/AymanYouss/relay/internal/config"
	"github.com/AymanYouss/relay/internal/embed"
	"github.com/AymanYouss/relay/internal/provider"
	"github.com/AymanYouss/relay/internal/router"
	"github.com/AymanYouss/relay/internal/telemetry"
	"github.com/AymanYouss/relay/internal/usage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
)

// fakeProvider is a programmable provider for pipeline tests.
type fakeProvider struct {
	name  string
	kind  string
	calls int32
	chat  func(ctx context.Context, req provider.Request) (*apitypes.ChatCompletionResponse, error)
}

func (f *fakeProvider) Name() string { return f.name }
func (f *fakeProvider) Kind() string {
	if f.kind == "" {
		return "openai"
	}
	return f.kind
}
func (f *fakeProvider) ChatCompletion(ctx context.Context, req provider.Request) (*apitypes.ChatCompletionResponse, error) {
	atomic.AddInt32(&f.calls, 1)
	return f.chat(ctx, req)
}
func (f *fakeProvider) ChatCompletionStream(ctx context.Context, req provider.Request) (provider.StreamReader, error) {
	atomic.AddInt32(&f.calls, 1)
	resp, err := f.chat(ctx, req)
	if err != nil {
		return nil, err
	}
	return newFakeStream(resp), nil
}
func (f *fakeProvider) Embeddings(context.Context, apitypes.EmbeddingRequest) (*apitypes.EmbeddingResponse, error) {
	return nil, errors.New("not implemented")
}

type fakeStream struct {
	chunks []apitypes.StreamChunk
	i      int
}

func newFakeStream(resp *apitypes.ChatCompletionResponse) *fakeStream {
	content := resp.Choices[0].Message.Content
	stop := "stop"
	return &fakeStream{chunks: []apitypes.StreamChunk{
		{ID: resp.ID, Choices: []apitypes.StreamChoice{{Delta: apitypes.ChatMessage{Role: "assistant"}}}},
		{ID: resp.ID, Choices: []apitypes.StreamChoice{{Delta: apitypes.ChatMessage{Content: content}}}},
		{ID: resp.ID, Choices: []apitypes.StreamChoice{{Delta: apitypes.ChatMessage{}, FinishReason: &stop}}, Usage: &resp.Usage},
	}}
}

func (s *fakeStream) Recv() (apitypes.StreamChunk, error) {
	if s.i >= len(s.chunks) {
		return apitypes.StreamChunk{}, io.EOF
	}
	c := s.chunks[s.i]
	s.i++
	if s.i == len(s.chunks) {
		return c, io.EOF
	}
	return c, nil
}
func (s *fakeStream) Close() error { return nil }

func okResponse(model, content string) *apitypes.ChatCompletionResponse {
	return &apitypes.ChatCompletionResponse{
		ID:      "resp",
		Object:  "chat.completion",
		Model:   model,
		Choices: []apitypes.Choice{{Message: apitypes.ChatMessage{Role: "assistant", Content: content}, FinishReason: "stop"}},
		Usage:   apitypes.Usage{PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30},
	}
}

type testHarness struct {
	gw       *Gateway
	recorder *usage.Recorder
	cheap    *fakeProvider
	strong   *fakeProvider
	backup   *fakeProvider
	store    *usage.MemoryStore
}

func newHarness(t *testing.T, cacheOn bool) *testHarness {
	t.Helper()
	cheap := &fakeProvider{name: "cheap-prov", chat: func(_ context.Context, _ provider.Request) (*apitypes.ChatCompletionResponse, error) {
		return okResponse("cheap", "cheap answer"), nil
	}}
	strong := &fakeProvider{name: "strong-prov", chat: func(_ context.Context, _ provider.Request) (*apitypes.ChatCompletionResponse, error) {
		return okResponse("strong", "strong answer"), nil
	}}
	backup := &fakeProvider{name: "backup-prov", chat: func(_ context.Context, _ provider.Request) (*apitypes.ChatCompletionResponse, error) {
		return okResponse("backup", "backup answer"), nil
	}}
	reg := provider.NewRegistry(cheap, strong, backup)

	cfg := &config.Config{
		Cache:  config.CacheConfig{Enabled: cacheOn, SimilarityThreshold: 0.9, Namespace: "test", MaxCandidates: 5},
		Router: config.RouterConfig{Strategy: "auto", CheapModel: "cheap", StrongModel: "strong", ComplexityThreshold: 0.55},
		Models: []config.ModelConfig{
			{Name: "cheap", Provider: "cheap-prov", Upstream: "cheap-u", Tier: "cheap", InputPricePerM: 0.15, OutputPricePerM: 0.6, Fallbacks: []string{"backup"}},
			{Name: "strong", Provider: "strong-prov", Upstream: "strong-u", Tier: "strong", InputPricePerM: 2.5, OutputPricePerM: 10},
			{Name: "backup", Provider: "backup-prov", Upstream: "backup-u", Tier: "cheap", InputPricePerM: 0.1, OutputPricePerM: 0.4},
		},
	}

	store := usage.NewMemoryStore()
	recorder := usage.NewRecorder(store, map[string]usage.Pricing{
		"cheap":  {InputPerM: 0.15, OutputPerM: 0.6},
		"strong": {InputPerM: 2.5, OutputPerM: 10},
		"backup": {InputPerM: 0.1, OutputPerM: 0.4},
	})

	var semCache *cache.SemanticCache
	if cacheOn {
		semCache = cache.New(embed.NewHashEmbedder(512), cache.NewMemoryStore(), cache.Options{
			Threshold: 0.9, TTL: time.Hour, Namespace: "test", MaxCandidates: 5,
		})
	}

	gw, err := New(Deps{
		Config:   cfg,
		Registry: reg,
		Router:   router.New(cfg.Router, cfg.Models, router.NewHeuristicClassifier()),
		Executor: router.NewExecutor(2, time.Millisecond),
		Cache:    semCache,
		Recorder: recorder,
		Metrics:  telemetry.NewMetrics(),
		Tracer:   otel.Tracer("test"),
	})
	require.NoError(t, err)
	return &testHarness{gw: gw, recorder: recorder, cheap: cheap, strong: strong, backup: backup, store: store}
}

func principal() auth.Principal {
	return auth.Principal{ID: "k1", Name: "tester"}
}

func req(model, content string) *apitypes.ChatCompletionRequest {
	return &apitypes.ChatCompletionRequest{
		Model:    model,
		Messages: []apitypes.ChatMessage{{Role: "user", Content: content}},
	}
}

func TestGatewayCompleteRoutesSimpleToCheap(t *testing.T) {
	h := newHarness(t, false)
	resp, err := h.gw.Complete(context.Background(), principal(), req("auto", "hi"))
	require.NoError(t, err)
	assert.Equal(t, "cheap", resp.Model)
	require.NotNil(t, resp.Relay)
	assert.False(t, resp.Relay.CacheHit)
	assert.Equal(t, "cheap-prov", resp.Relay.RoutedProvider)
	assert.Greater(t, resp.Relay.CostUSD, 0.0)
	assert.EqualValues(t, 1, h.cheap.calls)
}

func TestGatewayCompleteRoutesComplexToStrong(t *testing.T) {
	h := newHarness(t, false)
	prompt := "Design and architect a distributed system, analyze the trade-offs step by step and prove correctness. ```code```"
	resp, err := h.gw.Complete(context.Background(), principal(), req("auto", prompt))
	require.NoError(t, err)
	assert.Equal(t, "strong", resp.Model)
	assert.EqualValues(t, 1, h.strong.calls)
}

func TestGatewayCacheHitSecondCall(t *testing.T) {
	h := newHarness(t, true)
	r := req("cheap", "what is the capital of France")

	resp1, err := h.gw.Complete(context.Background(), principal(), r)
	require.NoError(t, err)
	assert.False(t, resp1.Relay.CacheHit)
	assert.EqualValues(t, 1, h.cheap.calls)

	resp2, err := h.gw.Complete(context.Background(), principal(), req("cheap", "what is the capital of France"))
	require.NoError(t, err)
	assert.True(t, resp2.Relay.CacheHit)
	assert.Equal(t, 0.0, resp2.Relay.CostUSD)
	assert.EqualValues(t, 1, h.cheap.calls, "cache hit must not call the upstream again")
}

func TestGatewayFailover(t *testing.T) {
	h := newHarness(t, false)
	// Make the cheap provider fail with a retryable error; backup is its fallback.
	h.cheap.chat = func(context.Context, provider.Request) (*apitypes.ChatCompletionResponse, error) {
		return nil, &provider.Error{Provider: "cheap-prov", StatusCode: 503, Retryable: true}
	}
	resp, err := h.gw.Complete(context.Background(), principal(), req("cheap", "hi"))
	require.NoError(t, err)
	assert.Equal(t, "backup", resp.Model)
	assert.GreaterOrEqual(t, resp.Relay.Attempts, 2)
	assert.EqualValues(t, 1, h.backup.calls)
}

func TestGatewayAllProvidersFail(t *testing.T) {
	h := newHarness(t, false)
	fail := func(context.Context, provider.Request) (*apitypes.ChatCompletionResponse, error) {
		return nil, &provider.Error{StatusCode: 503, Retryable: true}
	}
	h.cheap.chat, h.backup.chat = fail, fail
	_, err := h.gw.Complete(context.Background(), principal(), req("cheap", "hi"))
	require.Error(t, err)
}

func TestGatewayBudgetEnforced(t *testing.T) {
	h := newHarness(t, false)
	p := auth.Principal{ID: "poor", Name: "poor", MonthlyBudgetUSD: 0.0001}
	// Spend past the budget.
	require.NoError(t, h.recorder.Record(context.Background(), usage.Event{KeyID: "poor", CostUSD: 1, Time: time.Now()}))
	_, err := h.gw.Complete(context.Background(), p, req("cheap", "hi"))
	require.ErrorIs(t, err, ErrBudgetExceeded)
}

func TestGatewayModelNotAllowed(t *testing.T) {
	h := newHarness(t, false)
	// A key restricted to "strong" may not call "cheap".
	ks := auth.NewKeyStore([]config.APIKeyConfig{
		{Key: "sk-restricted", Name: "restricted", AllowedModels: []string{"strong"}},
	})
	restricted, ok := ks.Authenticate("sk-restricted")
	require.True(t, ok)
	_, err := h.gw.Complete(context.Background(), restricted, req("cheap", "hi"))
	require.ErrorIs(t, err, ErrModelNotAllowed)
}

func TestGatewayStream(t *testing.T) {
	h := newHarness(t, true)
	var content strings.Builder
	meta, err := h.gw.Stream(context.Background(), principal(), req("cheap", "stream please"), func(c apitypes.StreamChunk) error {
		for _, ch := range c.Choices {
			content.WriteString(ch.Delta.Content)
		}
		return nil
	})
	require.NoError(t, err)
	require.NotNil(t, meta)
	assert.Equal(t, "cheap answer", content.String())
	assert.Equal(t, "cheap", meta.RoutedModel)

	// A second identical stream should be a cache hit.
	var content2 strings.Builder
	meta2, err := h.gw.Stream(context.Background(), principal(), req("cheap", "stream please"), func(c apitypes.StreamChunk) error {
		for _, ch := range c.Choices {
			content2.WriteString(ch.Delta.Content)
		}
		return nil
	})
	require.NoError(t, err)
	assert.True(t, meta2.CacheHit)
	assert.Equal(t, "cheap answer", content2.String())
}
