# Relay

A high-performance, OpenAI-compatible LLM gateway and semantic router.

Relay sits in front of every model provider you use — OpenAI, Anthropic, and
self-hosted models — behind a single OpenAI-compatible endpoint, and adds the
production concerns that individual SDKs leave to you: semantic caching, cost- and
complexity-aware routing, automatic retries and failover, per-key budgets and
rate limits, and full OpenTelemetry tracing.

Full documentation is being assembled. See `docs/` for architecture and deployment.
