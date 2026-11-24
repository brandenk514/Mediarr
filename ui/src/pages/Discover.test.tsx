import { render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { client } from '../api/client'
import type { DiscoverCellError, DiscoverItem, DiscoverPage } from '../api/types'
import {
  DISCOVER_KINDS,
  DISCOVER_ROWS,
  cellErrorsForRow,
  dateLabel,
  DiscoverPage as DiscoverPageComponent,
  groupRowItems,
  itemMeta,
  targetFor,
  type DiscoverKind,
  type DiscoverRow,
} from './Discover'

// Unified Discover page (issue #118, B slice): three cross-kind feed rows
// over the finished GET /api/v1/discover contract. The ONLY thing mocked is
// the API layer (the client); every assertion is on the real rendered
// output (the GlobalSearch.test pattern).
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
      discover: vi.fn(),
    },
  }
})

// ---- fixture builders (the real wire shapes) --------------------------------

type ItemSpec = Pick<DiscoverItem, 'kind' | 'sourceId' | 'title'> & Partial<DiscoverItem>

function item(spec: ItemSpec): DiscoverItem {
  return { year: 0, subtitle: '', posterUrl: '', date: '', ...spec }
}

function cellError(kind: DiscoverKind, row: 'trending' | 'upcoming' | 'new_releases', reason: string): DiscoverCellError {
  return { cell: `${kind}:${row}`, reason }
}

function page(partial: Partial<DiscoverPage> = {}): DiscoverPage {
  return { trending: [], upcoming: [], newReleases: [], unavailable: [], ...partial }
}

// ---- render helpers ----------------------------------------------------------

// Sentinel: stands in for the detail / add routes the cards may navigate to.
// It echoes the landed URL (pathname + search), so a click proves BOTH the
// route and the id/params that reached it.
function Sentinel() {
  const location = useLocation()
  return <div data-testid="sentinel">{location.pathname}{location.search}</div>
}

function renderDiscover() {
  return render(
    <MemoryRouter initialEntries={['/discover']}>
      <Routes>
        <Route path="/discover" element={<DiscoverPageComponent />} />
        <Route path="/movies/:id" element={<Sentinel />} />
        <Route path="/tv/:id" element={<Sentinel />} />
        <Route path="/music/:id" element={<Sentinel />} />
        <Route path="/movies/add" element={<Sentinel />} />
      </Routes>
    </MemoryRouter>,
  )
}

beforeEach(() => {
  vi.clearAllMocks()
})

// ---- rows + kind badges -------------------------------------------------------

