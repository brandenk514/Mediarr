import { render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { client } from '../../api/client'
import type { Movie, PageMovie } from '../../api/types'
import { MoviesPage } from './Movies'

// Radarr R1 movies library: 6-up grid with status ribbons + per-card hover
// controls (Refresh/Edit), status filter chips, stats bar, and the Radarr
// toolbar. The P7 list view (Title/Year/Quality/Monitor) and A–Z jump carry
// over. All data comes from the (faked) API client — no seeded DB.
vi.mock('../../api/client', () => ({
  client: {
    listMovies: vi.fn(),
    setMovieMonitored: vi.fn(),
    getMovie: vi.fn(),
    getMovieStats: vi.fn(),
  },
}))

function page(items: Movie[], total = items.length, pg = 1): PageMovie {
  return { items, total, page: pg, page_size: 25 }
}

function movie(
  id: number,
  title: string,
  monitored = true,
  quality = 'HD',
  status?: Movie['Status'],
): Movie {
  return {
    ID: id,
    Title: title,
    Year: 2020 + (id % 6),
    QualityProfile: quality,
    Monitored: monitored,
    AddedAt: '2026-01-01T00:00:00Z',
    Status: status,
  }
}

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
  vi.mocked(client.getMovie).mockReset()
  vi.mocked(client.getMovieStats).mockReset()
  vi.mocked(client.getMovieStats).mockResolvedValue({
    MovieCount: 2,
    FileCount: 1,
    MonitoredCount: 1,
    UnmonitoredCount: 1,
    TotalSize: 1073741824,
  })
})

afterEach(() => {
  vi.clearAllMocks()
})

