import { expect, test } from '@playwright/test'
import { signIn } from './login'

// Add-movie journey (P7): sign in → /movies/add → type title → Add.
// A successful add navigates to /movies, where the P7 poster grid shows the
// new movie. The TMDB search box is deliberately skipped: it depends on an
// external provider (503 when unconfigured), and direct title entry is the
// idempotent core path.
//
// NOTE (v1): there is no movie delete endpoint, so the probe movie is not
// cleaned up here — the runner removes it from the database after the suite
// (CI runs against a fresh database and needs no cleanup).
test('add a movie through the form and see it in the grid', async ({ page }) => {
  const title = `E2E Playwright Probe ${Date.now()}`

  // Hit the protected route FIRST: AuthGate bounces us to /login carrying
  // state.from, and Login.tsx client-side-navigates back to /movies/add after
  // sign-in. (A page.goto after sign-in would be a full load that drops the
  // in-memory token and bounces us back to the login page.)
  await page.goto('/movies/add')
  await signIn(page)
  await expect(page).toHaveURL(/\/movies\/add$/)
  await expect(page.getByTestId('movie-add-title')).toBeVisible()
  // R2 two-pane: the right pane's edition-aware fields are present with the
  // Radarr defaults (All / Released) — no live TMDB needed for this check.
  await expect(page.getByTestId('movie-monitor')).toHaveValue('all')
  await expect(page.getByTestId('movie-min-availability')).toHaveValue('released')
  await page.getByTestId('movie-title-input').fill(title)
  await page.getByTestId('movie-add-button').click()

  await expect(page).toHaveURL(/\/movies$/)
  await expect(page.getByTestId('movies-grid')).toBeVisible()
  // Local dev databases accumulate probe movies across runs, and the grid is
  // paginated (25/page): with the default title sort the freshest probe can
  // sit on page 2. Sort newest-added first so the new movie leads the grid
  // regardless of database state (CI runs against a fresh database).
  await page.getByTestId('movies-sort-btn').click()
  await page.getByTestId('movies-sort-opt-added-desc').click()
  // Grid cards carry the title in their tooltip (the R1 grid design shows the
  // status ribbon + monitored/quality footer, not the title text), so assert
  // on the title attribute of the new movie's poster cell.
  await expect(
    page.getByTestId('movies-grid').locator(`.poster-cell[title="${title}"]`),
  ).toBeVisible()
})

test('the add form shows the quality profile dropdown (R5b)', async ({ page }) => {
  await page.goto('/movies/add')
  await signIn(page)
  await expect(page).toHaveURL(/\/movies\/add$/)
  await expect(page.getByTestId('movie-add-title')).toBeVisible()
  // R5b: the dropdown is populated from the profiles section's movie
  // profiles and defaults to the Radarr factory profile '4K Best Effort'.
  const profile = page.getByTestId('movie-profile')
  await expect(profile).toBeVisible()
  await expect(profile).toHaveValue('4K Best Effort')
})

test('open a movie from the grid into its detail page', async ({ page }) => {
  // Reuse the probe from the previous test if the grid is populated; the
  // grid lists whatever movies exist in the database.
  await page.goto('/movies')
  await signIn(page)
  await expect(page).toHaveURL(/\/movies$/)

  const grid = page.getByTestId('movies-grid')
  // Wait for the list to resolve (grid or the empty state) before deciding
  // whether this database has anything to open.
  await expect(grid.or(page.getByText('No movies'))).toBeVisible()
  if (!(await grid.count())) return // no movies in this database — nothing to open
  const firstCard = page.locator('[data-testid^="movie-card-"]').first()
  await firstCard.click()

  // The R1 detail page: title header, the always-rendered Files section,
  // and the per-card refresh/scan toolbar.
  await expect(page).toHaveURL(/\/movies\/\d+$/)
  await expect(page.getByTestId('movie-detail')).toBeVisible()
  await expect(page.getByTestId('movie-detail-header')).toBeVisible()
  await expect(page.getByTestId('movie-detail-files')).toBeVisible()
})
