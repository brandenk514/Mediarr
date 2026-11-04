import { render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { client } from '../../api/client'
import type { Author, Book, PageAuthor } from '../../api/types'
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
    // Readarr B2 Edit Author modal pickers + full author update.
    updateAuthor: vi.fn(),
    listRootFolders: vi.fn(),
    getProfiles: vi.fn(),
  },
}))

const AUTHORS: Author[] = [
  {
    ID: 1,
    Name: 'Ursula K. Le Guin',
    Monitored: true,
    AddedAt: '2026-11-01T00:00:00Z',
    // Readarr B2 fields, exercised by the Edit Author modal tests.
    MonitorPolicy: 'existing',
    RootFolder: '/media/books',
    QualityProfile: 'EPUB',
    Tags: ['sci-fi'],
  },
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

// Readarr B2: the Edit Author modal on the author detail — pre-filled from
// the loaded author and persisted through the full author-update PUT.
describe('AuthorDetailPage (Readarr B2 Edit Author modal)', () => {
  function mockAuthorDetail() {
    vi.mocked(client.getAuthor).mockResolvedValue(AUTHORS[0]! as never)
    vi.mocked(client.listTitles).mockResolvedValue([])
    vi.mocked(client.listAuthorSeries).mockResolvedValue([])
    vi.mocked(client.listRootFolders).mockResolvedValue([
      { ID: 1, Path: '/media/books', Name: 'Books', CreatedAt: '', FreeSpace: 100 * 1024 ** 3 },
    ] as never)
    vi.mocked(client.getProfiles).mockResolvedValue({
      profiles: ['EPUB', 'MOBI'],
      default: 'EPUB',
    } as never)
  }

  async function openEditModal(user: ReturnType<typeof userEvent.setup>) {
    renderWithProbe()
    const grid = await screen.findByTestId('books-authors-grid')
    const card = within(grid).getByTestId('author-card-1')
    await user.click(within(card).getByRole('button'))
    await user.click(await screen.findByTestId('author-detail-edit'))
    await screen.findByTestId('edit-modal')
  }

  it('opens the modal pre-filled from the loaded author (policy, root folder, profile, tags, monitored)', async () => {
    vi.mocked(client.listAuthors).mockResolvedValue(pageOf(AUTHORS))
    mockAuthorDetail()
    const user = userEvent.setup()
    await openEditModal(user)

    expect(screen.getByTestId('edit-monitored')).toBeChecked()
    expect(screen.getByTestId('edit-monitor-policy')).toHaveValue('existing')
    expect(screen.getByTestId('edit-root-folder')).toHaveValue('/media/books')
    expect(screen.getByTestId('edit-profile')).toHaveValue('EPUB')
    expect(screen.getByTestId('edit-tags-chips')).toHaveTextContent('sci-fi')
    // The metadata profile is a B3 stub: present but disabled.
    expect(screen.getByTestId('edit-metadata-profile')).toBeDisabled()
    expect(
      screen.getByText('Metadata profiles land in a later phase (B3).'),
    ).toBeInTheDocument()
    // The 7 monitor states render in the policy select.
    const policy = screen.getByTestId('edit-monitor-policy') as HTMLSelectElement
    expect(Array.from(policy.options).map((o) => o.label)).toEqual([
      'All Albums',
      'Future Albums',
      'Missing Albums',
      'Existing Albums',
      'First Album',
      'Lastest Album',
      'None',
    ])
  })

  it('saves a full author update with the changed fields', async () => {
    vi.mocked(client.listAuthors).mockResolvedValue(pageOf(AUTHORS))
    vi.mocked(client.updateAuthor).mockResolvedValue({ id: 1, monitored: false } as never)
    mockAuthorDetail()
    const user = userEvent.setup()
    await openEditModal(user)

    // Change the policy, un-monitor, and append a tag.
    await user.selectOptions(screen.getByTestId('edit-monitor-policy'), 'missing')
    await user.click(screen.getByTestId('edit-monitored'))
    await user.type(screen.getByTestId('edit-tags-input'), 'e2e{Enter}')

    await user.click(screen.getByTestId('edit-save'))
    await waitFor(() =>
      expect(client.updateAuthor).toHaveBeenCalledWith(1, {
        monitored: false,
        monitor_policy: 'missing',
        root_folder: '/media/books',
        quality_profile: 'EPUB',
        tags: ['sci-fi', 'e2e'],
      }),
    )
    // The modal closes and the page reports the save (and reloads).
    expect(screen.queryByTestId('edit-modal')).not.toBeInTheDocument()
    expect(await screen.findByTestId('author-detail-status')).toHaveTextContent(
      'Author updated.',
    )
  })
})
