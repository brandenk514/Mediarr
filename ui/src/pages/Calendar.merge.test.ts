import { describe, expect, it } from 'vitest'
import type { CalendarEntry } from '../api/types'
import { mergeCalendarEntries } from './Calendar'

// #121: the unified calendar merges the per-kind feeds (tv/move/music) into
// one list sorted by air start. mergeCalendarEntries is the pure,
// deterministic half of that — the fan-out + kind filtering live in the
// page (covered in Calendar.test.tsx).

function entry(kind: CalendarEntry['kind'], title: string, air: string): CalendarEntry {
  return {
    kind,
    series_id: 1,
    series_title: title,
    season: 0,
    episode: 0,
    episode_title: '',
    sxe: '',
    air_start: air,
    air_end: air,
  }
}

describe('mergeCalendarEntries (issue #121)', () => {
  it('merges per-kind chunks and sorts by air_start', () => {
    const merged = mergeCalendarEntries([
      [entry('tv', 'Severance', '2026-11-05T22:00:00Z')],
      [entry('movie', 'Dune: Part Three', '2026-11-04T00:00:00Z')],
      [entry('music', 'A Moon Shaped Pool', '2026-11-06T00:00:00Z')],
    ])
    expect(merged.map((e) => e.series_title)).toEqual([
      'Dune: Part Three',
      'Severance',
      'A Moon Shaped Pool',
    ])
  })

  it('handles empty chunks and a single chunk', () => {
    expect(mergeCalendarEntries([[], []])).toEqual([])
    const single = mergeCalendarEntries([[entry('tv', 'X', '2026-01-01T00:00:00Z')]])
    expect(single).toHaveLength(1)
  })

  it('breaks air_start ties deterministically (kind, then title)', () => {
    const merged = mergeCalendarEntries([
      [
        entry('tv', 'B Show', '2026-01-01T00:00:00Z'),
        entry('tv', 'A Show', '2026-01-01T00:00:00Z'),
      ],
      [entry('music', 'Z Album', '2026-01-01T00:00:00Z')],
    ])
    // 'music' < 'tv', then the two tv titles alphabetically.
    expect(merged.map((e) => e.series_title)).toEqual(['Z Album', 'A Show', 'B Show'])
  })
})
