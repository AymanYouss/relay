import type { ReactNode } from 'react'
import { ArrowDownRight, ArrowUpRight } from 'lucide-react'
import { Card, cn } from './ui'

interface Props {
  label: string
  value: string
  sublabel?: ReactNode
  trend?: { value: string; direction: 'up' | 'down'; good: boolean }
  accent?: boolean
  spark?: ReactNode
}

export function MetricCard({ label, value, sublabel, trend, accent, spark }: Props) {
  return (
    <Card className={cn('relative overflow-hidden hover:shadow-cardhover', accent && 'ring-1 ring-brand-600/10')}>
      {accent && (
        <div className="pointer-events-none absolute inset-x-0 top-0 h-0.5 bg-gradient-to-r from-brand-500 to-brand-600" />
      )}
      <div className="p-5">
        <div className="flex items-center justify-between">
          <span className="text-[11px] font-semibold uppercase tracking-wider text-zinc-500 dark:text-zinc-400">
            {label}
          </span>
          {trend && (
            <span
              className={cn(
                'inline-flex items-center gap-0.5 rounded-md px-1.5 py-0.5 text-[11px] font-medium',
                trend.good
                  ? 'bg-brand-50 text-brand-700 dark:bg-brand-950 dark:text-brand-300'
                  : 'bg-amber-50 text-amber-700 dark:bg-amber-950/50 dark:text-amber-400'
              )}
            >
              {trend.direction === 'up' ? (
                <ArrowUpRight className="h-3 w-3" />
              ) : (
                <ArrowDownRight className="h-3 w-3" />
              )}
              {trend.value}
            </span>
          )}
        </div>
        <div className="mt-3 flex items-end justify-between gap-3">
          <div
            className={cn(
              'nums text-3xl font-semibold tracking-tight tabular-nums',
              accent ? 'text-brand-700 dark:text-brand-400' : 'text-zinc-900 dark:text-zinc-50'
            )}
          >
            {value}
          </div>
          {spark && <div className="mb-1 h-9 w-24 shrink-0">{spark}</div>}
        </div>
        {sublabel && (
          <div className="mt-2 text-xs text-zinc-500 dark:text-zinc-400">{sublabel}</div>
        )}
      </div>
    </Card>
  )
}
