import { buildDemoConfig, buildDemoDashboard, isEmptyDashboard } from './demo'
import type { AppConfig, Dashboard, DashboardResult, WindowDays } from './types'

const TOKEN_KEY = 'relay.adminToken'
export const DEFAULT_TOKEN = 'change-me-admin-token'

export function getToken(): string {
  try {
    return localStorage.getItem(TOKEN_KEY) ?? DEFAULT_TOKEN
  } catch {
    return DEFAULT_TOKEN
  }
}

export function setToken(token: string): void {
  try {
    localStorage.setItem(TOKEN_KEY, token)
  } catch {
    /* ignore storage errors */
  }
}

async function authedFetch(path: string): Promise<Response> {
  return fetch(path, {
    headers: {
      Authorization: `Bearer ${getToken()}`,
      Accept: 'application/json',
    },
  })
}

export async function fetchDashboard(window: WindowDays): Promise<DashboardResult> {
  let data: Dashboard | null = null
  let config: AppConfig | null = null
  let ok = false

  try {
    const res = await authedFetch(`/admin/api/dashboard?window=${window}`)
    if (res.ok) {
      data = (await res.json()) as Dashboard
      ok = true
    }
  } catch {
    ok = false
  }

  // Fall back to demo data on any failure OR an empty/all-zero payload so the
  // dashboard always looks meaningful.
  const useDemo = !ok || isEmptyDashboard(data)
  if (useDemo) {
    data = buildDemoDashboard(window)
    config = buildDemoConfig()
    return { data, config, isDemo: true }
  }

  try {
    const res = await authedFetch('/admin/api/config')
    if (res.ok) {
      config = (await res.json()) as AppConfig
    }
  } catch {
    config = null
  }

  return { data: data as Dashboard, config, isDemo: false }
}
