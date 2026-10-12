import { afterEach, describe, expect, it, vi } from 'vitest'
import { client, ApiError } from './client'

// Client tests for the add-flow endpoints (#42): provider search URL
// encoding, the profiles lookup, and the new provider-id fields on the add
// requests. The request pipeline itself is covered in client.test.ts.
function mockFetch(handler: (url: string, init?: RequestInit) => Response) {
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL, init?: RequestInit) =>
      Promise.resolve(handler(String(input), init)),
    ),
  )
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('client add-flow search (#42)', () => {
  it('searchMovies URL-encodes the query term', async () => {
    let url = ''
    mockFetch((u) => {
      url = u
      return new Response(JSON.stringify([{ id: '603', title: 'Dune: Part Two', year: 2024, extra: {} }]), { status: 200 })
    })
    const hits = await client.searchMovies('dune part two')
    expect(url).toBe('/api/v1/search/movies?query=dune%20part%20two')
    expect(hits).toHaveLength(1)
    expect(hits[0].id).toBe('603')
  })

  it('searchBookWorks sends both query and author', async () => {
    let url = ''
    mockFetch((u) => {
      url = u
      return new Response(JSON.stringify([]), { status: 200 })
    })
    await client.searchBookWorks('left hand', 'Le Guin')
    expect(url).toContain('/api/v1/search/books/works?')
    expect(url).toContain('query=left+hand')
    expect(url).toContain('author=Le+Guin')
  })

  it('searchBookWorks omits author when empty', async () => {
    let url = ''
    mockFetch((u) => {
      url = u
      return new Response(JSON.stringify([]), { status: 200 })
    })
    await client.searchBookWorks('dune', '')
    expect(url).toBe('/api/v1/search/books/works?query=dune')
  })

  it('maps a 503 (provider not configured) to an ApiError the UI can branch on', async () => {
    mockFetch(() =>
      new Response(JSON.stringify({ error: 'tmdb is not configured (missing API key)' }), { status: 503 }),
    )
    await expect(client.searchMovies('dune')).rejects.toSatisfy(
      (e: unknown) => e instanceof ApiError && e.status === 503,
    )
  })
})

describe('client profiles + add requests (#42)', () => {
  it('getProfiles returns the kind profiles + default', async () => {
    let url = ''
    mockFetch((u) => {
      url = u
      return new Response(JSON.stringify({ profiles: ['HD-1080p', 'UHD-2160p'], default: 'HD-1080p' }), { status: 200 })
    })
    const p = await client.getProfiles('movies')
    expect(url).toBe('/api/v1/profiles?kind=movies')
    expect(p.default).toBe('HD-1080p')
  })

  it('addSeries posts the provider ids for the selected source', async () => {
    let body: unknown = null
    mockFetch((_, init) => {
      body = JSON.parse(String(init?.body))
      return new Response(JSON.stringify({ id: 9 }), { status: 201 })
    })
    await client.addSeries({
      title: 'Breaking Bad',
      year: '2008',
      quality_profile: 'HD-1080p',
      monitored: true,
      tvdb_id: '82350',
    })
    expect(body).toEqual({
      title: 'Breaking Bad',
      year: '2008',
      quality_profile: 'HD-1080p',
      monitored: true,
      tvdb_id: '82350',
    })
  })

  it('addArtist posts the mbid of the selected result', async () => {
    let body: unknown = null
    mockFetch((_, init) => {
      body = JSON.parse(String(init?.body))
      return new Response(JSON.stringify({ id: 4 }), { status: 201 })
    })
    await client.addArtist({ name: 'Radiohead', mbid: 'a74b1cee-3cc7-4c58-9ac8-75290173c7f4', monitored: true })
    expect(body).toEqual({ name: 'Radiohead', mbid: 'a74b1cee-3cc7-4c58-9ac8-75290173c7f4', monitored: true })
  })
})
