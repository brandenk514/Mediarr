import { expect, test } from '@playwright/test'
import { signIn } from './login'

// Unified app shell coverage (issue #116; reshaped by issue #148 to the Seerr
// layout: sidebar with brand + nav + version pill, and a content header with
// the global search + avatar menu — no top bar). Verifies the shell chrome
// and that every route in the route tree renders at its path. Each test signs
// in fresh because the auth token is in-memory (page-scoped) — a page.goto
// would drop it.

test('authenticated shell shows the sidebar and content header', async ({ page }) => {
  await page.goto('/tv')
  await signIn(page)
  await expect(page).toHaveURL(/\/tv$/)

  // Content header chrome. Global search is functional (issue #117): typing
  // + Enter navigates to the /search/all results route. The notifications
  // bell is a no-op placeholder.
  await expect(page.getByTestId('global-search')).toBeVisible()
  await expect(page.getByTestId('notifications')).toBeVisible()
  // The avatar button stands in for the old topbar logout link; opening it
  // reveals the sign-out menu item.
  await expect(page.getByTestId('user-menu')).toBeVisible()
  await page.getByTestId('user-menu').click()
  await expect(page.getByTestId('logout')).toBeVisible()

  // Sidebar: brand, the nine sections (issue #148 IA: kind-first, Activity
  // before Calendar; Indexers parked under Settings), and the version pill.
  await expect(page.getByTestId('brand')).toBeVisible()
  for (const id of [
    'discover',
    'movie',
    'tv',
    'music',
    'book',
    'activity',
    'calendar',
    'settings',
    'system',
  ]) {
    await expect(page.getByTestId(`rail-${id}`)).toBeVisible()
  }
  await expect(page.getByTestId('version-pill')).toBeVisible()

  // While on a /tv route the Series section is active and reveals its
  // sub-links (first-class per-kind pages).
  await expect(page.getByTestId('rail-sub-tvadd')).toBeVisible()
  await expect(page.getByTestId('rail-sub-tvimport')).toBeVisible()
})

test('kind sections navigate to the per-kind pages (#148 IA)', async ({ page }) => {
  await page.goto('/tv')
  await signIn(page)
  await expect(page).toHaveURL(/\/tv$/)

  // #148: the kind sections are top-level sidebar entries pointing at the
  // per-kind routes, not facets of the /library grid.
  const targets: [string, RegExp][] = [
    ['rail-movie', /\/movies$/],
    ['rail-music', /\/music$/],
    ['rail-book', /\/books$/],
  ]
  for (const [id, re] of targets) {
    await page.getByTestId(id).click()
    await expect(page).toHaveURL(re)
  }
})

// [route, page-testid] pairs: the parity route tree (P1) and the page it renders.
const ROUTES: [string, string][] = [
  ['/calendar', 'calendar'],
  ['/activity/queue', 'queue-title'],
  ['/activity/history', 'history'],
  ['/activity/blocklist', 'blocklist'],
  ['/wanted/missing', 'wanted-missing'],
  ['/wanted/cutoff', 'wanted-cutoff'],
  ['/settings/media-management', 'settings-shell'],
  ['/system/status', 'system-status'],
  ['/tv/import', 'library-import'],
  // #148: /dashboard is a redirect to the Discover landing (the Dashboard
  // page was removed).
  ['/dashboard', 'discover-page-title'],
  // Prowlarr PR3: the Indexers hub + Stats page, and the Search/History pages.
  ['/indexers', 'idx-hub-title'],
  ['/indexers/stats', 'indexers-stats-title'],
  ['/search', 'release-search-title'],
  ['/history', 'search-history-title'],
]

for (const [route, testId] of ROUTES) {
  test(`parity route ${route} renders`, async ({ page }) => {
    await page.goto(route)
    await signIn(page)
    // /settings and /system have no-section redirects; assert the final URL.
    await expect(page.getByTestId(testId)).toBeVisible()
  })
}

test('/settings redirects to the default section', async ({ page }) => {
  await page.goto('/settings')
  await signIn(page)
  await expect(page).toHaveURL(/\/settings\/media-management$/)
  await expect(page.getByTestId('settings-shell')).toBeVisible()
})

test('series detail route renders at /tv/:id', async ({ page }) => {
  // A deep link to the detail route: AuthGate bounces to /login and sign-in
  // returns us client-side to /tv/:id. The id may or may not exist in this
  // DB; the detail component renders in any case (data or error state). The
  // list→detail entry point (and the full detail build) land in P2.
  await page.goto('/tv/1')
  await signIn(page)
  await expect(page).toHaveURL(/\/tv\/1$/)
  const settled = page.getByTestId('series-detail').or(page.getByTestId('series-detail-error'))
  await expect(settled).toBeVisible()
})

// #118: the default landing is the unified cross-kind Discover page
// (supersedes the R7 Movies-list landing). The Movies list is a top-level
// sidebar section since #148.
test('landing at / resolves to the unified Discover page', async ({ page }) => {
  await page.goto('/')
  await signIn(page)
  await expect(page).toHaveURL(/\/discover$/)
  await expect(page.getByTestId('discover-page-title')).toBeVisible()
})

// #148: the Dashboard page was removed; the /dashboard deep link stays
// navigable and lands on the Discover landing.
test('/dashboard redirects to the Discover landing', async ({ page }) => {
  await page.goto('/dashboard')
  await signIn(page)
  await expect(page).toHaveURL(/\/discover$/)
  await expect(page.getByTestId('discover-page-title')).toBeVisible()
})
