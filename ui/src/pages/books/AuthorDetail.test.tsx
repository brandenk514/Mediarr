// AuthorDetailPage in-detail Add Edition tests (issue #120 part B).
//
// A book row in the author detail view must be able to add an edition to
// the EXISTING title without leaving the page: the expanded book row
// carries an Add Edition affordance that opens a modal offering the
// provider-fed Open Library editions (the same lookupBooks ->
// getBookWorkEditions chain the /books/add page uses), with a manual
// edition form as the fallback when the provider can't offer editions.
// Submit posts to the existing POST /books/titles/{id}/editions endpoint
// (client.addEdition), reports through the shared status line, and
// refreshes the title's edition list.
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { ApiError, client } from '../../api/client'
import type { Author, BookLookupResult, BookWorkDetails, Title } from '../../api/types'
import { AuthorDetailPage } from './AuthorDetail'

vi.mock('../../api/client', () => {
  const ApiError = class ApiError extends Error {
    status: number
    constructor(status: number, body: string) {
      super(body)
      this.status = status
    }
  }
  const client = {
    getAuthor: vi.fn(),
    listTitles: vi.fn(),
    listAuthorSeries: vi.fn(),
    listRootFolders: vi.fn(),
    listBookMetadataProfiles: vi.fn(),
    getProfiles: vi.fn(),
    updateAuthor: vi.fn(),
    runBooksPipeline: vi.fn(),
    setAuthorMonitored: vi.fn(),
    setTitleMonitored: vi.fn(),
    listEditions: vi.fn(),
    setEditionMonitored: vi.fn(),
    listBookFiles: vi.fn(),
    listAuthorHistory: vi.fn(),
    listTitleSeries: vi.fn(),
    listBookSeries: vi.fn(),
    addBookSeries: vi.fn(),
    linkTitleToSeries: vi.fn(),
    lookupBooks: vi.fn(),
    getBookWorkEditions: vi.fn(),
    addEdition: vi.fn(),
  }
  return { ApiError, client }
})

// ---- fixtures ----

function baseAuthor(over: Partial<Author> = {}): Author {
  return {
    ID: 3,
    Name: 'Ursula K. Le Guin',
    Monitored: true,
    AddedAt: '2026-01-01T00:00:00Z',
    ...over,
  }
}

function baseTitle(over: Partial<Title> = {}): Title {
  return {
    ID: 11,
    AuthorID: 3,
    Name: 'The Left Hand of Darkness',
    Monitored: true,
    AddedAt: '2026-01-01T00:00:00Z',
    ...over,
  }
}

function bookHit(over: Partial<BookLookupResult> = {}): BookLookupResult {
  return {
    kind: 'book',
    name: 'The Left Hand of Darkness',
    author: 'Ursula K. Le Guin',
    year: 1969,
    work_key: 'OL8189755W',
    added: false,
    ...over,
  }
}

function workDetails(over: Partial<BookWorkDetails> = {}): BookWorkDetails {
  return {
    work_key: 'OL8189755W',
    title: 'The Left Hand of Darkness',
    editions: [
      {
        key: 'e1',
        title: 'Grove Press edition',
        formats: ['EPUB', 'PDF'],
        languages: ['en'],
        publishers: ['Grove Press'],
        year: 1969,
        pages: 304,
        isbn: '9780802144001',
      },
      { key: 'e2', title: 'Print scan only', formats: ['PDF'] },
    ],
    ...over,
  }
}

function renderBooks() {
  return render(
    <MemoryRouter initialEntries={['/books/3']}>
      <Routes>
        <Route path="/books/add" element={<div data-testid="books-add-stub">Add book</div>} />
        <Route path="/books/:id" element={<AuthorDetailPage />} />
        <Route path="/books" element={<div data-testid="books-landing">Books</div>} />
      </Routes>
    </MemoryRouter>,
  )
}

