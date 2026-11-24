import { render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { userEvent } from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { client } from '../api/client'
import type {
  Artist,
  Author,
  Movie,
  PageArtist,
  PageAuthor,
  PageMovie,
  PageSeries,
  Series,
} from '../api/types'
import {
  LibraryPage,
  LIBRARY_KIND_LABEL,
  detailPathFor,
  filterByGenre,
  genreOptions,
  itemMeta,
  toLibraryItems,
  type LibraryItem,
} from './Library'

// The unified cross-kind Library grid (issue #119): client-side fan-out over
// the four per-kind list endpoints, a grid grouped into kind sections, the
// three filters (kind URL-synced, genre client-side, status server-side), and
// per-kind failure isolation. The ONLY thing mocked is the API client.
vi.mock('../api/client', () => ({
  client: {
    listMovies: vi.fn(),
    listSeries: vi.fn(),
    listArtists: vi.fn(),
    listAuthors: vi.fn(),
  },
}))

// ---- fixture builders (the real per-kind wire shapes) ----------------------

const T = '2026-01-01T00:00:00Z'

function movie(
  id: number,
  title: string,
  opts: { year?: number; genres?: string[]; monitored?: boolean } = {},
): Movie {
  return {
    ID: id,
    Title: title,
    Year: opts.year ?? 0,
    QualityProfile: '4K',
    Monitored: opts.monitored ?? true,
    AddedAt: T,
    Genres: opts.genres?.length ? opts.genres : undefined,
  }
}

function series(
  id: number,
  title: string,
  opts: { year?: number; quality?: string; monitored?: boolean } = {},
): Series {
  return {
    ID: id,
    TVDBID: 0,
    TMDBID: 0,
    Title: title,
    Year: opts.year ?? 0,
    QualityProfile: opts.quality ?? '',
    Monitored: opts.monitored ?? true,
    SeriesType: 'standard',
    SeasonFolder: false,
    Tags: [],
    AddedAt: T,
  }
}

function artist(
  id: number,
  name: string,
  opts: { genre?: string; rating?: number; cover?: string; monitored?: boolean } = {},
): Artist {
  return {
    ID: id,
    MBID: `mb-${id}`,
    Name: name,
    Monitored: opts.monitored ?? true,
    AddedAt: T,
    Genre: opts.genre,
    Rating: opts.rating,
    Cover: opts.cover,
  }
}

function author(id: number, name: string, opts: { monitored?: boolean } = {}): Author {
  return {
    ID: id,
    Name: name,
    Monitored: opts.monitored ?? true,
    AddedAt: T,
  }
}

function moviePage(items: Movie[], total = items.length): PageMovie {
  return { items, total, page: 1, page_size: 25 }
}
function seriesPage(items: Series[], total = items.length): PageSeries {
  return { items, total, page: 1, page_size: 25 }
}
function artistPage(items: Artist[], total = items.length): PageArtist {
  return { items, total, page: 1, page_size: 25 }
}
function authorPage(items: Author[], total = items.length): PageAuthor {
  return { items, total, page: 1, page_size: 25 }
}

// A class shaped like the client's ApiError (a rejected leg carries a message).
class ApiError extends Error {
  status = 502
}

// ---- render helpers ---------------------------------------------------------

// UrlProbe exposes the current location so ?kind= URL-sync (both directions)
// is assertable in the test.
function renderLibrary(initial = '/library') {
  function UrlProbe() {
    const loc = useLocation()
    return <span data-testid="current-url">{loc.pathname + loc.search}</span>
  }
  return render(
    <MemoryRouter initialEntries={[initial]}>
      <Routes>
        <Route
          path="/library"
          element={
            <>
              <LibraryPage />
              <UrlProbe />
            </>
          }
        />
      </Routes>
    </MemoryRouter>,
  )
}

// Stub all four legs (the "All" view's default fan-out) with empty pages.
function stubAll() {
  vi.mocked(client.listMovies).mockResolvedValue(moviePage([]))
  vi.mocked(client.listSeries).mockResolvedValue(seriesPage([]))
  vi.mocked(client.listArtists).mockResolvedValue(artistPage([]))
  vi.mocked(client.listAuthors).mockResolvedValue(authorPage([]))
}

beforeEach(() => {
  vi.clearAllMocks()
  stubAll()
})

// ---- 1. grouped grid from four fanned-out lists ----------------------------

describe('Library — grouped grid from four fanned-out lists', () => {
  it('fans out to all four kinds and renders one grid grouped into kind sections, in kind order, each headed with label + total', async () => {
    vi.mocked(client.listMovies).mockResolvedValue(moviePage([movie(1, 'Dune', { year: 2021, genres: ['Sci-Fi'] })]))
    vi.mocked(client.listSeries).mockResolvedValue(seriesPage([series(7, 'Dune: The Show', { year: 2021 })]))
    vi.mocked(client.listArtists).mockResolvedValue(artistPage([artist(3, 'Dune Radio')]))
    vi.mocked(client.listAuthors).mockResolvedValue(authorPage([author(9, 'Frank Herbert')]))
    renderLibrary('/library')

    // Every leg fired (page 1, page size 25, no monitored → default status).
    await vi.waitFor(() => {
      expect(client.listMovies).toHaveBeenCalledWith({ page: 1, page_size: 25 })
      expect(client.listSeries).toHaveBeenCalledWith({ page: 1, page_size: 25 })
      expect(client.listArtists).toHaveBeenCalledWith({ page: 1, page_size: 25 })
      expect(client.listAuthors).toHaveBeenCalledWith({ page: 1, page_size: 25 })
    })

    // Wait for the fan-out to settle, then check the four sections, in the
    // deterministic kind order, each headed label + total.
    await screen.findByTestId('library-section-movie')
    const sections = screen.getAllByTestId(/^library-section-(movie|tv|music|book)$/)
    const titles = sections.map((s) => s.querySelector('h3')!.textContent)
    expect(titles).toEqual(['Movies (1)', 'TV (1)', 'Music (1)', 'Books (1)'])

    // Each cell is a kind-badged PosterCard.
    expect(screen.getByTestId('library-card-movie-1')).toHaveTextContent('Movies')
    expect(screen.getByTestId('library-card-tv-7')).toHaveTextContent('TV')
    expect(screen.getByTestId('library-card-music-3')).toHaveTextContent('Music')
    expect(screen.getByTestId('library-card-book-9')).toHaveTextContent('Books')
  })

  it('shows a dim "No <kind> yet." line for an empty kind section (the others still group)', async () => {
    vi.mocked(client.listMovies).mockResolvedValue(moviePage([movie(1, 'Solo Movie', { year: 2000 })]))
    renderLibrary('/library')
    await screen.findByTestId('library-grid-movie')
    expect(screen.getByTestId('library-empty-tv')).toHaveTextContent('No TV yet.')
    expect(screen.getByTestId('library-empty-music')).toHaveTextContent('No Music yet.')
    expect(screen.getByTestId('library-empty-book')).toHaveTextContent('No Books yet.')
    // Only the Movies section has a grid.
    expect(screen.queryByTestId('library-grid-tv')).not.toBeInTheDocument()
  })
})

// ---- 2. ?kind= preselect + URL sync (both directions) ----------------------

describe('Library — ?kind= URL-synced both directions', () => {
  it('preselects the single-kind view from ?kind= (only that endpoint hits)', async () => {
    vi.mocked(client.listMovies).mockResolvedValue(moviePage([movie(1, 'Only A Movie', { year: 2010 })]))
    renderLibrary('/library?kind=movie')

    // The single-kind view hits only that kind's endpoint.
    await vi.waitFor(() => expect(client.listMovies).toHaveBeenCalledWith({ page: 1, page_size: 25 }))
    expect(client.listSeries).not.toHaveBeenCalled()
    expect(client.listArtists).not.toHaveBeenCalled()
    expect(client.listAuthors).not.toHaveBeenCalled()

    // Only the Movies section renders; the URL carries ?kind=movie.
    await screen.findByTestId('library-section-movie')
    expect(screen.queryByTestId('library-section-tv')).not.toBeInTheDocument()
    expect(screen.getByTestId('current-url')).toHaveTextContent('/library?kind=movie')
  })

  it('updates the URL when the kind filter changes, and clears it for All', async () => {
    const user = userEvent.setup()
    renderLibrary('/library')
    await screen.findByTestId('library-body')

    // All → Movies: the URL gains ?kind=movie and the view narrows to Movies.
    await user.click(screen.getByTestId('library-kind-btn'))
    await user.click(screen.getByTestId('library-kind-opt-movie'))
    await vi.waitFor(() =>
      expect(screen.getByTestId('current-url')).toHaveTextContent('/library?kind=movie'),
    )
    expect(await screen.findByTestId('library-section-movie')).toBeInTheDocument()
    expect(screen.queryByTestId('library-section-tv')).not.toBeInTheDocument()

    // Movies → All: the param is removed and all four sections come back.
    await user.click(screen.getByTestId('library-kind-btn'))
    await user.click(screen.getByTestId('library-kind-opt-all'))
    await vi.waitFor(() => expect(screen.getByTestId('current-url')).toHaveTextContent('/library'))
    await screen.findByTestId('library-section-tv')
    await screen.findByTestId('library-section-book')
  })
})

// ---- 3. genre filter (client-side; TV/Books vanish) ------------------------

describe('Library — genre filter', () => {
  it('offers the sorted union of loaded genres and narrows Movies/Music while hiding TV/Books', async () => {
    const user = userEvent.setup()
    vi.mocked(client.listMovies).mockResolvedValue(
      moviePage([movie(1, 'SciFi Movie', { genres: ['Sci-Fi'] }), movie(2, 'Drama Movie', { genres: ['Drama'] })]),
    )
    vi.mocked(client.listSeries).mockResolvedValue(seriesPage([series(7, 'A Show', { year: 1999 })]))
    vi.mocked(client.listArtists).mockResolvedValue(artistPage([artist(3, 'Rock Artist', { genre: 'Rock' })]))
    vi.mocked(client.listAuthors).mockResolvedValue(authorPage([author(9, 'An Author')]))
    renderLibrary('/library')
    await screen.findByTestId('library-grid-movie')

    // Open the Genre menu: All genres + the sorted union of loaded genres.
    await user.click(screen.getByTestId('library-genre-btn'))
    expect(screen.getByTestId('library-genre-opt-all')).toBeInTheDocument()
    expect(screen.getByTestId('library-genre-opt-Rock')).toBeInTheDocument()
    expect(screen.getByTestId('library-genre-opt-Drama')).toBeInTheDocument()
    expect(screen.getByTestId('library-genre-opt-Sci-Fi')).toBeInTheDocument()

    // Select Sci-Fi: the matching movie stays; the Drama movie, the (no-genre)
    // TV/Books, and the Rock artist (wrong genre) all vanish.
    await user.click(screen.getByTestId('library-genre-opt-Sci-Fi'))
    await vi.waitFor(() =>
      expect(screen.getByTestId('library-card-movie-1')).toBeInTheDocument(),
    )
    expect(screen.queryByTestId('library-card-movie-2')).not.toBeInTheDocument()
    expect(screen.queryByTestId('library-section-tv')).not.toBeInTheDocument()
    expect(screen.queryByTestId('library-section-music')).not.toBeInTheDocument()
    expect(screen.queryByTestId('library-section-book')).not.toBeInTheDocument()
  })

  it('shows only "All genres" when no loaded item carries a genre', async () => {
    const user = userEvent.setup()
    vi.mocked(client.listMovies).mockResolvedValue(moviePage([movie(1, 'Plain Movie', { year: 2000 })]))
    vi.mocked(client.listSeries).mockResolvedValue(seriesPage([series(7, 'A Show', { year: 1999 })]))
    vi.mocked(client.listArtists).mockResolvedValue(artistPage([artist(3, 'Nameless Artist')]))
    vi.mocked(client.listAuthors).mockResolvedValue(authorPage([author(9, 'An Author')]))
    renderLibrary('/library')
    await screen.findByTestId('library-grid-movie')

    await user.click(screen.getByTestId('library-genre-btn'))
    expect(screen.getByTestId('library-genre-opt-all')).toBeInTheDocument()
    // No genre options beyond All: the genre menu has exactly one item.
    expect(screen.getAllByRole('menuitem')).toHaveLength(1)
  })
})

// ---- 4. status filter (server-side monitored) ------------------------------

describe('Library — status filter passes monitored on the wire', () => {
  it('sends monitored true/false for Monitored/Unmonitored on every kind', async () => {
    const user = userEvent.setup()
    renderLibrary('/library')
    // Default (All): no monitored key on the wire.
    await vi.waitFor(() =>
      expect(client.listMovies).toHaveBeenCalledWith({ page: 1, page_size: 25 }),
    )

    await user.click(screen.getByTestId('library-status-btn'))
    await user.click(screen.getByTestId('library-status-opt-monitored'))
    await vi.waitFor(() =>
      expect(client.listMovies).toHaveBeenLastCalledWith({ page: 1, page_size: 25, monitored: true }),
    )
    await vi.waitFor(() =>
      expect(client.listSeries).toHaveBeenLastCalledWith({ page: 1, page_size: 25, monitored: true }),
    )
    await vi.waitFor(() =>
      expect(client.listArtists).toHaveBeenLastCalledWith({ page: 1, page_size: 25, monitored: true }),
    )
    await vi.waitFor(() =>
      expect(client.listAuthors).toHaveBeenLastCalledWith({ page: 1, page_size: 25, monitored: true }),
    )

    await user.click(screen.getByTestId('library-status-btn'))
    await user.click(screen.getByTestId('library-status-opt-unmonitored'))
    await vi.waitFor(() =>
      expect(client.listMovies).toHaveBeenLastCalledWith({ page: 1, page_size: 25, monitored: false }),
    )
  })
})

// ---- 5. per-kind failure isolation -----------------------------------------

describe('Library — per-kind failure isolation', () => {
  it('renders a dim per-section note for the failed kind while the others still render', async () => {
    vi.mocked(client.listMovies).mockResolvedValue(moviePage([movie(1, 'Still A Movie', { year: 2000 })]))
    vi.mocked(client.listSeries).mockResolvedValue(seriesPage([series(7, 'Still A Show', { year: 1999 })]))
    vi.mocked(client.listArtists).mockRejectedValue(new ApiError('musicbrainz upstream failed'))
    vi.mocked(client.listAuthors).mockResolvedValue(authorPage([author(9, 'Still An Author')]))
    renderLibrary('/library')

    // The failed kind's inline note (role=alert).
    const note = await screen.findByTestId('library-error-music')
    expect(note).toHaveTextContent('Music failed to load: musicbrainz upstream failed')

    // No page-level error (not all legs failed), and the other kinds render.
    expect(screen.queryByTestId('library-error')).not.toBeInTheDocument()
    expect(screen.getByTestId('library-grid-movie')).toHaveTextContent('Still A Movie')
    expect(screen.getByTestId('library-grid-tv')).toHaveTextContent('Still A Show')
    expect(screen.getByTestId('library-grid-book')).toHaveTextContent('Still An Author')
    expect(screen.queryByTestId('library-grid-music')).not.toBeInTheDocument()
  })

  it('a total failure (all four reject) is a page-level error with Retry', async () => {
    vi.mocked(client.listMovies).mockRejectedValue(new ApiError('radarr down'))
    vi.mocked(client.listSeries).mockRejectedValue(new ApiError('sonarr down'))
    vi.mocked(client.listArtists).mockRejectedValue(new ApiError('lidarr down'))
    vi.mocked(client.listAuthors).mockRejectedValue(new ApiError('readarr down'))
    renderLibrary('/library')

    const err = await screen.findByTestId('library-error-message')
    expect(err).toBeInTheDocument()
    expect(screen.queryByTestId(/^library-section-(movie|tv|music|book)$/)).not.toBeInTheDocument()
    expect(screen.getByTestId('library-retry')).toBeInTheDocument()
  })
})

// ---- 6. single-kind pagination ---------------------------------------------

describe('Library — single-kind pagination', () => {
  it('pages the single kind and passes the page on the wire', async () => {
    const user = userEvent.setup()
    const mk = (p: number) => {
      const items = Array.from({ length: 25 }, (_, i) => movie((p - 1) * 25 + i + 1, `Movie ${p}-${i}`))
      return moviePage(items, 60)
    }
    vi.mocked(client.listMovies).mockImplementation((q) =>
      Promise.resolve(mk((q?.page ?? 1) as number)),
    )
    renderLibrary('/library?kind=movie')

    // 60 items / 25 per page = 3 pages.
    await screen.findByTestId('library-grid-movie')
    expect(screen.getByTestId('pagination')).toHaveTextContent('page 1 of 3')
    expect(client.listMovies).toHaveBeenCalledWith({ page: 1, page_size: 25 })

    await user.click(screen.getByTestId('page-next'))
    await vi.waitFor(() =>
      expect(client.listMovies).toHaveBeenLastCalledWith({ page: 2, page_size: 25 }),
    )
    await vi.waitFor(() =>
      expect(screen.getByTestId('pagination')).toHaveTextContent('page 2 of 3'),
    )
    // A page-2 item is now in the grid.
    expect(screen.getByTestId('library-card-movie-26')).toBeInTheDocument()
  })
})

// ---- pure model -------------------------------------------------------------

describe('Library — pure model', () => {
  const libItem = (p: Partial<LibraryItem> & { kind: LibraryItem['kind'] }): LibraryItem => ({
    id: 1,
    title: 'T',
    image: null,
    monitored: true,
    year: null,
    quality: '',
    rating: null,
    genres: [],
    ...p,
  })

  it('itemMeta composes per kind with no stray separators', () => {
    expect(itemMeta(libItem({ kind: 'tv', year: 2021, quality: 'HD' }))).toBe('2021 · HD')
    expect(itemMeta(libItem({ kind: 'tv', year: 0, quality: '' }))).toBe('')
    expect(itemMeta(libItem({ kind: 'movie', year: 2024 }))).toBe('2024')
    expect(itemMeta(libItem({ kind: 'music', rating: 8.5 }))).toBe('8.5')
    expect(itemMeta(libItem({ kind: 'music', rating: null, genres: ['Rock'] }))).toBe('Rock')
    expect(itemMeta(libItem({ kind: 'book' }))).toBe('')
  })

  it('genreOptions is the sorted union; filterByGenre hides items without it', () => {
    const items = [
      libItem({ kind: 'movie', genres: ['Sci-Fi', 'Drama'] }),
      libItem({ kind: 'music', genres: ['Rock'] }),
      libItem({ kind: 'tv' }),
      libItem({ kind: 'book' }),
    ]
    expect(genreOptions(items)).toEqual(['Drama', 'Rock', 'Sci-Fi'])
    expect(filterByGenre(items, 'all')).toHaveLength(4)
    expect(filterByGenre(items, 'Sci-Fi')).toHaveLength(1)
    // TV/Books have no genres: they never match a specific genre.
    expect(filterByGenre(items, 'Rock')).toHaveLength(1)
    expect(filterByGenre([libItem({ kind: 'tv' })], 'Drama')).toHaveLength(0)
  })

  it('detailPathFor routes each kind to its detail page (Books → author detail)', () => {
    expect(detailPathFor('movie', 1)).toBe('/movies/1')
    expect(detailPathFor('tv', 2)).toBe('/tv/2')
    expect(detailPathFor('music', 3)).toBe('/music/3')
    expect(detailPathFor('book', 4)).toBe('/books/4')
  })

  it('toLibraryItems maps each wire shape (genres/cover/rating only where present)', () => {
    const m = toLibraryItems('movie', moviePage([movie(1, 'M', { year: 2020, genres: ['Sci-Fi'], monitored: false })]))
    expect(m[0]).toMatchObject({ id: 1, kind: 'movie', title: 'M', year: 2020, genres: ['Sci-Fi'], monitored: false, image: null })

    const s = toLibraryItems('tv', seriesPage([series(2, 'S', { year: 1999, quality: 'HD' })]))
    expect(s[0]).toMatchObject({ id: 2, kind: 'tv', title: 'S', year: 1999, quality: 'HD', genres: [] })

    const a = toLibraryItems('music', artistPage([artist(3, 'A', { genre: 'Rock', rating: 7, cover: 'https://x/c.jpg' })]))
    expect(a[0]).toMatchObject({ id: 3, kind: 'music', title: 'A', genres: ['Rock'], rating: 7, image: 'https://x/c.jpg' })

    const b = toLibraryItems('book', authorPage([author(4, 'B')]))
    expect(b[0]).toMatchObject({ id: 4, kind: 'book', title: 'B', genres: [], image: null })
  })

  it('kind labels are the four human kind names', () => {
    expect(LIBRARY_KIND_LABEL).toEqual({ movie: 'Movies', tv: 'TV', music: 'Music', book: 'Books' })
  })
})
