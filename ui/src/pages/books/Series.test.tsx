import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { client } from '../../api/client'
import type { Book, BookSeries, BookSeriesBook } from '../../api/types'
import { BookSeriesDetailPage, SeriesIndexPage } from './Series'

// Page tests for the Readarr B4 Series surface: the index (series cards with
// the member rollup + create-series flow) and the detail page (member books
// across authors with quality/missing/monitored + the link-a-book flow).
vi.mock('../../api/client', () => ({
  client: {
    listBookSeries: vi.fn(),
    listSeriesBooks: vi.fn(),
    getBookSeries: vi.fn(),
    addBookSeries: vi.fn(),
    listBooks: vi.fn(),
    linkTitleToSeries: vi.fn(),
  },
}))

const RECENT = new Date(Date.now() - 24 * 60 * 60 * 1000).toISOString()
const SERIES: BookSeries[] = [{ ID: 10, Name: 'Earthsea', AddedAt: RECENT }]
const MEMBERS: BookSeriesBook[] = [
  { ID: 1, AuthorID: 11, Name: 'A', Monitored: true, AddedAt: RECENT, AuthorName: 'Auth One', HasFile: true, Format: 'EPUB' },
  { ID: 2, AuthorID: 11, Name: 'B', Monitored: true, AddedAt: RECENT, AuthorName: 'Auth One', HasFile: false, Format: '' },
]
const ALL_BOOKS: Book[] = [
  { ID: 1, AuthorID: 11, Name: 'A', Monitored: true, AddedAt: RECENT, AuthorName: 'Auth One', HasFile: true, Format: 'EPUB' },
  { ID: 2, AuthorID: 11, Name: 'B', Monitored: true, AddedAt: RECENT, AuthorName: 'Auth One', HasFile: false, Format: '' },
  { ID: 3, AuthorID: 12, Name: 'C', Monitored: false, AddedAt: RECENT, AuthorName: 'Auth Two', HasFile: true, Format: 'MOBI' },
]

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(client.listBookSeries).mockResolvedValue(SERIES)
  vi.mocked(client.listSeriesBooks).mockResolvedValue(MEMBERS)
  vi.mocked(client.getBookSeries).mockResolvedValue(SERIES[0]!)
  vi.mocked(client.addBookSeries).mockResolvedValue({ id: 11 })
  vi.mocked(client.listBooks).mockResolvedValue(ALL_BOOKS)
  vi.mocked(client.linkTitleToSeries).mockResolvedValue({ title_id: 3, series_id: 10 })
})

afterEach(() => {
  vi.clearAllMocks()
})

// SeriesIndexPage renders <Link> elements, so it needs a Router context.
function renderIndex() {
  return render(
    <MemoryRouter initialEntries={['/books/series']}>
      <SeriesIndexPage />
    </MemoryRouter>,
  )
}

describe('SeriesIndexPage (Readarr B4)', () => {
  it('lists every series with its member rollup and quality', async () => {
    renderIndex()
    await screen.findByTestId('book-series-grid')
    expect(screen.getByTestId('book-series-title')).toHaveTextContent('Series')
    expect(screen.getByTestId('book-series-card-10')).toHaveTextContent('Earthsea')
    expect(screen.getByTestId('book-series-card-10')).toHaveTextContent('2 members · 1 missing · 2 monitored')
    expect(screen.getByTestId('book-series-card-10')).toHaveTextContent('Quality: EPUB')
    // The card links through to the detail page.
    expect(screen.getByTestId('book-series-link-10')).toHaveAttribute('href', '/books/series/10')
  })

  it('shows the empty state when there are no series', async () => {
    vi.mocked(client.listBookSeries).mockResolvedValue([])
    renderIndex()
    expect(await screen.findByTestId('book-series-empty')).toHaveTextContent('No series')
    expect(screen.queryByTestId('book-series-grid')).not.toBeInTheDocument()
  })

  it('surfaces an error state when the fetch fails', async () => {
    vi.mocked(client.listBookSeries).mockRejectedValue(new Error('boom'))
    renderIndex()
    expect(await screen.findByTestId('book-series-error')).toHaveTextContent('Series unavailable')
  })

  it('creates a series via the modal and reloads', async () => {
    const user = userEvent.setup()
    renderIndex()
    await screen.findByTestId('book-series-grid')
    await user.click(screen.getByTestId('book-series-new'))
    await screen.findByTestId('book-series-modal')
    await user.type(screen.getByTestId('book-series-name-input'), 'New Cycle')
    await user.click(screen.getByTestId('book-series-create'))
    await waitFor(() => expect(client.addBookSeries).toHaveBeenCalledWith({ name: 'New Cycle' }))
    // The modal closes after a successful create.
    await waitFor(() => expect(screen.queryByTestId('book-series-modal')).not.toBeInTheDocument())
  })
})

describe('BookSeriesDetailPage (Readarr B4)', () => {
  function renderDetail() {
    return render(
      <MemoryRouter initialEntries={['/books/series/10']}>
        <Routes>
          <Route path="/books/series/:id" element={<BookSeriesDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )
  }

  it('lists member books across authors with quality/missing/monitored', async () => {
    renderDetail()
    await screen.findByTestId('book-series-members')
    // The header rollup.
    expect(screen.getByTestId('book-series-detail-rollup')).toHaveTextContent('2 members · 1 missing · 2 monitored · Quality: EPUB')
    // Member A: has a file, EPUB, monitored.
    expect(screen.getByTestId('book-series-member-1')).toHaveTextContent('Auth One')
    expect(screen.getByTestId('book-series-member-quality-1')).toHaveTextContent('EPUB')
    expect(screen.getByTestId('book-series-member-status-1')).toHaveTextContent('Has file')
    expect(screen.getByTestId('book-series-member-monitored-1')).toHaveTextContent('Monitored')
    // Member B: missing.
    expect(screen.getByTestId('book-series-member-status-2')).toHaveTextContent('Missing')
    // The member title links to the author's detail page (where the book lives).
    expect(screen.getByTestId('book-series-member-link-1')).toHaveAttribute('href', '/books/11')
  })

  it('links a library book into the series', async () => {
    renderDetail()
    await screen.findByTestId('book-series-link-card')
    // Pick book C (id 3, not yet a member) and link it.
    fireEvent.change(screen.getByTestId('book-series-link-select'), { target: { value: '3' } })
    await screen.getByTestId('book-series-link-btn').click()
    await waitFor(() => expect(client.linkTitleToSeries).toHaveBeenCalledWith(3, 10))
    expect(await screen.findByTestId('book-series-link-status')).toHaveTextContent('Linked "C" to Earthsea.')
  })

  it('shows the empty state when the series has no members', async () => {
    vi.mocked(client.listSeriesBooks).mockResolvedValue([])
    renderDetail()
    expect(await screen.findByTestId('book-series-detail-empty')).toHaveTextContent('No member books')
  })

  it('surfaces an error state when the fetch fails', async () => {
    vi.mocked(client.getBookSeries).mockRejectedValue(new Error('boom'))
    renderDetail()
    expect(await screen.findByTestId('book-series-detail-error')).toHaveTextContent('Series unavailable')
  })
})