// openAddEdition expands the book row (if needed) and opens the Add
// Edition modal for title 11.
async function openAddEdition(user: ReturnType<typeof userEvent.setup>) {
  await screen.findByTestId('book-accordion-11')
  await user.click(screen.getByTestId('book-expand-11'))
  await user.click(await screen.findByTestId('book-add-edition-11'))
  return screen.findByTestId('add-edition-modal')
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(client.getAuthor).mockResolvedValue(baseAuthor())
  vi.mocked(client.listTitles).mockResolvedValue([baseTitle()])
  vi.mocked(client.listAuthorSeries).mockResolvedValue([])
  vi.mocked(client.listRootFolders).mockResolvedValue([])
  vi.mocked(client.listBookMetadataProfiles).mockResolvedValue([])
  vi.mocked(client.getProfiles).mockResolvedValue({
    profiles: ['Best', 'EPUB', 'Kindle', 'Any'],
    default: 'Best',
  })
  vi.mocked(client.updateAuthor).mockResolvedValue({ id: 3, monitored: false })
  vi.mocked(client.runBooksPipeline).mockResolvedValue({ imported: 'left-hand.epub' })
  vi.mocked(client.setAuthorMonitored).mockResolvedValue({ id: 3, monitored: false })
  vi.mocked(client.setTitleMonitored).mockResolvedValue({ id: 11, monitored: false })
  vi.mocked(client.listEditions).mockResolvedValue([])
  vi.mocked(client.setEditionMonitored).mockResolvedValue({ id: 1, monitored: false })
  vi.mocked(client.listBookFiles).mockResolvedValue([])
  vi.mocked(client.listAuthorHistory).mockResolvedValue([])
  vi.mocked(client.listTitleSeries).mockResolvedValue([])
  vi.mocked(client.listBookSeries).mockResolvedValue([])
  vi.mocked(client.addBookSeries).mockResolvedValue({ id: 1 })
  vi.mocked(client.linkTitleToSeries).mockResolvedValue({ title_id: 11, series_id: 1 })
  vi.mocked(client.lookupBooks).mockResolvedValue([bookHit()])
  vi.mocked(client.getBookWorkEditions).mockResolvedValue(workDetails())
  vi.mocked(client.addEdition).mockResolvedValue({ id: 50 })
})

