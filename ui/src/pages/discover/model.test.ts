// #151: pure row-model + degradation helpers for the Discover cinematic row
// set. TDD RED slice: these tests define the contract; model.ts implements.
import { describe, expect, it } from 'vitest'
import type {
  Artist,
  Book,
  Dashboard,
  DiscoverCellError,
  DiscoverPage,
  Movie,
  Series,
} from '../../api/types'
import {
  DISCOVER_KIND_LABEL,
  DISCOVER_KINDS,
  DISCOVER_ROWS,
  buildRecentlyAdded,
  buildWatchlist,
  cellNote,
  cellsForRow,
  feedItemsForRow,
  feedTarget,
  libraryTarget,
  mergeRecentActivity,
  queueTone,
  rowNotes,
  topGenres,
  topStudios,
  unclaimedCells,
} from './model'

// ---- fixture builders (real wire shapes) ------------------------------------

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

function cell(kind: string, row: string, reason: string): DiscoverCellError {
  return { cell: `${kind}:${row}`, reason }
}

function dash(p: Partial<Dashboard> = {}): Dashboard {
  return {
    libraries: [],
    totals: { libraries: 0, wants: 0, queue: 0 },
    recent_history: [],
    recent_queue: [],
    indexers: [],
    generated_at: '2026-11-27T00:00:00Z',
    ...p,
  }
}

function discoverPage(p: Partial<DiscoverPage> = {}): DiscoverPage {
  return { trending: [], upcoming: [], newReleases: [], unavailable: [], ...p }
}

// ---- row set -------------------------------------------------------------------

describe('row set', () => {
  it('exposes the eleven rows in render order (KPI strip is not a row)', () => {
    expect(DISCOVER_ROWS.map((r) => r.id)).toEqual([
      'recentlyAdded',
      'recentActivity',
      'trending',
      'popularMovies',
      'popularSeries',
      'upcomingMovies',
      'upcomingSeries',
      'watchlist',
      'movieGenres',
      'studios',
    ])
  })

  it('maps the feed rows to the server cell keys and the kind-filtered rows to one kind', () => {
    const byId = (id: string) => DISCOVER_ROWS.find((r) => r.id === id)
    expect(byId('trending')?.cellRow).toBe('trending')
    expect(byId('trending')?.kind).toBeUndefined()
    expect(byId('popularMovies')?.cellRow).toBe('new_releases')
    expect(byId('popularMovies')?.kind).toBe('movie')
    expect(byId('popularSeries')?.cellRow).toBe('new_releases')
    expect(byId('popularSeries')?.kind).toBe('tv')
    expect(byId('upcomingMovies')?.cellRow).toBe('upcoming')
    expect(byId('upcomingMovies')?.kind).toBe('movie')
    expect(byId('upcomingSeries')?.cellRow).toBe('upcoming')
    expect(byId('upcomingSeries')?.kind).toBe('tv')
    // The non-feed rows claim no cells.
    expect(byId('watchlist')?.cellRow).toBeUndefined()
    expect(byId('movieGenres')?.cellRow).toBeUndefined()
  })

  it('keeps the four kinds in the documented order', () => {
    expect(DISCOVER_KINDS).toEqual(['movie', 'tv', 'music', 'book'])
    expect(Object.values(DISCOVER_KIND_LABEL)).toEqual(['Movies', 'TV', 'Music', 'Books'])
  })
})

// ---- degradation mapper (acceptance-critical) ---------------------------------

