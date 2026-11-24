import { render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useNavigate } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { client } from '../api/client'
import type { ArtistSearchResult, BookLookupResult, MovieSearchHit, SearchResult } from '../api/types'
import { GlobalSearchPage } from './GlobalSearch'
import {
  GLOBAL_SEARCH_KIND_LABEL,
  mapBookHits,
  mapMovieHits,
  mapMusicHits,
  mapTvHits,
} from './globalSearchModel'

// Global search results page (issue #117): client-side fan-out over the four
// per-kind endpoints, ONE unified badged grid, per-kind isolation. The ONLY
// thing mocked is the API layer (the client); every assertion is on the
// real rendered output.
vi.mock('../api/client', () => {
  class ApiError extends Error {
    status: number
    constructor(status: number, message: string) {
      super(message)
      this.status = status
    }
  }
  return {
    ApiError,
    client: {
      searchTv: vi.fn(),
      searchMovies: vi.fn(),
      searchArtists: vi.fn(),
      lookupBooks: vi.fn(),
    },
  }
})

// ---- fixture builders (the real per-kind wire shapes) ----------------------

function tvHit(id: string, title: string, year = 2023): SearchResult {
  return { id, title, year, extra: { overview: 'A TV hit for the test.' } }
}

function movieHit(id: string, title: string, poster = ''): MovieSearchHit {
  return {
    id,
    title,
    year: 2021,
    poster,
    overview: 'A movie hit for the test.',
    certification: 'PG-13',
    runtime: 166,
    original_language: 'en',
    studio: 'Studio X',
    genres: ['Sci-Fi'],
    tmdb_pct: 88,
    imdb: '',
    links: { tmdb: `https://www.themoviedb.org/movie/${id}` },
  }
}

function artistResult(artists: { id: string; title: string; country?: string }[]): ArtistSearchResult {
  return {
    query: '',
    artists: artists.map((a) => ({
      id: a.id,
      title: a.title,
      type: 'Group',
      score: 90,
      country: a.country ?? '',
      disambiguation: '',
      genres: [],
      links: [],
      already_added: false,
    })),
    release_groups: [],
  }
}

function bookHit(kind: 'author' | 'book', name: string, author = '', year = 0): BookLookupResult {
  return { kind, name, author, year, added: false }
}

// ---- render helpers ---------------------------------------------------------

function renderPage(initial = '/search/all?q=dune') {
  return render(
    <MemoryRouter initialEntries={[initial]}>
      <Routes>
        <Route path="/search/all" element={<GlobalSearchPage />} />
      </Routes>
    </MemoryRouter>,
  )
}

// NavigateTo changes ?q= client-side to prove the page re-fans-out on query
// change (deep-link / back-forward handling).
function renderPageWithNav(initial: string) {
  function NavigateTo() {
    const navigate = useNavigate()
    return (
      <button
        type="button"
        data-testid="nav-to"
        onClick={() => navigate('/search/all?q=radiohead', { replace: true })}
      >
        nav
      </button>
    )
  }
  return render(
    <MemoryRouter initialEntries={[initial]}>
      <Routes>
        <Route
          path="/search/all"
          element={
            <>
              <GlobalSearchPage />
              <NavigateTo />
            </>
          }
        />
      </Routes>
    </MemoryRouter>,
  )
}

beforeEach(() => {
  vi.clearAllMocks()
})

// ---- route / query handling -------------------------------------------------

