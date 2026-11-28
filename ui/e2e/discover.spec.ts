import { expect, test } from '@playwright/test'
import type { Page } from '@playwright/test'
import { signIn } from './login'

// Discover row rebuild (issue #151): the page is now the Seerr-style
// cinematic row set — a compact KPI strip (the absorbed Dashboard counts),
// a landscape recent-activity row (the absorbed Dashboard feeds), the
// split feed rows (Trending, Popular Movies/Series, Upcoming Movies/Series),
// the merged Recently Added + Watchlist rows, the client-grouped genre /
// studio facet rows, and chevron-scroller chrome on every row.
//
// These specs run against the LIVE dev stack (deploy/compose.dev.yml,
// :8080), which is KEYLESS: no TMDB key, no Google Books key, MusicBrainz
// + Open Library are the keyless public feeds. The deterministic contract
// mirrors the #117 global-search spec:
//   - Books (Open Library) resolves in every environment;
//   - Movies/TV (TMDB) are keyless → friendly "not configured" notes,
//     NEVER the raw wire reasons (grep-level guard below);
//   - Music (MusicBrainz) is keyless but the public service rate-limits
//     aggressively → assert the per-cell contract (cards OR friendly
//     note), never the data.
//
// Auth: the token lives in page memory, so each test signs in first (the
// app's returnTo lands on /discover) and then lets client-side navigation
// carry the flow — never page.goto after sign-in.

// The row ids, in DOM order (the contract #158 and this suite depend on).
const ROWS = [
  'recentlyAdded',
  'recentActivity',
  'trending',
  'popularMovies',
  'popularSeries',
  'upcomingMovies',
  'upcomingSeries',
  'watchlist',
  'movieGenres',
  'studios',
] as const

// The raw wire reasons that must NEVER reach the DOM (the mapper's input
// keys; internal/services/discover/discover.go). Case-sensitive: the server
// emits exactly these strings, and the friendly notes are capitalised.
const RAW_REASONS = [
  'tmdb is not configured (missing API key)',
  'google books is not configured (missing API key)',
  'musicbrainz is not configured',
  'openlibrary is not configured',
  'calendar is not configured',
  'books library is not configured',
]

async function gotoDiscover(page: Page): Promise<void> {
  await page.goto('/discover')
  // Unsigned in, the app redirects every route to /login — wait for the
  // form before acting (the redirect is not instant), then sign in and let
  // the app's returnTo carry the flow to /discover (the token lives in page
  // memory, so never page.goto once authenticated).
  await expect(page.getByTestId('username')).toBeVisible()
  await signIn(page)
  await expect(page).toHaveURL(/\/discover$/)
  // The page has ONE loading phase: when the trending row settles (cards,
  // a friendly note, or its EmptyState) every row has settled.
  await expect
    .poll(async () =>
      page
        .getByTestId('discover-row-trending')
        .locator(
          '[data-testid^="discover-card-"], [data-testid="discover-row-notes"], .empty-state',
        )
        .count(),
    )
    .toBeGreaterThan(0)
}

test('renders the KPI strip and all ten rows in the reference order', async ({ page }) => {
  await gotoDiscover(page)

  await expect(page.getByTestId('discover-page')).toBeVisible()
  await expect(page.getByTestId('discover-page-title')).toHaveText('Discover')
  // The compact KPI strip (the absorbed Dashboard counts) renders.
  await expect(page.getByTestId('discover-kpi')).toBeVisible()

  // All ten rows, in the reference order, each a labelled <section>.
  const ids = await page
    .locator('section[data-testid^="discover-row-"]')
    .evaluateAll((els) => els.map((e) => e.getAttribute('data-testid')))
  expect(ids).toEqual(ROWS.map((r) => `discover-row-${r}`))
})

test('every row has chevron scrollers and a focusable (keyboard) scroll track', async ({
  page,
}) => {
  await gotoDiscover(page)

  expect(await page.locator('[data-testid$="-prev"]').count()).toBe(10)
  expect(await page.locator('[data-testid$="-next"]').count()).toBe(10)
  // Every track is keyboard-reachable (tabindex 0) and labelled.
  const tracks = page.locator('[role="group"][aria-label].discover-row-track')
  expect(await tracks.count()).toBe(10)
  const tabindexes = await tracks.evaluateAll((els) => els.map((e) => e.getAttribute('tabindex')))
  expect(tabindexes).toEqual(Array(10).fill('0'))
  // A fresh stack overflows at most a few rows: at least one chevron pair
  // must be deterministically disabled at rest.
  expect(await page.locator('button[data-testid$="-prev"][disabled]').count()).toBeGreaterThanOrEqual(1)
})

