import { render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError, client } from '../../api/client'
import type { Movie, MovieWanted } from '../../api/types'
import { MovieDetailPage } from './MovieDetail'

// P7 movie detail: the same pattern as the TV series detail page — back link,
// title header, shared toolbar (Refresh & Scan / Search Releases / Options),
// a badge header with the metadata, and the movie's release state (the
// wanted row: pending → satisfied).
vi.mock('../../api/client', async () => {
  const actual = await vi.importActual<typeof import('../../api/client')>('../../api/client')
  return {
    ...actual,
    client: {
      getMovie: vi.fn(),
      getMovieWanted: vi.fn(),
      setMovieMonitored: vi.fn(),
      runMoviePipeline: vi.fn(),
    },
  }
})

function dune(monitored = true): Movie {
  return {
    ID: 7,
    Title: 'Dune',
    Year: 2021,
    QualityProfile: 'HD',
    Monitored: monitored,
    AddedAt: '2026-01-01T00:00:00Z',
  }
}

function wanted(status: MovieWanted['Status'], releaseTitle = ''): MovieWanted {
  return {
    MovieID: 7,
    Status: status,
    ReleaseTitle: releaseTitle,
    CreatedAt: '2026-01-02T00:00:00Z',
    SatisfiedAt: status === 'satisfied' ? '2026-01-03T00:00:00Z' : null,
  }
}

beforeEach(() => {
  vi.mocked(client.getMovie).mockReset()
  vi.mocked(client.getMovieWanted).mockReset()
  vi.mocked(client.setMovieMonitored).mockReset()
  vi.mocked(client.runMoviePipeline).mockReset()
})

afterEach(() => {
  vi.clearAllMocks()
})

function renderDetail() {
  return render(
    <MemoryRouter initialEntries={['/movies/7']}>
      <Routes>
        <Route path="/movies/:id" element={<MovieDetailPage />} />
        <Route path="/movies" element={<div data-testid="movies-landing">Movies library</div>} />
      </Routes>
    </MemoryRouter>,
  )
}

describe('MovieDetailPage', () => {
  it('shows the pending release state for a monitored movie', async () => {
    vi.mocked(client.getMovie).mockResolvedValue(dune())
    vi.mocked(client.getMovieWanted).mockResolvedValue(wanted('pending'))
    renderDetail()

    expect(await screen.findByTestId('movie-detail')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Dune (2021)' })).toBeInTheDocument()
    expect(screen.getByTestId('badge-monitored')).toHaveTextContent('Monitored')
    expect(screen.getByTestId('badge-quality')).toHaveTextContent('HD')
    expect(screen.getByTestId('badge-year')).toHaveTextContent('2021')
    expect(screen.getByTestId('movie-detail-release')).toBeInTheDocument()
    expect(screen.getByTestId('movie-detail-release-state')).toHaveTextContent('Awaiting release')
  })

  it('shows the satisfied (imported) release state with the release title', async () => {
    vi.mocked(client.getMovie).mockResolvedValue(dune())
    vi.mocked(
      client.getMovieWanted,
    ).mockResolvedValue(wanted('satisfied', 'Dune.2021.1080p.BluRay.x264'))
    renderDetail()

    await screen.findByTestId('movie-detail')
    expect(screen.getByTestId('movie-detail-release-state')).toHaveTextContent('Imported')
    expect(screen.getByTestId('movie-detail-release-title')).toHaveTextContent(
      'Dune.2021.1080p.BluRay.x264',
    )
  })

  it('shows a placeholder when the movie has no wanted row yet (204 → null)', async () => {
    vi.mocked(client.getMovie).mockResolvedValue(dune())
    vi.mocked(client.getMovieWanted).mockResolvedValue(null)
    renderDetail()

    await screen.findByTestId('movie-detail')
    expect(screen.getByTestId('movie-detail-release-state')).toHaveTextContent(
      'No release request yet',
    )
  })

  it('toggles monitoring from the header', async () => {
    vi.mocked(client.getMovie).mockResolvedValue(dune())
    vi.mocked(client.getMovieWanted).mockResolvedValue(null)
    vi.mocked(client.setMovieMonitored).mockResolvedValue({ id: 7, monitored: false })
    const user = userEvent.setup()
    renderDetail()
    await screen.findByTestId('movie-detail')

    await user.click(screen.getByTestId('movie-mon-7'))
    expect(client.setMovieMonitored).toHaveBeenCalledWith(7, false)
    expect(await screen.findByTestId('badge-monitored')).toHaveTextContent('Unmonitored')
  })

  it('searches releases through the pipeline and reports the result', async () => {
    vi.mocked(client.getMovie).mockResolvedValue(dune())
    vi.mocked(client.getMovieWanted).mockResolvedValue(null)
    vi.mocked(client.runMoviePipeline).mockResolvedValue({ imported: 'Dune.2021.1080p' })
    const user = userEvent.setup()
    renderDetail()
    await screen.findByTestId('movie-detail')

    await user.click(screen.getByTestId('movie-detail-search'))
    expect(client.runMoviePipeline).toHaveBeenCalledWith(7)
    expect(await screen.findByTestId('movie-detail-status')).toHaveTextContent('imported')
  })

  it('surfaces a 404 with a sign-in-free error card', async () => {
    vi.mocked(client.getMovie).mockRejectedValue(new ApiError(404, 'movie not found'))
    renderDetail()

    expect(await screen.findByTestId('movie-detail-error')).toBeInTheDocument()
    expect(screen.getByTestId('movie-detail-error-message')).toHaveTextContent(
      'Movie 7 was not found.',
    )
  })

  it('surfaces load failures with a retry', async () => {
    vi.mocked(client.getMovie).mockRejectedValue(new Error('boom'))
    renderDetail()
    expect(await screen.findByTestId('movie-detail-error-message')).toHaveTextContent('boom')
    expect(screen.getByText('Retry')).toBeInTheDocument()
  })

  it('the back link targets the movies list', async () => {
    vi.mocked(client.getMovie).mockResolvedValue(dune())
    vi.mocked(client.getMovieWanted).mockResolvedValue(null)
    renderDetail()
    await screen.findByTestId('movie-detail')
    expect(screen.getByTestId('movie-detail-back')).toHaveAttribute('href', '/movies')
  })
})
