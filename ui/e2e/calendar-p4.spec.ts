import { expect, test, type Page } from '@playwright/test'
import { E2E_PASSWORD, E2E_USER, signIn } from './login'

// P4 Calendar e2e, run against the running dev stack (Vite :5173 -> backend
// :8081 via the /api proxy). The dev stack may still be a pre-P4 build, in which
// case the new /api/v1/calendar + /calendar.ics routes 404 and the page surfaces
// its error state. The spec is build-agnostic: the page must render its base
// shell + toolbar + week strip and settle into one of (loaded | empty | error);
// the expected settled state is asserted from a direct endpoint probe.
//
// The unit suite (vitest) covers the full data + view + iCal flows against a
// mocked client; this spec verifies the page integrates with the live backend
// and degrades gracefully (error-gated, no empty/rows) when the route is absent.

async function apiToken(page: Page): Promise<string> {
  const res = await page.request.post('/api/v1/auth/login', {
    data: { username: E2E_USER, password: E2E_PASSWORD },
  })
  if (!res.ok()) throw new Error(`e2e login failed: ${res.status()}`)
  const body = (await res.json()) as { token: string }
  return body.token
}

// Probe whether the P4 calendar endpoint is live (200) on the running backend.
async function calendarEndpointLive(page: Page): Promise<boolean> {
  const token = await apiToken(page)
  const res = await page.request.get('/api/v1/calendar', {
    headers: { Authorization: `Bearer ${token}` },
  })
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

test('calendar page renders the shell + toolbar + week strip', async ({ page }) => {
  await page.goto('/calendar')
  await signIn(page)
  await expect(page).toHaveURL(/\/calendar$/)
  await expect(page.getByTestId('calendar')).toBeVisible()
  // Toolbar: iCal Link + the five view toggles.
  await expect(page.getByTestId('calendar-ical')).toBeVisible()
  for (const v of ['month', 'week', 'forecast', 'day', 'agenda']) {
    await expect(page.getByTestId(`calendar-view-${v}`)).toBeVisible()
  }
  // Week strip: Sun–Sat + Today.
  await expect(page.getByTestId('calendar-weekstrip')).toBeVisible()
  await expect(page.getByTestId('calendar-today')).toBeVisible()
})

test('calendar page settles (week view on P4, error on pre-P4)', async ({ page }) => {
  await page.goto('/calendar')
  await signIn(page)
  await expect(page).toHaveURL(/\/calendar$/)
  await expect(page.getByTestId('calendar')).toBeVisible()
  const live = await calendarEndpointLive(page)
  const which = await race(page, 'calendar-week', 'calendar-empty', 'calendar-error')
  if (which === 'pending') throw new Error('calendar settled into no recognised state')
  if (live) expect(['loaded', 'empty']).toContain(which)
  else expect(which).toBe('error') // documented: running backend predates the P4 endpoint
})

test('iCal Link exposes the token-authed feed URL', async ({ page }) => {
  await page.goto('/calendar')
  await signIn(page)
  await expect(page).toHaveURL(/\/calendar$/)
  await expect(page.getByTestId('calendar')).toBeVisible()
  // Clicking the iCal Link button reveals the token-authed feed URL (a plain
  // navigation can't set the Authorization header, so the token rides as ?token=).
  await page.getByTestId('calendar-ical').click()
  const url = await page.getByTestId('calendar-ical-url').inputValue()
  expect(url).toContain('/calendar.ics')
  expect(url).toContain('token=')
})
