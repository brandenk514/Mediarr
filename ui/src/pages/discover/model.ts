// #151: the Discover cinematic row set — pure row model.
//
// TDD RED slice: the contract is defined by model.test.ts; the helpers
// below are stubs until the GREEN slice. (Row model, degradation mapper,
// grouping.) All functions here are pure and deterministic — the React
// components (components/discover/*) compose them.
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

// ---- implementations (GREEN slice) ---------------------------------------------

const NOT_IMPLEMENTED = () => {
  throw new Error('not implemented (RED slice)')
}

export function cellNote(_cell: DiscoverCellError): string {
  return NOT_IMPLEMENTED()
}

export function rowNotes(_cells: DiscoverCellError[]): string[] {
  return NOT_IMPLEMENTED()
}

export function cellsForRow(_row: DiscoverRowDef, _cells: DiscoverCellError[]): DiscoverCellError[] {
  return NOT_IMPLEMENTED()
}

export function unclaimedCells(_rows: DiscoverRowDef[], _cells: DiscoverCellError[]): DiscoverCellError[] {
  return NOT_IMPLEMENTED()
}

export function feedItemsForRow(_row: DiscoverRowDef, _page: DiscoverPage): DiscoverItem[] {
  return NOT_IMPLEMENTED()
}

export function libraryTarget(_kind: DiscoverKind, _id: number): string | null {
  return NOT_IMPLEMENTED()
}

export function feedTarget(_row: DiscoverRowDef, _item: DiscoverItem): string | null {
  return NOT_IMPLEMENTED()
}

export function buildRecentlyAdded(
  _movies: Movie[],
  _series: Series[],
  _artists: Artist[],
  _books: Book[],
): LibraryCard[] {
  return NOT_IMPLEMENTED()
}

export function buildWatchlist(
  _movies: Movie[],
  _series: Series[],
  _artists: Artist[],
  _books: Book[],
): LibraryCard[] {
  return NOT_IMPLEMENTED()
}

export function topGenres(_movies: Movie[], _n?: number): Facet[] {
  return NOT_IMPLEMENTED()
}

export function topStudios(_movies: Movie[], _n?: number): Facet[] {
  return NOT_IMPLEMENTED()
}

export function mergeRecentActivity(_dash: Dashboard | null): ActivityCard[] {
  return NOT_IMPLEMENTED()
}

export function queueTone(_state: string): 'neutral' | 'accent' | 'info' | 'success' | 'warning' | 'error' {
  return NOT_IMPLEMENTED()
}

// Keep the imports live while the stubs are in place (the GREEN slice uses
// them all).
export type { Dashboard }
