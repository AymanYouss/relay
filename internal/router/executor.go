package router

import (
	"context"
	"errors"
	"time"

	"github.com/AymanYouss/relay/internal/provider"
)

// AttemptFunc performs a single upstream attempt against the given model. It
// returns nil on success. The executor inspects the error to decide whether to
// retry or fail over.
type AttemptFunc func(ctx context.Context, model string) error

// Outcome describes how a chain execution resolved.
type Outcome struct {
	Model    string // model that ultimately succeeded (empty on failure)
	Attempts int    // total upstream attempts made
	Err      error  // non-nil if every attempt failed
}

// Executor runs a routing chain with bounded retries, exponential backoff and
// jitter, and provider failover. Non-retryable errors (e.g. 400s, auth
// failures) abort immediately; transient errors advance through the chain and,
// once the chain is exhausted, wrap back to the primary until the retry budget
// is spent.
type Executor struct {
	maxRetries int
	backoff    time.Duration
}

// NewExecutor builds an Executor.
func NewExecutor(maxRetries int, backoff time.Duration) *Executor {
	if maxRetries < 0 {
		maxRetries = 0
	}
	if backoff <= 0 {
		backoff = 200 * time.Millisecond
	}
	return &Executor{maxRetries: maxRetries, backoff: backoff}
}

// Run executes the chain. The retry budget is maxRetries additional attempts on
// top of the initial one, shared across the whole chain.
func (e *Executor) Run(ctx context.Context, chain []Hop, attempt AttemptFunc) Outcome {
	if len(chain) == 0 {
		return Outcome{Err: errors.New("router: empty routing chain")}
	}
	budget := e.maxRetries + 1
	var lastErr error
	attempts := 0

	for i := 0; i < budget; i++ {
		if err := ctx.Err(); err != nil {
			return Outcome{Attempts: attempts, Err: err}
		}
		hop := chain[i%len(chain)]
		attempts++
		err := attempt(ctx, hop.Model)
		if err == nil {
			return Outcome{Model: hop.Model, Attempts: attempts}
		}
		lastErr = err
		if !provider.IsRetryable(err) {
			return Outcome{Attempts: attempts, Err: err}
		}
		// Backoff before the next attempt, unless this was the last one.
		if i < budget-1 {
			if !sleep(ctx, e.backoffFor(i)) {
				return Outcome{Attempts: attempts, Err: ctx.Err()}
			}
		}
	}
	return Outcome{Attempts: attempts, Err: lastErr}
}

// backoffFor computes an exponential backoff with light deterministic jitter.
func (e *Executor) backoffFor(iteration int) time.Duration {
	d := e.backoff << iteration
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	// +/- 12.5% jitter derived from the iteration to avoid a global config dep.
	jitter := d / 8
	if iteration%2 == 0 {
		return d + jitter
	}
	return d - jitter
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
