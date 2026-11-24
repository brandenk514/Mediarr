// Navigation model for the unified app shell (issue #116).
//
// SINGLE SOURCE OF TRUTH for the shell: the eight sidebar sections
// (Dashboard, Discover, Library, Calendar, Activity, Indexers, Settings,
// System), their routes, and their icons/labels. The rail (pages/Layout.tsx)
// and the route tree (App.tsx, via PATHS) both derive from this file, so the
// rail and the route tree can't drift.
//
// "Kind" (TV / Movies / Music / Books) is a FACET, not a top-level switch:
// the four kind groups are nested under the Library section (the legacy
// kind switcher and kind-parameterized rail are gone). #119: the kind rows
// link to the unified Library grid filtered to that kind (/library?kind=…);
// the per-kind list pages stay fully routed, reachable by direct URL and via
// the deep sub-links.

// ---- route paths -----------------------------------------------------------
// Every route path used by the shell and the route tree lives here, so the
// nav model and App.tsx reference the same strings (no drift).
export const PATHS = {
  home: '/',
  dashboard: '/dashboard',
  // Per-kind library pages (the kind facet under Library).
  tv: '/tv',
  movie: '/movies',
  music: '/music',
  book: '/books',
  // #119: the unified cross-kind Library grid (the Library section home; the
  // per-kind list pages stay reachable by direct URL beneath it).
  library: '/library',
  tvAdd: '/tv/add',
  movieAdd: '/movies/add',
  musicAdd: '/music/add',
  bookAdd: '/books/add',
  tvImport: '/tv/import',
  // Per-kind deep pages.
  tvDetail: '/tv/:id',
  movieDetail: '/movies/:id',
  movieCollections: '/movies/collections',
  movieDiscover: '/movies/discover',
  movieCalendar: '/movies/calendar',
  musicUnmapped: '/music/unmapped',
  musicDetail: '/music/:id',
  bookUnmapped: '/books/unmapped',
  bookBookshelves: '/books/bookshelves',
  bookSeries: '/books/series',
  bookSeriesDetail: '/books/series/:id',
  bookDetail: '/books/:id',
  // Shared sections.
  // #118: the unified cross-kind Discover page — the app's default
  // landing (supersedes the R7 Movies-list landing) and the rail's
  // Discover destination.
  discover: '/discover',
  calendar: '/calendar',
  /** Legacy queue route, kept for existing e2e alongside /activity/queue. */
  queue: '/queue',
  activityQueue: '/activity/queue',
  activityHistory: '/activity/history',
  activityBlocklist: '/activity/blocklist',
  wantedMissing: '/wanted/missing',
  wantedCutoff: '/wanted/cutoff',
  search: '/search',
  // Global cross-kind search results (issue #117). Distinct from `search`
  // (the Indexers manual release-search page): the top-bar global search
  // navigates here with the query in the URL (?q=…), so results are
  // deep-linkable.
  globalSearch: '/search/all',
  history: '/history',
  indexers: '/indexers',
  indexerStats: '/indexers/stats',
  settings: '/settings',
  settingsSection: '/settings/:section',
  system: '/system',
  systemPage: '/system/:page',
} as const

// ---- model -----------------------------------------------------------------

export type SectionId =
  | 'dashboard'
  | 'discover'
  | 'library'
  | 'calendar'
  | 'activity'
  | 'indexers'
  | 'settings'
  | 'system'

export type KindId = 'tv' | 'movie' | 'music' | 'book'

/** A single navigation link (route + label). */
export interface NavLink {
  to: string
  label: string
}

/**
 * One kind group under the Library section (the kind FACET). The group row
 * links to the kind's list page; its sub-links (Add New, Collections, …) are
 * revealed while the route is within `base`.
 */
export interface KindGroup {
  kind: KindId
  label: string
  icon: string
  /** The unified Library grid filtered to this kind (#119). */
  to: string
  /** Route prefix that marks this group active/expanded (e.g. /movies). */
  base: string
  /** First-class sub-links under the kind. */
  sub: NavLink[]
}

/** One sidebar section. */
export interface NavSection {
  id: SectionId
  label: string
  icon: string
  /** The section's default destination (the header row links here). */
  to: string
  /**
   * Route prefixes that mark the section as the current area: its
   * sub-links are revealed while the route is within any of them.
   */
  base: string[]
  /** Flat sub-links (Activity, Indexers). */
  subs?: NavLink[]
  /** Nested groups (the Library section's per-kind groups). */
  // #119: the unified cross-kind Library grid is the Library section's
  // default destination. The kind groups below it become /library?kind=…
  // shortcuts (the unified grid filtered to that kind); their per-kind
  // list + deep pages stay reachable by direct URL and via the sub-links,
  // which are revealed while the route is within `base`.
  groups?: KindGroup[]
}

