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
      return new Response(JSON.stringify({ items: [], total: 0, page: 1, page_size: 25 }), { status: 200 })
    })
    await client.listMovies()
    expect(auth).toBe('Bearer ma_secret')
  })

  it('omits the Authorization header when not logged in', async () => {
    mockFetch((_, init) => {
      const headers = (init?.headers ?? {}) as Record<string, string>
      expect(headers.Authorization).toBeUndefined()
      return new Response(JSON.stringify({ items: [], total: 0, page: 1, page_size: 25 }), { status: 200 })
    })
    await client.listMovies()
  })

  it('throws ApiError with the server message on non-2xx', async () => {
    mockFetch(() => new Response(JSON.stringify({ error: 'title is required' }), { status: 400 }))
    await expect(client.addMovie({ title: '', minimum_availability: 'released', start_search: false })).rejects.toThrow('title is required')
    await expect(client.addMovie({ title: '', minimum_availability: 'released', start_search: false })).rejects.toSatisfy(
      (e: unknown) => e instanceof ApiError && e.status === 400,
    )
  })

  it('maps 401 to an expiry message', async () => {
    mockFetch(() => new Response(JSON.stringify({ error: 'invalid token' }), { status: 401 }))
    await expect(client.me()).rejects.toThrow('authentication expired or invalid')
  })

  it('returns the paged envelope for list endpoints (#41)', async () => {
    mockFetch(() =>
      new Response(
        JSON.stringify({
          items: [{ ID: 1, Title: 'Dune', Year: 2021, QualityProfile: '1080p', Monitored: true, AddedAt: '2026-01-01T00:00:00Z' }],
          total: 1,
          page: 1,
          page_size: 25,
        }),
        { status: 200 },
      ),
    )
    const page = await client.listMovies()
    expect(page.total).toBe(1)
    expect(page.items[0].Title).toBe('Dune')
  })

  it('serializes ListQuery into the list URL (#41)', async () => {
    let url = ''
    mockFetch((u) => {
      url = u
      return new Response(JSON.stringify({ items: [], total: 0, page: 2, page_size: 25 }), { status: 200 })
    })
    await client.listSeries({ q: 'the', monitored: true, page: 2, page_size: 25 })
    expect(url).toContain('/api/v1/tv?')
    expect(url).toContain('q=the')
    expect(url).toContain('monitored=true')
    expect(url).toContain('page=2')
    expect(url).toContain('page_size=25')
  })

  it('omits unset ListQuery fields', async () => {
    let url = ''
    mockFetch((u) => {
      url = u
      return new Response(JSON.stringify({ items: [], total: 0, page: 1, page_size: 25 }), { status: 200 })
    })
    await client.listMovies()
    expect(url).toBe('/api/v1/movies')
  })

  it('setMovieMonitored PUTs {monitored} to /movies/{id} (#41)', async () => {
    let method = ''
    let body: unknown = null
    mockFetch((_, init) => {
      method = init?.method ?? ''
      body = init?.body
      return new Response(JSON.stringify({ id: 7, monitored: false }), { status: 200 })
    })
    const res = await client.setMovieMonitored(7, false)
    expect(method).toBe('PUT')
    expect(JSON.parse(String(body))).toEqual({ monitored: false })
    expect(res.monitored).toBe(false)
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