describe('DiscoverPage — rows and kind badges', () => {
  it('renders all three rows from the mocked client, each card badged with its kind', async () => {
    vi.mocked(client.discover).mockResolvedValue(
      page({
        trending: [
          item({ kind: 'movie', sourceId: '603', title: 'Dune: Part Two', year: 2024 }),
          item({ kind: 'tv', sourceId: '81858', title: 'Severance', year: 2025, subtitle: 'S2' }),
          item({ kind: 'music', sourceId: 'mb-1', title: 'Radiohead', subtitle: 'GB' }),
          item({ kind: 'book', sourceId: 'ol-1', title: 'The Long Walk', year: 2025 }),
        ],
        upcoming: [item({ kind: 'tv', sourceId: '77', title: 'Monitored Show', subtitle: 'S1E1' })],
        newReleases: [item({ kind: 'music', sourceId: 'mb-2', title: 'Fresh Release' })],
      }),
    )
    renderDiscover()

    // All three rows render, in order.
    await screen.findByTestId('discover-row-title-trending')
    expect(screen.getByTestId('discover-row-title-upcoming')).toHaveTextContent('Upcoming')
    expect(screen.getByTestId('discover-row-title-newReleases')).toHaveTextContent('New Releases')
    expect(client.discover).toHaveBeenCalledTimes(1)

    // Each card carries its kind badge (the #115 Badge over the art).
    expect(screen.getByTestId('discover-card-trending-movie-603')).toHaveTextContent('Movies')
    expect(screen.getByTestId('discover-card-trending-tv-81858')).toHaveTextContent('TV')
    expect(screen.getByTestId('discover-card-trending-music-mb-1')).toHaveTextContent('Music')
    expect(screen.getByTestId('discover-card-trending-book-ol-1')).toHaveTextContent('Books')

    // The kind label is the badge, not a guess from the title.
    const tvCard = screen.getByTestId('discover-card-trending-tv-81858')
    expect(tvCard).toHaveTextContent('Severance')
    expect(tvCard).toHaveTextContent('S2')
  })

  it('groups a row by kind in the deterministic order movie, tv, music, book (defensive against a shuffled wire order)', async () => {
    // Deliberately shuffled: book, music, tv, movie.
    vi.mocked(client.discover).mockResolvedValue(
      page({
        trending: [
          item({ kind: 'book', sourceId: 'b1', title: 'A Book' }),
          item({ kind: 'music', sourceId: 'a1', title: 'An Artist' }),
          item({ kind: 'tv', sourceId: 't1', title: 'A Show' }),
          item({ kind: 'movie', sourceId: 'm1', title: 'A Movie' }),
        ],
      }),
    )
    renderDiscover()
    const grid = await screen.findByTestId('discover-row-grid-trending')
    expect(Array.from(grid.children).map((c) => c.getAttribute('data-testid'))).toEqual([
      'discover-card-trending-movie-m1',
      'discover-card-trending-tv-t1',
      'discover-card-trending-music-a1',
      'discover-card-trending-book-b1',
    ])
  })
})

// ---- unavailable cells (per-cell grey-out) ------------------------------------

describe('DiscoverPage — unavailable cells (per-cell grey-out, R3 pattern)', () => {
  it('renders a grey-out note per unavailable cell naming the feed, while the row still renders its items', async () => {
    vi.mocked(client.discover).mockResolvedValue(
      page({
        trending: [item({ kind: 'tv', sourceId: 't1', title: 'Still Trending' })],
        unavailable: [
          cellError('movie', 'trending', 'tmdb is not configured (missing API key)'),
          cellError('music', 'trending', 'musicbrainz is not configured'),
        ],
      }),
    )
    renderDiscover()
    await screen.findByTestId('discover-unavailable-movie-trending')

    // The R3 keyless wording is surfaced verbatim, feed named.
    expect(screen.getByTestId('discover-unavailable-movie-trending')).toHaveTextContent(
      'Movies: tmdb is not configured (missing API key)',
    )
    expect(screen.getByTestId('discover-unavailable-music-trending')).toHaveTextContent(
      'Music: musicbrainz is not configured',
    )

    // The row's items still render (an unavailable entry never hides a row).
    expect(screen.getByTestId('discover-card-trending-tv-t1')).toBeInTheDocument()
    // And no page-level error: per-cell problems stay notes.
    expect(screen.queryByTestId('discover-error')).not.toBeInTheDocument()
  })

  it('A2: a cell in BOTH a row and unavailable renders the items AND the note (keyless Google Books fallback)', async () => {
    vi.mocked(client.discover).mockResolvedValue(
      page({
        newReleases: [item({ kind: 'book', sourceId: 'ol-9', title: 'OpenLibrary Fallback', year: 2026 })],
        unavailable: [cellError('book', 'new_releases', 'google books is not configured (missing API key)')],
      }),
    )
    renderDiscover()
    // The row renders its (fallback) items…
    const card = await screen.findByTestId('discover-card-newReleases-book-ol-9')
    expect(card).toHaveTextContent('OpenLibrary Fallback')
    // …and the missing key is named in the row's grey-out note.
    expect(screen.getByTestId('discover-unavailable-book-new_releases')).toHaveTextContent(
      'Books: google books is not configured (missing API key)',
    )
  })

  it('notes only appear under their own row (no cross-row bleed)', async () => {
    vi.mocked(client.discover).mockResolvedValue(
      page({
        trending: [item({ kind: 'movie', sourceId: 'm1', title: 'Trending Movie' })],
        newReleases: [item({ kind: 'tv', sourceId: 't1', title: 'New TV' })],
        unavailable: [cellError('movie', 'trending', 'tmdb is not configured (missing API key)')],
      }),
    )
    renderDiscover()
    await screen.findByTestId('discover-unavailable-movie-trending')
    // Exactly one note for the cell, and it lives in the trending row.
    expect(screen.getAllByTestId('discover-unavailable-movie-trending')).toHaveLength(1)
    expect(screen.getByTestId('discover-row-trending')).toContainElement(
      screen.getByTestId('discover-unavailable-movie-trending'),
    )
    expect(screen.queryByTestId('discover-unavailable-movie-new_releases')).not.toBeInTheDocument()
    expect(screen.getByTestId('discover-row-newReleases')).not.toHaveTextContent('tmdb is not configured')
  })
})