// The eight sidebar sections of the unified shell, in rail order.
export const SECTIONS: NavSection[] = [
  {
    id: 'dashboard',
    label: 'Dashboard',
    icon: '▦',
    to: PATHS.dashboard,
    base: [PATHS.dashboard],
  },
  {
    // #118: the rail's Discover section links to the unified cross-kind
    // page (/discover, the default landing). The base ALSO covers the
    // Movies discover page (/movies/discover), which stays reachable
    // from the Movies group's sub-links — the section highlights on
    // both routes (its pre-#118 behavior is preserved).
    id: 'discover',
    label: 'Discover',
    icon: '✨',
    to: PATHS.discover,
    base: [PATHS.discover, PATHS.movieDiscover],
  },
  {
    // #119: the unified cross-kind Library grid is the Library section's
    // default destination (supersedes the R7 Movies-list landing). The four
    // kind groups below it become /library?kind=… shortcuts into the grid;
    // their per-kind list + deep pages stay reachable by direct URL and via
    // the sub-links (the per-kind base is unchanged, so deep pages still
    // expand their group).
    id: 'library',
    label: 'Library',
    icon: '📚',
    to: PATHS.library,
    base: [PATHS.library, PATHS.tv, PATHS.movie, PATHS.music, PATHS.book],
    groups: [
      {
        kind: 'tv',
        label: 'TV',
        icon: '▶',
        to: `${PATHS.library}?kind=tv`,
        base: PATHS.tv,
        sub: [
          { to: PATHS.tvAdd, label: 'Add New' },
          { to: PATHS.tvImport, label: 'Library Import' },
        ],
      },
      {
        kind: 'movie',
        label: 'Movies',
        icon: '🎬',
        to: `${PATHS.library}?kind=movie`,
        base: PATHS.movie,
        sub: [
          { to: PATHS.movieAdd, label: 'Add New' },
          { to: PATHS.movieCollections, label: 'Collections' },
          { to: PATHS.movieDiscover, label: 'Discover' },
          { to: PATHS.movieCalendar, label: 'Calendar' },
        ],
      },
      {
        kind: 'music',
        label: 'Music',
        icon: '♫',
        to: `${PATHS.library}?kind=music`,
        base: PATHS.music,
        sub: [{ to: PATHS.musicAdd, label: 'Add New' }],
      },
      {
        kind: 'book',
        label: 'Books',
        icon: '📖',
        to: `${PATHS.library}?kind=book`,
        base: PATHS.book,
        sub: [
          { to: PATHS.bookAdd, label: 'Add New' },
          { to: PATHS.bookUnmapped, label: 'Unmapped Files' },
          { to: PATHS.bookBookshelves, label: 'Bookshelves' },
          { to: PATHS.bookSeries, label: 'Series' },
        ],
      },
    ],
  },
  {
    id: 'calendar',
    label: 'Calendar',
    icon: '📅',
    to: PATHS.calendar,
    base: [PATHS.calendar],
  },
  {
    // /queue (legacy alias, kept for existing e2e) also counts as the
    // current area.
    id: 'activity',
    label: 'Activity',
    icon: '⚡',
    to: PATHS.activityQueue,
    base: [PATHS.activityQueue, PATHS.activityHistory, PATHS.activityBlocklist, PATHS.queue],
    subs: [
      { to: PATHS.activityQueue, label: 'Queue' },
      { to: PATHS.activityHistory, label: 'History' },
      { to: PATHS.activityBlocklist, label: 'Blocklist' },
    ],
  },
  {
    // Prowlarr IA: Indexers → Search → History (manual multi-indexer search
    // and query history fold in as Indexers sub-links); Stats is the
    // Indexers' own sub-page.
    id: 'indexers',
    label: 'Indexers',
    icon: '🧲',
    to: PATHS.indexers,
    base: [PATHS.indexers, PATHS.search, PATHS.history],
    subs: [
      { to: PATHS.search, label: 'Search' },
      { to: PATHS.history, label: 'History' },
      { to: PATHS.indexerStats, label: 'Stats' },
    ],
  },
  {
    id: 'settings',
    label: 'Settings',
    icon: '⚙',
    to: PATHS.settings,
    base: [PATHS.settings],
  },
  {
    id: 'system',
    label: 'System',
    icon: '🖥',
    to: PATHS.system,
    base: [PATHS.system],
  },
]

// ---- helpers -----------------------------------------------------------------

/** Look up a section by id (never undefined: SECTIONS is the full set). */
export function sectionById(id: SectionId): NavSection {
  const s = SECTIONS.find((x) => x.id === id)
  if (!s) throw new Error(`unknown nav section: ${id}`)
  return s
}

function isWithin(pathname: string, base: string): boolean {
  return pathname === base || pathname.startsWith(base + '/')
}

/** The section that owns the current route, or null (e.g. /wanted/*, /login).
 *
 * Precedence: an exact match on a section's default destination wins first;
 * then section bases in rail order. The Discover section's base carries
 * BOTH /discover and the Movies discover page (/movies/discover), so both
 * routes highlight Discover even though the latter is also under the
 * Movies group's base.
 */
export function sectionForPath(pathname: string): NavSection | null {
  // Tolerate a stray query: a destination may carry one (the #119 Library
  // kind shortcuts are /library?kind=…), but the active state depends only on
  // the pathname.
  const path = pathname.split('?')[0]
  for (const s of SECTIONS) {
    if (path === s.to) return s
  }
  for (const s of SECTIONS) {
    if (s.base.some((b) => isWithin(path, b))) return s
  }
  return null
}

/**
 * The kind facet of the current route: which Library kind group the route
 * belongs to (null when the route is not within a kind group).
 */
export function activeKindForPath(pathname: string): KindId | null {
  const library = sectionById('library')
  for (const g of library.groups ?? []) {
    if (isWithin(pathname, g.base)) return g.kind
  }
  return null
}
