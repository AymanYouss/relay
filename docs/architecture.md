# Architecture

Relay is a single Go binary that exposes an OpenAI-compatible API and orchestrates
a fixed request pipeline. This document explains the moving parts and the design
decisions behind them.

![Architecture](portfolio/architecture.png)

## Request pipeline

The gateway (`internal/gateway`) composes the subsystems into one ordered path.
Both the non-streaming (`Complete`) and streaming (`Stream`) entry points share
the same admission, caching, routing and accounting logic.

1. **Admission** (`internal/auth`, `internal/ratelimit`, `internal/usage`)
   The API key is resolved to a principal. Model allow-list, per-minute rate limit
   and month-to-date budget are checked before any upstream work.

2. **Semantic cache** (`internal/cache`, `internal/embed`)
   The prompt is embedded and searched by cosine similarity within a namespace
   scoped to the requested model. A match above the configured threshold is
   returned immediately. The cache is a thin policy layer over a `VectorStore`
   interface with two implementations: an exact in-memory store and a Redis
   (RediSearch) HNSW index. The interface is deliberately narrow so an alternative
   backend (for example a BlazeKV-backed index) can drop in unchanged.

3. **Routing** (`internal/router`)
   On a miss, a `Classifier` scores prompt complexity from signals that correlate
   with difficulty (length, multi-turn depth, code and reasoning cues, tool use).
   The `auto` strategy routes below the threshold to the cheap model and above it
   to the strong model; `cost` and `quality` force a tier, and a pinned model name
   bypasses routing. The result is a chain: the chosen model followed by its
   configured fallbacks.

4. **Execution and failover** (`internal/router`)
   The `Executor` runs the chain with a shared retry budget, exponential backoff
   with jitter, and provider failover. Non-retryable errors (4xx, auth) abort
   immediately; transient errors (429, 5xx, transport) advance through the chain.
   For streaming, failover is possible up to the moment the first chunk is
   committed to the client.

5. **Providers** (`internal/provider`)
   Each adapter translates the canonical OpenAI-compatible request into its native
   dialect and normalizes the response, including streaming events. The Anthropic
   adapter lifts system prompts into the top-level field and converts the typed
   Messages event stream into OpenAI delta chunks. Local/self-hosted OpenAI
   -compatible servers (vLLM, Ollama, TGI) reuse the OpenAI adapter.

6. **Normalize, cache, account**
   The response is relabeled to the logical model, stored in the cache, and
   recorded. Accounting writes are detached from the request so a disconnected
   client never loses a usage record.

## State

A single Redis connection backs three concerns, each behind its own interface:

- **Vector index** for the semantic cache (RediSearch).
- **Rate-limit counters** via an atomic fixed-window Lua script.
- **Usage rollups** (daily aggregates, per-key monthly spend, recent activity)
  written in one pipeline per event.

All three have in-memory implementations, so the gateway runs with zero external
dependencies for local development, tests and demos (`backend: memory`).

## Observability

`internal/telemetry` provides OpenTelemetry tracing (OTLP exporter) and a private
Prometheus registry. The gateway emits spans for cache lookup, routing and each
upstream attempt, and metrics for request rate, cache results, latency
histograms, spend, tokens, retries and failovers. The admin analytics API reads
the usage rollups to drive the embedded dashboard.

## Configuration

Configuration (`internal/config`) is YAML with `${VAR}` / `${VAR:-default}`
environment expansion, so the same document promotes from local Docker to
Kubernetes with secrets injected from the environment. It is validated at load
time for referential integrity (models reference real providers, fallbacks and
router tiers reference real models).
