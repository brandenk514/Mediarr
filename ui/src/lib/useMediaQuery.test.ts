import { act, renderHook } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { useMediaQuery } from './useMediaQuery'

// jsdom has no real matchMedia; each test stubs it so the breakpoint state
// (and its change events) are controlled (issue #123).
function stubMatchMedia(initial: boolean) {
  const listeners = new Set<(e: { matches: boolean }) => void>()
  const mql = {
    matches: initial,
    media: '(max-width: 768px)',
    addEventListener: (_: string, cb: (e: { matches: boolean }) => void) => {
      listeners.add(cb)
    },
    removeEventListener: (_: string, cb: (e: { matches: boolean }) => void) => {
      listeners.delete(cb)
    },
  }
  vi.stubGlobal('matchMedia', vi.fn(() => mql))
  return {
    fire(matches: boolean) {
      mql.matches = matches
      listeners.forEach((cb) => cb({ matches }))
    },
  }
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('useMediaQuery (issue #123)', () => {
  it('reflects the initial match state', () => {
    stubMatchMedia(true)
    const { result } = renderHook(() => useMediaQuery('(max-width: 768px)'))
    expect(result.current).toBe(true)
  })

  it('returns false when the query does not match', () => {
    stubMatchMedia(false)
    const { result } = renderHook(() => useMediaQuery('(max-width: 768px)'))
    expect(result.current).toBe(false)
  })

  it('updates when the media query fires a change event', () => {
    const mql = stubMatchMedia(false)
    const { result } = renderHook(() => useMediaQuery('(max-width: 768px)'))
    expect(result.current).toBe(false)
    act(() => mql.fire(true))
    expect(result.current).toBe(true)
    act(() => mql.fire(false))
    expect(result.current).toBe(false)
  })

  it('re-syncs when the MQL state changed before the effect ran', () => {
    // The hook reads matchMedia twice (state initializer, then the effect).
    // If a resize lands in between, the effect's read must win.
    let calls = 0
    vi.stubGlobal(
      'matchMedia',
      vi.fn(() => {
        calls += 1
        return {
          matches: calls === 1,
          media: '(max-width: 768px)',
          addEventListener: () => {},
          removeEventListener: () => {},
        }
      }),
    )
    const { result } = renderHook(() => useMediaQuery('(max-width: 768px)'))
    expect(result.current).toBe(false)
  })
})
