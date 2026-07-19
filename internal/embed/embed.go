// Package embed provides text embedding used by the semantic cache to measure
// prompt similarity. The primary implementation calls an OpenAI-compatible
// embeddings endpoint; a deterministic local implementation keeps the cache
// functional in tests and air-gapped environments without a network dependency.
package embed

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"math"
	"strings"

	"github.com/AymanYouss/relay/internal/apitypes"
	"github.com/AymanYouss/relay/internal/provider"
)

// Embedder turns text into dense vectors.
type Embedder interface {
	// Embed returns one vector per input string.
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	// Dimensions is the length of the vectors produced.
	Dimensions() int
}

// ProviderEmbedder embeds via an OpenAI-compatible provider.
type ProviderEmbedder struct {
	provider provider.Provider
	model    string
	dims     int
}

// NewProviderEmbedder wires an embedder to a provider and model.
func NewProviderEmbedder(p provider.Provider, model string, dims int) *ProviderEmbedder {
	return &ProviderEmbedder{provider: p, model: model, dims: dims}
}

func (e *ProviderEmbedder) Dimensions() int { return e.dims }

// Embed calls the provider's embeddings endpoint and returns vectors ordered to
// match the inputs.
func (e *ProviderEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	resp, err := e.provider.Embeddings(ctx, apitypes.EmbeddingRequest{
		Model: e.model,
		Input: texts,
	})
	if err != nil {
		return nil, err
	}
	out := make([][]float32, len(texts))
	for _, d := range resp.Data {
		if d.Index >= 0 && d.Index < len(out) {
			out[d.Index] = d.Embedding
		}
	}
	return out, nil
}

// HashEmbedder is a deterministic, dependency-free embedder. It projects token
// hashes into a fixed-dimension space and L2-normalizes the result, yielding a
// stable bag-of-words style vector where lexically similar prompts land close
// together. It is used for tests and offline development, never for production
// semantic quality.
type HashEmbedder struct {
	dims int
}

// NewHashEmbedder returns a HashEmbedder of the given dimensionality.
func NewHashEmbedder(dims int) *HashEmbedder {
	if dims <= 0 {
		dims = 256
	}
	return &HashEmbedder{dims: dims}
}

func (h *HashEmbedder) Dimensions() int { return h.dims }

// Embed produces deterministic vectors for the inputs.
func (h *HashEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = h.vector(t)
	}
	return out, nil
}

func (h *HashEmbedder) vector(text string) []float32 {
	vec := make([]float32, h.dims)
	for _, tok := range tokenize(text) {
		sum := sha256.Sum256([]byte(tok))
		idx := binary.BigEndian.Uint32(sum[0:4]) % uint32(h.dims)
		// Sign bit derived from a second slice keeps features signed.
		sign := float32(1)
		if sum[4]&1 == 1 {
			sign = -1
		}
		vec[idx] += sign
	}
	normalize(vec)
	return vec
}

func tokenize(s string) []string {
	s = strings.ToLower(s)
	fields := strings.FieldsFunc(s, func(r rune) bool {
		isAlnum := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		return !isAlnum
	})
	return fields
}

func normalize(v []float32) {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if sum == 0 {
		return
	}
	inv := float32(1 / math.Sqrt(sum))
	for i := range v {
		v[i] *= inv
	}
}

// Cosine returns the cosine similarity of two equal-length vectors.
func Cosine(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
