import { describe, expect, it } from 'vitest'
import { PATHS, SECTIONS, activeKindForPath, sectionForPath } from './nav'

// Unified shell nav model (issue #116): the eight fixed sidebar sections,
// kind as a facet under Library (no kind switcher), and the route→section
// mapping the sidebar uses for active state.

describe('nav model — unified shell sections (issue #116)', () => {
  it('defines exactly the eight sidebar sections, in rail order', () => {
    expect(SECTIONS.map((s) => s.id)).toEqual([
      'dashboard',
      'discover',
      'library',
      'calendar',
      'activity',
      'indexers',
      'settings',
      'system',
    ])
  })

  it('gives every section a label, icon, default destination, and bases', () => {
    for (const s of SECTIONS) {
      expect(s.label.length).toBeGreaterThan(0)
      expect(s.icon.length).toBeGreaterThan(0)
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
      }
      for (const g of s.groups ?? []) {
        expect(known.has(g.to), `group ${g.kind} to=${g.to} not in PATHS`).toBe(true)
        expect(known.has(g.base), `group ${g.kind} base=${g.base} not in PATHS`).toBe(true)
        for (const l of g.sub) {
          expect(known.has(l.to), `group ${g.kind} sub ${l.to} not in PATHS`).toBe(true)
        }
      }
    }
  })

  it('routes Discover to the existing Movies discover page (the cross-kind grid is #118)', () => {
    const discover = SECTIONS.find((s) => s.id === 'discover')
    expect(discover?.to).toBe(PATHS.movieDiscover)
  })

  it('keeps the per-kind pages reachable: Library carries the four kind groups', () => {
    const library = SECTIONS.find((s) => s.id === 'library')
    expect(library?.groups?.map((g) => g.kind)).toEqual(['tv', 'movie', 'music', 'book'])
    expect(library?.groups?.map((g) => g.to)).toEqual([
      PATHS.tv,
      PATHS.movie,
      PATHS.music,
      PATHS.book,
    ])
  })

  it('keeps the per-kind first-class sub-links (Add New, deep pages)', () => {
    const library = SECTIONS.find((s) => s.id === 'library')
    const subs: Record<string, string[]> = {}
    for (const g of library?.groups ?? []) subs[g.kind] = g.sub.map((l) => l.to)
    expect(subs.tv).toEqual([PATHS.tvAdd, PATHS.tvImport])
    expect(subs.movie).toEqual([
      PATHS.movieAdd,
      PATHS.movieCollections,
      PATHS.movieDiscover,
      PATHS.movieCalendar,
    ])
    expect(subs.music).toEqual([PATHS.musicAdd])
    expect(subs.book).toEqual([
      PATHS.bookAdd,
      PATHS.bookUnmapped,
      PATHS.bookBookshelves,
      PATHS.bookSeries,
    ])
  })

  it('folds Search / History / Stats in as Indexers sub-links (Prowlarr IA)', () => {
    const indexers = SECTIONS.find((s) => s.id === 'indexers')
    expect(indexers?.subs?.map((l) => l.to)).toEqual([
      PATHS.search,
      PATHS.history,
      PATHS.indexerStats,
    ])
    expect(indexers?.base).toContain(PATHS.search)
    expect(indexers?.base).toContain(PATHS.history)
  })

  it('has no kind switcher: no top-level section is a kind or a Wanted section', () => {
    const labels = SECTIONS.map((s) => s.label)
    for (const kindLabel of ['TV', 'Movies', 'Music', 'Books', 'Wanted']) {
      expect(labels, `top-level section labeled ${kindLabel}`).not.toContain(kindLabel)
    }
    const ids = SECTIONS.map((s) => s.id as string)
    for (const kindId of ['tv', 'movie', 'music', 'book', 'wanted']) {
      expect(ids, `top-level section id ${kindId}`).not.toContain(kindId)
    }
  })
})

describe('sectionForPath — route → section (active state)', () => {
  const cases: [string, string | null][] = [
    ['/dashboard', 'dashboard'],
    ['/movies/discover', 'discover'],
    ['/movies', 'library'],
    ['/movies/calendar', 'library'],
    ['/movies/collections', 'library'],
    ['/movies/add', 'library'],
    ['/tv', 'library'],
    ['/tv/import', 'library'],
    ['/music', 'library'],
    ['/music/add', 'library'],
    ['/books', 'library'],
    ['/books/series/7', 'library'],
    ['/calendar', 'calendar'],
    ['/activity/queue', 'activity'],
    ['/activity/history', 'activity'],
    ['/activity/blocklist', 'activity'],
    ['/queue', 'activity'],
    ['/indexers', 'indexers'],
    ['/indexers/stats', 'indexers'],
    ['/search', 'indexers'],
    ['/history', 'indexers'],
    ['/settings', 'settings'],
    ['/settings/media-management', 'settings'],
    ['/system', 'system'],
    ['/system/status', 'system'],
    // Wanted stays routed but is not a sidebar section in v0.4.0.
    ['/wanted/missing', null],
    ['/wanted/cutoff', null],
    ['/login', null],
  ]
  it.each(cases)('maps %s to %s', (path, want) => {
    expect(sectionForPath(path)?.id ?? null).toBe(want)
  })

  it('maps every kind group to the Library section', () => {
    for (const g of SECTIONS.find((s) => s.id === 'library')?.groups ?? []) {
      expect(sectionForPath(g.to)?.id).toBe('library')
    }
  })
})

describe('activeKindForPath — the kind facet', () => {
  const cases: [string, string | null][] = [
    ['/tv', 'tv'],
    ['/tv/add', 'tv'],
    ['/movies', 'movie'],
    ['/movies/calendar', 'movie'],
    ['/music', 'music'],
    ['/books/series', 'book'],
    ['/movies/discover', 'movie'],
    ['/calendar', null],
    ['/dashboard', null],
    ['/wanted/missing', null],
  ]
  it.each(cases)('maps %s to %s', (path, want) => {
    expect(activeKindForPath(path)).toBe(want)
  })
})
