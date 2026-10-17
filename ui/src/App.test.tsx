import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import { AuthProvider } from './auth/AuthContext'
import App from './App'

vi.mock('./api/client', () => ({
  client: {
    login: vi.fn(),
    me: vi.fn(),
    logout: vi.fn(),
  },
}))

// Regression: App renders <Routes> and AuthGate calls useAuth, so it must be
// mounted inside a Router and an AuthProvider (main.tsx). Without either, the
// app throws "useRoutes() may be used only in the context of a <Router>
// component" on first paint.
describe('App', () => {
  it('boots to the login page when no session exists', () => {
    render(
      <MemoryRouter>
        <AuthProvider>
          <App />
        </AuthProvider>
      </MemoryRouter>,
    )
    expect(screen.getByTestId('username')).toBeInTheDocument()
    expect(screen.getByTestId('password')).toBeInTheDocument()
  })
})
