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
	"github.com/google/uuid"
)

const anthropicVersion = "2023-06-01"

// Anthropic adapts the Anthropic Messages API to Relay's OpenAI-compatible
// canonical types. It handles the two structural differences that matter:
// system prompts live in a top-level field, and the streaming protocol uses
// typed events rather than OpenAI's delta chunks. Both are normalized here so
// callers see a uniform OpenAI stream regardless of the backing vendor.
type Anthropic struct {
	name    string
	baseURL string
	apiKey  string
	client  *http.Client
}

// NewAnthropic constructs an Anthropic provider.
func NewAnthropic(name, baseURL, apiKey string, timeout time.Duration) *Anthropic {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com/v1"
	}
	return &Anthropic{
		name:    name,
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		client:  &http.Client{Timeout: timeout},
	}
}

func (a *Anthropic) Name() string { return a.name }
func (a *Anthropic) Kind() string { return "anthropic" }

// --- request translation ---

type anthropicRequest struct {
	Model         string             `json:"model"`
	Messages      []anthropicMessage `json:"messages"`
	System        string             `json:"system,omitempty"`
	MaxTokens     int                `json:"max_tokens"`
	Temperature   *float64           `json:"temperature,omitempty"`
	TopP          *float64           `json:"top_p,omitempty"`
	StopSequences []string           `json:"stop_sequences,omitempty"`
	Stream        bool               `json:"stream,omitempty"`
	Tools         []anthropicTool    `json:"tools,omitempty"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
}

func (a *Anthropic) buildPayload(req Request) anthropicRequest {
	b := req.Body
	var system []string
	msgs := make([]anthropicMessage, 0, len(b.Messages))
	for _, m := range b.Messages {
		switch m.Role {
		case apitypes.RoleSystem:
			system = append(system, m.Content)
		case apitypes.RoleTool:
			// Represent tool results as user turns so context is preserved.
			msgs = append(msgs, anthropicMessage{Role: apitypes.RoleUser, Content: m.Content})
		default:
			msgs = append(msgs, anthropicMessage{Role: m.Role, Content: m.Content})
		}
	}
	maxTokens := 4096
	if b.MaxTokens != nil && *b.MaxTokens > 0 {
		maxTokens = *b.MaxTokens
	}
	out := anthropicRequest{
		Model:         req.Model,
		Messages:      msgs,
		System:        strings.Join(system, "\n\n"),
		MaxTokens:     maxTokens,
		Temperature:   b.Temperature,
		TopP:          b.TopP,
		StopSequences: b.Stop,
		Stream:        b.Stream,
	}
	for _, t := range b.Tools {
		out.Tools = append(out.Tools, anthropicTool{
			Name:        t.Function.Name,
			Description: t.Function.Description,
			InputSchema: t.Function.Parameters,
		})
	}
	return out
}

func (a *Anthropic) newRequest(ctx context.Context, body any) (*http.Request, error) {
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/messages", bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", a.apiKey)
	req.Header.Set("anthropic-version", anthropicVersion)
	return req, nil
}

// --- response translation ---

type anthropicResponse struct {
	ID         string `json:"id"`
	Model      string `json:"model"`
	StopReason string `json:"stop_reason"`
	Content    []struct {
		Type  string          `json:"type"`
		Text  string          `json:"text"`
		ID    string          `json:"id"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	} `json:"content"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

func mapStopReason(reason string) string {
	switch reason {
	case "end_turn", "stop_sequence":
		return "stop"
	case "max_tokens":
		return "length"
	case "tool_use":
		return "tool_calls"
	default:
		return reason
	}
}

// ChatCompletion performs a non-streaming Anthropic completion.
func (a *Anthropic) ChatCompletion(ctx context.Context, req Request) (*apitypes.ChatCompletionResponse, error) {
	payload := a.buildPayload(req)
	payload.Stream = false
	httpReq, err := a.newRequest(ctx, payload)
	if err != nil {
		return nil, &Error{Provider: a.name, Message: "build request", Err: err}
	}
	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, &Error{Provider: a.name, Message: "transport error", Retryable: true, Err: err}
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, &Error{
			Provider:   a.name,
			StatusCode: resp.StatusCode,
			Message:    extractErrorMessage(data),
			Retryable:  retryableStatus(resp.StatusCode),
		}
	}
	var ar anthropicResponse
	if err := json.Unmarshal(data, &ar); err != nil {
		return nil, &Error{Provider: a.name, Message: "decode response", Err: err}
	}

	msg := apitypes.ChatMessage{Role: apitypes.RoleAssistant}
	var text strings.Builder
	for _, block := range ar.Content {
		switch block.Type {
		case "text":
			text.WriteString(block.Text)
		case "tool_use":
			msg.ToolCalls = append(msg.ToolCalls, apitypes.ToolCall{
				ID:   block.ID,
				Type: "function",
				Function: apitypes.FunctionCall{
					Name:      block.Name,
					Arguments: string(block.Input),
				},
			})
		}
	}
	msg.Content = text.String()

	return &apitypes.ChatCompletionResponse{
		ID:      ar.ID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   ar.Model,
		Choices: []apitypes.Choice{{
			Index:        0,
			Message:      msg,
			FinishReason: mapStopReason(ar.StopReason),
		}},
		Usage: apitypes.Usage{
			PromptTokens:     ar.Usage.InputTokens,
			CompletionTokens: ar.Usage.OutputTokens,
			TotalTokens:      ar.Usage.InputTokens + ar.Usage.OutputTokens,
		},
	}, nil
}

// ChatCompletionStream performs a streaming completion, translating Anthropic's
// typed SSE events into OpenAI-style delta chunks.
func (a *Anthropic) ChatCompletionStream(ctx context.Context, req Request) (StreamReader, error) {
	payload := a.buildPayload(req)
	payload.Stream = true
	httpReq, err := a.newRequest(ctx, payload)
	if err != nil {
		return nil, &Error{Provider: a.name, Message: "build request", Err: err}
	}
	httpReq.Header.Set("Accept", "text/event-stream")
	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, &Error{Provider: a.name, Message: "transport error", Retryable: true, Err: err}
	}
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		return nil, &Error{
			Provider:   a.name,
			StatusCode: resp.StatusCode,
			Message:    extractErrorMessage(data),
			Retryable:  retryableStatus(resp.StatusCode),
		}
	}
	return &anthropicStream{
		body:    resp.Body,
		scanner: newSSEScanner(resp.Body),
		id:      "chatcmpl-" + uuid.NewString(),
		model:   req.Model,
	}, nil
}

type anthropicStream struct {
	body     io.ReadCloser
	scanner  *sseScanner
	id       string
	model    string
	promptTk int
	outTk    int
	stop     string
	roleSent bool
}

// Anthropic streaming event payloads we care about.
type anthropicStreamEvent struct {
	Type    string `json:"type"`
	Message *struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	} `json:"message"`
	Delta *struct {
		Type       string `json:"type"`
		Text       string `json:"text"`
		StopReason string `json:"stop_reason"`
	} `json:"delta"`
	Usage *struct {
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

func (s *anthropicStream) Recv() (apitypes.StreamChunk, error) {
	for {
		ev, err := s.scanner.next()
		if err != nil {
			return apitypes.StreamChunk{}, err
		}
		if len(ev.Data) == 0 {
			continue
		}
		var ase anthropicStreamEvent
		if err := json.Unmarshal(ev.Data, &ase); err != nil {
			continue
		}
		switch ase.Type {
		case "message_start":
			if ase.Message != nil {
				s.id = "chatcmpl-" + ase.Message.ID
				s.model = ase.Message.Model
				s.promptTk = ase.Message.Usage.InputTokens
			}
			// Emit an opening chunk carrying the assistant role.
			s.roleSent = true
			return s.chunk(apitypes.ChatMessage{Role: apitypes.RoleAssistant}, nil, nil), nil
		case "content_block_delta":
			if ase.Delta != nil && ase.Delta.Type == "text_delta" && ase.Delta.Text != "" {
				return s.chunk(apitypes.ChatMessage{Content: ase.Delta.Text}, nil, nil), nil
			}
		case "message_delta":
			if ase.Delta != nil && ase.Delta.StopReason != "" {
				s.stop = mapStopReason(ase.Delta.StopReason)
			}
			if ase.Usage != nil {
				s.outTk = ase.Usage.OutputTokens
			}
		case "message_stop":
			reason := s.stop
			usage := &apitypes.Usage{
				PromptTokens:     s.promptTk,
				CompletionTokens: s.outTk,
				TotalTokens:      s.promptTk + s.outTk,
			}
			return s.chunk(apitypes.ChatMessage{}, &reason, usage), io.EOF
		case "error":
			return apitypes.StreamChunk{}, &Error{Provider: "anthropic", Message: "stream error", Retryable: true}
		}
	}
}

func (s *anthropicStream) chunk(delta apitypes.ChatMessage, finish *string, usage *apitypes.Usage) apitypes.StreamChunk {
	return apitypes.StreamChunk{
		ID:      s.id,
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   s.model,
		Choices: []apitypes.StreamChoice{{
			Index:        0,
			Delta:        delta,
			FinishReason: finish,
		}},
		Usage: usage,
	}
}

func (s *anthropicStream) Close() error { return s.body.Close() }

// Embeddings is not offered by Anthropic; callers should route embeddings to an
// OpenAI-compatible provider. Returning a typed error keeps the failure clear.
func (a *Anthropic) Embeddings(ctx context.Context, req apitypes.EmbeddingRequest) (*apitypes.EmbeddingResponse, error) {
	return nil, &Error{Provider: a.name, Message: fmt.Sprintf("provider %q does not support embeddings", a.name)}
}
