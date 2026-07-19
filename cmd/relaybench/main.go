// Command relaybench is a load generator and benchmarking harness for a running
// Relay gateway. It replays a prompt workload (with a configurable share of
// repeated/paraphrased prompts) at a target concurrency and reports throughput,
// latency percentiles, semantic cache hit rate and realized cost.
//
// It is used to produce the before/after semantic-caching comparison: run once
// with -cache=false and once with -cache=true against the same workload.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type options struct {
	url         string
	apiKey      string
	model       string
	concurrency int
	requests    int
	repeatRate  float64
	cache       bool
	seed        int64
	out         string
}

type result struct {
	latency  time.Duration
	cacheHit bool
	cost     float64
	status   int
	err      error
}

// Summary is the machine-readable benchmark report.
type Summary struct {
	Label          string  `json:"label"`
	Requests       int     `json:"requests"`
	Concurrency    int     `json:"concurrency"`
	CacheEnabled   bool    `json:"cache_enabled"`
	Errors         int     `json:"errors"`
	Throughput     float64 `json:"throughput_rps"`
	CacheHitRate   float64 `json:"cache_hit_rate"`
	TotalCostUSD   float64 `json:"total_cost_usd"`
	LatencyP50Ms   float64 `json:"latency_p50_ms"`
	LatencyP95Ms   float64 `json:"latency_p95_ms"`
	LatencyP99Ms   float64 `json:"latency_p99_ms"`
	LatencyMeanMs  float64 `json:"latency_mean_ms"`
	WallSeconds    float64 `json:"wall_seconds"`
}

func main() {
	opt := options{}
	flag.StringVar(&opt.url, "url", "http://localhost:8080", "gateway base URL")
	flag.StringVar(&opt.apiKey, "key", envOr("RELAY_KEY", "sk-relay-team-a"), "API key")
	flag.StringVar(&opt.model, "model", "auto", "model or routing alias")
	flag.IntVar(&opt.concurrency, "c", 32, "concurrent workers")
	flag.IntVar(&opt.requests, "n", 2000, "total requests")
	flag.Float64Var(&opt.repeatRate, "repeat", 0.5, "fraction of requests drawn from a small repeated set (drives cache hits)")
	flag.BoolVar(&opt.cache, "cache", true, "enable semantic cache for this run")
	flag.Int64Var(&opt.seed, "seed", 1, "PRNG seed for reproducibility")
	flag.StringVar(&opt.out, "out", "", "optional path to write the JSON summary")
	flag.Parse()

	summary := run(opt)
	printSummary(summary)
	if opt.out != "" {
		writeJSON(opt.out, summary)
	}
}

func run(opt options) Summary {
	rng := rand.New(rand.NewSource(opt.seed))
	prompts := workload(rng)

	jobs := make(chan string, opt.concurrency)
	results := make(chan result, opt.requests)
	var wg sync.WaitGroup

	client := &http.Client{Timeout: 120 * time.Second}
	var inflight int64

	for i := 0; i < opt.concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for prompt := range jobs {
				atomic.AddInt64(&inflight, 1)
				results <- doRequest(client, opt, prompt)
				atomic.AddInt64(&inflight, -1)
			}
		}()
	}

	start := time.Now()
	go func() {
		// A fraction of traffic is drawn from a small pool of popular, cacheable
		// prompts; the rest are novel one-off queries. This mirrors real LLM
		// traffic (a cacheable head over a unique long tail) so the measured hit
		// rate reflects the cacheable share rather than artificial repetition.
		hot := hotSet(prompts, 12)
		for i := 0; i < opt.requests; i++ {
			var p string
			if rng.Float64() < opt.repeatRate {
				p = hot[rng.Intn(len(hot))]
			} else {
				p = novelPrompt(rng, prompts)
			}
			jobs <- p
		}
		close(jobs)
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	var (
		latencies []float64
		errors    int
		hits      int
		totalCost float64
	)
	for r := range results {
		if r.err != nil || r.status != http.StatusOK {
			errors++
			continue
		}
		latencies = append(latencies, float64(r.latency.Microseconds())/1000)
		if r.cacheHit {
			hits++
		}
		totalCost += r.cost
	}
	wall := time.Since(start)

	sort.Float64s(latencies)
	label := "cache-off"
	if opt.cache {
		label = "cache-on"
	}
	ok := len(latencies)
	s := Summary{
		Label:        label,
		Requests:     opt.requests,
		Concurrency:  opt.concurrency,
		CacheEnabled: opt.cache,
		Errors:       errors,
		WallSeconds:  wall.Seconds(),
		TotalCostUSD: totalCost,
	}
	if wall.Seconds() > 0 {
		s.Throughput = float64(ok) / wall.Seconds()
	}
	if ok > 0 {
		s.CacheHitRate = float64(hits) / float64(ok)
		s.LatencyP50Ms = pct(latencies, 50)
		s.LatencyP95Ms = pct(latencies, 95)
		s.LatencyP99Ms = pct(latencies, 99)
		s.LatencyMeanMs = mean(latencies)
	}
	return s
}

