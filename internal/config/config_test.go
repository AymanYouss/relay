package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpandEnv(t *testing.T) {
	t.Setenv("FOO", "bar")
	cases := map[string]string{
		"${FOO}":            "bar",
		"${MISSING:-def}":   "def",
		"${FOO:-def}":       "bar",
		"prefix-${FOO}-end": "prefix-bar-end",
		"${MISSING}":        "",
	}
	for in, want := range cases {
		got := string(expandEnv([]byte(in)))
		assert.Equal(t, want, got, "expandEnv(%q)", in)
	}
}

func TestParseDefaultsAndValidation(t *testing.T) {
	yaml := `
providers:
  - name: openai
    kind: openai
    api_key: sk-test
models:
  - name: gpt-4o-mini
    provider: openai
    tier: cheap
  - name: gpt-4o
    provider: openai
    tier: strong
router:
  cheap_model: gpt-4o-mini
  strong_model: gpt-4o
`
	cfg, err := Parse([]byte(yaml))
	require.NoError(t, err)

	// Defaults applied.
	assert.Equal(t, ":8080", cfg.Server.Addr)
	assert.Equal(t, ":9090", cfg.Server.AdminAddr)
	assert.Equal(t, "redis", cfg.Redis.Backend)
	assert.Equal(t, 0.92, cfg.Cache.SimilarityThreshold)
	assert.Equal(t, 24*time.Hour, cfg.Cache.TTL)
	assert.Equal(t, "auto", cfg.Router.Strategy)
	assert.Equal(t, 2, cfg.Router.MaxRetries)
	assert.Equal(t, 1536, cfg.Embedding.Dimensions)

	// Upstream defaults to model name.
	m, ok := cfg.ModelByName("gpt-4o")
	require.True(t, ok)
	assert.Equal(t, "gpt-4o", m.Upstream)
}

func TestValidationErrors(t *testing.T) {
	t.Run("no providers", func(t *testing.T) {
		_, err := Parse([]byte(`models: []`))
		require.Error(t, err)
	})
	t.Run("unknown provider kind", func(t *testing.T) {
		_, err := Parse([]byte(`
providers:
  - name: x
    kind: bogus
`))
		require.Error(t, err)
	})
	t.Run("model references unknown provider", func(t *testing.T) {
		_, err := Parse([]byte(`
providers:
  - name: openai
    kind: openai
models:
  - name: m1
    provider: missing
`))
		require.Error(t, err)
	})
	t.Run("unknown fallback", func(t *testing.T) {
		_, err := Parse([]byte(`
providers:
  - name: openai
    kind: openai
models:
  - name: m1
    provider: openai
    fallbacks: [ghost]
`))
		require.Error(t, err)
	})
	t.Run("router references undefined model", func(t *testing.T) {
		_, err := Parse([]byte(`
providers:
  - name: openai
    kind: openai
models:
  - name: m1
    provider: openai
router:
  strong_model: nope
`))
		require.Error(t, err)
	})
}

func TestExpandEnvInConfig(t *testing.T) {
	t.Setenv("MY_KEY", "sk-secret")
	cfg, err := Parse([]byte(`
providers:
  - name: openai
    kind: openai
    api_key: ${MY_KEY}
`))
	require.NoError(t, err)
	p, ok := cfg.ProviderByName("openai")
	require.True(t, ok)
	assert.Equal(t, "sk-secret", p.APIKey)
}
