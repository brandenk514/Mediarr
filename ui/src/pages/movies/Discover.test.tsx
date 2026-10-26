import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { client } from '../../api/client'
import type { MovieSearchHit } from '../../api/types'
import { MovieDiscoverPage } from './Discover'

// Page test for the Radarr R3 Discover page: trending poster cards
// (title (year), genre chips, studio, overview) with an 'Add Movie' action
// that opens /movies/add?tmdb=<id>; keyless dev shows the 503 reason in a
// greyed-out empty state (the R2 search grey-out pattern).
vi.mock('../../api/client', () => ({
  client: {
    discoverMovies: vi.fn(),
  },
}))

function hit(id: string, title: string): MovieSearchHit {
  return {
    id,
    title,
    year: 2024,
    poster: '',
    overview: 'A trending movie.',
    certification: 'PG-13',
    runtime: 140,
    original_language: 'en',
    studio: 'Studio X',
    genres: ['Sci-Fi'],
    tmdb_pct: 80,
    imdb: '',
    links: { tmdb: `https://www.themoviedb.org/movie/${id}` },
  }
}

function AddProbe() {
  const location = useLocation()
  return <div data-testid="add-probe">{location.pathname}{location.search}</div>
}

function renderPage() {
  return render(
    <MemoryRouter initialEntries={['/movies/discover']}>
      <Routes>
        <Route path="/movies/discover" element={<MovieDiscoverPage />} />
        <Route path="/movies/add" element={<AddProbe />} />
      </Routes>
    </MemoryRouter>,
  )
}

beforeEach(() => {
  vi.mocked(client.discoverMovies).mockReset()
})

describe('MovieDiscoverPage (R3)', () => {
  it('renders trending cards and Add Movie links to /movies/add?tmdb=<id>', async () => {
    const user = userEvent.setup()
    vi.mocked(client.discoverMovies).mockResolvedValue({
      results: [hit('603', 'Dune: Part Two'), hit('604', 'The Odyssey')],
      page: 1,
      total: 20,
    })
    renderPage()

    expect(await screen.findByTestId('discover-card-603')).toHaveTextContent('Dune: Part Two (2024)')
    expect(screen.getByTestId('discover-studio-603')).toHaveTextContent('Studio X')
    expect(screen.getByTestId('discover-overview-603')).toHaveTextContent('A trending movie.')
    expect(screen.getByTestId('discover-card-603')).toHaveTextContent('Sci-Fi')
    expect(screen.getByTestId('discover-count')).toHaveTextContent('Page 1 of trending movies (20 total)')

    await user.click(screen.getByTestId('discover-add-603'))
    expect(screen.getByTestId('add-probe')).toHaveTextContent('/movies/add?tmdb=603')
  })

  it('shows the 503 reason in a greyed-out empty state when keyless', async () => {
    vi.mocked(client.discoverMovies).mockRejectedValue(
      new Error('tmdb is not configured (missing API key)'),
    )
    renderPage()

    const box = await screen.findByTestId('discover-unavailable')
    expect(box).toHaveTextContent('Discover unavailable')
    expect(box).toHaveTextContent('tmdb is not configured (missing API key)')
    expect(screen.queryByTestId('discover-grid')).not.toBeInTheDocument()
  })
})
