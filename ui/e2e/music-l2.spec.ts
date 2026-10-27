import { expect, test, type APIRequestContext } from '@playwright/test'
import { E2E_PASSWORD, E2E_USER, signIn } from './login'

// Lidarr L2: the Add Artist flow (search page + "Add New Artist" modal) and
// the full Edit Artist modal on the detail page.
//
// DB-state-independent: the detail-page tests seed a probe artist through
// the API only when the music library is empty (the long-lived dev DB
// accumulates probe rows). The search-page test exercises the real
// MusicBrainz path — when the provider is unreachable in an environment
// (502/503) the search assertion is skipped rather than failing, the same
// way the R2 movie e2e skips the live TMDB box.

const PROBE_ARTIST = 'E2E Lidarr L2 Probe'

async function ensureProbeArtist(request: APIRequestContext): Promise<void> {
  const login = await request.post('/api/v1/auth/login', {
    data: { username: E2E_USER, password: E2E_PASSWORD },
  })
  const token = ((await login.json()) as { token: string }).token
  const headers = { Authorization: `Bearer ${token}` }
  const list = await request.get('/api/v1/music/artists', { headers })
  const body = (await list.json()) as { items?: unknown[] }
  if (!body.items || body.items.length === 0) {
    await request.post('/api/v1/music/artists', {
      headers,
      data: { name: PROBE_ARTIST, mbid: 'e2e-lidarr-l2-probe' },
    })
  }
}

test('the add page renders the Lidarr shell (placeholder, id lookup hint, back link)', async ({
  page,
}) => {
  await page.goto('/music/add')
  await signIn(page)
  await expect(page).toHaveURL(/\/music\/add$/)

  await expect(page.getByTestId('artist-add-title')).toBeVisible()
  await expect(page.getByTestId('artist-search-input')).toBeVisible()
  // The placeholder shows an example artist name followed by 'lidarr:' and
  // the id hint.
  await expect(page.getByTestId('artist-search-input')).toHaveAttribute(
    'placeholder',
    /lidarr:/,
  )
  await expect(page.getByTestId('artist-search-help')).toContainText(
    'MusicBrainz id',
  )
  await expect(page.getByTestId('artist-add-back')).toBeVisible()
})

test('search renders results and the Add New Artist modal has the Lidarr fields', async ({
  page,
}) => {
  await page.goto('/music/add')
  await signIn(page)
  await expect(page).toHaveURL(/\/music\/add$/)

  await page.getByTestId('artist-search-input').fill('Radiohead')

  // Wait for either results or the provider-failure line. MusicBrainz is
  // keyless but rate-limited: a 502/503 from the backend maps to the
  // error/unavailable note, and this environment assertion is skipped
  // rather than failing (the R2 e2e treats the live provider the same).
  await expect(
    page
      .getByTestId('artist-results')
      .or(page.getByTestId('artist-search-error'))
      .or(page.getByTestId('artist-search-unavailable')),
  ).toBeVisible({ timeout: 30_000 })

  const providerOk =
    (await page.getByTestId('artist-results').count()) > 0 &&
    (await page.locator('[data-testid^="artist-result-"]').count()) > 0
  if (!providerOk) {
    test.skip(true, 'MusicBrainz search unavailable or empty in this environment')
    return
  }

  // An already-added artist renders as a jump link; a plain artist row is a
  // button-like div. Click the first plain row to open the add modal (fall
  // back to the first row of any kind if every hit is already in the
  // library — the modal then re-opens on the same artist).
  const plainRows = page.locator('div[data-testid^="artist-result-"]')
  const anyRow = page.locator('[data-testid^="artist-result-"]')
  await expect(anyRow).not.toHaveCount(0)
  const target = (await plainRows.count()) > 0 ? plainRows.first() : anyRow.first()
  await target.click()

  // The "Add New Artist" modal: artwork placeholder + name, then the
  // fields in Lidarr's order (Root Folder · Monitor · Monitor New Albums ·
  // Quality Profile · Tags) with the start-search footer.
  await expect(page.getByTestId('add-artist-modal')).toBeVisible()
  await expect(page.getByTestId('add-artist-artwork')).toBeVisible()
  await expect(page.getByTestId('add-artist-root-folder')).toBeVisible()
  await expect(page.getByTestId('add-artist-root-folder-help')).toContainText(
    'subfolder will be created automatically',
  )
  await expect(page.getByTestId('add-artist-monitor-policy')).toBeVisible()
  await expect(page.getByTestId('add-artist-monitor-policy-help')).toContainText(
    'one-time adjustment',
  )
  await expect(page.getByTestId('add-artist-monitor-new-albums')).toBeVisible()
  await expect(page.getByTestId('add-artist-monitor-new-albums-help')).toContainText(
    'Which new albums should be monitored',
  )
  await expect(page.getByTestId('add-artist-quality-profile')).toBeVisible()
  await expect(page.getByTestId('add-artist-tags-input')).toBeVisible()
  await expect(page.getByTestId('artist-start-search')).toBeVisible()
  await expect(page.getByTestId('artist-add-button')).toBeVisible()

  // The 7 monitor options, in Lidarr's labels (including "Lastest").
  const options = await page
    .getByTestId('add-artist-monitor-policy')
    .locator('option')
    .allTextContents()
  await expect(options).toEqual([
    'All Albums',
    'Future Albums',
    'Missing Albums',
    'Existing Albums',
    'First Album',
    'Lastest Album',
    'None',
  ])

  // Cancel: the modal closes, results stay.
  await page.getByTestId('modal-close').click()
  await expect(page.getByTestId('add-artist-modal')).toBeHidden()
})

test('the detail page Edit Artist modal has the full L2 field set', async ({
  page,
  request,
}) => {
  await ensureProbeArtist(request)
  await page.goto('/music')
  await signIn(page)
  await expect(page).toHaveURL(/\/music$/)

  const card = page.locator('[data-testid^="artist-card-"]').first()
  await expect(card).toBeVisible()
  await card.click()
  await expect(page).toHaveURL(/\/music\/\d+$/)

  await page.getByTestId('artist-detail-edit').click()
  await expect(page.getByTestId('edit-modal')).toBeVisible()

  // Monitored · Monitor New Albums · Monitor Policy · Quality Profile ·
  // Metadata Profile (L3 stub, disabled) · Path · Tags.
  await expect(page.getByTestId('edit-monitored')).toBeVisible()
  await expect(page.getByTestId('edit-monitor-new-albums')).toBeVisible()
  await expect(page.getByTestId('edit-monitor-policy')).toBeVisible()
  await expect(page.getByTestId('edit-profile')).toBeVisible()
  await expect(page.getByTestId('edit-metadata-profile')).toBeDisabled()
  await expect(page.getByText('Metadata profiles land in a later phase (L3).')).toBeVisible()
  await expect(page.getByTestId('edit-path')).toBeVisible()
  await expect(page.getByTestId('edit-tags-input')).toBeVisible()

  const options = await page.getByTestId('edit-monitor-policy').locator('option').allTextContents()
  await expect(options).toEqual([
    'All Albums',
    'Future Albums',
    'Missing Albums',
    'Existing Albums',
    'First Album',
    'Lastest Album',
    'None',
  ])

  await page.getByTestId('modal-close').click()
  await expect(page.getByTestId('edit-modal')).toBeHidden()
})