describe('cellNote (the one deterministic DiscoverCellError → friendly mapper)', () => {
  const RAW = [
    'tmdb is not configured (missing API key)',
    'google books is not configured (missing API key)',
    'musicbrainz is not configured',
    'openlibrary is not configured',
    'calendar is not configured',
    'books library is not configured',
  ]

  it('maps every known not-configured reason to a friendly note that does not echo the raw reason', () => {
    for (const reason of RAW) {
      const note = cellNote(cell('movie', 'trending', reason))
      expect(note).toBeTruthy()
      // The raw wire reason (lowercase provider + "(missing API key)"
      // phrasing) must never be rendered.
      expect(note).not.toContain(reason)
      expect(note).not.toContain('(missing API key)')
    }
  })

  it('names the provider in friendly, page-safe wording', () => {
    expect(cellNote(cell('movie', 'trending', RAW[0]))).toContain('TMDB')
    expect(cellNote(cell('movie', 'trending', RAW[0]))).toContain('Settings')
    expect(cellNote(cell('music', 'trending', RAW[2]))).toContain('MusicBrainz')
    expect(cellNote(cell('book', 'new_releases', RAW[1]))).toContain('Google Books')
    expect(cellNote(cell('book', 'trending', RAW[3]))).toContain('Open Library')
    expect(cellNote(cell('tv', 'upcoming', RAW[4]))).toContain('calendar')
    expect(cellNote(cell('book', 'upcoming', RAW[5]))).toContain('books library')
  })

  it('prefixes only the generic (feed-specific) note with the kind label; known reasons are feed-independent', () => {
    // Unknown reason → the note names the feed.
    expect(cellNote(cell('movie', 'trending', 'tmdb trending movies: GET https://api.tmdb.org/... 502'))).toMatch(
      /^Movies: /,
    )
    // Known not-configured reasons carry no kind prefix (one note per feed,
    // rendered once by rowNotes' dedupe).
    expect(cellNote(cell('tv', 'upcoming', RAW[4]))).not.toMatch(/^TV: /)
    expect(cellNote(cell('music', 'trending', RAW[2]))).not.toMatch(/^Music: /)
  })

  it('an unknown (pass-through) reason gets the generic friendly note, never the raw text', () => {
    const note = cellNote(cell('movie', 'trending', 'tmdb trending movies: GET https://api.tmdb.org/... 502'))
    expect(note).not.toContain('502')
    expect(note).not.toContain('tmdb trending movies')
    expect(note).toMatch(/^Movies: /)
    expect(note).toContain('unavailable')
  })

  it('rowNotes dedupes identical mapped notes (one per reason) and keeps order', () => {
    const notes = rowNotes([
      cell('movie', 'upcoming', 'calendar is not configured'),
      cell('tv', 'upcoming', 'calendar is not configured'),
      cell('music', 'upcoming', 'musicbrainz is not configured'),
    ])
    expect(notes).toHaveLength(2)
    expect(notes[0]).toContain('calendar')
    expect(notes[1]).toContain('MusicBrainz')
    expect(rowNotes([])).toEqual([])
  })
})

// ---- cell claiming per row ------------------------------------------------------

describe('cellsForRow / unclaimedCells', () => {
  const reasons = [
    'tmdb is not configured (missing API key)',
    'tmdb is not configured (missing API key)',
    'musicbrainz is not configured',
    'openlibrary is not configured',
    'tmdb is not configured (missing API key)',
    'tmdb is not configured (missing API key)',
    'musicbrainz is not configured',
    'google books is not configured (missing API key)',
    'calendar is not configured',
    'calendar is not configured',
    'calendar is not configured',
    'books library is not configured',
  ]
  const allCells = [
    cell('movie', 'trending', reasons[0]),
    cell('tv', 'trending', reasons[1]),
    cell('music', 'trending', reasons[2]),
    cell('book', 'trending', reasons[3]),
    cell('movie', 'new_releases', reasons[4]),
    cell('tv', 'new_releases', reasons[5]),
    cell('music', 'new_releases', reasons[6]),
    cell('book', 'new_releases', reasons[7]),
    cell('movie', 'upcoming', reasons[8]),
    cell('tv', 'upcoming', reasons[9]),
    cell('music', 'upcoming', reasons[10]),
    cell('book', 'upcoming', reasons[11]),
  ]

  it('a kind-filtered feed row claims only its own kind cell', () => {
    const byId = (id: string) => DISCOVER_ROWS.find((r) => r.id === id)
    expect(cellsForRow(byId('popularMovies')!, allCells).map((c) => c.cell)).toEqual([
      'movie:new_releases',
    ])
    expect(cellsForRow(byId('upcomingSeries')!, allCells).map((c) => c.cell)).toEqual([
      'tv:upcoming',
    ])
  })

  it('the mixed trending row claims all four kinds of its row, and nothing else', () => {
    const trending = DISCOVER_ROWS.find((r) => r.id === 'trending')!
    expect(cellsForRow(trending, allCells).map((c) => c.cell)).toEqual([
      'movie:trending',
      'tv:trending',
      'music:trending',
      'book:trending',
    ])
  })

  it('non-feed rows claim nothing', () => {
    const watchlist = DISCOVER_ROWS.find((r) => r.id === 'watchlist')!
    expect(cellsForRow(watchlist, allCells)).toEqual([])
  })

  it('unclaimedCells are exactly the unrendered music/book cells of the split rows', () => {
    expect(unclaimedCells(DISCOVER_ROWS, allCells).map((c) => c.cell)).toEqual([
      'music:new_releases',
      'book:new_releases',
      'music:upcoming',
      'book:upcoming',
    ])
  })
})

