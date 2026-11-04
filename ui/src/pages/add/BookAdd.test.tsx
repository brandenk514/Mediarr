import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { client } from '../../api/client'
import type { BookLookupResult } from '../../api/types'
import { BookAddPage } from './BookAdd'

// Page test for the Readarr B2 book add flow: the combined Open Library
// lookup (kind-tagged author/book rows, already-added jump links) and the
// "Add New Author/Book" modal (Root Folder · Monitor · Quality Profile ·
// Metadata Profile (B3 stub, disabled) · Tags, the start-search checkbox,
// and the green add button). The books model is hierarchical, so a book add
// with a new author is two calls (author then title) and with an existing
// author is a direct title add.
vi.mock('../../api/client', () => {
  class ApiError extends Error {
    status: number
    constructor(status: number, body: string) {
      super(body)
      this.status = status
    }
  }
  return {
    ApiError,
    client: {
      lookupBooks: vi.fn(),
      listRootFolders: vi.fn(),
      getProfiles: vi.fn(),
      addAuthor: vi.fn(),
      addTitle: vi.fn(),
      runBooksPipeline: vi.fn(),
    },
  }
})

function renderPage() {
  return render(
    <MemoryRouter initialEntries={['/books/add']}>
      <Routes>
        <Route path="/books/add" element={<BookAddPage />} />
        <Route path="/books/:id" element={<div data-testid="author-detail-landing">Author detail</div>} />
        <Route path="/books" element={<div data-testid="books-landing">Books</div>} />
      </Routes>
    </MemoryRouter>,
  )
}

const authorHit: BookLookupResult = {
  kind: 'author',
  name: 'Ursula K. Le Guin',
  added: false,
  library_id: 0,
  author_id: 0,
}

const addedAuthorHit: BookLookupResult = {
  kind: 'author',
  name: 'K. William Wilson',
  added: true,
  library_id: 7,
  author_id: 7,
}

const bookHit: BookLookupResult = {
  kind: 'book',
  name: 'The Dispossessed',
  author: 'Ursula K. Le Guin',
  year: 1974,
  added: false,
  library_id: 0,
  author_id: 0,
}

const addedBookHit: BookLookupResult = {
  kind: 'book',
  name: 'The Left Hand of Darkness',
  author: 'Ursula K. Le Guin',
  year: 1969,
  added: true,
  library_id: 99,
  author_id: 7,
}

const existingAuthorBookHit: BookLookupResult = {
  kind: 'book',
  name: 'The Word World',
  author: 'Ursula K. Le Guin',
  year: 2001,
  added: false,
  library_id: 0,
  author_id: 7,
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(client.lookupBooks).mockResolvedValue([authorHit, bookHit])
  vi.mocked(client.listRootFolders).mockResolvedValue([
    { ID: 3, Path: '/media/books', Name: 'Books', CreatedAt: '', FreeSpace: 500 * 1024 ** 3 },
  ] as never)
  vi.mocked(client.getProfiles).mockResolvedValue({
    profiles: ['EPUB', 'MOBI'],
    default: 'EPUB',
  } as never)
  vi.mocked(client.addAuthor).mockResolvedValue({ id: 71 })
  vi.mocked(client.addTitle).mockResolvedValue({ id: 55 })
  vi.mocked(client.runBooksPipeline).mockResolvedValue({ imported: '' } as never)
})

afterEach(() => {
  vi.clearAllMocks()
})

