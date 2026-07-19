import {
  formatCompact,
  formatMs,
  formatPct,
  formatUSD,
  formatUSDPrecise,
  relativeTime,
} from '../lib/format'
import type { ApiKey, ModelUsage, RecentEvent } from '../lib/types'
import { Badge, ProgressBar, ProviderDot, cn } from './ui'
import { Zap } from 'lucide-react'

const TH =
  'px-4 py-2.5 text-left text-[11px] font-semibold uppercase tracking-wider text-zinc-400 dark:text-zinc-500'
const TD = 'px-4 py-3 text-sm text-zinc-700 dark:text-zinc-300'

export function ModelTable({ models }: { models: ModelUsage[] }) {
  const max = Math.max(...models.map((m) => m.requests), 1)
  return (
    <div className="overflow-x-auto">
      <table className="w-full min-w-[560px] border-collapse">
        <thead>
          <tr className="border-b border-zinc-200/80 dark:border-zinc-800">
            <th className={TH}>Model</th>
            <th className={cn(TH, 'text-right')}>Requests</th>
            <th className={cn(TH, 'text-right')}>Tokens</th>
            <th className={cn(TH, 'text-right')}>Cost</th>
            <th className={cn(TH, 'w-40')}>Share</th>
          </tr>
        </thead>
        <tbody>
          {models.map((m) => (
            <tr
              key={m.model}
              className="border-b border-zinc-100 last:border-0 transition-colors hover:bg-zinc-50/60 dark:border-zinc-800/60 dark:hover:bg-zinc-800/30"
            >
              <td className={TD}>
                <div className="flex items-center gap-2">
                  <ProviderDot provider={m.provider} />
                  <span className="font-medium text-zinc-900 dark:text-zinc-100">{m.model}</span>
                  <span className="text-xs text-zinc-400">{m.provider}</span>
                </div>
              </td>
              <td className={cn(TD, 'nums text-right tabular-nums')}>{formatCompact(m.requests)}</td>
              <td className={cn(TD, 'nums text-right tabular-nums text-zinc-500')}>
                {formatCompact(m.tokens)}
              </td>
              <td className={cn(TD, 'nums text-right font-medium tabular-nums text-zinc-900 dark:text-zinc-100')}>
                {formatUSD(m.cost_usd, { compact: true })}
              </td>
              <td className={TD}>
                <div className="flex items-center gap-2">
                  <div className="h-1.5 flex-1 overflow-hidden rounded-full bg-zinc-100 dark:bg-zinc-800">
                    <div
                      className="h-full rounded-full bg-brand-500"
                      style={{ width: `${(m.requests / max) * 100}%` }}
                    />
                  </div>
                  <span className="nums w-10 shrink-0 text-right text-xs tabular-nums text-zinc-500">
                    {formatPct(m.share, 0)}
                  </span>
                </div>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

export function KeyTable({ keys }: { keys: ApiKey[] }) {
  return (
    <div className="overflow-x-auto">
      <table className="w-full min-w-[680px] border-collapse">
        <thead>
          <tr className="border-b border-zinc-200/80 dark:border-zinc-800">
            <th className={TH}>Key</th>
            <th className={cn(TH, 'text-right')}>Requests</th>
            <th className={cn(TH, 'text-right')}>MTD cost</th>
            <th className={cn(TH, 'text-right')}>Budget</th>
            <th className={cn(TH, 'w-44')}>Budget used</th>
            <th className={cn(TH, 'text-right')}>Rate limit</th>
          </tr>
        </thead>
        <tbody>
          {keys.map((k) => (
            <tr
              key={k.key_id}
              className="border-b border-zinc-100 last:border-0 transition-colors hover:bg-zinc-50/60 dark:border-zinc-800/60 dark:hover:bg-zinc-800/30"
            >
              <td className={TD}>
                <div className="font-medium text-zinc-900 dark:text-zinc-100">{k.name}</div>
                <div className="nums text-[11px] tabular-nums text-zinc-400">{k.key_id}</div>
              </td>
              <td className={cn(TD, 'nums text-right tabular-nums')}>{formatCompact(k.requests)}</td>
              <td className={cn(TD, 'nums text-right font-medium tabular-nums text-zinc-900 dark:text-zinc-100')}>
                {formatUSD(k.cost_usd)}
              </td>
              <td className={cn(TD, 'nums text-right tabular-nums text-zinc-500')}>
                {formatUSD(k.budget_usd, { cents: false })}
              </td>
              <td className={TD}>
                <ProgressBar pct={k.budget_used_pct} />
              </td>
              <td className={cn(TD, 'nums text-right tabular-nums text-zinc-500')}>
                {formatCompact(k.rate_limit_rpm)} rpm
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

export function RecentFeed({ events }: { events: RecentEvent[] }) {
  return (
    <div className="overflow-x-auto">
      <table className="w-full min-w-[760px] border-collapse">
        <thead>
          <tr className="border-b border-zinc-200/80 dark:border-zinc-800">
            <th className={TH}>Time</th>
            <th className={TH}>Key</th>
            <th className={TH}>Model</th>
            <th className={TH}>Route</th>
            <th className={cn(TH, 'text-right')}>Latency</th>
            <th className={cn(TH, 'text-right')}>Cost</th>
            <th className={cn(TH, 'text-right')}>Status</th>
          </tr>
        </thead>
        <tbody>
          {events.map((e, i) => (
            <tr
              key={i}
              className="border-b border-zinc-100 last:border-0 transition-colors hover:bg-zinc-50/60 dark:border-zinc-800/60 dark:hover:bg-zinc-800/30"
            >
              <td className={cn(TD, 'nums whitespace-nowrap tabular-nums text-zinc-500')}>
                {relativeTime(e.time)}
              </td>
              <td className={cn(TD, 'whitespace-nowrap text-zinc-500')}>{e.key_name}</td>
              <td className={TD}>
                <div className="flex items-center gap-2">
                  <ProviderDot provider={e.provider} />
                  <span className="font-medium text-zinc-900 dark:text-zinc-100">{e.model}</span>
                  {e.cache_hit && (
                    <Badge tone="success">
                      <Zap className="h-2.5 w-2.5" /> cache
                    </Badge>
                  )}
                </div>
              </td>
              <td className={cn(TD, 'whitespace-nowrap')}>
                <span className="text-xs text-zinc-500">{e.strategy}</span>
              </td>
              <td
                className={cn(
                  TD,
                  'nums whitespace-nowrap text-right tabular-nums',
                  e.cache_hit ? 'text-brand-600 dark:text-brand-400' : 'text-zinc-700 dark:text-zinc-300'
                )}
              >
                {formatMs(e.latency_ms)}
              </td>
              <td className={cn(TD, 'nums whitespace-nowrap text-right tabular-nums')}>
                {e.cache_hit ? (
                  <span className="text-brand-600 dark:text-brand-400">
                    saved {formatUSDPrecise(e.saved_usd)}
                  </span>
                ) : (
                  <span className="text-zinc-900 dark:text-zinc-100">{formatUSDPrecise(e.cost_usd)}</span>
                )}
              </td>
              <td className={cn(TD, 'text-right')}>
                {e.status === 'ok' ? (
                  <Badge tone="neutral">ok</Badge>
                ) : (
                  <Badge tone="danger">error</Badge>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
