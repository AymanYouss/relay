package provider

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AymanYouss/relay/internal/apitypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnthropicRequestTranslation(t *testing.T) {
	var received map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/messages", r.URL.Path)
		assert.Equal(t, "sk-ant", r.Header.Get("x-api-key"))
		assert.Equal(t, anthropicVersion, r.Header.Get("anthropic-version"))
		json.NewDecoder(r.Body).Decode(&received)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{
			"id":"msg_1","model":"claude-3-5-sonnet","stop_reason":"end_turn",
			"content":[{"type":"text","text":"Bonjour"}],
			"usage":{"input_tokens":8,"output_tokens":3}
		}`)
	}))
	defer srv.Close()

	p := NewAnthropic("anthropic", srv.URL, "sk-ant", 5*time.Second)
	resp, err := p.ChatCompletion(context.Background(), Request{
		Model: "claude-3-5-sonnet-20241022",
		Body: apitypes.ChatCompletionRequest{
			Messages: []apitypes.ChatMessage{
				{Role: "system", Content: "You are helpful"},
				{Role: "user", Content: "Say hello in French"},
			},
		},
	})
	require.NoError(t, err)

	// System prompt is lifted into the top-level field.
	assert.Equal(t, "You are helpful", received["system"])
	// max_tokens is always set (Anthropic requires it).
	assert.NotNil(t, received["max_tokens"])
	// Only the user turn remains in messages.
	msgs := received["messages"].([]any)
	assert.Len(t, msgs, 1)

	// Response normalized to OpenAI shape.
	assert.Equal(t, "Bonjour", resp.Choices[0].Message.Content)
	assert.Equal(t, "stop", resp.Choices[0].FinishReason)
	assert.Equal(t, 11, resp.Usage.TotalTokens)
}

func TestAnthropicStreamNormalization(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		events := []string{
			`event: message_start` + "\n" + `data: {"type":"message_start","message":{"id":"msg_2","model":"claude-3-5-sonnet","usage":{"input_tokens":10,"output_tokens":0}}}`,
			`event: content_block_delta` + "\n" + `data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"Hel"}}`,
			`event: content_block_delta` + "\n" + `data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"lo"}}`,
			`event: message_delta` + "\n" + `data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`,
			`event: message_stop` + "\n" + `data: {"type":"message_stop"}`,
		}
		for _, e := range events {
			io.WriteString(w, e+"\n\n")
			fl.Flush()
		}
	}))
	defer srv.Close()

	p := NewAnthropic("anthropic", srv.URL, "sk", 5*time.Second)
	stream, err := p.ChatCompletionStream(context.Background(), Request{
		Model: "claude-3-5-sonnet-20241022",
		Body:  apitypes.ChatCompletionRequest{Stream: true},
	})
	require.NoError(t, err)
	defer stream.Close()

	var content strings.Builder
	var finalUsage apitypes.Usage
	var sawRole bool
	for {
		chunk, err := stream.Recv()
		for _, ch := range chunk.Choices {
			if ch.Delta.Role == apitypes.RoleAssistant {
				sawRole = true
			}
			content.WriteString(ch.Delta.Content)
		}
		if chunk.Usage != nil {
			finalUsage = *chunk.Usage
		}
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
	}
	assert.True(t, sawRole, "stream should open with an assistant role delta")
	assert.Equal(t, "Hello", content.String())
	assert.Equal(t, 12, finalUsage.TotalTokens)
}

func TestAnthropicMapStopReason(t *testing.T) {
	assert.Equal(t, "stop", mapStopReason("end_turn"))
	assert.Equal(t, "length", mapStopReason("max_tokens"))
	assert.Equal(t, "tool_calls", mapStopReason("tool_use"))
	assert.Equal(t, "stop", mapStopReason("stop_sequence"))
}
