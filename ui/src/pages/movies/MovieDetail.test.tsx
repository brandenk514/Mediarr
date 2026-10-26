import { render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError, client } from '../../api/client'
import type { Movie, PageMovie } from '../../api/types'
import { MovieDetailPage } from './MovieDetail'

// Radarr R1 movie detail: toolbar, header card (poster, badges, score
// badges, field grid, overview), conditional Editions section, Files table,
// Cast/Crew/Titles. All data comes from the (faked) API client.
vi.mock('../../api/client', async () => {
  const actual = await vi.importActual<typeof import('../../api/client')>('../../api/client')
  return {
    ...actual,
    client: {
      getMovie: vi.fn(),
      listMovies: vi.fn(),
      setMovieMonitored: vi.fn(),
      setMovieEditionMonitored: vi.fn(),
      runMoviePipeline: vi.fn(),
    },
  }
})

function baseMovie(over: Partial<Movie> = {}): Movie {
  return {
    ID: 7,
    Title: 'Dune',
    Year: 2021,
    QualityProfile: 'HD',
    Monitored: true,
    AddedAt: '2026-01-01T00:00:00Z',
    ...over,
  }
}

function libPage(movies: Movie[]): PageMovie {
  return { items: movies, total: movies.length, page: 1, page_size: 25 }
}

