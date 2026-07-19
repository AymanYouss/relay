package server

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AymanYouss/relay/internal/apitypes"
	"github.com/AymanYouss/relay/internal/auth"
	"github.com/AymanYouss/relay/internal/cache"
	"github.com/AymanYouss/relay/internal/config"
	"github.com/AymanYouss/relay/internal/embed"
	"github.com/AymanYouss/relay/internal/gateway"
	"github.com/AymanYouss/relay/internal/provider"
	"github.com/AymanYouss/relay/internal/ratelimit"
	"github.com/AymanYouss/relay/internal/router"
	"github.com/AymanYouss/relay/internal/telemetry"
	"github.com/AymanYouss/relay/internal/usage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
)

// fakeUpstream mimics an OpenAI-compatible provider for end-to-end tests.
func fakeUpstream() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		streaming, _ := body["stream"].(bool)
		if streaming {
			w.Header().Set("Content-Type", "text/event-stream")
			fl := w.(http.Flusher)
			for _, c := range []string{
				`data: {"id":"1","object":"chat.completion.chunk","model":"gpt-4o-mini","choices":[{"index":0,"delta":{"role":"assistant"}}]}`,
				`data: {"id":"1","object":"chat.completion.chunk","model":"gpt-4o-mini","choices":[{"index":0,"delta":{"content":"pong"}}]}`,
				`data: {"id":"1","object":"chat.completion.chunk","model":"gpt-4o-mini","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`,
				`data: [DONE]`,
			} {
				io.WriteString(w, c+"\n\n")
				fl.Flush()
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"cmpl","object":"chat.completion","model":"gpt-4o-mini",
			"choices":[{"index":0,"message":{"role":"assistant","content":"pong"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`)
	}))
}

func buildTestServer(t *testing.T, upstreamURL string) *Server {
	t.Helper()
	cfg := &config.Config{
		Server: config.ServerConfig{Addr: ":0", AdminAddr: ":0", TrustedAdminToken: "admin-secret"},
		Cache:  config.CacheConfig{Enabled: true, SimilarityThreshold: 0.9, Namespace: "test", MaxCandidates: 5},
		Router: config.RouterConfig{Strategy: "cost", CheapModel: "gpt-4o-mini", StrongModel: "gpt-4o-mini", ComplexityThreshold: 0.55},
		Models: []config.ModelConfig{
			{Name: "gpt-4o-mini", Provider: "openai", Upstream: "gpt-4o-mini", Tier: "cheap", InputPricePerM: 0.15, OutputPricePerM: 0.6},
		},
		Telemetry: config.TelemetryConfig{ServiceName: "relay-test"},
	}
	reg := provider.NewRegistry(provider.NewOpenAI("openai", upstreamURL, "sk", 5*time.Second))
	semCache := cache.New(embed.NewHashEmbedder(512), cache.NewMemoryStore(), cache.Options{
		Threshold: 0.9, TTL: time.Hour, Namespace: "test", MaxCandidates: 5,
	})
	store := usage.NewMemoryStore()
	recorder := usage.NewRecorder(store, map[string]usage.Pricing{"gpt-4o-mini": {InputPerM: 0.15, OutputPerM: 0.6}})
	// The gateway and server share one metrics registry so /metrics reflects
	// activity driven through the request pipeline.
	metrics := telemetry.NewMetrics()
	gw, err := gateway.New(gateway.Deps{
		Config:   cfg,
		Registry: reg,
		Router:   router.New(cfg.Router, cfg.Models, router.NewHeuristicClassifier()),
		Executor: router.NewExecutor(1, time.Millisecond),
		Cache:    semCache,
		Recorder: recorder,
		Metrics:  metrics,
		Tracer:   otel.Tracer("test"),
	})
	require.NoError(t, err)

	return New(Deps{
		Config:   cfg,
		Gateway:  gw,
		Keys:     auth.NewKeyStore([]config.APIKeyConfig{{Key: "sk-user", Name: "user", RateLimitRPM: 3}}),
		Limiter:  ratelimit.NewMemoryLimiter(),
		Recorder: recorder,
		Metrics:  metrics,
		Version:  "test",
	})
}

func TestChatCompletionEndToEnd(t *testing.T) {
	up := fakeUpstream()
	defer up.Close()
	ts := httptest.NewServer(buildTestServer(t, up.URL).PublicHandler())
	defer ts.Close()

	body := `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"ping"}]}`
	req, _ := http.NewRequest("POST", ts.URL+"/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer sk-user")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out apitypes.ChatCompletionResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Equal(t, "pong", out.Choices[0].Message.Content)
	require.NotNil(t, out.Relay)
	assert.False(t, out.Relay.CacheHit)
	assert.Equal(t, "gpt-4o-mini", out.Relay.RoutedModel)
}

func TestChatCompletionUnauthorized(t *testing.T) {
	up := fakeUpstream()
	defer up.Close()
	ts := httptest.NewServer(buildTestServer(t, up.URL).PublicHandler())
	defer ts.Close()

	req, _ := http.NewRequest("POST", ts.URL+"/v1/chat/completions",
		strings.NewReader(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"x"}]}`))
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestChatCompletionStreamingSSE(t *testing.T) {
	up := fakeUpstream()
	defer up.Close()
	ts := httptest.NewServer(buildTestServer(t, up.URL).PublicHandler())
	defer ts.Close()

	body := `{"model":"gpt-4o-mini","stream":true,"messages":[{"role":"user","content":"ping"}]}`
	req, _ := http.NewRequest("POST", ts.URL+"/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer sk-user")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "text/event-stream")

	var content strings.Builder
	sawDone := false
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			sawDone = true
			break
		}
		var chunk apitypes.StreamChunk
		if json.Unmarshal([]byte(payload), &chunk) == nil {
			for _, c := range chunk.Choices {
				content.WriteString(c.Delta.Content)
			}
		}
	}
	assert.Equal(t, "pong", content.String())
	assert.True(t, sawDone, "stream must terminate with [DONE]")
}

