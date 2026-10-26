import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { client } from '../../api/client'
import { MovieAddPage } from './MovieAdd'

// Page test for the Radarr R2 two-pane Add Movie page: debounced TMDB search
// with enriched result rows (poster, badges, overview, links) on the left;
// the add form (root folder, edition-aware monitor, minimum availability,
// quality profile, tags, start-search) on the right. Manual title entry
// stays the keyless path: search greys out on 503 and the form still works.
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
      searchMovies: vi.fn(),
      addMovie: vi.fn(),
      getProfiles: vi.fn(),
      listRootFolders: vi.fn(),
    },
  }
})

function renderPage(initial = '/movies/add') {
  return render(
    <MemoryRouter initialEntries={[initial]}>
      <Routes>
        <Route path="/movies/add" element={<MovieAddPage />} />
        <Route path="/movies" element={<div data-testid="movies-landing">Movie library</div>} />
      </Routes>
    </MemoryRouter>,
  )
}

const hit = {
  id: '603',
  title: 'Dune: Part Two',
  year: 2024,
  poster: '',
  overview: 'Fremen.',
  certification: 'PG-13',
  runtime: 166,
  original_language: 'en',
  studio: 'Legendary',
  genres: ['Sci-Fi'],
  tmdb_pct: 87,
  imdb: 'https://www.imdb.com/title/tt15239678/',
  links: { tmdb: 'https://www.themoviedb.org/movie/603' },
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(client.searchMovies).mockResolvedValue([hit])
  vi.mocked(client.getProfiles).mockResolvedValue({ profiles: ['HD-1080p'], default: 'HD-1080p' })
  vi.mocked(client.addMovie).mockResolvedValue({ id: 71 })
  vi.mocked(client.listRootFolders).mockResolvedValue([
    { ID: 3, Path: '/media/movies', Name: 'Movies', CreatedAt: '', FreeSpace: 500 * 1024 ** 3 },
  ])
})

afterEach(() => {
  vi.clearAllMocks()
})

describe('MovieAddPage (R2 two-pane)', () => {
  it('renders selectable result rows with badges and overview', async () => {
    const user = userEvent.setup()
    renderPage()
    await user.type(screen.getByTestId('movie-search-input'), 'dune')
    const row = await screen.findByTestId('movie-result-603')
    expect(row).toHaveTextContent('Dune: Part Two (2024)')
    expect(row).toHaveTextContent('PG-13')
    expect(row).toHaveTextContent('166 min')
    expect(row).toHaveTextContent('★ 87%')
    expect(row).toHaveTextContent('IMDb')
    expect(row).toHaveTextContent('EN')
    expect(row).toHaveTextContent('Legendary')
    expect(row).toHaveTextContent('Sci-Fi')
    expect(row).toHaveTextContent('TMDB')
    expect(row).toHaveTextContent('Fremen.')
  })

  it('selecting a hit fills the right pane and submits the full R2 payload', async () => {
    const user = userEvent.setup()
    renderPage()
    await user.type(screen.getByTestId('movie-search-input'), 'dune')
    await user.click(await screen.findByTestId('movie-result-603'))

    // Right pane: pre-filled title/year, the TMDB 'More Info' link, and the
    // subfolder helper line.
    expect(screen.getByTestId('movie-title-input')).toHaveValue('Dune: Part Two')
    expect(screen.getByTestId('movie-year-input')).toHaveValue('2024')
    expect(screen.getByTestId('movie-more-info')).toHaveAttribute(
      'href',
      'https://www.themoviedb.org/movie/603',
    )
    expect(screen.getByTestId('movie-root-folder-help')).toHaveTextContent(
      "'Dune: Part Two (2024)' subfolder will be created automatically",
    )

    // Exercise every R2 field before adding.
    await user.selectOptions(screen.getByTestId('movie-root-folder'), '/media/movies')
    await user.selectOptions(screen.getByTestId('movie-monitor'), 'all_except_3d')
    await user.selectOptions(screen.getByTestId('movie-min-availability'), 'announced')
    await user.type(screen.getByTestId('movie-tags-input'), '4k{Enter}')
    await user.click(screen.getByTestId('movie-start-search'))

    await user.click(screen.getByTestId('movie-add-button'))
    await waitFor(() =>
      expect(client.addMovie).toHaveBeenCalledWith({
        title: 'Dune: Part Two',
        year: '2024',
        quality_profile: 'HD-1080p',
        monitor: 'all_except_3d',
        minimum_availability: 'announced',
        tags: ['4k'],
        root_folder: '/media/movies',
        start_search: true,
      }),
    )
    expect(await screen.findByTestId('movies-landing')).toBeInTheDocument()
  })

  it('sends defaults (all / released / no search) when only a hit is selected', async () => {
    const user = userEvent.setup()
    renderPage()
    await user.type(screen.getByTestId('movie-search-input'), 'dune')
    await user.click(await screen.findByTestId('movie-result-603'))
    await user.click(screen.getByTestId('movie-add-button'))
    await waitFor(() =>
      expect(client.addMovie).toHaveBeenCalledWith({
        title: 'Dune: Part Two',
        year: '2024',
        quality_profile: 'HD-1080p',
        monitor: 'all',
        minimum_availability: 'released',
        start_search: false,
      }),
    )
  })

  it('surfaces the 409 duplicate as an AddError', async () => {
    const { ApiError } = await import('../../api/client')
    vi.mocked(client.addMovie).mockRejectedValue(
      new (ApiError as new (s: number, b: string) => Error)(409, 'movie already exists'),
    )
    const user = userEvent.setup()
    renderPage()
    await user.type(screen.getByTestId('movie-search-input'), 'dune')
    await user.click(await screen.findByTestId('movie-result-603'))
    await user.click(screen.getByTestId('movie-add-button'))
    expect(await screen.findByTestId('movie-add-error')).toHaveTextContent('movie already exists')
  })

  it('greys out the search on keyless 503 and still adds via manual title entry', async () => {
    // ApiError is exported from the mocked client module.
    const { ApiError } = await import('../../api/client')
    vi.mocked(client.searchMovies).mockRejectedValue(
      new (ApiError as new (s: number, b: string) => Error)(503, 'tmdb is not configured'),
    )
    const user = userEvent.setup()
    renderPage()
    await user.type(screen.getByTestId('movie-search-input'), 'dune')
    expect(await screen.findByTestId('movie-search-unavailable')).toHaveTextContent(
      'tmdb is not configured',
    )
    expect(screen.getByTestId('movie-search-input')).toBeDisabled()

    // Manual entry: title + year, then add — the e2e relies on this path.
    await user.type(screen.getByTestId('movie-title-input'), 'Dune: Part Two')
    await user.type(screen.getByTestId('movie-year-input'), '2024')
    const btn = screen.getByTestId('movie-add-button')
    expect(btn).toBeEnabled()
    await user.click(btn)
    await waitFor(() =>
      expect(client.addMovie).toHaveBeenCalledWith(
        expect.objectContaining({ title: 'Dune: Part Two', year: '2024' }),
      ),
    )
    expect(await screen.findByTestId('movies-landing')).toBeInTheDocument()
  })

  it('disables the add button until a title is present', () => {
    renderPage()
    expect(screen.getByTestId('movie-add-button')).toBeDisabled()
  })
})

