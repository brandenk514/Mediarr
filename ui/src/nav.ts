// Central navigation model: the single source of truth for both the
// sidebar rail and the route tree. Routes are DERIVED from this module so
// the rail and the router can never drift apart.
//
// Issue #148 (Seerr-shaped shell): the four kinds are TOP-LEVEL sections —
// Discover, Movies, Series, Music, Books — each pointing at its per-kind
// route, with the kind's first-class pages (add, collections, …) as
// sub-links beneath it. Activity precedes Calendar (approved IA).
// Dashboard is dropped (its route redirects to Discover); Indexers leaves
// the rail and is parked as a sub-link under Settings until #154 folds it
// into a Settings tab.
import {
  BookOpenIcon,
  CalendarIcon,
  Cog6ToothIcon,
  FilmIcon,
  MusicalNoteIcon,
  NewspaperIcon,
  ServerIcon,
  SparklesIcon,
  Square2StackIcon,
} from '@heroicons/react/24/outline'
import type { ComponentType, SVGProps } from 'react'

export const PATHS = {
  home: '/',
  // #148: the Dashboard route is kept ONLY as a redirect to the Discover
  // landing (no sidebar section owns it anymore).
  dashboard: '/dashboard',
  // Issue #118: the unified cross-kind landing (the new default home).
  discover: '/discover',
  tv: '/tv',
  movie: '/movies',
  music: '/music',
  book: '/books',
  library: '/library',
  tvAdd: '/tv/add',
  movieAdd: '/movies/add',
  musicAdd: '/music/add',
  bookAdd: '/books/add',
  tvImport: '/tv/import',
  movieCollections: '/movies/collections',
  movieDiscover: '/movies/discover',
  movieCalendar: '/movies/calendar',
  musicUnmapped: '/music/unmapped',
  bookUnmapped: '/books/unmapped',
  bookBookshelves: '/books/bookshelves',
  bookSeries: '/books/series',
  bookSeriesDetail: '/books/series/:id',
  // Detail routes (App.tsx renders these; kept in PATHS so the route table
  // and the nav model share one source of truth).
  tvDetail: '/tv/:id',
  movieDetail: '/movies/:id',
  musicDetail: '/music/:id',
  bookDetail: '/books/:id',
  calendar: '/calendar',
  queue: '/queue',
  activityQueue: '/activity/queue',
  activityHistory: '/activity/history',
  activityBlocklist: '/activity/blocklist',
  wantedMissing: '/wanted/missing',
  wantedCutoff: '/wanted/cutoff',
  search: '/search',
  // Issue #117: the global search results page is a first-class route.
  // The header pill submits here; the URL stays deep-linkable (?q=…).
  globalSearch: '/search/all',
  history: '/history',
  indexers: '/indexers',
  indexerStats: '/indexers/stats',
  settings: '/settings',
  settingsSection: '/settings/:section',
  system: '/system',
  systemPage: '/system/:page',
} as const

export type SectionId =
  | 'discover'
  | 'movie'
  | 'tv'
  | 'music'
  | 'book'
  | 'activity'
  | 'calendar'
  | 'settings'
  | 'system'

export interface NavLink {
  to: string
  label: string
  /**
   * Route prefixes that mark this link as the current area. Required so a
   * parent (e.g. Indexers, a Settings sub-link) can reveal its sub-links
   * while a CHILD route is active, not only its exact default route.
   */
  base?: string[]
  /** Nested sub-links (the Prowlarr group under the Settings sub-link). */
  subs?: NavLink[]
}

export interface Section {
  id: SectionId
  label: string
  /** SVG line-icon component (heroicons outline); never glyph characters. */
  icon: ComponentType<SVGProps<SVGSVGElement>>
  to: string
  /**
   * Route prefixes that belong to this section. `to` is always included
   * here (the "base" set), plus any nested routes (e.g. the unified
   * Discover page also owns the kept Movies discover page, issue #118).
   */
  base: string[]
  /**
   * Optional sub-links beneath the section row (a kind's first-class
   * pages, or the parked Indexers group under Settings, issue #148).
   */
  subs?: NavLink[]
}

