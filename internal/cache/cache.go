package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/AymanYouss/relay/internal/apitypes"
	"github.com/AymanYouss/relay/internal/embed"
	"github.com/google/uuid"
)

// SemanticCache decides whether an incoming request can be served from a
// previously cached, semantically-similar response. It is intentionally thin:
// embedding is delegated to an Embedder and storage to a VectorStore, leaving
// this type responsible only for the hit/miss policy and serialization.
type SemanticCache struct {
	embedder      embed.Embedder
	store         VectorStore
	threshold     float64
	ttl           time.Duration
	namespace     string
	maxCandidates int
}

// Options configures a SemanticCache.
type Options struct {
	Threshold     float64
	TTL           time.Duration
	Namespace     string
	MaxCandidates int
}

// New builds a SemanticCache.
func New(embedder embed.Embedder, store VectorStore, opts Options) *SemanticCache {
	if opts.MaxCandidates <= 0 {
		opts.MaxCandidates = 5
	}
	if opts.Namespace == "" {
		opts.Namespace = "default"
	}
	return &SemanticCache{
		embedder:      embedder,
		store:         store,
		threshold:     opts.Threshold,
		ttl:           opts.TTL,
		namespace:     opts.Namespace,
		maxCandidates: opts.MaxCandidates,
	}
}

// Result is the outcome of a cache lookup.
type Result struct {
	Hit      bool
	Score    float64
	Response *apitypes.ChatCompletionResponse
	// Vector is the embedding of the looked-up prompt, returned so the caller can
	// store the eventual upstream response without recomputing it on a miss.
	Vector []float32
}

// namespaceFor scopes cache entries to the requested model so a cheap-model
// answer is never returned for a request that asked for a stronger model.
func (c *SemanticCache) namespaceFor(model string) string {
	return c.namespace + "|" + model
}

// Lookup embeds the request prompt and searches for a sufficiently similar
// cached response. On a miss it still returns the computed embedding vector so
// the caller can persist the fresh response cheaply.
func (c *SemanticCache) Lookup(ctx context.Context, req *apitypes.ChatCompletionRequest) (Result, error) {
	prompt := req.PromptText()
	vecs, err := c.embedder.Embed(ctx, []string{prompt})
	if err != nil {
		return Result{}, fmt.Errorf("embed prompt: %w", err)
	}
	if len(vecs) == 0 || len(vecs[0]) == 0 {
		return Result{}, fmt.Errorf("embedder returned no vector")
	}
	vec := vecs[0]

	matches, err := c.store.Search(ctx, c.namespaceFor(req.Model), vec, c.maxCandidates)
	if err != nil {
		return Result{}, err
	}
	for _, m := range matches {
		if m.Score >= c.threshold {
			var resp apitypes.ChatCompletionResponse
			if err := json.Unmarshal(m.Entry.Response, &resp); err != nil {
				continue
			}
			return Result{Hit: true, Score: m.Score, Response: &resp, Vector: vec}, nil
		}
	}
	return Result{Hit: false, Vector: vec}, nil
}

// Store persists a fresh upstream response keyed by the request's prompt vector.
// Passing the vector from a prior Lookup avoids a second embedding round-trip.
func (c *SemanticCache) Store(ctx context.Context, req *apitypes.ChatCompletionRequest, resp *apitypes.ChatCompletionResponse, vector []float32) error {
	if len(vector) == 0 {
		vecs, err := c.embedder.Embed(ctx, []string{req.PromptText()})
		if err != nil {
			return fmt.Errorf("embed prompt: %w", err)
		}
		vector = vecs[0]
	}
	payload, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("marshal response: %w", err)
	}
	entry := Entry{
		ID:        uuid.NewString(),
		Namespace: c.namespaceFor(req.Model),
		Prompt:    req.PromptText(),
		Model:     req.Model,
		Vector:    vector,
		Response:  payload,
		CreatedAt: time.Now(),
	}
	return c.store.Upsert(ctx, entry, c.ttl)
}

// Threshold returns the configured similarity threshold.
func (c *SemanticCache) Threshold() float64 { return c.threshold }
