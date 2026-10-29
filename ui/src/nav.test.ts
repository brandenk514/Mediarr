import { describe, expect, it } from 'vitest'
import { INDEXERS_SECTION, SHARED_SECTIONS } from './nav'

// The Prowlarr PR1 Indexers hub is a first-class rail section (the list IS the
// home), distinct from the shared sections (Calendar/Activity/Wanted/Settings/
// System). Verify the nav model that the Layout rail renders.
describe('nav model — Prowlarr PR1 Indexers hub', () => {
  it('defines the Indexers shared section: home is the list, sub = Stats stub', () => {
    expect(INDEXERS_SECTION.id).toBe('indexers')
    expect(INDEXERS_SECTION.label).toBe('Indexers')
    expect(INDEXERS_SECTION.to).toBe('/indexers')
    expect(INDEXERS_SECTION.base).toBe('/indexers')
    expect(INDEXERS_SECTION.sub).toEqual([{ to: '/indexers/stats', label: 'Stats' }])
  })

  it('is not folded into SHARED_SECTIONS (it renders as its own rail section)', () => {
    expect(SHARED_SECTIONS.some((s) => s.id === 'indexers')).toBe(false)
  })
})