// ---- empty rows + missing fields ---------------------------------------------

describe('DiscoverPage — empty rows and missing fields', () => {
  it('a row with zero items and no unavailable cells renders the EmptyState', async () => {
    vi.mocked(client.discover).mockResolvedValue(
      page({ trending: [item({ kind: 'movie', sourceId: 'm1', title: 'Only Movie' })] }),
    )
    renderDiscover()
    await screen.findByTestId('discover-row-empty-upcoming')
    expect(screen.getByTestId('discover-row-empty-upcoming')).toHaveTextContent('Nothing coming up.')
    expect(screen.getByTestId('discover-row-empty-newReleases')).toHaveTextContent('No new releases right now.')
    // The row with items has no EmptyState.
    expect(screen.queryByTestId('discover-row-empty-trending')).not.toBeInTheDocument()
  })

  it('a row whose cells are unavailable (and has no items) shows the notes instead of an EmptyState', async () => {
    vi.mocked(client.discover).mockResolvedValue(
      page({
        unavailable: [
          cellError('movie', 'upcoming', 'calendar is not configured'),
          cellError('tv', 'upcoming', 'calendar is not configured'),
          cellError('music', 'upcoming', 'calendar is not configured'),
          cellError('book', 'upcoming', 'books library is not configured'),
        ],
      }),
    )
    renderDiscover()
    await screen.findByTestId('discover-unavailable-movie-upcoming')
    expect(screen.queryByTestId('discover-row-empty-upcoming')).not.toBeInTheDocument()
    expect(screen.getByTestId('discover-row-upcoming')).toHaveTextContent('calendar is not configured')
  })

  it('empty posterUrl renders the initials stand-in; a real posterUrl renders the image', async () => {
    vi.mocked(client.discover).mockResolvedValue(
      page({
        trending: [
          item({ kind: 'movie', sourceId: 'm1', title: 'With Poster', posterUrl: 'https://image.tmdb.org/t/p.jpg' }),
          item({ kind: 'book', sourceId: 'b1', title: 'The Wire', posterUrl: '' }),
        ],
      }),
    )
    renderDiscover()
    const movieCard = await screen.findByTestId('discover-card-trending-movie-m1')
    const img = movieCard.querySelector('img')
    expect(img).not.toBeNull()
    expect(img?.getAttribute('src')).toBe('https://image.tmdb.org/t/p.jpg')

    // No poster → the posterInitials stand-in (no invented art).
    const bookCard = screen.getByTestId('discover-card-trending-book-b1')
    expect(bookCard.querySelector('img')).toBeNull()
    expect(bookCard.querySelector('span[aria-hidden]')?.textContent).toBe('W')
  })

  it('never crashes on missing fields (year 0, empty subtitle, empty date) and keeps no stray meta separators', async () => {
    vi.mocked(client.discover).mockResolvedValue(
      page({
        upcoming: [
          // The fully-optional item: everything missing.
          item({ kind: 'tv', sourceId: 't1', title: 'Bare Item' }),
          // The fully-populated item.
          item({ kind: 'movie', sourceId: 'm1', title: 'Full Item', year: 2026, subtitle: 'Studio X', posterUrl: '', date: '2026-11-24T00:00:00Z' }),
        ],
      }),
    )
    renderDiscover()
    const bare = await screen.findByTestId('discover-card-upcoming-tv-t1')
    expect(bare).toHaveTextContent('Bare Item')
    // No meta line at all (PosterCard omits it) — no stray '·'. Only the
    // title span is .truncate (the meta span would add a second one).
    expect(bare.querySelectorAll('.truncate')).toHaveLength(1)
    expect(bare).not.toHaveTextContent('·')

    // The full item composes date · subtitle (the year is inside the date).
    const full = screen.getByTestId('discover-card-upcoming-movie-m1')
    expect(full).toHaveTextContent('Studio X')
  })
})