beforeEach(() => {
  vi.mocked(client.getMovie).mockReset()
  vi.mocked(client.listMovies).mockReset()
  vi.mocked(client.setMovieMonitored).mockReset()
  vi.mocked(client.setMovieEditionMonitored).mockReset()
  vi.mocked(client.runMoviePipeline).mockReset()
  vi.mocked(client.listMovies).mockResolvedValue(
    libPage([baseMovie({ ID: 6 }), baseMovie(), baseMovie({ ID: 8 })]),
  )
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
  it('renders the header card with em-dash placeholders for missing enrichment', async () => {
    vi.mocked(client.getMovie).mockResolvedValue(baseMovie())
    renderDetail()

    expect(await screen.findByTestId('movie-detail')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Dune (2021)' })).toBeInTheDocument()
    expect(screen.getByTestId('badge-monitored')).toHaveTextContent('Monitored')
    expect(screen.getByTestId('badge-year')).toHaveTextContent('2021')
    // No enrichment yet: em-dashes, never an error.
    expect(screen.getByTestId('field-path')).toHaveTextContent('—')
    expect(screen.getByTestId('field-size')).toHaveTextContent('—')
    expect(screen.getByTestId('field-studio')).toHaveTextContent('—')
    expect(screen.getByTestId('field-genres')).toHaveTextContent('—')
    expect(screen.getByTestId('movie-detail-overview')).toHaveTextContent('—')
    // No scores section when there are no scores.
    expect(screen.queryByTestId('movie-detail-scores')).not.toBeInTheDocument()
  })

  it('renders TMDB enrichment: badges, score badges and the field grid', async () => {
    vi.mocked(client.getMovie).mockResolvedValue(
      baseMovie({
        Certification: 'PG-13',
        Runtime: 157,
        Overview: 'Fremens rise in the desert.',
        Scores: { TmdbPct: 84.4, Imdb: null, RottenTomatoes: null, Metacritic: null },
        Genres: ['Sci-Fi', 'Drama'],
        Studio: 'Legendary',
        CollectionName: 'Dune Saga',
        OriginalLanguage: 'en',
        Path: 'Dune (2021)/Dune.mkv',
        Status: 'downloaded:monitored',
        Size: 1073741824,
      }),
    )
    renderDetail()
    await screen.findByTestId('movie-detail')

    expect(screen.getByTestId('badge-certification')).toHaveTextContent('PG-13')
    expect(screen.getByTestId('badge-runtime')).toHaveTextContent('157 min')
    // Only the non-null score renders.
    const scores = screen.getByTestId('movie-detail-scores')
    expect(scores).toHaveTextContent('TMDB 84%')
    expect(scores).not.toHaveTextContent('IMDb')

    expect(screen.getByTestId('field-path')).toHaveTextContent('Dune (2021)/Dune.mkv')
    expect(screen.getByTestId('field-status')).toHaveTextContent('downloaded:monitored')
    expect(screen.getByTestId('field-size')).toHaveTextContent('1.0 GiB')
    expect(screen.getByTestId('field-collection')).toHaveTextContent('Dune Saga')
    expect(screen.getByTestId('field-language')).toHaveTextContent('en')
    expect(screen.getByTestId('field-studio')).toHaveTextContent('Legendary')
    expect(screen.getByTestId('field-genres')).toHaveTextContent('Sci-Fi, Drama')
    expect(screen.getByTestId('movie-detail-overview')).toHaveTextContent(
      'Fremens rise in the desert.',
    )
  })

  it('shows the files table with media-info columns', async () => {
    vi.mocked(client.getMovie).mockResolvedValue(
      baseMovie({
        Files: [
          {
            RelativePath: 'Dune (2021)/Dune.mkv',
            VideoCodec: 'h265',
            AudioInfo: 'EAC3, en, 6ch',
            Size: 1073741824,
            Languages: ['en'],
            Quality: '2160p',
            ReleaseGroup: 'NTb',
            Formats: ['WEB-DL'],
          },
        ],
      }),
    )
    renderDetail()
    await screen.findByTestId('movie-detail')

    const table = screen.getByTestId('movie-detail-files-table')
    expect(table).toBeInTheDocument()
    expect(screen.getByTestId('file-row-0')).toHaveTextContent('Dune (2021)/Dune.mkv')
    expect(table).toHaveTextContent('h265')
    expect(table).toHaveTextContent('EAC3, en, 6ch')
    expect(table).toHaveTextContent('1.0 GiB')
    expect(table).toHaveTextContent('NTb')
  })

  it('shows the files empty state when the movie has no files', async () => {
    vi.mocked(client.getMovie).mockResolvedValue(baseMovie())
    renderDetail()
    await screen.findByTestId('movie-detail')
    expect(screen.getByTestId('movie-detail-files-empty')).toHaveTextContent('No files yet.')
    expect(screen.queryByTestId('movie-detail-files-table')).not.toBeInTheDocument()
  })

  it('renders the editions section only when the movie has more than one edition', async () => {
    vi.mocked(client.getMovie).mockResolvedValue(
      baseMovie({
        Editions: [
          { ID: 1, Label: 'Theatrical', Monitored: true, Quality: 'HD', Size: 0 },
          { ID: 2, Label: 'Extended', Monitored: false, Quality: 'UHD', Size: 536870912 },
        ],
      }),
    )
    renderDetail()
    await screen.findByTestId('movie-detail')
    const editions = screen.getByTestId('movie-detail-editions')
    expect(editions).toHaveTextContent('Theatrical')
    expect(editions).toHaveTextContent('Extended')
    expect(editions).toHaveTextContent('512 MiB')
  })

  it('hides the editions section for a single (seeded) edition', async () => {
    vi.mocked(client.getMovie).mockResolvedValue(
      baseMovie({ Editions: [{ ID: 1, Label: 'Theatrical', Monitored: true, Quality: 'HD', Size: 0 }] }),
    )
    renderDetail()
    await screen.findByTestId('movie-detail')
    expect(screen.queryByTestId('movie-detail-editions')).not.toBeInTheDocument()
  })

  it('toggles a single edition through the per-edition endpoint', async () => {
    vi.mocked(client.getMovie).mockResolvedValue(
      baseMovie({
        Editions: [
          { ID: 1, Label: 'Theatrical', Monitored: true, Quality: 'HD', Size: 0 },
          { ID: 2, Label: 'Extended', Monitored: false, Quality: 'UHD', Size: 0 },
        ],
      }),
    )
    vi.mocked(client.setMovieEditionMonitored).mockResolvedValue({ id: 2, monitored: true })
    const user = userEvent.setup()
    renderDetail()
    await screen.findByTestId('edition-mon-2')

    await user.click(screen.getByTestId('edition-mon-2'))
    expect(client.setMovieEditionMonitored).toHaveBeenCalledWith(7, 2, true)
    await vi.waitFor(() =>
      expect(screen.getByTestId('edition-mon-2')).toHaveTextContent('monitored'),
    )
  })

  it('renders cast, crew and alternative titles when present', async () => {
    vi.mocked(client.getMovie).mockResolvedValue(
      baseMovie({
        Cast: [{ Name: 'Timothée Chalamet', Role: 'Paul Atreides' }],
        Crew: [{ Name: 'Denis Villeneuve', Job: 'Director' }],
        AlternativeTitles: [{ Title: 'Dune: Part One', Type: 'Tmdb' }],
      }),
    )
    renderDetail()
    await screen.findByTestId('movie-detail')

    expect(screen.getByTestId('movie-detail-cast')).toHaveTextContent('Timothée Chalamet → Paul Atreides')
    expect(screen.getByTestId('movie-detail-crew')).toHaveTextContent('Denis Villeneuve → Director')
    expect(screen.getByTestId('movie-detail-titles')).toHaveTextContent('Dune: Part One → Tmdb')
  })

  it('hides empty cast/crew/titles sections', async () => {
    vi.mocked(client.getMovie).mockResolvedValue(baseMovie())
    renderDetail()
    await screen.findByTestId('movie-detail')
    expect(screen.queryByTestId('movie-detail-cast')).not.toBeInTheDocument()
    expect(screen.queryByTestId('movie-detail-crew')).not.toBeInTheDocument()
    expect(screen.queryByTestId('movie-detail-titles')).not.toBeInTheDocument()
  })

  it('shows prev/next navigation by library order', async () => {
    vi.mocked(client.getMovie).mockResolvedValue(baseMovie())
    renderDetail()
    await screen.findByTestId('movie-detail')

    expect(screen.getByTestId('movie-detail-prev')).toBeInTheDocument()
    expect(screen.getByTestId('movie-detail-next')).toBeInTheDocument()
  })

  it('hides the prev arrow for the first movie in library order', async () => {
    vi.mocked(client.getMovie).mockResolvedValue(baseMovie({ ID: 6 }))
    vi.mocked(client.listMovies).mockResolvedValue(libPage([baseMovie({ ID: 6 }), baseMovie()]))
    renderDetail()
    await screen.findByTestId('movie-detail')
    expect(screen.queryByTestId('movie-detail-prev')).not.toBeInTheDocument()
    expect(screen.getByTestId('movie-detail-next')).toBeInTheDocument()
  })

  it('toggles monitoring from the header', async () => {
    vi.mocked(client.getMovie).mockResolvedValue(baseMovie())
    vi.mocked(client.setMovieMonitored).mockResolvedValue({ id: 7, monitored: false })
    const user = userEvent.setup()
    renderDetail()
    await screen.findByTestId('movie-detail')

    await user.click(screen.getByTestId('movie-mon-7'))
    expect(client.setMovieMonitored).toHaveBeenCalledWith(7, false)
    expect(await screen.findByTestId('badge-monitored')).toHaveTextContent('Unmonitored')
  })

  it('searches releases through the pipeline and reports the result', async () => {
    vi.mocked(client.getMovie).mockResolvedValue(baseMovie())
    vi.mocked(client.runMoviePipeline).mockResolvedValue({ imported: 'Dune.2021.1080p' })
    const user = userEvent.setup()
    renderDetail()
    await screen.findByTestId('movie-detail')

    await user.click(screen.getByTestId('movie-detail-search'))
    expect(client.runMoviePipeline).toHaveBeenCalledWith(7)
    expect(await screen.findByTestId('movie-detail-status')).toHaveTextContent('imported')
  })

  it('disables the no-backend toolbar buttons with later-phase titles', async () => {
    vi.mocked(client.getMovie).mockResolvedValue(baseMovie())
    renderDetail()
    await screen.findByTestId('movie-detail')

    for (const tid of [
      'movie-detail-interactive',
      'movie-detail-rename',
      'movie-detail-manage-files',
      'movie-detail-history',
      'movie-detail-edit',
      'movie-detail-delete',
    ]) {
      const btn = screen.getByTestId(tid)
      expect(btn).toBeDisabled()
      expect(btn).toHaveAttribute('title', expect.stringMatching(/later phase/))
    }
    // Refresh & Scan and Search Movie are live.
    expect(screen.getByTestId('movie-detail-refresh')).toBeEnabled()
    expect(screen.getByTestId('movie-detail-search')).toBeEnabled()
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
    vi.mocked(client.getMovie).mockResolvedValue(baseMovie())
    renderDetail()
    await screen.findByTestId('movie-detail')
    expect(screen.getByTestId('movie-detail-back')).toHaveAttribute('href', '/movies')
  })
})
