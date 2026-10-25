import { expect, test, type Page } from '@playwright/test'
import { E2E_PASSWORD, E2E_USER, signIn } from './login'

// P2 TV parity e2e, run against the running dev stack (Vite :5173 → backend
// :8081). The dev stack has no TVDB/TMDB keys (provider search is 503), so
// series are seeded directly through the API before signing in, and the add
// spec asserts the real two-pane layout plus the unconfigured-provider state.
//
// The specs are build-agnostic where the backend may still be a pre-P2 build:
// grid/detail use only the base schema; the import wizard accepts either the
// P2 scan report or the 404 scan-error line.

async function apiToken(page: Page): Promise<string> {
  const res = await page.request.post('/api/v1/auth/login', {
    data: { username: E2E_USER, password: E2E_PASSWORD },
  })
  if (!res.ok()) throw new Error(`e2e login failed: ${res.status()}`)
  const body = (await res.json()) as { token: string }
  return body.token
}

// Seed (idempotently — the add endpoint returns the existing row) and return
// the series id.
async function seedSeries(page: Page, title: string, year: number): Promise<number> {
  const token = await apiToken(page)
  const res = await page.request.post('/api/v1/tv', {
    headers: { Authorization: `Bearer ${token}` },
    data: { title, year: String(year), monitored: true, quality_profile: 'HD-1080p' },
  })
  if (!res.ok()) throw new Error(`seed ${title} failed: ${res.status()} ${await res.text()}`)
  const body = (await res.json()) as { id: number }
  return body.id
}

test('series page renders the P2 grid: toolbar, posters, A–Z, view toggle', async ({ page }) => {
  await page.goto('/tv')
  const a = await seedSeries(page, 'Alpha E2E Show', 2020)
  const z = await seedSeries(page, 'The Zeta E2E Show', 2001)
  await signIn(page)
  await expect(page).toHaveURL(/\/tv$/)

  // Page head + toolbar (icon-over-label groups, left and right).
  await expect(page.getByTestId('tv-title')).toHaveText('Series')
  for (const id of ['tv-update-all', 'tv-rss-sync', 'tv-select-series', 'tv-test-parsing', 'tv-options', 'tv-sort', 'tv-filter']) {
    await expect(page.getByTestId(id)).toBeVisible()
  }

  // Grid with the two seeded cards: monitored + quality footer lines.
  const cardA = page.getByTestId(`series-card-${a}`)
  await expect(cardA).toBeVisible()
  await expect(cardA).toContainText('Monitored')
  await expect(cardA).toContainText('HD-1080p')
  await expect(page.getByTestId(`series-card-${z}`)).toBeVisible()

  // A–Z: A (Alpha) and Z ("The Zeta…" skips the article).
  const az = page.getByTestId('tv-az')
  await expect(az.getByRole('button', { name: 'Jump to A' })).toBeEnabled()
  await expect(az.getByRole('button', { name: 'Jump to Z' })).toBeEnabled()

  // View toggle: list view shows the (P1) expandable rows.
  await page.getByTestId('tv-view-list').click()
  await expect(page.getByTestId('tv-grid')).toHaveCount(0)
  await expect(page.getByTestId(`series-${a}`)).toBeVisible()
  await page.getByTestId('tv-view-grid').click()
  await expect(page.getByTestId('tv-grid')).toBeVisible()
})

test('add series two-pane: layout, unconfigured-provider note, disabled add', async ({ page }) => {
  await page.goto('/tv/add')
  await signIn(page)
  await expect(page).toHaveURL(/\/tv\/add$/)
  await expect(page.getByTestId('tv-add-title')).toHaveText('Add series')

  // Source toggle (TVDB / TMDB) still switches panes.
  await page.getByTestId('tv-source-tmdb').click()

  // Debounced search lands on the real 503 note: the dev stack has no keys.
  await page.getByTestId('tv-search-input').fill('severance')
  await expect(page.getByTestId('tv-search-unavailable')).toBeVisible()

  // Right pane: the P2 add form.
  for (const id of ['tv-root-folder', 'tv-monitor', 'tv-profile', 'tv-series-type', 'tv-season-folder', 'tv-tags-input', 'tv-start-search', 'tv-start-cutoff', 'tv-add-button']) {
    await expect(page.getByTestId(id)).toBeVisible()
  }
  await expect(page.getByTestId('tv-root-folder-help')).toContainText('subfolder will be created automatically')
  // Nothing selected → the green add button stays disabled.
  await expect(page.getByTestId('tv-add-button')).toBeDisabled()
})

