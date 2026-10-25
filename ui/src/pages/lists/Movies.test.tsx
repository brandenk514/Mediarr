import { render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { client } from '../../api/client'
import type { Movie, PageMovie } from '../../api/types'
import { MoviesPage } from './Movies'

// P7 movies list: the shared Sonarr-style pattern — a 6-column poster grid
// by default (card → the movie detail page), the shared toolbar (Update All /
// RSS Sync / Select / Options + View / Sort / Filter) and an A–Z quick-jump.
// The P1 monitoring table is kept behind the list view.
vi.mock('../../api/client', () => ({
  client: {
    listMovies: vi.fn(),
    setMovieMonitored: vi.fn(),
  },
}))

function page(items: Movie[], total = items.length, pg = 1): PageMovie {
  return { items, total, page: pg, page_size: 25 }
}

function movie(id: number, title: string, monitored = true, quality = 'HD'): Movie {
  return {
    ID: id,
    Title: title,
    Year: 2020 + (id % 6),
    QualityProfile: quality,
    Monitored: monitored,
    AddedAt: '2026-01-01T00:00:00Z',
  }
}

// Renders the movies page with a probe route so a card click can be observed.
function renderWithProbe() {
  function Probe() {
    const location = useLocation()
    return <div data-testid="probe-location">{location.pathname}</div>
  }
  return render(
    <MemoryRouter initialEntries={['/movies']}>
      <Routes>
        <Route
          path="/movies"
          element={
            <>
              <MoviesPage />
              <Probe />
            </>
          }
        />
        <Route path="/movies/:id" element={<Probe />} />
      </Routes>
    </MemoryRouter>,
  )
}

beforeEach(() => {
  vi.mocked(client.listMovies).mockReset()
  vi.mocked(client.setMovieMonitored).mockReset()
})

afterEach(() => {
  vi.clearAllMocks()
})

describe('MoviesPage', () => {
  it('renders the poster grid with ribbons, quality footers and the A–Z jump', async () => {
    vi.mocked(client.listMovies).mockResolvedValue(
      page([movie(1, 'The Big Lebowski', false, 'Lossless'), movie(2, 'Arrival')]),
    )
    renderWithProbe()

    expect(await screen.findByTestId('movies-grid')).toBeInTheDocument()
    expect(screen.getByTestId('movie-card-1')).toBeInTheDocument()
    expect(screen.getByTestId('movie-card-2')).toBeInTheDocument()
    // The unmonitored movie carries the red status ribbon; the monitored one doesn't.
    expect(screen.getByTestId('ribbon-1')).toBeInTheDocument()
    expect(screen.queryByTestId('ribbon-2')).not.toBeInTheDocument()
    // Quality profile in the card footer.
    expect(screen.getByTestId('card-quality-1')).toHaveTextContent('Lossless')
    // The A–Z quick-jump is present.
    expect(screen.getByTestId('movies-az')).toBeInTheDocument()
  })

  it('navigates to the movie detail page when a card is clicked', async () => {
    vi.mocked(client.listMovies).mockResolvedValue(page([movie(3, 'Dune')]))
    const user = userEvent.setup()
    renderWithProbe()

    await screen.findByTestId('movie-card-3')
    await user.click(screen.getByTestId('movie-card-3'))
    expect(await screen.findByTestId('probe-location')).toHaveTextContent('/movies/3')
  })

  it('switches to the list view for bulk monitoring toggles', async () => {
    vi.mocked(client.listMovies).mockResolvedValue(page([movie(4, 'Parasite', false)]))
    vi.mocked(client.setMovieMonitored).mockResolvedValue({ id: 4, monitored: true })
    const user = userEvent.setup()
    renderWithProbe()
    await screen.findByTestId('movie-card-4')

    await user.click(screen.getByTestId('movies-view-list'))
    expect(await screen.findByTestId('movie-4')).toBeInTheDocument()
    await user.click(screen.getByTestId('movie-mon-4'))
    expect(client.setMovieMonitored).toHaveBeenCalledWith(4, true)
    // The toggle reloads the list.
    expect(client.listMovies).toHaveBeenCalledTimes(2)
  })

  it('sorts the grid client-side by title descending', async () => {
    vi.mocked(client.listMovies).mockResolvedValue(page([movie(1, 'Zulu'), movie(2, 'Alpha')]))
    const user = userEvent.setup()
    renderWithProbe()
    await screen.findByTestId('movies-grid')

    await user.click(screen.getByTestId('movies-sort-btn'))
    await user.click(screen.getByTestId('movies-sort-opt-title-desc'))
    const grid = await screen.findByTestId('movies-grid')
    const titles = [...grid.querySelectorAll('.poster-card')].map((c) => c.getAttribute('title'))
    expect(titles).toEqual(['Zulu (2021)', 'Alpha (2022)'])
  })

  it('filters by monitored through the Filter menu', async () => {
    vi.mocked(client.listMovies).mockResolvedValue(page([movie(1, 'A', true), movie(2, 'B', false)]))
    const user = userEvent.setup()
    renderWithProbe()
    await screen.findByTestId('movies-grid')

    await user.click(screen.getByTestId('movies-filter-btn'))
    await user.click(screen.getByTestId('movies-filter-opt-unmonitored'))
    // 'unmonitored' maps to a server-side monitored=false query.
    await vi.waitFor(() =>
      expect(client.listMovies).toHaveBeenLastCalledWith(
        expect.objectContaining({ monitored: false, page: 1 }),
      ),
    )
  })

  it('shows the empty state with an add shortcut when there are no movies', async () => {
    vi.mocked(client.listMovies).mockResolvedValue(page([]))
    renderWithProbe()
    expect(await screen.findByText('No movies')).toBeInTheDocument()
    expect(screen.getByTestId('movies-empty-add')).toHaveAttribute('href', '/movies/add')
  })

  it('surfaces load errors with a retry', async () => {
    vi.mocked(client.listMovies).mockRejectedValue(new Error('boom'))
    const user = userEvent.setup()
    renderWithProbe()
    expect(await screen.findByTestId('movies-error')).toBeInTheDocument()
    expect(screen.getByTestId('movies-error-message')).toHaveTextContent('boom')

    vi.mocked(client.listMovies).mockResolvedValue(page([movie(3, 'Blade Runner')]))
    await user.click(screen.getByText('Retry'))
    expect(await screen.findByTestId('movie-card-3')).toBeInTheDocument()
  })

  it('Update All reports a status and refetches', async () => {
    vi.mocked(client.listMovies).mockResolvedValue(page([movie(1, 'A')]))
    const user = userEvent.setup()
    renderWithProbe()
    await screen.findByTestId('movie-card-1')
    await user.click(screen.getByTestId('movies-update-all'))
    expect(await screen.findByTestId('movies-status')).toHaveTextContent('Updated 1 movies just now')
    expect(client.listMovies).toHaveBeenCalledTimes(2)
  })
})
