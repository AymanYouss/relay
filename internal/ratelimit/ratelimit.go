// Package ratelimit provides per-API-key request rate limiting.
//
// Two backends implement the same Limiter interface: a Redis fixed-window
// counter for multi-replica deployments (the window increment and TTL are set
// atomically in a single Lua script) and an in-process token bucket for tests
// and single-node use.
package ratelimit

import (
	"context"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// Decision is the result of a rate-limit check.
type Decision struct {
	Allowed    bool
	Limit      int
	Remaining  int
	RetryAfter time.Duration
}

// Limiter enforces a per-key requests-per-minute limit. A limit of 0 means
// unlimited and must always be allowed.
type Limiter interface {
	Allow(ctx context.Context, key string, rpm int) (Decision, error)
}

// --- Redis fixed-window limiter ---

// fixedWindowScript atomically increments the current minute's counter and sets
// its expiry on first use, returning the post-increment count.
var fixedWindowScript = redis.NewScript(`
local current = redis.call("INCR", KEYS[1])
if current == 1 then
  redis.call("EXPIRE", KEYS[1], ARGV[1])
end
return current
`)

// RedisLimiter is a distributed fixed-window rate limiter.
type RedisLimiter struct {
	rdb *redis.Client
}

// NewRedisLimiter builds a Redis-backed limiter.
func NewRedisLimiter(rdb *redis.Client) *RedisLimiter { return &RedisLimiter{rdb: rdb} }

// Allow checks and records a request against the current one-minute window.
func (l *RedisLimiter) Allow(ctx context.Context, key string, rpm int) (Decision, error) {
	if rpm <= 0 {
		return Decision{Allowed: true}, nil
	}
	window := time.Now().Unix() / 60
	redisKey := "relay:rl:" + key + ":" + itoa(window)
	count, err := fixedWindowScript.Run(ctx, l.rdb, []string{redisKey}, 60).Int()
	if err != nil {
		return Decision{}, err
	}
	remaining := rpm - count
	if remaining < 0 {
		remaining = 0
	}
	if count > rpm {
		// Time until the window rolls over.
		next := (window + 1) * 60
		return Decision{
			Allowed:    false,
			Limit:      rpm,
			Remaining:  0,
			RetryAfter: time.Until(time.Unix(next, 0)),
		}, nil
	}
	return Decision{Allowed: true, Limit: rpm, Remaining: remaining}, nil
}

// --- in-memory token bucket ---

type bucket struct {
	tokens   float64
	lastFill time.Time
}

// MemoryLimiter is a per-key token bucket refilling at rpm/60 tokens per second
// with burst capacity equal to rpm.
type MemoryLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	now     func() time.Time
}

// NewMemoryLimiter builds an in-memory limiter.
func NewMemoryLimiter() *MemoryLimiter {
	return &MemoryLimiter{buckets: map[string]*bucket{}, now: time.Now}
}

// Allow implements Limiter.
func (l *MemoryLimiter) Allow(_ context.Context, key string, rpm int) (Decision, error) {
	if rpm <= 0 {
		return Decision{Allowed: true}, nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	refillRate := float64(rpm) / 60.0
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: float64(rpm), lastFill: now}
		l.buckets[key] = b
	}
	elapsed := now.Sub(b.lastFill).Seconds()
	b.tokens = minf(float64(rpm), b.tokens+elapsed*refillRate)
	b.lastFill = now

	if b.tokens >= 1 {
		b.tokens--
		return Decision{Allowed: true, Limit: rpm, Remaining: int(b.tokens)}, nil
	}
	deficit := 1 - b.tokens
	return Decision{
		Allowed:    false,
		Limit:      rpm,
		Remaining:  0,
		RetryAfter: time.Duration(deficit / refillRate * float64(time.Second)),
	}, nil
}

func minf(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := v < 0
	if neg {
		v = -v
	}
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
