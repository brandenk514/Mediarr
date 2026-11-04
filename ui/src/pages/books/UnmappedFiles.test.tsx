import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { client } from '../../api/client'
import type { UnmappedFile } from '../../api/types'
import { UnmappedBooksFilesPage } from './UnmappedFiles'

// Page test for the Readarr B3 books Unmapped Files page: a read-only
// listing of the book files under the books media root not mapped to a
// monitored author (Path · Size · Date Added · Quality). Covers the five
// states: loading, populated, empty, 404-no-root, and 401-auth-lost.
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
      listBookUnmappedFiles: vi.fn(),
    },
  }
})

const FILES: UnmappedFile[] = [
  {
    path: '/media/books/Unsorted/Book A.epub',
    size: 1048576, // 1 MiB
    date_added: '2026-11-01T00:00:00Z',
    quality: 'EPUB',
  },
  {
    path: '/media/books/Some Author/Book B.mobi',
    size: 524288, // 512 KiB
    date_added: '2026-11-02T00:00:00Z',
    quality: 'MOBI',
  },
]

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(client.listBookUnmappedFiles).mockResolvedValue(FILES)
})

afterEach(() => {
  vi.clearAllMocks()
})

describe('UnmappedBooksFilesPage (Readarr B3)', () => {
  it('lists unmapped book files with the Path · Size · Date Added · Quality columns', async () => {
    render(<UnmappedBooksFilesPage />)
    // Wait for the populated state to settle first — the loading-state
    // heading is replaced when the data renders, so assert the heading
    // only after the rows are in the document.
    await screen.findByTestId('books-unmapped-row-0')
    expect(screen.getByRole('heading', { name: 'Unmapped Files' })).toBeInTheDocument()
    // Two files, 1.5 MiB total (1 MiB + 512 KiB).
    expect(screen.getByTestId('books-unmapped-row-1')).toBeInTheDocument()
    expect(screen.getByTestId('books-unmapped-row-0')).toHaveTextContent('/media/books/Unsorted/Book A.epub')
    expect(screen.getByTestId('books-unmapped-row-0')).toHaveTextContent('1 MiB')
    expect(screen.getByTestId('books-unmapped-row-1')).toHaveTextContent('512 KiB')
    // Quality is the book format (the music codec analogue).
    expect(screen.getByTestId('books-unmapped-row-0')).toHaveTextContent('EPUB')
    expect(screen.getByTestId('books-unmapped-row-1')).toHaveTextContent('MOBI')
    // The count/total summary.
    expect(screen.getByTestId('books-unmapped')).toHaveTextContent('2 files · 1.5 MiB total')
    // The header carries the four column labels.
    expect(screen.getByTestId('books-unmapped')).toHaveTextContent('Path')
    expect(screen.getByTestId('books-unmapped')).toHaveTextContent('Size')
    expect(screen.getByTestId('books-unmapped')).toHaveTextContent('Date Added')
    expect(screen.getByTestId('books-unmapped')).toHaveTextContent('Quality')
  })

  it('shows the empty state when nothing is unmapped', async () => {
    vi.mocked(client.listBookUnmappedFiles).mockResolvedValue([])
    render(<UnmappedBooksFilesPage />)
    expect(await screen.findByTestId('books-unmapped-empty')).toHaveTextContent(
      'Every book file is mapped to a monitored author.',
    )
    expect(screen.getByTestId('books-unmapped')).toHaveTextContent('Nothing is unmapped.')
    // No table rows render for the empty walk.
    expect(screen.queryByTestId('books-unmapped-row-0')).not.toBeInTheDocument()
  })

  it('renders the loading state before the fetch settles', async () => {
    // A promise that never resolves keeps the page in its loading state.
    vi.mocked(client.listBookUnmappedFiles).mockReturnValue(
      new Promise<UnmappedFile[]>(() => {}) as Promise<UnmappedFile[]>,
    )
    render(<UnmappedBooksFilesPage />)
    expect(screen.getByTestId('books-unmapped')).toHaveTextContent('Loading unmapped book files…')
  })

  it('surfaces a 404 (no books root folder configured) with a retry', async () => {
    const { ApiError } = await import('../../api/client')
    vi.mocked(client.listBookUnmappedFiles).mockRejectedValue(
      new (ApiError as new (s: number, b: string) => Error)(404, 'root folder not found'),
    )
    render(<UnmappedBooksFilesPage />)
    expect(await screen.findByTestId('books-unmapped')).toHaveTextContent(
      'No books root folder is configured.',
    )
    expect(screen.getByTestId('books-unmapped')).toHaveTextContent('Retry')
  })

  it('surfaces an auth-lost (401) state', async () => {
    const { ApiError } = await import('../../api/client')
    vi.mocked(client.listBookUnmappedFiles).mockRejectedValue(
      new (ApiError as new (s: number, b: string) => Error)(401, 'unauthorized'),
    )
    render(<UnmappedBooksFilesPage />)
    expect(await screen.findByTestId('books-unmapped')).toHaveTextContent(
      'Your session expired — please sign in again.',
    )
  })

  it('retries after an error and then lists the files', async () => {
    const { ApiError } = await import('../../api/client')
    vi.mocked(client.listBookUnmappedFiles)
      .mockRejectedValueOnce(
        new (ApiError as new (s: number, b: string) => Error)(404, 'root folder not found'),
      )
      .mockResolvedValueOnce(FILES)
    const user = userEvent.setup()
    render(<UnmappedBooksFilesPage />)
    expect(await screen.findByTestId('books-unmapped')).toHaveTextContent(
      'No books root folder is configured.',
    )
    // The retry button re-issues the walk; the files then render.
    await user.click(screen.getByRole('button', { name: 'Retry' }))
    await waitFor(() => expect(screen.getByTestId('books-unmapped-row-0')).toBeInTheDocument())
  })
})
