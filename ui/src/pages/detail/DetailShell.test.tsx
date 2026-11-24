import { act } from 'react'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError, client } from '../../api/client'
import type {
  Album,
  Artist,
  Author,
  BookAuthorSeries,
  Episode,
  Movie,
  PageArtist,
  Series,
  Title,
  Track,
} from '../../api/types'
import { DetailShell, KIND_LABEL, type DetailShellProps } from './DetailShell'
import { MovieDetailPage } from '../movies/MovieDetail'
import { SeriesDetailPage } from '../tv/SeriesDetail'
import { ArtistDetailPage } from '../music/ArtistDetail'
import { AuthorDetailPage } from '../books/AuthorDetail'

// Unified detail shell (issue #120 part A): one shared detail surface for
// the four existing detail routes. The shell unit tests cover the shared
// chrome (hero, poster placeholder / failed-art fallback, kind badge, action
// row, status line, nav arrows, loading/error/retry/auth states); the
// integration tests render all four real pages and assert the shared
// surface plus each kind's metadata, body sections and representative
// actions. All data comes from the (faked) API client.
vi.mock('../../api/client', () => {
  const ApiError = class ApiError extends Error {
    status: number
    constructor(status: number, body: string) {
      super(body)
      this.status = status
    }
  }
  const client = {
    // movies
    getMovie: vi.fn(),
    listMovies: vi.fn(),
    setMovieMonitored: vi.fn(),
    setMovieEditionMonitored: vi.fn(),
    runMoviePipeline: vi.fn(),
    // tv
    getSeries: vi.fn(),
    listEpisodes: vi.fn(),
    setEpisodeMonitored: vi.fn(),
    searchMissingEpisodes: vi.fn(),
    // music
    getArtist: vi.fn(),
    listAlbums: vi.fn(),
    listArtists: vi.fn(),
    listTracks: vi.fn(),
    runMusicPipeline: vi.fn(),
    getArtistHistory: vi.fn(),
    setArtistMonitored: vi.fn(),
    setAlbumMonitored: vi.fn(),
    getProfiles: vi.fn(),
    updateArtist: vi.fn(),
    // books
    getAuthor: vi.fn(),
    listTitles: vi.fn(),
    listAuthorSeries: vi.fn(),
    runBooksPipeline: vi.fn(),
    listRootFolders: vi.fn(),
    listBookMetadataProfiles: vi.fn(),
    updateAuthor: vi.fn(),
    setTitleMonitored: vi.fn(),
    listEditions: vi.fn(),
    setEditionMonitored: vi.fn(),
    listBookFiles: vi.fn(),
    listAuthorHistory: vi.fn(),
    listTitleSeries: vi.fn(),
    listBookSeries: vi.fn(),
    addBookSeries: vi.fn(),
    linkTitleToSeries: vi.fn(),
  }
  return { ApiError, client }
})

// ---- fixtures ----

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

function baseSeries(over: Partial<Series> = {}): Series {
  return {
    ID: 1,
    TVDBID: 1000,
    TMDBID: 2000,
    Title: 'Breaking Bad',
    Year: 2008,
    QualityProfile: 'HD-1080p',
    Monitored: true,
    SeriesType: 'standard',
    SeasonFolder: false,
    Tags: [],
    AddedAt: '2026-01-01T00:00:00Z',
    ...over,
  }
}

function baseArtist(over: Partial<Artist> = {}): Artist {
  return {
    ID: 7,
    MBID: 'mbid-7',
    Name: 'Radiohead',
    Monitored: true,
    Status: 'continuing',
    QualityProfile: 'FLAC',
    Path: 'Music/Radiohead',
    Rating: 87,
    Genre: 'Alternative',
    Links: [{ Name: 'Wikipedia', URL: 'https://en.wikipedia.org/wiki/Radiohead' }],
    TrackCount: 9,
    FileCount: 6,
    QueuedCount: 0,
    TotalSize: 1073741824,
    AddedAt: '2026-01-01T00:00:00Z',
    ...over,
  }
}