test('keyboard scrolling: arrow keys drive an overflowing track', async ({ page }) => {
  await gotoDiscover(page)

  const track = page.locator('[role="group"][aria-label="Trending"]')
  await track.focus()
  // The track scrolls when — and only when — its content overflows; on a
  // fresh keyless stack the book cells (Open Library) typically fill the
  // trending row. When it is (rarely) empty there is nothing to scroll.
  const overflows = await track.evaluate((el) => el.scrollWidth > el.clientWidth)
  if (overflows) {
    await track.press('ArrowRight')
    await expect
      .poll(async () => track.evaluate((el) => el.scrollLeft))
      .toBeGreaterThan(0)
    await track.press('ArrowLeft')
    await expect
      .poll(async () => track.evaluate((el) => el.scrollLeft))
      .toBe(0)
  } else {
    await expect(track).toBeVisible()
  }
})

test('raw provider reasons never reach the DOM (grep-level guard)', async ({ page }) => {
  await gotoDiscover(page)

  const text = await page.locator('body').innerText()
  for (const raw of RAW_REASONS) {
    expect(text, `raw wire reason leaked into the DOM: "${raw}"`).not.toContain(raw)
  }
  // The "(missing API key)" marker is server-internal and never user-facing.
  expect(text).not.toContain('(missing API key)')
})

test('keyless dev stack: friendly notes for the keyed cells, real art for the keyless ones', async ({
  page,
}) => {
  await gotoDiscover(page)

  // TMDB is keyless on the dev stack: the movie/tv feed cells degrade to
  // the friendly note (never the raw reason).
  await expect(
    page.getByText('TMDB is not configured — add an API key in Settings to enable this feed.').first(),
  ).toBeVisible()
  // Google Books is keyless: the new_releases:book cell names the missing
  // key with the friendly wording (its OpenLibrary fallback items still
  // render in the trending row).
  await expect(
    page.getByText('Google Books is not configured — add an API key in Settings to enable this feed.').first(),
  ).toBeVisible()
  // Open Library is keyless and resolves in every environment: at least
  // one real book card lands in the trending row.
  await expect(page.locator('[data-testid^="discover-card-trending-book-"]').first()).toBeVisible()
})

test('every row settles to cards, a friendly note, or its EmptyState — no blank track', async ({ page }) => {
  await gotoDiscover(page)

  // Data-independent invariant: whatever this stack's libraries hold, each
  // row must resolve to at least one of (a) real cells, (b) friendly
  // grey-out notes (keyless / failed cells), or (c) its EmptyState. A row
  // that renders a bare empty track is a layout bug.
  for (const id of ROWS) {
    const track = page.getByTestId(`discover-row-${id}`).locator('.discover-row-track')
    await expect
      .poll(async () =>
        track
          .locator(
            '[data-testid^="discover-card-"], .discover-row-note, [data-testid^="discover-row-empty-"]',
          )
          .count(),
      )
      .toBeGreaterThan(0)
  }
})

test('the KPI strip cards are links into the kind list routes', async ({ page }) => {
  await gotoDiscover(page)

  await expect(page.getByTestId('discover-kpi')).toBeVisible()
  // The totals card always renders (the dashboard totals are always in the
  // wire). Kind cards render per configured library; when present they
  // must be real links into the list routes.
  await expect(page.getByTestId('discover-kpi-total')).toBeVisible()
  const kindRoute: Record<string, string> = { movie: '/movies', tv: '/tv', music: '/music', book: '/books' }
  for (const kind of Object.keys(kindRoute)) {
    const card = page.getByTestId(`discover-kpi-${kind}`)
    if ((await card.count()) > 0) {
      await expect(card).toHaveAttribute('href', kindRoute[kind])
    }
  }
})

test('#123 responsive contract: at 375px the rows scroll, nothing clips', async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 667 })
  await gotoDiscover(page)

  // The mechanism is horizontal scroll: every track is overflow-x:auto,
  // and the fixed 144px (w-36) cells never shrink to fit (no clipping).
  const tracks = page.locator('.discover-row-track')
  expect(await tracks.count()).toBe(10)
  const overflowX = await tracks.evaluateAll((els) =>
    els.map((e) => getComputedStyle(e).overflowX),
  )
  expect(overflowX.every((v) => v === 'auto')).toBe(true)

  // A card keeps its design width at the mobile viewport (144px ± AA).
  const card = page.locator('.discover-card').first()
  if ((await card.count()) > 0) {
    const box = await card.boundingBox()
    expect(box).not.toBeNull()
    expect(Math.abs(box!.width - 144)).toBeLessThanOrEqual(2)
  }
})

test('deep link: a trending movie card routes into the Movies add flow', async ({ page }) => {
  await gotoDiscover(page)

  // TMDB is keyless on the dev stack, so trending movie cards are not
  // present — the deep-link contract is exercised whenever a movie feed
  // cell does resolve (keyed environment / provider data).
  const card = page.locator('[data-testid^="discover-card-trending-movie-"]').first()
  if ((await card.count()) > 0) {
    await card.click()
    await expect(page).toHaveURL(/\/movies\/add\?tmdb=\d+/)
  } else {
    await expect(page.getByTestId('discover-row-trending')).toBeVisible()
  }
})
