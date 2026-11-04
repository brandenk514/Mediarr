import { render, screen, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { client } from '../../api/client'
import type { Book, PageAuthor } from '../../api/types'
import { BooksPage } from './Books'
import { AuthorDetailPage } from '../books/AuthorDetail'

// Readarr B1 books library: a dual-view page. The Authors tab (home) is the
// server-paginated author grid/table; the Books tab is the flat index of
// every book across all authors. A–Z jump, the grid/list toggle, and the
// filter menu come from the shared P1 shell (same contracts as the TV page:
// `{kind}-card-{id}` grid cells, `{menu}-btn`/`{menu}-opt-{value}` filter
// options, `aria-label`-only A–Z letter buttons). All data is faked.
vi.mock('../../api/client', () => ({
  client: {
    listAuthors: vi.fn(),
    listBooks: vi.fn(),
    setAuthorMonitored: vi.fn(),
    setTitleMonitored: vi.fn(),
    runBooksPipeline: vi.fn(),
    getAuthor: vi.fn(),
    listTitles: vi.fn(),
    listAuthorSeries: vi.fn(),
    listTitleSeries: vi.fn(),
    listEditions: vi.fn(),
    listBookFiles: vi.fn(),
    listAuthorHistory: vi.fn(),
    setEditionMonitored: vi.fn(),
  },
}))

const AUTHORS = [
  { ID: 1, Name: 'Ursula K. Le Guin', Monitored: true, AddedAt: '2026-11-01T00:00:00Z' },
  { ID: 2, Name: 'K. William Wilson', Monitored: false, AddedAt: '2026-11-01T00:00:00Z' },
]

const BOOKS: Book[] = [
  {
    ID: 1,
    AuthorID: 1,
    Name: 'The Dispossessed',
    Monitored: true,
    AddedAt: '2026-11-01T00:00:00Z',
    AuthorName: 'Ursula K. Le Guin',
  },
  {
    ID: 2,
    AuthorID: 2,
    Name: 'The Memory Police',
    Monitored: false,
    AddedAt: '2026-11-01T00:00:00Z',
    AuthorName: 'K. William Wilson',
  },
]

function pageOf(items: typeof AUTHORS): PageAuthor {
  return { items, total: items.length, page: 1, page_size: 25 }
}

function renderWithProbe() {
  function Probe() {
    const location = useLocation()
    return <div data-testid="probe-location">{location.pathname}</div>
  }
  return render(
    <MemoryRouter initialEntries={['/books']}>
      <Routes>
        <Route path="/books" element={<BooksPage />} />
        <Route
          path="/books/:id"
          element={
            <>
              <AuthorDetailPage />
              <Probe />
            </>
          }
        />
        <Route
          path="*"
          element={
            <Probe />
          }
        />
      </Routes>
    </MemoryRouter>,
  )
}

describe('BooksPage (Readarr B1 dual view)', () => {
  it('shows the Authors grid as the home view', async () => {
    vi.mocked(client.listAuthors).mockResolvedValue(pageOf(AUTHORS))
    renderWithProbe()

    expect(screen.getByTestId('books-tab-authors')).toHaveAttribute('aria-pressed', 'true')
    const grid = await screen.findByTestId('books-authors-grid')
    // Card names live in the cell title attribute (poster-grid design, as in TV).
    expect(within(grid).getByTestId('author-card-1')).toHaveAttribute('title', 'Ursula K. Le Guin')
    expect(within(grid).getByTestId('author-card-2')).toHaveAttribute('title', 'K. William Wilson')
    // The A–Z quick-jump renders with the authors.
    const az = screen.getByTestId('books-authors-az')
    expect(within(az).getByLabelText('Jump to U')).toBeInTheDocument()
    expect(within(az).getByLabelText('Jump to K')).toBeInTheDocument()
    // The flat index is not loaded on the Authors tab.
    expect(vi.mocked(client.listBooks)).not.toHaveBeenCalled()
  })

  it('switches to the flat Books index and shows every book with its author', async () => {
    vi.mocked(client.listAuthors).mockResolvedValue(pageOf(AUTHORS))
    vi.mocked(client.listBooks).mockResolvedValue(BOOKS)
    const user = userEvent.setup()
    renderWithProbe()

    await user.click(screen.getByTestId('books-tab-books'))

    const grid = await screen.findByTestId('books-grid')
    expect(within(grid).getByTestId('book-card-1')).toBeInTheDocument()
    expect(within(grid).getByTestId('book-card-2')).toBeInTheDocument()
    // Titles live in the card title attribute; each card's footer shows its
    // author name (the flat index is every book of every author).
    expect(within(grid).getByTestId('book-card-1')).toHaveAttribute('title', 'The Dispossessed')
    expect(within(grid).getByTestId('book-card-2')).toHaveAttribute('title', 'The Memory Police')
    expect(screen.getAllByText('Ursula K. Le Guin').length).toBeGreaterThan(0)
    expect(screen.getAllByText('K. William Wilson').length).toBeGreaterThan(0)
    // "The Dispossessed" jumps under D (leading article skipped, Sonarr-style).
    const az = screen.getByTestId('books-az')
    expect(within(az).getByLabelText('Jump to D')).toBeInTheDocument()
  })

  it('filters the flat Books index to monitored books', async () => {
    vi.mocked(client.listAuthors).mockResolvedValue(pageOf(AUTHORS))
    vi.mocked(client.listBooks).mockResolvedValue(BOOKS)
    const user = userEvent.setup()
    renderWithProbe()

    await user.click(screen.getByTestId('books-tab-books'))
    await user.click(screen.getByTestId('books-index-filter-btn'))
    await user.click(screen.getByTestId('books-index-filter-opt-monitored'))

    const grid = await screen.findByTestId('books-grid')
    expect(within(grid).getByTestId('book-card-1')).toBeInTheDocument()
    expect(within(grid).queryByTestId('book-card-2')).not.toBeInTheDocument()
  })

  it('filters the flat Books index to unmonitored books', async () => {
    vi.mocked(client.listAuthors).mockResolvedValue(pageOf(AUTHORS))
    vi.mocked(client.listBooks).mockResolvedValue(BOOKS)
    const user = userEvent.setup()
    renderWithProbe()

    await user.click(screen.getByTestId('books-tab-books'))
    await user.click(screen.getByTestId('books-index-filter-btn'))
    await user.click(screen.getByTestId('books-index-filter-opt-unmonitored'))
    // K. William Wilson's book is the only unmonitored one.
    const grid = await screen.findByTestId('books-grid')
    expect(within(grid).getByTestId('book-card-2')).toBeInTheDocument()
    expect(within(grid).queryByTestId('book-card-1')).not.toBeInTheDocument()
  })

  it('shows the flat index empty state when there are no books', async () => {
    vi.mocked(client.listAuthors).mockResolvedValue(pageOf(AUTHORS))
    vi.mocked(client.listBooks).mockResolvedValue([])
    const user = userEvent.setup()
    renderWithProbe()

    await user.click(screen.getByTestId('books-tab-books'))

    expect(await screen.findByText('No books')).toBeInTheDocument()
    expect(screen.getByTestId('books-index-empty-add')).toBeInTheDocument()
  })

  it('toggles the Authors table view', async () => {
    vi.mocked(client.listAuthors).mockResolvedValue(pageOf(AUTHORS))
    const user = userEvent.setup()
    renderWithProbe()

    await user.click(screen.getByTestId('books-view-list'))

    const row = await screen.findByTestId('author-1')
    expect(row).toHaveTextContent('Ursula K. Le Guin')
    expect(screen.getByTestId('author-mon-1')).toBeInTheDocument()
    // The grid is gone in table view.
    expect(screen.queryByTestId('books-authors-grid')).not.toBeInTheDocument()
  })

  it('navigates to the author detail from an author card', async () => {
    vi.mocked(client.listAuthors).mockResolvedValue(pageOf(AUTHORS))
    const user = userEvent.setup()
    renderWithProbe()

    const grid = await screen.findByTestId('books-authors-grid')
    const card = within(grid).getByTestId('author-card-1')
    await user.click(within(card).getByRole('button'))

    expect(screen.getByTestId('probe-location')).toHaveTextContent('/books/1')
  })
})
