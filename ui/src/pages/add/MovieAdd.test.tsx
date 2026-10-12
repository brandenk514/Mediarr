import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError, client } from '../../api/client'
import { MovieAddPage } from './MovieAdd'

// E2E-style page test for the movie add flow (#42): debounced provider
// search → result cards → profile picker → add → navigate to the library.
// The client is mocked; the router is real, so navigation is asserted for
// real.
vi.mock('../../api/client', () => ({
  ApiError: class ApiError extends Error {
    status: number
    constructor(status: number, body: string) {
      super(body)
      this.status = status
    }
  },
  client: {
    searchMovies: vi.fn(),
    getProfiles: vi.fn(),
    addMovie: vi.fn(),
  },
}))

function renderPage() {
  return render(
    <MemoryRouter initialEntries={['/movies/add']}>
      <Routes>
        <Route path="/movies/add" element={<MovieAddPage />} />
        <Route path="/movies" element={<div data-testid="movies-landing">Movies library</div>} />
      </Routes>
    </MemoryRouter>,
  )
}

const duneHit = {
  id: '603',
  title: 'Dune: Part Two',
  year: 2024,
  extra: { overview: 'Fremen.', rating: '7.8' },
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(client.searchMovies).mockResolvedValue([duneHit])
  vi.mocked(client.getProfiles).mockResolvedValue({
    profiles: ['SD', 'HD-1080p', 'UHD-2160p'],
    default: 'HD-1080p',
  })
  vi.mocked(client.addMovie).mockResolvedValue({ id: 11 })
})

afterEach(() => {
  vi.clearAllMocks()
})

describe('MovieAddPage', () => {
  it('runs the full flow: search → select → default profile → add → navigate', async () => {
    const user = userEvent.setup()
    renderPage()

    // Debounced provider search fires for the typed term.
    await user.type(screen.getByTestId('movie-search-input'), 'dune')
    expect(await screen.findByTestId('result-603')).toHaveTextContent('Dune: Part Two')

    // Selecting the hit prefills title/year and marks it selected.
    await user.click(screen.getByTestId('result-603'))
    expect(screen.getByTestId('movie-title-input')).toHaveValue('Dune: Part Two')
    expect(screen.getByTestId('movie-year-input')).toHaveValue('2024')
    expect(screen.getByTestId('movie-selected')).toHaveTextContent('Dune: Part Two')

    // The profile picker prefills the kind default (HD-1080p).
    const profile = await screen.findByTestId('movie-profile')
    expect(profile).toHaveValue('HD-1080p')

    // Add: the request carries title, year, and the chosen profile.
    await user.click(screen.getByTestId('movie-add-button'))
    await waitFor(() =>
      expect(client.addMovie).toHaveBeenCalledWith({
        title: 'Dune: Part Two',
        year: '2024',
        quality_profile: 'HD-1080p',
      }),
    )
    // Success navigates back to the movies list.
    expect(await screen.findByTestId('movies-landing')).toBeInTheDocument()
  })

  it('lets the user change the profile before adding', async () => {
    const user = userEvent.setup()
    renderPage()
    await user.type(screen.getByTestId('movie-search-input'), 'dune')
    await user.click(await screen.findByTestId('result-603'))
    await user.selectOptions(await screen.findByTestId('movie-profile'), 'UHD-2160p')
    await user.click(screen.getByTestId('movie-add-button'))
    await waitFor(() =>
      expect(client.addMovie).toHaveBeenCalledWith(
        expect.objectContaining({ quality_profile: 'UHD-2160p' }),
      ),
    )
  })

  it('disables the add button until a title is present', () => {
    renderPage()
    expect(screen.getByTestId('movie-add-button')).toBeDisabled()
  })

  it('surfaces the API error inline on a failed add', async () => {
    vi.mocked(client.addMovie).mockRejectedValue(new Error('title is required'))
    const user = userEvent.setup()
    renderPage()
    await user.type(screen.getByTestId('movie-search-input'), 'dune')
    await user.click(await screen.findByTestId('result-603'))
    await user.click(screen.getByTestId('movie-add-button'))
    expect(await screen.findByTestId('movie-add-error')).toHaveTextContent('title is required')
    // Still on the add page (no navigation on failure).
    expect(screen.getByTestId('movie-add-form')).toBeInTheDocument()
  })

  it('greys the search box when TMDB is not configured (503)', async () => {
    vi.mocked(client.searchMovies).mockRejectedValue(
      new ApiError(503, 'tmdb is not configured (missing API key)'),
    )
    const user = userEvent.setup()
    renderPage()
    await user.type(screen.getByTestId('movie-search-input'), 'dune')
    const input = screen.getByTestId('movie-search-input')
    await waitFor(() => expect(input).toBeDisabled())
    expect(screen.getByTestId('movie-search-unavailable')).toHaveTextContent('not configured')
  })
})
