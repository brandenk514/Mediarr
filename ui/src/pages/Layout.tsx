import { useEffect, useState } from 'react'
import { Link, useLocation, useNavigate } from 'react-router-dom'
import { client } from '../api/client'
import { useAuth } from '../auth/AuthContext'
import { Badge, SidebarItem } from '../components/primitives'
import { SECTIONS, sectionForPath, type KindGroup, type NavSection } from '../nav'

// Layout is the unified app shell (issue #116): a full-width top bar
// (brand · global search · notifications · profile) over a sidebar (the eight
// nav.ts sections, each a design-system SidebarItem) and a content pane that
// renders the current route via `children`. Replaces the Sonarr-parity shell:
// the TV/Movies/Music/Books kind switcher is gone — kind is now a facet under
// the Library section, and the rail is no longer parameterized by it.
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
        <Sidebar />
        <div className="main">
          <div className="content">{children}</div>
        </div>
      </div>
    </div>
  )
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
  // The global search input is VISUAL/STRUCTURAL ONLY in this order: it
  // renders and holds a draft query, but does not execute a search. Functional
  // cross-kind search (query execution, results) is issue #117.
  const [q, setQ] = useState('')

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

// useSystemBadge polls the structured health endpoint and returns the number
// of non-ok checks (warning + error) — the orange System nav badge. It
// returns 0 when the endpoint is unavailable (e.g. a P1-only dev stack
// answers 404) so no badge is shown.
function useSystemBadge(): number {
  const [count, setCount] = useState(0)
  useEffect(() => {
    let live = true
    client
      .getSystemHealth()
      .then((h) => {
        if (!live) return
        const nonOk = (h.warning ?? 0) + (h.error ?? 0)
        setCount(nonOk > 0 ? nonOk : 0)
      })
      .catch(() => {
        if (live) setCount(0)
      })
    return () => {
      live = false
    }
  }, [])
  return count
}
export { useSystemBadge }

// ---- sidebar ----

// Sidebar renders the unified shell's navigation: one SidebarItem per nav.ts
// section (active state from the current route), with sub-links revealed
// while the route is within the section, and — under Library — the per-kind
// groups (the kind facet) and their first-class sub-links.
export function Sidebar() {
  const { pathname } = useLocation()
  const navigate = useNavigate()
  const current = sectionForPath(pathname)
  const systemBadge = useSystemBadge()

  return (
    <nav className="rail flex flex-col gap-0.5" aria-label="Primary">
      {SECTIONS.map((section) => (
        <SectionNode
          key={section.id}
          section={section}
          active={current?.id === section.id}
          pathname={pathname}
          onNavigate={navigate}
          badge={
            section.id === 'system' && systemBadge > 0 ? (
              <Badge tone="warning">{systemBadge}</Badge>
            ) : undefined
          }
        />
      ))}
    </nav>
  )
}

// SectionNode renders one sidebar section: the section row (SidebarItem) and,
// while the section is the current area, its sub-links (flat subs and/or the
// Library's per-kind groups).
function SectionNode({
  section,
  active,
  pathname,
  onNavigate,
  badge,
}: {
  section: NavSection
  active: boolean
  pathname: string
  onNavigate: (to: string) => void
  badge?: React.ReactNode
}) {
  const showSubs = active && (section.subs?.length ?? 0) > 0
  const showGroups = active && (section.groups?.length ?? 0) > 0

  return (
    <>
      <SidebarItem
        label={section.label}
        icon={section.icon}
        active={active}
        badge={badge}
        onClick={() => onNavigate(section.to)}
        testId={`rail-${section.id}`}
      />
      {(showSubs || showGroups) && (
        <div className="flex flex-col gap-0.5 pl-3">
          {(section.subs ?? []).map((l) => (
            <NavLinkRow key={l.to} to={l.to} label={l.label} pathname={pathname} onNavigate={onNavigate} />
          ))}
          {(section.groups ?? []).map((g) => (
            <KindGroupNode key={g.kind} group={g} pathname={pathname} onNavigate={onNavigate} />
          ))}
        </div>
      )}
    </>
  )
}

// KindGroupNode renders one Library kind group (the kind facet): the group
// row links to the kind's list page, and its first-class sub-links (Add New,
// Collections, …) are revealed while the route is within the group's base.
function KindGroupNode({
  group,
  pathname,
  onNavigate,
}: {
  group: KindGroup
  pathname: string
  onNavigate: (to: string) => void
}) {
  const inScope = pathname === group.base || pathname.startsWith(group.base + '/')
  return (
    <>
      <NavLinkRow
        to={group.to}
        label={group.label}
        pathname={pathname}
        onNavigate={onNavigate}
        icon={group.icon}
      />
      {inScope && group.sub.length > 0 && (
        <div className="flex flex-col gap-0.5 pl-3">
          {group.sub.map((l) => (
            <NavLinkRow key={l.to} to={l.to} label={l.label} pathname={pathname} onNavigate={onNavigate} />
          ))}
        </div>
      )}
    </>
  )
}

// NavLinkRow is one navigation link row (a SidebarItem). Its testId follows
// the rail-sub-<path-without-separators> scheme the e2e suite has used since
// P1, so existing specs keep their selectors (e.g. /movies/add ->
// rail-sub-moviesadd).
function NavLinkRow({
  to,
  label,
  pathname,
  onNavigate,
  icon,
}: {
  to: string
  label: string
  pathname: string
  onNavigate: (to: string) => void
  icon?: string
}) {
  return (
    <SidebarItem
      label={label}
      icon={icon}
      active={pathname === to}
      onClick={() => onNavigate(to)}
      testId={`rail-sub-${stripPath(to)}`}
    />
  )
}

// stripPath mirrors the P1 rail-sub testId scheme: '/movies/collections' ->
// 'moviescollections'.
function stripPath(to: string): string {
  return to.replace(/[^a-z]/gi, '')
}
