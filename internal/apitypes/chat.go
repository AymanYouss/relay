// Package apitypes defines the OpenAI-compatible wire types that Relay accepts
// on its public API and normalizes to when talking to upstream providers.
//
// Keeping a single canonical representation lets every provider adapter translate
// to and from its own dialect while the rest of the gateway (routing, caching,
// accounting) operates on one stable shape.
package apitypes

import "encoding/json"

// Role values for chat messages.
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
)

// ChatMessage is a single message in a chat completion request or response.
type ChatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	Name       string     `json:"name,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ToolCall represents a function/tool invocation emitted by a model.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// FunctionCall is the name/arguments pair of a tool call.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Tool describes a callable tool exposed to the model.
type Tool struct {
	Type     string             `json:"type"`
	Function ToolFunctionSchema `json:"function"`
}

// ToolFunctionSchema is the JSON schema description of a tool function.
type ToolFunctionSchema struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// ChatCompletionRequest is the canonical inbound chat request. It mirrors the
// OpenAI Chat Completions schema so existing SDKs work unmodified.
type ChatCompletionRequest struct {
	Model            string        `json:"model"`
	Messages         []ChatMessage `json:"messages"`
	Temperature      *float64      `json:"temperature,omitempty"`
	TopP             *float64      `json:"top_p,omitempty"`
	MaxTokens        *int          `json:"max_tokens,omitempty"`
	Stream           bool          `json:"stream,omitempty"`
	Stop             []string      `json:"stop,omitempty"`
	PresencePenalty  *float64      `json:"presence_penalty,omitempty"`
	FrequencyPenalty *float64      `json:"frequency_penalty,omitempty"`
	Tools            []Tool        `json:"tools,omitempty"`
	ToolChoice       any           `json:"tool_choice,omitempty"`
	User             string        `json:"user,omitempty"`

	// Relay extensions. These are optional and namespaced so they never collide
	// with upstream provider fields.
	RelayCache *bool  `json:"relay_cache,omitempty"` // per-request cache override
	RelayRoute string `json:"relay_route,omitempty"` // "auto", "quality", "cost", or a pinned model
}

// ChatCompletionResponse is the canonical non-streaming response.
type ChatCompletionResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`

	// Relay observability fields, surfaced to clients that want them.
	Relay *RelayMeta `json:"relay,omitempty"`
}

// Choice is one completion candidate.
type Choice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

// Usage reports token counts for a completion.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// RelayMeta carries gateway decisions back to the caller for transparency.
type RelayMeta struct {
	CacheHit       bool    `json:"cache_hit"`
	CacheScore     float64 `json:"cache_score,omitempty"`
	RoutedModel    string  `json:"routed_model"`
	RoutedProvider string  `json:"routed_provider"`
	RouteReason    string  `json:"route_reason,omitempty"`
	UpstreamMillis int64   `json:"upstream_ms,omitempty"`
	CostUSD        float64 `json:"cost_usd"`
	Attempts       int     `json:"attempts,omitempty"`
}

// StreamChunk is a single Server-Sent Events delta in a streaming response.
type StreamChunk struct {
	ID      string        `json:"id"`
	Object  string        `json:"object"`
	Created int64         `json:"created"`
	Model   string        `json:"model"`
	Choices []StreamChoice `json:"choices"`
	Usage   *Usage        `json:"usage,omitempty"`
}

// StreamChoice is a delta choice within a stream chunk.
type StreamChoice struct {
	Index        int          `json:"index"`
	Delta        ChatMessage  `json:"delta"`
	FinishReason *string      `json:"finish_reason"`
}

// PromptText flattens the messages into a single string used for embedding,
// semantic cache lookups, and complexity classification.
func (r *ChatCompletionRequest) PromptText() string {
	var b []byte
	for i, m := range r.Messages {
		if i > 0 {
			b = append(b, '\n')
		}
		b = append(b, m.Role...)
		b = append(b, ':', ' ')
		b = append(b, m.Content...)
	}
	return string(b)
}
