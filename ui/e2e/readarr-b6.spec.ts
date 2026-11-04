import { execSync } from 'node:child_process'
import { expect, test } from '@playwright/test'
import { signIn } from './login'

// B6 Readarr drop-in e2e, run against the dev stack (Vite :5173 → backend
// :8081 → Postgres). The fake Readarr v0.4 server runs as the `fake-readarr`
// container on the dev docker network — the migration dry-run fetch happens
// INSIDE the backend container, so the fake must be reachable there by its
// container name (not from the browser). The fake serves authors + books +
// editions + a Calibre root folder with the v0.4 shapes (X-Api-Key auth).
//
// This spec drives a DRY RUN through the real UI and asserts the mapping
// report (counts + item rows) plus the status transition to completed — and
// that the dry run wrote nothing to Postgres (read-only contract).
const FAKE_READARR_URL = 'http://fake-readarr:8888'

// pgCounts snapshots the row counts of every table a readarr migration
// could touch. The postgres password is derived from the container's own
// environment at runtime (never inlined).
function pgCounts(): Record<string, number> {
  const pw = execSync(`docker exec mediarr-dev-postgres-1 sh -c 'echo $POSTGRES_PASSWORD'`, {
    encoding: 'utf8',
  }).trim()
  const tables = [
    'book_authors',
    'book_titles',
    'book_editions',
    'book_files',
    'book_series',
    'book_series_members',
    'readarr_migration_state',
  ]
  const out: Record<string, number> = {}
  for (const t of tables) {
    out[t] = Number(
      execSync(
        `docker exec -e PGPASSWORD=${pw} mediarr-dev-postgres-1 psql -U mediarr -d mediarr -tAc "SELECT count(*) FROM ${t}"`,
        { encoding: 'utf8' },
      ).trim(),
    )
  }
  return out
}

test('readarr drop-in: dry run renders the mapping report and writes nothing (B6)', async ({
  page,
}) => {
  // Protected route first, then the form-only sign-in (the token only lives
  // in page memory — never goto again once authenticated).
  await page.goto('/system/import')
  await signIn(page)
  await expect(page).toHaveURL(/\/system\/import$/)

  // The migration section renders (the P6 Import panel).
  await expect(page.getByTestId('migration-source')).toBeVisible()
  await expect(page.getByTestId('migration-status')).toBeVisible()

  // Snapshot before the run (zero-writes assertion).
  const before = pgCounts()

  // Select Readarr and fill the form.
  await page.getByTestId('migration-source').selectOption('readarr')
  await expect(page.getByTestId('migration-source-help')).toContainText(
    'Base URL of the Readarr instance.',
  )
  await expect(
    page.getByTestId('migration-source-url'),
  ).toHaveAttribute('placeholder', 'http://readarr.local:8888')
  await page.getByTestId('migration-source-url').fill(FAKE_READARR_URL)
  await page.getByTestId('migration-api-key').fill('e2e-readarr-key')
  await expect(page.getByTestId('migration-run-btn')).toBeEnabled()
  await page.getByTestId('migration-run-btn').click()

  // The mapping report renders: counts + at least one item row.
  const result = page.getByTestId('migration-result')
  await expect(result).toBeVisible()
  await expect(result).toContainText('dry run')
  const countsText = (await page.getByTestId('migration-counts').textContent()) ?? ''
  const total = Number(/total\s*(\d+)/.exec(countsText)?.[1] ?? '0')
  expect(total, `mapping report total (got counts "${countsText}")`).toBeGreaterThan(0)
  await expect(page.getByTestId('migration-item-0')).toBeVisible()
  await expect(result).toContainText('Ursula K. Le Guin')
  await expect(result).toContainText('The Dispossessed')

  // The readarr-scoped status transitions to completed (books).
  await expect(page.getByTestId('migration-status')).toContainText('completed')
  await expect(page.getByTestId('migration-status')).toContainText('3/3 books')

  // Zero writes: the dry run is read-only.
  const after = pgCounts()
  expect(
    after,
    `dry run must not write; before=${JSON.stringify(before)} after=${JSON.stringify(after)}`,
  ).toEqual(before)
})
