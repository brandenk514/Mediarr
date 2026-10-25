import { useEffect, useState } from 'react'
import { Link, NavLink, useLocation, useNavigate } from 'react-router-dom'
import { useAuth } from '../auth/AuthContext'
import { KINDS, SHARED_SECTIONS, kindById, type KindId, type SharedSection } from '../nav'

// Layout is the authenticated Sonarr-parity shell (P1): a full-width teal top
// bar (logo · global search · notifications · profile) over a slate left rail
// (kind switcher + per-kind library section + shared Activity/Wanted/Settings/
// System sections) and a charcoal content pane that renders the current route
// via <Outlet/>. Replaces the old top-nav Layout.
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
    <div className="app">
      <TopBar version={version} username={me?.username ?? ''} onLogout={logout} />
      <div className="app-body">
        <Rail />
        <div className="main">
          <div className="content">{children}</div>
        </div>
      </div>
    </div>
  )
}

// kindFromPath maps the current URL to the active library kind. Kind-specific
// paths (/tv, /movies, ...) map to their kind; every shared page (Calendar,
// Activity, Wanted, Settings, System, Dashboard) falls back to the parity
// default, TV.
function kindFromPath(pathname: string): KindId {
  if (pathname.startsWith('/movies')) return 'movie'
  if (pathname.startsWith('/music')) return 'music'
  if (pathname.startsWith('/books')) return 'book'
  return 'tv'
}

// ---- top bar ----

function TopBar({
  version,
  username,
  onLogout,
}: {
  version: string
  username: string
  onLogout: () => void
}) {
  const location = useLocation()
  const navigate = useNavigate()
  const [q, setQ] = useState('')
  const kind = kindFromPath(location.pathname)

  return (
    <header className="topbar">
      <Link to="/" className="brand" data-testid="brand">
        mediarr
      </Link>
      <div className="topbar-search">
        <input
          type="search"
          placeholder="Search…"
          value={q}
          onChange={(e) => setQ(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && q.trim()) {
              // P1: global search narrows the active library. (Full cross-kind
              // search lands with the per-kind pages.)
              navigate(`${kindById(kind).libPath}?q=${encodeURIComponent(q.trim())}`)
              setQ('')
            }
          }}
          aria-label="Global search"
          data-testid="global-search"
        />
      </div>
      <div className="topbar-right">
        {version && <span className="dim small">{version}</span>}
        <button
          type="button"
          className="topbar-icon"
          title="Notifications"
          aria-label="Notifications"
          data-testid="notifications"
        >
          ♡
        </button>
        <div className="topbar-user">
          <span className="topbar-avatar" aria-hidden>
            {(username[0] ?? '?').toUpperCase()}
          </span>
          <span>{username}</span>
        </div>
        <button className="ghost" onClick={onLogout} data-testid="logout">
          Sign out
        </button>
      </div>
    </header>
  )
}

// ---- left rail ----

function Rail() {
  const { pathname } = useLocation()
  const kind = kindFromPath(pathname)
  const meta = kindById(kind)

  return (
    <nav className="rail" aria-label="Primary">
      {/* Kind switcher: Mediarr is four-kind; the rail is parameterized by it. */}
      <div className="rail-kind" role="group" aria-label="Library kind">
        <div className="segmented">
          {KINDS.map((k) => (
            <NavLink
              key={k.id}
              to={k.libPath}
              className={`seg ${k.id === kind ? 'active' : ''}`}
              data-testid={`kind-${k.id}`}
            >
              {k.label}
            </NavLink>
          ))}
        </div>
      </div>

      {/* Library section (per kind): header + Add New / Library Import. */}
      <RailSection
        icon={meta.libIcon}
        label={meta.libLabel}
        to={meta.libPath}
        scope={meta.libPath}
        pathname={pathname}
        sub={[
          { to: meta.addPath, label: 'Add New' },
          ...(meta.importPath ? [{ to: meta.importPath, label: 'Library Import' }] : []),
        ]}
      />

      {/* Shared sections: Calendar, Activity, Wanted, Settings, System. */}
      {SHARED_SECTIONS.map((s) => (
        <SharedRailSection key={s.id} section={s} pathname={pathname} />
      ))}
    </nav>
  )
}

// RailSection renders one rail section: a header row (icon + label + caret)
// that links to `to`, with its sub-items revealed while the route is within
// `scope`.
function RailSection({
  icon,
  label,
  to,
  scope,
  pathname,
  sub,
}: {
  icon: string
  label: string
  to: string
  scope: string
  pathname: string
  sub: { to: string; label: string }[]
}) {
  const inScope = pathname === scope || pathname.startsWith(scope + '/')
  const headerActive = pathname === to
  return (
    <div className="rail-section">
      <NavLink
        to={to}
        className={`rail-item ${headerActive ? 'active' : ''}`}
        aria-expanded={inScope && sub.length > 0}
        data-testid={`rail-${label.toLowerCase().replace(/\s+/g, '-')}`}
      >
        <span className="rail-icon" aria-hidden>
          {icon}
        </span>
        <span className="rail-label">{label}</span>
        {sub.length > 0 && (
          <span className={`rail-caret ${inScope ? 'open' : ''}`} aria-hidden>
            ▸
          </span>
        )}
      </NavLink>
      {inScope && sub.length > 0 && (
        <div className="rail-sub">
          {sub.map((s) => (
            <NavLink
              key={s.to}
              to={s.to}
              className={`rail-item ${pathname === s.to ? 'active' : ''}`}
              data-testid={`rail-sub-${s.to.replace(/[^a-z]/gi, '')}`}
            >
              <span className="rail-label">{s.label}</span>
            </NavLink>
          ))}
        </div>
      )}
    </div>
  )
}

// SharedRailSection wraps RailSection for a SharedSection (adds the System
// count badge when present).
function SharedRailSection({ section, pathname }: { section: SharedSection; pathname: string }) {
  const inScope = pathname === section.base || pathname.startsWith(section.base + '/')
  const headerActive = pathname === section.to
  return (
    <div className="rail-section">
      <NavLink
        to={section.to}
        className={`rail-item ${headerActive ? 'active' : ''}`}
        data-testid={`rail-${section.id}`}
      >
        <span className="rail-icon" aria-hidden>
          {section.icon}
        </span>
        <span className="rail-label">{section.label}</span>
        {typeof section.badge === 'number' && section.badge > 0 && (
          <span className="rail-badge" data-testid={`rail-${section.id}-badge`}>
            {section.badge}
          </span>
        )}
        {section.sub.length > 0 && (
          <span className={`rail-caret ${inScope ? 'open' : ''}`} aria-hidden>
            ▸
          </span>
        )}
      </NavLink>
      {inScope && section.sub.length > 0 && (
        <div className="rail-sub">
          {section.sub.map((s) => (
            <NavLink
              key={s.to}
              to={s.to}
              className={`rail-item ${pathname === s.to ? 'active' : ''}`}
              data-testid={`rail-sub-${s.to.replace(/[^a-z]/gi, '')}`}
            >
              <span className="rail-label">{s.label}</span>
            </NavLink>
          ))}
        </div>
      )}
    </div>
  )
}
