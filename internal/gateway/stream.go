package gateway

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/AymanYouss/relay/internal/apitypes"
	"github.com/AymanYouss/relay/internal/auth"
	"github.com/AymanYouss/relay/internal/provider"
	"github.com/AymanYouss/relay/internal/router"
	"github.com/AymanYouss/relay/internal/usage"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// ChunkFunc receives one normalized streaming chunk. Returning an error aborts
// the stream (typically because the client disconnected).
type ChunkFunc func(apitypes.StreamChunk) error

// Stream runs the streaming pipeline. Failover is possible up to the moment the
// first chunk is committed to the client; afterwards the stream is bound to the
// chosen upstream. The returned metadata is also emitted to the client in the
// terminal chunk by the HTTP layer.
func (g *Gateway) Stream(ctx context.Context, p auth.Principal, req *apitypes.ChatCompletionRequest, onChunk ChunkFunc) (*apitypes.RelayMeta, error) {
	start := time.Now()
	ctx, span := g.deps.Tracer.Start(ctx, "gateway.Stream", trace.WithAttributes(
		attribute.String("relay.key", p.ID),
		attribute.String("relay.requested_model", req.Model),
	))
	defer span.End()

	if err := g.admit(ctx, p, req); err != nil {
		return nil, err
	}

	// Cache: serve a synthesized stream on hit.
	var cachedVector []float32
	if g.cacheEnabled(req) {
		res, err := g.lookupCache(ctx, req)
		if err != nil {
			g.deps.Logger.Warn("cache lookup failed", "err", err)
		} else if res.Hit {
			if err := g.streamCached(res.Response, onChunk); err != nil {
				return nil, err
			}
			g.recordCacheHit(ctx, p, req, res, start)
			return res.Response.Relay, nil
		} else {
			cachedVector = res.Vector
		}
	}

	decision := g.deps.Router.Resolve(req)
	span.SetAttributes(
		attribute.String("relay.strategy", decision.Strategy),
		attribute.Float64("relay.complexity", decision.Complexity),
	)
	if len(decision.Chain) == 0 {
		return nil, ErrNoUpstream
	}

	g.deps.Metrics.ActiveStreams.Inc()
	defer g.deps.Metrics.ActiveStreams.Dec()

	var (
		streamReader  provider.StreamReader
		firstChunk    apitypes.StreamChunk
		usedEntry     modelEntry
		upstreamStart time.Time
	)
	outcome := g.deps.Executor.Run(ctx, decision.Chain, func(ctx context.Context, model string) error {
		entry, ok := g.catalog.resolve(model)
		if !ok {
			return &provider.Error{Provider: model, Message: "model not in catalog"}
		}
		upstreamStart = time.Now()
		sr, err := entry.provider.ChatCompletionStream(ctx, provider.Request{Model: entry.upstream, Body: *req})
		if err != nil {
			return err
		}
		// Read the first chunk while we can still fail over cleanly.
		fc, err := sr.Recv()
		if err != nil && !errors.Is(err, io.EOF) {
			sr.Close()
			return err
		}
		streamReader = sr
		firstChunk = fc
		usedEntry = entry
		return nil
	})
	g.recordRetries(outcome)
	if outcome.Err != nil {
		g.recordError(ctx, p, req, decision, usedEntry, start)
		return nil, outcome.Err
	}
	defer streamReader.Close()
	g.deps.Metrics.UpstreamLatency.WithLabelValues(usedEntry.provider.Kind(), usedEntry.name).Observe(time.Since(upstreamStart).Seconds())

	// Relabel chunks to the logical model and stream to the client while
	// accumulating the full text and token usage for caching and accounting.
	var (
		content  strings.Builder
		usage    apitypes.Usage
		streamID = firstChunk.ID
	)
	if streamID == "" {
		streamID = "chatcmpl-" + uuid.NewString()
	}

	emit := func(chunk apitypes.StreamChunk) error {
		chunk.Model = usedEntry.name
		chunk.ID = streamID
		for _, ch := range chunk.Choices {
			content.WriteString(ch.Delta.Content)
		}
		if chunk.Usage != nil {
			usage = *chunk.Usage
		}
		return onChunk(chunk)
	}

	if firstChunk.ID != "" || len(firstChunk.Choices) > 0 {
		if err := emit(firstChunk); err != nil {
			return nil, err
		}
	}
	for {
		chunk, err := streamReader.Recv()
		if errors.Is(err, io.EOF) {
			if chunk.ID != "" || len(chunk.Choices) > 0 || chunk.Usage != nil {
				if e := emit(chunk); e != nil {
					return nil, e
				}
			}
			break
		}
		if err != nil {
			// Mid-stream failure: the client already has partial output, so we
			// cannot fail over. Record the error and stop.
			g.recordError(ctx, p, req, decision, usedEntry, start)
			return nil, err
		}
		if err := emit(chunk); err != nil {
			return nil, err
		}
	}

	cost := g.deps.Recorder.Cost(usedEntry.name, usage.PromptTokens, usage.CompletionTokens)
	meta := &apitypes.RelayMeta{
		CacheHit:       false,
		RoutedModel:    usedEntry.name,
		RoutedProvider: usedEntry.provider.Name(),
		RouteReason:    decision.Reason,
		UpstreamMillis: time.Since(upstreamStart).Milliseconds(),
		CostUSD:        cost,
		Attempts:       outcome.Attempts,
	}

	// Assemble a canonical response for caching from the streamed content.
	full := &apitypes.ChatCompletionResponse{
		ID:      streamID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   usedEntry.name,
		Choices: []apitypes.Choice{{
			Index:        0,
			Message:      apitypes.ChatMessage{Role: apitypes.RoleAssistant, Content: content.String()},
			FinishReason: "stop",
		}},
		Usage: usage,
	}
	if g.cacheEnabled(req) {
		g.storeCache(ctx, req, full, cachedVector)
	}
	g.record(ctx, buildEvent(p, req, decision, usedEntry, usage, cost, start), false)

	return meta, nil
}

