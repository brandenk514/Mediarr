import { expect, test } from '@playwright/test'
import { signIn } from './login'

// Global search (issue #117): the top-bar search is functional — type a
// query, Enter (or the submit affordance), land on the first-class,
// deep-linkable results route /search/all?q=… . The page fans out
// client-side to the four per-kind endpoints and renders ONE unified grid
// in which every cell carries its kind badge (TV / Movies / Music / Books).
//
// The auth token lives in page memory, so each test signs in first and then
// lets the app's own (client-side) navigation carry the flow — never
// page.goto after sign-in.
//
// Provider note: Books (Open Library) is keyless and resolves in every
// environment. TV (TVDB) and Movies (TMDB) return a 503 "provider not
// configured" note on a keyless dev stack. Music (MusicBrainz) is keyless
// too, but the public service rate-limits aggressively ("server currently
// busy", 503) — so the specs below assert the deterministic contract:
// per-kind settled state is either hits (badged cells in the unified grid)
// or an inline error note, and the grid holds exactly the sum of the
// settled per-kind counts (the cross-kind merge).

const Q = 'Radiohead'

// Settle all four fan-out legs: no status chip left in "searching…".
async function waitForFanoutSettle(page: import('@playwright/test').Page): Promise<void> {
  await expect
    .poll(async () => (await page.getByTestId('global-search-status').textContent()) ?? '')
    .not.toContain('searching…')
}

test('top-bar search navigates to the deep-linkable results route', async ({ page }) => {
  await page.goto('/tv')
  await signIn(page)
  await expect(page).toHaveURL(/\/tv$/)

  const input = page.getByTestId('global-search')
  const submit = page.getByTestId('global-search-submit')

  // Empty draft: the affordance is disabled and nothing fires.
  await expect(submit).toBeDisabled()

  await input.fill(Q)
  await expect(submit).toBeEnabled()
  await input.press('Enter')

  // The results route keeps the query in the URL (deep-linkable), reached by
  // the app's client-side navigation (the in-memory token is preserved).
  await expect(page).toHaveURL(/\/search\/all\?q=Radiohead/)
  await expect(page.getByTestId('global-search-title')).toBeVisible()
  await expect(page.getByTestId('global-search-query')).toHaveText(`Results for \u201C${Q}\u201D`)
})

test('submit affordance (button) works, not just Enter', async ({ page }) => {
  await page.goto('/tv')
  await signIn(page)
  await expect(page).toHaveURL(/\/tv$/)

  await page.getByTestId('global-search').fill(Q)
  await page.getByTestId('global-search-submit').click()
  await expect(page).toHaveURL(/\/search\/all\?q=Radiohead/)
  await expect(page.getByTestId('global-search-title')).toBeVisible()
})

test('results fan out cross-kind into one unified, badged grid', async ({ page }) => {
  await page.goto('/tv')
  await signIn(page)
  await expect(page).toHaveURL(/\/tv$/)

  const pageErrors: string[] = []
  page.on('pageerror', (err) => pageErrors.push(String(err)))

  await page.getByTestId('global-search').fill(Q)
  await page.getByTestId('global-search-submit').click()
  await expect(page).toHaveURL(/\/search\/all\?q=Radiohead/)
  await waitForFanoutSettle(page)

  // Read the settled per-kind counts from the status chips
  // ("<Kind> · <n>" when done, "<Kind> · failed" when the endpoint errored).
  const kinds = ['tv', 'movie', 'music', 'book'] as const
  const countOf: Record<string, number> = {}
  for (const k of kinds) {
    const text = (await page.getByTestId(`global-search-status-${k}`).textContent()) ?? ''
    const m = text.match(/·\s*(\d+)$/u)
    countOf[k] = m ? Number(m[1]) : 0
  }

  // Books (Open Library, keyless) resolves in every environment: at least
  // one real, badged result must land in the grid.
  expect(countOf.book).toBeGreaterThan(0)
  await expect(page.getByTestId(/^global-search-card-book-\d+$/).first()).toBeVisible()

  // The unified grid holds EXACTLY the sum of the settled per-kind counts —
  // one badged cell per hit, across all four kinds (the merge contract).
  const total = kinds.reduce((sum, k) => sum + countOf[k], 0)
  const grid = page.getByTestId('global-search-grid')
  await expect(grid).toBeVisible()
  await expect(page.getByTestId(/^global-search-card-(tv|movie|music|book)-\d+$/)).toHaveCount(total)

  // Every kind that has hits is badged in the grid. (MusicBrainz adds its
  // cells whenever it is not rate-limiting the stack; when it is, the
  // per-kind isolation test covers that outcome.)
  const LABEL: Record<string, string> = { tv: 'TV', movie: 'Movies', music: 'Music', book: 'Books' }
  for (const k of kinds) {
    if (countOf[k] > 0) {
      await expect(grid).toContainText(LABEL[k])
    }
  }
  await expect(page.getByTestId(/^global-search-card-book-\d+$/).first()).toContainText('Books')

  // A provider failure (or outage) must not have crashed the page.
  expect(pageErrors).toHaveLength(0)
})

test('per-kind isolation: a failed kind renders an inline note, others still render', async ({
  page,
}) => {
  await page.goto('/tv')
  await signIn(page)
  await expect(page).toHaveURL(/\/tv$/)

  await page.getByTestId('global-search').fill(Q)
  await page.getByTestId('global-search-submit').click()
  await expect(page).toHaveURL(/\/search\/all\?q=Radiohead/)
  await waitForFanoutSettle(page)

  // For every kind whose leg failed — a 503 "provider not configured" on a
  // keyless dev stack (TV/Movies), or an upstream provider outage — the page
  // renders an inline error note instead of a blank/missing grid.
  for (const kind of ['tv', 'movie', 'music', 'book']) {
    const chip = page.getByTestId(`global-search-status-${kind}`)
    const failed = (await chip.textContent())?.includes('failed')
    if (failed) {
      await expect(page.getByTestId(`global-search-error-${kind}`)).toBeVisible()
    }
  }

  // The non-failed kinds' results are still in the unified grid.
  await expect(page.getByTestId('global-search-grid')).toBeVisible()
})

test('deep link to the results route survives sign-in at the route level', async ({ page }) => {
  // The full pathname (without the query — Login.tsx's returnTo carries the
  // pathname, matching the app's existing deep-link behavior) bounces to
  // /login while signed out; sign-in lands back on the results route, which
  // renders its idle state there.
  await page.goto('/search/all')
  await signIn(page)
  await expect(page).toHaveURL(/\/search\/all$/)
  await expect(page.getByTestId('global-search-idle')).toBeVisible()
})
