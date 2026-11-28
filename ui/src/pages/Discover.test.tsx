import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { client } from '../api/client'
import type {
  Artist,
  Book,
  Dashboard,
  DiscoverCellError,
  DiscoverItem,
  DiscoverPage,
  Movie,
  Series,
} from '../api/types'
import { DiscoverPage as DiscoverPageComponent } from './Discover'

// #151: the cinematic row set. The ONLY thing mocked is the API layer (the
// client); every assertion is on the real rendered output (the #118 /
// GlobalSearch test pattern).
vi.mock('../api/client', () => {
  return {
    client: {
      discover: vi.fn(),
      getDashboard: vi.fn(),
      listMovies: vi.fn(),
      listSeries: vi.fn(),
      listArtists: vi.fn(),
      listBooks: vi.fn(),
    },
  }
})

// ---- fixture builders (the real wire shapes) --------------------------------

function movie(p: Partial<Movie> & Pick<Movie, 'ID' | 'Title'>): Movie {
  return {
    Year: 0,
    QualityProfile: 'Standard',
    Monitored: false,
    AddedAt: '2026-11-01T00:00:00Z',
    Tags: [],
    RootFolder: '',
    ...p,
  } as Movie
}

function series(p: Partial<Series> & Pick<Series, 'ID' | 'Title'>): Series {
  return {
    TVDBID: 0,
    TMDBID: 0,
    Year: 0,
    QualityProfile: 'Standard',
    Monitored: false,
    SeriesType: 'standard',
    SeasonFolder: false,
    Tags: [],
    AddedAt: '2026-11-01T00:00:00Z',
    ...p,
  } as Series
}

function artist(p: Partial<Artist> & Pick<Artist, 'ID' | 'Name'>): Artist {
  return {
    MBID: '',
    Monitored: false,
    AddedAt: '2026-11-01T00:00:00Z',
    ...p,
  } as Artist
}

function book(p: Partial<Book> & Pick<Book, 'ID' | 'Name'>): Book {
  return {
    AuthorID: 0,
    Monitored: false,
    AddedAt: '2026-11-01T00:00:00Z',
    AuthorName: '',
    HasFile: false,
    Format: '',
    ...p,
  } as Book
}

type ItemSpec = Pick<DiscoverItem, 'kind' | 'sourceId' | 'title'> & Partial<DiscoverItem>
function item(spec: ItemSpec): DiscoverItem {
  return { year: 0, subtitle: '', posterUrl: '', date: '', ...spec }
}

function cellError(kind: string, row: string, reason: string): DiscoverCellError {
  return { cell: `${kind}:${row}`, reason }
}

function feedPage(partial: Partial<DiscoverPage> = {}): DiscoverPage {
  return { trending: [], upcoming: [], newReleases: [], unavailable: [], ...partial }
}

function pageOf<T>(items: T[]) {
  return { items, total: items.length, page: 1, page_size: 25 }
}

const RAW = {
  tmdb: 'tmdb is not configured (missing API key)',
  gbooks: 'google books is not configured (missing API key)',
  mb: 'musicbrainz is not configured',
  ol: 'openlibrary is not configured',
  cal: 'calendar is not configured',
  lib: 'books library is not configured',
} as const

function dashFixture(p: Partial<Dashboard> = {}): Dashboard {
  return {
    libraries: [
      { kind: 'movie', label: 'Movies', count: 3, wants: 2, queue: 1 },
      { kind: 'tv', label: 'TV', count: 4, wants: 0, queue: 0 },
    ],
    totals: { libraries: 2, wants: 2, queue: 1 },
    recent_history: [],
    recent_queue: [],
    indexers: [],
    generated_at: '2026-11-27T00:00:00Z',
    ...p,
  }
}

// ---- render helpers ----------------------------------------------------------