function baseAuthor(over: Partial<Author> = {}): Author {
  return {
    ID: 3,
    Name: 'Ursula K. Le Guin',
    Monitored: true,
    AddedAt: '2026-01-01T00:00:00Z',
    ...over,
  }
}

function baseTitle(over: Partial<Title> = {}): Title {
  return {
    ID: 11,
    AuthorID: 3,
    Name: 'The Left Hand of Darkness',
    Monitored: true,
    AddedAt: '2026-01-01T00:00:00Z',
    ...over,
  }
}

function ep(id: number, season: number, number: number, title = 'Pilot'): Episode {
  return { ID: id, SeriesID: 1, Season: season, Episode: number, Title: title, Monitored: true, AddedAt: '' }
}

function baseAlbum(over: Partial<Album> = {}): Album {
  return {
    ID: 11,
    ArtistID: 7,
    MBID: 'alb-11',
    Name: 'In Rainbows',
    Year: 2007,
    QualityProfile: 'FLAC',
    Monitored: true,
    VerifyChecksums: false,
    Checksum: '',
    AddedAt: '2026-01-01T00:00:00Z',
    ...over,
  }
}

function baseTrack(over: Partial<Track> = {}): Track {
  return {
    ID: 1,
    AlbumID: 11,
    MBID: 'trk-1',
    Disc: 1,
    Number: 1,
    Title: '15 Step',
    Checksum: '',
    Monitored: true,
    AddedAt: '2026-01-01T00:00:00Z',
    ...over,
  }
}

beforeEach(() => {
  vi.clearAllMocks()
  // Movies
  vi.mocked(client.getMovie).mockResolvedValue(baseMovie())
  vi.mocked(client.listMovies).mockResolvedValue({
    items: [baseMovie({ ID: 6 }), baseMovie(), baseMovie({ ID: 8 })],
    total: 3,
    page: 1,
    page_size: 25,
  })
  vi.mocked(client.setMovieMonitored).mockResolvedValue({ id: 7, monitored: false })
  vi.mocked(client.setMovieEditionMonitored).mockResolvedValue({ id: 2, monitored: true })
  vi.mocked(client.runMoviePipeline).mockResolvedValue({ imported: 'Dune.2021.1080p' })
  // TV
  vi.mocked(client.getSeries).mockResolvedValue(baseSeries())
  vi.mocked(client.listEpisodes).mockResolvedValue([ep(11, 1, 1)])
  vi.mocked(client.setEpisodeMonitored).mockResolvedValue({ id: 11, monitored: false })
  vi.mocked(client.searchMissingEpisodes).mockResolvedValue({ searched: 3 })
  // Music
  vi.mocked(client.getArtist).mockResolvedValue(baseArtist())
  vi.mocked(client.listAlbums).mockResolvedValue([baseAlbum()])
  vi.mocked(client.listArtists).mockResolvedValue({
    items: [baseArtist({ ID: 1, Name: 'Alpha' }), baseArtist(), baseArtist({ ID: 2, Name: 'Beta' })],
    total: 3,
    page: 1,
    page_size: 25,
  } as PageArtist)
  vi.mocked(client.listTracks).mockResolvedValue([baseTrack()])
  vi.mocked(client.runMusicPipeline).mockResolvedValue({ imported: 'In Rainbows.flac' })
  vi.mocked(client.getArtistHistory).mockResolvedValue([])
  vi.mocked(client.setArtistMonitored).mockResolvedValue({ id: 7, monitored: false })
  vi.mocked(client.setAlbumMonitored).mockResolvedValue({ id: 11, monitored: false })
  vi.mocked(client.getProfiles).mockResolvedValue({ profiles: ['FLAC'], default: 'FLAC' })
  vi.mocked(client.updateArtist).mockResolvedValue({ id: 7, monitored: false })
  // Books
  vi.mocked(client.getAuthor).mockResolvedValue(baseAuthor())
  vi.mocked(client.listTitles).mockResolvedValue([baseTitle()])
  vi.mocked(client.listAuthorSeries).mockResolvedValue([] as BookAuthorSeries[])
  vi.mocked(client.runBooksPipeline).mockResolvedValue({ imported: 'left-hand.epub' })
  vi.mocked(client.listRootFolders).mockResolvedValue([])
  vi.mocked(client.listBookMetadataProfiles).mockResolvedValue([])
  vi.mocked(client.updateAuthor).mockResolvedValue({ id: 3, monitored: false })
  vi.mocked(client.setTitleMonitored).mockResolvedValue({ id: 11, monitored: false })
  vi.mocked(client.listEditions).mockResolvedValue([])
  vi.mocked(client.setEditionMonitored).mockResolvedValue({ id: 1, monitored: false })
  vi.mocked(client.listBookFiles).mockResolvedValue([])
  vi.mocked(client.listAuthorHistory).mockResolvedValue([])
  vi.mocked(client.listTitleSeries).mockResolvedValue([])
  vi.mocked(client.listBookSeries).mockResolvedValue([])
  vi.mocked(client.addBookSeries).mockResolvedValue({ id: 1 })
  vi.mocked(client.linkTitleToSeries).mockResolvedValue({ title_id: 11, series_id: 1 })
})

