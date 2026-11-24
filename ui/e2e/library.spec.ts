import { expect, test, type Page } from '@playwright/test'
import { signIn } from './login'

// Unified cross-kind Library grid (issue #119): /library is the Library
// section's home. It fans out client-side to the four per-kind list endpoints
// and renders one grid grouped into kind sections. The CI stack (app +
// Postgres + Redis, no seeded media) settles the lists to empty, so the "All"
// view shows all four kind sections — the spec asserts that structure, the
// URL-synced kind filter, and the sidebar wiring. The auth token lives in page
// memory: sign in first, then let the app's client-side navigation carry the
// flow (never page.goto after sign-in).

// The "All" view settles when all four kind sections are rendered (each is
// titled regardless of grid / empty-note / per-kind-error content); a
// single-kind view settles when just its one section is. `toHaveCount` is the
// race-free settle signal (it polls until the count matches the view).
async function expectLibrarySettled(page: Page, sections: number): Promise<void> {
  await expect(page.getByTestId('library-title')).toBeVisible()
  await expect(page.getByTestId(/^library-section-title-/)).toHaveCount(sections)
}

test('the unified grid renders at /library with all four kind sections', async ({ page }) => {
  await page.goto('/library')
  await signIn(page)
  await expect(page).toHaveURL(/\/library$/)
  await expectLibrarySettled(page, 4)

  // The toolbar holds exactly the three filters.
  await expect(page.getByTestId('library-kind-btn')).toBeVisible()
  await expect(page.getByTestId('library-genre-btn')).toBeVisible()
  await expect(page.getByTestId('library-status-btn')).toBeVisible()
})

test('the kind filter switches the view and syncs ?kind= in the URL', async ({ page }) => {
  await page.goto('/library')
  await signIn(page)
  await expect(page).toHaveURL(/\/library$/)
  await expectLibrarySettled(page, 4)

  // All → Movies: the URL gains ?kind=movie and only the Movies section
  // remains.
  await page.getByTestId('library-kind-btn').click()
  await page.getByTestId('library-kind-opt-movie').click()
  await expect(page).toHaveURL(/\/library\?kind=movie$/)
  await expectLibrarySettled(page, 1)
  await expect(page.getByTestId('library-section-title-movie')).toBeVisible()

  // Movies → All: the param clears and all four sections come back.
  await page.getByTestId('library-kind-btn').click()
  await page.getByTestId('library-kind-opt-all').click()
  await expect(page).toHaveURL(/\/library$/)
  await expectLibrarySettled(page, 4)
})

test('sidebar: kind row filters the grid; Library header returns to /library', async ({ page }) => {
  await page.goto('/library')
  await signIn(page)
  await expect(page).toHaveURL(/\/library$/)
  await expectLibrarySettled(page, 4)

  // The Movies kind row is a shortcut into the grid filtered to Movies (#119)
  // — its destination is the unified grid, not the per-kind list page.
  await page.getByTestId('rail-sub-movies').click()
  await expect(page).toHaveURL(/\/library\?kind=movie$/)
  await expectLibrarySettled(page, 1)

  // The section header is the Library home: clicking it resets to the
  // unfiltered grid (the ?kind= param is cleared).
  await page.getByTestId('rail-library').click()
  await expect(page).toHaveURL(/\/library$/)
  await expectLibrarySettled(page, 4)
})