// Sentinel: stands in for the detail / add / list routes. It echoes the
// landed URL (pathname + search), so a click proves BOTH the route and the
// id/params that reached it.
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
        <Route path="/movies" element={<Sentinel />} />
        <Route path="/tv" element={<Sentinel />} />
        <Route path="/music" element={<Sentinel />} />
        <Route path="/books" element={<Sentinel />} />
      </Routes>
    </MemoryRouter>,
  )
}

// Baseline: every section empty, nothing rejected. Tests override what they
// need (the page's one loading phase resolves in a microtask).
function defaults() {
  vi.mocked(client.discover).mockResolvedValue(feedPage())
  vi.mocked(client.getDashboard).mockResolvedValue(dashFixture())
  vi.mocked(client.listMovies).mockResolvedValue(pageOf<Movie>([]))
  vi.mocked(client.listSeries).mockResolvedValue(pageOf<Series>([]))
  vi.mocked(client.listArtists).mockResolvedValue(pageOf<Artist>([]))
  vi.mocked(client.listBooks).mockResolvedValue([])
}

beforeEach(() => {
  vi.clearAllMocks()
  defaults()
})

// ---- the row set ----------------------------------------------------------------

describe('row set', () => {
  it('renders the KPI strip and all ten rows in the reference order', async () => {
    renderDiscover()
    await screen.findByTestId('discover-row-trending')

    expect(screen.getByTestId('discover-page')).toBeInTheDocument()
    expect(screen.getByTestId('discover-page-title')).toHaveTextContent('Discover')
    expect(screen.getByTestId('discover-kpi')).toBeInTheDocument()

    const sections = [...document.querySelectorAll('section[data-testid^="discover-row-"]')]
    expect(sections.map((s) => s.getAttribute('data-testid'))).toEqual([
      'discover-row-recentlyAdded',
      'discover-row-recentActivity',
      'discover-row-trending',
      'discover-row-popularMovies',
      'discover-row-popularSeries',
      'discover-row-upcomingMovies',
      'discover-row-upcomingSeries',
      'discover-row-watchlist',
      'discover-row-movieGenres',
      'discover-row-studios',
    ])
  })

  it('every row has chevron scrollers and a focusable (keyboard) scroll track', async () => {
    renderDiscover()
    await screen.findByTestId('discover-row-studios')

    // 10 rows × (prev + next).
    expect(screen.getAllByTestId(/^discover-row-[\w]+-prev$/).length).toBe(10)
    expect(screen.getAllByTestId(/^discover-row-[\w]+-next$/).length).toBe(10)
    // At rest (no scroll) neither direction is enabled.
    expect(screen.getByTestId('discover-row-trending-prev')).toBeDisabled()
    expect(screen.getByTestId('discover-row-trending-next')).toBeDisabled()
    // The track is keyboard reachable (tabIndex 0) and labelled.
    const track = screen.getByRole('group', { name: 'Trending' })
    expect(track).toHaveAttribute('tabindex', '0')
  })
})

// ---- KPI strip (the absorbed Dashboard counts) ------------------------------------

describe('KPI strip (absorbed Dashboard)', () => {
  it('renders per-kind counts with wants/queue and links into the list pages', async () => {
    renderDiscover()
    const movieKpi = await screen.findByTestId('discover-kpi-movie')
    expect(movieKpi).toHaveTextContent('Movies')
    expect(movieKpi).toHaveTextContent('3')
    expect(movieKpi).toHaveTextContent('wants 2')
    expect(movieKpi).toHaveTextContent('queue 1')
    const total = screen.getByTestId('discover-kpi-total')
    expect(total).toHaveTextContent('2')
    expect(total).toHaveTextContent('2 wants')
    expect(total).toHaveTextContent('1 in queue')
  })

  it('the KPI cards are links into the kind list routes', async () => {
    renderDiscover()
    const movieKpi = await screen.findByTestId('discover-kpi-movie')
    expect(movieKpi.tagName).toBe('A')
    expect(movieKpi).toHaveAttribute('href', '/movies')
    const booksKpi = screen.getByTestId('discover-kpi-total')
    expect(booksKpi).not.toHaveAttribute('href')
  })
})

