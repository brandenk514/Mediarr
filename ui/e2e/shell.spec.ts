import { expect, test } from '@playwright/test'
import { signIn } from './login'

// Sonarr-parity shell coverage (P1). Verifies the new chrome (top bar + left
// rail) and that every route in the parity route tree renders at its
// Sonarr-shaped path. Each test signs in fresh because the auth token is
// in-memory (page-scoped) — a page.goto would drop it.

test('authenticated shell shows the top bar and left rail', async ({ page }) => {
  await page.goto('/tv')
  await signIn(page)
  await expect(page).toHaveURL(/\/tv$/)

  // Top bar chrome.
  await expect(page.getByTestId('brand')).toBeVisible()
  await expect(page.getByTestId('global-search')).toBeVisible()
  await expect(page.getByTestId('notifications')).toBeVisible()
  await expect(page.getByTestId('logout')).toBeVisible()

  // Kind switcher (four-kind app).
  for (const id of ['tv', 'movie', 'music', 'book']) {
    await expect(page.getByTestId(`kind-${id}`)).toBeVisible()
  }

  // Left rail: the TV library section + all five shared sections.
  await expect(page.getByTestId('rail-series')).toBeVisible()
  await expect(page.getByTestId('rail-calendar')).toBeVisible()
  await expect(page.getByTestId('rail-activity')).toBeVisible()
  await expect(page.getByTestId('rail-wanted')).toBeVisible()
  await expect(page.getByTestId('rail-settings')).toBeVisible()
  await expect(page.getByTestId('rail-system')).toBeVisible()
})

test('kind switcher navigates to each kind library', async ({ page }) => {
  await page.goto('/tv')
  await signIn(page)
  await expect(page).toHaveURL(/\/tv$/)

  const targets: [string, RegExp][] = [
    ['kind-movie', /\/movies$/],
    ['kind-music', /\/music$/],
    ['kind-book', /\/books$/],
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
  ['/indexers', 'indexers-hub'],
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

// R7 home decision: the default landing is the Movies list (the Radarr users'
// home); the cross-kind Dashboard is a Mediarr extra reachable from the rail.
test('landing at / resolves to the Movies list', async ({ page }) => {
  await page.goto('/')
  await signIn(page)
  await expect(page).toHaveURL(/\/movies$/)
  await expect(page.getByTestId('movies-title')).toBeVisible()
})

test('rail Dashboard item opens the Dashboard', async ({ page }) => {
  await page.goto('/tv')
  await signIn(page)
  await expect(page.getByTestId('rail-dashboard')).toBeVisible()
  await page.getByTestId('rail-dashboard').click()
  await expect(page).toHaveURL(/\/dashboard$/)
  await expect(page.getByTestId('refresh')).toBeVisible()
})