describe('GlobalSearchPage — route and query handling', () => {
  it('shows the idle state (no fan-out) when there is no query', () => {
    renderPage('/search/all')
    expect(screen.getByTestId('global-search-title')).toBeInTheDocument()
    expect(screen.getByTestId('global-search-idle')).toBeInTheDocument()
    expect(screen.queryByTestId('global-search-grid')).not.toBeInTheDocument()
    // No endpoint may fire without a query.
    expect(client.searchTv).not.toHaveBeenCalled()
    expect(client.searchMovies).not.toHaveBeenCalled()
    expect(client.searchArtists).not.toHaveBeenCalled()
    expect(client.lookupBooks).not.toHaveBeenCalled()
  })

  it('carries the URL query into the four endpoint calls', async () => {
    vi.mocked(client.searchTv).mockResolvedValue([])
    vi.mocked(client.searchMovies).mockResolvedValue([])
    vi.mocked(client.searchArtists).mockResolvedValue(artistResult([]))
    vi.mocked(client.lookupBooks).mockResolvedValue([])
    renderPage('/search/all?q=hello%20world')
    await screen.findByTestId('global-search-status')
    await vi.waitFor(() => {
      expect(client.searchTv).toHaveBeenCalledWith('hello world')
      expect(client.searchMovies).toHaveBeenCalledWith('hello world')
      expect(client.searchArtists).toHaveBeenCalledWith('hello world')
      expect(client.lookupBooks).toHaveBeenCalledWith('hello world')
    })
  })

  it('re-fans-out when the URL query changes', async () => {
    vi.mocked(client.searchTv).mockResolvedValue([tvHit('tvdb-1', 'One')])
    vi.mocked(client.searchMovies).mockResolvedValue([])
    vi.mocked(client.searchArtists).mockResolvedValue(artistResult([]))
    vi.mocked(client.lookupBooks).mockResolvedValue([])
    renderPageWithNav('/search/all?q=first')
    await vi.waitFor(() => expect(client.searchTv).toHaveBeenCalledWith('first'))

    // The first query's TV leg settles, then the user changes ?q=.
    await vi.waitFor(() =>
      expect(screen.getByTestId('global-search-status-tv')).toHaveTextContent('TV · 1'),
    )
    await screen.getByTestId('nav-to').click()
    await vi.waitFor(() => {
      expect(client.searchTv).toHaveBeenLastCalledWith('radiohead')
      expect(client.searchMovies).toHaveBeenLastCalledWith('radiohead')
      expect(client.searchArtists).toHaveBeenLastCalledWith('radiohead')
      expect(client.lookupBooks).toHaveBeenLastCalledWith('radiohead')
    })
  })
})

// ---- fan-out + merge + kind badges ------------------------------------------