// ---- feed item splits -------------------------------------------------------------

describe('feedItemsForRow', () => {
  const page = discoverPage({
    trending: [
      { kind: 'movie', sourceId: '603', title: 'Dune', year: 2024, subtitle: '', posterUrl: '', date: '' },
      { kind: 'tv', sourceId: '81858', title: 'Severance', year: 2025, subtitle: 'S2', posterUrl: '', date: '' },
    ],
    upcoming: [
      { kind: 'movie', sourceId: '42', title: 'Monitored Movie', year: 0, subtitle: '', posterUrl: '', date: '' },
      { kind: 'tv', sourceId: '77', title: 'Monitored Show', year: 0, subtitle: 'S1E1', posterUrl: '', date: '' },
      { kind: 'book', sourceId: '55', title: 'Book', year: 0, subtitle: '', posterUrl: '', date: '' },
    ],
    newReleases: [
      { kind: 'movie', sourceId: '909', title: 'Now Playing', year: 2026, subtitle: '', posterUrl: '', date: '' },
      { kind: 'tv', sourceId: '33', title: 'New Season', year: 2026, subtitle: '', posterUrl: '', date: '' },
    ],
  })
  const byId = (id: string) => DISCOVER_ROWS.find((r) => r.id === id)!

  it('returns the kind-filtered items of the feed, in wire order', () => {
    expect(feedItemsForRow(byId('popularMovies'), page).map((i) => i.sourceId)).toEqual(['909'])
    expect(feedItemsForRow(byId('upcomingSeries'), page).map((i) => i.sourceId)).toEqual(['77'])
  })

  it('returns the whole mixed row for trending', () => {
    expect(feedItemsForRow(byId('trending'), page).map((i) => i.sourceId)).toEqual(['603', '81858'])
  })

  it('returns [] for non-feed rows', () => {
    expect(feedItemsForRow(byId('studios'), page)).toEqual([])
  })
})

// ---- Recently Added ----------------------------------------------------------------

describe('buildRecentlyAdded', () => {
  it('merges all four kinds, newest AddedAt first, capped at 12', () => {
    const mk = (n: number, at: string) => movie({ ID: n, Title: `M${n}`, AddedAt: at })
    const movies = [mk(1, '2026-11-10T00:00:00Z'), mk(2, '2026-11-09T00:00:00Z')]
    const seriesList = [series({ ID: 10, Title: 'S10', AddedAt: '2026-11-12T00:00:00Z' })]
    const artists = [artist({ ID: 100, Name: 'Artist100', AddedAt: '2026-11-11T00:00:00Z' })]
    const books = [book({ ID: 1000, Name: 'Book1000', AddedAt: '2026-11-08T00:00:00Z' })]

    const cards = buildRecentlyAdded(movies, seriesList, artists, books)
    expect(cards.map((c) => `${c.kind}:${c.id}`)).toEqual([
      'tv:10',
      'music:100',
      'movie:1',
      'movie:2',
      'book:1000',
    ])
  })

  it('caps at 12 across kinds', () => {
    const movies = Array.from({ length: 15 }, (_, i) =>
      movie({ ID: i + 1, Title: `M${i}`, AddedAt: `2026-11-${String(15 - i).padStart(2, '0')}T00:00:00Z` }),
    )
    const cards = buildRecentlyAdded(movies, [series({ ID: 50, Title: 'S50' })], [], [])
    expect(cards).toHaveLength(12)
  })

  it('carries kind-appropriate targets, meta, and art', () => {
    const cards = buildRecentlyAdded(
      [movie({ ID: 42, Title: 'Movie', Year: 2024, PosterURL: 'https://img/m.jpg' })],
      [series({ ID: 7, Title: 'Show', Year: 2025 })],
      [artist({ ID: 9, Name: 'Band', Genre: 'Rock' })],
      [book({ ID: 55, Name: 'A Book', AuthorName: 'An Author', PosterURL: 'https://img/b.jpg' })],
    )
    expect(cards[0]).toMatchObject({
      kind: 'movie',
      id: 42,
      title: 'Movie',
      meta: '2024',
      posterUrl: 'https://img/m.jpg',
      target: '/movies/42',
    })
    expect(cards[1]).toMatchObject({ kind: 'tv', target: '/tv/7' })
    expect(cards[2]).toMatchObject({ kind: 'music', target: '/music/9', meta: 'Rock' })
    expect(cards[3]).toMatchObject({
      kind: 'book',
      target: null, // no title-level book detail route
      meta: 'An Author',
      posterUrl: 'https://img/b.jpg',
    })
  })

  it('is deterministic on ties (kind order, then id)', () => {
    const at = '2026-11-01T00:00:00Z'
    const a = buildRecentlyAdded(
      [movie({ ID: 2, Title: 'M2', AddedAt: at }), movie({ ID: 1, Title: 'M1', AddedAt: at })],
      [series({ ID: 5, Title: 'S5', AddedAt: at })],
      [],
      [],
    )
    expect(a.map((c) => `${c.kind}:${c.id}`)).toEqual(['movie:1', 'movie:2', 'tv:5'])
  })
})

