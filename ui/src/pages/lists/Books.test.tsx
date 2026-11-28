import { render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { client } from '../../api/client'
import type { Author, Book, PageAuthor } from '../../api/types'
import { BooksPage } from './Books'
import { AuthorDetailPage } from '../books/AuthorDetail'

// Readarr B1 books library, re-cast for #150: a dual-view page. The Authors
// tab (home) is the server-paginated author poster grid (real PosterURL art
// with an initials stand-in, kind pill, hover edit) or table, with a heading
// sort cluster (server-side sort) and a slide-over drawer (monitored). The
// Books tab is the flat index of every book across all authors: its shelf
// filters (monitored / missing / recent) are client-side and live in the
// same drawer; the grid renders the whole filtered index (no pagination
// chrome), the table keeps page slicing. All data is faked.
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
    // Readarr B3: the book metadata profile names for the edit modal.
    listBookMetadataProfiles: vi.fn(),
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
    MetadataProfile: 'Default',
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
    // Readarr B4 rollup pair (required on the generated Book type).
    HasFile: true,
    Format: 'EPUB',
  },
  {
    ID: 2,
    AuthorID: 2,
    Name: 'The Memory Police',
    Monitored: false,
    AddedAt: '2026-11-01T00:00:00Z',
    AuthorName: 'K. William Wilson',
    HasFile: false,
    Format: '',
  },
]

function pageOf(items: typeof AUTHORS): PageAuthor {
  return { items, total: items.length, page: 1, page_size: 25 }
}

