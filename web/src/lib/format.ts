export function formatNumber(n: number): string {
  return new Intl.NumberFormat('en-US').format(Math.round(n))
}

export function formatCompact(n: number): string {
  if (n < 1000) return formatNumber(n)
  if (n < 1_000_000) {
    const v = n / 1000
    return `${trim(v)}k`
  }
  if (n < 1_000_000_000) {
    const v = n / 1_000_000
    return `${trim(v)}M`
  }
  return `${trim(n / 1_000_000_000)}B`
}

function trim(v: number): string {
  return v >= 100 ? Math.round(v).toString() : v.toFixed(1).replace(/\.0$/, '')
}

export function formatUSD(n: number, opts?: { compact?: boolean; cents?: boolean }): string {
  if (opts?.compact && Math.abs(n) >= 1000) {
    return `$${formatCompact(n)}`
  }
  const digits = opts?.cents === false ? 0 : n < 100 ? 2 : n < 1000 ? 2 : 0
  return `$${new Intl.NumberFormat('en-US', {
    minimumFractionDigits: digits,
    maximumFractionDigits: digits,
  }).format(n)}`
}

export function formatUSDPrecise(n: number): string {
  if (n === 0) return '$0.00'
  if (n < 0.01) return `$${n.toFixed(4)}`
  return `$${n.toFixed(n < 1 ? 3 : 2)}`
}

export function formatPct(fraction: number, digits = 1): string {
  return `${(fraction * 100).toFixed(digits)}%`
}

export function formatMs(ms: number): string {
  if (ms >= 1000) return `${(ms / 1000).toFixed(2)}s`
  return `${Math.round(ms)}ms`
}

export function relativeTime(iso: string): string {
  const then = new Date(iso).getTime()
  const now = Date.now()
  const diff = Math.max(0, now - then)
  const sec = Math.round(diff / 1000)
  if (sec < 60) return `${sec}s ago`
  const min = Math.round(sec / 60)
  if (min < 60) return `${min}m ago`
  const hr = Math.round(min / 60)
  if (hr < 24) return `${hr}h ago`
  return `${Math.round(hr / 24)}d ago`
}

export function shortDate(iso: string): string {
  const d = new Date(iso + (iso.length === 10 ? 'T00:00:00' : ''))
  return d.toLocaleDateString('en-US', { month: 'short', day: 'numeric' })
}
