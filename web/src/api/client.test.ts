import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError, api, csrfToken } from './client'

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

describe('csrfToken', () => {
  afterEach(() => {
    document.cookie = 'aiptimize_csrf=; Max-Age=0; path=/'
  })

  it('reads the double-submit cookie', () => {
    document.cookie = 'aiptimize_csrf=abc123; path=/'
    expect(csrfToken()).toBe('abc123')
  })

  it('returns an empty string without a cookie', () => {
    expect(csrfToken()).toBe('')
  })
})

describe('api client', () => {
  beforeEach(() => {
    document.cookie = 'aiptimize_csrf=token-1; path=/'
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('sends the CSRF header on mutations and parses JSON', async () => {
    const fetchMock = vi.fn(async () => jsonResponse(200, { ok: true }))
    vi.stubGlobal('fetch', fetchMock)

    const result = await api.post<{ ok: boolean }>('/settings', { section: 'site' })

    expect(result.ok).toBe(true)
    const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit]
    expect(url).toBe('/api/v1/settings')
    expect(init.method).toBe('POST')
    const headers = init.headers as Headers
    expect(headers.get('X-CSRF-Token')).toBe('token-1')
    expect(headers.get('Content-Type')).toBe('application/json')
  })

  it('does not send the CSRF header on GET', async () => {
    const fetchMock = vi.fn(async () => jsonResponse(200, { status: 'ok' }))
    vi.stubGlobal('fetch', fetchMock)

    await api.get('/status')
    const [, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit]
    const headers = init.headers as Headers
    expect(headers.get('X-CSRF-Token')).toBeNull()
  })

  it('maps error envelopes to ApiError', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () =>
        jsonResponse(422, {
          error: { code: 'validation_failed', message: 'bad timezone', details: { field: 'timezone' } },
        }),
      ),
    )

    await expect(api.patch('/settings', {})).rejects.toMatchObject({
      name: 'ApiError',
      code: 'validation_failed',
      status: 422,
      message: 'bad timezone',
      details: { field: 'timezone' },
    })
  })

  it('wraps network failures', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => {
        throw new TypeError('Failed to fetch')
      }),
    )

    const error = await api.get('/meta').catch((err: unknown) => err)
    expect(error).toBeInstanceOf(ApiError)
    expect((error as ApiError).code).toBe('network_error')
  })

  it('returns undefined for 204 responses', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(null, { status: 204 })))
    await expect(api.post('/auth/logout')).resolves.toBeUndefined()
  })
})
