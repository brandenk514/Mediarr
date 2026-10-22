import { expect, test } from '@playwright/test'
import { signIn } from './login'

// Add-movie journey (#42 UI): sign in → /movies/add → type title → Add.
// A successful add navigates to /movies. The TMDB search box is deliberately
// skipped: it depends on an external provider (503 when unconfigured), and
// direct title entry is the idempotent core path.
//
// NOTE (v1): there is no movie delete endpoint, so the probe movie is not
// cleaned up here — the runner removes it from the database after the suite
// (CI runs against a fresh database and needs no cleanup).
test('add a movie through the form and see it in the list', async ({ page }) => {
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
  await expect(page.getByTestId('movies-table')).toBeVisible()

  // Filter the list so the probe row is found regardless of pagination.
  await page.getByTestId('list-search').fill(title)
  await expect(page.getByTestId('movies-table').getByText(title)).toBeVisible()
})