// ---- Recent Activity (the absorbed Dashboard feeds) --------------------------------

describe('Recent Activity row (landscape cards)', () => {
  it('merges queue + history, newest first, with status pills and sources', async () => {
    vi.mocked(client.getDashboard).mockResolvedValue(
      dashFixture({
        recent_queue: [
          {
            kind: 'movie',
            release_title: 'Q Movie',
            indexer: 'NZBsorg',
            state: 'queued',
            progress: 0,
            updated_at: '2026-11-27T10:00:00Z',
          },
        ],
        recent_history: [
          { kind: 'music', name: 'H Music', event: 'download', detail: '', at: '2026-11-27T12:00:00Z' },
          { kind: 'book', name: 'H Book', event: 'added', detail: '', at: '2026-11-27T09:00:00Z' },
        ],
      }),
    )
    renderDiscover()
    await screen.findByTestId('discover-card-recentActivity-history-0')

    const row = screen.getByTestId('discover-row-recentActivity')
    const cards = [...row.querySelectorAll('[data-testid^="discover-card-recentActivity-"]')]
    expect(cards.map((c) => (c as HTMLElement).dataset.testid)).toEqual([
      'discover-card-recentActivity-history-0',
      'discover-card-recentActivity-queue-0',
      'discover-card-recentActivity-history-1',
    ])
    // The first card is the newest (history at 12:00 beats the queue at 10:00).
    expect(cards[0]).toHaveTextContent('H Music')
    expect(cards[0]).toHaveTextContent('download')
    // The queue card carries its state pill + indexer source.
    expect(cards[1]).toHaveTextContent('Q Movie')
    expect(cards[1]).toHaveTextContent('queued')
    expect(cards[1]).toHaveTextContent('via NZBsorg')
  })
})

// ---- Recently Added -----------------------------------------------------------------

describe('Recently Added row', () => {
  it('merges all four kinds, newest AddedAt first', async () => {
    vi.mocked(client.listMovies).mockImplementation(async (q) =>
      q?.monitored ? pageOf<Movie>([]) : pageOf([movie({ ID: 1, Title: 'M1', AddedAt: '2026-11-10T00:00:00Z' })]),
    )
    vi.mocked(client.listSeries).mockResolvedValue(
      pageOf([series({ ID: 10, Title: 'S10', AddedAt: '2026-11-12T00:00:00Z' })]),
    )
    vi.mocked(client.listArtists).mockResolvedValue(
      pageOf([artist({ ID: 100, Name: 'Artist100', AddedAt: '2026-11-11T00:00:00Z' })]),
    )
    vi.mocked(client.listBooks).mockResolvedValue([book({ ID: 1000, Name: 'Book1000', AddedAt: '2026-11-08T00:00:00Z' })])
    renderDiscover()
    await screen.findByTestId('discover-card-recentlyAdded-tv-10')

    const row = screen.getByTestId('discover-row-recentlyAdded')
    const cards = [...row.querySelectorAll('[data-testid^="discover-card-recentlyAdded-"]')]
    expect(cards.map((c) => (c as HTMLElement).dataset.testid)).toEqual([
      'discover-card-recentlyAdded-tv-10',
      'discover-card-recentlyAdded-music-100',
      'discover-card-recentlyAdded-movie-1',
      'discover-card-recentlyAdded-book-1000',
    ])
  })
})

// ---- Watchlist ------------------------------------------------------------------------

