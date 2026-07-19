package auth

import (
	"net/http/httptest"
	"testing"

	"github.com/AymanYouss/relay/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthenticate(t *testing.T) {
	ks := NewKeyStore([]config.APIKeyConfig{
		{Key: "sk-alpha", Name: "alpha", RateLimitRPM: 100, MonthlyBudgetUSD: 50},
		{Key: "sk-beta", Name: "beta", AllowedModels: []string{"gpt-4o-mini"}},
	})

	p, ok := ks.Authenticate("sk-alpha")
	require.True(t, ok)
	assert.Equal(t, "alpha", p.Name)
	assert.Equal(t, 100, p.RateLimitRPM)
	assert.NotEmpty(t, p.ID)

	_, ok = ks.Authenticate("sk-wrong")
	assert.False(t, ok)

	_, ok = ks.Authenticate("")
	assert.False(t, ok)
}

func TestModelAllowed(t *testing.T) {
	ks := NewKeyStore([]config.APIKeyConfig{
		{Key: "sk-open", Name: "open"},
		{Key: "sk-limited", Name: "limited", AllowedModels: []string{"gpt-4o-mini"}},
	})

	open, _ := ks.Authenticate("sk-open")
	assert.True(t, open.ModelAllowed("anything"), "empty allow-list permits all")

	limited, _ := ks.Authenticate("sk-limited")
	assert.True(t, limited.ModelAllowed("gpt-4o-mini"))
	assert.False(t, limited.ModelAllowed("gpt-4o"))
}

func TestBearerToken(t *testing.T) {
	cases := []struct {
		header, value, want string
	}{
		{"Authorization", "Bearer sk-123", "sk-123"},
		{"Authorization", "bearer sk-lower", "sk-lower"},
		{"Authorization", "sk-raw", "sk-raw"},
		{"api-key", "sk-openai-style", "sk-openai-style"},
	}
	for _, c := range cases {
		r := httptest.NewRequest("POST", "/", nil)
		r.Header.Set(c.header, c.value)
		assert.Equal(t, c.want, BearerToken(r))
	}
}

func TestPrincipalsStableID(t *testing.T) {
	ks := NewKeyStore([]config.APIKeyConfig{{Key: "sk-x", Name: "x"}})
	ps := ks.Principals()
	require.Len(t, ps, 1)
	p, _ := ks.Authenticate("sk-x")
	assert.Equal(t, p.ID, ps[0].ID)
}
