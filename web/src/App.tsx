import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Sidebar } from './components/Sidebar'
import { TopBar } from './components/TopBar'
import { MetricCard } from './components/MetricCard'
import {
  CacheDonut,
  CostChart,
  HitRateChart,
  LatencyChart,
  Sparkline,
  StrategyBars,
} from './components/charts'
import { KeyTable, ModelTable, RecentFeed } from './components/tables'
import { Card, CardHeader, Label, cn } from './components/ui'
import {
  formatCompact,
  formatMs,
  formatPct,
  formatUSD,
} from './lib/format'
import { DEFAULT_TOKEN, fetchDashboard, getToken, setToken } from './lib/api'
import type { DashboardResult, WindowDays } from './lib/types'

const THEME_KEY = 'relay.theme'

export function App() {
  const [window, setWindow] = useState<WindowDays>(7)
  const [result, setResult] = useState<DashboardResult | null>(null)
  const [loading, setLoading] = useState(true)
  const [lastUpdated, setLastUpdated] = useState<Date | null>(null)
  const [token, setTokenState] = useState<string>(getToken())
  const [dark, setDark] = useState<boolean>(() => {
    try {
      return localStorage.getItem(THEME_KEY) === 'dark'
    } catch {
      return false
    }
  })
  const timer = useRef<number | null>(null)

  useEffect(() => {
    document.documentElement.classList.toggle('dark', dark)
    try {
      localStorage.setItem(THEME_KEY, dark ? 'dark' : 'light')
    } catch {
      /* ignore */
    }
  }, [dark])

  const load = useCallback(async (w: WindowDays) => {
    setLoading(true)
    const r = await fetchDashboard(w)
    setResult(r)
    setLastUpdated(new Date())
    setLoading(false)
  }, [])

  useEffect(() => {
    void load(window)
  }, [window, load])

  // Light polling for the "live" feel when connected to a real backend.
  useEffect(() => {
    if (timer.current) clearInterval(timer.current)
    timer.current = window
      ? (setInterval(() => void load(window), 30000) as unknown as number)
      : null
    return () => {
      if (timer.current) clearInterval(timer.current)
    }
  }, [window, load])

  const handleToken = (t: string) => {
    const value = t.trim() || DEFAULT_TOKEN
    setToken(value)
    setTokenState(value)
    void load(window)
  }

  const data = result?.data
  const config = result?.config
  const isDemo = result?.isDemo ?? false

  const summary = data?.summary
  const misses = summary ? summary.requests - summary.cache_hits : 0

  const savedShare = useMemo(() => {
    if (!summary) return 0
    const total = summary.total_cost_usd + summary.cost_saved_usd
    return total > 0 ? summary.cost_saved_usd / total : 0
  }, [summary])

  return (
    <div className="min-h-screen bg-zinc-50 dark:bg-zinc-950">
      <Sidebar
        serviceName={config?.service_name ?? 'relay-gateway'}
        version={config?.version ?? '1.0.0'}
      />

      <div className="lg:pl-60">
        <TopBar
          window={window}
          onWindow={setWindow}
          isDemo={isDemo}
          loading={loading}
          onRefresh={() => void load(window)}
          dark={dark}
          onToggleTheme={() => setDark((v) => !v)}
          token={token}
          onToken={handleToken}
          lastUpdated={lastUpdated}
        />

        {data && summary ? (
          <main className="animate-fadein px-5 py-6 sm:px-8">
            {/* Headline metrics */}
            <section className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
              <MetricCard
                label="Cache Hit Rate"
                value={formatPct(summary.cache_hit_rate)}
                accent
                trend={{ value: '+3.2 pts', direction: 'up', good: true }}
                sublabel={
                  <span>
                    <span className="nums tabular-nums">{formatCompact(summary.cache_hits)}</span> of{' '}
                    <span className="nums tabular-nums">{formatCompact(summary.requests)}</span>{' '}
                    served from cache
                  </span>
                }
                spark={<Sparkline data={data.series} dataKey="cache_hit_rate" color="#10b981" />}
              />
              <MetricCard
                label="Cost Saved"
                value={formatUSD(summary.cost_saved_usd, { compact: true })}
                accent
                trend={{ value: `${formatPct(savedShare, 0)} of spend`, direction: 'up', good: true }}
                sublabel={
                  <span>
                    via semantic cache over{' '}
                    <span className="nums tabular-nums">{summary.window_days}d</span>
                  </span>
                }
                spark={<Sparkline data={data.series} dataKey="cost_saved_usd" color="#2dd4bf" />}
              />
              <MetricCard
                label="p99 Latency"
                value={formatMs(summary.latency_p99_ms)}
                accent
                trend={{ value: '-48ms', direction: 'down', good: true }}
                sublabel={
                  <span>
                    p50 <span className="nums tabular-nums">{formatMs(summary.latency_p50_ms)}</span>
                    {'  '}·{'  '}p95{' '}
                    <span className="nums tabular-nums">{formatMs(summary.latency_p95_ms)}</span>
                  </span>
                }
                spark={<Sparkline data={data.series} dataKey="latency_p99_ms" color="#0f766e" />}
              />
              <MetricCard
                label="Total Requests"
                value={formatCompact(summary.requests)}
                trend={{ value: '+12.4%', direction: 'up', good: true }}
                sublabel={
                  <span>
                    error rate{' '}
                    <span
                      className={cn(
                        'nums tabular-nums',
                        summary.error_rate > 0.02
                          ? 'text-amber-600 dark:text-amber-400'
                          : 'text-zinc-500'
                      )}
                    >
                      {formatPct(summary.error_rate, 2)}
                    </span>
                  </span>
                }
                spark={<Sparkline data={data.series} dataKey="requests" color="#a1a1aa" />}
              />
            </section>

            {/* Cost + Latency */}
            <section className="mt-4 grid grid-cols-1 gap-4 xl:grid-cols-5">
              <Card className="xl:col-span-3">
                <CardHeader
                  title="Cost over time"
                  subtitle="Actual spend vs cost avoided by the semantic cache"
                  action={<LegendChips items={[
                    { label: 'Actual cost', color: '#0f766e' },
                    { label: 'Cost saved', color: '#2dd4bf' },
                  ]} />}
                />
                <div className="h-64 px-2 pb-4 pt-3">
                  <CostChart data={data.series} />
                </div>
              </Card>

              <Card className="xl:col-span-2">
                <CardHeader
                  title="Latency percentiles"
                  subtitle="Response time distribution over the window"
                  action={<LegendChips items={[
                    { label: 'p50', color: '#a1a1aa' },
                    { label: 'p95', color: '#14b8a6' },
                    { label: 'p99', color: '#0f766e' },
                  ]} />}
                />
                <div className="h-64 px-2 pb-4 pt-3">
                  <LatencyChart data={data.series} />
                </div>
              </Card>
            </section>

            {/* Cache performance + Routing */}
            <section className="mt-4 grid grid-cols-1 gap-4 xl:grid-cols-5">
              <Card className="xl:col-span-3">
                <CardHeader title="Cache performance" subtitle="Hit rate trend and hit/miss split" />
                <div className="grid grid-cols-1 gap-2 p-2 sm:grid-cols-5">
                  <div className="h-52 sm:col-span-3">
                    <div className="px-3 pt-1">
                      <Label>Hit rate over time</Label>
                    </div>
                    <div className="h-44">
                      <HitRateChart data={data.series} />
                    </div>
                  </div>
                  <div className="relative flex flex-col items-center justify-center sm:col-span-2">
                    <div className="relative h-40 w-40">
                      <CacheDonut hits={summary.cache_hits} misses={misses} />
                      <div className="pointer-events-none absolute inset-0 flex flex-col items-center justify-center">
                        <span className="nums text-2xl font-semibold tabular-nums text-zinc-900 dark:text-zinc-50">
                          {formatPct(summary.cache_hit_rate, 0)}
                        </span>
                        <span className="text-[10px] font-medium uppercase tracking-wider text-zinc-400">
                          hit rate
                        </span>
                      </div>
                    </div>
                    <div className="mt-3 flex gap-4">
                      <DonutLegend color="#10b981" label="Hits" value={formatCompact(summary.cache_hits)} />
                      <DonutLegend color="#d4d4d8" label="Misses" value={formatCompact(misses)} />
                    </div>
                  </div>
                </div>
              </Card>

              <Card className="xl:col-span-2">
                <CardHeader
                  title="Routing decisions"
                  subtitle="Requests by routing strategy"
                />
                <div className="h-44 px-2 pb-3 pt-4">
                  <StrategyBars data={data.routes} />
                </div>
                <div className="border-t border-zinc-100 px-5 py-3 dark:border-zinc-800">
                  <p className="text-xs text-zinc-500 dark:text-zinc-400">
                    Strategy{' '}
                    <span className="font-medium text-zinc-700 dark:text-zinc-300">
                      {config?.router.strategy ?? 'auto'}
                    </span>
                    , complexity threshold{' '}
                    <span className="nums font-medium tabular-nums text-zinc-700 dark:text-zinc-300">
                      {config?.router.complexity_threshold ?? 0.55}
                    </span>
                  </p>
                </div>
              </Card>
            </section>

            {/* Model usage + API keys */}
            <section className="mt-4 grid grid-cols-1 gap-4 xl:grid-cols-2">
              <Card>
                <CardHeader title="Model usage" subtitle="Traffic and cost by underlying model" />
                <div className="mt-2 px-1 pb-2">
                  <ModelTable models={data.models} />
                </div>
              </Card>

              <Card>
                <CardHeader title="API keys" subtitle="Consumption and budget utilization" />
                <div className="mt-2 px-1 pb-2">
                  <KeyTable keys={data.keys} />
                </div>
              </Card>
            </section>

            {/* Recent activity */}
            <section className="mt-4">
              <Card>
                <CardHeader
                  title="Recent activity"
                  subtitle="Latest inference requests across all keys"
                  action={
                    <span className="flex items-center gap-1.5 text-[11px] font-medium text-zinc-400">
                      <span
                        className={cn(
                          'h-1.5 w-1.5 rounded-full',
                          isDemo ? 'bg-zinc-400' : 'animate-pulsedot bg-brand-500'
                        )}
                      />
                      {isDemo ? 'sample' : 'streaming'}
                    </span>
                  }
                />
                <div className="mt-2 px-1 pb-2">
                  <RecentFeed events={data.recent} />
                </div>
              </Card>
            </section>

            {/* Configuration footer */}
            {config && <ConfigFooter config={config} />}

            <footer className="mt-6 flex flex-col items-center justify-between gap-2 border-t border-zinc-200/70 pt-5 text-xs text-zinc-400 dark:border-zinc-800 sm:flex-row">
              <span>
                Relay {config?.service_name ?? 'gateway'} · v{config?.version ?? '1.0.0'}
              </span>
              <span>
                Showing {summary.window_days}-day window
                {isDemo && ' · demo dataset'}
              </span>
            </footer>
          </main>
        ) : (
          <LoadingState />
        )}
      </div>
    </div>
  )
}

