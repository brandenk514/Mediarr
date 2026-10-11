import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { client } from '../../api/client'
import type { Author, Edition, Page, Title } from '../../api/types'
import { BooksPage } from './Books'

// Page test for the books list (#41): paginated authors with two-level lazy
// expansion (author → title → edition).
vi.mock('../../api/client', () => ({
  client: {
    listAuthors: vi.fn(),
    listTitles: vi.fn(),
    listEditions: vi.fn(),
    setAuthorMonitored: vi.fn(),
    setTitleMonitored: vi.fn(),
    setEditionMonitored: vi.fn(),
  },
}))

function authorPage(items: Author[], total = items.length, pg = 1): Page<Author> {
  return { items, total, page: pg, page_size: 25 }
}

function author(id: number, name: string, monitored = true): Author {
  return { ID: id, Name: name, Monitored: monitored, AddedAt: '' }
}

function title(id: number, authorId: number, name: string): Title {
  return { ID: id, AuthorID: authorId, Name: name, Year: 1984, QualityProfile: 'epub', Monitored: true, AddedAt: '' }
}

function edition(id: number, titleId: number, format: string): Edition {
  return { ID: id, TitleID: titleId, Format: format, Publisher: 'P', ISBN: '978-1', Year: 1984, Pages: 300, Monitored: true, AddedAt: '' }
}

beforeEach(() => {
  vi.mocked(client.listAuthors).mockReset()
  vi.mocked(client.listTitles).mockReset()
  vi.mocked(client.listEditions).mockReset()
})

afterEach(() => {
  vi.clearAllMocks()
})

describe('BooksPage', () => {
  it('lazily expands author → title → edition, fetching each level on demand', async () => {
    vi.mocked(client.listAuthors).mockResolvedValue(authorPage([author(1, 'Aldous Huxley')]))
    vi.mocked(client.listTitles).mockResolvedValue([title(21, 1, 'Brave New World')])
    vi.mocked(client.listEditions).mockResolvedValue([edition(311, 21, 'epub'), edition(312, 21, 'pdf')])
    const user = userEvent.setup()
    render(<BooksPage />)

    await screen.findByTestId('author-1')

    expect(client.listTitles).not.toHaveBeenCalled()
    expect(client.listEditions).not.toHaveBeenCalled()

    // Level 1: expand the author → fetch titles.
    await user.click(screen.getByTestId('author-toggle-1'))
    expect(await screen.findByTestId('titles-1')).toBeInTheDocument()
    expect(client.listTitles).toHaveBeenCalledWith(1)
    expect(screen.getByTestId('title-toggle-21')).toBeInTheDocument()

    // Level 2: expand the title → fetch editions.
    await user.click(screen.getByTestId('title-toggle-21'))
    expect(await screen.findByTestId('editions-21')).toBeInTheDocument()
    expect(client.listEditions).toHaveBeenCalledWith(21)
    // Both editions render their format.
    expect(screen.getAllByText('epub').length).toBeGreaterThanOrEqual(1)
    expect(screen.getByText('pdf')).toBeInTheDocument()
  })

  it('shows an empty message when an author has no titles', async () => {
    vi.mocked(client.listAuthors).mockResolvedValue(authorPage([author(2, 'New Author')]))
    vi.mocked(client.listTitles).mockResolvedValue([])
    const user = userEvent.setup()
    render(<BooksPage />)
    await screen.findByTestId('author-2')
    await user.click(screen.getByTestId('author-toggle-2'))
    expect(await screen.findByText('No titles yet.')).toBeInTheDocument()
  })

  it('toggles an edition monitored optimistically', async () => {
    vi.mocked(client.listAuthors).mockResolvedValue(authorPage([author(1, 'Aldous Huxley')]))
    vi.mocked(client.listTitles).mockResolvedValue([title(21, 1, 'Brave New World')])
    vi.mocked(client.listEditions).mockResolvedValue([edition(311, 21, 'epub')])
    vi.mocked(client.setEditionMonitored).mockResolvedValue({ id: 311, monitored: false })
    const user = userEvent.setup()
    render(<BooksPage />)
    await screen.findByTestId('author-1')
    await user.click(screen.getByTestId('author-toggle-1'))
    await user.click(screen.getByTestId('title-toggle-21'))
    await screen.findByTestId('edition-mon-311')

    await user.click(screen.getByTestId('edition-mon-311'))
    expect(client.setEditionMonitored).toHaveBeenCalledWith(311, false)
    // Toggling a child does NOT refetch the top-level list.
    expect(client.listAuthors).toHaveBeenCalledTimes(1)
  })
})
