package embed

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCosine(t *testing.T) {
	a := []float32{1, 0, 0}
	assert.InDelta(t, 1.0, Cosine(a, a), 1e-9)
	assert.InDelta(t, 0.0, Cosine([]float32{1, 0}, []float32{0, 1}), 1e-9)
	assert.InDelta(t, -1.0, Cosine([]float32{1, 0}, []float32{-1, 0}), 1e-9)
	// Mismatched lengths and zero vectors are handled gracefully.
	assert.Equal(t, 0.0, Cosine([]float32{1}, []float32{1, 2}))
	assert.Equal(t, 0.0, Cosine([]float32{0, 0}, []float32{0, 0}))
}

func TestHashEmbedderDeterministic(t *testing.T) {
	e := NewHashEmbedder(256)
	ctx := context.Background()
	v1, err := e.Embed(ctx, []string{"the quick brown fox"})
	require.NoError(t, err)
	v2, err := e.Embed(ctx, []string{"the quick brown fox"})
	require.NoError(t, err)
	assert.Equal(t, v1[0], v2[0], "same text must embed identically")
	assert.Equal(t, 256, e.Dimensions())
	assert.Len(t, v1[0], 256)
}

func TestHashEmbedderSimilarity(t *testing.T) {
	e := NewHashEmbedder(512)
	ctx := context.Background()
	vecs, err := e.Embed(ctx, []string{
		"how do I reset my password",
		"how can I reset my password please",
		"what is the tallest mountain in the world",
	})
	require.NoError(t, err)

	simRelated := Cosine(vecs[0], vecs[1])
	simUnrelated := Cosine(vecs[0], vecs[2])
	assert.Greater(t, simRelated, simUnrelated,
		"lexically similar prompts should be closer than unrelated ones")
	assert.Greater(t, simRelated, 0.5)
}

func BenchmarkHashEmbed(b *testing.B) {
	e := NewHashEmbedder(1536)
	ctx := context.Background()
	text := []string{"summarize the following document about distributed systems and consensus"}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = e.Embed(ctx, text)
	}
}