func TestRateLimitReturns429(t *testing.T) {
	up := fakeUpstream()
	defer up.Close()
	ts := httptest.NewServer(buildTestServer(t, up.URL).PublicHandler())
	defer ts.Close()

	do := func() int {
		req, _ := http.NewRequest("POST", ts.URL+"/v1/chat/completions",
			strings.NewReader(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"x"}]}`))
		req.Header.Set("Authorization", "Bearer sk-user")
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		resp.Body.Close()
		return resp.StatusCode
	}
	// Limit is 3 rpm; the 4th request in the same window is rejected.
	statuses := []int{do(), do(), do(), do()}
	assert.Equal(t, http.StatusTooManyRequests, statuses[3])
}

func TestAdminDashboardRequiresToken(t *testing.T) {
	up := fakeUpstream()
	defer up.Close()
	srv := buildTestServer(t, up.URL)
	ts := httptest.NewServer(srv.AdminHandler())
	defer ts.Close()

	// Without token: 401.
	resp, err := http.Get(ts.URL + "/admin/api/dashboard")
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	// With token: 200 and valid JSON.
	req, _ := http.NewRequest("GET", ts.URL+"/admin/api/dashboard?window=7", nil)
	req.Header.Set("Authorization", "Bearer admin-secret")
	resp2, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp2.Body.Close()
	assert.Equal(t, http.StatusOK, resp2.StatusCode)
	var dash usage.Dashboard
	require.NoError(t, json.NewDecoder(resp2.Body).Decode(&dash))
	assert.Equal(t, 7, dash.Summary.WindowDays)
}

func TestMetricsEndpoint(t *testing.T) {
	up := fakeUpstream()
	defer up.Close()
	srv := buildTestServer(t, up.URL)
	public := httptest.NewServer(srv.PublicHandler())
	defer public.Close()
	admin := httptest.NewServer(srv.AdminHandler())
	defer admin.Close()

	// Drive one request through the pipeline so labeled metrics are emitted.
	req, _ := http.NewRequest("POST", public.URL+"/v1/chat/completions",
		strings.NewReader(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"x"}]}`))
	req.Header.Set("Authorization", "Bearer sk-user")
	r, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	r.Body.Close()

	resp, err := http.Get(admin.URL + "/metrics")
	require.NoError(t, err)
	defer resp.Body.Close()
	buf := new(bytes.Buffer)
	buf.ReadFrom(resp.Body)
	body := buf.String()
	assert.Contains(t, body, "relay_requests_total")
	assert.Contains(t, body, "relay_cache_lookups_total")
}
