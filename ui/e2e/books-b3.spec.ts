import { expect, test, type APIRequestContext } from '@playwright/test'
import { E2E_PASSWORD, E2E_USER, signIn } from './login'

// Readarr B3: the books "Unmapped Files" e2e — the rail entry and the page
// (a read-only listing of the book files under the books media root not
// mapped to a monitored author, in the Readarr column set Path · Size ·
// Date Added · Quality), plus the B3 metadata-profile selects that replaced
// the B2 disabled stubs in the add-book and edit-author modals.
//
// DB-state independent on the long-lived dev DB: the page's state (rows /
// empty / no-root 404) is whatever the seeded DB + media root produce, so
// the spec probes /api/v1/books/unmapped with a separately logged-in request
// context and asserts the SAME state the API reports (the books-b2 "drive
// from the API" convention) instead of assuming a fixture layout. When the
// provider is unreachable in an environment (502/503 →
// book-search-unavailable) or the search comes back empty, the modal tests
// are skipped rather than failing — the same way books-b2 treats Open Library.

const PROBE_AUTHOR = 'Ursula K. Le Guin'

async function apiToken(request: APIRequestContext): Promise<string> {
  const login = await request.post('/api/v1/auth/login', {
    data: { username: E2E_USER, password: E2E_PASSWORD },
  })
  return ((await login.json()) as { token: string }).token
}

test('the books rail exposes Unmapped Files and the page renders its state', async ({
  page,
  request,
}) => {
  // Hit the protected route FIRST: AuthGate bounces us to /login carrying
  // state.from, and Login.tsx client-side-navigates back after sign-in.
  await page.goto('/books')
  await signIn(page)
  await expect(page).toHaveURL(/\/books$/)

  // The Authors rail section is in scope on /books; its sub-links carry the
  // B3 Unmapped Files entry (after Add New).
  await expect(page.getByTestId('rail-sub-booksunmapped')).toBeVisible()
  await expect(page.getByTestId('rail-sub-booksunmapped')).toContainText('Unmapped Files')

  // Probe the backend's unmapped walk with a separate API context; the page
  // settles into the same state, so assert its rendering against it.
  const token = await apiToken(request)
  const resp = await request.get('/api/v1/books/unmapped', {
    headers: { Authorization: `Bearer ${token}` },
  })

  await page.getByTestId('rail-sub-booksunmapped').click()
  await expect(page).toHaveURL(/\/books\/unmapped$/)

  if (resp.status() === 404) {
    // No books root folder configured: the page surfaces the 404 state with
    // a retry.
    await expect(page.getByTestId('books-unmapped')).toContainText(
      'No books root folder is configured.',
    )
    await expect(
      page.getByTestId('books-unmapped').getByRole('button', { name: 'Retry' }),
    ).toBeVisible()
    return
  }
  expect(resp.status()).toBe(200)
  const files = (await resp.json()) as { path: string }[]

  if (files.length === 0) {
    // Empty walk: the all-mapped note renders, no rows.
    await expect(page.getByTestId('books-unmapped')).toContainText('Nothing is unmapped.')
    await expect(page.getByTestId('books-unmapped')).toContainText(
      'Every book file is mapped to a monitored author.',
    )
    return
  }

  // Populated walk: the Readarr column set and one row per unmapped file.
  const header = page.getByTestId('books-unmapped').locator('.lt-head')
  await expect(header).toContainText('Path')
  await expect(header).toContainText('Size')
  await expect(header).toContainText('Date Added')
  await expect(header).toContainText('Quality')
  await expect(page.locator('[data-testid^="books-unmapped-row-"]')).toHaveCount(files.length)
})

// The add-book modal's metadata-profile select (a disabled stub pre-B3) is
// now live, populated from the seeded book metadata profiles. Provider-gated:
// the modal only opens on a plain (not-added) author search row.
test('the add-book modal carries a live metadata-profile select (B3)', async ({
  page,
}) => {
  await page.goto('/books/add')
  await signIn(page)
  await expect(page).toHaveURL(/\/books\/add$/)

  await page.getByTestId('book-search-input').fill(PROBE_AUTHOR)
  await expect(
    page
      .getByTestId('book-results')
      .or(page.getByTestId('book-search-error'))
      .or(page.getByTestId('book-search-unavailable')),
  ).toBeVisible({ timeout: 30_000 })
  if ((await page.getByTestId('book-results').count()) === 0) {
    test.skip(true, 'Open Library search unavailable or empty in this environment')
    return
  }

  const plainAuthor = page.locator('div[data-testid^="book-result-author-"]')
  if (!(await plainAuthor.count())) {
    test.skip(true, `${PROBE_AUTHOR} is already in the library from a previous run`)
    return
  }
  await plainAuthor.first().click()
  await expect(page.getByTestId('add-book-modal')).toBeVisible()

  // B3: the select is enabled and offers the seeded book metadata profiles
  // (at least the 'Default' alongside the empty choice).
  await expect(page.getByTestId('add-book-metadata-profile')).toBeEnabled()
  const options = await page
    .getByTestId('add-book-metadata-profile')
    .locator('option')
    .allTextContents()
  expect(options).toContain('Default')
})