afterEach(() => {
  vi.clearAllMocks()
})

// Render the four existing detail routes under one router.
function renderRoute(entry: string) {
  return render(
    <MemoryRouter initialEntries={[entry]}>
      <Routes>
        <Route path="/movies/:id" element={<MovieDetailPage />} />
        <Route path="/movies" element={<div data-testid="movies-landing">Movies</div>} />
        <Route path="/tv/:id" element={<SeriesDetailPage />} />
        <Route path="/tv" element={<div data-testid="tv-landing">TV</div>} />
        <Route path="/music/:id" element={<ArtistDetailPage />} />
        <Route path="/music" element={<div data-testid="music-landing">Music</div>} />
        <Route path="/books/:id" element={<AuthorDetailPage />} />
        <Route path="/books" element={<div data-testid="books-landing">Books</div>} />
      </Routes>
    </MemoryRouter>,
  )
}

// ---- shell unit tests ----

function renderShell(over: Partial<DetailShellProps> = {}) {
  const props: DetailShellProps = {
    kind: 'movie',
    pageTestId: 'movie-detail',
    loading: false,
    loadingText: 'Loading movie…',
    loadingTestId: 'movie-detail-loading',
    error: null,
    errorTitle: 'Movie unavailable',
    errorTestId: 'movie-detail-error',
    errorTestIdMessage: 'movie-detail-error-message',
    onRetry: () => {},
    back: { to: '/movies', label: '← Movies', testId: 'movie-detail-back' },
    status: null,
    statusTestId: 'movie-detail-status',
    title: 'Dune',
    heroTestId: 'movie-detail-header',
    children: <div data-testid="shell-body">body section</div>,
    ...over,
  }
  return render(
    <MemoryRouter>
      <DetailShell {...props} />
    </MemoryRouter>,
  )
}