describe('Watchlist row', () => {
  it('shows the monitored items only, grouped by kind in the deterministic order', async () => {
    vi.mocked(client.listMovies).mockImplementation(async (q) =>
      q?.monitored
        ? pageOf([movie({ ID: 2, Title: 'Watched M2', Monitored: true }), movie({ ID: 1, Title: 'Watched M1', Monitored: true })])
        : pageOf([]),
    )
    vi.mocked(client.listSeries).mockImplementation(async (q) =>
      q?.monitored ? pageOf([series({ ID: 5, Title: 'Watched S5', Monitored: true })]) : pageOf([]),
    )
    vi.mocked(client.listArtists).mockResolvedValue(pageOf([]))
    vi.mocked(client.listBooks).mockResolvedValue([
      book({ ID: 55, Name: 'Watched Book', Monitored: true }),
      book({ ID: 56, Name: 'Unwatched Book', Monitored: false }),
    ])
    renderDiscover()
    await screen.findByTestId('discover-card-watchlist-movie-2')

    const row = screen.getByTestId('discover-row-watchlist')
    const cards = [...row.querySelectorAll('[data-testid^="discover-card-watchlist-"]')]
    expect(cards.map((c) => (c as HTMLElement).dataset.testid)).toEqual([
      'discover-card-watchlist-movie-2',
      'discover-card-watchlist-movie-1',
      'discover-card-watchlist-tv-5',
      'discover-card-watchlist-book-55',
    ])
    // The unmonitored book never appears.
    expect(screen.queryByTestId('discover-card-watchlist-book-56')).not.toBeInTheDocument()
  })
})

// ---- feed rows (split by kind) ----------------------------------------------------------

describe('feed rows', () => {
  it('splits new_releases into Popular Movies (movie cells) and Popular Series (tv cells); trending stays mixed', async () => {
    vi.mocked(client.discover).mockResolvedValue(
      feedPage({
        trending: [
          item({ kind: 'movie', sourceId: '603', title: 'Dune: Part Two', year: 2024 }),
          item({ kind: 'tv', sourceId: '81858', title: 'Severance', year: 2025, subtitle: 'S2' }),
          item({ kind: 'music', sourceId: 'mb-1', title: 'Radiohead', subtitle: 'GB' }),
          item({ kind: 'book', sourceId: 'ol-1', title: 'The Long Walk', year: 2025 }),
        ],
        upcoming: [
          item({ kind: 'movie', sourceId: '42', title: 'Monitored Movie', date: '2026-12-01T00:00:00Z' }),
          item({ kind: 'tv', sourceId: '77', title: 'Monitored Show', subtitle: 'S1E1', date: '2026-12-02T00:00:00Z' }),
          item({ kind: 'book', sourceId: '55', title: 'Monitored Book', date: '2026-12-03T00:00:00Z' }),
        ],
        newReleases: [
          item({ kind: 'movie', sourceId: '909', title: 'Now Playing', year: 2026 }),
          item({ kind: 'tv', sourceId: '33', title: 'New Season', year: 2026 }),
        ],
      }),
    )
    renderDiscover()
    await screen.findByTestId('discover-card-trending-movie-603')

    // Trending: all four kinds, mixed.
    const trending = screen.getByTestId('discover-row-trending')
    expect(trending.querySelectorAll('[data-testid^="discover-card-trending-"]')).toHaveLength(4)

    // Popular Movies: only the movie cell of new_releases.
    const popularMovies = screen.getByTestId('discover-row-popularMovies')
    expect(popularMovies.querySelectorAll('[data-testid^="discover-card-popularMovies-"]')).toHaveLength(1)
    expect(screen.getByTestId('discover-card-popularMovies-movie-909')).toHaveTextContent('Now Playing')

    // Popular Series: only the tv cell of new_releases.
    const popularSeries = screen.getByTestId('discover-row-popularSeries')
    expect(popularSeries.querySelectorAll('[data-testid^="discover-card-popularSeries-"]')).toHaveLength(1)
    expect(screen.getByTestId('discover-card-popularSeries-tv-33')).toHaveTextContent('New Season')

    // Upcoming Movies / Series: the kind-filtered upcoming cells.
    expect(screen.getByTestId('discover-card-upcomingMovies-movie-42')).toHaveTextContent('Monitored Movie')
    expect(screen.getByTestId('discover-card-upcomingSeries-tv-77')).toHaveTextContent('Monitored Show')
    // The book upcoming cell is unrendered (recorded deviation) — no card.
    expect(screen.queryByTestId('discover-card-upcomingSeries-book-55')).not.toBeInTheDocument()
  })

  it('deep-links upcoming cards on local ids', async () => {
    const user = userEvent.setup()
    vi.mocked(client.discover).mockResolvedValue(
      feedPage({ upcoming: [item({ kind: 'movie', sourceId: '42', title: 'Monitored Movie' })] }),
    )
    renderDiscover()
    await user.click(await screen.findByTestId('discover-card-upcomingMovies-movie-42'))
    // Upcoming items carry LOCAL ids (server-side matches) → the detail route.
    expect(await screen.findByTestId('sentinel')).toHaveTextContent('/movies/42')
  })

  it('feed cards route into the Movies add flow with the TMDB id preselected', async () => {
    const user = userEvent.setup()
    vi.mocked(client.discover).mockResolvedValue(
      feedPage({ trending: [item({ kind: 'movie', sourceId: '603', title: 'Dune: Part Two', year: 2024 })] }),
    )
    renderDiscover()
    await user.click(await screen.findByTestId('discover-card-trending-movie-603'))
    expect(await screen.findByTestId('sentinel')).toHaveTextContent('/movies/add?tmdb=603')
  })

  it('music/book feed cells are inert (no add-flow preselect exists for them)', async () => {
    vi.mocked(client.discover).mockResolvedValue(
      feedPage({
        trending: [
          item({ kind: 'music', sourceId: 'mb-1', title: 'Radiohead' }),
          item({ kind: 'book', sourceId: 'ol-1', title: 'The Long Walk' }),
        ],
      }),
    )
    renderDiscover()
    const musicCard = await screen.findByTestId('discover-card-trending-music-mb-1')
    expect(musicCard.closest('a')).toBeNull()
    const bookCard = screen.getByTestId('discover-card-trending-book-ol-1')
    expect(bookCard.closest('a')).toBeNull()
  })
})

