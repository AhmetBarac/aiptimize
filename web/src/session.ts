import { reactive } from 'vue'
import { api } from './api/client'
import type { Meta, SettingsDocument, User } from './api/types'

interface SessionState {
  bootstrapped: boolean
  meta: Meta | null
  user: User | null
  error: string | null
}

/** Administrators are the only accounts that may open Settings. */
export const isAdmin = (): boolean => session.user?.role === 'admin'

export const session = reactive<SessionState>({
  bootstrapped: false,
  meta: null,
  user: null,
  error: null,
})

/** Loads /meta, which also seeds the CSRF cookie. */
export async function loadMeta(): Promise<Meta | null> {
  try {
    session.meta = await api.get<Meta>('/meta')
    session.error = null
    return session.meta
  } catch (err) {
    session.error = err instanceof Error ? err.message : 'network error'
    return null
  }
}

/** Restores the session from the server on first navigation. */
export async function bootstrap(force = false): Promise<void> {
  if (session.bootstrapped && !force) return
  const meta = await loadMeta()
  if (meta && !meta.setup_required) {
    try {
      const me = await api.get<{ user: User }>('/auth/me')
      session.user = me.user
    } catch {
      session.user = null
    }
  }
  session.bootstrapped = true
}

export async function signIn(username: string, password: string): Promise<void> {
  const result = await api.post<{ user: User }>('/auth/login', { username, password })
  session.user = result.user
  session.bootstrapped = true
  await loadMeta()
}

export interface SetupPayload {
  username: string
  password: string
  site: {
    name: string
    timezone: string
    latitude: number
    longitude: number
  }
}

export async function completeSetup(payload: SetupPayload): Promise<void> {
  const result = await api.post<{ user: User }>('/setup', payload)
  session.user = result.user
  session.bootstrapped = true
  await loadMeta()
}

export async function signOut(): Promise<void> {
  try {
    await api.post('/auth/logout')
  } finally {
    session.user = null
  }
}

export async function loadSettings(): Promise<SettingsDocument> {
  return api.get<SettingsDocument>('/settings')
}

export async function patchSettings(
  section: string,
  values: unknown,
  extra?: Record<string, unknown>,
): Promise<void> {
  await api.patch('/settings', { section, values, ...extra })
}
