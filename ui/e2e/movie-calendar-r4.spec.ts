import { expect, test, type Page } from '@playwright/test'
import { E2E_PASSWORD, E2E_USER, signIn } from './login'

// Radarr R4: /movies/calendar (the movie calendar). Mirrors the P4 TV
// calendar spec: the page must render its base shell + toolbar + status
// chips + week strip and settle into one of (loaded | empty | error); the
// expected settled state is asserted from a direct endpoint probe. Keyless
// dev has no TMDB release dates, so the empty state is a legitimate settled
// state here. Never require live TMDB data in this spec.

async function apiToken(page: Page): Promise<string> {
  const res = await page.request.post('/api/v1/auth/login', {
    data: { username: E2E_USER, password: E2E_PASSWORD },
  })
  if (!res.ok()) throw new Error(`e2e login failed: ${res.status()}`)
  const body = (await res.json()) as { token: string }
  return body.token
}

// Probe whether the R4 movie calendar endpoint is live (200) on the running
// backend (the same route as P4's, with the kind query added).
async function movieCalendarEndpointLive(page: Page): Promise<boolean> {
  const token = await apiToken(page)
  const auth = ['Bearer', token].join(' ')
  const res = await page.request.get('/api/v1/calendar?kind=movie', {
    headers: { Authorization: auth },
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

test('movies rail shows the Calendar sub-link', async ({ page }) => {
  await page.goto('/movies')
  await signIn(page)
  await expect(page).toHaveURL(/\/movies$/)

  // Sub-items are revealed while the route is within the /movies scope.
  await expect(page.getByTestId('rail-sub-moviesadd')).toBeVisible()
  await expect(page.getByTestId('rail-sub-moviescollections')).toBeVisible()
  await expect(page.getByTestId('rail-sub-moviesdiscover')).toBeVisible()
  await expect(page.getByTestId('rail-sub-moviescalendar')).toBeVisible()
})

test('movie calendar rail link navigates to /movies/calendar', async ({ page }) => {
  await page.goto('/movies')
  await signIn(page)
  await expect(page).toHaveURL(/\/movies$/)
  await page.getByTestId('rail-sub-moviescalendar').click()
  await expect(page).toHaveURL(/\/movies\/calendar$/)
  await expect(page.getByTestId('calendar')).toBeVisible()
})

test('movie calendar page renders the shell + toolbar + status chips + week strip', async ({
  page,
}) => {
  await page.goto('/movies/calendar')
  await signIn(page)
  await expect(page).toHaveURL(/\/movies\/calendar$/)
  await expect(page.getByTestId('calendar')).toBeVisible()
  // Toolbar: iCal Link + the five view toggles (shared with the TV page).
  await expect(page.getByTestId('calendar-ical')).toBeVisible()
  for (const v of ['month', 'week', 'forecast', 'day', 'agenda']) {
    await expect(page.getByTestId(`calendar-view-${v}`)).toBeVisible()
  }
  // R4: the Radarr movie status chips (movie-only; the TV page has none).
  for (const chip of ['downloaded-monitored', 'missing-monitored', 'queued', 'unreleased']) {
    await expect(page.getByTestId(`calendar-chip-${chip}`)).toBeVisible()
  }
  // Week strip: Sun–Sat + Today.
  await expect(page.getByTestId('calendar-weekstrip')).toBeVisible()
  await expect(page.getByTestId('calendar-today')).toBeVisible()
})

test('movie calendar page settles (week view or empty; error only if the endpoint is absent)', async ({
  page,
}) => {
  await page.goto('/movies/calendar')
  await signIn(page)
  await expect(page).toHaveURL(/\/movies\/calendar$/)
  await expect(page.getByTestId('calendar')).toBeVisible()
  const live = await movieCalendarEndpointLive(page)
  const which = await race(page, 'calendar-week', 'calendar-empty', 'calendar-error')
  if (which === 'pending') throw new Error('movie calendar settled into no recognised state')
  if (live) expect(['loaded', 'empty']).toContain(which)
  else expect(which).toBe('error') // documented: running backend predates the endpoint
})

test('iCal Link exposes the movie feed URL (kind=movie + token)', async ({ page }) => {
  await page.goto('/movies/calendar')
  await signIn(page)
  await expect(page).toHaveURL(/\/movies\/calendar$/)
  await expect(page.getByTestId('calendar')).toBeVisible()
  // Clicking the iCal Link button reveals the token-authed movie feed URL
  // (a plain navigation can't set the Authorization header, so the token
  // rides as ?token=).
  await page.getByTestId('calendar-ical').click()
  const url = await page.getByTestId('calendar-ical-url').inputValue()
  expect(url).toContain('/calendar.ics')
  expect(url).toContain('kind=movie')
  expect(url).toContain('token=')
})