// streamCached synthesizes an OpenAI-style stream from a cached response so
// streaming clients receive an identical event shape on a cache hit.
func (g *Gateway) streamCached(resp *apitypes.ChatCompletionResponse, onChunk ChunkFunc) error {
	id := resp.ID
	if id == "" {
		id = "chatcmpl-" + uuid.NewString()
	}
	now := time.Now().Unix()
	content := ""
	if len(resp.Choices) > 0 {
		content = resp.Choices[0].Message.Content
	}
	// Role opener.
	if err := onChunk(apitypes.StreamChunk{
		ID: id, Object: "chat.completion.chunk", Created: now, Model: resp.Model,
		Choices: []apitypes.StreamChoice{{Index: 0, Delta: apitypes.ChatMessage{Role: apitypes.RoleAssistant}}},
	}); err != nil {
		return err
	}
	// Content in a single delta (already fully known).
	if err := onChunk(apitypes.StreamChunk{
		ID: id, Object: "chat.completion.chunk", Created: now, Model: resp.Model,
		Choices: []apitypes.StreamChoice{{Index: 0, Delta: apitypes.ChatMessage{Content: content}}},
	}); err != nil {
		return err
	}
	// Terminal chunk with finish reason and usage.
	stop := "stop"
	return onChunk(apitypes.StreamChunk{
		ID: id, Object: "chat.completion.chunk", Created: now, Model: resp.Model,
		Choices: []apitypes.StreamChoice{{Index: 0, Delta: apitypes.ChatMessage{}, FinishReason: &stop}},
		Usage:   &resp.Usage,
	})
}

func buildEvent(p auth.Principal, req *apitypes.ChatCompletionRequest, d router.Decision, entry modelEntry, u apitypes.Usage, cost float64, start time.Time) usage.Event {
	return usage.Event{
		Time:             time.Now(),
		KeyID:            p.ID,
		KeyName:          p.Name,
		RequestedModel:   req.Model,
		Model:            entry.name,
		Provider:         entry.provider.Name(),
		Strategy:         d.Strategy,
		Complexity:       d.Complexity,
		RouteReason:      d.Reason,
		PromptTokens:     u.PromptTokens,
		CompletionTokens: u.CompletionTokens,
		CostUSD:          cost,
		LatencyMS:        time.Since(start).Milliseconds(),
		Status:           "ok",
	}
}
