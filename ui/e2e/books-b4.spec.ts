import { expect, test, type APIRequestContext } from '@playwright/test'
import { E2E_PASSWORD, E2E_USER, signIn } from './login'

// Readarr B4: the Bookshelves + Series e2e — the two new books rail entries
// (Bookshelves, Series) and their pages.
//
// DB-state independent on the long-lived dev DB (the books-b2/b3 "drive from
// the API" convention): each page's state is whatever the seeded library
// produces, so the spec probes the same endpoints the page calls with a
// separately logged-in request context and asserts the page settles into the
// matching state — never assuming a fixture layout. The Bookshelves grid
// renders 5 standard shelves + one per series; the Series grid renders one
// card per series.

async function apiToken(request: APIRequestContext): Promise<string> {
  const login = await request.post('/api/v1/auth/login', {
    data: { username: E2E_USER, password: E2E_PASSWORD },
  })
  return ((await login.json()) as { token: string }).token
}

test('the books rail exposes Bookshelves and Series; both pages render their state', async ({
  page,
  request,
}) => {
  // Hit the protected route FIRST: AuthGate bounces to /login carrying
  // state.from, and Login client-side-navigates back after sign-in.
  await page.goto('/books')
  await signIn(page)
  await expect(page).toHaveURL(/\/books$/)

  // The B4 rail sub-links (derived from the path: /books/bookshelves,
  // /books/series).
  await expect(page.getByTestId('rail-sub-booksbookshelves')).toBeVisible()
  await expect(page.getByTestId('rail-sub-booksbookshelves')).toContainText('Bookshelves')
  await expect(page.getByTestId('rail-sub-booksseries')).toBeVisible()
  await expect(page.getByTestId('rail-sub-booksseries')).toContainText('Series')

  // Probe the endpoints the pages call.
  const token = await apiToken(request)
  const auth = { headers: { Authorization: `Bearer ${token}` } }
  const booksResp = await request.get('/api/v1/books', auth)
  const seriesResp = await request.get('/api/v1/books/series', auth)
  expect(booksResp.status()).toBe(200)
  expect(seriesResp.status()).toBe(200)
  const books = (await booksResp.json()) as unknown[]
  const series = (await seriesResp.json()) as { id: number }[]

  // Bookshelves: 5 standard shelves + one per series, or the empty state
  // when the library has neither books nor series.
  await page.getByTestId('rail-sub-booksbookshelves').click()
  await expect(page).toHaveURL(/\/books\/bookshelves$/)
  await expect(page.getByTestId('bookshelves-title')).toBeVisible()
  if (books.length === 0 && series.length === 0) {
    await expect(page.getByTestId('bookshelves-empty')).toBeVisible()
  } else {
    await expect(page.getByTestId('bookshelves-grid')).toBeVisible()
    await expect(page.locator('section[data-testid^="bookshelf-"]')).toHaveCount(5 + series.length)
  }

  // Series: one card per series, or the empty state.
  await page.getByTestId('rail-sub-booksseries').click()
  await expect(page).toHaveURL(/\/books\/series$/)
  await expect(page.getByTestId('book-series-title')).toBeVisible()
  if (series.length === 0) {
    await expect(page.getByTestId('book-series-empty')).toBeVisible()
  } else {
    await expect(page.getByTestId('book-series-grid')).toBeVisible()
    await expect(page.locator('section[data-testid^="book-series-card-"]')).toHaveCount(series.length)
  }
})

test('the series detail page lists the series members across authors', async ({
  page,
  request,
}) => {
  const token = await apiToken(request)
  const series = (await (
    await request.get('/api/v1/books/series', { headers: { Authorization: `Bearer ${token}` } })
  ).json()) as { id: number }[]
  if (series.length === 0) {
    test.skip(true, 'no series in the dev library to open a detail page for')
    return
  }
  const first = series[0]!

  // Navigate CLIENT-SIDE to the detail page (a goto would drop the in-memory
  // auth token and bounce to /login): sign in, rail to the series index, then
  // click the first series card through.
  await page.goto('/books')
  await signIn(page)
  await expect(page).toHaveURL(/\/books$/)
  await page.getByTestId('rail-sub-booksseries').click()
  await expect(page).toHaveURL(/\/books\/series$/)
  await page.getByTestId(`book-series-link-${first.id}`).click()
  await expect(page).toHaveURL(new RegExp(`/books/series/${first.id}$`))
  await expect(page.getByTestId('book-series-detail-title')).toBeVisible()

  // Probe the members the page lists and assert the same count.
  const members = (await (
    await request.get(`/api/v1/books/series/${first.id}/books`, {
      headers: { Authorization: `Bearer ${token}` },
    })
  ).json()) as unknown[]
  if (members.length === 0) {
    await expect(page.getByTestId('book-series-detail-empty')).toBeVisible()
  } else {
    const table = page.getByTestId('book-series-members')
    await expect(table).toBeVisible()
    await expect(table.locator('.lt-row')).toHaveCount(members.length)
  }
})