describe('BookAddPage (Readarr B2 combined lookup)', () => {
  it('renders kind-tagged rows: author badge, book badge with year and author line', async () => {
    const user = userEvent.setup()
    renderPage()
    await user.type(screen.getByTestId('book-search-input'), 'le guin')

    const authorRow = await screen.findByTestId('book-result-author-0')
    expect(authorRow).toHaveTextContent('Ursula K. Le Guin')
    expect(screen.getByTestId('book-result-author-0-kind')).toHaveTextContent('Author')

    const bookRow = screen.getByTestId('book-result-book-1')
    expect(bookRow).toHaveTextContent('The Dispossessed')
    expect(bookRow).toHaveTextContent('(1974)')
    expect(bookRow).toHaveTextContent('by Ursula K. Le Guin')
    expect(screen.getByTestId('book-result-book-1-kind')).toHaveTextContent('Book')

    // The combined lookup is a single debounced call with the plain term.
    await waitFor(() => expect(client.lookupBooks).toHaveBeenCalledWith('le guin'))
  })

  it('renders an already-added author hit as a jump link to its detail page', async () => {
    vi.mocked(client.lookupBooks).mockResolvedValue([addedAuthorHit])
    const user = userEvent.setup()
    renderPage()
    await user.type(screen.getByTestId('book-search-input'), 'wilson')
    const link = await screen.findByTestId('book-result-author-0')
    expect(link).toHaveAttribute('href', '/books/7')
    expect(link).toHaveTextContent('In library')
    expect(link).toHaveTextContent('Author')
  })

  it('renders an already-added book hit as a jump link to its author detail (/books/{author_id})', async () => {
    vi.mocked(client.lookupBooks).mockResolvedValue([addedBookHit])
    const user = userEvent.setup()
    renderPage()
    await user.type(screen.getByTestId('book-search-input'), 'darkness')
    const link = await screen.findByTestId('book-result-book-0')
    // The link follows the work's author (author_id 7), not the title id
    // (library_id 99) — book rows navigate to the author's detail page.
    expect(link).toHaveAttribute('href', '/books/7')
    expect(link).toHaveTextContent('In library')
    expect(link).toHaveTextContent('Book')
  })

  it('greys out the search on a keyless 503', async () => {
    const { ApiError } = await import('../../api/client')
    vi.mocked(client.lookupBooks).mockRejectedValue(
      new (ApiError as new (s: number, b: string) => Error)(503, 'open library is unavailable'),
    )
    const user = userEvent.setup()
    renderPage()
    await user.type(screen.getByTestId('book-search-input'), 'le guin')
    expect(await screen.findByTestId('book-search-unavailable')).toHaveTextContent(
      'open library is unavailable',
    )
    expect(screen.getByTestId('book-search-input')).toBeDisabled()
  })
})

