import { expect, test, type APIRequestContext, type Locator, type Page } from '@playwright/test'
import { E2E_PASSWORD, E2E_USER, signIn } from './login'

// Readarr B2: the book add flow e2e — the combined Open Library lookup
// (kind-tagged author/book rows, already-added rows as jump links) and the
// "Add New Author/Book" modal, plus the created rows landing in the library.
//
// DB-state independent on the long-lived dev DB: the probe rows are real
// Open Library hits (the names come from the lookup, not from us), so a
// re-run finds them already added and takes the jump-link branch instead of
// re-adding. When the provider is unreachable in an environment (502/503 →
// book-search-unavailable) or the search comes back empty, the affected
// assertion is skipped rather than failing — the same way music-l2 treats
// MusicBrainz and the R2 e2e treats live TMDB.
//
// Authors are listed by id (insertion order), so an author created by THIS
// run is the last card on the last page of the paginated Authors grid — the
// spec walks there with the pagination control, and additionally verifies
// through the API with a separately logged-in request context (the
// music-l2 ensureProbeArtist pattern).

const PROBE_AUTHOR = 'Ursula K. Le Guin'
const PROBE_BOOK = 'The Dispossessed'
// The Open Library work key for PROBE_BOOK (the repo's canonical probe
// work — the same key the backend MT1 fixtures use). It drives the MT2
// id-lookup edition path: a keyed lookup opens the modal with the
// provider-fed editions (or the manual fallback when OL has none).
const PROBE_WORK_KEY = 'OL59863W'

async function apiToken(request: APIRequestContext): Promise<string> {
  const login = await request.post('/api/v1/auth/login', {
    data: { username: E2E_USER, password: E2E_PASSWORD },
  })
  return ((await login.json()) as { token: string }).token
}

// Type the term, then wait for either the result list or the
// provider-failure note; returns true when real rows are on screen.
async function searchBooks(page: Page, term: string): Promise<boolean> {
  await page.getByTestId('book-search-input').fill(term)
  await expect(
    page
      .getByTestId('book-results')
      .or(page.getByTestId('book-search-error'))
      .or(page.getByTestId('book-search-unavailable')),
  ).toBeVisible({ timeout: 30_000 })
  const rowsVisible =
    (await page.getByTestId('book-results').count()) > 0 &&
    (await page.locator('[data-testid^="book-result-"]').count()) > 0
  if (!rowsVisible) {
    test.skip(true, 'Open Library search unavailable or empty in this environment')
  }
  return rowsVisible
}

// The authors grid is paginated 25/page, ordered by id — the freshest
// author is the last card on the last page. Click Next until it disables.
async function jumpToLastAuthorsPage(page: Page): Promise<void> {
  const next = page.getByTestId('page-next')
  if (!(await next.count())) return
  for (let i = 0; i < 50 && !(await next.isDisabled()); i++) {
    await next.click()
    await page.waitForLoadState('networkidle')
  }
}

test('the add page renders the Readarr shell (search, back link)', async ({ page }) => {
  // Hit the protected route FIRST: AuthGate bounces us to /login carrying
  // state.from, and Login.tsx client-side-navigates back after sign-in.
  // (A page.goto after sign-in is a full load that drops the in-memory
  // token and bounces us back to /login.)
  await page.goto('/books/add')
  await signIn(page)
  await expect(page).toHaveURL(/\/books\/add$/)

  await expect(page.getByTestId('book-add-title')).toBeVisible()
  await expect(page.getByTestId('book-search-input')).toBeVisible()
  await expect(page.getByTestId('book-add-back')).toBeVisible()

  // MT1: the lookup box advertises the id-lookup syntax (client-side hint —
  // no provider round-trip needed).
  await expect(page.getByTestId('book-search-hint')).toContainText('isbn:')
  await expect(page.getByTestId('book-search-hint')).toContainText('olid:')
})

test('combined search renders kind-tagged rows and the modal has the Readarr fields', async ({
  page,
}) => {
  await page.goto('/books/add')
  await signIn(page)
  await expect(page).toHaveURL(/\/books\/add$/)

  if (!(await searchBooks(page, PROBE_AUTHOR))) return

  // Every row carries a kind badge (Author / Book); the probe term returns
  // at least one of them from the combined lookup.
  expect(await page.locator('[data-testid$="-kind"]').count()).toBeGreaterThan(0)

  // An already-added hit renders as a jump link with the In library badge.
  const authorLinks = page.locator('a[data-testid^="book-result-author-"]')
  if (await authorLinks.count()) {
    await expect(authorLinks.first()).toHaveText(/In library/)
  }

  // A plain (not-added) author row opens the "Add New Author" modal.
  const plainAuthor = page.locator('div[data-testid^="book-result-author-"]')
  if (!(await plainAuthor.count())) return
  await plainAuthor.first().click()

  await expect(page.getByTestId('add-book-modal')).toBeVisible()
  await expect(page.getByTestId('add-book-artwork')).toBeVisible()
  await expect(page.getByTestId('add-book-name')).toContainText(PROBE_AUTHOR)
  await expect(page.getByTestId('add-book-root-folder')).toBeVisible()
  await expect(page.getByTestId('add-book-root-folder-help')).toContainText(
    'subfolder will be created automatically',
  )
  await expect(page.getByTestId('add-book-monitor-policy')).toBeVisible()
  await expect(page.getByTestId('add-book-monitor-policy-help')).toContainText('one-time adjustment')
  await expect(page.getByTestId('add-book-quality-profile')).toBeVisible()
  // Metadata profile: B3 enabled this select (the B2 disabled stub is gone) —
  // it is populated from the seeded book metadata profiles.
  await expect(page.getByTestId('add-book-metadata-profile')).toBeEnabled()
  await expect(page.getByTestId('add-book-metadata-profile-help')).toContainText(
    'metadata filters',
  )
  await expect(page.getByTestId('add-book-tags-input')).toBeVisible()
  await expect(page.getByTestId('book-start-search')).toBeVisible()
  await expect(page.getByTestId('book-add-button')).toBeVisible()

  // The 7 monitor options, in the shared (Lidarr-derived) labels.
  const options = await page
    .getByTestId('add-book-monitor-policy')
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
  await expect(page.getByTestId('add-book-modal')).toBeHidden()
})

