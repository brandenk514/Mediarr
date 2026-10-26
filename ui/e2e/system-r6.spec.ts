import { expect, test, type Page } from '@playwright/test'
import { E2E_PASSWORD, E2E_USER, signIn } from './login'

// Radarr R6 e2e (minimal): the System page reaches the drop-in migration
// panel and the source toggle (Sonarr | Radarr) is present. The full panel
// flows (per-source helper text, the submitted payload) are covered by the
// MigrationPanel vitest suite against mocked clients.

async function apiToken(page: Page): Promise<string> {
  const res = await page.request.post('/api/v1/auth/login', {
    data: { username: E2E_USER, password: E2E_PASSWORD },
  })
  if (!res.ok()) throw new Error(`e2e login failed: ${res.status()}`)
  const body = (await res.json()) as { token: string }
  return body.token
}

test('system import panel renders with the R6 source toggle', async ({ page }) => {
  await page.goto('/system/import')
  await signIn(page)
  await expect(page).toHaveURL(/\/system\/import$/)

  // The panel settles into its card (the status endpoint is P6-era, so it is
  // live on any current backend).
  const card = page.getByTestId('system-migration')
  await card.waitFor({ state: 'visible', timeout: 10000 })

  // R6: the source toggle is present, defaulting to Sonarr.
  const source = page.getByTestId('migration-source')
  await expect(source).toBeVisible()
  await expect(source).toHaveValue('sonarr')
  await expect(page.getByTestId('migration-source-help')).toContainText('Sonarr')

  // Switching to Radarr updates the per-source helper text.
  await source.selectOption('radarr')
  await expect(page.getByTestId('migration-source-help')).toContainText('Radarr')
})
