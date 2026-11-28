// #151: the Discover cinematic row set — pure row model.
//
// TDD: the contract is defined by model.test.ts (RED → GREEN). All
// functions here are pure and deterministic; the React components
// (components/discover/*) and the page (pages/Discover.tsx) compose them.
//
// Wire surface consumed (no API changes): GET /api/v1/discover,
// GET /api/v1/dashboard?recent=20, and the four list endpoints with the
// #150 sort allowlists (all four kinds accept "added-desc" server-side;
// books are a full array — client sort by AddedAt).
import type {
  Artist,
  Book,
  Dashboard,
  DiscoverCellError,
  DiscoverItem,
  DiscoverPage,
  Movie,
  Series,
} from '../../api/types'

export const DISCOVER_KINDS = ['movie', 'tv', 'music', 'book'] as const
export type DiscoverKind = (typeof DISCOVER_KINDS)[number]

export const DISCOVER_KIND_LABEL: Record<DiscoverKind, string> = {
  movie: 'Movies',
  tv: 'TV',
  music: 'Music',
  book: 'Books',
}

/** One row of the page, in render order. The KPI strip is not a row. */
export interface DiscoverRowDef {
  id: string
  label: string
  /** Feed rows only: the server cell-row key (unavailable cells are "<kind>:<row>"). */
  cellRow?: 'trending' | 'upcoming' | 'new_releases'
  /** Kind-filtered feed rows only: the single kind the row renders. */
  kind?: DiscoverKind
}

// The mapped reference inventory (issue #151; the Seerr slider set adapted
// to Mediarr's library data). RECORDED DECISIONS: "Series Genres" and
// "Networks" are omitted (the Series wire schema has no genre/network
// fields and #151 forbids API changes), and there are no dedicated
// music/book feed rows (the reference inventory is TV/movies-only; the
// music/book cells stay in the API and remain visible through the mixed
// Trending row, Recently Added, and Watchlist).
export const DISCOVER_ROWS: DiscoverRowDef[] = [
  { id: 'recentlyAdded', label: 'Recently Added' },
  { id: 'recentActivity', label: 'Recent Activity' },
  { id: 'trending', label: 'Trending', cellRow: 'trending' },
  { id: 'popularMovies', label: 'Popular Movies', cellRow: 'new_releases', kind: 'movie' },
  { id: 'popularSeries', label: 'Popular Series', cellRow: 'new_releases', kind: 'tv' },
  { id: 'upcomingMovies', label: 'Upcoming Movies', cellRow: 'upcoming', kind: 'movie' },
  { id: 'upcomingSeries', label: 'Upcoming Series', cellRow: 'upcoming', kind: 'tv' },
  { id: 'watchlist', label: 'Your Watchlist' },
  { id: 'movieGenres', label: 'Movie Genres' },
  { id: 'studios', label: 'Studios' },
]

/** One library item in a portrait cell (recently added / watchlist rows). */
export interface LibraryCard {
  kind: DiscoverKind
  /** Local library id (the wire ids of the list endpoints). */
  id: number
  title: string
  year: number
  /** One-line meta under the title (year, genre, author). '' when n/a. */
  meta: string
  /** Poster / cover URL; '' when unknown (the cell renders initials). */
  posterUrl: string
  /** Navigation target, or null (books: no title-level detail route). */
  target: string | null
}

/** One landscape recent-activity card (the absorbed Dashboard feeds). */
export interface ActivityCard {
  key: string
  kind: DiscoverKind
  title: string
  /** Queue state or history event label. */
  status: string
  statusTone: 'neutral' | 'accent' | 'info' | 'success' | 'warning' | 'error'
  /** RFC3339 instant ('' when unknown). */
  date: string
  /** Indexer name for queue cards; '' otherwise. */
  source: string
}

/** One facet tile (genre / studio). */
export interface Facet {
  name: string
  count: number
}

// ---- degradation (acceptance-critical) ----------------------------------------
//
// ONE deterministic mapper: DiscoverCellError → a friendly, styled note.
// The raw wire reason ("tmdb is not configured (missing API key)", a
// pass-through provider error, …) NEVER reaches the DOM. The known
// not-configured reasons come from the server (internal/services/discover/
// discover.go); anything else gets the generic note.

