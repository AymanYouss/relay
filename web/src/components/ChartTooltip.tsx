import type { ReactNode } from 'react'

interface TooltipEntry {
  name?: string | number
  value?: number | string | Array<number | string>
  color?: string
  dataKey?: string | number
}

interface Props {
  active?: boolean
  payload?: TooltipEntry[]
  label?: string | number
  labelText?: string
  formatter?: (value: number, entry: TooltipEntry) => ReactNode
  nameMap?: Record<string, string>
}

export function ChartTooltip({
  active,
  payload,
  label,
  labelText,
  formatter,
  nameMap,
}: Props) {
  if (!active || !payload || payload.length === 0) return null
  return (
    <div className="rounded-lg border border-zinc-200 bg-white/95 px-3 py-2 shadow-cardhover backdrop-blur dark:border-zinc-700 dark:bg-zinc-900/95">
      <div className="mb-1.5 text-[11px] font-medium text-zinc-500 dark:text-zinc-400">
        {labelText ?? label}
      </div>
      <div className="space-y-1">
        {payload.map((entry, i) => {
          const key = String(entry.dataKey ?? entry.name ?? i)
          const displayName = nameMap?.[key] ?? entry.name ?? key
          return (
            <div key={i} className="flex items-center justify-between gap-4 text-xs">
              <span className="flex items-center gap-1.5 text-zinc-600 dark:text-zinc-300">
                <span
                  className="inline-block h-2 w-2 rounded-full"
                  style={{ backgroundColor: entry.color }}
                />
                {displayName}
              </span>
              <span className="nums font-semibold text-zinc-900 tabular-nums dark:text-zinc-100">
                {formatter && typeof entry.value === 'number'
                  ? formatter(entry.value, entry)
                  : entry.value}
              </span>
            </div>
          )
        })}
      </div>
    </div>
  )
}