test('add an author with policy, tags, and start search; it lands in the library', async ({
  page,
  request,
}) => {
  await page.goto('/books/add')
  await signIn(page)
  await expect(page).toHaveURL(/\/books\/add$/)

  if (!(await searchBooks(page, PROBE_AUTHOR))) return

  const plainAuthor = page.locator('div[data-testid^="book-result-author-"]')
  if (!(await plainAuthor.count())) {
    test.skip(true, `${PROBE_AUTHOR} is already in the library from a previous run`)
    return
  }
  await plainAuthor.first().click()
  await expect(page.getByTestId('add-book-modal')).toBeVisible()

  const tag = `e2e-books-${Date.now()}`
  await page.getByTestId('add-book-monitor-policy').selectOption('missing')
  await page.getByTestId('add-book-tags-input').fill(tag)
  await page.getByTestId('add-book-tags-input').press('Enter')
  await expect(page.getByTestId('add-book-tags-chips')).toContainText(tag)
  await page.getByTestId('book-start-search').check()
  await page.getByTestId('book-add-button').click()

  // The full AddAuthorRequest POST: policy, root folder, quality profile,
  // tags and start_search ride one call.
  const [addResponse] = await Promise.all([
    page.waitForResponse(
      (r) => r.url().includes('/api/v1/books/authors') && r.request().method() === 'POST',
    ),
    expect(page).toHaveURL(/\/books$/),
  ])
  expect(addResponse.status()).toBeLessThan(300)

  // It is in the library: the API sees it, and the Authors grid does too —
  // the grid is ordered by id, so it is the last card on the last page.
  const token = await apiToken(request)
  const authors = (await (
    await request.get(`/api/v1/books/authors?q=${encodeURIComponent(PROBE_AUTHOR)}`, {
      headers: { Authorization: `Bearer ${token}` },
    })
  ).json()) as { items: { ID: number; Name: string }[] }
  expect(authors.items.map((a) => a.Name)).toContain(PROBE_AUTHOR)

  await expect(page.getByTestId('books-authors-grid')).toBeVisible()
  await jumpToLastAuthorsPage(page)
  const card = page
    .getByTestId('books-authors-grid')
    .locator(`.poster-cell[title="${PROBE_AUTHOR}"]`)
  await expect(card).toBeVisible()
  await card.click()
  await expect(page).toHaveURL(/\/books\/\d+$/)

  // The fields show on the author detail — pre-filled into the Edit Author
  // modal through the full author-update shape.
  await expect(page.getByTestId('author-detail')).toBeVisible()
  await expect(page.getByTestId('author-detail-name')).toContainText(PROBE_AUTHOR)
  await page.getByTestId('author-detail-edit').click()
  await expect(page.getByTestId('edit-modal')).toBeVisible()
  await expect(page.getByTestId('edit-monitor-policy')).toHaveValue('missing')
  await expect(page.getByTestId('edit-tags-chips')).toContainText(tag)
})

