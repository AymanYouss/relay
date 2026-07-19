import {
  Area,
  AreaChart,
  Bar,
  BarChart,
  CartesianGrid,
  Cell,
  Line,
  LineChart,
  Pie,
  PieChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import { ChartTooltip } from './ChartTooltip'
import { formatCompact, formatMs, formatUSD, shortDate } from '../lib/format'
import type { SeriesPoint } from '../lib/types'

const AXIS = {
  stroke: 'currentColor',
  fontSize: 11,
  tickLine: false,
  axisLine: false,
}

const GRID = 'currentColor'

// ---- Cost over time (actual vs saved) ----
export function CostChart({ data }: { data: SeriesPoint[] }) {
  return (
    <ResponsiveContainer width="100%" height="100%">
      <AreaChart data={data} margin={{ top: 8, right: 8, left: 0, bottom: 0 }}>
        <defs>
          <linearGradient id="gCost" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="#0f766e" stopOpacity={0.28} />
            <stop offset="100%" stopColor="#0f766e" stopOpacity={0.02} />
          </linearGradient>
          <linearGradient id="gSaved" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="#2dd4bf" stopOpacity={0.22} />
            <stop offset="100%" stopColor="#2dd4bf" stopOpacity={0.02} />
          </linearGradient>
        </defs>
        <CartesianGrid vertical={false} stroke={GRID} strokeOpacity={0.08} />
        <XAxis
          dataKey="date"
          {...AXIS}
          className="text-zinc-400"
          tickFormatter={shortDate}
          minTickGap={28}
        />
        <YAxis
          {...AXIS}
          className="text-zinc-400"
          width={48}
          tickFormatter={(v: number) => formatUSD(v, { compact: true })}
        />
        <Tooltip
          content={(p) => (
            <ChartTooltip
              {...p}
              labelText={p.label ? shortDate(String(p.label)) : ''}
              formatter={(v) => formatUSD(v, { cents: true })}
              nameMap={{ cost_usd: 'Actual cost', cost_saved_usd: 'Cost saved' }}
            />
          )}
        />
        <Area
          type="monotone"
          dataKey="cost_saved_usd"
          stroke="#2dd4bf"
          strokeWidth={2}
          fill="url(#gSaved)"
          stackId="1"
        />
        <Area
          type="monotone"
          dataKey="cost_usd"
          stroke="#0f766e"
          strokeWidth={2}
          fill="url(#gCost)"
          stackId="1"
        />
      </AreaChart>
    </ResponsiveContainer>
  )
}

// ---- Latency percentiles ----
export function LatencyChart({ data }: { data: SeriesPoint[] }) {
  return (
    <ResponsiveContainer width="100%" height="100%">
      <LineChart data={data} margin={{ top: 8, right: 8, left: 0, bottom: 0 }}>
        <CartesianGrid vertical={false} stroke={GRID} strokeOpacity={0.08} />
        <XAxis
          dataKey="date"
          {...AXIS}
          className="text-zinc-400"
          tickFormatter={shortDate}
          minTickGap={28}
        />
        <YAxis
          {...AXIS}
          className="text-zinc-400"
          width={48}
          tickFormatter={(v: number) => `${v}ms`}
        />
        <Tooltip
          content={(p) => (
            <ChartTooltip
              {...p}
              labelText={p.label ? shortDate(String(p.label)) : ''}
              formatter={(v) => formatMs(v)}
              nameMap={{
                latency_p50_ms: 'p50',
                latency_p95_ms: 'p95',
                latency_p99_ms: 'p99',
              }}
            />
          )}
        />
        <Line type="monotone" dataKey="latency_p50_ms" stroke="#a1a1aa" strokeWidth={1.75} dot={false} />
        <Line type="monotone" dataKey="latency_p95_ms" stroke="#14b8a6" strokeWidth={1.75} dot={false} />
        <Line type="monotone" dataKey="latency_p99_ms" stroke="#0f766e" strokeWidth={2} dot={false} />
      </LineChart>
    </ResponsiveContainer>
  )
}

// ---- Cache hit rate over time ----
export function HitRateChart({ data }: { data: SeriesPoint[] }) {
  return (
    <ResponsiveContainer width="100%" height="100%">
      <AreaChart data={data} margin={{ top: 8, right: 8, left: 0, bottom: 0 }}>
        <defs>
          <linearGradient id="gHit" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="#10b981" stopOpacity={0.25} />
            <stop offset="100%" stopColor="#10b981" stopOpacity={0.02} />
          </linearGradient>
        </defs>
        <CartesianGrid vertical={false} stroke={GRID} strokeOpacity={0.08} />
        <XAxis
          dataKey="date"
          {...AXIS}
          className="text-zinc-400"
          tickFormatter={shortDate}
          minTickGap={28}
        />
        <YAxis
          {...AXIS}
          className="text-zinc-400"
          width={40}
          domain={[0, 0.7]}
          tickFormatter={(v: number) => `${Math.round(v * 100)}%`}
        />
        <Tooltip
          content={(p) => (
            <ChartTooltip
              {...p}
              labelText={p.label ? shortDate(String(p.label)) : ''}
              formatter={(v) => `${(v * 100).toFixed(1)}%`}
              nameMap={{ cache_hit_rate: 'Hit rate' }}
            />
          )}
        />
        <Area
          type="monotone"
          dataKey="cache_hit_rate"
          stroke="#10b981"
          strokeWidth={2}
          fill="url(#gHit)"
        />
      </AreaChart>
    </ResponsiveContainer>
  )
}

// ---- Donut: hits vs misses ----
export function CacheDonut({ hits, misses }: { hits: number; misses: number }) {
  const data = [
    { name: 'Cache hits', value: hits, color: '#10b981' },
    { name: 'Misses', value: misses, color: '#e4e4e7' },
  ]
  return (
    <ResponsiveContainer width="100%" height="100%">
      <PieChart>
        <Pie
          data={data}
          dataKey="value"
          nameKey="name"
          innerRadius="66%"
          outerRadius="100%"
          paddingAngle={2}
          startAngle={90}
          endAngle={-270}
          stroke="none"
        >
          {data.map((d, i) => (
            <Cell key={i} fill={d.color} className={i === 1 ? 'dark:opacity-30' : ''} />
          ))}
        </Pie>
        <Tooltip
          content={(p) => (
            <ChartTooltip {...p} formatter={(v) => formatCompact(v)} />
          )}
        />
      </PieChart>
    </ResponsiveContainer>
  )
}

// ---- Horizontal bars: routing by strategy ----
export function StrategyBars({
  data,
}: {
  data: { strategy: string; requests: number }[]
}) {
  const colors: Record<string, string> = {
    auto: '#0f766e',
    cost: '#14b8a6',
    quality: '#5eead4',
    cache: '#10b981',
  }
  return (
    <ResponsiveContainer width="100%" height="100%">
      <BarChart
        data={data}
        layout="vertical"
        margin={{ top: 0, right: 12, left: 0, bottom: 0 }}
        barCategoryGap={10}
      >
        <CartesianGrid horizontal={false} stroke={GRID} strokeOpacity={0.08} />
        <XAxis
          type="number"
          {...AXIS}
          className="text-zinc-400"
          tickFormatter={(v: number) => formatCompact(v)}
        />
        <YAxis
          type="category"
          dataKey="strategy"
          {...AXIS}
          className="text-zinc-500"
          width={62}
          tickFormatter={(v: string) => v.charAt(0).toUpperCase() + v.slice(1)}
        />
        <Tooltip
          cursor={{ fill: 'currentColor', opacity: 0.04 }}
          content={(p) => (
            <ChartTooltip
              {...p}
              formatter={(v) => `${formatCompact(v)} req`}
              nameMap={{ requests: 'Requests' }}
            />
          )}
        />
        <Bar dataKey="requests" radius={[0, 4, 4, 0]} barSize={16}>
          {data.map((d, i) => (
            <Cell key={i} fill={colors[d.strategy] ?? '#a1a1aa'} />
          ))}
        </Bar>
      </BarChart>
    </ResponsiveContainer>
  )
}

// ---- Sparkline for metric cards ----
export function Sparkline({
  data,
  dataKey,
  color = '#0f766e',
}: {
  data: SeriesPoint[]
  dataKey: string
  color?: string
}) {
  const id = `spark-${dataKey}`
  return (
    <ResponsiveContainer width="100%" height="100%">
      <AreaChart data={data} margin={{ top: 2, right: 0, left: 0, bottom: 0 }}>
        <defs>
          <linearGradient id={id} x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor={color} stopOpacity={0.3} />
            <stop offset="100%" stopColor={color} stopOpacity={0} />
          </linearGradient>
        </defs>
        <Area
          type="monotone"
          dataKey={dataKey}
          stroke={color}
          strokeWidth={1.5}
          fill={`url(#${id})`}
          isAnimationActive={false}
        />
      </AreaChart>
    </ResponsiveContainer>
  )
}
