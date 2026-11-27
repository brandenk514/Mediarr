import { describe, expect, it } from 'vitest'
import { colorTones, genreColorMap, toneForGenre, toneNames } from './tone-map'

// Fidelity guard for the Seerr duotone port (issue #147): the tables below
// are verbatim from seerr/src/components/Discover/constants.ts @ 9399fe5a
// (MIT (c) 2020 sct). A future edit that drifts from the source fails here.

describe('tone-map (Seerr duotone port, issue #147)', () => {
  it('ports all 14 colorTones verbatim from the Seerr source', () => {
    expect(toneNames).toHaveLength(14)
    expect(colorTones).toEqual({
      red: ['991B1B', 'FCA5A5'],
      darkred: ['1F2937', 'F87171'],
      blue: ['032541', '01b4e4'],
      lightblue: ['1F2937', '60A5FA'],
      darkblue: ['1F2937', '2864d2'],
      orange: ['92400E', 'FCD34D'],
      lightgreen: ['065F46', '6EE7B7'],
      green: ['087d29', '21cb51'],
      purple: ['5B21B6', 'C4B5FD'],
      yellow: ['777e0d', 'e4ed55'],
      darkorange: ['552c01', 'd47c1d'],
      black: ['1F2937', 'D1D5DB'],
      pink: ['9D174D', 'F9A8D4'],
      darkpurple: ['480c8b', 'a96bef'],
    })
  })

  it('ports the 28-entry TMDB genre -> tone map with spot-checked entries', () => {
    expect(Object.keys(genreColorMap)).toHaveLength(28)
    expect(genreColorMap[0]).toBe('black') // fallback genre id
    expect(genreColorMap[28]).toBe('red') // Action
    expect(genreColorMap[16]).toBe('blue') // Animation
    expect(genreColorMap[27]).toBe('black') // Horror
    expect(genreColorMap[10751]).toBe('yellow') // Family
    expect(genreColorMap[10759]).toBe('darkpurple') // Action & Adventure
    expect(genreColorMap[10764]).toBe('darkorange') // Reality
    // Every mapped tone resolves to a real colorTones entry.
    for (const tone of Object.values(genreColorMap)) {
      expect(colorTones).toHaveProperty(tone)
    }
  })

  it('resolves toneForGenre with the black fallback for unmapped ids', () => {
    expect(toneForGenre(16)).toEqual(['032541', '01b4e4']) // Animation -> blue
    expect(toneForGenre(999999)).toEqual(colorTones.black)
  })
})
