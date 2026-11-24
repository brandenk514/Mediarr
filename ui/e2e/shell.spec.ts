import { expect, test } from '@playwright/test'
import { signIn } from './login'

// Unified app shell coverage (issue #116). Verifies the shell chrome (top
// bar with global search + sidebar) and that every route in the route tree
// renders at its path. Each test signs in fresh because the auth token is
// in-memory (page-scoped) — a page.goto would drop it.

test('authenticated shell shows the top bar and sidebar', async ({ page }) => {
  await page.goto('/tv')
  await signIn(page)
  await expect(page).toHaveURL(/\/tv$/)

  // Top bar chrome. Global search is functional (issue #117): typing +
  // Enter navigates to the /search/all results route.
  await expect(page.getByTestId('brand')).toBeVisible()
  await expect(page.getByTestId('global-search')).toBeVisible()
  await expect(page.getByTestId('notifications')).toBeVisible()
  await expect(page.getByTestId('logout')).toBeVisible()

  // Sidebar: the eight unified-shell sections (no kind switcher).
  for (const id of [
    'dashboard',
    'discover',
    'library',
    'calendar',
    'activity',
    'indexers',
    'settings',
    'system',
  ]) {
    await expect(page.getByTestId(`rail-${id}`)).toBeVisible()
  }

  // The kind facet: the Library section's four kind rows (visible while on a
  // library route, where /tv is).
  for (const id of ['tv', 'movies', 'music', 'books']) {
    await expect(page.getByTestId(`rail-sub-${id}`)).toBeVisible()
  }
})

test('Library kind rows navigate to each kind library', async ({ page }) => {
  await page.goto('/tv')
  await signIn(page)
  await expect(page).toHaveURL(/\/tv$/)

  const targets: [string, RegExp][] = [
    ['rail-sub-movies', /\/movies$/],
    ['rail-sub-music', /\/music$/],
    ['rail-sub-books', /\/books$/],
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
  ['/dashboard', 'refresh'],
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
// (supersedes the R7 Movies-list landing). The Movies list stays reachable
// from the Library rail section.
test('landing at / resolves to the unified Discover page', async ({ page }) => {
  await page.goto('/')
  await signIn(page)
  await expect(page).toHaveURL(/\/discover$/)
  await expect(page.getByTestId('discover-page-title')).toBeVisible()
})

test('rail Dashboard item opens the Dashboard', async ({ page }) => {
  await page.goto('/tv')
  await signIn(page)
  await expect(page.getByTestId('rail-dashboard')).toBeVisible()
  await page.getByTestId('rail-dashboard').click()
  await expect(page).toHaveURL(/\/dashboard$/)
  await expect(page.getByTestId('refresh')).toBeVisible()
})
