import type {
  AppConfig,
  ApiKey,
  Dashboard,
  ModelUsage,
  RecentEvent,
  RouteDecision,
  SeriesPoint,
  Summary,
} from './types'

// Deterministic pseudo-random generator so demo data is stable across renders
// and looks internally consistent rather than jumping around on every load.
function mulberry32(seed: number) {
  let a = seed
  return () => {
    a |= 0
    a = (a + 0x6d2b79f5) | 0
    let t = Math.imul(a ^ (a >>> 15), 1 | a)
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
}

interface ModelSpec {
  model: string
  provider: string
  weight: number // share of traffic
  inPrice: number // $ / M input tokens
  outPrice: number // $ / M output tokens
  baseLatency: number
}

const MODEL_SPECS: ModelSpec[] = [
  { model: 'gpt-4o-mini', provider: 'openai', weight: 0.38, inPrice: 0.15, outPrice: 0.6, baseLatency: 420 },
  { model: 'claude-3-5-haiku', provider: 'anthropic', weight: 0.24, inPrice: 0.8, outPrice: 4.0, baseLatency: 480 },
  { model: 'gpt-4o', provider: 'openai', weight: 0.18, inPrice: 2.5, outPrice: 10.0, baseLatency: 720 },
  { model: 'claude-3-5-sonnet', provider: 'anthropic', weight: 0.14, inPrice: 3.0, outPrice: 15.0, baseLatency: 860 },
  { model: 'claude-3-opus', provider: 'anthropic', weight: 0.06, inPrice: 15.0, outPrice: 75.0, baseLatency: 1180 },
]

const KEY_SPECS = [
  { name: 'Production API', share: 0.44, budget: 5000, rpm: 1200 },
  { name: 'Mobile App', share: 0.27, budget: 2500, rpm: 600 },
  { name: 'Data Pipeline', share: 0.16, budget: 3000, rpm: 400 },
  { name: 'Internal Tools', share: 0.09, budget: 800, rpm: 200 },
  { name: 'Staging', share: 0.04, budget: 500, rpm: 120 },
]

const STRATEGIES = ['auto', 'cost', 'quality', 'cache']

const ROUTE_REASONS: Record<string, string[]> = {
  auto: ['low complexity: routed cheap', 'high complexity: routed strong', 'balanced routing'],
  cost: ['cost-optimized model', 'cheapest eligible model'],
  quality: ['quality tier selected', 'strong model required'],
  cache: ['semantic cache hit', 'exact cache hit'],
}

export function buildDemoDashboard(windowDays: number): Dashboard {
  const rand = mulberry32(0x5eed + windowDays)
  const days = windowDays
  const series: SeriesPoint[] = []

  let totalRequests = 0
  let totalCacheHits = 0
  let totalCost = 0
  let totalSaved = 0
  let totalPromptTokens = 0
  let totalCompletionTokens = 0
  let totalErrors = 0
  const p50s: number[] = []
  const p95s: number[] = []
  const p99s: number[] = []

  const today = new Date()
  today.setHours(0, 0, 0, 0)

  for (let i = days - 1; i >= 0; i--) {
    const d = new Date(today)
    d.setDate(today.getDate() - i)
    const date = d.toISOString().slice(0, 10)

    // Weekly rhythm: weekdays busier than weekends, plus a gentle growth trend.
    const dow = d.getDay()
    const weekend = dow === 0 || dow === 6
    const growth = 1 + (days - i) / (days * 3.2)
    const base = (weekend ? 5200 : 9400) * growth
    const requests = Math.round(base * (0.9 + rand() * 0.2))

    const hitRate = 0.41 + rand() * 0.09 // ~41-50%
    const cacheHits = Math.round(requests * hitRate)
    const errors = Math.round(requests * (0.004 + rand() * 0.006))

    // Latency wobbles around a base with occasional spikes.
    const spike = rand() > 0.86 ? 1.35 : 1
    const p50 = Math.round((190 + rand() * 60) * spike)
    const p95 = Math.round((520 + rand() * 140) * spike)
    const p99 = Math.round((860 + rand() * 320) * spike)

    // Cost: cache hits cost ~nothing. Compute per-day cost from served requests.
    const servedRequests = requests - cacheHits
    const avgCostPerReq = 0.0021 + rand() * 0.0006
    const cost = round4(servedRequests * avgCostPerReq)
    // Saved is the cost those hits WOULD have incurred, slightly above avg cost.
    const saved = round4(cacheHits * avgCostPerReq * (1.05 + rand() * 0.1))

    const promptTokens = Math.round(servedRequests * (620 + rand() * 180))
    const completionTokens = Math.round(servedRequests * (280 + rand() * 120))

    series.push({
      date,
      requests,
      cache_hits: cacheHits,
      cache_hit_rate: hitRate,
      cost_usd: cost,
      cost_saved_usd: saved,
      latency_p50_ms: p50,
      latency_p95_ms: p95,
      latency_p99_ms: p99,
    })

    totalRequests += requests
    totalCacheHits += cacheHits
    totalCost += cost
    totalSaved += saved
    totalPromptTokens += promptTokens
    totalCompletionTokens += completionTokens
    totalErrors += errors
    p50s.push(p50)
    p95s.push(p95)
    p99s.push(p99)
  }

  const summary: Summary = {
    requests: totalRequests,
    cache_hits: totalCacheHits,
    cache_hit_rate: totalCacheHits / totalRequests,
    errors: totalErrors,
    error_rate: totalErrors / totalRequests,
    total_cost_usd: round2(totalCost),
    cost_saved_usd: round2(totalSaved),
    prompt_tokens: totalPromptTokens,
    completion_tokens: totalCompletionTokens,
    latency_p50_ms: median(p50s),
    latency_p95_ms: percentile(p95s, 0.9),
    latency_p99_ms: percentile(p99s, 0.9),
    latency_avg_ms: median(p50s) * 1.4,
    window_days: days,
  }

  // Models: distribute served (non-cache) requests by weight.
  const servedTotal = totalRequests - totalCacheHits
  const models: ModelUsage[] = MODEL_SPECS.map((m) => {
    const requests = Math.round(servedTotal * m.weight)
    const promptTok = Math.round(requests * 700)
    const compTok = Math.round(requests * 320)
    const cost = round2(
      (promptTok / 1_000_000) * m.inPrice + (compTok / 1_000_000) * m.outPrice
    )
    return {
      model: m.model,
      provider: m.provider,
      requests,
      tokens: promptTok + compTok,
      cost_usd: cost,
      share: m.weight,
    }
  })
  // Normalise share to sum exactly to 1 based on request counts.
  const modelReqSum = models.reduce((s, m) => s + m.requests, 0)
  models.forEach((m) => (m.share = m.requests / modelReqSum))

  // Routing decisions.
  const routeWeights = [0.52, 0.19, 0.14, 0.15] // auto, cost, quality, cache
  const routes: RouteDecision[] = STRATEGIES.map((strategy, idx) => ({
    strategy,
    requests: Math.round(totalRequests * routeWeights[idx]),
  }))

  // API keys with varied budget usage (one intentionally near/over the line).
  const keys: ApiKey[] = KEY_SPECS.map((k, idx) => {
    const requests = Math.round(totalRequests * k.share)
    const tokens = Math.round(requests * 1000)
    // Month-to-date cost scaled so budget usage varies meaningfully.
    const usagePct = [0.62, 0.94, 0.41, 1.03, 0.18][idx]
    const mtdCost = round2(k.budget * usagePct)
    return {
      key_id: `key_${(idx + 1).toString().padStart(2, '0')}${randHex(rand, 6)}`,
      name: k.name,
      requests,
      cost_usd: mtdCost,
      tokens,
      budget_usd: k.budget,
      budget_used_pct: usagePct * 100,
      rate_limit_rpm: k.rpm,
    }
  })

  const recent = buildRecent(rand, 16)

  return { summary, series, models, routes, keys, recent }
}

function buildRecent(rand: () => number, count: number): RecentEvent[] {
  const events: RecentEvent[] = []
  const now = Date.now()
  const keyNames = KEY_SPECS.map((k) => k.name)
  const requestedModels = ['gpt-4o', 'claude-3-5-sonnet', 'auto', 'gpt-4o-mini', 'claude-3-5-haiku']

  for (let i = 0; i < count; i++) {
    const spec = pickWeighted(rand, MODEL_SPECS)
    const cacheHit = rand() < 0.44
    const strategy = cacheHit ? 'cache' : STRATEGIES[Math.floor(rand() * 3)]
    const complexity = round2(rand())
    const promptTokens = Math.round(400 + rand() * 900)
    const completionTokens = cacheHit ? 0 : Math.round(120 + rand() * 500)
    const fullCost = round4(
      (promptTokens / 1_000_000) * spec.inPrice + (completionTokens / 1_000_000) * spec.outPrice
    )
    const status: 'ok' | 'error' = rand() < 0.035 ? 'error' : 'ok'
    const latency = cacheHit
      ? Math.round(8 + rand() * 22)
      : Math.round(spec.baseLatency * (0.7 + rand() * 0.7))
    const reasons = ROUTE_REASONS[strategy] ?? ['routed']
    events.push({
      time: new Date(now - i * (18000 + rand() * 42000)).toISOString(),
      key_name: keyNames[Math.floor(rand() * keyNames.length)],
      requested_model: requestedModels[Math.floor(rand() * requestedModels.length)],
      model: spec.model,
      provider: spec.provider,
      strategy,
      complexity,
      route_reason: reasons[Math.floor(rand() * reasons.length)],
      prompt_tokens: promptTokens,
      completion_tokens: completionTokens,
      cost_usd: cacheHit ? 0 : fullCost,
      saved_usd: cacheHit ? fullCost : 0,
      cache_hit: cacheHit,
      cache_score: cacheHit ? round2(0.9 + rand() * 0.09) : round2(rand() * 0.6),
      latency_ms: latency,
      status,
    })
  }
  return events
}

export function buildDemoConfig(): AppConfig {
  return {
    service_name: 'relay-gateway',
    version: '1.4.2',
    cache: { enabled: true, similarity_threshold: 0.92, ttl_seconds: 86400 },
    router: {
      strategy: 'auto',
      cheap_model: 'gpt-4o-mini',
      strong_model: 'claude-3-5-sonnet',
      complexity_threshold: 0.55,
    },
    models: MODEL_SPECS.map((m) => ({
      name: m.model,
      provider: m.provider,
      tier: m.weight > 0.2 ? 'cheap' : 'strong',
      input_price_per_m: m.inPrice,
      output_price_per_m: m.outPrice,
    })),
    providers: [
      { name: 'openai', kind: 'openai' },
      { name: 'anthropic', kind: 'anthropic' },
    ],
  }
}

// Detect an empty/all-zero response so we can fall back to demo data.
export function isEmptyDashboard(d: Dashboard | null | undefined): boolean {
  if (!d || !d.summary) return true
  const s = d.summary
  return (!s.requests || s.requests === 0) && (!d.series || d.series.length === 0)
}

function pickWeighted(rand: () => number, specs: ModelSpec[]): ModelSpec {
  const total = specs.reduce((s, m) => s + m.weight, 0)
  let r = rand() * total
  for (const m of specs) {
    r -= m.weight
    if (r <= 0) return m
  }
  return specs[0]
}

function median(arr: number[]): number {
  return percentile(arr, 0.5)
}
function percentile(arr: number[], p: number): number {
  const sorted = [...arr].sort((a, b) => a - b)
  const idx = Math.min(sorted.length - 1, Math.floor(p * sorted.length))
  return sorted[idx] ?? 0
}
function round2(n: number): number {
  return Math.round(n * 100) / 100
}
function round4(n: number): number {
  return Math.round(n * 10000) / 10000
}
function randHex(rand: () => number, len: number): string {
  let s = ''
  const chars = 'abcdef0123456789'
  for (let i = 0; i < len; i++) s += chars[Math.floor(rand() * chars.length)]
  return s
}
