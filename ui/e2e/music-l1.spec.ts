import { expect, test } from '@playwright/test'
import { signIn } from './login'

// Lidarr L1: artist library grid + artist detail. The long-lived dev DB
// accumulates probe rows, so these assertions are DB-state-independent:
// no fixed counts, no page-1 positions — just "the grid renders with at
// least one card" and "clicking a card reaches the detail page".

test('music library renders the artist grid', async ({ page }) => {
  await page.goto('/music')
  await signIn(page)
  await expect(page).toHaveURL(/\/music$/)

  // The grid (at least one card) or, in a wiped library, the empty state's
  // add link — both are the correct terminal states.
  const grid = page.getByTestId('music-grid')
  const emptyAdd = page.getByTestId('music-empty-add')
  await expect(grid.or(emptyAdd)).toBeVisible()
  if (await grid.isVisible()) {
    await expect(grid.locator('[data-testid^="artist-card-"]')).not.toHaveCount(0)
  }

  // The footer status bar is always populated from /music/library/summary.
  await expect(page.getByTestId('music-summary')).toBeVisible()
  // The ribbon legend sits under the grid.
  await expect(page.getByTestId('music-legend')).toBeVisible()
})

test('clicking an artist card opens the detail page', async ({ page }) => {
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
