import { expect, test } from '@playwright/test'
import { signIn } from './login'

// Radarr R3: Collections + Discover pages. Both must work WITHOUT a live
// TMDB key (keyless dev): Collections renders its empty state (library
// movies have no collection id yet), Discover shows the 503 reason greyed
// out. The two pages are first-class sub-links of the Movies rail section.
// Never require live TMDB data here.

test('movies rail shows Collections and Discover sub-links', async ({ page }) => {
  await page.goto('/movies')
  await signIn(page)
  await expect(page).toHaveURL(/\/movies$/)

  // Sub-items are revealed while the route is within the /movies scope.
  await expect(page.getByTestId('rail-sub-moviesadd')).toBeVisible()
  await expect(page.getByTestId('rail-sub-moviescollections')).toBeVisible()
  await expect(page.getByTestId('rail-sub-moviesdiscover')).toBeVisible()
})

test('/movies/collections renders (empty state or cards)', async ({ page }) => {
  await page.goto('/movies/collections')
  await signIn(page)
  await expect(page).toHaveURL(/\/movies\/collections$/)
  await expect(page.getByTestId('collections-title')).toBeVisible()
  // Keyless: every movie is ungrouped, so the empty state is expected —
  // but the grid is equally valid once collections exist.
  await expect(
    page.locator('[data-testid="collections-empty"], [data-testid="collections-grid"]'),
  ).toBeVisible()
})

test('/movies/discover shows the keyless 503 state (or cards when keyed)', async ({ page }) => {
  await page.goto('/movies/discover')
  await signIn(page)
  await expect(page).toHaveURL(/\/movies\/discover$/)
  await expect(page.getByTestId('discover-title')).toBeVisible()
  // Keyless dev: the 503 reason in the greyed-out empty state. With a TMDB
  // key the trending grid renders instead — both pass.
  await expect(
    page.locator('[data-testid="discover-unavailable"], [data-testid="discover-grid"]'),
  ).toBeVisible()
})
