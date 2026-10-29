// Navigation model for the Sonarr-parity shell (P1).
//
// Mediarr is a four-kind app (TV / Movies / Music / Books) that shares ONE
// Sonarr-shaped shell. The left rail's library section is parameterized by the
// active kind (Series / Movies / Artists / Authors); the other sections
// (Calendar, Activity, Wanted, Settings, System) are shared. This is the single
// source of truth for the rail + the per-kind route paths, so the rail and the
// route tree can't drift.

export type KindId = 'tv' | 'movie' | 'music' | 'book'

export interface KindMeta {
  id: KindId
  /** Label in the top-of-rail kind switcher. */
  label: string
  /** Label of the rail's library section for this kind. */
  libLabel: string
  /** Icon glyph for the library section. */
  libIcon: string
  /** The per-kind list page (the kind's "home"). */
  libPath: string
  /** The per-kind add page. */
  addPath: string
  /** The per-kind library-import page (TV-only in P1). */
  importPath: string | null
  /**
   * Extra first-class sub-links for the kind's library section, rendered
   * after 'Add New' / 'Library Import' (Radarr R3: Movies gets Collections
   * and Discover; the other kinds' rails are untouched).
   */
  extraLinks?: RailLink[]
}

export const KINDS: KindMeta[] = [
  {
    id: 'tv',
    label: 'TV',
    libLabel: 'Series',
    libIcon: '▶',
    libPath: '/tv',
    addPath: '/tv/add',
    importPath: '/tv/import',
  },
  {
    id: 'movie',
    label: 'Movies',
    libLabel: 'Movies',
    libIcon: '🎬',
    libPath: '/movies',
    addPath: '/movies/add',
    importPath: null,
    // Radarr R3: Collections + Discover are first-class movie sub-links.
    // Radarr R4: the movie calendar (Radarr v6: Movies ▸ Calendar).
    extraLinks: [
      { to: '/movies/collections', label: 'Collections' },
      { to: '/movies/discover', label: 'Discover' },
      { to: '/movies/calendar', label: 'Calendar' },
    ],
  },
  {
    id: 'music',
    label: 'Music',
    libLabel: 'Artists',
    libIcon: '♫',
    libPath: '/music',
    addPath: '/music/add',
    importPath: null,
  },
  {
    id: 'book',
    label: 'Books',
    libLabel: 'Authors',
    libIcon: '📖',
    libPath: '/books',
    addPath: '/books/add',
    importPath: null,
  },
]

export function kindById(id: string): KindMeta {
  return KINDS.find((k) => k.id === id) ?? KINDS[0]
}

// A shared (kind-independent) rail entry.
export interface RailLink {
  to: string
  label: string
}

// The shared sections that appear for every kind, in Sonarr's order. The
// library (kind) section is rendered separately above these, with its own
// per-kind label + Add New / Library Import sub-items.
export interface SharedSection {
  id: string
  label: string
  icon: string
  /** The section's default destination (the header row links here). */
  to: string
  /** Route prefix that marks the section active/expanded (e.g. /activity). */
  base: string
  /** Sub-items revealed under the active section. */
  sub: RailLink[]
  /** Optional count badge value (System shows a health-count badge). */
  badge?: number | null
}

// Mediarr's own extra section (not in Sonarr): the cross-library overview.
// R7 home decision — the Movies list is the default landing; the Dashboard
// stays reachable from this rail item (and the /dashboard route).
export const DASHBOARD_SECTION: SharedSection = {
  id: 'dashboard',
  label: 'Dashboard',
  icon: '▦',
  to: '/dashboard',
  base: '/dashboard',
  sub: [],
}

// Prowlarr PR1: Indexers is a first-class hub (the list IS the page), reachable
// from the rail for every kind. Its home is /indexers; the only PR1 sub-item is
// a Stats stub (the real stats page lands in PR3).
export const INDEXERS_SECTION: SharedSection = {
  id: 'indexers',
  label: 'Indexers',
  icon: '🧲',
  to: '/indexers',
  base: '/indexers',
  sub: [{ to: '/indexers/stats', label: 'Stats' }],
}

export const SHARED_SECTIONS: SharedSection[] = [
  { id: 'calendar', label: 'Calendar', icon: '📅', to: '/calendar', base: '/calendar', sub: [] },
  {
    id: 'activity',
    label: 'Activity',
    icon: '⚡',
    to: '/activity/queue',
    base: '/activity',
    sub: [
      { to: '/activity/queue', label: 'Queue' },
      { to: '/activity/history', label: 'History' },
      { to: '/activity/blocklist', label: 'Blocklist' },
    ],
  },
  {
    id: 'wanted',
    label: 'Wanted',
    icon: '⭐',
    to: '/wanted/missing',
    base: '/wanted',
    sub: [
      { to: '/wanted/missing', label: 'Missing' },
      { to: '/wanted/cutoff', label: 'Cutoff Unmet' },
    ],
  },
  { id: 'settings', label: 'Settings', icon: '⚙', to: '/settings', base: '/settings', sub: [] },
  { id: 'system', label: 'System', icon: '🖥', to: '/system', base: '/system', sub: [] },
]
