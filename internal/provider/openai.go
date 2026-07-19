package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/AymanYouss/relay/internal/apitypes"
)

// OpenAI adapts any OpenAI-compatible Chat Completions endpoint. Because Relay's
// canonical types already mirror OpenAI, this adapter is a thin, faithful
// passthrough that manages auth, timeouts and SSE normalization. The same
// adapter (with a different base URL) serves Azure OpenAI, Together, Groq,
// Fireworks and other OpenAI-compatible vendors.
type OpenAI struct {
	name    string
	baseURL string
	apiKey  string
	client  *http.Client
}

// NewOpenAI constructs an OpenAI-compatible provider.
func NewOpenAI(name, baseURL, apiKey string, timeout time.Duration) *OpenAI {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	return &OpenAI{
		name:    name,
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		client:  &http.Client{Timeout: timeout},
	}
}

func (o *OpenAI) Name() string { return o.name }
func (o *OpenAI) Kind() string { return "openai" }

func (o *OpenAI) newRequest(ctx context.Context, path string, body any) (*http.Request, error) {
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+path, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+o.apiKey)
	return req, nil
}

// wire is the OpenAI request payload. We build it explicitly rather than
// forwarding Relay extensions so provider-specific fields never leak upstream.
type openAIChatRequest struct {
	Model            string                  `json:"model"`
	Messages         []apitypes.ChatMessage  `json:"messages"`
	Temperature      *float64                `json:"temperature,omitempty"`
	TopP             *float64                `json:"top_p,omitempty"`
	MaxTokens        *int                    `json:"max_tokens,omitempty"`
	Stream           bool                    `json:"stream,omitempty"`
	StreamOptions    *streamOptions          `json:"stream_options,omitempty"`
	Stop             []string                `json:"stop,omitempty"`
	PresencePenalty  *float64                `json:"presence_penalty,omitempty"`
	FrequencyPenalty *float64                `json:"frequency_penalty,omitempty"`
	Tools            []apitypes.Tool         `json:"tools,omitempty"`
	ToolChoice       any                     `json:"tool_choice,omitempty"`
	User             string                  `json:"user,omitempty"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

func (o *OpenAI) buildPayload(req Request) openAIChatRequest {
	b := req.Body
	return openAIChatRequest{
		Model:            req.Model,
		Messages:         b.Messages,
		Temperature:      b.Temperature,
		TopP:             b.TopP,
		MaxTokens:        b.MaxTokens,
		Stream:           b.Stream,
		Stop:             b.Stop,
		PresencePenalty:  b.PresencePenalty,
		FrequencyPenalty: b.FrequencyPenalty,
		Tools:            b.Tools,
		ToolChoice:       b.ToolChoice,
		User:             b.User,
	}
}

// ChatCompletion performs a non-streaming completion.
func (o *OpenAI) ChatCompletion(ctx context.Context, req Request) (*apitypes.ChatCompletionResponse, error) {
	payload := o.buildPayload(req)
	payload.Stream = false
	httpReq, err := o.newRequest(ctx, "/chat/completions", payload)
	if err != nil {
		return nil, &Error{Provider: o.name, Message: "build request", Err: err}
	}
	resp, err := o.client.Do(httpReq)
	if err != nil {
		return nil, &Error{Provider: o.name, Message: "transport error", Retryable: true, Err: err}
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, &Error{
			Provider:   o.name,
			StatusCode: resp.StatusCode,
			Message:    extractErrorMessage(data),
			Retryable:  retryableStatus(resp.StatusCode),
		}
	}
	var out apitypes.ChatCompletionResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, &Error{Provider: o.name, Message: "decode response", Err: err}
	}
	return &out, nil
}

// ChatCompletionStream performs a streaming completion, requesting usage in the
// final chunk so accounting stays accurate even for streamed responses.
func (o *OpenAI) ChatCompletionStream(ctx context.Context, req Request) (StreamReader, error) {
	payload := o.buildPayload(req)
	payload.Stream = true
	payload.StreamOptions = &streamOptions{IncludeUsage: true}
	httpReq, err := o.newRequest(ctx, "/chat/completions", payload)
	if err != nil {
		return nil, &Error{Provider: o.name, Message: "build request", Err: err}
	}
	httpReq.Header.Set("Accept", "text/event-stream")
	resp, err := o.client.Do(httpReq)
	if err != nil {
		return nil, &Error{Provider: o.name, Message: "transport error", Retryable: true, Err: err}
	}
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		return nil, &Error{
			Provider:   o.name,
			StatusCode: resp.StatusCode,
			Message:    extractErrorMessage(data),
			Retryable:  retryableStatus(resp.StatusCode),
		}
	}
	return &openAIStream{body: resp.Body, scanner: newSSEScanner(resp.Body)}, nil
}

type openAIStream struct {
	body    io.ReadCloser
	scanner *sseScanner
}

func (s *openAIStream) Recv() (apitypes.StreamChunk, error) {
	for {
		ev, err := s.scanner.next()
		if err != nil {
			return apitypes.StreamChunk{}, err
		}
		if len(ev.Data) == 0 {
			continue
		}
		if bytes.Equal(bytes.TrimSpace(ev.Data), []byte("[DONE]")) {
			return apitypes.StreamChunk{}, io.EOF
		}
		var chunk apitypes.StreamChunk
		if err := json.Unmarshal(ev.Data, &chunk); err != nil {
			// Skip keep-alives or malformed frames rather than aborting.
			continue
		}
		return chunk, nil
	}
}

func (s *openAIStream) Close() error { return s.body.Close() }

// Embeddings computes embeddings via the OpenAI-compatible endpoint.
func (o *OpenAI) Embeddings(ctx context.Context, req apitypes.EmbeddingRequest) (*apitypes.EmbeddingResponse, error) {
	httpReq, err := o.newRequest(ctx, "/embeddings", req)
	if err != nil {
		return nil, &Error{Provider: o.name, Message: "build request", Err: err}
	}
	resp, err := o.client.Do(httpReq)
	if err != nil {
		return nil, &Error{Provider: o.name, Message: "transport error", Retryable: true, Err: err}
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, &Error{
			Provider:   o.name,
			StatusCode: resp.StatusCode,
			Message:    extractErrorMessage(data),
			Retryable:  retryableStatus(resp.StatusCode),
		}
	}
	var out apitypes.EmbeddingResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, &Error{Provider: o.name, Message: "decode response", Err: err}
	}
	return &out, nil
}

// extractErrorMessage pulls a human-readable message out of an OpenAI-style
// error envelope, falling back to the raw body.
func extractErrorMessage(data []byte) string {
	var env apitypes.APIError
	if err := json.Unmarshal(data, &env); err == nil && env.Error.Message != "" {
		return env.Error.Message
	}
	if len(data) == 0 {
		return "empty error response"
	}
	if len(data) > 300 {
		data = data[:300]
	}
	return fmt.Sprintf("upstream error: %s", string(data))
}
