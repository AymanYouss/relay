export interface Summary {
  requests: number
  cache_hits: number
  cache_hit_rate: number
  errors: number
  error_rate: number
  total_cost_usd: number
  cost_saved_usd: number
  prompt_tokens: number
  completion_tokens: number
  latency_p50_ms: number
  latency_p95_ms: number
  latency_p99_ms: number
  latency_avg_ms: number
  window_days: number
}

export interface SeriesPoint {
  date: string
  requests: number
  cache_hits: number
  cache_hit_rate: number
  cost_usd: number
  cost_saved_usd: number
  latency_p50_ms: number
  latency_p95_ms: number
  latency_p99_ms: number
}

export interface ModelUsage {
  model: string
  provider: string
  requests: number
  cost_usd: number
  tokens: number
  share: number
}

export interface RouteDecision {
  strategy: string
  requests: number
}

export interface ApiKey {
  key_id: string
  name: string
  requests: number
  cost_usd: number
  tokens: number
  budget_usd: number
  budget_used_pct: number
  rate_limit_rpm: number
}

export interface RecentEvent {
  time: string
  key_name: string
  requested_model: string
  model: string
  provider: string
  strategy: string
  complexity: number
  route_reason: string
  prompt_tokens: number
  completion_tokens: number
  cost_usd: number
  saved_usd: number
  cache_hit: boolean
  cache_score: number
  latency_ms: number
  status: 'ok' | 'error'
}

export interface Dashboard {
  summary: Summary
  series: SeriesPoint[]
  models: ModelUsage[]
  routes: RouteDecision[]
  keys: ApiKey[]
  recent: RecentEvent[]
}

export interface ModelConfig {
  name: string
  provider: string
  tier: string
  input_price_per_m: number
  output_price_per_m: number
}

export interface AppConfig {
  service_name: string
  version: string
  cache: {
    enabled: boolean
    similarity_threshold: number
    ttl_seconds: number
  }
  router: {
    strategy: string
    cheap_model: string
    strong_model: string
    complexity_threshold: number
  }
  models: ModelConfig[]
  providers: { name: string; kind: string }[]
}

export type WindowDays = 1 | 7 | 30

export interface DashboardResult {
  data: Dashboard
  config: AppConfig | null
  isDemo: boolean
}
