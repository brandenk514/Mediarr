import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { client } from '../../api/client'
import type { Book, BookSeries, BookSeriesBook } from '../../api/types'
import { BookshelvesPage, isRecentlyAdded, qualityOf } from './Bookshelves'

// Page + helper tests for the Readarr B4 Bookshelves page: the pure rollup
// helpers (qualityOf / isRecentlyAdded) and the page's shelf aggregation
// (standard shelves + per-series shelves, each with its books/missing/
// monitored/quality rollup), covering the populated / empty / error /
// loading states.
vi.mock('../../api/client', () => ({
  client: {
    listBooks: vi.fn(),
    listBookSeries: vi.fn(),
    listSeriesBooks: vi.fn(),
  },
}))

const DAY = 24 * 60 * 60 * 1000
const RECENT = new Date(Date.now() - DAY).toISOString() // 1 day ago → in window
const OLD = new Date(Date.now() - 90 * DAY).toISOString() // 90 days ago → out

const BOOKS: Book[] = [
  { ID: 1, AuthorID: 11, Name: 'A', Monitored: true, AddedAt: RECENT, AuthorName: 'Auth One', HasFile: true, Format: 'EPUB' },
  { ID: 2, AuthorID: 11, Name: 'B', Monitored: true, AddedAt: RECENT, AuthorName: 'Auth One', HasFile: false, Format: '' },
  { ID: 3, AuthorID: 12, Name: 'C', Monitored: false, AddedAt: OLD, AuthorName: 'Auth Two', HasFile: true, Format: 'MOBI' },
]
const SERIES: BookSeries[] = [{ ID: 10, Name: 'Earthsea', AddedAt: RECENT }]
const MEMBERS: BookSeriesBook[] = [
  { ID: 1, AuthorID: 11, Name: 'A', Monitored: true, AddedAt: RECENT, AuthorName: 'Auth One', HasFile: true, Format: 'EPUB' },
  { ID: 2, AuthorID: 11, Name: 'B', Monitored: true, AddedAt: RECENT, AuthorName: 'Auth One', HasFile: false, Format: '' },
]

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(client.listBooks).mockResolvedValue(BOOKS)
  vi.mocked(client.listBookSeries).mockResolvedValue(SERIES)
  vi.mocked(client.listSeriesBooks).mockResolvedValue(MEMBERS)
})

afterEach(() => {
  vi.clearAllMocks()
})

// BookshelvesPage renders <Link> elements, so it needs a Router context.
function renderPage() {
  return render(
    <MemoryRouter initialEntries={['/books/bookshelves']}>
      <BookshelvesPage />
    </MemoryRouter>,
  )
}

describe('qualityOf (Readarr B4 rollup)', () => {
  it('returns the single format when all resolvable members agree', () => {
    expect(qualityOf([{ Format: 'EPUB' }, { Format: 'EPUB' }])).toBe('EPUB')
    // A resolvable format alongside an unknown ('') still resolves to the one.
    expect(qualityOf([{ Format: 'EPUB' }, { Format: '' }])).toBe('EPUB')
  })
  it("returns 'Mixed' when members disagree", () => {
    expect(qualityOf([{ Format: 'EPUB' }, { Format: 'MOBI' }])).toBe('Mixed')
  })
  it("returns '—' when nothing is resolvable", () => {
    expect(qualityOf([{ Format: '' }, { Format: '' }])).toBe('\u2014')
    expect(qualityOf([])).toBe('\u2014')
  })
})

describe('isRecentlyAdded (Readarr B4 rollup)', () => {
  it('is true inside the 30-day window', () => {
    expect(isRecentlyAdded(RECENT)).toBe(true)
    expect(isRecentlyAdded(new Date(Date.now() - 29 * DAY).toISOString())).toBe(true)
  })
  it('is false outside the 30-day window', () => {
    expect(isRecentlyAdded(OLD)).toBe(false)
    expect(isRecentlyAdded(new Date(Date.now() - 31 * DAY).toISOString())).toBe(false)
  })
  it('is false for an unparseable date', () => {
    expect(isRecentlyAdded('not-a-date')).toBe(false)
  })
})

describe('BookshelvesPage (Readarr B4)', () => {
  it('renders the standard + per-series shelves with their rollups', async () => {
    renderPage()
    await screen.findByTestId('bookshelves-grid')
    expect(screen.getByTestId('bookshelves-title')).toHaveTextContent('Bookshelves')
    // All Books: 3 books, 1 missing (B), 2 monitored (A,C), mixed quality.
    expect(screen.getByTestId('bookshelf-all')).toHaveTextContent('3 books · 1 missing · 2 monitored')
    expect(screen.getByTestId('bookshelf-all')).toHaveTextContent('Quality: Mixed')
    // Missing shelf: only B, which is monitored.
    expect(screen.getByTestId('bookshelf-missing')).toHaveTextContent('1 book · 1 missing · 1 monitored')
    // Unmonitored shelf: only C (MOBI).
    expect(screen.getByTestId('bookshelf-unmonitored')).toHaveTextContent('1 book · 0 missing · 0 monitored')
    // Per-series shelf (Earthsea): 2 members, 1 missing, 2 monitored, EPUB.
    expect(screen.getByTestId('bookshelf-series-10')).toHaveTextContent('2 books · 1 missing · 2 monitored')
    expect(screen.getByTestId('bookshelf-series-10')).toHaveTextContent('Quality: EPUB')
    // The series shelf links through to the series detail page.
    expect(screen.getByTestId('bookshelf-link-series-10')).toHaveAttribute('href', '/books/series/10')
  })

  it('shows the empty state when there are no books and no series', async () => {
    vi.mocked(client.listBooks).mockResolvedValue([])
    vi.mocked(client.listBookSeries).mockResolvedValue([])
    renderPage()
    expect(await screen.findByTestId('bookshelves-empty')).toHaveTextContent('No bookshelves')
    expect(screen.queryByTestId('bookshelves-grid')).not.toBeInTheDocument()
  })

  it('renders the loading state before the fetch settles', async () => {
    vi.mocked(client.listBooks).mockReturnValue(new Promise<Book[]>(() => {}) as Promise<Book[]>)
    renderPage()
    expect(screen.getByTestId('bookshelves-loading')).toHaveTextContent('Loading bookshelves…')
  })

  it('surfaces an error state when the fetch fails', async () => {
    vi.mocked(client.listBooks).mockRejectedValue(new Error('boom'))
    renderPage()
    expect(await screen.findByTestId('bookshelves-error')).toHaveTextContent('Bookshelves unavailable')
    expect(screen.getByTestId('bookshelves-error-message')).toHaveTextContent('boom')
  })
})