const FRIENDLY_NOTES: Record<string, string> = {
  'tmdb is not configured (missing API key)':
    'TMDB is not configured — add an API key in Settings to enable this feed.',
  'google books is not configured (missing API key)':
    'Google Books is not configured — add an API key in Settings to enable this feed.',
  'musicbrainz is not configured': 'MusicBrainz is not configured — enable it in Settings to see music here.',
  'openlibrary is not configured': 'Open Library is not configured — enable it in Settings to see books here.',
  // The not-configured notes must never contain the raw wire reason as a
  // substring ("calendar is not configured" / "books library is not
  // configured" have no "(missing API key)" marker, so word them around it).
  'calendar is not configured':
    'The calendar has not been set up yet — dates appear here once it is enabled in Settings.',
  'books library is not configured':
    'The books library has not been set up yet — books appear here once it is enabled in Settings.',
}

const GENERIC_CELL_NOTE = 'This feed is temporarily unavailable. If it keeps happening, check Settings.'

/**
 * The friendly note for one unavailable cell. Known not-configured reasons
 * are feed-independent (the same note on every cell of that feed), so they
 * render WITHOUT a kind prefix; the generic note (an unknown pass-through
 * reason) identifies the feed with the kind label prefix.
 */
export function cellNote(cell: DiscoverCellError): string {
  const note = FRIENDLY_NOTES[cell.reason] ?? GENERIC_CELL_NOTE
  if (note !== GENERIC_CELL_NOTE) return note
  const [kind] = cell.cell.split(':')
  const label = DISCOVER_KIND_LABEL[kind as DiscoverKind]
  return label ? `${label}: ${note}` : note
}

/**
 * The row's deduped friendly notes, in cell order. A row whose cells are
 * all unavailable shows these grey-out notes in place of an EmptyState.
 */
export function rowNotes(cells: DiscoverCellError[]): string[] {
  const seen = new Set<string>()
  const out: string[] = []
  for (const c of cells) {
    const note = cellNote(c)
    if (!seen.has(note)) {
      seen.add(note)
      out.push(note)
    }
  }
  return out
}

// ---- cell claiming per row -----------------------------------------------------

/**
 * The unavailable cells this row claims, by exact (kind, row) match: a
 * kind-filtered feed row claims only its own kind; the mixed trending row
 * claims all four kinds of its row; non-feed rows claim nothing.
 */
export function cellsForRow(row: DiscoverRowDef, cells: DiscoverCellError[]): DiscoverCellError[] {
  if (!row.cellRow) return []
  return cells.filter((c) => {
    const [kind, r] = c.cell.split(':')
    if (r !== row.cellRow) return false
    if (row.kind) return kind === row.kind
    return (DISCOVER_KINDS as readonly string[]).includes(kind)
  })
}

/**
 * The unavailable cells no row renders (the unrendered music/book cells of
 * the split rows, per the recorded deviations). The page surfaces their
 * friendly notes in one muted block so no degradation information is lost.
 */
export function unclaimedCells(rows: DiscoverRowDef[], cells: DiscoverCellError[]): DiscoverCellError[] {
  return cells.filter((c) => !rows.some((r) => r.cellRow !== undefined && cellsForRow(r, [c]).length > 0))
}

// ---- feed item splits ------------------------------------------------------------

const CELL_ROW_FIELD: Record<NonNullable<DiscoverRowDef['cellRow']>, 'trending' | 'upcoming' | 'newReleases'> = {
  trending: 'trending',
  upcoming: 'upcoming',
  new_releases: 'newReleases',
}

/** The feed items a row renders (kind-filtered for the split rows). */
export function feedItemsForRow(row: DiscoverRowDef, page: DiscoverPage): DiscoverItem[] {
  if (!row.cellRow) return []
  const items = page[CELL_ROW_FIELD[row.cellRow]]
  return row.kind ? items.filter((i) => i.kind === row.kind) : items
}

// ---- feed item meta -------------------------------------------------------------

// feedItemMeta composes a feed card's second line from the item's one-line
// subtitle (artist / author / studio / episode SxE) and, when known, the
// year. Every field may be missing (year 0, subtitle '') — the join keeps
// no stray separators. For items that carry a date (upcoming rows) the
// human date leads; the bare year is then redundant and is left out.
export function feedItemMeta(item: DiscoverItem): string {
  const parts: string[] = []
  const date = dateLabel(item.date)
  if (date) parts.push(date)
  if (item.subtitle) parts.push(item.subtitle)
  if (!date && item.year > 0) parts.push(String(item.year))
  return parts.join(' · ')
}

// dateLabel is the human date for an RFC3339 instant (the upcoming rows'
// air/release starts); '' when the item carries none (partial dates) or
// the value is not a parseable date (the UI must never crash on either).
export function dateLabel(iso: string): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  return d.toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' })
}

// ---- navigation (no invented routes) ----------------------------------------------

