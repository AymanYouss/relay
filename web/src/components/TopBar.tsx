import { useState } from 'react'
import { Check, KeyRound, Moon, RefreshCw, Sun } from 'lucide-react'
import { cn } from './ui'
import type { WindowDays } from '../lib/types'

const WINDOWS: { value: WindowDays; label: string }[] = [
  { value: 1, label: '24h' },
  { value: 7, label: '7d' },
  { value: 30, label: '30d' },
]

interface Props {
  window: WindowDays
  onWindow: (w: WindowDays) => void
  isDemo: boolean
  loading: boolean
  onRefresh: () => void
  dark: boolean
  onToggleTheme: () => void
  token: string
  onToken: (t: string) => void
  lastUpdated: Date | null
}

export function TopBar({
  window,
  onWindow,
  isDemo,
  loading,
  onRefresh,
  dark,
  onToggleTheme,
  token,
  onToken,
  lastUpdated,
}: Props) {
  const [tokenOpen, setTokenOpen] = useState(false)
  const [draft, setDraft] = useState(token)

  return (
    <header className="sticky top-0 z-20 flex h-16 items-center justify-between gap-4 border-b border-zinc-200/80 bg-zinc-50/80 px-5 backdrop-blur-md dark:border-zinc-800 dark:bg-zinc-950/80 sm:px-8">
      <div className="flex items-center gap-3">
        <div>
          <h1 className="text-[15px] font-semibold tracking-tight text-zinc-900 dark:text-zinc-50">
            Overview
          </h1>
          <p className="hidden text-xs text-zinc-500 dark:text-zinc-400 sm:block">
            Inference traffic, cost, and cache performance
          </p>
        </div>
      </div>

      <div className="flex items-center gap-2.5">
        {isDemo && (
          <span className="hidden items-center gap-1.5 rounded-md bg-amber-50 px-2 py-1 text-[11px] font-medium text-amber-700 ring-1 ring-inset ring-amber-200 dark:bg-amber-950/40 dark:text-amber-400 dark:ring-amber-900 sm:inline-flex">
            <span className="h-1.5 w-1.5 rounded-full bg-amber-500" />
            Demo data
          </span>
        )}

        <span className="hidden items-center gap-1.5 text-[11px] font-medium text-zinc-500 dark:text-zinc-400 md:inline-flex">
          <span
            className={cn(
              'h-1.5 w-1.5 rounded-full',
              isDemo ? 'bg-zinc-400' : 'animate-pulsedot bg-brand-500'
            )}
          />
          {isDemo ? 'Static' : 'Live'}
        </span>

        {/* Window selector */}
        <div className="flex items-center rounded-lg border border-zinc-200 bg-white p-0.5 dark:border-zinc-800 dark:bg-zinc-900">
          {WINDOWS.map((w) => (
            <button
              key={w.value}
              onClick={() => onWindow(w.value)}
              className={cn(
                'nums rounded-md px-2.5 py-1 text-xs font-medium tabular-nums transition-colors',
                window === w.value
                  ? 'bg-zinc-900 text-white shadow-sm dark:bg-zinc-100 dark:text-zinc-900'
                  : 'text-zinc-500 hover:text-zinc-900 dark:text-zinc-400 dark:hover:text-zinc-100'
              )}
            >
              {w.label}
            </button>
          ))}
        </div>

        <IconButton onClick={onRefresh} title="Refresh">
          <RefreshCw className={cn('h-4 w-4', loading && 'animate-spin')} />
        </IconButton>

        <IconButton onClick={onToggleTheme} title="Toggle theme">
          {dark ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
        </IconButton>

        <div className="relative">
          <IconButton onClick={() => setTokenOpen((v) => !v)} title="Admin token">
            <KeyRound className="h-4 w-4" />
          </IconButton>
          {tokenOpen && (
            <>
              <div className="fixed inset-0 z-30" onClick={() => setTokenOpen(false)} />
              <div className="absolute right-0 top-11 z-40 w-72 animate-fadein rounded-xl border border-zinc-200 bg-white p-3 shadow-cardhover dark:border-zinc-800 dark:bg-zinc-900">
                <label className="text-[11px] font-semibold uppercase tracking-wider text-zinc-500 dark:text-zinc-400">
                  Admin token
                </label>
                <div className="mt-2 flex items-center gap-2">
                  <input
                    type="password"
                    value={draft}
                    onChange={(e) => setDraft(e.target.value)}
                    className="nums min-w-0 flex-1 rounded-lg border border-zinc-200 bg-zinc-50 px-2.5 py-1.5 text-xs text-zinc-900 outline-none focus:border-brand-500 focus:ring-1 focus:ring-brand-500 dark:border-zinc-700 dark:bg-zinc-950 dark:text-zinc-100"
                    placeholder="Bearer token"
                  />
                  <button
                    onClick={() => {
                      onToken(draft)
                      setTokenOpen(false)
                    }}
                    className="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg bg-brand-600 text-white hover:bg-brand-700"
                    title="Save"
                  >
                    <Check className="h-4 w-4" />
                  </button>
                </div>
                <p className="mt-2 text-[11px] leading-relaxed text-zinc-500 dark:text-zinc-400">
                  Sent as{' '}
                  <code className="rounded bg-zinc-100 px-1 py-0.5 text-[10px] dark:bg-zinc-800">
                    Authorization: Bearer
                  </code>
                  . Stored locally.
                </p>
              </div>
            </>
          )}
        </div>

        <div className="hidden items-center border-l border-zinc-200 pl-3 dark:border-zinc-800 xl:flex">
          <span className="nums text-[11px] tabular-nums text-zinc-400 dark:text-zinc-500">
            {lastUpdated
              ? `Updated ${lastUpdated.toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit' })}`
              : ''}
          </span>
        </div>
      </div>
    </header>
  )
}

function IconButton({
  children,
  onClick,
  title,
}: {
  children: React.ReactNode
  onClick: () => void
  title: string
}) {
  return (
    <button
      onClick={onClick}
      title={title}
      className="flex h-8 w-8 items-center justify-center rounded-lg border border-zinc-200 bg-white text-zinc-500 transition-colors hover:bg-zinc-50 hover:text-zinc-900 dark:border-zinc-800 dark:bg-zinc-900 dark:text-zinc-400 dark:hover:bg-zinc-800 dark:hover:text-zinc-100"
    >
      {children}
    </button>
  )
}
