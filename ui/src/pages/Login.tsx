import { useState } from 'react'
import { useAuth } from '../auth/AuthContext'

export function LoginPage() {
  const { login } = useAuth()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setError(null)
    setBusy(true)
    try {
      await login(username, password)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'login failed')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="login-wrap">
      <form className="card login-card" onSubmit={submit} aria-label="login">
        <h1>
          mediarr <span className="dim">sign in</span>
        </h1>
        <label>
          Username
          <input
            data-testid="username"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            autoComplete="username"
            required
          />
        </label>
        <label>
          Password
          <input
            data-testid="password"
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="current-password"
            required
          />
        </label>
        {error && (
          <p className="error" role="alert" data-testid="login-error">
            {error}
          </p>
        )}
        <button type="submit" disabled={busy}>
          {busy ? 'Signing in…' : 'Sign in'}
        </button>
        <p className="dim small">
          First run: the username + password you enter become the initial admin account.
        </p>
      </form>
    </div>
  )
}