function LegendChips({ items }: { items: { label: string; color: string }[] }) {
  return (
    <div className="hidden items-center gap-3 sm:flex">
      {items.map((it) => (
        <span key={it.label} className="flex items-center gap-1.5 text-[11px] text-zinc-500 dark:text-zinc-400">
          <span className="h-2 w-2 rounded-full" style={{ backgroundColor: it.color }} />
          {it.label}
        </span>
      ))}
    </div>
  )
}

function DonutLegend({ color, label, value }: { color: string; label: string; value: string }) {
  return (
    <div className="flex items-center gap-1.5">
      <span className="h-2 w-2 rounded-full" style={{ backgroundColor: color }} />
      <span className="text-xs text-zinc-500 dark:text-zinc-400">{label}</span>
      <span className="nums text-xs font-semibold tabular-nums text-zinc-900 dark:text-zinc-100">
        {value}
      </span>
    </div>
  )
}

function ConfigFooter({ config }: { config: import('./lib/types').AppConfig }) {
  return (
    <section className="mt-4">
      <Card>
        <CardHeader title="Configuration" subtitle="Live gateway settings" />
        <div className="grid grid-cols-1 gap-px overflow-hidden rounded-b-xl bg-zinc-100 dark:bg-zinc-800 sm:grid-cols-2 lg:grid-cols-4">
          <ConfigCell label="Router strategy" value={config.router.strategy} />
          <ConfigCell
            label="Cache threshold"
            value={config.cache.enabled ? config.cache.similarity_threshold.toFixed(2) : 'disabled'}
          />
          <ConfigCell label="Cheap model" value={config.router.cheap_model} />
          <ConfigCell label="Strong model" value={config.router.strong_model} />
        </div>
        <div className="flex flex-wrap gap-2 border-t border-zinc-100 p-4 dark:border-zinc-800">
          {config.models.map((m) => (
            <span
              key={m.name}
              className="inline-flex items-center gap-1.5 rounded-lg border border-zinc-200 bg-zinc-50 px-2.5 py-1 text-xs dark:border-zinc-700 dark:bg-zinc-800/50"
            >
              <span className="font-medium text-zinc-700 dark:text-zinc-200">{m.name}</span>
              <span className="text-zinc-400">·</span>
              <span className="nums tabular-nums text-zinc-500">
                {formatUSD(m.input_price_per_m, { cents: true })}/
                {formatUSD(m.output_price_per_m, { cents: true })} per M
              </span>
            </span>
          ))}
        </div>
      </Card>
    </section>
  )
}

function ConfigCell({ label, value }: { label: string; value: string }) {
  return (
    <div className="bg-white p-4 dark:bg-zinc-900">
      <Label>{label}</Label>
      <div className="nums mt-1 truncate text-sm font-medium tabular-nums text-zinc-900 dark:text-zinc-100">
        {value}
      </div>
    </div>
  )
}

function LoadingState() {
  return (
    <main className="px-5 py-6 sm:px-8">
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
        {Array.from({ length: 4 }).map((_, i) => (
          <div
            key={i}
            className="h-32 animate-pulse rounded-xl border border-zinc-200 bg-white dark:border-zinc-800 dark:bg-zinc-900"
          />
        ))}
      </div>
      <div className="mt-4 grid grid-cols-1 gap-4 xl:grid-cols-5">
        <div className="h-72 animate-pulse rounded-xl border border-zinc-200 bg-white dark:border-zinc-800 dark:bg-zinc-900 xl:col-span-3" />
        <div className="h-72 animate-pulse rounded-xl border border-zinc-200 bg-white dark:border-zinc-800 dark:bg-zinc-900 xl:col-span-2" />
      </div>
    </main>
  )
}
