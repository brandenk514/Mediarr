import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { client } from '../../api/client'
import type { Movie, MovieCollection } from '../../api/types'
import { MovieCollectionsPage } from './Collections'

// Page test for the Radarr R3 Collections page: collection cards with the
// missing-count line, aggregated quality/root + genre chips, and member
// rows (poster thumb, Title (year), monitored-edition chips + monitored
// state, quality profile). Keyless-safe: name-less groups show 'Unknown'.
vi.mock('../../api/client', () => ({
  client: {
    listMovieCollections: vi.fn(),
  },
}))

function movie(
  id: number,
  title: string,
  quality = 'HD',
  monitored = true,
  editions: Movie['Editions'] = [],
  root = '/media/movies',
  genres: string[] = [],
): Movie {
  return {
    ID: id,
    Title: title,
    Year: 2021,
    QualityProfile: quality,
    Monitored: monitored,
    AddedAt: '2026-01-01T00:00:00Z',
    RootFolder: root,
    Genres: genres,
    Editions: editions,
  }
}

const theatrical = { ID: 10, Label: 'Theatrical', Monitored: true, Quality: 'HD', Size: 0 }
const twoD = { ID: 11, Label: '2D', Monitored: true, Quality: 'HD', Size: 0 }

const dune = movie(1, 'Dune', 'HD', true, [theatrical, twoD], '/media/movies', ['Sci-Fi', 'Drama'])
const dune2 = movie(2, 'Dune: Part Two', 'HD', true, [theatrical], '/media/movies', ['Sci-Fi'])
const matrix = movie(3, 'The Matrix', 'SD', false, [theatrical], '/media/movies', ['Action'])

function collection(id: string, name: string, missing: number, movies: Movie[]): MovieCollection {
  return { id, name, movie_count: movies.length, missing_count: missing, movies }
}

function renderPage() {
  return render(
    <MemoryRouter initialEntries={['/movies/collections']}>
      <MovieCollectionsPage />
    </MemoryRouter>,
  )
}

beforeEach(() => {
  vi.mocked(client.listMovieCollections).mockReset()
})

describe('MovieCollectionsPage (R3)', () => {
  it('renders a card per collection with missing line, aggregates, and member rows', async () => {
    const user = userEvent.setup()
    vi.mocked(client.listMovieCollections).mockResolvedValue([
      collection('603', 'Dune', 2, [dune, dune2]),
      collection('604', 'The Matrix', 0, [matrix]),
    ])
    renderPage()

    const card = await screen.findByTestId('collection-card-603')
    expect(screen.getByTestId('collection-card-604')).toBeInTheDocument()
    expect(card).toHaveTextContent('Dune')
    expect(screen.getByTestId('collection-name-603')).toHaveTextContent('Dune')
    expect(screen.getByTestId('collection-missing-603')).toHaveTextContent(
      '2 missing movies from library',
    )
    expect(screen.getByTestId('collection-quality-603')).toHaveTextContent('HD · /media/movies')
    const genres = screen.getByTestId('collection-genres-603')
    expect(genres).toHaveTextContent('Drama')
    expect(genres).toHaveTextContent('Sci-Fi')

    // Member rows: title link, monitored state + edition chips, quality.
    const row = screen.getByTestId('collection-member-1')
    expect(screen.getByTestId('collection-member-link-1')).toHaveTextContent('Dune (2021)')
    expect(row).toHaveTextContent('Monitored')
    expect(row).toHaveTextContent('Theatrical')
    expect(row).toHaveTextContent('2D')
    expect(screen.getByTestId('collection-member-quality-1')).toHaveTextContent('HD')
    expect(screen.getByTestId('collection-member-2')).toHaveTextContent('Dune: Part Two (2021)')

    await user.click(screen.getByTestId('collection-member-link-1'))
  })

  it('shows Mixed when members disagree on quality or root', async () => {
    vi.mocked(client.listMovieCollections).mockResolvedValue([
      collection(
        '604',
        'Mixed',
        0,
        [
          movie(10, 'Alpha', 'HD', true, [], '/media/movies'),
          movie(11, 'Beta', 'SD', false, [], '/media/other'),
        ],
      ),
    ])
    renderPage()
    expect(await screen.findByTestId('collection-quality-604')).toHaveTextContent('Mixed · Mixed')
  })

  it('renders the empty state when no library movie has a collection', async () => {
    vi.mocked(client.listMovieCollections).mockResolvedValue([])
    renderPage()
    expect(await screen.findByTestId('collections-empty')).toHaveTextContent('No collections')
    expect(screen.getByTestId('collections-empty-add')).toHaveTextContent('+ Add movie')
  })

  it('is keyless-safe: a name-less group shows Unknown and missing 0', async () => {
    vi.mocked(client.listMovieCollections).mockResolvedValue([
      collection('unknown', '', 0, [movie(20, 'Solo Movie')]),
    ])
    renderPage()
    expect(await screen.findByTestId('collection-name-unknown')).toHaveTextContent('Unknown')
    expect(screen.getByTestId('collection-missing-unknown')).toHaveTextContent(
      '0 missing movies from library',
    )
  })
})
