import { expect, test, type Page } from '@playwright/test'
import { signIn } from './login'

// Containerized-UI verification (issue #90). These specs run against the
// PR-local image served by the isolated compose stack in CI (the `ui-e2e`
// job in .github/workflows/ci.yml, deploy/compose.ci.yml), or against any
// live stack via E2E_BASE_URL. They cover the browser auth + navigation
// subset the issue calls for:
//
//   * sign in through the REAL login form (not a hand-minted token);
//   * the cross-kind Dashboard renders behind the gate;
//   * each per-kind library page loads without unhandled JS errors.
//
// "Without unhandled errors" is tracked via page.on('pageerror'), which
// fires only on uncaught exceptions / unhandled rejections. We deliberately
// do NOT assert on console 'error' messages: the unauthenticated bounce to
// /login (and the first /api calls before the token exists) produce expected
// 401 network logs that would make a console assertion flaky.

// Per-kind library pages: route -> the testid that proves the page rendered.
const KIND_PAGES: [string, string][] = [
  ['/movies', 'movies-title'],
  ['/tv', 'tv-title'],
  ['/music', 'music-title'],
  ['/books', 'books-title'],
]

// Collect unhandled page errors for the lifetime of the page.
function trackUnhandled(page: Page): string[] {
  const unhandled: string[] = []
  page.on('pageerror', (err) => unhandled.push(err.message))
  return unhandled
}

// Wait for the page to settle so late-firing async unhandled exceptions are
// observed before we assert (the toBeVisible assertions above already waited
// for the render; this only adds a short tail for async rejection flushes).
async function settle(page: Page): Promise<void> {
  await page.waitForTimeout(1000)
}

test('signs in through the real login form and the dashboard renders', async ({ page }) => {
  const unhandled = trackUnhandled(page)

  // Deep-link to the Dashboard while unauthenticated: AuthGate bounces us to
  // /login, and sign-in's returnTo brings us client-side back to /dashboard.
  await page.goto('/dashboard')
  await expect(page).toHaveURL(/\/login$/)
  await signIn(page)
  await expect(page).toHaveURL(/\/dashboard$/)

  // The dashboard rendered its live chrome (not the error card).
  await expect(page.getByTestId('refresh')).toBeVisible()
  await expect(page.getByTestId('dashboard-error')).toHaveCount(0)

  await settle(page)
  expect(unhandled, `unhandled JS errors: ${unhandled.join('; ')}`).toEqual([])
})

for (const [route, testId] of KIND_PAGES) {
  test(`per-kind page ${route} loads without unhandled errors`, async ({ page }) => {
    const unhandled = trackUnhandled(page)

    // Unauthenticated deep link: the gate bounces to /login, sign-in returns
    // client-side to the target route (same pattern as the other e2e specs).
    await page.goto(route)
    await expect(page).toHaveURL(/\/login$/)
    await signIn(page)
    // Unanchored tail match (house style, see auth.spec.ts): toHaveURL tests
    // the pattern against the FULL url (http://host:8080/movies), so a ^
    // anchor would never match.
    await expect(page).toHaveURL(new RegExp(`${route}$`))

    // The page rendered its real header (not an error card).
    await expect(page.getByTestId(testId)).toBeVisible()

    await settle(page)
    expect(unhandled, `unhandled JS errors on ${route}: ${unhandled.join('; ')}`).toEqual([])
  })
}
