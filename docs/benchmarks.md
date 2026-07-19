# Benchmarks

Relay ships with a reproducible benchmark harness (`cmd/relaybench`) and a mock
upstream (`cmd/mockllm`) that simulates realistic per-model inference latency and
token usage, so the semantic-cache value can be measured without provider spend.

## Methodology

The workload models real LLM traffic: a small pool of popular, cacheable prompts
(the "hot" set, with paraphrases) over a long tail of genuinely novel one-off
queries. `-repeat 0.45` means 45% of requests are drawn from the cacheable head
and 55% are unique. The same workload (same seed) is replayed twice, once with
the semantic cache disabled and once enabled.

```bash
# Terminal 1: mock upstream (simulates ~320ms cheap / ~680ms strong inference)
go run ./cmd/mockllm -addr :1234

# Terminal 2: gateway with the benchmark config
go run ./cmd/relay -config relay.bench.yaml

# Terminal 3: before / after
go run ./cmd/relaybench -c 32 -n 3000 -repeat 0.45 -cache=false -out bench/results/cache-off.json
go run ./cmd/relaybench -c 32 -n 3000 -repeat 0.45 -cache=true  -out bench/results/cache-on.json
```

Run parameters: 3,000 requests, concurrency 32, `auto` routing, seed 7.

## Results

| Metric | Cache off | Cache on | Change |
| --- | ---: | ---: | ---: |
| Semantic cache hit rate | 0.0% | **43.7%** | — |
| Realized cost | $0.5148 | **$0.2911** | **-43%** |
| Throughput | 70 req/s | **123 req/s** | **+76%** |
| Latency p50 | 454 ms | **352 ms** | -22% |
| Latency p95 | 570 ms | 560 ms | -2% |
| Latency p99 | 580 ms | 577 ms | ~0% |
| Cache-hit latency | — | **< 1 ms** | ~500x vs upstream |

## Reading the numbers

- **Cost tracks the hit rate.** With 44% of traffic served from cache, spend
  falls ~43%. On production traffic with higher repetition (support bots, RAG
  over a fixed corpus, evals), hit rates of 60 to 80% are common and the savings
  scale accordingly.
- **Cache hits are effectively free and instant** (sub-millisecond versus
  hundreds of milliseconds for a real completion), which is what lifts median
  latency and throughput.
- **Tail latency (p95/p99) barely moves**, and that is expected and honest: the
  tail is dominated by cache *misses*, which still make a real upstream call.
  Relay improves the cost and the typical-case latency, not the cost of a genuine
  miss.

Raw results are in [`bench/results/`](../bench/results). Numbers vary with
hardware and the simulated latency profile in `cmd/mockllm`; the relative
improvement is the reproducible part.