function renderWithProbe(entry = '/books') {
  function Probe() {
    const location = useLocation()
    return <div data-testid="probe-location">{location.pathname}</div>
  }
  return render(
    <MemoryRouter initialEntries={[entry]}>
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

beforeEach(() => {
  // The per-kind view toggles persist to localStorage; a leak from a
  // list-view test would put later tests in the table view and hide the grid.
  localStorage.clear()
  vi.mocked(client.listAuthors).mockReset()
  vi.mocked(client.listBooks).mockReset()
  vi.mocked(client.setAuthorMonitored).mockReset()
  vi.mocked(client.setTitleMonitored).mockReset()
})

describe('BooksPage (Readarr B1 dual view, #150 grids)', () => {
  it('shows the Authors poster grid as the home view', async () => {
    vi.mocked(client.listAuthors).mockResolvedValue(pageOf(AUTHORS))
    renderWithProbe()

    expect(screen.getByTestId('books-tab-authors')).toHaveAttribute('aria-pressed', 'true')
    const grid = await screen.findByTestId('books-authors-grid')
    // Card names live in the cell title attribute (poster-grid design).
    expect(within(grid).getByTestId('author-card-1')).toHaveAttribute('title', 'Ursula K. Le Guin')
    expect(within(grid).getByTestId('author-card-2')).toHaveAttribute('title', 'K. William Wilson')
    // The kind pill marks the kind.
    expect(within(grid).getByTestId('author-card-1')).toHaveTextContent('Author')
    // The flat index is not loaded on the Authors tab.
    expect(vi.mocked(client.listBooks)).not.toHaveBeenCalled()
  })

  it('sorts the authors server-side (default added-desc) and toggles the direction', async () => {
    vi.mocked(client.listAuthors).mockResolvedValue(pageOf(AUTHORS))
    const user = userEvent.setup()
    renderWithProbe()
    await screen.findByTestId('books-authors-grid')

    // Default sort: added descending (a just-added author leads the grid).
    expect(client.listAuthors).toHaveBeenLastCalledWith(
      expect.objectContaining({ sort: 'added-desc', page: 1 }),
    )

    // Flipping the direction keeps the field (added) and sends added-asc.
    await user.click(screen.getByTestId('books-authors-sort-dir'))
    await vi.waitFor(() =>
      expect(client.listAuthors).toHaveBeenLastCalledWith(
        expect.objectContaining({ sort: 'added-asc', page: 1 }),
      ),
    )

    // Picking a new field keeps the direction (asc) and sends title-asc.
    await user.click(screen.getByTestId('books-authors-sort-btn'))
    await user.click(screen.getByTestId('books-authors-sort-opt-title'))
    await vi.waitFor(() =>
      expect(client.listAuthors).toHaveBeenLastCalledWith(
        expect.objectContaining({ sort: 'title-asc', page: 1 }),
      ),
    )
  })

  it('filters the authors server-side through the drawer monitored facet', async () => {
    vi.mocked(client.listAuthors).mockResolvedValue(pageOf(AUTHORS))
    const user = userEvent.setup()
    renderWithProbe()
    await screen.findByTestId('books-authors-grid')

    await user.click(screen.getByTestId('books-filter-btn'))
    expect(screen.getByTestId('filter-drawer-count')).toHaveTextContent('0 Active Filters')
    await user.selectOptions(screen.getByTestId('books-facet-monitored'), 'unmonitored')

    await vi.waitFor(() => {
      const calls = vi.mocked(client.listAuthors).mock.calls
      const last = calls[calls.length - 1]?.[0]
      expect(last).toMatchObject({ monitored: false, page: 1 })
    })
    expect(screen.getByTestId('books-filter-btn')).toHaveTextContent('1 Active Filter')

    await user.click(screen.getByTestId('filter-drawer-clear'))
    await vi.waitFor(() => {
      const calls = vi.mocked(client.listAuthors).mock.calls
      const last = calls[calls.length - 1]?.[0]
      expect(last).toBeDefined()
      expect(last?.monitored).toBeUndefined()
    })
    expect(screen.getByTestId('books-filter-btn')).toHaveTextContent('0 Active Filters')
  })

  it('switches to the flat Books index and shows every book with its author', async () => {
    vi.mocked(client.listAuthors).mockResolvedValue(pageOf(AUTHORS))
    vi.mocked(client.listBooks).mockResolvedValue(BOOKS)
    const user = userEvent.setup()
    renderWithProbe()

    await user.click(screen.getByTestId('books-tab-books'))

    const grid = await screen.findByTestId('books-grid')
    // Titles live in the card title attribute; each card's meta line shows
    // its author name (the flat index is every book of every author).
    expect(within(grid).getByTestId('book-card-1')).toHaveAttribute('title', 'The Dispossessed')
    expect(within(grid).getByTestId('book-card-2')).toHaveAttribute('title', 'The Memory Police')
    expect(within(grid).getByTestId('book-card-1')).toHaveTextContent('Ursula K. Le Guin')
    expect(within(grid).getByTestId('book-card-2')).toHaveTextContent('K. William Wilson')
    // The kind pill marks the kind.
    expect(within(grid).getByTestId('book-card-1')).toHaveTextContent('Book')
  })

  it('filters the flat Books index to monitored books through the drawer', async () => {
    vi.mocked(client.listAuthors).mockResolvedValue(pageOf(AUTHORS))
    vi.mocked(client.listBooks).mockResolvedValue(BOOKS)
    const user = userEvent.setup()
    renderWithProbe()

    await user.click(screen.getByTestId('books-tab-books'))
    await user.click(screen.getByTestId('books-index-filter-btn'))
    await user.selectOptions(screen.getByTestId('books-index-facet-monitored'), 'monitored')

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
    await user.selectOptions(screen.getByTestId('books-index-facet-monitored'), 'unmonitored')
    // K. William Wilson's book is the only unmonitored one.
    const grid = await screen.findByTestId('books-grid')
    expect(within(grid).getByTestId('book-card-2')).toBeInTheDocument()
    expect(within(grid).queryByTestId('book-card-1')).not.toBeInTheDocument()
  })

  it('pre-sets the missing shelf from the Bookshelves click-through URL', async () => {
    vi.mocked(client.listAuthors).mockResolvedValue(pageOf(AUTHORS))
    vi.mocked(client.listBooks).mockResolvedValue(BOOKS)
    const user = userEvent.setup()
    renderWithProbe('/books?tab=books&missing=1')

    // The grid shows only the book without a file (the Readarr B4 "missing"
    // shelf) and the drawer pre-checks the shelf facet.
    const grid = await screen.findByTestId('books-grid')
    expect(within(grid).getByTestId('book-card-2')).toBeInTheDocument()
    expect(within(grid).queryByTestId('book-card-1')).not.toBeInTheDocument()
    expect(screen.getByTestId('books-index-filter-btn')).toHaveTextContent('1 Active Filter')

    await user.click(screen.getByTestId('books-index-filter-btn'))
    expect(screen.getByTestId('books-index-facet-shelves-opt-missing')).toBeChecked()
  })

  it('shows a Missing badge on book cards without a file', async () => {
    vi.mocked(client.listAuthors).mockResolvedValue(pageOf(AUTHORS))
    vi.mocked(client.listBooks).mockResolvedValue(BOOKS)
    const user = userEvent.setup()
    renderWithProbe()

    await user.click(screen.getByTestId('books-tab-books'))
    const grid = await screen.findByTestId('books-grid')
    expect(within(grid).getByTestId('book-card-2')).toHaveTextContent('Missing')
    expect(within(grid).getByTestId('book-card-1').querySelector('.badge')).toBeNull()
  })

  it('shows the flat index empty state when there are no books', async () => {
    vi.mocked(client.listAuthors).mockResolvedValue(pageOf(AUTHORS))
    vi.mocked(client.listBooks).mockResolvedValue([])
    const user = userEvent.setup()
    renderWithProbe()

    await user.click(screen.getByTestId('books-tab-books'))

    expect(await screen.findByText('No results')).toBeInTheDocument()
    expect(screen.getByText('Add a book to get started.')).toBeInTheDocument()
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

  it('the flat index list view slices rows and keeps per-book monitoring', async () => {
    vi.mocked(client.listAuthors).mockResolvedValue(pageOf(AUTHORS))
    vi.mocked(client.listBooks).mockResolvedValue(BOOKS)
    vi.mocked(client.setTitleMonitored).mockResolvedValue({ id: 2, monitored: true })
    const user = userEvent.setup()
    renderWithProbe()

    await user.click(screen.getByTestId('books-tab-books'))
    await user.click(screen.getByTestId('books-index-view-list'))

    const table = await screen.findByTestId('books-index-table')
    expect(within(table).getByTestId('book-1')).toHaveTextContent('The Dispossessed')
    expect(within(table).getByTestId('book-2')).toHaveTextContent('The Memory Police')
    expect(within(table).getByTestId('book-quality-2')).toHaveTextContent('—')
    expect(within(table).getByTestId('book-status-2')).toHaveTextContent('Missing')

    await user.click(within(table).getByTestId('book-mon-2'))
    expect(client.setTitleMonitored).toHaveBeenCalledWith(2, true)
  })

  it('navigates to the author detail from an author card', async () => {
    vi.mocked(client.listAuthors).mockResolvedValue(pageOf(AUTHORS))
    const user = userEvent.setup()
    renderWithProbe()

    const grid = await screen.findByTestId('books-authors-grid')
    const card = within(grid).getByTestId('author-card-1')
    // The card cell now carries two buttons (open + hover edit); use the
    // explicit open control.
    await user.click(within(card).getByTestId('author-card-1-open'))

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
    // Readarr B3: the book metadata profile names for the edit modal.
    vi.mocked(client.listBookMetadataProfiles).mockResolvedValue(['Default', 'E-book Only'])
  }

  async function openEditModal(user: ReturnType<typeof userEvent.setup>) {
    renderWithProbe()
    const grid = await screen.findByTestId('books-authors-grid')
    const card = within(grid).getByTestId('author-card-1')
    // The card cell now carries two buttons (open + hover edit); use the
    // explicit open control.
    await user.click(within(card).getByTestId('author-card-1-open'))
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
    // Readarr B3: the metadata profile is a live select, pre-filled from
    // the author and populated from the book metadata profile names.
    const metaProfile = screen.getByTestId('edit-metadata-profile') as HTMLSelectElement
    await waitFor(() => expect(metaProfile).toHaveValue('Default'))
    expect(Array.from(metaProfile.options).map((o) => o.value)).toEqual(['', 'Default', 'E-book Only'])
    expect(
      screen.getByText("Which metadata filters apply to this author's books (empty = server default)"),
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

    // Change the policy, un-monitor, append a tag, and switch the metadata
    // profile (the pre-filled 'Default' → 'E-book Only').
    await user.selectOptions(screen.getByTestId('edit-monitor-policy'), 'missing')
    await user.click(screen.getByTestId('edit-monitored'))
    await user.type(screen.getByTestId('edit-tags-input'), 'e2e{Enter}')
    await user.selectOptions(screen.getByTestId('edit-metadata-profile'), 'E-book Only')

    await user.click(screen.getByTestId('edit-save'))
    await waitFor(() =>
      expect(client.updateAuthor).toHaveBeenCalledWith(1, {
        monitored: false,
        monitor_policy: 'missing',
        root_folder: '/media/books',
        quality_profile: 'EPUB',
        metadata_profile: 'E-book Only',
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