/** The library detail routes; books are inert (the /books/:id route is the author page). */
export function libraryTarget(kind: DiscoverKind, id: number): string | null {
  switch (kind) {
    case 'movie':
      return `/movies/${id}`
    case 'tv':
      return `/tv/${id}`
    case 'music':
      return `/music/${id}`
    case 'book':
      return null
  }
}

/**
 * A feed card's navigation target:
 *   upcoming rows carry LOCAL library ids → the kind's detail route
 *     (books stay inert — title ids, no title-level route);
 *   feed rows carry EXTERNAL ids → only the Movies add flow accepts one
 *     (the ?tmdb=<numeric> preselect); every other kind's add flow is a
 *     manual search, so the card is inert.
 */
export function feedTarget(row: DiscoverRowDef, item: DiscoverItem): string | null {
  if (row.cellRow === 'upcoming') {
    const id = Number(item.sourceId)
    return Number.isFinite(id) && id > 0 ? libraryTarget(item.kind, id) : null
  }
  if (item.kind === 'movie' && /^\d+$/.test(item.sourceId)) {
    return `/movies/add?tmdb=${item.sourceId}`
  }
  return null
}

// ---- Recently Added ----------------------------------------------------------------

const RECENTLY_ADDED_CAP = 12

// The kind tie-break order (the documented deterministic kind order).
const KIND_ORDER: Record<DiscoverKind, number> = { movie: 0, tv: 1, music: 2, book: 3 }

interface DatedCard extends LibraryCard {
  at: number
}

function cardFor(kind: DiscoverKind, raw: {
  id: number
  title: string
  year: number
  meta: string
  posterUrl: string
  addedAt: string
}): DatedCard {
  const t = Date.parse(raw.addedAt)
  return {
    kind,
    id: raw.id,
    title: raw.title,
    year: raw.year,
    meta: raw.meta,
    posterUrl: raw.posterUrl,
    target: libraryTarget(kind, raw.id),
    at: Number.isNaN(t) ? 0 : t,
  }
}

/**
 * The Recently Added row: the top ~12 library items across all four kinds,
 * newest AddedAt first. Each kind is pre-sorted server-side with
 * "added-desc" (all four list endpoints accept it), so page 1 is the
 * kind's newest items; the merge re-sorts across kinds. Books have no
 * title-level detail route, so their cards are inert.
 */
export function buildRecentlyAdded(
  movies: Movie[],
  series: Series[],
  artists: Artist[],
  books: Book[],
): LibraryCard[] {
  const cards: DatedCard[] = []
  for (const m of movies) {
    cards.push(
      cardFor('movie', {
        id: m.ID,
        title: m.Title,
        year: m.Year,
        meta: m.Year > 0 ? String(m.Year) : '',
        posterUrl: m.PosterURL ?? '',
        addedAt: m.AddedAt,
      }),
    )
  }
  for (const s of series) {
    cards.push(
      cardFor('tv', {
        id: s.ID,
        title: s.Title,
        year: s.Year,
        meta: s.Year > 0 ? String(s.Year) : '',
        posterUrl: s.PosterURL ?? '',
        addedAt: s.AddedAt,
      }),
    )
  }
  for (const a of artists) {
    cards.push(
      cardFor('music', {
        id: a.ID,
        title: a.Name,
        year: 0,
        meta: a.Genre ?? '',
        posterUrl: a.PosterURL ?? '',
        addedAt: a.AddedAt,
      }),
    )
  }
  for (const b of books) {
    cards.push(
      cardFor('book', {
        id: b.ID,
        title: b.Name,
        year: 0,
        meta: b.AuthorName ?? '',
        posterUrl: b.PosterURL ?? '',
        addedAt: b.AddedAt,
      }),
    )
  }
  cards.sort(
    (a, b) =>
      b.at - a.at ||
      KIND_ORDER[a.kind] - KIND_ORDER[b.kind] ||
      a.id - b.id,
  )
  return cards.slice(0, RECENTLY_ADDED_CAP).map(({ at: _at, ...c }) => c)
}

// ---- Watchlist -----------------------------------------------------------------------

/**
 * The Your Watchlist row: the monitored library items (the reference
 * watchlist is a Plex concept, redefined here as monitored items),
 * grouped by kind in the deterministic kind order, each kind keeping the
 * server order (the list requests sort "added-desc").
 */
