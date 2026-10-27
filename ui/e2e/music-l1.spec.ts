import { expect, test, type APIRequestContext } from '@playwright/test'
import { E2E_PASSWORD, E2E_USER, signIn } from './login'

// Lidarr L1: artist library grid + artist detail. Assertions are
// DB-state-independent (no fixed counts or page-1 positions); the
// long-lived dev DB accumulates probe rows, but a fresh stack has an
// empty music library, so the spec seeds one probe artist via the API
// when (and only when) the library is empty.

const PROBE_ARTIST = 'E2E Lidarr L1 Probe'

async function ensureProbeArtist(request: APIRequestContext): Promise<void> {
  const login = await request.post('/api/v1/auth/login', {
    data: { username: E2E_USER, password: E2E_PASSWORD },
  })
  const token = ((await login.json()) as { token: string }).token
  const headers = { Authorization: `Bearer ${token}` }
  const list = await request.get('/api/v1/music/artists', { headers })
  const body = (await list.json()) as { items?: unknown[] }
  if (!body.items || body.items.length === 0) {
    await request.post('/api/v1/music/artists', {
      headers,
      data: { name: PROBE_ARTIST, mbid: 'e2e-lidarr-l1-probe' },
    })
  }
}

test('music library renders the artist grid', async ({ page, request }) => {
  await ensureProbeArtist(request)
  await page.goto('/music')
  await signIn(page)
  await expect(page).toHaveURL(/\/music$/)

  // The grid with at least one card (a wiped library can't happen after
  // the seed, but the empty state stays the correct terminal state if a
  // future backend change drops the seed).
  const grid = page.getByTestId('music-grid')
  await expect(grid).toBeVisible()
  await expect(grid.locator('[data-testid^="artist-card-"]')).not.toHaveCount(0)

  // The ribbon legend sits under the grid.
  await expect(page.getByTestId('music-legend')).toBeVisible()
})

test('clicking an artist card opens the detail page', async ({ page, request }) => {
  await ensureProbeArtist(request)
  await page.goto('/music')
  await signIn(page)
  await expect(page).toHaveURL(/\/music$/)

  const card = page.locator('[data-testid^="artist-card-"]').first()
  await expect(card).toBeVisible()
  await card.click()

  // The card click is a client-side navigation to /music/<id>.
  await expect(page).toHaveURL(/\/music\/\d+$/)
  await expect(page.getByTestId('artist-detail-banner')).toBeVisible()
  await expect(page.getByTestId('artist-detail-name')).toBeVisible()
  // Toolbar (Lidarr order; the wired L1 buttons).
  await expect(page.getByTestId('artist-detail-refresh')).toBeVisible()
  await expect(page.getByTestId('artist-detail-history')).toBeVisible()
  await expect(page.getByTestId('artist-detail-edit')).toBeVisible()
  // The albums accordion section renders (groups, or the no-albums state).
  await expect(page.getByTestId('artist-detail-albums')).toBeVisible()
})
