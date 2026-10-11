import { describe, expect, it } from 'vitest'
import { progressLabel, timeAgo } from './format'

describe('timeAgo', () => {
  const now = new Date('2026-10-11T12:00:00Z').getTime()
  const iso = (msAgo: number) => new Date(now - msAgo).toISOString()

  it('reads very recent events as "just now"', () => {
    expect(timeAgo(iso(0), now)).toBe('just now')
    expect(timeAgo(iso(4_000), now)).toBe('just now')
  })

  it('escalates through seconds, minutes, hours, days', () => {
    expect(timeAgo(iso(30_000), now)).toBe('30s ago')
    expect(timeAgo(iso(3 * 60_000), now)).toBe('3m ago')
    expect(timeAgo(iso(5 * 3_600_000), now)).toBe('5h ago')
    expect(timeAgo(iso(2 * 86_400_000), now)).toBe('2d ago')
  })

  it('falls back to a date for older events', () => {
    expect(timeAgo(iso(30 * 86_400_000), now)).toMatch(/2026/)
  })

  it('returns empty for unparseable input', () => {
    expect(timeAgo('not-a-date', now)).toBe('')
  })
})

describe('progressLabel', () => {
  it('shows nothing at 0 (state pill carries the meaning)', () => {
    expect(progressLabel(0)).toBe('')
  })

  it('caps 100 at "100%"', () => {
    expect(progressLabel(100)).toBe('100%')
    expect(progressLabel(150)).toBe('100%')
  })

  it('shows the percent in between', () => {
    expect(progressLabel(42)).toBe('42%')
  })
})