describe('GlobalSearchPage — unified grid with kind badges', () => {
  it('fans out to all four kinds and renders one unified grid, each cell badged with its kind', async () => {
    vi.mocked(client.searchTv).mockResolvedValue([tvHit('tvdb-1', 'Dune Chronicles')])
    vi.mocked(client.searchMovies).mockResolvedValue([movieHit('603', 'Dune: Part Two')])
    vi.mocked(client.searchArtists).mockResolvedValue(artistResult([{ id: 'mb-1', title: 'Dune Radio' }]))
    vi.mocked(client.lookupBooks).mockResolvedValue([bookHit('book', 'Dune', 'Frank Herbert', 1965)])
    renderPage('/search/all?q=dune')

    const grid = await screen.findByTestId('global-search-grid')
    // One cell per hit: TV, Movies, Music — plus the Books cell (checked by
    // its own testid below; 'Dune' alone is a substring of the other titles).
    expect(grid).toHaveTextContent('Dune Chronicles')
    expect(grid).toHaveTextContent('Dune: Part Two')
    expect(grid).toHaveTextContent('Dune Radio')

    // Kind badges on each result (the #115 Badge primitive, per cell).
    expect(screen.getByTestId('global-search-card-tv-0')).toHaveTextContent('TV')
    expect(screen.getByTestId('global-search-card-movie-0')).toHaveTextContent('Movies')
    expect(screen.getByTestId('global-search-card-music-0')).toHaveTextContent('Music')
    const bookCard = screen.getByTestId('global-search-card-book-0')
    expect(bookCard).toHaveTextContent('Books')
    expect(bookCard).toHaveTextContent('Frank Herbert · 1965')

    // Per-kind status chips report the resolved counts.
    expect(screen.getByTestId('global-search-status-tv')).toHaveTextContent('TV · 1')
    expect(screen.getByTestId('global-search-status-movie')).toHaveTextContent('Movies · 1')
    expect(screen.getByTestId('global-search-status-music')).toHaveTextContent('Music · 1')
    expect(screen.getByTestId('global-search-status-book')).toHaveTextContent('Books · 1')
    expect(screen.getByTestId('global-search-query')).toHaveTextContent('Results for “dune”')
  })

  it('uses the real TMDB poster for Movies and the initials stand-in for art-less kinds', async () => {
    vi.mocked(client.searchTv).mockResolvedValue([])
    vi.mocked(client.searchMovies).mockResolvedValue([movieHit('603', 'Dune: Part Two', 'https://image.tmdb.org/t/t1.jpg')])
    vi.mocked(client.searchArtists).mockResolvedValue(artistResult([]))
    vi.mocked(client.lookupBooks).mockResolvedValue([bookHit('book', 'Dune', 'Frank Herbert', 1965)])
    renderPage('/search/all?q=dune')

    const movieCard = await screen.findByTestId('global-search-card-movie-0')
    // The only art in the grid is the Movies cell's real poster URL.
    const img = movieCard.querySelector('img')
    expect(img).not.toBeNull()
    expect(img?.getAttribute('src')).toBe('https://image.tmdb.org/t/t1.jpg')

    // The book hit has no poster in its wire shape: the initials stand-in
    // (posterInitials("Dune") = "D"), no invented art.
    const bookCard = screen.getByTestId('global-search-card-book-0')
    expect(bookCard.querySelector('img')).toBeNull()
    expect(bookCard.querySelector('span[aria-hidden]')?.textContent).toBe('D')
    expect(bookCard).toHaveTextContent('Frank Herbert · 1965')
  })

  it('shows the empty state when every kind resolves with zero hits', async () => {
    vi.mocked(client.searchTv).mockResolvedValue([])
    vi.mocked(client.searchMovies).mockResolvedValue([])
    vi.mocked(client.searchArtists).mockResolvedValue(artistResult([]))
    vi.mocked(client.lookupBooks).mockResolvedValue([])
    renderPage('/search/all?q=zzzznotfound')

    await expect(screen.findByTestId('global-search-empty')).resolves.toBeVisible()
    expect(screen.queryByTestId('global-search-grid')).not.toBeInTheDocument()
  })
})

// ---- per-kind isolation ------------------------------------------------------