describe('MoviesPage', () => {
  it('renders status ribbons derived from the movie status', async () => {
    vi.mocked(client.listMovies).mockResolvedValue(
      page([
        movie(1, 'The Big Lebowski', false, 'Lossless', 'missing:unmonitored'),
        movie(2, 'Arrival', true, 'HD', 'missing:monitored'),
        movie(3, 'Dune', true, 'HD', 'downloaded:monitored'),
      ]),
    )
    renderWithProbe()

    expect(await screen.findByTestId('movies-grid')).toBeInTheDocument()
    // Missing (unmonitored) keeps the legacy "Unmonitored" ribbon; missing
    // (monitored) shows "Missing"; downloaded shows "Downloaded".
    expect(screen.getByTestId('ribbon-1')).toHaveTextContent('Unmonitored')
    expect(screen.getByTestId('ribbon-2')).toHaveTextContent('Missing')
    expect(screen.getByTestId('ribbon-3')).toHaveTextContent('Downloaded')
    // Quality profile in the card footer.
    expect(screen.getByTestId('card-quality-1')).toHaveTextContent('Lossless')
    // The A–Z quick-jump is present.
    expect(screen.getByTestId('movies-az')).toBeInTheDocument()
  })

  it('shows the stats bar from the stats endpoint', async () => {
    vi.mocked(client.listMovies).mockResolvedValue(page([movie(1, 'A')]))
    renderWithProbe()

    const stats = await screen.findByTestId('movies-stats')
    expect(screen.getByTestId('stats-movies')).toHaveTextContent('2')
    expect(screen.getByTestId('stats-files')).toHaveTextContent('1')
    expect(screen.getByTestId('stats-monitored')).toHaveTextContent('1')
    expect(screen.getByTestId('stats-unmonitored')).toHaveTextContent('1')
    expect(screen.getByTestId('stats-size')).toHaveTextContent('1.0 GiB')
    expect(stats).toBeInTheDocument()
  })

  it('filters the grid client-side with the status chips', async () => {
    vi.mocked(client.listMovies).mockResolvedValue(
      page([
        movie(1, 'Alpha', true, 'HD', 'missing:monitored'),
        movie(2, 'Beta', false, 'HD', 'missing:unmonitored'),
        movie(3, 'Gamma', true, 'HD', 'downloaded:monitored'),
      ]),
    )
    const user = userEvent.setup()
    renderWithProbe()
    await screen.findByTestId('movies-grid')

    await user.click(screen.getByTestId('movies-chip-missing-monitored'))
    // Only the matching card remains; the chip is toggled active.
    expect(screen.getByTestId('movie-card-1')).toBeInTheDocument()
    expect(screen.queryByTestId('movie-card-2')).not.toBeInTheDocument()
    expect(screen.queryByTestId('movie-card-3')).not.toBeInTheDocument()
    // The filter is client-side: no extra list request.
    expect(client.listMovies).toHaveBeenCalledTimes(1)

    // Clicking the chip again clears the filter.
    await user.click(screen.getByTestId('movies-chip-missing-monitored'))
    expect(await screen.findByTestId('movie-card-2')).toBeInTheDocument()
  })

  it('shows a filtered empty state when the chip matches nothing', async () => {
    vi.mocked(client.listMovies).mockResolvedValue(page([movie(1, 'A', true, 'HD', 'queued')]))
    const user = userEvent.setup()
    renderWithProbe()
    await screen.findByTestId('movies-grid')

    await user.click(screen.getByTestId('movies-chip-unreleased'))
    expect(await screen.findByText('No movies with this status')).toBeInTheDocument()
    expect(screen.queryByTestId('movies-grid')).not.toBeInTheDocument()
  })

  it('refreshes a single movie through the card hover control', async () => {
    vi.mocked(client.listMovies).mockResolvedValue(
      page([movie(1, 'Inception', true, 'HD', 'missing:monitored')]),
    )
    vi.mocked(client.getMovie).mockResolvedValue(
      movie(1, 'Inception', true, 'HD', 'downloaded:monitored'),
    )
    const user = userEvent.setup()
    renderWithProbe()
    await screen.findByTestId('card-refresh-1')

    await user.click(screen.getByTestId('card-refresh-1'))
    expect(client.getMovie).toHaveBeenCalledWith(1)
    // The ribbon reflects the refreshed status.
    expect(await screen.findByTestId('ribbon-1')).toHaveTextContent('Downloaded')
  })

  it('navigates to the movie detail page when a card is clicked', async () => {
    vi.mocked(client.listMovies).mockResolvedValue(page([movie(3, 'Dune')]))
    const user = userEvent.setup()
    renderWithProbe()

    await screen.findByTestId('movie-card-3')
    // The card itself is a button inside the cell.
    const card = (screen.getByTestId('movie-card-3') as HTMLElement).querySelector('button')!
    await user.click(card)
    expect(await screen.findByTestId('probe-location')).toHaveTextContent('/movies/3')
  })

  it('navigates to the detail page from the card edit control', async () => {
    vi.mocked(client.listMovies).mockResolvedValue(page([movie(5, 'Tenet')]))
    const user = userEvent.setup()
    renderWithProbe()

    await screen.findByTestId('card-edit-5')
    await user.click(screen.getByTestId('card-edit-5'))
    expect(await screen.findByTestId('probe-location')).toHaveTextContent('/movies/5')
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
    const titles = [...grid.querySelectorAll('.poster-cell')].map((c) => c.getAttribute('title'))
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

  it('disables the no-backend toolbar buttons with later-phase titles', async () => {
    vi.mocked(client.listMovies).mockResolvedValue(page([movie(1, 'A')]))
    renderWithProbe()
    await screen.findByTestId('movies-grid')

    for (const tid of [
      'movies-rss-sync',
      'movies-search-all',
      'movies-manual-import',
      'movies-bulk-edit',
      'movies-options',
    ]) {
      const btn = screen.getByTestId(tid)
      expect(btn).toBeDisabled()
      expect(btn).toHaveAttribute('title', expect.stringMatching(/later phase/))
    }
  })
})
