import '@testing-library/jest-dom/vitest'

// jsdom does not implement window.matchMedia (issue #123's useMediaQuery
// relies on it for the responsive shell). Assign a desktop-default,
// non-matching mock directly — NOT via vi.stubGlobal — so a test file's own
// vi.stubGlobal stub records this function as the original and
// vi.unstubAllGlobals() restores exactly it.
window.matchMedia =
  window.matchMedia ??
  ((query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addEventListener: () => {},
    removeEventListener: () => {},
    addListener: () => {},
    removeListener: () => {},
    dispatchEvent: () => false,
  }))
