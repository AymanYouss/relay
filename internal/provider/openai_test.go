package provider

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AymanYouss/relay/internal/apitypes"
)

func TestOpenAIChatCompletion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/chat/completions", r.URL.Path)
		assert.Equal(t, "Bearer sk-test", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{
			"id":"chatcmpl-1","object":"chat.completion","model":"gpt-4o",
			"choices":[{"index":0,"message":{"role":"assistant","content":"Hello!"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}
		}`)
	}))
	defer srv.Close()

	p := NewOpenAI("openai", srv.URL, "sk-test", 5*time.Second)
	resp, err := p.ChatCompletion(context.Background(), Request{
		Model: "gpt-4o",
		Body:  apitypes.ChatCompletionRequest{Messages: []apitypes.ChatMessage{{Role: "user", Content: "hi"}}},
	})
	require.NoError(t, err)
	assert.Equal(t, "Hello!", resp.Choices[0].Message.Content)
	assert.Equal(t, 7, resp.Usage.TotalTokens)
}

func TestOpenAIRetryableError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		io.WriteString(w, `{"error":{"message":"rate limited","type":"rate_limit_error"}}`)
	}))
	defer srv.Close()

	p := NewOpenAI("openai", srv.URL, "sk", time.Second)
	_, err := p.ChatCompletion(context.Background(), Request{Model: "m"})
	require.Error(t, err)
	assert.True(t, IsRetryable(err))
	var pe *Error
	require.True(t, errors.As(err, &pe))
	assert.Equal(t, 429, pe.StatusCode)
	assert.Contains(t, pe.Message, "rate limited")
}

func TestOpenAINonRetryableError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":{"message":"bad","type":"invalid_request_error"}}`)
	}))
	defer srv.Close()

	p := NewOpenAI("openai", srv.URL, "sk", time.Second)
	_, err := p.ChatCompletion(context.Background(), Request{Model: "m"})
	require.Error(t, err)
	assert.False(t, IsRetryable(err))
}

func TestOpenAIStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		chunks := []string{
			`data: {"id":"1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"role":"assistant"}}]}`,
			`data: {"id":"1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"content":"Hel"}}]}`,
			`data: {"id":"1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"content":"lo"}}]}`,
			`data: {"id":"1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`,
			`data: [DONE]`,
		}
		for _, c := range chunks {
			io.WriteString(w, c+"\n\n")
			fl.Flush()
		}
	}))
	defer srv.Close()

	p := NewOpenAI("openai", srv.URL, "sk", 5*time.Second)
	stream, err := p.ChatCompletionStream(context.Background(), Request{
		Model: "gpt-4o",
		Body:  apitypes.ChatCompletionRequest{Stream: true, Messages: []apitypes.ChatMessage{{Role: "user", Content: "hi"}}},
	})
	require.NoError(t, err)
	defer stream.Close()

	var content strings.Builder
	var usage apitypes.Usage
	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		for _, ch := range chunk.Choices {
			content.WriteString(ch.Delta.Content)
		}
		if chunk.Usage != nil {
			usage = *chunk.Usage
		}
	}
	assert.Equal(t, "Hello", content.String())
	assert.Equal(t, 5, usage.TotalTokens)
}

func TestSSEScannerMultiline(t *testing.T) {
	raw := "data: {\"a\":1}\n\ndata: line1\ndata: line2\n\n"
	sc := newSSEScanner(strings.NewReader(raw))
	ev1, err := sc.next()
	require.NoError(t, err)
	assert.Equal(t, `{"a":1}`, string(ev1.Data))
	ev2, err := sc.next()
	require.NoError(t, err)
	assert.Equal(t, "line1\nline2", string(ev2.Data))
}
