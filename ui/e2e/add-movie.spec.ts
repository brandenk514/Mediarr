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
  await page.getByTestId('movie-title-input').fill(title)
  await page.getByTestId('movie-add-button').click()

  await expect(page).toHaveURL(/\/movies$/)
  await expect(page.getByTestId('movies-grid')).toBeVisible()
  // The new movie is a poster card in the grid (its title attribute is the
  // movie title — no year, so no " (year)" suffix).
  await expect(page.getByTestId('movies-grid').getByText(title)).toBeVisible()
})

test('open a movie from the grid into its detail page', async ({ page }) => {
  // Reuse the probe from the previous test if the grid is populated; the
  // grid lists whatever movies exist in the database.
  await page.goto('/movies')
  await signIn(page)
  await expect(page).toHaveURL(/\/movies$/)

  const grid = page.getByTestId('movies-grid')
  if (!(await grid.count())) return // no movies in this database — nothing to open
  const firstCard = page.locator('[data-testid^="movie-card-"]').first()
  await firstCard.click()

  // The detail page: title header, toolbar, and the release (wanted) state.
  await expect(page).toHaveURL(/\/movies\/\d+$/)
  await expect(page.getByTestId('movie-detail')).toBeVisible()
  await expect(page.getByTestId('movie-detail-header')).toBeVisible()
  await expect(page.getByTestId('movie-detail-release')).toBeVisible()
})