// ---- loading -------------------------------------------------------------------

describe('DiscoverPage — loading', () => {
  it('shows a per-row loading line for the single in-flight request, then the rows', async () => {
    let resolvePage!: (p: DiscoverPage) => void
    vi.mocked(client.discover).mockReturnValue(
      new Promise<DiscoverPage>((r) => {
        resolvePage = r
      }),
    )
    renderDiscover()

    // Per-row loading lines (the established loading pattern), no grids yet.
    await screen.findByTestId('discover-row-loading-trending')
    expect(screen.getByTestId('discover-row-loading-upcoming')).toBeInTheDocument()
    expect(screen.getByTestId('discover-row-loading-newReleases')).toBeInTheDocument()
    expect(screen.queryByTestId('discover-row-grid-trending')).not.toBeInTheDocument()

    // The request resolves: the rows render their content.
    resolvePage(page({ trending: [item({ kind: 'book', sourceId: 'b1', title: 'Settled' })] }))
    await screen.findByTestId('discover-card-trending-book-b1')
    expect(screen.queryByTestId('discover-row-loading-trending')).not.toBeInTheDocument()
  })
})

// ---- navigation (no invented routes) --------------------------------------------

describe('DiscoverPage — navigation', () => {
  it('upcoming movie items deep-link to /movies/:id with the local library id', async () => {
    vi.mocked(client.discover).mockResolvedValue(
      page({ upcoming: [item({ kind: 'movie', sourceId: '42', title: 'Monitored Movie' })] }),
    )
    renderDiscover()
    const button = await screen.findByTestId('discover-card-upcoming-movie-42-open')
    button.click()
    expect(await screen.findByTestId('sentinel')).toHaveTextContent('/movies/42')
  })

  it('upcoming tv items deep-link to /tv/:id', async () => {
    vi.mocked(client.discover).mockResolvedValue(
      page({ upcoming: [item({ kind: 'tv', sourceId: '77', title: 'Monitored Show', subtitle: 'S1E1' })] }),
    )
    renderDiscover()
    const button = await screen.findByTestId('discover-card-upcoming-tv-77-open')
    button.click()
    expect(await screen.findByTestId('sentinel')).toHaveTextContent('/tv/77')
  })

  it('upcoming music items deep-link to /music/:id', async () => {
    vi.mocked(client.discover).mockResolvedValue(
      page({ upcoming: [item({ kind: 'music', sourceId: '9', title: 'Monitored Artist' })] }),
    )
    renderDiscover()
    const button = await screen.findByTestId('discover-card-upcoming-music-9-open')
    button.click()
    expect(await screen.findByTestId('sentinel')).toHaveTextContent('/music/9')
  })

  it('upcoming book items do not navigate (title id; there is no title-level detail route)', async () => {
    vi.mocked(client.discover).mockResolvedValue(
      page({ upcoming: [item({ kind: 'book', sourceId: '55', title: 'Monitored Title' })] }),
    )
    renderDiscover()
    const button = await screen.findByTestId('discover-card-upcoming-book-55-open')
    button.click()
    expect(screen.queryByTestId('sentinel')).not.toBeInTheDocument()
    // Still on the Discover page.
    expect(screen.getByTestId('discover-page-title')).toBeInTheDocument()
  })

  it('movie feed items (trending / newReleases) link to the add flow with ?tmdb= (the R3 pattern)', async () => {
    vi.mocked(client.discover).mockResolvedValue(
      page({
        trending: [item({ kind: 'movie', sourceId: '603', title: 'Trending Movie' })],
        newReleases: [item({ kind: 'movie', sourceId: '909', title: 'Now Playing' })],
      }),
    )
    renderDiscover()
    const button = await screen.findByTestId('discover-card-trending-movie-603-open')
    button.click()
    expect(await screen.findByTestId('sentinel')).toHaveTextContent('/movies/add?tmdb=603')
  })

  it('external-only feed items of other kinds do not navigate (their add flows are manual search)', async () => {
    vi.mocked(client.discover).mockResolvedValue(
      page({
        trending: [
          item({ kind: 'tv', sourceId: '81858', title: 'Trending Show' }),
          item({ kind: 'music', sourceId: 'mb-1', title: 'Trending Artist' }),
          item({ kind: 'book', sourceId: 'ol-1', title: 'Trending Book' }),
        ],
      }),
    )
    renderDiscover()
    const tvButton = await screen.findByTestId('discover-card-trending-tv-81858-open')
    tvButton.click()
    expect(screen.queryByTestId('sentinel')).not.toBeInTheDocument()
    screen.getByTestId('discover-card-trending-music-mb-1-open').click()
    expect(screen.queryByTestId('sentinel')).not.toBeInTheDocument()
    screen.getByTestId('discover-card-trending-book-ol-1-open').click()
    expect(screen.queryByTestId('sentinel')).not.toBeInTheDocument()
    expect(screen.getByTestId('discover-page-title')).toBeInTheDocument()
  })
})