describe('AuthorDetailPage (in-detail Add Edition)', () => {
  it('exposes an Add Edition affordance in the expanded book row', async () => {
    const user = userEvent.setup()
    renderBooks()
    await openAddEdition(user)
    // The modal is addressed to the specific title.
    expect(await screen.findByTestId('add-edition-modal')).toBeInTheDocument()
    expect(screen.getByTestId('add-edition-title')).toHaveTextContent('The Left Hand of Darkness')
  })

  it('lists provider-fed editions and adds the picked ones to the title', async () => {
    const user = userEvent.setup()
    renderBooks()
    await openAddEdition(user)

    // The lookup + editions fetch run for the title's work key.
    expect(client.lookupBooks).toHaveBeenCalledWith('The Left Hand of Darkness')
    expect(client.getBookWorkEditions).toHaveBeenCalledWith('OL8189755W')

    // Both fetched editions are listed; the EPUB one is monitorable
    // (checked by default), the PDF-only scan is not (toggle disabled).
    const e1 = await screen.findByTestId('add-edition-provider-e1')
    expect(e1).toHaveTextContent('Grove Press edition')
    expect(e1).toHaveTextContent('EPUB, PDF')
    expect(e1).toHaveTextContent('en')
    expect(e1).toHaveTextContent('Grove Press')
    expect(e1).toHaveTextContent('1969')
    expect(e1).toHaveTextContent('ISBN 9780802144001')
    expect(screen.getByTestId('add-edition-provider-e1-mon')).toBeChecked()
    expect(screen.getByTestId('add-edition-provider-e2')).toBeInTheDocument()
    expect(screen.getByTestId('add-edition-provider-e2-mon')).toBeDisabled()

    await user.click(screen.getByTestId('add-edition-button'))
    expect(client.addEdition).toHaveBeenCalledWith(11, {
      format: 'epub',
      language: 'en',
      isbn: '9780802144001',
      publisher: 'Grove Press',
      year: 1969,
      pages: 304,
      monitored: true,
    })
    // The modal closes, the status line reports, and the edition list
    // reloads (initial fetch + post-add refresh).
    expect(screen.queryByTestId('add-edition-modal')).not.toBeInTheDocument()
    expect(screen.getByTestId('author-detail-status')).toHaveTextContent(
      'Added 1 edition to "The Left Hand of Darkness".',
    )
    expect(client.listEditions).toHaveBeenCalledTimes(2)
  })

  it('omits un-picked editions and disables submit when none are picked', async () => {
    const user = userEvent.setup()
    renderBooks()
    await openAddEdition(user)
    await screen.findByTestId('add-edition-provider-e1')

    // Unpick the only monitorable edition: submit disables.
    await user.click(screen.getByTestId('add-edition-provider-e1-mon'))
    expect(screen.getByTestId('add-edition-button')).toBeDisabled()

    // Pick it back: submit re-enables and only the picked edition is sent.
    await user.click(screen.getByTestId('add-edition-provider-e1-mon'))
    expect(screen.getByTestId('add-edition-button')).toBeEnabled()
    await user.click(screen.getByTestId('add-edition-button'))
    expect(client.addEdition).toHaveBeenCalledTimes(1)
  })

  it('falls back to the manual form when the work has no fetched editions', async () => {
    const user = userEvent.setup()
    vi.mocked(client.getBookWorkEditions).mockResolvedValue(
      workDetails({ editions: [] }),
    )
    renderBooks()
    await openAddEdition(user)

    // No provider list — the manual edition form is offered instead.
    expect(await screen.findByTestId('add-edition-manual')).toBeInTheDocument()
    expect(screen.queryByTestId('add-edition-provider-e1')).not.toBeInTheDocument()

    // Defaults: EPUB format, monitored on. Fill the optional fields.
    expect(screen.getByTestId('add-edition-format')).toHaveValue('epub')
    expect(screen.getByTestId('add-edition-monitored')).toBeChecked()
    await user.type(screen.getByTestId('add-edition-language'), 'en')
    await user.type(screen.getByTestId('add-edition-isbn'), '9780802144001')
    await user.type(screen.getByTestId('add-edition-year'), '1969')

    await user.click(screen.getByTestId('add-edition-button'))
    expect(client.addEdition).toHaveBeenCalledWith(11, {
      format: 'epub',
      language: 'en',
      isbn: '9780802144001',
      year: 1969,
      monitored: true,
    })
    expect(screen.queryByTestId('add-edition-modal')).not.toBeInTheDocument()
    expect(screen.getByTestId('author-detail-status')).toHaveTextContent(
      'Added 1 edition to "The Left Hand of Darkness".',
    )
  })

  it('falls back to the manual form when the lookup finds no work key', async () => {
    const user = userEvent.setup()
    vi.mocked(client.lookupBooks).mockResolvedValue([
      { kind: 'author', name: 'Ursula K. Le Guin', added: false },
    ])
    renderBooks()
    await openAddEdition(user)

    // No book hit -> no editions fetch, straight to the manual form.
    expect(await screen.findByTestId('add-edition-manual')).toBeInTheDocument()
    expect(client.getBookWorkEditions).not.toHaveBeenCalled()

    // Submitting the default form sends only the required fields.
    await user.click(screen.getByTestId('add-edition-button'))
    expect(client.addEdition).toHaveBeenCalledWith(11, {
      format: 'epub',
      monitored: true,
    })
  })

  it('keeps the modal open and surfaces the error when the add fails', async () => {
    const user = userEvent.setup()
    vi.mocked(client.addEdition).mockRejectedValue(new ApiError(409, 'edition already exists'))
    renderBooks()
    await openAddEdition(user)
    await screen.findByTestId('add-edition-provider-e1')

    await user.click(screen.getByTestId('add-edition-button'))

    // The modal stays open with the API error inline…
    expect(screen.getByTestId('add-edition-modal')).toBeInTheDocument()
    expect(screen.getByTestId('add-edition-error')).toHaveTextContent('edition already exists')
    // …and the shared status line carries the same message.
    expect(screen.getByTestId('author-detail-status')).toHaveTextContent('edition already exists')
  })
})