describe('GlobalSearchPage — per-kind isolation', () => {
  it('renders an inline error note for the failed kind while the others still render', async () => {
    class ApiError extends Error {
      status = 502
      constructor(message: string) {
        super(message)
      }
    }
    // Music's provider is down; the other three are fine.
    vi.mocked(client.searchTv).mockResolvedValue([tvHit('tvdb-1', 'Still Here')])
    vi.mocked(client.searchMovies).mockResolvedValue([movieHit('603', 'Still A Movie')])
    vi.mocked(client.searchArtists).mockRejectedValue(new ApiError('musicbrainz upstream failed'))
    vi.mocked(client.lookupBooks).mockResolvedValue([bookHit('book', 'Still A Book', 'Someone', 2000)])

    renderPage('/search/all?q=still')

    // The inline error note appears (role=alert).
    const note = await screen.findByTestId('global-search-error-music')
    expect(note).toHaveTextContent('Music search failed: musicbrainz upstream failed')

    // The other three kinds' results still render in the unified grid.
    const grid = screen.getByTestId('global-search-grid')
    expect(grid).toHaveTextContent('Still Here')
    expect(grid).toHaveTextContent('Still A Movie')
    expect(grid).toHaveTextContent('Still A Book')
    // No Music cells, and its status chip reports the failure.
    expect(screen.queryAllByTestId(/^global-search-card-music-/)).toHaveLength(0)
    expect(screen.getByTestId('global-search-status-music')).toHaveTextContent('Music · failed')
    expect(screen.getByTestId('global-search-status-tv')).toHaveTextContent('TV · 1')
  })

  it('a slow kind does not block the others (per-kind loading)', async () => {
    let resolveBooks!: (rows: BookLookupResult[]) => void
    const pendingBooks = new Promise<BookLookupResult[]>((r) => {
      resolveBooks = r
    })
    vi.mocked(client.searchTv).mockResolvedValue([])
    vi.mocked(client.searchMovies).mockResolvedValue([])
    vi.mocked(client.searchArtists).mockResolvedValue(artistResult([{ id: 'mb-1', title: 'Quick Artist' }]))
    vi.mocked(client.lookupBooks).mockReturnValue(pendingBooks)

    renderPage('/search/all?q=quick')

    // Music resolves first: its cell renders even though Books is pending.
    const card = await screen.findByTestId('global-search-card-music-0')
    expect(card).toHaveTextContent('Quick Artist')
    expect(screen.getByTestId('global-search-status-music')).toHaveTextContent('Music · 1')
    // Books is still loading: its chip says so and it has no cells yet.
    expect(screen.getByTestId('global-search-status-book')).toHaveTextContent('Books · searching…')
    expect(screen.queryAllByTestId(/^global-search-card-book-/)).toHaveLength(0)

    // Resolve the slow leg: its cell appears and the chip shows the count.
    resolveBooks([bookHit('author', 'Quick Author')])
    await vi.waitFor(() =>
      expect(screen.getByTestId('global-search-status-book')).toHaveTextContent('Books · 1'),
    )
    expect(screen.getByTestId('global-search-card-book-0')).toHaveTextContent('Quick Author')
  })
})

// ---- pure field mapping -------------------------------------------------------

describe('per-kind field mapping (pure)', () => {
  it('movies: meta from year · certification · runtime; poster only when present', () => {
    const hits = mapMovieHits([movieHit('1', 'With Poster', 'https://x/p.jpg'), movieHit('2', 'No Poster')])
    expect(hits).toHaveLength(2)
    expect(hits[0]).toEqual({
      kind: 'movie',
      title: 'With Poster',
      meta: '2021 · PG-13 · 166m',
      image: 'https://x/p.jpg',
    })
    expect(hits[1].image).toBeUndefined()
  })

  it('tv: title + year, no invented art', () => {
    const hits = mapTvHits([tvHit('t1', 'The Show', 1999), tvHit('t2', 'No Year', 0)])
    expect(hits).toEqual([
      { kind: 'tv', title: 'The Show', meta: '1999' },
      { kind: 'tv', title: 'No Year', meta: '' },
    ])
  })

  it('music: maps the artists list (release groups are add-flow data, not kind results)', () => {
    const hits = mapMusicHits(artistResult([{ id: 'a1', title: 'Radiohead', country: 'GB' }, { id: 'a2', title: 'Nameless' }]))
    expect(hits).toEqual([
      { kind: 'music', title: 'Radiohead', meta: 'GB' },
      { kind: 'music', title: 'Nameless', meta: '' },
    ])
  })

  it('books: book hits carry author · year, author hits are labeled Author', () => {
    const hits = mapBookHits([
      bookHit('book', 'Dune', 'Frank Herbert', 1965),
      bookHit('book', 'Yearless', 'A. Person', 0),
      bookHit('author', 'Frank Herbert'),
    ])
    expect(hits).toEqual([
      { kind: 'book', title: 'Dune', meta: 'Frank Herbert · 1965' },
      { kind: 'book', title: 'Yearless', meta: 'A. Person' },
      { kind: 'book', title: 'Frank Herbert', meta: 'Author' },
    ])
  })

  it('kind labels are the four human kind names', () => {
    expect(GLOBAL_SEARCH_KIND_LABEL).toEqual({
      tv: 'TV',
      movie: 'Movies',
      music: 'Music',
      book: 'Books',
    })
  })
})
