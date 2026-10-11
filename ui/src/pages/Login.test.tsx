import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { AuthProvider } from '../auth/AuthContext'
import { LoginPage } from './Login'
import { client } from '../api/client'

vi.mock('../api/client', () => ({
  client: {
    login: vi.fn(),
    me: vi.fn(),
    logout: vi.fn(),
  },
}))

function renderLogin() {
  render(
    <AuthProvider>
      <LoginPage />
    </AuthProvider>,
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
})
