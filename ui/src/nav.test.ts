import { describe, expect, it } from 'vitest'
import { PATHS, SECTIONS, sectionForPath } from './nav'

// App-shell nav model (issue #148): the approved Seerr-shaped IA — the four
// kinds are TOP-LEVEL sections (Discover, Movies, Series, Music, Books),
// Activity now precedes Calendar, Dashboard is dropped, and Indexers leaves
// the sidebar as a sub-link under Settings (interim, until #154 folds it in).
describe('nav model — Seerr-shaped shell sections (issue #148)', () => {
  it('defines exactly the nine sidebar sections, in approved rail order', () => {
    expect(SECTIONS.map((s) => s.id)).toEqual([
      'discover',
      'movie',
      'tv',
      'music',
      'book',
      'activity',
      'calendar',
      'settings',
      'system',
    ])
  })

  it('gives every section a label, an SVG icon, a default destination, and bases', () => {
    for (const s of SECTIONS) {
      expect(s.label.length).toBeGreaterThan(0)
      // Icons are SVG line-icon components (heroicons), never glyph strings.
      expect(typeof s.icon).toBe('function')
      expect(s.to.startsWith('/')).toBe(true)
      expect(s.base.length).toBeGreaterThan(0)
    }
  })

  it('derives every nav route from PATHS (rail and route tree cannot drift)', () => {
    const known = new Set<string>(Object.values(PATHS))
    for (const s of SECTIONS) {
      expect(known.has(s.to), `section ${s.id} to=${s.to} not in PATHS`).toBe(true)
      for (const b of s.base) {
        expect(known.has(b), `section ${s.id} base=${b} not in PATHS`).toBe(true)
      }
      for (const l of s.subs ?? []) {
        expect(known.has(l.to), `section ${s.id} sub ${l.to} not in PATHS`).toBe(true)
        for (const b of l.base ?? []) {
          expect(known.has(b), `section ${s.id} sub ${l.to} base ${b} not in PATHS`).toBe(true)
        }
        for (const l2 of l.subs ?? []) {
          expect(known.has(l2.to), `section ${s.id} nested sub ${l2.to} not in PATHS`).toBe(true)
        }
      }
    }
  })

  it('makes the four kinds TOP-LEVEL sections pointing at the per-kind routes', () => {
    const byId = (id: string) => SECTIONS.find((s) => s.id === id)
    expect(byId('movie')?.to).toBe(PATHS.movie)
    expect(byId('tv')?.to).toBe(PATHS.tv)
    expect(byId('music')?.to).toBe(PATHS.music)
    expect(byId('book')?.to).toBe(PATHS.book)
    expect(byId('movie')?.base).toEqual([PATHS.movie])
    expect(byId('tv')?.base).toEqual([PATHS.tv])
    expect(byId('music')?.base).toEqual([PATHS.music])
    expect(byId('book')?.base).toEqual([PATHS.book])
  })

  it('keeps the first-class sub-links of every kind under its top-level section', () => {
    const subs: Record<string, string[]> = {}
    for (const s of SECTIONS) subs[s.id] = (s.subs ?? []).map((l) => l.to)
    expect(subs.movie).toEqual([
      PATHS.movieAdd,
      PATHS.movieCollections,
      PATHS.movieDiscover,
      PATHS.movieCalendar,
    ])
    expect(subs.tv).toEqual([PATHS.tvAdd, PATHS.tvImport])
    expect(subs.music).toEqual([PATHS.musicAdd])
    expect(subs.book).toEqual([
      PATHS.bookAdd,
      PATHS.bookUnmapped,
      PATHS.bookBookshelves,
      PATHS.bookSeries,
    ])
    expect(subs.activity).toEqual([
      PATHS.activityQueue,
      PATHS.activityHistory,
      PATHS.activityBlocklist,
    ])
    expect(subs.discover).toEqual([])
    expect(subs.calendar).toEqual([])
    expect(subs.system).toEqual([])
  })

  it('routes Discover to the unified cross-kind page, and keeps the Movies discover page highlighted (issue #118)', () => {
    const discover = SECTIONS.find((s) => s.id === 'discover')
    expect(discover?.to).toBe(PATHS.discover)
    // Its base covers BOTH routes, so /movies/discover keeps highlighting the
    // Discover section (its pre-#118 behavior).
    expect(discover?.base).toEqual([PATHS.discover, PATHS.movieDiscover])
  })

  it('drops Dashboard and Indexers as sidebar sections (issue #148)', () => {
    const ids = SECTIONS.map((s) => s.id)
    expect(ids, 'no dashboard section').not.toContain('dashboard')
    expect(ids, 'no indexers section').not.toContain('indexers')
  })

  it('parks Indexers as a sub-link under Settings (interim reachability, issue #148)', () => {
    const settings = SECTIONS.find((s) => s.id === 'settings')
    const indexers = settings?.subs?.find((l) => l.to === PATHS.indexers)
    expect(indexers?.label).toBe('Indexers')
    // The Prowlarr sub-pages stay navigable as nested sub-links, and the
    // nested group's base covers all three indexer routes.
    expect(indexers?.subs?.map((l) => l.to)).toEqual([
      PATHS.search,
      PATHS.history,
      PATHS.indexerStats,
    ])
    expect(indexers?.base).toEqual([PATHS.indexers, PATHS.search, PATHS.history])
    // The Settings section stays active across the whole indexer area.
    expect(settings?.base).toContain(PATHS.indexers)
    expect(settings?.base).toContain(PATHS.search)
    expect(settings?.base).toContain(PATHS.history)
  })

  it('registers the global search results route, distinct from the Indexers release search (issue #117)', () => {
    expect(PATHS.globalSearch).toBe('/search/all')
    expect(PATHS.globalSearch).not.toBe(PATHS.search)
  })
})

describe('sectionForPath — route → section (active state)', () => {
  const cases: [string, string | null][] = [
    // #148: the Dashboard route is now a redirect to Discover; no section
    // owns the transient route.
    ['/dashboard', null],
    ['/discover', 'discover'],
    // The (kept) Movies discover page highlights Discover via the section's
    // base, same as before.
    ['/movies/discover', 'discover'],
    // #148: /library is a direct-URL route (no Library section anymore).
    ['/library', null],
    ['/movies', 'movie'],
    ['/movies/calendar', 'movie'],
    ['/movies/collections', 'movie'],
    ['/movies/add', 'movie'],
    ['/tv', 'tv'],
    ['/tv/import', 'tv'],
    ['/music', 'music'],
    ['/music/add', 'music'],
    ['/books', 'book'],
    ['/books/series/7', 'book'],
    ['/calendar', 'calendar'],
    ['/activity/queue', 'activity'],
    ['/activity/history', 'activity'],
    ['/activity/blocklist', 'activity'],
    ['/queue', 'activity'],
    // #148: Indexers is a Settings sub-link (interim): its routes keep
    // highlighting the Settings section.
    ['/indexers', 'settings'],
    ['/indexers/stats', 'settings'],
    ['/search', 'settings'],
    ['/history', 'settings'],
    ['/search/all', 'settings'],
    ['/settings', 'settings'],
    ['/settings/media-management', 'settings'],
    ['/system', 'system'],
    ['/system/status', 'system'],
    // Wanted stays routed but is not a sidebar section.
    ['/wanted/missing', null],
    ['/wanted/cutoff', null],
    ['/login', null],
  ]
  it.each(cases)('maps %s to %s', (path, want) => {
    expect(sectionForPath(path)?.id ?? null).toBe(want)
  })
})