// ---- degradation (acceptance-critical) ----------------------------------------------------

describe('degradation (no raw provider strings)', () => {
  const ALL_CELLS = [
    cellError('movie', 'trending', RAW.tmdb),
    cellError('tv', 'trending', RAW.tmdb),
    cellError('music', 'trending', RAW.mb),
    cellError('book', 'trending', RAW.ol),
    cellError('movie', 'new_releases', RAW.tmdb),
    cellError('tv', 'new_releases', RAW.tmdb),
    cellError('music', 'new_releases', RAW.mb),
    cellError('book', 'new_releases', RAW.gbooks),
    cellError('movie', 'upcoming', RAW.cal),
    cellError('tv', 'upcoming', RAW.cal),
    cellError('music', 'upcoming', RAW.cal),
    cellError('book', 'upcoming', RAW.lib),
  ]

  it('never renders a raw wire reason anywhere on the page (grep-level)', async () => {
    vi.mocked(client.discover).mockResolvedValue(feedPage({ unavailable: ALL_CELLS }))
    renderDiscover()
    await screen.findByTestId('discover-unclaimed-notes')

    const text = document.body.textContent ?? ''
    for (const raw of Object.values(RAW)) {
      expect(text, `raw reason leaked: "${raw}"`).not.toContain(raw)
    }
    // The friendly notes carry the actionable wording instead.
    expect(text).toContain('TMDB is not configured — add an API key in Settings to enable this feed.')
    expect(text).toContain('MusicBrainz is not configured — enable it in Settings to see music here.')
    expect(text).toContain('Google Books is not configured — add an API key in Settings to enable this feed.')
    expect(text).toContain('Open Library is not configured — enable it in Settings to see books here.')
  })

  it('the unrendered music/book cells of the split rows surface their friendly notes in one muted block', async () => {
    vi.mocked(client.discover).mockResolvedValue(feedPage({ unavailable: ALL_CELLS }))
    renderDiscover()
    const unclaimed = await screen.findByTestId('discover-unclaimed-notes')
    expect(unclaimed).toHaveTextContent('MusicBrainz is not configured')
    expect(unclaimed).toHaveTextContent('Google Books is not configured')
    expect(unclaimed).toHaveTextContent('books library has not been set up')
    // Deduped: the music note appears once, not twice (new_releases + upcoming).
    expect(unclaimed.textContent?.match(/MusicBrainz is not configured/g)).toHaveLength(1)
  })

  it('a fully keyless row shows the styled grey-out notes IN the row, not an EmptyState', async () => {
    vi.mocked(client.discover).mockResolvedValue(
      feedPage({
        upcoming: [],
        unavailable: [
          cellError('movie', 'upcoming', RAW.cal),
          cellError('tv', 'upcoming', RAW.cal),
          cellError('music', 'upcoming', RAW.cal),
          cellError('book', 'upcoming', RAW.lib),
        ],
      }),
    )
    renderDiscover()
    // The loading and loaded trees are different: wait for the data
    // (notes exist only once loaded) BEFORE capturing the row node.
    await screen.findAllByTestId('discover-row-notes')
    const row = screen.getByTestId('discover-row-upcomingMovies')
    const notes = within(row).getByTestId('discover-row-notes')
    expect(notes).toHaveTextContent('calendar has not been set up')
    // The row claims its own cells: no EmptyState for a row whose cells
    // are unavailable.
    expect(screen.queryByTestId('discover-row-empty-upcomingMovies')).not.toBeInTheDocument()
    expect(screen.queryByTestId('discover-row-empty-upcomingSeries')).not.toBeInTheDocument()
  })

  it('a partial row keeps its items next to the per-cell grey-out note', async () => {
    vi.mocked(client.discover).mockResolvedValue(
      feedPage({
        upcoming: [item({ kind: 'movie', sourceId: '42', title: 'Still Coming' })],
        unavailable: [cellError('movie', 'upcoming', RAW.cal)],
      }),
    )
    renderDiscover()
    await screen.findByTestId('discover-card-upcomingMovies-movie-42')
    const row = screen.getByTestId('discover-row-upcomingMovies')
    expect(row).toHaveTextContent('Still Coming')
    expect(row).toHaveTextContent('calendar has not been set up')
  })
})

