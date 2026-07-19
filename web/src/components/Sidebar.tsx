import {
  Activity,
  KeyRound,
  LayoutDashboard,
  Route,
  Server,
  Settings,
  Zap,
} from 'lucide-react'
import { cn } from './ui'

const NAV = [
  { id: 'overview', label: 'Overview', icon: LayoutDashboard },
  { id: 'routing', label: 'Routing', icon: Route },
  { id: 'cache', label: 'Cache', icon: Zap },
  { id: 'keys', label: 'API Keys', icon: KeyRound },
  { id: 'activity', label: 'Activity', icon: Activity },
  { id: 'models', label: 'Models', icon: Server },
]

export function Sidebar({
  serviceName,
  version,
}: {
  serviceName: string
  version: string
}) {
  return (
    <aside className="fixed inset-y-0 left-0 z-30 hidden w-60 flex-col border-r border-zinc-800 bg-zinc-950 lg:flex">
      <div className="flex h-16 items-center gap-2.5 px-6">
        <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-brand-600 shadow-sm">
          <svg viewBox="0 0 24 24" className="h-4.5 w-4.5" fill="none" width="18" height="18">
            <path
              d="M4 12h6l2-5 4 10 2-5h2"
              stroke="white"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
            />
          </svg>
        </div>
        <div className="leading-tight">
          <div className="text-[15px] font-semibold tracking-tight text-white">Relay</div>
          <div className="text-[10px] font-medium uppercase tracking-wider text-zinc-500">
            Gateway
          </div>
        </div>
      </div>

      <nav className="flex-1 space-y-0.5 px-3 py-4">
        <div className="px-3 pb-2 text-[10px] font-semibold uppercase tracking-wider text-zinc-600">
          Monitor
        </div>
        {NAV.map((item, idx) => {
          const Icon = item.icon
          const active = idx === 0
          return (
            <a
              key={item.id}
              href={`#${item.id}`}
              className={cn(
                'group flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition-colors',
                active
                  ? 'bg-zinc-800/80 text-white'
                  : 'text-zinc-400 hover:bg-zinc-900 hover:text-zinc-200'
              )}
            >
              <Icon
                className={cn(
                  'h-4 w-4 shrink-0',
                  active ? 'text-brand-400' : 'text-zinc-500 group-hover:text-zinc-300'
                )}
              />
              {item.label}
            </a>
          )
        })}
      </nav>

      <div className="border-t border-zinc-800 p-3">
        <a
          href="#settings"
          className="group flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium text-zinc-400 transition-colors hover:bg-zinc-900 hover:text-zinc-200"
        >
          <Settings className="h-4 w-4 text-zinc-500 group-hover:text-zinc-300" />
          Settings
        </a>
        <div className="mt-2 flex items-center justify-between rounded-lg bg-zinc-900/60 px-3 py-2">
          <div className="min-w-0">
            <div className="truncate text-xs font-medium text-zinc-300">{serviceName}</div>
            <div className="text-[10px] text-zinc-500">v{version}</div>
          </div>
          <span className="flex items-center gap-1.5 text-[10px] font-medium text-brand-400">
            <span className="h-1.5 w-1.5 rounded-full bg-brand-500" />
            healthy
          </span>
        </div>
      </div>
    </aside>
  )
}