test('add a book through a book hit; the title lands monitored under its author', async ({
  page,
  request,
}) => {
  await page.goto('/books/add')
  await signIn(page)
  await expect(page).toHaveURL(/\/books\/add$/)

  if (!(await searchBooks(page, PROBE_BOOK))) return

  // The combined lookup is fuzzy and its ordering drifts with Open Library's
  // data, so target the probe work by its byline among the plain (not-added)
  // book rows rather than assuming it is first.
  const plainBooks = page.locator('div[data-testid^="book-result-book-"]')
  let probeRow: Locator | null = null
  for (let i = 0; i < (await plainBooks.count()); i++) {
    const row = plainBooks.nth(i)
    if ((await row.textContent())?.includes(`by ${PROBE_AUTHOR}`)) {
      probeRow = row
      break
    }
  }
  if (probeRow) {
    // Book row: the Book kind badge and the work's author as its byline.
    await expect(probeRow).toContainText('Book')
    await expect(probeRow).toContainText(`by ${PROBE_AUTHOR}`)
    await probeRow.click()
    await expect(page.getByTestId('add-book-modal')).toBeVisible()
    await expect(page.getByTestId('add-book-byline')).toContainText(PROBE_AUTHOR)

    // MT2: a plain-text work has no work key to fetch editions with, so the
    // modal offers the manual edition fallback with its note (the automatic
    // editions fetch is never attempted for a non-key query).
    await expect(page.getByTestId('add-book-editions-section')).toBeVisible()
    await expect(page.getByTestId('add-book-edition-manual')).toBeVisible()
    await expect(page.getByTestId('add-book-editions-note')).toContainText('work-key lookups')

    const [titleResponse] = await Promise.all([
      page.waitForResponse(
        (r) =>
          /\/api\/v1\/books\/authors\/\d+\/titles/.test(r.url()) &&
          r.request().method() === 'POST',
      ),
      page.getByTestId('book-add-button').click(),
      expect(page).toHaveURL(/\/books$/),
    ])
    expect(titleResponse.status()).toBeLessThan(300)

    // The title landed in the library, monitored: the API confirms it under
    // the work's author.
    const token = await apiToken(request)
    const authors = (await (
      await request.get(`/api/v1/books/authors?q=${encodeURIComponent(PROBE_AUTHOR)}`, {
        headers: { Authorization: `Bearer ${token}` },
      })
    ).json()) as { items: { ID: number; Name: string }[] }
    const author = authors.items.find((a) => a.Name === PROBE_AUTHOR)
    expect(author, `${PROBE_AUTHOR} should be in the library`).toBeDefined()
    const titles = (await (
      await request.get(`/api/v1/books/authors/${author!.ID}/titles`, {
        headers: { Authorization: `Bearer ${token}` },
      })
    ).json()) as { Name: string; Monitored: boolean }[]
    const added = titles.find((t) => t.Name === PROBE_BOOK)
    expect(added, `${PROBE_BOOK} should be under ${PROBE_AUTHOR}`).toBeDefined()
    expect(added!.Monitored).toBe(true)
    return
  }

  // Re-run branch: the work is already in the library from a previous run,
  // so its hit is a jump link to the author's detail — where the title row
  // proves it is still there. (If the lookup returns no book hit at all, the
  // environment simply doesn't surface the probe work — skip.)
  const bookLinks = page.locator('a[data-testid^="book-result-book-"]')
  if (!(await bookLinks.count())) {
    test.skip(true, `no book hit for ${PROBE_BOOK} in this environment`)
    return
  }
  await bookLinks.first().click()
  await expect(page).toHaveURL(/\/books\/\d+$/)
  await expect(page.getByTestId('author-detail')).toBeVisible()
  await expect(page.getByTestId('author-detail').getByText(PROBE_BOOK)).toBeVisible()
})

test('a work-key id lookup opens the modal with provider-fed editions or the manual fallback (MT2)', async ({
  page,
}) => {
  await page.goto('/books/add')
  await signIn(page)
  await expect(page).toHaveURL(/\/books\/add$/)

  // A prefixed work key is an id-lookup query: a single kind=book hit. When
  // Open Library is unreachable in this environment the lookup fails (502)
  // and the test skips, like the search-based tests above.
  await page.getByTestId('book-search-input').fill(`olid:${PROBE_WORK_KEY}`)
  await expect(
    page
      .getByTestId('book-results')
      .or(page.getByTestId('book-search-error'))
      .or(page.getByTestId('book-search-unavailable')),
  ).toBeVisible({ timeout: 30_000 })
  if (!(await page.getByTestId('book-results').count())) {
    test.skip(true, 'Open Library id-lookup unavailable in this environment')
    return
  }

  // The id hit renders as an ordinary selectable work row.
  const row = page.locator('div[data-testid^="book-result-book-"]')
  if (!(await row.count())) {
    test.skip(true, `no book hit for ${PROBE_WORK_KEY} in this environment`)
    return
  }
  await row.first().click()
  await expect(page.getByTestId('add-book-modal')).toBeVisible()
  await expect(page.getByTestId('add-book-editions-section')).toBeVisible()

  // MT2: the keyed work offers the fetched editions, or — when OL has no
  // editions for it (the plan's coverage-gap risk) — the manual fallback
  // with its short note. Either way the add flow never dead-ends.
  await expect(
    page.getByTestId('add-book-editions').or(page.getByTestId('add-book-edition-manual')),
  ).toBeVisible({ timeout: 30_000 })

  if (await page.getByTestId('add-book-editions').count()) {
    // Provider-fed editions: every row carries a monitored toggle, checked
    // by default (the disabled unmonitorable ones are excluded).
    const toggles = page.locator('input[data-testid^="add-book-edition-mon-"]')
    expect(await toggles.count()).toBeGreaterThan(0)
    for (let i = 0; i < (await toggles.count()); i++) {
      const toggle = toggles.nth(i)
      if (await toggle.isDisabled()) continue
      expect(await toggle.isChecked()).toBe(true)
    }
  } else {
    await expect(page.getByTestId('add-book-editions-note')).toContainText('unavailable')
  }
})