// ---- Watchlist ---------------------------------------------------------------------

describe('buildWatchlist', () => {
  it('groups by kind (movie, tv, music, book) and keeps the server order within each kind', () => {
    const cards = buildWatchlist(
      [movie({ ID: 2, Title: 'M2' }), movie({ ID: 1, Title: 'M1' })],
      [series({ ID: 5, Title: 'S5' })],
      [artist({ ID: 9, Name: 'A9' })],
      [book({ ID: 55, Name: 'B55' })],
    )
    expect(cards.map((c) => `${c.kind}:${c.id}`)).toEqual(['movie:2', 'movie:1', 'tv:5', 'music:9', 'book:55'])
    // The same targets as Recently Added (local ids; books inert).
    expect(cards.map((c) => c.target)).toEqual(['/movies/2', '/movies/1', '/tv/5', '/music/9', null])
  })

  it('empty libraries produce an empty row (the page shows the EmptyState)', () => {
    expect(buildWatchlist([], [], [], [])).toEqual([])
  })
})

// ---- Genres / Studios facets ---------------------------------------------------------

describe('topGenres / topStudios', () => {
  const movies = [
    movie({ ID: 1, Title: 'A', Genres: ['Action', 'Drama'] }),
    movie({ ID: 2, Title: 'B', Genres: ['action', 'Sci-Fi'] }),
    movie({ ID: 3, Title: 'C', Genres: ['Drama', 'Sci-Fi'] }),
    movie({ ID: 4, Title: 'D', Studio: 'Studio X' }),
    movie({ ID: 5, Title: 'E', Studio: 'Studio X' }),
    movie({ ID: 6, Title: 'F', Studio: 'Studio Y' }),
    movie({ ID: 7, Title: 'G', Studio: null }),
    movie({ ID: 8, Title: 'H', Studio: '' }),
  ]

  it('counts genres case-insensitively and returns the top n by (count desc, name asc)', () => {
    expect(topGenres(movies, 8)).toEqual([
      { name: 'Action', count: 2 },
      { name: 'Drama', count: 2 },
      { name: 'Sci-Fi', count: 2 },
    ])
  })

  it('caps at n and keeps the ordering stable', () => {
    expect(topGenres(movies, 2)).toEqual([
      { name: 'Action', count: 2 },
      { name: 'Drama', count: 2 },
    ])
  })

  it('topStudios ignores null/empty and counts occurrences', () => {
    expect(topStudios(movies, 8)).toEqual([
      { name: 'Studio X', count: 2 },
      { name: 'Studio Y', count: 1 },
    ])
    expect(topStudios([], 8)).toEqual([])
    expect(topGenres([], 8)).toEqual([])
  })
})

