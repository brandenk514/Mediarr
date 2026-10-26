import { expect, test, type Page } from '@playwright/test'
import { E2E_PASSWORD, E2E_USER, signIn } from './login'

// P5 Settings e2e, run against the running dev stack (Vite :5173 → backend
// :8081 via the /api proxy). The dev stack may still be a P1 build, in which
// case the /settings/:section document routes 404 and each section surfaces
// its error-gated state (the section's own SectionError card — the shell nav
// keeps working, so a section failure never blanks the page). The spec is
// build-agnostic: every section must render inside the 13-section shell and
// settle into one of (loaded | error), with the expected settled state
// asserted from a direct endpoint probe.
//
// The unit suite (vitest) covers the full form flows (save, add/edit,
// P5.7 source-list ordering, score/test/run actions) against mocked clients;
// this spec verifies the pages integrate with the live backend and degrade
// gracefully when a route is absent.

async function apiToken(page: Page): Promise<string> {
  const res = await page.request.post('/api/v1/auth/login', {
    data: { username: E2E_USER, password: E2E_PASSWORD },
  })
  if (!res.ok()) throw new Error(`e2e login failed: ${res.status()}`)
  const body = (await res.json()) as { token: string }
  return body.token
}

// Probe whether a settings section document route is live (200) on the
// running backend.
async function sectionLive(page: Page, slug: string): Promise<boolean> {
  const token = await apiToken(page)
  const res = await page.request.get(`/api/v1/settings/${slug}`, {
    headers: { Authorization: `Bearer ${token}` },
  })
  return res.status() === 200
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

// The five wave-2-3 sections and their (card, error) testid pairs.
const SECTIONS: { slug: string; card: string; error: string }[] = [
  { slug: 'custom-formats', card: 'cf-card', error: 'cf-error' },
  { slug: 'connect', card: 'connect-card', error: 'connect-error' },
  { slug: 'metadata', card: 'metadata-card', error: 'metadata-error' },
  { slug: 'metadata-source', card: 'ms-card', error: 'metadata-source-error' },
  { slug: 'import-lists', card: 'import-lists-card', error: 'import-lists-error' },
]

test('settings shell renders the 13-section nav (the P5 IA contract)', async ({ page }) => {
  await page.goto('/settings/media-management')
  await signIn(page)
  await expect(page).toHaveURL(/\/settings\/media-management$/)
  const slugs = [
    'media-management',
    'profiles',
    'quality',
    'custom-formats',
    'indexers',
    'download-clients',
    'import-lists',
    'connect',
    'metadata',
    'metadata-source',
    'tags',
    'general',
    'ui',
  ]
  for (const s of slugs) {
    await expect(page.getByTestId(`settings-nav-${s}`)).toBeVisible()
  }
})

for (const { slug, card, error } of SECTIONS) {
  test(`settings ${slug} renders and settles (form on P5, error-gated on pre-P5)`, async ({
    page,
  }) => {
    await page.goto(`/settings/${slug}`)
    await signIn(page)
    await expect(page).toHaveURL(new RegExp(`/settings/${slug}$`))
    // The nav stays intact no matter what the section's fetch does.
    await expect(page.getByTestId(`settings-nav-${slug}`)).toBeVisible()
    const live = await sectionLive(page, slug)
    const which = await race(page, card, error)
    if (which === 'pending') throw new Error(`${slug} settled into no recognised state`)
    if (live) expect(which).toBe('loaded')
    else expect(which).toBe('error') // documented: running backend predates the P5 endpoint
  })
}

// Radarr R5b: the modified sections show their Radarr surfaces. The
// assertions are block/heading level (not seeded-value level) so the spec
// stays green whether the running backend is pre-R5a (empty blocks) or
// seeded with the Radarr defaults.
test('radarr R5b settings surfaces render (movie naming, movie profiles, movie quality)', async ({
  page,
}) => {
  await page.goto('/settings/media-management')
  await signIn(page)
  await expect(page).toHaveURL(/\/settings\/media-management$/)
  // Media Management: the Radarr Movie Naming block + the Unmonitor Deleted
  // Movies file-management toggle.
  await expect(page.getByText('Movie naming')).toBeVisible()
  await expect(page.getByTestId('mm-movie-format')).toBeVisible()
  await expect(page.getByTestId('mm-unmonitor')).toBeVisible()

  // Quality: the movie definitions sub-section sits beside the TV table.
  await page.getByTestId('settings-nav-quality').click()
  await expect(page).toHaveURL(/\/settings\/quality$/)
  await expect(page.getByText('TV quality definitions')).toBeVisible()
  await expect(page.getByText('Movie quality definitions')).toBeVisible()

  // Profiles: the movie / delay / release sub-sections render.
  await page.getByTestId('settings-nav-profiles').click()
  await expect(page).toHaveURL(/\/settings\/profiles$/)
  await expect(page.getByRole('heading', { name: 'Movie quality profiles' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Delay profiles' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Release profiles' })).toBeVisible()

  // Import lists: the card reaches with the Radarr bridge in the kind list.
  await page.getByTestId('settings-nav-import-lists').click()
  await expect(page).toHaveURL(/\/settings\/import-lists$/)
  await expect(page.getByTestId('import-lists-card')).toBeVisible()
  await expect(page.getByTestId('il-new-kind')).toHaveValue('trakt')
})