// ---- total failure -----------------------------------------------------------------

describe('DiscoverPage — total failure', () => {
  it('a failed request shows a page-level error with retry (per-cell notes are NOT the failure path)', async () => {
    class ApiError extends Error {
      status = 502
      constructor(message: string) {
        super(message)
      }
    }
    vi.mocked(client.discover).mockRejectedValueOnce(new ApiError('upstream failed'))
    renderDiscover()
    const note = await screen.findByTestId('discover-error-message')
    expect(note).toHaveTextContent('upstream failed')
    // No row content in the error state.
    expect(screen.queryByTestId('discover-row-grid-trending')).not.toBeInTheDocument()

    // Retry re-issues the request and renders the page.
    vi.mocked(client.discover).mockResolvedValue(
      page({ trending: [item({ kind: 'book', sourceId: 'b1', title: 'After Retry' })] }),
    )
    screen.getByTestId('discover-retry').click()
    await screen.findByTestId('discover-card-trending-book-b1')
    expect(client.discover).toHaveBeenCalledTimes(2)
  })
})

// ---- pure helpers ---------------------------------------------------------------------

describe('pure helpers', () => {
  it('groupRowItems orders the kinds movie, tv, music, book and drops empty kinds', () => {
    const items = [
      item({ kind: 'book', sourceId: 'b1', title: 'B' }),
      item({ kind: 'movie', sourceId: 'm1', title: 'M' }),
      item({ kind: 'tv', sourceId: 't1', title: 'T' }),
      item({ kind: 'movie', sourceId: 'm2', title: 'M2' }),
    ]
    const groups = groupRowItems(items)
    expect(groups.map(([kind]) => kind)).toEqual(['movie', 'tv', 'book'])
    expect(groups[0]?.[1].map((i) => i.sourceId)).toEqual(['m1', 'm2'])
    expect(groupRowItems([])).toEqual([])
  })

  it('cellErrorsForRow maps the newReleases row to the server key new_releases and keeps cell order', () => {
    const cells = [
      cellError('music', 'new_releases', 'a'),
      cellError('book', 'new_releases', 'b'),
      cellError('movie', 'trending', 'c'),
    ]
    const byId = (id: string) => DISCOVER_ROWS.find((r) => r.id === id) as DiscoverRow
    expect(cellErrorsForRow(byId('newReleases'), cells).map((c) => c.cell)).toEqual([
      'music:new_releases',
      'book:new_releases',
    ])
    expect(cellErrorsForRow(byId('trending'), cells).map((c) => c.cell)).toEqual(['movie:trending'])
    expect(cellErrorsForRow(byId('upcoming'), cells)).toEqual([])
  })

  it('itemMeta composes subtitle · year and drops the year when a date leads', () => {
    const base = { kind: 'book' as DiscoverKind, sourceId: 'x', title: 'X', posterUrl: '', date: '' }
    expect(itemMeta({ ...base, subtitle: 'Frank Herbert', year: 1965 })).toBe('Frank Herbert · 1965')
    expect(itemMeta({ ...base, subtitle: '', year: 0 })).toBe('')
    expect(itemMeta({ ...base, subtitle: '', year: 2026 })).toBe('2026')
    // Dated item: the human date leads, the (redundant) year is dropped.
    const dated = '2026-11-24T00:00:00Z'
    const expected = `${new Date(dated).toLocaleDateString([], {
      month: 'short',
      day: 'numeric',
      year: 'numeric',
    })} · S2E4`
    expect(itemMeta({ ...base, subtitle: 'S2E4', year: 2026, date: dated })).toBe(expected)
  })

  it('dateLabel is empty for missing or unparseable dates (the UI must not crash)', () => {
    expect(dateLabel('')).toBe('')
    expect(dateLabel('not-a-date')).toBe('')
  })

  it('targetFor: upcoming rows → detail routes (books excluded); feed rows → ?tmdb= for numeric movie ids only', () => {
    const byId = (id: string) => DISCOVER_ROWS.find((r) => r.id === id) as DiscoverRow
    const upcoming = byId('upcoming')
    const trending = byId('trending')
    const newReleases = byId('newReleases')
    const mk = (kind: DiscoverKind, sourceId: string) =>
      item({ kind, sourceId, title: 'T', year: 0, subtitle: '', posterUrl: '', date: '' })

    expect(targetFor(upcoming, mk('movie', '42'))).toBe('/movies/42')
    expect(targetFor(upcoming, mk('tv', '77'))).toBe('/tv/77')
    expect(targetFor(upcoming, mk('music', '9'))).toBe('/music/9')
    expect(targetFor(upcoming, mk('book', '55'))).toBeNull()

    expect(targetFor(trending, mk('movie', '603'))).toBe('/movies/add?tmdb=603')
    expect(targetFor(newReleases, mk('movie', '909'))).toBe('/movies/add?tmdb=909')
    expect(targetFor(trending, mk('movie', 'not-a-number'))).toBeNull()
    expect(targetFor(trending, mk('tv', '81858'))).toBeNull()
    expect(targetFor(trending, mk('music', 'mb-1'))).toBeNull()
    expect(targetFor(trending, mk('book', 'ol-1'))).toBeNull()
  })

  it('the model exposes the four kinds in the documented order', () => {
    expect(DISCOVER_KINDS).toEqual(['movie', 'tv', 'music', 'book'])
    expect(DISCOVER_ROWS.map((r) => r.id)).toEqual(['trending', 'upcoming', 'newReleases'])
  })
})
