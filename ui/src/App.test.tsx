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
      listSeries: vi.fn(),
      getDashboard: vi.fn(),
      getSystemHealth: vi.fn(),
    },
  }
})

vi.mock('./api/store', () => ({
  getToken: vi.fn(),
}))

const EMPTY_PAGE = { items: [], total: 0, page: 1, page_size: 25 } as const

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

  // P7 home decision: the default landing is the TV library (the Sonarr
  // users' home), not the cross-kind Dashboard.
  it('lands a signed-in user on the TV library from /', async () => {
    ;(getToken as ReturnType<typeof vi.fn>).mockReturnValue('tok')
    ;(client.me as ReturnType<typeof vi.fn>).mockResolvedValue({ username: 'admin' })
    ;(client.listSeries as ReturnType<typeof vi.fn>).mockResolvedValue(EMPTY_PAGE)
    ;(client.getSystemHealth as ReturnType<typeof vi.fn>).mockRejectedValue(new Error('nope'))

    renderApp(['/'])
    await expect(screen.findByTestId('tv-title')).resolves.toBeVisible()
  })

  // The Dashboard stays reachable from the /dashboard route (the rail's
  // Dashboard item links here).
  it('renders the Dashboard at /dashboard', async () => {
    ;(getToken as ReturnType<typeof vi.fn>).mockReturnValue('tok')
    ;(client.me as ReturnType<typeof vi.fn>).mockResolvedValue({ username: 'admin' })
    ;(client.getDashboard as ReturnType<typeof vi.fn>).mockRejectedValue(new Error('offline'))
    ;(client.getSystemHealth as ReturnType<typeof vi.fn>).mockRejectedValue(new Error('nope'))

    renderApp(['/dashboard'])
    await expect(screen.findByTestId('dashboard-error')).resolves.toBeVisible()
  })
})
