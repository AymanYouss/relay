package router

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/AymanYouss/relay/internal/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecutorSucceedsFirstTry(t *testing.T) {
	e := NewExecutor(2, time.Millisecond)
	var tried []string
	out := e.Run(context.Background(), []Hop{{"a"}, {"b"}}, func(_ context.Context, m string) error {
		tried = append(tried, m)
		return nil
	})
	require.NoError(t, out.Err)
	assert.Equal(t, "a", out.Model)
	assert.Equal(t, 1, out.Attempts)
	assert.Equal(t, []string{"a"}, tried)
}

func TestExecutorFailsOverToNextModel(t *testing.T) {
	e := NewExecutor(2, time.Millisecond)
	var tried []string
	out := e.Run(context.Background(), []Hop{{"a"}, {"b"}}, func(_ context.Context, m string) error {
		tried = append(tried, m)
		if m == "a" {
			return &provider.Error{Provider: "a", StatusCode: 503, Retryable: true}
		}
		return nil
	})
	require.NoError(t, out.Err)
	assert.Equal(t, "b", out.Model)
	assert.Equal(t, 2, out.Attempts)
	assert.Equal(t, []string{"a", "b"}, tried)
}

func TestExecutorNonRetryableAbortsImmediately(t *testing.T) {
	e := NewExecutor(3, time.Millisecond)
	var count int
	out := e.Run(context.Background(), []Hop{{"a"}, {"b"}}, func(_ context.Context, _ string) error {
		count++
		return &provider.Error{Provider: "a", StatusCode: 400, Retryable: false}
	})
	require.Error(t, out.Err)
	assert.Equal(t, 1, count, "non-retryable errors must not retry")
	assert.Equal(t, 1, out.Attempts)
}

func TestExecutorExhaustsBudget(t *testing.T) {
	e := NewExecutor(2, time.Millisecond) // 3 total attempts
	var count int
	out := e.Run(context.Background(), []Hop{{"a"}, {"b"}}, func(_ context.Context, _ string) error {
		count++
		return &provider.Error{Retryable: true, Message: "boom"}
	})
	require.Error(t, out.Err)
	assert.Equal(t, 3, count)
	assert.Equal(t, 3, out.Attempts)
}

func TestExecutorRespectsContextCancellation(t *testing.T) {
	e := NewExecutor(5, 50*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	out := e.Run(ctx, []Hop{{"a"}}, func(_ context.Context, _ string) error {
		cancel()
		return &provider.Error{Retryable: true}
	})
	require.Error(t, out.Err)
	assert.True(t, errors.Is(out.Err, context.Canceled))
}

func TestExecutorEmptyChain(t *testing.T) {
	e := NewExecutor(1, time.Millisecond)
	out := e.Run(context.Background(), nil, func(context.Context, string) error { return nil })
	require.Error(t, out.Err)
}