describe('AddBookModal (Readarr B2 "Add New Author/Book")', () => {
  async function openAuthorModal(user: ReturnType<typeof userEvent.setup>) {
    vi.mocked(client.lookupBooks).mockResolvedValue([authorHit])
    renderPage()
    await user.type(screen.getByTestId('book-search-input'), 'le guin')
    await user.click(await screen.findByTestId('book-result-author-0'))
    return screen.findByTestId('add-book-modal')
  }

  async function openBookModal(hit: BookLookupResult, user: ReturnType<typeof userEvent.setup>) {
    vi.mocked(client.lookupBooks).mockResolvedValue([hit])
    renderPage()
    await user.type(screen.getByTestId('book-search-input'), hit.name.slice(0, 6))
    await user.click(await screen.findByTestId('book-result-book-0'))
    return screen.findByTestId('add-book-modal')
  }

  it('opens with the book field set: root folder, 7 monitor states, quality profile, B3-stub metadata profile, tags', async () => {
    const user = userEvent.setup()
    await openAuthorModal(user)
    await screen.findByTestId('add-book-modal')

    expect(screen.getByTestId('add-book-name')).toHaveTextContent('Ursula K. Le Guin')
    // Field order (the ArtistAdd order, minus the music-only Monitor New
    // Albums, plus the metadata profile stub).
    const order = [
      'add-book-root-folder',
      'add-book-monitor-policy',
      'add-book-quality-profile',
      'add-book-metadata-profile',
      'add-book-tags-input',
    ]
    const byPosition = [...document.querySelectorAll('[data-testid^="add-book-"]')]
      .map((el) => el.getAttribute('data-testid'))
      .filter((id) => id !== null)
    const five = byPosition.filter((id) => order.includes(id as (typeof order)[number]))
    expect(five).toEqual(order)

    // The 7 monitor options, shared with the music add flow.
    const monitor = screen.getByTestId('add-book-monitor-policy') as HTMLSelectElement
    expect(Array.from(monitor.options).map((o) => o.label)).toEqual([
      'All Albums',
      'Future Albums',
      'Missing Albums',
      'Existing Albums',
      'First Album',
      'Lastest Album',
      'None',
    ])
    expect(monitor).toHaveValue('all')
    expect(screen.getByTestId('add-book-monitor-policy-help')).toHaveTextContent(
      'Which books should be monitored after the author is added (one-time adjustment)',
    )

    // Root folder: the bracketed library label + path, the free-space note,
    // and the automatic-subfolder helper.
    const rootFolder = screen.getByTestId('add-book-root-folder') as HTMLSelectElement
    expect(rootFolder.options[0]!.textContent).toContain('[Books] /media/books')
    expect(screen.getByTestId('add-book-root-folder-help')).toHaveTextContent(
      "'Ursula K. Le Guin' subfolder will be created automatically",
    )

    // Quality profile: the books profiles with the default preselected.
    const profile = screen.getByTestId('add-book-quality-profile') as HTMLSelectElement
    await waitFor(() => expect(profile).toHaveValue('EPUB'))
    expect(Array.from(profile.options).map((o) => o.value)).toEqual(['EPUB', 'MOBI'])

    // Metadata profile: no listing endpoint until B3 → disabled stub.
    expect(screen.getByTestId('add-book-metadata-profile')).toBeDisabled()
    expect(screen.getByTestId('add-book-metadata-profile-help')).toHaveTextContent(
      'Metadata profiles land in a later phase (B3).',
    )
  })

  it('adds an author with the full AddAuthorRequest payload and navigates to /books', async () => {
    const user = userEvent.setup()
    await openAuthorModal(user)
    await screen.findByTestId('book-add-button')

    await user.selectOptions(screen.getByTestId('add-book-monitor-policy'), 'existing')
    await user.type(screen.getByTestId('add-book-tags-input'), 'sci-fi{Enter}')
    expect(screen.getByTestId('add-book-tags-chips')).toHaveTextContent('sci-fi')
    await user.click(screen.getByTestId('book-start-search'))
    await user.click(screen.getByTestId('book-add-button'))

    await waitFor(() =>
      expect(client.addAuthor).toHaveBeenCalledWith({
        name: 'Ursula K. Le Guin',
        monitor_policy: 'existing',
        start_search: true,
        root_folder: '/media/books',
        quality_profile: 'EPUB',
        tags: ['sci-fi'],
      }),
    )
    // An author add is a single call — no title add on top of it.
    expect(client.addTitle).not.toHaveBeenCalled()
    expect(await screen.findByTestId('books-landing')).toBeInTheDocument()
  })

  it('adds a book with a new author in two steps (author, then title) and navigates to /books', async () => {
    const user = userEvent.setup()
    await openBookModal(bookHit, user)
    await screen.findByTestId('book-add-button')
    await user.click(screen.getByTestId('book-add-button'))

    // The work's author is created first with the modal fields…
    await waitFor(() =>
      expect(client.addAuthor).toHaveBeenCalledWith({
        name: 'Ursula K. Le Guin',
        monitor_policy: 'all',
        start_search: false,
        root_folder: '/media/books',
        quality_profile: 'EPUB',
      }),
    )
    // …then the title lands in the new author, monitored.
    await waitFor(() =>
      expect(client.addTitle).toHaveBeenCalledWith(71, { name: 'The Dispossessed', monitored: true }),
    )
    expect(await screen.findByTestId('books-landing')).toBeInTheDocument()
  })

  it('adds a book whose author is already in the library directly as a title add', async () => {
    const user = userEvent.setup()
    await openBookModal(existingAuthorBookHit, user)
    await screen.findByTestId('book-add-button')
    await user.click(screen.getByTestId('book-add-button'))

    await waitFor(() =>
      expect(client.addTitle).toHaveBeenCalledWith(7, { name: 'The Word World', monitored: true }),
    )
    expect(client.addAuthor).not.toHaveBeenCalled()
    expect(await screen.findByTestId('books-landing')).toBeInTheDocument()
  })

  it('surfaces the 400 validation error in the modal', async () => {
    const { ApiError } = await import('../../api/client')
    vi.mocked(client.addAuthor).mockRejectedValue(
      new (ApiError as new (s: number, b: string) => Error)(400, 'name is required'),
    )
    const user = userEvent.setup()
    await openAuthorModal(user)
    await user.click(await screen.findByTestId('book-add-button'))
    expect(await screen.findByTestId('book-add-error')).toHaveTextContent('name is required')
  })
})
