import { expect, test, type Page } from '@playwright/test'
import { E2E_PASSWORD, E2E_USER, signIn } from './login'

// P6 System e2e, run against the running dev stack (Vite :5173 → backend
// :8081 via the /api proxy). The dev stack may still be a P1 build, in which
// case the /api/v1/system/* routes 404 and each sub-page surfaces its
// error-gated state (the sub-nav keeps working, so a sub-page failure never
// blanks the page). The spec is build-agnostic: every sub-page must render
// inside the 7-item system nav and settle into one of (loaded | error), with
// the expected settled state asserted from a direct endpoint probe.
//
// The unit suite (vitest) covers the full panel flows (backup export/import,
// events filtering, updates highlighting, the SSE log stream, the Sonarr
// migration form + mapping report) against mocked clients; this spec verifies
// the pages integrate with the live backend and degrade gracefully when a
// route is absent.

async function apiToken(page: Page): Promise<string> {
  const res = await page.request.post('/api/v1/auth/login', {
    data: { username: E2E_USER, password: E2E_PASSWORD },
  })
  if (!res.ok()) throw new Error(`e2e login failed: ${res.status()}`)
  const body = (await res.json()) as { token: string }
  return body.token
}

// Probe whether a system endpoint is live (not 404) on the running backend.
async function probeGet(page: Page, path: string): Promise<boolean> {
  const token = await apiToken(page)
  const res = await page.request.get(path, {
    headers: { Authorization: `Bearer ${token}` },
  })
  return res.status() !== 404
}

// The logs endpoint streams (SSE) and never completes, so a plain probe
// would hang: race the request against a short delay — a timeout means the
// stream was accepted, i.e. the route is live.
async function probeLogs(page: Page): Promise<boolean> {
  const token = await apiToken(page)
  const request = page.request.get('/api/v1/system/logs', {
    headers: { Authorization: `Bearer ${token}`, Accept: 'text/event-stream' },
  })
  const status = await Promise.race([
    request.then((r) => r.status()).catch(() => 404),
    new Promise<number>((resolve) => setTimeout(() => resolve(200), 3000)),
  ])
  return status === 200
}

// Wait for whichever settled state appears first and report it.
async function race(
  page: Page,
  loaded: string,
  error: string,
): Promise<'loaded' | 'error' | 'pending'> {
  try {
    return await Promise.race([
      page.getByTestId(loaded).waitFor({ state: 'visible', timeout: 8000 }).then(() => 'loaded' as const),
      page.getByTestId(error).waitFor({ state: 'visible', timeout: 8000 }).then(() => 'error' as const),
    ])
  } catch {
    return 'pending'
  }
}

// The system sub-pages and their (card, error) testid pairs + probe.
const SUBPAGES: {
  slug: string
  card: string
  error: string
  probe: (page: Page) => Promise<boolean>
}[] = [
  { slug: 'status', card: 'system-status', error: 'system-status-error', probe: (p) => probeGet(p, '/api/v1/system/health') },
  { slug: 'tasks', card: 'system-tasks', error: 'system-tasks-error', probe: (p) => probeGet(p, '/api/v1/system/tasks') },
  { slug: 'backup', card: 'system-backup', error: 'system-backup-error', probe: (p) => probeGet(p, '/api/v1/system/backup') },
  { slug: 'updates', card: 'system-updates', error: 'system-updates-error', probe: (p) => probeGet(p, '/api/v1/system/updates') },
  { slug: 'events', card: 'system-events', error: 'system-events-error', probe: (p) => probeGet(p, '/api/v1/system/events') },
  { slug: 'logs', card: 'system-logs', error: 'system-logs-error', probe: probeLogs },
  { slug: 'import', card: 'system-migration', error: 'system-migration-error', probe: (p) => probeGet(p, '/api/v1/system/migration/status') },
]

test('system shell renders the 7-sub-page nav (the P6 IA contract)', async ({ page }) => {
  await page.goto('/system')
  await signIn(page)
  await expect(page).toHaveURL(/\/system\/status$/)
  for (const { slug } of SUBPAGES) {
    await expect(page.getByTestId(`system-nav-${slug}`)).toBeVisible()
  }
})

for (const { slug, card, error, probe } of SUBPAGES) {
  test(`system ${slug} renders and settles (panel on P6, error-gated on pre-P6)`, async ({
    page,
  }) => {
    await page.goto(`/system/${slug}`)
    await signIn(page)
    await expect(page).toHaveURL(new RegExp(`/system/${slug}$`))
    // The sub-nav stays intact no matter what the panel's fetch does.
    await expect(page.getByTestId(`system-nav-${slug}`)).toBeVisible()
    const live = await probe(page)
    const which = await race(page, card, error)
    if (which === 'pending') throw new Error(`${slug} settled into no recognised state`)
    if (live) expect(which).toBe('loaded')
    else expect(which).toBe('error') // documented: running backend predates the P6 endpoint
  })
}

test('system sub-nav navigation keeps the nav intact across sub-pages', async ({ page }) => {
  await page.goto('/system/status')
  await signIn(page)
  await page.getByTestId('system-nav-tasks').click()
  await expect(page).toHaveURL(/\/system\/tasks$/)
  await page.getByTestId('system-nav-import').click()
  await expect(page).toHaveURL(/\/system\/import$/)
  await expect(page.getByTestId('system-nav')).toBeVisible()
})
