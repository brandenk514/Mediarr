import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { client } from './api/client'
import { getToken } from './api/store'
import { AuthProvider } from './auth/AuthContext'
import App from './App'

// The Layout top bar fetches /version raw; keep that off the network in tests.
vi.stubGlobal(
  'fetch',
  vi.fn().mockResolvedValue({ ok: false, json: async () => ({}) }),
)

vi.mock('./api/client', () => {
  class ApiError extends Error {
    status: number
    constructor(status: number, message: string) {
      super(message)
      this.status = status
    }
  }
  return {
    ApiError,
    // connectEvents returns a close function; the mock never connects.
    connectEvents: vi.fn(() => () => {}),
    client: {
      login: vi.fn(),
      me: vi.fn(),
      logout: vi.fn(),
      listSeries: vi.fn().mockResolvedValue({ items: [], total: 0, page: 1, page_size: 25 }),
      listMovies: vi.fn().mockResolvedValue({ items: [], total: 0, page: 1, page_size: 25 }),
      // #151: the cinematic Discover row set also reads the dashboard (KPI
      // + recent activity) and the artist / book lists (Recently Added +
      // Watchlist rows).
      getDashboard: vi.fn().mockResolvedValue({
        libraries: [],
        totals: { libraries: 0, wants: 0, queue: 0 },
        recent_history: [],
        recent_queue: [],
        indexers: [],
        generated_at: '2026-11-27T00:00:00Z',
      }),
      listArtists: vi.fn().mockResolvedValue({ items: [], total: 0, page: 1, page_size: 25 }),
      listBooks: vi.fn().mockResolvedValue([]),
      getMovieStats: vi.fn().mockResolvedValue(null),
      getSystemHealth: vi.fn(),
      // #118: the unified Discover page (the new default landing).
      discover: vi.fn(),
      // Issue #117: the global search results route (fan-out endpoints).
      searchTv: vi.fn(),
      searchMovies: vi.fn(),
      searchArtists: vi.fn(),
      lookupBooks: vi.fn(),
    },
  }
})

vi.mock('./api/store', () => ({
  getToken: vi.fn(),
}))

function renderApp(entries?: string[]) {
  return render(
    <MemoryRouter initialEntries={entries ?? ['/']}>
      <AuthProvider>
        <App />
      </AuthProvider>
    </MemoryRouter>,
  )
}

// Regression: App renders <Routes> and AuthGate calls useAuth, so it must be
// mounted inside a Router and an AuthProvider (main.tsx). Without either,
// the app throws "useRoutes() may be used only in the context of a <Router>
// component" on first paint.
describe('App', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('boots to the login page when no session exists', () => {
    ;(getToken as ReturnType<typeof vi.fn>).mockReturnValue(null)
    renderApp()
    expect(screen.getByTestId('username')).toBeInTheDocument()
    expect(screen.getByTestId('password')).toBeInTheDocument()
  })

  // #118: the default landing is the unified cross-kind Discover page
  // (supersedes the R7 Movies-list landing decision). The Movies list stays
  // reachable from the Library rail section.
  it('lands a signed-in user on /discover from /', async () => {
    ;(getToken as ReturnType<typeof vi.fn>).mockReturnValue('tok')
    ;(client.me as ReturnType<typeof vi.fn>).mockResolvedValue({ username: 'admin' })
    // The (all-empty) page still renders: title + the three row EmptyStates.
    ;(client.discover as ReturnType<typeof vi.fn>).mockResolvedValue({
      trending: [],
      upcoming: [],
      newReleases: [],
      unavailable: [],
    })
    ;(client.getSystemHealth as ReturnType<typeof vi.fn>).mockRejectedValue(new Error('nope'))

    renderApp(['/'])
    await expect(screen.findByTestId('discover-page-title')).resolves.toBeVisible()
  })

  // #148: the Dashboard section left the shell; the /dashboard route is kept
  // only as a redirect to the Discover landing.
  it('redirects /dashboard to /discover', async () => {
    ;(getToken as ReturnType<typeof vi.fn>).mockReturnValue('tok')
    ;(client.me as ReturnType<typeof vi.fn>).mockResolvedValue({ username: 'admin' })
    // The (all-empty) page still renders: title + the three row EmptyStates.
    ;(client.discover as ReturnType<typeof vi.fn>).mockResolvedValue({
      trending: [],
      upcoming: [],
      newReleases: [],
      unavailable: [],
    })
    ;(client.getSystemHealth as ReturnType<typeof vi.fn>).mockRejectedValue(new Error('nope'))

    renderApp(['/dashboard'])
    await expect(screen.findByTestId('discover-page-title')).resolves.toBeVisible()
  })

  // Issue #117: the global search results page is a FIRST-CLASS route
  // (PATHS.globalSearch = /search/all, distinct from the Indexers release
  // search at /search) with the query in the URL. If the route were missing,
  // nothing would render at that path and the title below would never appear.
  it('renders the global search results at /search/all with ?q=', async () => {
    ;(getToken as ReturnType<typeof vi.fn>).mockReturnValue('tok')
    ;(client.me as ReturnType<typeof vi.fn>).mockResolvedValue({ username: 'admin' })
    ;(client.getSystemHealth as ReturnType<typeof vi.fn>).mockRejectedValue(new Error('nope'))
    ;(client.searchTv as ReturnType<typeof vi.fn>).mockResolvedValue([])
    ;(client.searchMovies as ReturnType<typeof vi.fn>).mockResolvedValue([])
    ;(client.searchArtists as ReturnType<typeof vi.fn>).mockResolvedValue({
      query: 'dune',
      artists: [],
      release_groups: [],
    })
    ;(client.lookupBooks as ReturnType<typeof vi.fn>).mockResolvedValue([])

    renderApp(['/search/all?q=dune'])
    await expect(screen.findByTestId('global-search-title')).resolves.toBeVisible()
    await expect(screen.findByTestId('global-search-empty')).resolves.toBeVisible()
    expect(screen.getByTestId('global-search-query')).toHaveTextContent('Results for “dune”')
  })
})
