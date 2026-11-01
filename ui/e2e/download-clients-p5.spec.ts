import { expect, test } from '@playwright/test'
import { signIn } from './login'

// Prowlarr PR5 e2e: the plural Download Clients list flow over the DEDICATED
// plural endpoints (not the generic per-section document), run against the
// running dev stack (Vite :5173 -> backend :8081). Protected-route-first:
// goto /settings/download-clients (AuthGate returnTo), sign in, the app
// returns us to the section. The auth token lives in page memory only, so we
// never page.goto a protected route after sign-in.
//
// DB-state-independent: we add a client with a timestamped name, verify it by
// name, run Test All (the probe to a closed local port fails fast —
// connection refused — so a result cell appears rather than a 5xx), then
// delete it. We never assume any seeded rows, and we clean up after ourselves.
test('Download Clients: add -> row -> test-all -> delete', async ({ page }) => {
  await page.goto('/settings/download-clients')
  await signIn(page)
  await expect(page).toHaveURL(/\/settings\/download-clients$/)
  await expect(page.getByTestId('dc-card')).toBeVisible()

  // Timestamped so local runs are distinguishable in the list.
  const name = `E2E P5 DC ${Date.now()}`

  // The toolbar Add button is present in every state (empty and table).
  await page.getByTestId('dcl-add').click()
  await expect(page.getByTestId('dcl-modal')).toBeVisible()
  await page.getByTestId('dcl-f-name').fill(name)
  // kind defaults to qbittorrent. A closed local port makes the test probe
  // fail fast (connection refused) instead of hanging on the 5s probe timeout.
  await page.getByTestId('dcl-f-base').fill('http://127.0.0.1:9')
  await page.getByTestId('dcl-f-user').fill('admin')
  await page.getByTestId('dcl-save').click()

  // The new row appears by name; recover its id from the row testid for the
  // per-row action buttons below.
  const row = page.getByTestId(/^dcl-row-/).filter({ hasText: name })
  await expect(row).toBeVisible()
  const rowTestId = (await row.getAttribute('data-testid')) ?? ''
  const id = Number(rowTestId.replace('dcl-row-', ''))
  expect(Number.isNaN(id)).toBe(false)

  // Test All Clients is enabled now that there is at least one client. The
  // probe fails (no real client at the closed port); the section must render
  // the outcome, not crash. The last-test cell leaves '—'.
  await expect(page.getByTestId('dcl-test-all')).toBeEnabled()
  await page.getByTestId('dcl-test-all').click()
  await expect(page.getByTestId(`dcl-last-test-${id}`)).not.toHaveText('—', {
    timeout: 15000,
  })

  // Delete the client we added -> the row disappears (DB left clean).
  await page.getByTestId(`dcl-del-${id}`).click()
  await expect(page.getByText(name, { exact: true })).toBeHidden()
})
