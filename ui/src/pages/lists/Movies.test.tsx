import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { client } from '../../api/client'
import type { Movie, Page } from '../../api/types'
import { MoviesPage } from './Movies'

// Page test for the movies list (#41). The client is mocked so we exercise the
// envelope rendering, the optimistic monitored toggle, and the debounced
// search without a network.
vi.mock('../../api/client', () => ({
  client: {
    listMovies: vi.fn(),
    setMovieMonitored: vi.fn(),
  },
}))

function page(items: Movie[], total = items.length, pg = 1): Page<Movie> {
  return { items, total, page: pg, page_size: 25 }
}

function movie(id: number, title: string, monitored = true): Movie {
  return { ID: id, Title: title, Year: 2020, QualityProfile: 'HD', Monitored: monitored, AddedAt: '' }
}

beforeEach(() => {
  vi.mocked(client.listMovies).mockReset()
  vi.mocked(client.setMovieMonitored).mockReset()
})

afterEach(() => {
  vi.clearAllMocks()
})

describe('MoviesPage', () => {
  it('renders the paged envelope as rows and toggles monitoring optimistically', async () => {
    vi.mocked(client.listMovies).mockResolvedValue(page([movie(1, 'Dune'), movie(2, 'Arrival', false)]))
    vi.mocked(client.setMovieMonitored).mockResolvedValue({ id: 1, monitored: false })
    const user = userEvent.setup()
    render(
      <MemoryRouter>
        <MoviesPage />
      </MemoryRouter>,
    )

    // loading state, then rows from the envelope
    expect(await screen.findByTestId('movie-1')).toHaveTextContent('Dune')
    expect(screen.getByTestId('movie-2')).toHaveTextContent('Arrival')
    // the two monitored toggles reflect the item flags
    expect(screen.getByTestId('movie-mon-1')).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByTestId('movie-mon-2')).toHaveAttribute('aria-pressed', 'false')

    // optimistically un-monitor Dune: API is called with the new value, then
    // the row reloads (a second listMovies call via reload()).
    await user.click(screen.getByTestId('movie-mon-1'))
    expect(client.setMovieMonitored).toHaveBeenCalledWith(1, false)
    await waitFor(() => expect(client.listMovies).toHaveBeenCalledTimes(2))
  })

  it('shows the empty state when the envelope has no items', async () => {
    vi.mocked(client.listMovies).mockResolvedValue(page([]))
    render(
      <MemoryRouter>
        <MoviesPage />
      </MemoryRouter>,
    )
    expect(await screen.findByTestId('movies-empty')).toBeInTheDocument()
  })

  it('surfaces a load error with retry', async () => {
    vi.mocked(client.listMovies).mockRejectedValue(new Error('boom'))
    const user = userEvent.setup()
    render(
      <MemoryRouter>
        <MoviesPage />
      </MemoryRouter>,
    )
    await waitFor(() => expect(screen.getByTestId('movies-error')).toBeInTheDocument())
    expect(screen.getByTestId('movies-error-message')).toHaveTextContent('boom')

    // retry re-issues the list call
    vi.mocked(client.listMovies).mockResolvedValue(page([movie(3, 'Blade Runner')]))
    await user.click(screen.getByText('Retry'))
    expect(await screen.findByTestId('movie-3')).toHaveTextContent('Blade Runner')
  })

  it('debounces the search box and re-queries with the trimmed term', async () => {
    vi.mocked(client.listMovies).mockResolvedValue(page([]))
    const user = userEvent.setup()
    render(
      <MemoryRouter>
        <MoviesPage />
      </MemoryRouter>,
    )

    // initial load
    await waitFor(() => expect(client.listMovies).toHaveBeenCalledTimes(1))

    await user.type(screen.getByTestId('list-search'), 'dune ')

    // Let the 300ms debounce elapse, then the single re-query fires.
    await new Promise((r) => setTimeout(r, 400))
    await waitFor(() => expect(client.listMovies).toHaveBeenCalledTimes(2))
    expect(vi.mocked(client.listMovies).mock.calls.at(-1)).toEqual([
      expect.objectContaining({ q: 'dune', page: 1 }),
    ])
  })

  it('filters by monitored using the segmented control', async () => {
    vi.mocked(client.listMovies).mockResolvedValue(page([]))
    const user = userEvent.setup()
    render(
      <MemoryRouter>
        <MoviesPage />
      </MemoryRouter>,
    )
    await waitFor(() => expect(client.listMovies).toHaveBeenCalledTimes(1))

    // The monitored filter change re-queries immediately (only the free-text
    // search box is debounced), so no timer manipulation is needed.
    await user.click(screen.getByTestId('filter-monitored'))
    await waitFor(() => expect(client.listMovies).toHaveBeenCalledTimes(2))
    expect(vi.mocked(client.listMovies).mock.calls.at(-1)).toEqual([
      expect.objectContaining({ monitored: true, page: 1 }),
    ])
  })
})