// The Edit Author modal's metadata-profile select (a disabled stub pre-B3)
// is now live and pre-filled with the author's current profile. Non-mutating:
// it opens the first author in the library and cancels.
//
// Deterministic on any stack (fresh or long-lived): the modal's picker is
// populated lazily from the profiles settings document (book_metadata_profiles)
// — a fresh stack seeds only the single 'Default' profile, so the test seeds
// a second profile via the API (unique name) before opening the modal and
// removes it best-effort afterwards. The assertion then expects both
// profiles, and waits for the lazy population to settle before reading the
// option set (reading it immediately would race the fetch).
interface BookMetadataProfile {
  id: number
  name: string
  description?: string
  [key: string]: unknown
}
interface ProfilesDoc {
  book_metadata_profiles?: BookMetadataProfile[]
  [key: string]: unknown
}

async function profilesDoc(request: APIRequestContext, token: string): Promise<ProfilesDoc> {
  const resp = await request.get('/api/v1/settings/profiles', {
    headers: { Authorization: `Bearer ${token}` },
  })
  expect(resp.status()).toBe(200)
  return (await resp.json()) as ProfilesDoc
}

test('the edit-author modal carries a live metadata-profile select (B3)', async ({
  page,
  request,
}) => {
  const token = await apiToken(request)
  const seededName = `e2e-b3-profile-${Date.now()}`
  let seededAdded = false
  try {
    // Seed a second book metadata profile (a copy of the first, renamed)
    // so the "more than just Default" expectation is deterministic on a
    // fresh stack.
    const doc = await profilesDoc(request, token)
    const profiles = doc.book_metadata_profiles ?? []
    if (!profiles.some((p) => p.name === seededName)) {
      const base = profiles[0] ?? {}
      const nextId = profiles.reduce((max, p) => Math.max(max, p.id), 0) + 1
      doc.book_metadata_profiles = [
        ...profiles,
        { ...base, id: nextId, name: seededName, description: `Seeded by the e2e-b3 spec` },
      ]
      const put = await request.put('/api/v1/settings/profiles', {
        data: doc,
        headers: { Authorization: `Bearer ${token}` },
      })
      expect(put.status()).toBeLessThan(300)
      seededAdded = true
    }

    await page.goto('/books')
    await signIn(page)
    await expect(page).toHaveURL(/\/books$/)

    const grid = page.getByTestId('books-authors-grid')
    await expect(grid).toBeVisible()
    const cards = grid.locator('.poster-cell')
    if (!(await cards.count())) {
      test.skip(true, 'no authors in the library to open')
      return
    }
    await cards.first().click()
    await expect(page).toHaveURL(/\/books\/\d+$/)
    await expect(page.getByTestId('author-detail')).toBeVisible()

    await page.getByTestId('author-detail-edit').click()
    await expect(page.getByTestId('edit-modal')).toBeVisible()
    await expect(page.getByTestId('edit-metadata-profile')).toBeEnabled()
    // The picker populates lazily after the modal opens: wait for the
    // seeded profile's option to appear before reading the option set.
    // (expect.poll, not toBeVisible — options of a closed native select
    // are never "visible" to Playwright's visibility check.)
    const select = page
      .getByTestId('edit-modal')
      .getByTestId('edit-metadata-profile')
    await expect
      .poll(
        async () => (await select.locator('option').allTextContents()),
        { timeout: 10_000 },
      )
      .toContain(seededName)
    const options = await select.locator('option').allTextContents()
    // Both the seeded profiles and the spec's probe profile render — more
    // than just the empty choice.
    expect(options.length).toBeGreaterThan(1)
    expect(options).toContain('Default')
    expect(options).toContain(seededName)
  } finally {
    if (seededAdded) {
      // Best-effort cleanup: drop the seeded profile again. A cleanup
      // failure must not fail the test (the run already proved the point).
      try {
        const doc = await profilesDoc(request, token)
        doc.book_metadata_profiles = (doc.book_metadata_profiles ?? []).filter(
          (p) => p.name !== seededName,
        )
        await request.put('/api/v1/settings/profiles', {
          data: doc,
          headers: { Authorization: `Bearer ${token}` },
        })
      } catch {
        // best-effort
      }
    }
  }
})