type chatRequest struct {
	Model      string        `json:"model"`
	Messages   []chatMessage `json:"messages"`
	RelayCache *bool         `json:"relay_cache,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Relay struct {
		CacheHit bool    `json:"cache_hit"`
		CostUSD  float64 `json:"cost_usd"`
	} `json:"relay"`
}

func doRequest(client *http.Client, opt options, prompt string) result {
	body := chatRequest{
		Model:    opt.model,
		Messages: []chatMessage{{Role: "user", Content: prompt}},
	}
	if !opt.cache {
		off := false
		body.RelayCache = &off
	}
	buf, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, opt.url+"/v1/chat/completions", bytes.NewReader(buf))
	if err != nil {
		return result{err: err}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+opt.apiKey)

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return result{err: err, latency: time.Since(start)}
	}
	defer resp.Body.Close()
	var cr chatResponse
	json.NewDecoder(resp.Body).Decode(&cr)
	return result{
		latency:  time.Since(start),
		cacheHit: cr.Relay.CacheHit,
		cost:     cr.Relay.CostUSD,
		status:   resp.StatusCode,
	}
}

func printSummary(s Summary) {
	fmt.Printf("\nRelay benchmark [%s]\n", s.Label)
	fmt.Printf("  requests      : %d (concurrency %d, errors %d)\n", s.Requests, s.Concurrency, s.Errors)
	fmt.Printf("  wall time     : %.2fs\n", s.WallSeconds)
	fmt.Printf("  throughput    : %.1f req/s\n", s.Throughput)
	fmt.Printf("  cache hit rate: %.1f%%\n", s.CacheHitRate*100)
	fmt.Printf("  latency p50   : %.1f ms\n", s.LatencyP50Ms)
	fmt.Printf("  latency p95   : %.1f ms\n", s.LatencyP95Ms)
	fmt.Printf("  latency p99   : %.1f ms\n", s.LatencyP99Ms)
	fmt.Printf("  total cost    : $%.4f\n", s.TotalCostUSD)
}

func writeJSON(path string, s Summary) {
	f, err := os.Create(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "write summary: %v\n", err)
		return
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	enc.Encode(s)
}

func hotSet(all []string, n int) []string {
	if n > len(all) {
		n = len(all)
	}
	return all[:n]
}

var fillerWords = strings.Fields(
	"quarterly telemetry migration rollout invoice tenant schema latency shard " +
		"webhook cursor backfill artifact namespace throughput checkpoint quota " +
		"replica manifest cohort embedding partition envelope",
)

// novelPrompt returns a semantically distinct, effectively unique prompt by
// appending a random context clause to a base prompt, modeling genuinely new
// queries that cannot be served from cache.
func novelPrompt(rng *rand.Rand, prompts []string) string {
	base := prompts[rng.Intn(len(prompts))]
	var b strings.Builder
	b.WriteString(base)
	b.WriteString(" Context:")
	for i := 0; i < 6; i++ {
		b.WriteByte(' ')
		b.WriteString(fillerWords[rng.Intn(len(fillerWords))])
	}
	fmt.Fprintf(&b, " id-%d.", rng.Int63())
	return b.String()
}

func pct(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	rank := int(p/100*float64(len(sorted)-1) + 0.5)
	if rank >= len(sorted) {
		rank = len(sorted) - 1
	}
	return sorted[rank]
}

func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	var sum float64
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs))
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

// workload returns a representative prompt universe. The first entries are the
// "hot" FAQ-style prompts (with paraphrases the semantic cache should collapse);
// the remainder is a long tail generated from templates so the distribution
// resembles real traffic: a modest set of popular questions over a long tail of
// unique ones.
func workload(rng *rand.Rand) []string {
	hot := []string{
		"What is the capital of France?",
		"What's the capital city of France?",
		"Summarize the plot of Romeo and Juliet in two sentences.",
		"Give me a two sentence summary of Romeo and Juliet.",
		"Explain how TCP congestion control works.",
		"How does TCP congestion control actually work?",
		"What are the tradeoffs between REST and gRPC?",
		"Compare REST and gRPC and their tradeoffs.",
		"Explain the CAP theorem with an example.",
		"How do I reverse a linked list in Go?",
		"What is the difference between a process and a thread?",
		"How do I center a div with CSS?",
	}

	topics := []string{
		"TLS handshakes", "database indexing", "garbage collection", "OAuth 2.0",
		"consistent hashing", "vector databases", "message queues", "load balancing",
		"container networking", "columnar storage", "write-ahead logs", "CRDTs",
		"backpressure", "service meshes", "content delivery networks", "quorum reads",
		"idempotency keys", "circuit breakers", "leader election", "sharding",
	}
	tasks := []string{
		"parse a CSV file", "debounce a function", "implement an LRU cache",
		"validate an email address", "merge two sorted arrays", "flatten a nested object",
		"retry with exponential backoff", "compute a moving average",
		"deduplicate a slice", "throttle API calls", "paginate a query",
		"stream a large file",
	}
	langs := []string{"Go", "Python", "TypeScript", "Rust", "Java"}

	var tail []string
	for _, t := range topics {
		tail = append(tail, "Explain how "+t+" work in detail, with tradeoffs.")
		tail = append(tail, "What are best practices for "+t+" in production?")
	}
	for _, task := range tasks {
		for _, l := range langs {
			tail = append(tail, "Write a "+l+" function to "+task+".")
		}
	}

	all := append(append([]string{}, hot...), tail...)
	// Shuffle only the tail so the hot prompts stay at the front for hotSet.
	rng.Shuffle(len(tail), func(i, j int) { tail[i], tail[j] = tail[j], tail[i] })
	copy(all[len(hot):], tail)
	return all
}
