import { expect, test, type APIRequestContext } from '@playwright/test'
import { E2E_PASSWORD, E2E_USER, signIn } from './login'

// Readarr B5: the books SETTINGS surface e2e — the new Books Import Lists
// section (nav + card), the Media Management books blocks (book naming,
// book file management, the global Calibre flag, and the per-root Calibre
// column in the root-folder table), and the Metadata "Book tag writing"
// block.
//
// DB-state independent (the books-b2/b3 "drive from the API" convention):
// the save round-trip test probes the live book-import-lists document with a
// separately logged-in request context, adds a uniquely-named list through
// the UI, asserts the backend now carries it, then deletes it and asserts
// the document is back to its original shape — so it works over whatever the
// long-lived dev DB already holds and leaves it clean.

async function apiToken(request: APIRequestContext): Promise<string> {
  const login = await request.post('/api/v1/auth/login', {
    data: { username: E2E_USER, password: E2E_PASSWORD },
  })
  return ((await login.json()) as { token: string }).token
}

type ImportListDoc = {
  lists: { id: number; name: string }[]
  exclusions: unknown[]
}

async function getImportListDoc(
  request: APIRequestContext,
  token: string,
): Promise<ImportListDoc> {
  const res = await request.get('/api/v1/settings/book-import-lists', {
    headers: { Authorization: `Bearer ${token}` },
  })
  expect(res.status()).toBe(200)
  return (await res.json()) as ImportListDoc
}

test('the settings shell exposes Books Import Lists and the books blocks render (B5)', async ({
  page,
  request,
}) => {
  // Hit the protected route first: AuthGate bounces to /login carrying
  // state.from, and Login client-side-navigates back after sign-in.
  await page.goto('/settings/media-management')
  await signIn(page)
  await expect(page).toHaveURL(/\/settings\/media-management$/)

  // The B5 nav item is present (16-section IA).
  await expect(page.getByTestId('settings-nav-book-import-lists')).toBeVisible()
  await expect(page.getByTestId('settings-nav-book-import-lists')).toContainText(
    'Books Import Lists',
  )

  // Media Management: the Readarr books blocks (naming + file management) and
  // the global Calibre feature flag.
  await expect(page.getByRole('heading', { name: 'Book naming' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Book file management' })).toBeVisible()
  await expect(page.getByTestId('mm-book-format')).toBeVisible()
  await expect(page.getByTestId('mm-book-naming')).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Calibre' })).toBeVisible()
  await expect(page.getByTestId('mm-calibre-enabled')).toBeVisible()

  // The root-folder table gains the per-root Calibre column. Probe the
  // registry and assert the column header is present and (when roots exist)
  // one Calibre cell per root.
  const token = await apiToken(request)
  const roots = (await (
    await request.get('/api/v1/root-folders', {
      headers: { Authorization: `Bearer ${token}` },
    })
  ).json()) as unknown[]
  await expect(page.getByTestId('mm-roots')).toBeVisible()
  await expect(
    page.getByTestId('mm-roots').getByRole('columnheader', { name: 'Calibre' }),
  ).toBeVisible()
  if (roots.length > 0) {
    await expect(page.locator('[data-testid^="rfc-cell-"]')).toHaveCount(roots.length)
  }

  // Books Import Lists section: the lists card + exclusions card render.
  await page.getByTestId('settings-nav-book-import-lists').click()
  await expect(page).toHaveURL(/\/settings\/book-import-lists$/)
  await expect(page.getByTestId('blb-card')).toBeVisible()
  await expect(
    page.getByTestId('blb-card').getByRole('heading', { name: 'Books import lists' }),
  ).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Exclusions' })).toBeVisible()
  await expect(page.getByTestId('blb-add-card')).toBeVisible()

  // Metadata section: the book tag-writing block (book + audio tag types).
  await page.getByTestId('settings-nav-metadata').click()
  await expect(page).toHaveURL(/\/settings\/metadata$/)
  await expect(page.getByRole('heading', { name: 'Book tag writing' })).toBeVisible()
  await expect(page.getByTestId('md-book-tag')).toBeVisible()
  await expect(page.getByTestId('md-book-tag-type')).toBeVisible()
  await expect(page.getByTestId('md-audio-tag-type')).toBeVisible()
})

test('the books import-lists save round-trips to the backend (B5)', async ({
  page,
  request,
}) => {
  await page.goto('/settings/book-import-lists')
  await signIn(page)
  await expect(page).toHaveURL(/\/settings\/book-import-lists$/)

  // Baseline document (drive from the API — never assume a fixture layout).
  const token = await apiToken(request)
  const before = await getImportListDoc(request, token)

  // Add a uniquely-named Goodreads list through the UI.
  const uniqueName = `e2e-b5-probe-${Date.now()}`
  await expect(page.getByTestId('blb-card')).toBeVisible()
  await page.getByTestId('blb-new-name').fill(uniqueName)
  await page.getByTestId('blb-new-kind').selectOption('goodreads')
  await page.getByTestId('blb-new-listid').fill('1')
  await page.getByTestId('blb-new-baseurl').fill('https://www.goodreads.com')
  await page.getByTestId('blb-add').click()
  await expect(page.getByTestId('blb-name-1')).toBeVisible() // a row rendered

  // Save, and confirm the backend now carries the new list.
  await page.getByTestId('blb-save').click()
  await expect(page.getByTestId('blb-saved')).toBeVisible()
  const afterSave = await getImportListDoc(request, token)
  expect(afterSave.lists).toHaveLength(before.lists.length + 1)
  expect(afterSave.lists.map((l) => l.name)).toContain(uniqueName)

  // Clean up: delete the added list and save, restoring the original doc.
  const addedRow = page
    .locator('[data-testid^="blb-item-"]')
    .filter({ hasText: uniqueName })
  await addedRow.getByRole('button', { name: 'Delete' }).click()
  await page.getByTestId('blb-save').click()
  await expect(page.getByTestId('blb-saved')).toBeVisible()
  const afterDelete = await getImportListDoc(request, token)
  expect(afterDelete.lists).toHaveLength(before.lists.length)
  expect(afterDelete.lists.map((l) => l.name)).not.toContain(uniqueName)
})
