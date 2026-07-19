package cache

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/AymanYouss/relay/internal/embed"
)

// MemoryStore is an in-process, brute-force vector store. It is exact (no ANN
// approximation), which makes it ideal for tests and small single-node
// deployments, and it enforces TTLs lazily on read.
type MemoryStore struct {
	mu      sync.RWMutex
	entries map[string]Entry
	expiry  map[string]time.Time
}

// NewMemoryStore returns an empty in-memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		entries: make(map[string]Entry),
		expiry:  make(map[string]time.Time),
	}
}

// Upsert stores an entry.
func (m *MemoryStore) Upsert(_ context.Context, e Entry, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries[e.ID] = e
	if ttl > 0 {
		m.expiry[e.ID] = time.Now().Add(ttl)
	} else {
		delete(m.expiry, e.ID)
	}
	return nil
}

// Search returns the k most similar non-expired entries in the namespace.
func (m *MemoryStore) Search(_ context.Context, namespace string, vector []float32, k int) ([]Match, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	now := time.Now()
	matches := make([]Match, 0, len(m.entries))
	for id, e := range m.entries {
		if exp, ok := m.expiry[id]; ok && now.After(exp) {
			continue
		}
		if e.Namespace != namespace {
			continue
		}
		matches = append(matches, Match{Entry: e, Score: embed.Cosine(vector, e.Vector)})
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].Score > matches[j].Score })
	if len(matches) > k {
		matches = matches[:k]
	}
	return matches, nil
}

// Ping always succeeds for the in-memory store.
func (m *MemoryStore) Ping(context.Context) error { return nil }

// Close clears the store.
func (m *MemoryStore) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = map[string]Entry{}
	m.expiry = map[string]time.Time{}
	return nil
}
