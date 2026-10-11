import { useEffect, useState } from 'react'
import { Link, NavLink } from 'react-router-dom'
import { useAuth } from '../auth/AuthContext'

// Layout is the authenticated shell (issue #39 scaffold): top nav + content.
// The per-kind pages (#40–#44) render inside <Outlet/> via nested routes.
export function Layout({ children }: { children: React.ReactNode }) {
  const { me, logout } = useAuth()
  const [version, setVersion] = useState('')

  useEffect(() => {
    fetch('/version')
      .then((r) => (r.ok ? r.json() : Promise.reject(r.status)))
      .then((j: { version?: string }) => setVersion(j.version ?? ''))
      .catch(() => setVersion(''))
  }, [])

  return (
    <div className="layout">
      <header className="topbar">
        <Link to="/" className="brand">
          mediarr
        </Link>
        <nav>
          <NavLink to="/" end>
            Dashboard
          </NavLink>
          <NavLink to="/movies">Movies</NavLink>
          <NavLink to="/tv">TV</NavLink>
          <NavLink to="/music">Music</NavLink>
          <NavLink to="/books">Books</NavLink>
          <NavLink to="/queue">Queue</NavLink>
          <NavLink to="/settings">Settings</NavLink>
        </nav>
        <div className="topbar-right">
          {version && <span className="dim small">{version}</span>}
          <span className="dim small">{me?.username}</span>
          <button className="ghost" onClick={logout} data-testid="logout">
            Sign out
          </button>
        </div>
      </header>
      <main className="content">{children}</main>
    </div>
  )
}