describe('DetailShell (shared chrome)', () => {
  it('renders the hero: backdrop, poster placeholder, title, kind badge and metadata', () => {
    renderShell({
      heading: 'Dune (2021)',
      meta: <span data-testid="shell-meta">meta</span>,
    })
    const hero = screen.getByTestId('detail-hero')
    expect(hero).toBeInTheDocument()
    // The poster area is an accessible decorative placeholder (initials).
    const poster = screen.getByTestId('detail-hero-poster')
    expect(poster).toBeInTheDocument()
    expect(poster).toHaveAttribute('aria-hidden', 'true')
    expect(poster).toHaveTextContent('D')
    // Title + kind badge live in the hero.
    expect(screen.getByTestId('detail-hero-title')).toHaveTextContent('Dune (2021)')
    expect(screen.getByTestId('detail-hero-kind')).toHaveTextContent('Movie')
    expect(hero).toContainElement(screen.getByTestId('shell-meta'))
  })

  it('labels the kind badge for all four kinds', () => {
    const cases: { kind: DetailShellProps['kind']; label: string }[] = [
      { kind: 'movie', label: KIND_LABEL.movie },
      { kind: 'tv', label: KIND_LABEL.tv },
      { kind: 'music', label: KIND_LABEL.music },
      { kind: 'book', label: KIND_LABEL.book },
    ]
    for (const { kind, label } of cases) {
      const { unmount } = renderShell({ kind })
      expect(screen.getByTestId('detail-hero-kind')).toHaveTextContent(label)
      unmount()
    }
  })

  it('renders the common action row (left + right) and back link', () => {
    renderShell({
      actionsLeft: <button type="button" data-testid="shell-action-left">⟳</button>,
      actionsRight: <button type="button" data-testid="shell-action-right">⋯</button>,
    })
    const actions = screen.getByTestId('detail-actions')
    expect(actions).toContainElement(screen.getByTestId('shell-action-left'))
    expect(actions).toContainElement(screen.getByTestId('shell-action-right'))
    expect(screen.getByTestId('movie-detail-back')).toHaveAttribute('href', '/movies')
  })

  it('renders the status line, title extras and nav arrows', () => {
    const onPrev = vi.fn()
    const onNext = vi.fn()
    renderShell({
      status: 'Pipeline finished.',
      titleExtras: <span data-testid="shell-title-extras">extra</span>,
      nav: {
        prev: { testId: 'shell-nav-prev', icon: '←', label: 'Previous movie', onClick: onPrev },
        next: { testId: 'shell-nav-next', icon: '→', label: 'Next movie', onClick: onNext },
      },
    })
    expect(screen.getByTestId('movie-detail-status')).toHaveTextContent('Pipeline finished.')
    expect(screen.getByTestId('shell-title-extras')).toBeInTheDocument()
    const prev = screen.getByTestId('shell-nav-prev')
    expect(prev).toHaveAttribute('aria-label', 'Previous movie')
    void prev.click()
    expect(onPrev).toHaveBeenCalled()
    void screen.getByTestId('shell-nav-next').click()
    expect(onNext).toHaveBeenCalled()
  })

  it('shows the loading state while data is absent', () => {
    renderShell({ loading: true })
    expect(screen.getByTestId('movie-detail-loading')).toHaveTextContent('Loading movie…')
    expect(screen.queryByTestId('detail-hero')).not.toBeInTheDocument()
  })

  it('shows the error card with retry, and a sign-in link when auth is lost', async () => {
    const onRetry = vi.fn()
    const user = userEvent.setup()
    renderShell({ loading: true, error: 'boom', authLost: true, onRetry })
    expect(screen.getByTestId('movie-detail-error')).toBeInTheDocument()
    expect(screen.getByText('Movie unavailable')).toBeInTheDocument()
    expect(screen.getByTestId('movie-detail-error-message')).toHaveTextContent('boom')
    expect(screen.getByText('Sign in again')).toBeInTheDocument()
    await user.click(screen.getByText('Retry'))
    expect(onRetry).toHaveBeenCalledTimes(1)
  })

  it('falls back to the placeholder when the poster image fails to load', () => {
    const { container } = renderShell({ posterUrl: 'http://example.com/poster.jpg' })
    const img = container.querySelector('img')
    expect(img).not.toBeNull()
    // The image is replaced by the initials placeholder; no broken <img> remains.
    act(() => {
      img!.dispatchEvent(new Event('error'))
    })
    expect(screen.getByTestId('detail-hero-poster')).toHaveTextContent('D')
    expect(container.querySelector('img')).toBeNull()
  })
})

// ---- four-route integration tests ----