// Issue #148: icons are heroicons outline components (the legacy
// unicode-glyph/emoji icon set is retired; no glyph characters remain in
// the nav markup). The rail draws them at a fixed 20px; color is
// inherited from the item.
export const SECTIONS: Section[] = [
  {
    // The unified cross-kind landing is the section's home (issue #118).
    // Its base also owns the (kept) Movies discover page: while that page
    // is up the Discover row stays active — its pre-#118 behavior.
    id: 'discover',
    label: 'Discover',
    icon: SparklesIcon,
    to: PATHS.discover,
    base: [PATHS.discover, PATHS.movieDiscover],
  },
  {
    id: 'movie',
    label: 'Movies',
    icon: FilmIcon,
    to: PATHS.movie,
    base: [PATHS.movie],
    subs: [
      { to: PATHS.movieAdd, label: 'Add Movie' },
      { to: PATHS.movieCollections, label: 'Collections' },
      { to: PATHS.movieDiscover, label: 'Discover' },
      { to: PATHS.movieCalendar, label: 'Calendar' },
    ],
  },
  {
    id: 'tv',
    label: 'Series',
    icon: Square2StackIcon,
    to: PATHS.tv,
    base: [PATHS.tv],
    subs: [
      { to: PATHS.tvAdd, label: 'Add Series' },
      { to: PATHS.tvImport, label: 'Import' },
    ],
  },
  {
    id: 'music',
    label: 'Music',
    icon: MusicalNoteIcon,
    to: PATHS.music,
    base: [PATHS.music],
    subs: [{ to: PATHS.musicAdd, label: 'Add Music' }],
  },
  {
    id: 'book',
    label: 'Books',
    icon: BookOpenIcon,
    to: PATHS.book,
    base: [PATHS.book],
    subs: [
      { to: PATHS.bookAdd, label: 'Add Book' },
      { to: PATHS.bookUnmapped, label: 'Unmapped' },
      { to: PATHS.bookBookshelves, label: 'Bookshelves' },
      { to: PATHS.bookSeries, label: 'Series' },
    ],
  },
  {
    id: 'activity',
    label: 'Activity',
    icon: NewspaperIcon,
    to: PATHS.activityQueue,
    base: [PATHS.queue, PATHS.activityQueue, PATHS.activityHistory, PATHS.activityBlocklist],
    subs: [
      { to: PATHS.activityQueue, label: 'Queue' },
      { to: PATHS.activityHistory, label: 'History' },
      { to: PATHS.activityBlocklist, label: 'Blocklist' },
    ],
  },
  {
    id: 'calendar',
    label: 'Calendar',
    icon: CalendarIcon,
    to: PATHS.calendar,
    base: [PATHS.calendar],
  },
  {
    id: 'settings',
    label: 'Settings',
    icon: Cog6ToothIcon,
    to: PATHS.settings,
    // The whole Indexers area keeps highlighting Settings while Indexers is
    // parked there as a sub-link (interim, issue #148).
    base: [PATHS.settings, PATHS.indexers, PATHS.search, PATHS.history],
    subs: [
      // The Indexers section left the rail (issue #148) but must stay
      // navigable: it lives under Settings until #154 folds it into a
      // Settings tab. Its Prowlarr sub-pages follow as nested sub-links.
      {
        to: PATHS.indexers,
        label: 'Indexers',
        base: [PATHS.indexers, PATHS.search, PATHS.history],
        subs: [
          { to: PATHS.search, label: 'Search' },
          { to: PATHS.history, label: 'History' },
          { to: PATHS.indexerStats, label: 'Stats' },
        ],
      },
    ],
  },
  {
    id: 'system',
    label: 'System',
    icon: ServerIcon,
    to: PATHS.system,
    base: [PATHS.system],
  },
]

// Icon note: heroicons outline components above are the ONLY icon source in
// this file (the legacy glyph set is retired, issue #148).

/**
 * Which section owns a given route — the exact-match-then-base logic the
 * rail uses for its active state. A section is active when the path equals
 * one of its `base` entries, or is deeper under one (prefix + '/').
 * `/` maps to no section (App redirects it).
 */
export function sectionForPath(pathname: string): Section | null {
  if (pathname === PATHS.home) return null
  for (const s of SECTIONS) {
    if (s.base.some((b) => pathname === b || pathname.startsWith(`${b}/`))) return s
  }
  return null
}
