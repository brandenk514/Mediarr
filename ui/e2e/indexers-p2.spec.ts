import { expect, test } from '@playwright/test'
import { signIn } from './login'

// Prowlarr PR2 e2e: the two-stage Add Indexer flow over the built-in
// definition library, run against the running dev stack (Vite :5173 ->
// backend :8081). Protected-route-first: goto /indexers (AuthGate returnTo),
// sign in, and the app returns us to /indexers (the auth token lives in page
// memory only, so never page.goto a protected route after sign-in).
//
// We create a Generic Torznab indexer and assert the row appears BY NAME,
// not by position/count: the local dev DB accumulates rows across runs, so
// page-1 / index assertions are flaky. We do NOT assert the 584-definition
// count (seed is boot-time; exact counts are brittle) — presence of the
// specific definition row is enough.
test('Add Indexer: picker -> Generic Torznab -> create -> row appears', async ({ page }) => {
  await page.goto('/indexers')
  await signIn(page)
  await expect(page).toHaveURL(/\/indexers$/)

  // Timestamped so local runs are distinguishable in the list.
  const name = `E2E P2 ${Date.now()}`

  // Stage 1: Add Indexer opens the definition picker.
  await page.getByTestId('idx-add').click()
  await expect(page.getByTestId('idx-picker-modal')).toBeVisible()

  // Narrow to the Generic Torznab definition, then pick it.
  await page.getByTestId('idx-picker-search').fill('generic torznab')
  await page.getByTestId('idx-def-generic-torznab').click()

  // Stage 2: picking the definition opens the contract form.
  await expect(page.getByTestId('idx-form-modal')).toBeVisible()

  // generic-torznab has no candidate URLs (baseurl is a required contract
  // field) -> the Base Url field starts empty; fill Name + Base Url.
  await page.getByTestId('idx-name').fill(name)
  await page.getByTestId('idx-base').fill('https://example.org/torznab')
  await page.getByTestId('idx-save').click()

  // The new row appears in the list by name.
  await expect(page.getByText(name, { exact: true })).toBeVisible()
})