// ---- Recent activity (landscape cards) --------------------------------------------------

describe('mergeRecentActivity', () => {
  it('merges queue + history, newest first, capped at 10', () => {
    const d = dash({
      recent_queue: [
        { kind: 'movie', release_title: 'Q Movie', indexer: 'NZB', state: 'queued', progress: 0, updated_at: '2026-11-27T10:00:00Z' },
        { kind: 'tv', release_title: 'Q TV', indexer: '', state: 'complete', progress: 100, updated_at: '2026-11-27T11:00:00Z' },
      ],
      recent_history: [
        { kind: 'music', name: 'H Music', event: 'download', detail: '', at: '2026-11-27T12:00:00Z' },
        { kind: 'book', name: 'H Book', event: 'added', detail: '', at: '2026-11-26T12:00:00Z' },
      ],
    })
    const cards = mergeRecentActivity(d)
    expect(cards.map((c) => c.title)).toEqual(['H Music', 'Q TV', 'Q Movie', 'H Book'])
    expect(cards[0]).toMatchObject({
      kind: 'music',
      status: 'download',
      statusTone: 'info',
      source: '',
    })
    expect(cards[1]).toMatchObject({ kind: 'tv', status: 'complete', statusTone: 'success', source: '' })
    expect(cards[2]).toMatchObject({ kind: 'movie', status: 'queued', statusTone: 'warning', source: 'NZB' })
  })

  it('caps at 10 entries', () => {
    const d = dash({
      recent_queue: Array.from({ length: 8 }, (_, i) => ({
        kind: 'movie' as const,
        release_title: `Q${i}`,
        indexer: '',
        state: 'queued' as const,
        progress: 0,
        updated_at: `2026-11-2${i % 7}T00:00:00Z`,
      })),
      recent_history: Array.from({ length: 6 }, (_, i) => ({
        kind: 'tv' as const,
        name: `H${i}`,
        event: 'download',
        detail: '',
        at: `2026-11-2${(i + 3) % 7}T01:00:00Z`,
      })),
    })
    expect(mergeRecentActivity(d)).toHaveLength(10)
  })

  it('returns [] when the dashboard is missing (the row degrades to a note)', () => {
    expect(mergeRecentActivity(null)).toEqual([])
  })

  it('queueTone maps the wire states to semantic tones', () => {
    expect(queueTone('queued')).toBe('warning')
    expect(queueTone('downloading')).toBe('accent')
    expect(queueTone('complete')).toBe('success')
    expect(queueTone('failed')).toBe('error')
    expect(queueTone('something-else')).toBe('neutral')
  })
})

// ---- targets ------------------------------------------------------------------------------

describe('targets', () => {
  it('libraryTarget: the three kind detail routes; books are inert (no title-level route)', () => {
    expect(libraryTarget('movie', 42)).toBe('/movies/42')
    expect(libraryTarget('tv', 77)).toBe('/tv/77')
    expect(libraryTarget('music', 9)).toBe('/music/9')
    expect(libraryTarget('book', 55)).toBeNull()
  })

  it('feedTarget: upcoming rows deep-link on local ids; movie feed items link to the add flow with ?tmdb=', () => {
    const byId = (id: string) => DISCOVER_ROWS.find((r) => r.id === id)!
    const item = (kind: 'movie' | 'tv' | 'music' | 'book', sourceId: string) => ({
      kind,
      sourceId,
      title: 'T',
      year: 0,
      subtitle: '',
      posterUrl: '',
      date: '',
    })
    expect(feedTarget(byId('upcomingMovies'), item('movie', '42'))).toBe('/movies/42')
    expect(feedTarget(byId('upcomingSeries'), item('tv', '77'))).toBe('/tv/77')
    expect(feedTarget(byId('upcomingSeries'), item('book', '55'))).toBeNull()
    expect(feedTarget(byId('trending'), item('movie', '603'))).toBe('/movies/add?tmdb=603')
    expect(feedTarget(byId('trending'), item('movie', 'not-a-number'))).toBeNull()
    expect(feedTarget(byId('trending'), item('tv', '81858'))).toBeNull()
    expect(feedTarget(byId('popularMovies'), item('tv', '33'))).toBeNull()
  })
})