describe('MovieAddPage ?tmdb= preselect (R3 Discover link)', () => {
  it('types tmdb:<id> into the search form and pre-selects the hit into the pane', async () => {
    renderPage('/movies/add?tmdb=603')

    // The search form carries the tmdb:<id> lookup (the R2 search mechanism).
    expect(await screen.findByTestId('movie-search-input')).toHaveValue('tmdb:603')
    // The first hit lands in the right pane: title/year filled, selected
    // banner + More Info link, and the result row marked selected.
    expect(await screen.findByTestId('movie-selected')).toHaveTextContent('Dune: Part Two (2024)')
    expect(screen.getByTestId('movie-title-input')).toHaveValue('Dune: Part Two')
    expect(screen.getByTestId('movie-year-input')).toHaveValue('2024')
    expect(screen.getByTestId('movie-more-info')).toHaveAttribute(
      'href',
      'https://www.themoviedb.org/movie/603',
    )
    expect(screen.getByTestId('movie-result-603')).toHaveAttribute('aria-pressed', 'true')
  })

  it('no-ops the preselect when the search 503s (keyless) — manual entry still works', async () => {
    // ApiError is exported from the mocked client module.
    const { ApiError } = await import('../../api/client')
    vi.mocked(client.searchMovies).mockRejectedValue(
      new (ApiError as new (s: number, b: string) => Error)(503, 'tmdb is not configured'),
    )
    renderPage('/movies/add?tmdb=603')

    // The search box greys out with the reason (the R2 unavailable pattern)…
    expect(await screen.findByTestId('movie-search-unavailable')).toHaveTextContent(
      'tmdb is not configured',
    )
    // …the pane stays un-preselected, and the title field is still usable.
    expect(screen.queryByTestId('movie-selected')).not.toBeInTheDocument()
    expect(screen.getByTestId('movie-title-input')).toHaveValue('')
    expect(screen.getByTestId('movie-add-button')).toBeDisabled()
  })

  it('ignores malformed ?tmdb= params (no lookup, no preselect)', async () => {
    renderPage('/movies/add?tmdb=not-a-number')
    expect(await screen.findByTestId('movie-search-input')).toHaveValue('')
    expect(screen.queryByTestId('movie-selected')).not.toBeInTheDocument()
  })
})
