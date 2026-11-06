import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { client } from '../../api/client'
import type { BookLookupResult, BookWorkDetails } from '../../api/types'
import { BookAddPage, olWorkKeyOf, resolveEditionFormat } from './BookAdd'

// Page test for the Readarr B2 book add flow: the combined Open Library
// lookup (kind-tagged author/book rows, already-added jump links) and the
// "Add New Author/Book" modal (Root Folder · Monitor · Quality Profile ·
// Metadata Profile · Tags, the start-search checkbox, and the green add
// button). The books model is hierarchical, so a book add with a new author
// is two calls (author then title) and with an existing author is a direct
// title add.
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
      // Readarr B3: the book metadata profile names for the add modal.
      listBookMetadataProfiles: vi.fn(),
      addAuthor: vi.fn(),
      addTitle: vi.fn(),
      // Readarr MT2: a work's editions (provider-fed add flow) and the
      // edition create the add modal rides the picked editions onto.
      getBookWorkEditions: vi.fn(),
      addEdition: vi.fn(),
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
  vi.mocked(client.listBookMetadataProfiles).mockResolvedValue(['Default', 'E-book Only'])
  vi.mocked(client.addAuthor).mockResolvedValue({ id: 71 })
  vi.mocked(client.addTitle).mockResolvedValue({ id: 55 })
  vi.mocked(client.addEdition).mockResolvedValue({ id: 1 })
  vi.mocked(client.getBookWorkEditions).mockResolvedValue({
    work_key: 'OL59863W',
    title: 'The Dispossessed',
    editions: [],
  })
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

  it('opens with the book field set: root folder, 7 monitor states, quality profile, metadata profile, tags', async () => {
    const user = userEvent.setup()
    await openAuthorModal(user)
    await screen.findByTestId('add-book-modal')

    expect(screen.getByTestId('add-book-name')).toHaveTextContent('Ursula K. Le Guin')
    // Field order (the ArtistAdd order, minus the music-only Monitor New
    // Albums, plus the metadata profile).
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

    // Metadata profile (Readarr B3): a live select populated from the book
    // metadata profile names, '— None —' by default.
    const metaProfile = screen.getByTestId('add-book-metadata-profile') as HTMLSelectElement
    await waitFor(() =>
      expect(Array.from(metaProfile.options).map((o) => o.value)).toEqual(['', 'Default', 'E-book Only']),
    )
    expect(metaProfile).toHaveValue('')
    expect(metaProfile).not.toBeDisabled()
    expect(screen.getByTestId('add-book-metadata-profile-help')).toHaveTextContent(
      "Which metadata filters to apply when importing this author's books",
    )
  })

  it('adds an author with the full AddAuthorRequest payload and navigates to /books', async () => {
    const user = userEvent.setup()
    await openAuthorModal(user)
    await screen.findByTestId('book-add-button')

    await user.selectOptions(screen.getByTestId('add-book-monitor-policy'), 'existing')
    await user.type(screen.getByTestId('add-book-tags-input'), 'sci-fi{Enter}')
    expect(screen.getByTestId('add-book-tags-chips')).toHaveTextContent('sci-fi')
    // Readarr B3: pick a metadata profile — it lands in the payload.
    await user.selectOptions(screen.getByTestId('add-book-metadata-profile'), 'Default')
    await user.click(screen.getByTestId('book-start-search'))
    await user.click(screen.getByTestId('book-add-button'))

    await waitFor(() =>
      expect(client.addAuthor).toHaveBeenCalledWith({
        name: 'Ursula K. Le Guin',
        monitor_policy: 'existing',
        start_search: true,
        root_folder: '/media/books',
        quality_profile: 'EPUB',
        metadata_profile: 'Default',
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

describe('MT1 id-lookup UX', () => {
  it('advertises the id-lookup syntax under the search box', () => {
    renderPage()
    const hint = screen.getByTestId('book-search-hint')
    expect(hint).toHaveTextContent('isbn:')
    expect(hint).toHaveTextContent('asin:')
    expect(hint).toHaveTextContent('goodreads:')
    expect(hint).toHaveTextContent('olid:')
  })

  it('renders a single id-lookup hit as a selectable work row', async () => {
    const idHit: BookLookupResult = {
      kind: 'book',
      name: 'The Dispossessed',
      author: '',
      year: 1974,
      added: false,
      library_id: 0,
      author_id: 0,
    }
    vi.mocked(client.lookupBooks).mockResolvedValue([idHit])
    const user = userEvent.setup()
    renderPage()
    await user.type(screen.getByTestId('book-search-input'), 'isbn:9780140449335')
    const row = await screen.findByTestId('book-result-book-0')
    expect(screen.getByTestId('book-result-book-0-kind')).toHaveTextContent('Book')
    await user.click(row)
    expect(await screen.findByTestId('add-book-modal')).toBeVisible()
    expect(screen.getByTestId('add-book-byline')).toHaveTextContent('Author unknown')
  })
})

describe('olWorkKeyOf / resolveEditionFormat (MT1/MT2 helpers)', () => {
  it('extracts the work key from the key forms, case-insensitively', () => {
    expect(olWorkKeyOf('OL59863W')).toBe('OL59863W')
    expect(olWorkKeyOf('olid:OL59863W')).toBe('OL59863W')
    expect(olWorkKeyOf('OLID:ol59863w')).toBe('OL59863W')
    expect(olWorkKeyOf('http://openlibrary.org/works/OL59863W')).toBe('OL59863W')
  })

  it('rejects non-key queries (text, isbn/asins, malformed keys)', () => {
    expect(olWorkKeyOf('The Dispossessed')).toBeNull()
    expect(olWorkKeyOf('isbn:9780140449335')).toBeNull()
    expect(olWorkKeyOf('asin:B00005ZUJ2')).toBeNull()
    expect(olWorkKeyOf('goodreads:54466')).toBeNull()
    expect(olWorkKeyOf('9780140449335')).toBeNull()
    expect(olWorkKeyOf('OLx1W')).toBeNull()
    expect(olWorkKeyOf('')).toBeNull()
  })

  it('maps OL formats onto the quality-profile format enum', () => {
    expect(resolveEditionFormat(['PDF', 'EPUB'])).toBe('epub')
    expect(resolveEditionFormat(['mobi'])).toBe('mobi')
    expect(resolveEditionFormat(['AZW3'])).toBe('azw3')
    expect(resolveEditionFormat(['PDF'])).toBeNull()
    expect(resolveEditionFormat(undefined)).toBeNull()
  })
})

describe('MT2 provider-fed editions in the add flow', () => {
  // A work-key id-lookup hit: the wire shape for an id query is a single
  // kind=book hit with an empty author.
  const idBookHit: BookLookupResult = {
    kind: 'book',
    name: 'The Dispossessed',
    author: '',
    year: 1974,
    added: false,
    library_id: 0,
    author_id: 0,
  }

  // The shape getBookWorkEditions returns for a keyed work: three editions —
  // a monitorable EPUB+PDF, a PDF-only (unmonitorable) scan, and a bare
  // key-only edition with no title/isbn/publisher/year.
  const sampleDetails = {
    work_key: 'OL59863W',
    title: 'The Dispossessed',
    authors: ['Ursula K. Le Guin'],
    cover_url: 'https://covers.openlibrary.org/b/isbn/9781910902434-M.jpg',
    first_publish_year: 1974,
    editions: [
      {
        key: 'OL25828717M',
        title: 'The Dispossessed (Essays in Science Fiction)',
        isbn: '9781910902434',
        formats: ['EPUB', 'PDF'],
        languages: ['eng'],
        publishers: ['Island Books'],
        publish_date: '2018-08-31',
        year: 2018,
        pages: 348,
      },
      {
        key: 'OL17731959M',
        title: '',
        isbn: '9780802143022',
        formats: ['PDF'],
        languages: ['eng'],
        publishers: ['Ballantine Books'],
        publish_date: '1993-10-01',
        year: 1993,
        pages: 348,
      },
      {
        key: 'OL35929472M',
        title: '',
        isbn: '',
        formats: ['EPUB', 'MOBI'],
        languages: ['eng'],
        publishers: [],
        publish_date: '',
        year: 0,
        pages: 0,
      },
    ],
  }

  async function openWorkKeyModal(query: string, user: ReturnType<typeof userEvent.setup>) {
    renderPage()
    await user.type(screen.getByTestId('book-search-input'), query)
    await user.click(await screen.findByTestId('book-result-book-0'))
    return screen.findByTestId('add-book-modal')
  }

  it('offers the fetched editions pre-filled, each with a checked monitored toggle', async () => {
    vi.mocked(client.getBookWorkEditions).mockResolvedValue(sampleDetails)
    vi.mocked(client.lookupBooks).mockResolvedValue([idBookHit])
    const user = userEvent.setup()
    await openWorkKeyModal('olid:OL59863W', user)

    const first = await screen.findByTestId('add-book-edition-OL25828717M')
    expect(first).toHaveTextContent('The Dispossessed (Essays in Science Fiction)')
    expect(first).toHaveTextContent('EPUB, PDF')
    expect(first).toHaveTextContent('eng')
    expect(first).toHaveTextContent('Island Books')
    expect(first).toHaveTextContent('2018')
    expect(first).toHaveTextContent('ISBN 9781910902434')

    // A bare work-key lookup drives the same fetch; the key is normalised.
    await waitFor(() =>
      expect(client.getBookWorkEditions).toHaveBeenCalledWith('OL59863W'),
    )

    // The titleless edition shows a placeholder title.
    expect(screen.getByTestId('add-book-edition-title-OL35929472M')).toHaveTextContent('—')

    // Per-edition monitored toggles default to checked.
    for (const key of ['OL25828717M', 'OL17731959M', 'OL35929472M']) {
      expect(screen.getByTestId(`add-book-edition-mon-${key}`)).toBeChecked()
    }
    // The PDF-only edition has no monitorable format: its toggle is disabled.
    expect(screen.getByTestId('add-book-edition-mon-OL17731959M')).toBeDisabled()
    expect(screen.getByTestId('add-book-edition-OL17731959M')).toHaveTextContent(
      'no monitorable format',
    )
  })

  it('shows a loading state while the editions fetch is in flight', async () => {
    let resolveEditions: (value: BookWorkDetails) => void = () => {}
    vi.mocked(client.getBookWorkEditions).mockReturnValue(
      new Promise<BookWorkDetails>((res) => {
        resolveEditions = res
      }),
    )
    vi.mocked(client.lookupBooks).mockResolvedValue([idBookHit])
    const user = userEvent.setup()
    renderPage()
    // Case-insensitive prefix + lowercase key (the backend's lenient form).
    await user.type(screen.getByTestId('book-search-input'), 'OLID:ol59863w')
    await user.click(await screen.findByTestId('book-result-book-0'))
    expect(await screen.findByTestId('add-book-editions-loading')).toBeInTheDocument()
    resolveEditions(sampleDetails)
    expect(await screen.findByTestId('add-book-editions')).toBeInTheDocument()
  })

  it('falls back to the manual form when the editions call rejects', async () => {
    vi.mocked(client.getBookWorkEditions).mockRejectedValue(new Error('upstream'))
    vi.mocked(client.lookupBooks).mockResolvedValue([idBookHit])
    const user = userEvent.setup()
    await openWorkKeyModal('OL59863W', user)
    expect(await screen.findByTestId('add-book-edition-manual')).toBeInTheDocument()
    expect(screen.getByTestId('add-book-editions-note')).toHaveTextContent(
      'unavailable for this work',
    )
    expect(screen.queryByTestId('add-book-editions')).not.toBeInTheDocument()
  })

  it('falls back to the manual form when the work has no editions', async () => {
    vi.mocked(client.getBookWorkEditions).mockResolvedValue({
      work_key: 'OL59863W',
      title: 'The Dispossessed',
      authors: ['Ursula K. Le Guin'],
      cover_url: '',
      first_publish_year: 1974,
      editions: [],
    })
    vi.mocked(client.lookupBooks).mockResolvedValue([idBookHit])
    const user = userEvent.setup()
    await openWorkKeyModal('OL59863W', user)
    expect(await screen.findByTestId('add-book-edition-manual')).toBeInTheDocument()
    expect(screen.getByTestId('add-book-editions-note')).toHaveTextContent(
      'unavailable for this work',
    )
  })

  it('renders the provider-fed editions for a plain-text work hit carrying work_key (MT2)', async () => {
    // The MT2 wire shape: EVERY kind=book hit carries the work key, so a
    // plain-text selection fetches editions too — the hit's key, not the
    // plain-text query, drives the fetch.
    const plainHit: BookLookupResult = {
      kind: 'book',
      name: 'The Dispossessed',
      author: 'Ursula K. Le Guin',
      year: 1974,
      work_key: 'OL59863W',
      added: false,
      library_id: 0,
      author_id: 0,
    }
    vi.mocked(client.getBookWorkEditions).mockResolvedValue(sampleDetails)
    vi.mocked(client.lookupBooks).mockResolvedValue([plainHit])
    const user = userEvent.setup()
    renderPage()
    await user.type(screen.getByTestId('book-search-input'), 'The Di')
    await user.click(await screen.findByTestId('book-result-book-0'))

    await screen.findByTestId('add-book-editions')
    expect(screen.getByTestId('add-book-edition-OL25828717M')).toHaveTextContent(
      'The Dispossessed (Essays in Science Fiction)',
    )
    await waitFor(() =>
      expect(client.getBookWorkEditions).toHaveBeenCalledWith('OL59863W'),
    )
    // No manual form alongside the provider list.
    expect(screen.queryByTestId('add-book-edition-manual')).not.toBeInTheDocument()
  })

  it('falls back to the manual form when the hit carries an empty work_key', async () => {
    const emptyKeyHit: BookLookupResult = { ...bookHit, work_key: '' }
    vi.mocked(client.lookupBooks).mockResolvedValue([emptyKeyHit])
    const user = userEvent.setup()
    renderPage()
    await user.type(screen.getByTestId('book-search-input'), 'The Di')
    await user.click(await screen.findByTestId('book-result-book-0'))
    expect(await screen.findByTestId('add-book-edition-manual')).toBeInTheDocument()
    expect(screen.getByTestId('add-book-editions-note')).toHaveTextContent(
      "can't be fetched automatically",
    )
    expect(client.getBookWorkEditions).not.toHaveBeenCalled()
  })

  it('falls back to the query-derived key when the hit work_key is not OL-key-shaped', async () => {
    // Robustness: a malformed work_key on the wire must not break the
    // keyed-work path — the olWorkKeyOf(query) derivation still drives it.
    const shapedHit: BookLookupResult = { ...idBookHit, work_key: 'OLx1W' }
    vi.mocked(client.getBookWorkEditions).mockResolvedValue(sampleDetails)
    vi.mocked(client.lookupBooks).mockResolvedValue([shapedHit])
    const user = userEvent.setup()
    await openWorkKeyModal('olid:OL59863W', user)
    await screen.findByTestId('add-book-editions')
    await waitFor(() =>
      expect(client.getBookWorkEditions).toHaveBeenCalledWith('OL59863W'),
    )
  })

  it('shows the manual form for a plain-text work (no work key to fetch with)', async () => {
    vi.mocked(client.lookupBooks).mockResolvedValue([bookHit])
    const user = userEvent.setup()
    renderPage()
    await user.type(screen.getByTestId('book-search-input'), 'The Di')
    await user.click(await screen.findByTestId('book-result-book-0'))
    expect(await screen.findByTestId('add-book-edition-manual')).toBeInTheDocument()
    expect(screen.getByTestId('add-book-editions-note')).toHaveTextContent(
      "can't be fetched automatically",
    )
    expect(client.getBookWorkEditions).not.toHaveBeenCalled()
  })

  it('submits the picked provider editions with format, language, and monitored', async () => {
    vi.mocked(client.getBookWorkEditions).mockResolvedValue(sampleDetails)
    vi.mocked(client.lookupBooks).mockResolvedValue([idBookHit])
    const user = userEvent.setup()
    await openWorkKeyModal('olid:OL59863W', user)
    await screen.findByTestId('add-book-editions')
    await user.click(screen.getByTestId('book-add-button'))

    // The id hit has no author: author row first, then the title, then the
    // picked editions (the PDF-only one is skipped — unmonitorable).
    await waitFor(() =>
      expect(client.addTitle).toHaveBeenCalledWith(71, {
        name: 'The Dispossessed',
        monitored: true,
      }),
    )
    expect(client.addEdition).toHaveBeenCalledTimes(2)
    expect(client.addEdition).toHaveBeenCalledWith(55, {
      format: 'epub',
      language: 'eng',
      isbn: '9781910902434',
      publisher: 'Island Books',
      year: 2018,
      pages: 348,
      monitored: true,
    })
    expect(client.addEdition).toHaveBeenCalledWith(55, {
      format: 'epub',
      language: 'eng',
      monitored: true,
    })
    expect(await screen.findByTestId('books-landing')).toBeInTheDocument()
  })

  it('honours per-edition monitored picks (an unchecked edition is not created)', async () => {
    vi.mocked(client.getBookWorkEditions).mockResolvedValue(sampleDetails)
    vi.mocked(client.lookupBooks).mockResolvedValue([idBookHit])
    const user = userEvent.setup()
    await openWorkKeyModal('olid:OL59863W', user)
    // Uncheck the first edition by clicking its (checked) toggle.
    await user.click(await screen.findByTestId('add-book-edition-mon-OL25828717M'))
    await user.click(await screen.findByTestId('book-add-button'))
    await waitFor(() => expect(client.addTitle).toHaveBeenCalled())
    expect(client.addEdition).toHaveBeenCalledTimes(1)
    expect(client.addEdition).toHaveBeenCalledWith(55, {
      format: 'epub',
      language: 'eng',
      monitored: true,
    })
    expect(await screen.findByTestId('books-landing')).toBeInTheDocument()
  })

  it('submits the manual edition when automatic editions are unavailable', async () => {
    vi.mocked(client.getBookWorkEditions).mockRejectedValue(new Error('upstream'))
    vi.mocked(client.lookupBooks).mockResolvedValue([idBookHit])
    const user = userEvent.setup()
    await openWorkKeyModal('OL59863W', user)
    await screen.findByTestId('add-book-edition-manual')
    await user.selectOptions(screen.getByTestId('add-book-edition-format'), 'mobi')
    await user.type(screen.getByTestId('add-book-edition-language'), 'eng')
    await user.type(screen.getByTestId('add-book-edition-isbn'), '9780140449335')
    await user.type(screen.getByTestId('add-book-edition-publisher'), 'Ballantine')
    await user.type(screen.getByTestId('add-book-edition-year'), '1974')
    await user.click(await screen.findByTestId('book-add-button'))
    expect(client.addEdition).toHaveBeenCalledWith(55, {
      format: 'mobi',
      language: 'eng',
      isbn: '9780140449335',
      publisher: 'Ballantine',
      year: 1974,
      monitored: true,
    })
    expect(await screen.findByTestId('books-landing')).toBeInTheDocument()
  })

  it('adds the book without an edition when the manual edition is not monitored', async () => {
    vi.mocked(client.getBookWorkEditions).mockRejectedValue(new Error('upstream'))
    vi.mocked(client.lookupBooks).mockResolvedValue([idBookHit])
    const user = userEvent.setup()
    await openWorkKeyModal('OL59863W', user)
    await user.click(await screen.findByTestId('add-book-edition-monitored'))
    await user.click(await screen.findByTestId('book-add-button'))
    await waitFor(() => expect(client.addTitle).toHaveBeenCalled())
    expect(client.addEdition).not.toHaveBeenCalled()
    expect(await screen.findByTestId('books-landing')).toBeInTheDocument()
  })
})

describe('MT4 metadata profile on the lookup + editions calls', () => {
  // A work-key id-lookup hit (the MT2 shape): a single kind=book row that
  // drives the editions fetch from the query.
  const idBookHit: BookLookupResult = {
    kind: 'book',
    name: 'The Dispossessed',
    author: '',
    year: 1974,
    added: false,
    library_id: 0,
    author_id: 0,
  }

  // One monitorable edition so the provider list renders (the default mock
  // answers an empty list, which routes to the manual form).
  const oneEdition: BookWorkDetails = {
    work_key: 'OL59863W',
    title: 'The Dispossessed',
    editions: [
      {
        key: 'OL25828717M',
        title: 'The Dispossessed (Essays in Science Fiction)',
        isbn: '9781910902434',
        formats: ['EPUB'],
        languages: ['eng'],
        publishers: ['Island Books'],
        publish_date: '2018-08-31',
        year: 2018,
        pages: 348,
      },
    ],
  }

  // The metadata profile names arrive async; wait for the modal + the
  // populated select before picking.
  async function awaitMetadataProfileOptions() {
    await screen.findByTestId('add-book-modal')
    const metaProfile = screen.getByTestId('add-book-metadata-profile') as HTMLSelectElement
    await waitFor(() =>
      expect(Array.from(metaProfile.options).map((o) => o.value)).toEqual([
        '',
        'Default',
        'E-book Only',
      ]),
    )
    return metaProfile
  }

  it('passes the selected metadata profile to the lookup call (and re-runs it)', async () => {
    const user = userEvent.setup()
    renderPage()
    await user.type(screen.getByTestId('book-search-input'), 'le guin')
    await user.click(await screen.findByTestId('book-result-author-0'))
    // The first lookup (before a profile is picked) sends no profile param.
    await waitFor(() => expect(client.lookupBooks).toHaveBeenCalledTimes(1))
    expect(client.lookupBooks).toHaveBeenLastCalledWith('le guin')
    const metaProfile = await awaitMetadataProfileOptions()
    await user.selectOptions(metaProfile, 'Default')
    // Picking a profile re-runs the lookup with `profile` set.
    await waitFor(() => expect(client.lookupBooks).toHaveBeenCalledWith('le guin', 'Default'))
    expect(client.lookupBooks).toHaveBeenCalledTimes(2)
  })

  it("omits the profile param for '— None —' (the pre-MT4 call)", async () => {
    const user = userEvent.setup()
    renderPage()
    await user.type(screen.getByTestId('book-search-input'), 'le guin')
    await user.click(await screen.findByTestId('book-result-author-0'))
    const metaProfile = await awaitMetadataProfileOptions()
    // Pick a profile, then back to '— None —'.
    await user.selectOptions(metaProfile, 'Default')
    await waitFor(() => expect(client.lookupBooks).toHaveBeenCalledWith('le guin', 'Default'))
    await user.selectOptions(metaProfile, '')
    // '— None —' re-sends the parameter-less lookup (exactly one argument).
    await waitFor(() => {
      const calls = vi.mocked(client.lookupBooks).mock.calls
      expect(calls.length).toBe(3)
      expect(calls[calls.length - 1]).toEqual(['le guin'])
    })
  })

  it('passes the selected metadata profile to the editions call too', async () => {
    vi.mocked(client.lookupBooks).mockResolvedValue([idBookHit])
    vi.mocked(client.getBookWorkEditions).mockResolvedValue(oneEdition)
    const user = userEvent.setup()
    renderPage()
    await user.type(screen.getByTestId('book-search-input'), 'olid:OL59863W')
    await user.click(await screen.findByTestId('book-result-book-0'))
    await screen.findByTestId('add-book-editions')
    // The first editions fetch (no profile picked) sends no profile param.
    expect(client.getBookWorkEditions).toHaveBeenCalledTimes(1)
    expect(client.getBookWorkEditions).toHaveBeenLastCalledWith('OL59863W')
    const metaProfile = await awaitMetadataProfileOptions()
    await user.selectOptions(metaProfile, 'E-book Only')
    // Picking a profile re-fetches the editions with `profile` set.
    await waitFor(() =>
      expect(client.getBookWorkEditions).toHaveBeenCalledWith('OL59863W', 'E-book Only'),
    )
    expect(client.getBookWorkEditions).toHaveBeenCalledTimes(2)
  })
})
