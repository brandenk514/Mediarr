import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
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
})