describe('DetailShell across the four detail routes', () => {
  it('movie: shared hero (title, kind, placeholder poster, metadata) around the existing sections', async () => {
    const { container } = renderRoute('/movies/7')
    await screen.findByTestId('movie-detail')

    const hero = screen.getByTestId('detail-hero')
    expect(screen.getAllByTestId('detail-hero')).toHaveLength(1)
    expect(screen.getByTestId('detail-hero-title')).toHaveTextContent('Dune (2021)')
    expect(screen.getByTestId('detail-hero-kind')).toHaveTextContent('Movie')
    expect(screen.getByTestId('detail-hero-poster')).toHaveTextContent('D')
    // Existing metadata stays inside the shared hero.
    expect(hero).toContainElement(screen.getByTestId('movie-detail-fields'))
    expect(hero).toContainElement(screen.getByTestId('movie-mon-7'))
    // The existing body sections stay outside the hero.
    const files = screen.getByTestId('movie-detail-files')
    expect(hero.contains(files)).toBe(false)
    // No duplicated visible headings: exactly one h2 on the page.
    expect(container.querySelectorAll('h2')).toHaveLength(1)
    // Common action row carries the movie toolbar.
    const actions = screen.getByTestId('detail-actions')
    expect(actions).toContainElement(screen.getByTestId('movie-detail-refresh'))
    expect(actions).toContainElement(screen.getByTestId('movie-detail-search'))
  })

  it('movie: Search Movie still runs the pipeline and reports via the shared status line', async () => {
    const user = userEvent.setup()
    renderRoute('/movies/7')
    await screen.findByTestId('movie-detail')
    await user.click(screen.getByTestId('movie-detail-search'))
    expect(await screen.findByTestId('movie-detail-status')).toHaveTextContent('imported')
    expect(client.runMoviePipeline).toHaveBeenCalledWith(7)
  })

  it('movie: load failures keep the error card and retry', async () => {
    vi.mocked(client.getMovie).mockRejectedValue(new Error('boom'))
    renderRoute('/movies/7')
    expect(await screen.findByTestId('movie-detail-error')).toBeInTheDocument()
    expect(screen.getByTestId('movie-detail-error-message')).toHaveTextContent('boom')
    expect(screen.getByText('Retry')).toBeInTheDocument()
  })

  it('movie: the back link still targets the movies list', async () => {
    renderRoute('/movies/7')
    await screen.findByTestId('movie-detail')
    expect(screen.getByTestId('movie-detail-back')).toHaveAttribute('href', '/movies')
  })

  it('tv: shared hero with the series metadata and TheTVDB credit', async () => {
    const { container } = renderRoute('/tv/1')
    await screen.findByTestId('series-detail')

    const hero = screen.getByTestId('detail-hero')
    expect(screen.getAllByTestId('detail-hero')).toHaveLength(1)
    expect(screen.getByTestId('detail-hero-title')).toHaveTextContent('Breaking Bad')
    expect(screen.getByTestId('detail-hero-kind')).toHaveTextContent('TV')
    expect(screen.getByTestId('detail-hero-poster')).toHaveTextContent('BB')
    expect(hero).toContainElement(screen.getByTestId('badge-monitored'))
    expect(hero).toContainElement(screen.getByTestId('series-detail-meta'))
    // Seasons stay in the body.
    const season = screen.getByTestId('season-1')
    expect(hero.contains(season)).toBe(false)
    expect(container.querySelectorAll('h2')).toHaveLength(1)
    const actions = screen.getByTestId('detail-actions')
    expect(actions).toContainElement(screen.getByTestId('detail-refresh'))
    expect(actions).toContainElement(screen.getByTestId('detail-search'))
    expect(actions).toContainElement(screen.getByTestId('detail-options'))
  })

  it('tv: Search Monitored still reports through the shared status line', async () => {
    const user = userEvent.setup()
    renderRoute('/tv/1')
    await screen.findByTestId('series-detail')
    await user.click(screen.getByTestId('detail-search'))
    expect(await screen.findByTestId('series-detail-status')).toHaveTextContent('Started search for 3')
    expect(client.searchMissingEpisodes).toHaveBeenCalledWith(1)
  })

  it('tv: a 404 keeps the error card', async () => {
    vi.mocked(client.getSeries).mockRejectedValue(new ApiError(404, 'series not found'))
    renderRoute('/tv/1')
    expect(await screen.findByTestId('series-detail-error')).toBeInTheDocument()
    expect(screen.getByTestId('series-detail-error-message')).toHaveTextContent('series not found')
    expect(screen.getByText('Retry')).toBeInTheDocument()
  })

  it('music: shared hero with rating, genre, pills and paging arrows', async () => {
    const { container } = renderRoute('/music/7')
    await screen.findByTestId('artist-detail')

    const hero = screen.getByTestId('detail-hero')
    expect(screen.getAllByTestId('detail-hero')).toHaveLength(1)
    // The heading keeps the existing e2e testid (the shared surface is the
    // same; only the testid differs per kind).
    expect(screen.getByTestId('artist-detail-name')).toHaveTextContent('Radiohead')
    expect(screen.getByTestId('detail-hero-kind')).toHaveTextContent('Artist')
    expect(screen.getByTestId('detail-hero-poster')).toHaveTextContent('R')
    expect(hero).toContainElement(screen.getByTestId('artist-detail-meta'))
    expect(hero).toContainElement(screen.getByTestId('artist-detail-rating'))
    expect(hero).toContainElement(screen.getByTestId('pill-path'))
    expect(hero).toContainElement(screen.getByTestId('pill-link-Wikipedia'))
    expect(hero).toContainElement(screen.getByTestId('artist-detail-prev'))
    expect(hero).toContainElement(screen.getByTestId('artist-detail-next'))
    // Albums stay in the body.
    const albums = screen.getByTestId('artist-detail-albums')
    expect(hero.contains(albums)).toBe(false)
    expect(container.querySelectorAll('h2')).toHaveLength(1)
    const actions = screen.getByTestId('detail-actions')
    expect(actions).toContainElement(screen.getByTestId('artist-detail-refresh'))
    expect(actions).toContainElement(screen.getByTestId('artist-detail-history'))
    expect(actions).toContainElement(screen.getByTestId('artist-detail-expand-all'))
  })

  it('music: Refresh & Scan still runs the artist pipeline via the shared status line', async () => {
    const user = userEvent.setup()
    renderRoute('/music/7')
    await screen.findByTestId('artist-detail')
    await user.click(screen.getByTestId('artist-detail-refresh'))
    expect(await screen.findByTestId('artist-detail-status')).toHaveTextContent('In Rainbows.flac')
    expect(client.runMusicPipeline).toHaveBeenCalledWith(7)
  })

  it('books: shared hero with book count, series badges and the field grid', async () => {
    const { container } = renderRoute('/books/3')
    await screen.findByTestId('author-detail')

    const hero = screen.getByTestId('detail-hero')
    expect(screen.getAllByTestId('detail-hero')).toHaveLength(1)
    // The heading keeps the existing e2e testid (the shared surface is the
    // same; only the testid differs per kind).
    expect(screen.getByTestId('author-detail-name')).toHaveTextContent('Ursula K. Le Guin')
    expect(screen.getByTestId('detail-hero-kind')).toHaveTextContent('Author')
    expect(screen.getByTestId('detail-hero-poster')).toHaveTextContent('UK')
    expect(hero).toContainElement(screen.getByTestId('author-detail-monitored'))
    expect(hero).toContainElement(screen.getByTestId('badge-book-count'))
    expect(hero).toContainElement(screen.getByTestId('author-detail-fields'))
    // The books accordion stays in the body.
    const books = screen.getByTestId('author-detail-books')
    expect(hero.contains(books)).toBe(false)
    expect(container.querySelectorAll('h2')).toHaveLength(1)
    const actions = screen.getByTestId('detail-actions')
    expect(actions).toContainElement(screen.getByTestId('author-detail-refresh'))
    expect(actions).toContainElement(screen.getByTestId('author-detail-search'))
  })

  it('books: Search still runs the author pipeline via the shared status line', async () => {
    const user = userEvent.setup()
    renderRoute('/books/3')
    await screen.findByTestId('author-detail')
    await user.click(screen.getByTestId('author-detail-search'))
    expect(await screen.findByTestId('author-detail-status')).toHaveTextContent('left-hand.epub')
    expect(client.runBooksPipeline).toHaveBeenCalledWith(3)
  })

  it('books: a load failure keeps the error card and retry', async () => {
    vi.mocked(client.getAuthor).mockRejectedValue(new Error('boom'))
    renderRoute('/books/3')
    expect(await screen.findByTestId('author-detail-error')).toBeInTheDocument()
    expect(screen.getByTestId('author-detail-error-message')).toHaveTextContent('boom')
    expect(screen.getByText('Retry')).toBeInTheDocument()
  })
})