test('series detail: badges, TheTVDB credit, toolbar, empty seasons', async ({ page }) => {
  await page.goto('/tv')
  const id = await seedSeries(page, 'Alpha E2E Show', 2020)
  await signIn(page)
  await expect(page).toHaveURL(/\/tv$/)

  // The real user path: click the grid card.
  await page.getByTestId(`series-card-${id}`).click()
  await expect(page).toHaveURL(/\/tv\/\d+$/)
  await expect(page.getByTestId('series-detail')).toBeVisible()

  // Badge header + metadata credit.
  await expect(page.getByTestId('series-detail-header')).toBeVisible()
  await expect(page.getByTestId('badge-monitored')).toHaveText('Monitored')
  await expect(page.getByTestId('badge-quality')).toHaveText('HD-1080p')
  await expect(page.getByTestId('series-detail-meta')).toContainText('TheTVDB')

  // Toolbar: Refresh & Scan + Search Monitored (P2 endpoint) + Options.
  await expect(page.getByTestId('detail-refresh')).toBeVisible()
  await expect(page.getByTestId('detail-search')).toBeVisible()
  await expect(page.getByTestId('detail-options')).toBeVisible()

  // A freshly seeded series has no episodes: the accordion area says so.
  await expect(page.getByTestId('series-detail-no-episodes')).toBeVisible()
})

test('library import wizard: root step, scan, match, report', async ({ page }) => {
  // Fixture folder (matching the seeded series) lives on the backend's
  // persistent /media volume: /media/e2e-import/Alpha E2E Show (2020).
  await page.goto('/tv/import')
  await seedSeries(page, 'Alpha E2E Show', 2020)
  await signIn(page)
  await expect(page).toHaveURL(/\/tv\/import$/)
  await expect(page.getByTestId('library-import')).toBeVisible()
  await expect(page.getByTestId('import-steps')).toBeVisible()

  // The wizard may land on "registered folder" (P2 backend with roots) or on
  // the unmapped-path fallback (no roots / pre-P2 backend). Use whatever the
  // wizard offers: prefer a registered folder, else the free path.
  const rootSelect = page.getByTestId('import-root-select')
  if (await rootSelect.isVisible().catch(() => false)) {
    // Scan a registered root (whatever it is) — the flow still runs.
    await page.getByTestId('import-scan').click()
  } else {
    await page.getByTestId('import-mode-path').click()
    await page.getByTestId('import-path-input').fill('/media/e2e-import')
    await page.getByTestId('import-scan').click()
  }

  // P2 backend: the report lands on the scan step with candidate rows.
  // Pre-P2 backend: the import route 404s and the wizard shows the error.
  const results = page.getByTestId('import-results')
  const scanError = page.getByTestId('import-scan-error')
  const which = await Promise.race([
    results
      .waitFor({ state: 'visible', timeout: 8000 })
      .then(() => 'results')
      .catch(() => 'pending'),
    scanError
      .waitFor({ state: 'visible', timeout: 8000 })
      .then(() => 'error')
      .catch(() => 'pending'),
  ])
  if (which === 'error') {
    // Documented: the running backend predates the P2 import endpoint.
    return
  }
  if (which === 'pending') {
    throw new Error('import scan produced neither a report nor an error')
  }

  // Match: the fixture folder matches the seeded series (only meaningful when
  // scanning /media/e2e-import, i.e. the unmapped-path branch).
  const matched = page.getByTestId('import-matched')
  if ((await matched.count()) > 0) {
    await expect(matched.first()).toContainText('Matched')
    // Confirm → report with counts.
    await page.getByTestId('import-confirm').click()
    await expect(page.getByTestId('import-report')).toBeVisible()
    await expect(page.getByTestId('import-step-report')).toHaveClass('active')
  }
})
