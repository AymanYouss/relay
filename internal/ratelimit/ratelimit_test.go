package ratelimit

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryLimiterUnlimited(t *testing.T) {
	l := NewMemoryLimiter()
	for i := 0; i < 1000; i++ {
		d, err := l.Allow(context.Background(), "k", 0)
		require.NoError(t, err)
		require.True(t, d.Allowed)
	}
}

func TestMemoryLimiterBurstThenBlock(t *testing.T) {
	l := NewMemoryLimiter()
	rpm := 5
	allowed := 0
	for i := 0; i < 10; i++ {
		d, err := l.Allow(context.Background(), "k", rpm)
		require.NoError(t, err)
		if d.Allowed {
			allowed++
		}
	}
	assert.Equal(t, rpm, allowed, "burst is capped at rpm")
}

func TestMemoryLimiterRefills(t *testing.T) {
	l := NewMemoryLimiter()
	base := time.Now()
	l.now = func() time.Time { return base }
	rpm := 60 // one token per second

	// Drain the bucket.
	for i := 0; i < rpm; i++ {
		d, _ := l.Allow(context.Background(), "k", rpm)
		require.True(t, d.Allowed)
	}
	d, _ := l.Allow(context.Background(), "k", rpm)
	require.False(t, d.Allowed)
	assert.Greater(t, d.RetryAfter, time.Duration(0))

	// Advance time by 2 seconds: ~2 tokens refill.
	l.now = func() time.Time { return base.Add(2 * time.Second) }
	d1, _ := l.Allow(context.Background(), "k", rpm)
	d2, _ := l.Allow(context.Background(), "k", rpm)
	assert.True(t, d1.Allowed)
	assert.True(t, d2.Allowed)
}

func TestMemoryLimiterPerKeyIsolation(t *testing.T) {
	l := NewMemoryLimiter()
	rpm := 2
	// Exhaust key A.
	l.Allow(context.Background(), "A", rpm)
	l.Allow(context.Background(), "A", rpm)
	da, _ := l.Allow(context.Background(), "A", rpm)
	assert.False(t, da.Allowed)
	// Key B is unaffected.
	db, _ := l.Allow(context.Background(), "B", rpm)
	assert.True(t, db.Allowed)
}
