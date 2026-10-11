import { afterEach, describe, expect, it, vi } from 'vitest'
import { client, ApiError } from './client'
import { clearToken, getToken, setToken } from './store'

// Client tests exercise the real request pipeline (headers, auth injection,
// JSON decode, error mapping) against a stubbed fetch.
function mockFetch(handler: (url: string, init?: RequestInit) => Response) {
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL, init?: RequestInit) =>
      Promise.resolve(handler(String(input), init)),
    ),
  )
}

afterEach(() => {
  clearToken()
  vi.unstubAllGlobals()
})

describe('client.request', () => {
  it('sends the bearer token when present', async () => {
    setToken('ma_secret')
    let auth: string | undefined
    mockFetch((_, init) => {
      auth = (init?.headers as Record<string, string>)?.Authorization
      return new Response(JSON.stringify([]), { status: 200 })
    })
    await client.listMovies()
    expect(auth).toBe('Bearer ma_secret')
  })

  it('omits the Authorization header when not logged in', async () => {
    mockFetch((_, init) => {
      const headers = (init?.headers ?? {}) as Record<string, string>
      expect(headers.Authorization).toBeUndefined()
      return new Response(JSON.stringify([]), { status: 200 })
    })
    await client.listMovies()
  })

  it('throws ApiError with the server message on non-2xx', async () => {
    mockFetch(() => new Response(JSON.stringify({ error: 'title is required' }), { status: 400 }))
    await expect(client.addMovie({ title: '' })).rejects.toThrow('title is required')
    await expect(client.addMovie({ title: '' })).rejects.toSatisfy(
      (e: unknown) => e instanceof ApiError && e.status === 400,
    )
  })

  it('maps 401 to an expiry message', async () => {
    mockFetch(() => new Response(JSON.stringify({ error: 'invalid token' }), { status: 401 }))
    await expect(client.me()).rejects.toThrow('authentication expired or invalid')
  })

  it('returns typed JSON for list endpoints', async () => {
    mockFetch(() =>
      new Response(JSON.stringify([{ ID: 1, Title: 'Dune', Year: 2021, QualityProfile: '1080p', Monitored: true, AddedAt: '2026-01-01T00:00:00Z' }]), {
        status: 200,
      }),
    )
    const movies = await client.listMovies()
    expect(movies[0].Title).toBe('Dune')
  })
})

describe('client.login', () => {
  it('stores the returned token in memory (not browser storage)', async () => {
    mockFetch(() =>
      new Response(
        JSON.stringify({ token: 'ma_abc', user: 'admin', role: 'admin', token_prefix: 'ma_' }),
        { status: 200 },
      ),
    )
    const res = await client.login({ username: 'admin', password: 'a-very-long-password' })
    expect(res.token).toBe('ma_abc')
    expect(getToken()).toBe('ma_abc')
    expect(window.localStorage.length).toBe(0)
    expect(window.sessionStorage.length).toBe(0)
  })

  it('logout clears the in-memory token', async () => {
    setToken('ma_x')
    client.logout()
    expect(getToken()).toBeUndefined()
  })
})
