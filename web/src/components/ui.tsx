import type { ReactNode } from 'react'

export function cn(...parts: Array<string | false | null | undefined>): string {
  return parts.filter(Boolean).join(' ')
}

export function Card({
  children,
  className,
}: {
  children: ReactNode
  className?: string
}) {
  return (
    <div
      className={cn(
        'rounded-xl border border-zinc-200/80 bg-white shadow-card transition-shadow',
        'dark:border-zinc-800 dark:bg-zinc-900',
        className
      )}
    >
      {children}
    </div>
  )
}

export function CardHeader({
  title,
  subtitle,
  action,
}: {
  title: string
  subtitle?: string
  action?: ReactNode
}) {
  return (
    <div className="flex items-start justify-between gap-4 px-5 pt-5">
      <div>
        <h3 className="text-sm font-semibold text-zinc-900 dark:text-zinc-100">{title}</h3>
        {subtitle && (
          <p className="mt-0.5 text-xs text-zinc-500 dark:text-zinc-400">{subtitle}</p>
        )}
      </div>
      {action}
    </div>
  )
}

export function Label({ children }: { children: ReactNode }) {
  return (
    <span className="text-[11px] font-semibold uppercase tracking-wider text-zinc-500 dark:text-zinc-400">
      {children}
    </span>
  )
}

export function Badge({
  children,
  tone = 'neutral',
}: {
  children: ReactNode
  tone?: 'neutral' | 'success' | 'warning' | 'danger' | 'accent'
}) {
  const tones: Record<string, string> = {
    neutral:
      'bg-zinc-100 text-zinc-600 ring-zinc-200 dark:bg-zinc-800 dark:text-zinc-300 dark:ring-zinc-700',
    success:
      'bg-brand-50 text-brand-700 ring-brand-200 dark:bg-brand-950 dark:text-brand-300 dark:ring-brand-900',
    warning:
      'bg-amber-50 text-amber-700 ring-amber-200 dark:bg-amber-950/50 dark:text-amber-400 dark:ring-amber-900',
    danger:
      'bg-red-50 text-red-700 ring-red-200 dark:bg-red-950/50 dark:text-red-400 dark:ring-red-900',
    accent:
      'bg-brand-600/10 text-brand-700 ring-brand-600/20 dark:text-brand-300',
  }
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1 rounded-md px-1.5 py-0.5 text-[11px] font-medium ring-1 ring-inset',
        tones[tone]
      )}
    >
      {children}
    </span>
  )
}

export function ProgressBar({ pct }: { pct: number }) {
  const clamped = Math.min(100, Math.max(0, pct))
  const over = pct >= 100
  const tone =
    pct >= 100
      ? 'bg-red-500'
      : pct >= 85
        ? 'bg-amber-500'
        : 'bg-brand-500'
  return (
    <div className="flex items-center gap-2">
      <div className="h-1.5 w-full overflow-hidden rounded-full bg-zinc-100 dark:bg-zinc-800">
        <div
          className={cn('h-full rounded-full transition-all', tone)}
          style={{ width: `${clamped}%` }}
        />
      </div>
      <span
        className={cn(
          'nums w-11 shrink-0 text-right text-xs tabular-nums',
          over
            ? 'font-semibold text-red-600 dark:text-red-400'
            : pct >= 85
              ? 'font-medium text-amber-600 dark:text-amber-400'
              : 'text-zinc-500 dark:text-zinc-400'
        )}
      >
        {Math.round(pct)}%
      </span>
    </div>
  )
}

export function ProviderDot({ provider }: { provider: string }) {
  const colors: Record<string, string> = {
    openai: 'bg-brand-500',
    anthropic: 'bg-orange-400',
  }
  return (
    <span
      className={cn(
        'inline-block h-1.5 w-1.5 rounded-full',
        colors[provider.toLowerCase()] ?? 'bg-zinc-400'
      )}
    />
  )
}