export function buildWatchlist(
  movies: Movie[],
  series: Series[],
  artists: Artist[],
  books: Book[],
): LibraryCard[] {
  const cards: LibraryCard[] = []
  for (const m of movies) {
    cards.push({
      kind: 'movie',
      id: m.ID,
      title: m.Title,
      year: m.Year,
      meta: m.Year > 0 ? String(m.Year) : '',
      posterUrl: m.PosterURL ?? '',
      target: libraryTarget('movie', m.ID),
    })
  }
  for (const s of series) {
    cards.push({
      kind: 'tv',
      id: s.ID,
      title: s.Title,
      year: s.Year,
      meta: s.Year > 0 ? String(s.Year) : '',
      posterUrl: s.PosterURL ?? '',
      target: libraryTarget('tv', s.ID),
    })
  }
  for (const a of artists) {
    cards.push({
      kind: 'music',
      id: a.ID,
      title: a.Name,
      year: 0,
      meta: a.Genre ?? '',
      posterUrl: a.PosterURL ?? '',
      target: libraryTarget('music', a.ID),
    })
  }
  for (const b of books) {
    cards.push({
      kind: 'book',
      id: b.ID,
      title: b.Name,
      year: 0,
      meta: b.AuthorName ?? '',
      posterUrl: b.PosterURL ?? '',
      target: libraryTarget('book', b.ID),
    })
  }
  return cards
}

// ---- Genres / Studios facets -------------------------------------------------------------

const topFacets = (entries: [string, number][], n: number): Facet[] =>
  entries
    .sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
    .slice(0, n)
    .map(([name, count]) => ({ name, count }))

/**
 * Top n genres by item count across the given (page-1) movies. Genres are
 * case-insensitive (TMDB emits "Action"; a hand-entered title might not
 * match); ties break alphabetically so the row is deterministic.
 */
export function topGenres(movies: Movie[], n = 8): Facet[] {
  const counts = new Map<string, number>()
  for (const m of movies) {
    for (const g of m.Genres ?? []) {
      const key = g.trim()
      if (!key) continue
      counts.set(key.toLowerCase(), (counts.get(key.toLowerCase()) ?? 0) + 1)
    }
  }
  // Rebuild display names from the first occurrence (keeps the casing the
  // provider emits).
  const names = new Map<string, string>()
  for (const m of movies) {
    for (const g of m.Genres ?? []) {
      const key = g.trim().toLowerCase()
      if (key && !names.has(key)) names.set(key, g.trim())
    }
  }
  return topFacets(
    [...counts.entries()].map(([key, count]) => [names.get(key) ?? key, count] as [string, number]),
    n,
  )
}

/** Top n studios from the given (page-1) movies (Movie.Studio; null/'' ignored). */
export function topStudios(movies: Movie[], n = 8): Facet[] {
  const counts = new Map<string, number>()
  for (const m of movies) {
    const s = (m.Studio ?? '').trim()
    if (!s) continue
    counts.set(s, (counts.get(s) ?? 0) + 1)
  }
  return topFacets([...counts.entries()], n)
}

// ---- Recent activity (the absorbed Dashboard feeds) ---------------------------------------

const ACTIVITY_CAP = 10

export function queueTone(state: string): 'neutral' | 'accent' | 'info' | 'success' | 'warning' | 'error' {
  switch (state) {
    case 'queued':
      return 'warning'
    case 'downloading':
      return 'accent'
    case 'complete':
      return 'success'
    case 'failed':
      return 'error'
    default:
      return 'neutral'
  }
}

/**
 * The Recent Activity row: the dashboard's recent queue + recent history
 * merged, newest first, capped at 10 (the reference "Recent Requests"
 * landscape cards). History events render with an info pill and no source
 * (the wire types carry only the indexer on queue items).
 */
export function mergeRecentActivity(dash: Dashboard | null): ActivityCard[] {
  if (!dash) return []
  interface Dated extends ActivityCard {
    at: number
  }
  const cards: Dated[] = []
  dash.recent_queue.forEach((q, i) => {
    const t = Date.parse(q.updated_at)
    cards.push({
      key: `queue-${i}`,
      kind: q.kind,
      title: q.release_title,
      status: q.state,
      statusTone: queueTone(q.state),
      date: q.updated_at,
      source: q.indexer ?? '',
      at: Number.isNaN(t) ? 0 : t,
    })
  })
  dash.recent_history.forEach((h, i) => {
    const t = Date.parse(h.at)
    cards.push({
      key: `history-${i}`,
      kind: h.kind,
      title: h.name,
      status: h.event,
      statusTone: 'info',
      date: h.at,
      source: '',
      at: Number.isNaN(t) ? 0 : t,
    })
  })
  cards.sort((a, b) => b.at - a.at || a.key.localeCompare(b.key))
  return cards.slice(0, ACTIVITY_CAP).map(({ at: _at, ...c }) => c)
}
