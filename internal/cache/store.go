// Package cache implements Relay's semantic cache: incoming prompts are embedded
// and matched against previously seen prompts by vector similarity, so
// semantically equivalent requests (not just byte-identical ones) can be served
// from cache without hitting an upstream model.
//
// The cache is split into a small SemanticCache policy layer and a pluggable
// VectorStore. Two stores ship in-tree — an in-memory brute-force store for
// tests and single-node development, and a Redis (RediSearch) vector index for
// production — and the interface is deliberately narrow so an alternative
// backend (for example a BlazeKV-backed index) can drop in unchanged.
package cache

import (
	"context"
	"time"
)

// Entry is a cached prompt/response pair with its embedding vector.
type Entry struct {
	ID        string
	Namespace string
	Prompt    string
	Model     string
	Vector    []float32
	Response  []byte // serialized apitypes.ChatCompletionResponse
	CreatedAt time.Time
}

// Match is a search result with its cosine similarity score in [-1, 1].
type Match struct {
	Entry Entry
	Score float64
}

// VectorStore persists cache entries and answers nearest-neighbor queries.
type VectorStore interface {
	// Upsert stores or replaces an entry, expiring it after ttl (0 = no expiry).
	Upsert(ctx context.Context, e Entry, ttl time.Duration) error
	// Search returns up to k nearest entries within the namespace, ordered by
	// descending similarity.
	Search(ctx context.Context, namespace string, vector []float32, k int) ([]Match, error)
	// Ping verifies connectivity/readiness.
	Ping(ctx context.Context) error
	// Close releases resources.
	Close() error
}