// ---- empty + failure modes --------------------------------------------------------------

describe('empty and failure modes', () => {
  it('an empty (but healthy) row shows its EmptyState', async () => {
    renderDiscover()
    await screen.findByTestId('discover-row-empty-trending')
    expect(screen.getByTestId('discover-row-empty-watchlist')).toBeInTheDocument()
    // The KPI strip still renders (the dashboard succeeded).
    expect(screen.getByTestId('discover-kpi')).toBeInTheDocument()
  })

  it('a TOTAL discover failure is the only page-level error (with retry)', async () => {
    const user = userEvent.setup()
    vi.mocked(client.discover).mockRejectedValue(new Error('boom network'))
    renderDiscover()

    const message = await screen.findByTestId('discover-error-message')
    expect(message).toHaveTextContent('boom network')
    // The rows are not rendered at all in the total-failure state.
    expect(screen.queryByTestId('discover-row-trending')).not.toBeInTheDocument()

    await user.click(screen.getByTestId('discover-retry'))
    expect(client.discover).toHaveBeenCalledTimes(2)
  })

  it('a failed dashboard degrades only the KPI strip; the rows still render', async () => {
    vi.mocked(client.getDashboard).mockRejectedValue(new Error('dash down'))
    vi.mocked(client.discover).mockResolvedValue(
      feedPage({ trending: [item({ kind: 'movie', sourceId: '603', title: 'Dune' })] }),
    )
    renderDiscover()

    // KPI strip degraded to a note…
    expect(await screen.findByText('The summary counts could not be loaded.')).toBeInTheDocument()
    // …and the feed rows still render.
    expect(await screen.findByTestId('discover-card-trending-movie-603')).toHaveTextContent('Dune')
  })
})
