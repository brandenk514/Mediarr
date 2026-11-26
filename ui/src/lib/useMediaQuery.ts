import { useEffect, useState } from 'react'

// useMediaQuery — testable viewport-breakpoint check (issue #123).
//
// Returns whether `query` currently matches and keeps the state fresh when
// the browser fires change events (a resize crossing the breakpoint). It
// reads window.matchMedia, which tests stub with vi.stubGlobal (jsdom has
// no real matchMedia; the test setup file provides a desktop-default mock).
export function useMediaQuery(query: string): boolean {
  const [matches, setMatches] = useState(() => window.matchMedia(query).matches)

  useEffect(() => {
    const mql = window.matchMedia(query)
    const onChange = (e: MediaQueryListEvent) => setMatches(e.matches)
    // Re-sync: the MQL read in the effect wins over the state initializer
    // (a resize can land between render and effect).
    setMatches(mql.matches)
    mql.addEventListener('change', onChange)
    return () => mql.removeEventListener('change', onChange)
  }, [query])

  return matches
}
