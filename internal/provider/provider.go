// Package provider defines the upstream model provider abstraction and the
// concrete adapters (OpenAI, Anthropic, local OpenAI-compatible servers).
//
// Every adapter translates Relay's canonical OpenAI-compatible request into the
// provider's native dialect and normalizes the response — including streaming
// events — back into the canonical shape. The rest of the gateway therefore
// never needs to know which vendor actually served a request.
package provider

import (
	"context"
	"errors"
	"fmt"

	"github.com/AymanYouss/relay/internal/apitypes"
)

// Request is a provider-bound chat completion request. Model is the upstream
// (provider-native) model id, already resolved from the logical model name.
type Request struct {
	Model string
	Body  apitypes.ChatCompletionRequest
}

// StreamReader yields normalized streaming chunks. Recv returns io.EOF when the
// stream completes cleanly. Implementations must be safe to Close at any time.
type StreamReader interface {
	Recv() (apitypes.StreamChunk, error)
	Close() error
}

// Provider is a single upstream model vendor.
type Provider interface {
	// Name is the configured provider name (e.g. "openai-prod").
	Name() string
	// Kind is the adapter family ("openai", "anthropic", "local").
	Kind() string
	// ChatCompletion performs a non-streaming completion.
	ChatCompletion(ctx context.Context, req Request) (*apitypes.ChatCompletionResponse, error)
	// ChatCompletionStream performs a streaming completion.
	ChatCompletionStream(ctx context.Context, req Request) (StreamReader, error)
	// Embeddings computes embedding vectors.
	Embeddings(ctx context.Context, req apitypes.EmbeddingRequest) (*apitypes.EmbeddingResponse, error)
}

// Error is a normalized provider error carrying enough information for the
// router to decide whether to retry or fail over.
type Error struct {
	Provider   string
	StatusCode int
	Message    string
	Retryable  bool
	Err        error
}

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("provider %s: %s: %v", e.Provider, e.Message, e.Err)
	}
	return fmt.Sprintf("provider %s: %s (status %d)", e.Provider, e.Message, e.StatusCode)
}

func (e *Error) Unwrap() error { return e.Err }

// IsRetryable reports whether err represents a transient failure worth retrying
// against the same or an alternate provider.
func IsRetryable(err error) bool {
	var pe *Error
	if errors.As(err, &pe) {
		return pe.Retryable
	}
	// Context cancellation is never retryable; unknown errors are treated as
	// transient so the failover chain gets a chance.
	return !errors.Is(err, context.Canceled)
}

// retryableStatus classifies HTTP status codes returned by upstreams.
func retryableStatus(code int) bool {
	switch code {
	case 408, 409, 425, 429, 500, 502, 503, 504:
		return true
	default:
		return false
	}
}

// Registry holds the set of configured providers, keyed by name.
type Registry struct {
	providers map[string]Provider
}

// NewRegistry builds a registry from a slice of providers.
func NewRegistry(providers ...Provider) *Registry {
	m := make(map[string]Provider, len(providers))
	for _, p := range providers {
		m[p.Name()] = p
	}
	return &Registry{providers: m}
}

// Get returns the provider registered under name.
func (r *Registry) Get(name string) (Provider, bool) {
	p, ok := r.providers[name]
	return p, ok
}

// Add registers or replaces a provider.
func (r *Registry) Add(p Provider) {
	if r.providers == nil {
		r.providers = map[string]Provider{}
	}
	r.providers[p.Name()] = p
}
