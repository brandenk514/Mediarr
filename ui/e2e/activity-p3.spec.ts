import { expect, test, type Page } from '@playwright/test'
import { E2E_PASSWORD, E2E_USER, signIn } from './login'

// P3 Activity + Wanted e2e, run against the running dev stack (Vite :5173 →
// backend :8081 via the /api proxy). The dev stack may still be a pre-P3 build,
// in which case the new /blocklist, /history, /wanted routes 404 and the pages
// surface their error state. The specs are build-agnostic: every page must
// render its base shell and settle into one of (loaded | empty | error), and
// the expected settled state is asserted from a direct endpoint probe.
//
// The unit suite (vitest) covers the full data + action flows (search,
// unmonitor, unblock, manual import, grab/remove) against mocked clients; this
// spec verifies the pages integrate with the live backend and degrade
// gracefully when a route is absent.

async function apiToken(page: Page): Promise<string> {
  const res = await page.request.post('/api/v1/auth/login', {
    data: { username: E2E_USER, password: E2E_PASSWORD },
  })
  if (!res.ok()) throw new Error(`e2e login failed: ${res.status()}`)
  const body = (await res.json()) as { token: string }
  return body.token
}

// Probe whether a P3 endpoint is live (200) on the running backend.
async function endpointLive(page: Page, path: string): Promise<boolean> {
  const token = await apiToken(page)
  const res = await page.request.get(path, { headers: { Authorization: `Bearer ${token}` } })
  return res.status() === 200
}

// Wait for whichever settled state appears first and report it.
async function race(
  page: Page,
  loaded: string,
  empty: string,
  error: string,
): Promise<'loaded' | 'empty' | 'error' | 'pending'> {
  try {
    return await Promise.race([
      page.getByTestId(loaded).waitFor({ state: 'visible', timeout: 8000 }).then(() => 'loaded' as const),
      page.getByTestId(empty).waitFor({ state: 'visible', timeout: 8000 }).then(() => 'empty' as const),
      page.getByTestId(error).waitFor({ state: 'visible', timeout: 8000 }).then(() => 'error' as const),
    ])
  } catch {
    return 'pending'
  }
}

test('queue page loads the live queue (route exists pre-P3)', async ({ page }) => {
  await page.goto('/activity/queue')
  await signIn(page)
  await expect(page).toHaveURL(/\/activity\/queue$/)
  await expect(page.getByTestId('queue-title')).toBeVisible()
  const which = await race(page, 'queue-count', 'queue-empty', 'queue-error')
  expect(['loaded', 'empty', 'error']).toContain(which)
  if (which === 'loaded') await expect(page.getByTestId('queue-count')).toBeVisible()
})

test('blocklist page renders and settles (empty on P3, error on pre-P3)', async ({ page }) => {
  await page.goto('/activity/blocklist')
  await signIn(page)
  await expect(page).toHaveURL(/\/activity\/blocklist$/)
  await expect(page.getByTestId('blocklist')).toBeVisible()
  await expect(page.getByTestId('blocklist-kind')).toBeVisible()
  const live = await endpointLive(page, '/api/v1/blocklist')
  const which = await race(page, 'blocklist-table', 'blocklist-empty', 'blocklist-error')
  if (which === 'pending') throw new Error('blocklist settled into no recognised state')
  if (live) expect(['loaded', 'empty']).toContain(which)
  else expect(which).toBe('error') // documented: running backend predates the P3 endpoint
})

test('history page renders and settles (feed on P3, error on pre-P3)', async ({ page }) => {
  await page.goto('/activity/history')
  await signIn(page)
  await expect(page).toHaveURL(/\/activity\/history$/)
  await expect(page.getByTestId('history')).toBeVisible()
  await expect(page.getByTestId('history-event')).toBeVisible()
  const live = await endpointLive(page, '/api/v1/history')
  const which = await race(page, 'history-feed', 'history-empty', 'history-error')
  if (which === 'pending') throw new Error('history settled into no recognised state')
  if (live) expect(['loaded', 'empty']).toContain(which)
  else expect(which).toBe('error')
})

test('wanted page renders and settles (table on P3, error on pre-P3)', async ({ page }) => {
  await page.goto('/wanted/missing')
  await signIn(page)
  await expect(page).toHaveURL(/\/wanted\/missing$/)
  await expect(page.getByTestId('wanted-missing')).toBeVisible()
  await expect(page.getByTestId('wanted-tab-missing')).toBeVisible()
  const live = await endpointLive(page, '/api/v1/wanted?tab=missing')
  const which = await race(page, 'wanted-table-missing', 'wanted-empty-missing', 'wanted-error-missing')
  if (which === 'pending') throw new Error('wanted settled into no recognised state')
  if (live) expect(['loaded', 'empty']).toContain(which)
  else expect(which).toBe('error')
})
