// Package auth implements API-key authentication for Relay's public API.
//
// Keys are provisioned from configuration and indexed by SHA-256 digest so the
// raw secret is never held in a lookup table and comparisons run in constant
// time. Each key carries its own rate limit, monthly budget and model
// allow-list, which the gateway enforces downstream.
package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/AymanYouss/relay/internal/config"
)

// Principal is the authenticated identity behind a request.
type Principal struct {
	// ID is a short, stable, non-secret identifier derived from the key digest,
	// safe to use in logs, metrics and the dashboard.
	ID   string
	Name string

	RateLimitRPM     int
	MonthlyBudgetUSD float64
	allowedModels    map[string]bool
}

// ModelAllowed reports whether this principal may call the given model. An empty
// allow-list permits every model.
func (p Principal) ModelAllowed(model string) bool {
	if len(p.allowedModels) == 0 {
		return true
	}
	return p.allowedModels[model]
}

type keyRecord struct {
	principal Principal
	digest    []byte
}

// KeyStore authenticates raw API keys against the configured set.
type KeyStore struct {
	byDigest map[string]keyRecord
}

// NewKeyStore builds a KeyStore from configured API keys.
func NewKeyStore(keys []config.APIKeyConfig) *KeyStore {
	ks := &KeyStore{byDigest: make(map[string]keyRecord, len(keys))}
	for _, k := range keys {
		if k.Key == "" {
			continue
		}
		digest := sha256.Sum256([]byte(k.Key))
		hexDigest := hex.EncodeToString(digest[:])
		allowed := make(map[string]bool, len(k.AllowedModels))
		for _, m := range k.AllowedModels {
			allowed[m] = true
		}
		name := k.Name
		if name == "" {
			name = "key-" + hexDigest[:8]
		}
		ks.byDigest[hexDigest] = keyRecord{
			digest: digest[:],
			principal: Principal{
				ID:               hexDigest[:12],
				Name:             name,
				RateLimitRPM:     k.RateLimitRPM,
				MonthlyBudgetUSD: k.MonthlyBudgetUSD,
				allowedModels:    allowed,
			},
		}
	}
	return ks
}

// Authenticate resolves a raw key to its principal.
func (ks *KeyStore) Authenticate(rawKey string) (Principal, bool) {
	if rawKey == "" {
		return Principal{}, false
	}
	digest := sha256.Sum256([]byte(rawKey))
	hexDigest := hex.EncodeToString(digest[:])
	rec, ok := ks.byDigest[hexDigest]
	if !ok {
		return Principal{}, false
	}
	// Constant-time confirmation guards against a crafted digest collision path.
	if subtle.ConstantTimeCompare(rec.digest, digest[:]) != 1 {
		return Principal{}, false
	}
	return rec.principal, true
}

// Principals returns every configured principal. Used by the admin API to list
// keys (including those with no usage yet) alongside their limits.
func (ks *KeyStore) Principals() []Principal {
	out := make([]Principal, 0, len(ks.byDigest))
	for _, rec := range ks.byDigest {
		out = append(out, rec.principal)
	}
	return out
}

// BearerToken extracts a key from an Authorization: Bearer header or the
// OpenAI-style api-key header.
func BearerToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); h != "" {
		if strings.HasPrefix(strings.ToLower(h), "bearer ") {
			return strings.TrimSpace(h[7:])
		}
		return strings.TrimSpace(h)
	}
	return strings.TrimSpace(r.Header.Get("api-key"))
}
