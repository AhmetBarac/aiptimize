/** Typed fetch wrapper for the Aiptimize API. */

export class ApiError extends Error {
  readonly code: string
  readonly status: number
  readonly details?: unknown

  constructor(code: string, message: string, status: number, details?: unknown) {
    super(message)
    this.name = 'ApiError'
    this.code = code
    this.status = status
    this.details = details
  }
}

interface ErrorEnvelope {
  error?: {
    code?: string
    message?: string
    details?: unknown
  }
}

/** Reads the double-submit CSRF cookie set by the server. */
export function csrfToken(): string {
  const prefix = 'aiptimize_csrf='
  for (const part of document.cookie.split(';')) {
    const row = part.trim()
    if (row.startsWith(prefix)) {
      return decodeURIComponent(row.slice(prefix.length))
    }
  }
  return ''
}

function parseBody(text: string): unknown {
  if (!text) return null
  try {
    return JSON.parse(text)
  } catch {
    return text
  }
}

function toApiError(data: unknown, status: number, fallback: string): ApiError {
  const envelope = (data ?? {}) as ErrorEnvelope
  const detail = envelope.error
  return new ApiError(
    detail?.code ?? 'http_error',
    detail?.message ?? fallback,
    status,
    detail?.details,
  )
}

/** Performs an API request against /api/v1. */
export async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers)
  const method = (init.method ?? 'GET').toUpperCase()
  if (init.body !== undefined && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json')
  }
  if (!['GET', 'HEAD', 'OPTIONS'].includes(method)) {
    const token = csrfToken()
    if (token) headers.set('X-CSRF-Token', token)
  }

  let response: Response
  try {
    response = await fetch(`/api/v1${path}`, {
      credentials: 'same-origin',
      ...init,
      headers,
    })
  } catch (err) {
    throw new ApiError('network_error', err instanceof Error ? err.message : 'Network error', 0)
  }

  if (response.status === 204) {
    return undefined as T
  }
  const text = await response.text()
  const data = parseBody(text)
  if (!response.ok) {
    throw toApiError(data, response.status, response.statusText)
  }
  return data as T
}

function body(value: unknown): string {
  return JSON.stringify(value ?? {})
}

export const api = {
  get: <T>(path: string) => request<T>(path),
  post: <T>(path: string, value?: unknown) => request<T>(path, { method: 'POST', body: body(value) }),
  patch: <T>(path: string, value: unknown) => request<T>(path, { method: 'PATCH', body: body(value) }),
  put: <T>(path: string, value: unknown) => request<T>(path, { method: 'PUT', body: body(value) }),
  del: <T>(path: string) => request<T>(path, { method: 'DELETE' }),
}
