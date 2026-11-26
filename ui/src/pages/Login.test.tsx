import { render, screen } from '@testing-library/react'
import { MemoryRouter, useLocation } from 'react-router-dom'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { AuthProvider } from '../auth/AuthContext'
import App from '../App'
import { LoginPage } from './Login'
import { client } from '../api/client'

vi.mock('../api/client', () => ({
  client: {
    login: vi.fn(),
    me: vi.fn(),
    logout: vi.fn(),
    listMovies: vi.fn(),
    getMovieStats: vi.fn().mockRejectedValue(new Error('stats unavailable')),
    getSystemHealth: vi.fn().mockResolvedValue({ checks: [], ok: 0, warning: 0, error: 0 }),
    // #123 redirect tests: the global search results route (fan-out).
    searchTv: vi.fn(),
    searchMovies: vi.fn(),
    searchArtists: vi.fn(),
    lookupBooks: vi.fn(),
  },
}))

function renderLogin() {
  // LoginPage uses useNavigate/useLocation (post-login redirect), so it must
  // mount inside a Router.
  render(
    <MemoryRouter>
      <AuthProvider>
        <LoginPage />
      </AuthProvider>
    </MemoryRouter>,
  )
}

describe('LoginPage', () => {
  it('renders the username and password fields', () => {
    renderLogin()
    expect(screen.getByTestId('username')).toBeInTheDocument()
    expect(screen.getByTestId('password')).toBeInTheDocument()
  })

  it('logs in with entered credentials', async () => {
    const user = userEvent.setup()
    vi.mocked(client.login).mockResolvedValue({
      token: 'ma_t',
      user: 'admin',
      role: 'admin',
      token_prefix: 'ma_',
    } as never)
    vi.mocked(client.me).mockResolvedValue({ username: 'admin', role: 'admin' })

    renderLogin()
    await user.type(screen.getByTestId('username'), 'admin')
    await user.type(screen.getByTestId('password'), 'a-very-long-password')
    await user.click(screen.getByRole('button', { name: /sign in/i }))

    expect(client.login).toHaveBeenCalledWith({
      username: 'admin',
      password: 'a-very-long-password',
    })
  })

  it('shows a server error on failed login', async () => {
    const user = userEvent.setup()
    vi.mocked(client.login).mockRejectedValue(new Error('invalid credentials'))

    renderLogin()
    await user.type(screen.getByTestId('username'), 'admin')
    await user.type(screen.getByTestId('password'), 'wrong-password-here')
    await user.click(screen.getByRole('button', { name: /sign in/i }))

    expect(await screen.findByTestId('login-error')).toHaveTextContent('invalid credentials')
  })

  // Regression: the API used to accept the password but the UI never
  // navigated away from /login, so users were stuck on the login screen.
  // Signing in must return the user to the route they originally requested.
  it('returns to the protected route the user requested after a successful login', async () => {
    const user = userEvent.setup()
    vi.mocked(client.login).mockResolvedValue({
      token: 'ma_t',
      user: 'admin',
      role: 'admin',
      token_prefix: 'ma_',
    } as never)
    vi.mocked(client.me).mockResolvedValue({ username: 'admin', role: 'admin' })
    vi.mocked(client.listMovies).mockResolvedValue({
      items: [],
      total: 0,
      page: 1,
      page_size: 25,
    } as never)

    render(
      <MemoryRouter initialEntries={['/movies']}>
        <AuthProvider>
          <App />
        </AuthProvider>
      </MemoryRouter>,
    )

    // The auth gate bounced the visit to /login; sign in.
    await user.type(await screen.findByTestId('username'), 'admin')
    await user.type(screen.getByTestId('password'), 'a-very-long-password')
    await user.click(screen.getByRole('button', { name: /sign in/i }))

    // ...and the user lands back on the movies list, not the login page.
    expect(await screen.findByTestId('movies-title')).toBeInTheDocument()
    expect(screen.queryByTestId('username')).not.toBeInTheDocument()
  })

  // #123: the post-login redirect used to read ONLY from.pathname, so a
  // bounce from the top-bar global search (/search/all?q=…) landed on
  // /search/all with the query dropped. The stored location is the full
  // router location; the redirect must carry all of it.
  it('preserves the query string when returning to the global search route after login', async () => {
    const user = userEvent.setup()
    vi.mocked(client.login).mockResolvedValue({
      token: 'ma_t',
      user: 'admin',
      role: 'admin',
      token_prefix: 'ma_',
    } as never)
    vi.mocked(client.me).mockResolvedValue({ username: 'admin', role: 'admin' })
    vi.mocked(client.searchTv).mockResolvedValue([] as never)
    vi.mocked(client.searchMovies).mockResolvedValue([] as never)
    vi.mocked(client.searchArtists).mockResolvedValue({ artists: [] } as never)
    vi.mocked(client.lookupBooks).mockResolvedValue([] as never)

    render(
      <MemoryRouter initialEntries={['/search/all?q=dune']}>
        <AuthProvider>
          <App />
        </AuthProvider>
      </MemoryRouter>,
    )

    // The auth gate bounced the visit to /login with the full location
    // (pathname + ?q=) in state; sign in.
    await user.type(await screen.findByTestId('username'), 'admin')
    await user.type(screen.getByTestId('password'), 'a-very-long-password')
    await user.click(screen.getByRole('button', { name: /sign in/i }))

    // The preserved query reaches the results page, not the idle state.
    const queryLine = await screen.findByTestId('global-search-query')
    expect(queryLine).toHaveTextContent('dune')
    expect(screen.queryByTestId('global-search-idle')).not.toBeInTheDocument()
  })

  it('preserves the hash when returning to a route after login', async () => {
    const user = userEvent.setup()
    vi.mocked(client.login).mockResolvedValue({
      token: 'ma_t',
      user: 'admin',
      role: 'admin',
      token_prefix: 'ma_',
    } as never)
    vi.mocked(client.me).mockResolvedValue({ username: 'admin', role: 'admin' })
    vi.mocked(client.listMovies).mockResolvedValue({
      items: [],
      total: 0,
      page: 1,
      page_size: 25,
    } as never)

    // Probe: observes the location the router settles on after login.
    let observed = ''
    function LocationProbe() {
      const location = useLocation()
      observed = `${location.pathname}${location.search}${location.hash}`
      return null
    }
    render(
      <MemoryRouter initialEntries={['/movies#top']}>
        <AuthProvider>
          <App />
          <LocationProbe />
        </AuthProvider>
      </MemoryRouter>,
    )

    await user.type(await screen.findByTestId('username'), 'admin')
    await user.type(screen.getByTestId('password'), 'a-very-long-password')
    await user.click(screen.getByRole('button', { name: /sign in/i }))

    expect(await screen.findByTestId('movies-title')).toBeInTheDocument()
    expect(observed).toBe('/movies#top')
  })
})
