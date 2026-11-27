import {
  Bars3Icon,
  BellIcon,
  MagnifyingGlassIcon,
  XMarkIcon,
} from '@heroicons/react/24/outline'
import { useEffect, useRef, useState } from 'react'
import { Link, useLocation, useNavigate } from 'react-router-dom'
import { client } from '../api/client'
import { useAuth } from '../auth/AuthContext'
import { Badge, SidebarItem } from '../components/primitives'
import { useMediaQuery } from '../lib/useMediaQuery'
import { PATHS, SECTIONS, sectionForPath, type NavLink, type Section } from '../nav'

// Layout is the unified app shell (issue #116; reshaped by issue #148 to the
// Seerr layout: NO top bar). The shell is a row: a full-height sidebar
// (brand at the top, the nine nav.ts sections in the middle — each a
// design-system SidebarItem — and the version pill at the bottom, fed by
// the existing /version endpoint) beside a content column whose header
// holds the pill global search and the circular avatar menu (admin
// identity + sign-out). On mobile (≤768px, issue #123) the fixed rail is
// replaced by the nav drawer opened from the content header.
export function Layout({ children }: { children: React.ReactNode }) {
  const { me, logout } = useAuth()
  // #123: the shell is mobile (≤768px) when the breakpoint matches; the
  // fixed rail is then hidden behind the content-header nav drawer.
  const isMobile = useMediaQuery('(max-width: 768px)')
  const [navOpen, setNavOpen] = useState(false)

  // If the viewport grows back past the breakpoint, the drawer state is
  // meaningless for the desktop shell — close it.
  useEffect(() => {
    if (!isMobile) setNavOpen(false)
  }, [isMobile])

  return (
    <div className="app">
      {/* Desktop only: the full-height rail. On mobile the SAME Sidebar
          lives inside the nav drawer (NavDrawer), so it isn't rendered
          twice. */}
      {!isMobile && <Sidebar />}
      <div className="app-main">
        <ContentHeader
          username={me?.username ?? ''}
          onLogout={logout}
          onMenuClick={isMobile ? () => setNavOpen(true) : undefined}
          menuOpen={navOpen}
        />
        <div className="main">
          <div className="content">{children}</div>
        </div>
      </div>
      {isMobile && navOpen && <NavDrawer onClose={() => setNavOpen(false)} />}
    </div>
  )
}

// ---- sidebar (brand + nav + version pill) ----

// Sidebar renders the shell's navigation column (issue #148): the brand
// wordmark at the top, one SidebarItem per nav.ts section (active state from
// the current route; sub-links revealed while the route is within the
// section, incl. the Indexers group parked under Settings), and the version
// pill at the bottom.
export function Sidebar() {
  const { pathname } = useLocation()
  const navigate = useNavigate()
  const current = sectionForPath(pathname)
  const systemBadge = useSystemBadge()

  return (
    <aside className="rail flex flex-col" aria-label="Sidebar">
      <Link to={PATHS.discover} className="rail-brand" data-testid="brand">
        mediarr
      </Link>
      <nav className="rail-nav flex flex-col gap-0.5" aria-label="Primary">
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
      <div className="rail-foot">
        <VersionPill />
      </div>
    </aside>
  )
}

// versionPillLabel maps the /version payload to the pill's state label
// (Seerr's VersionStatus: "Stable" for stamped releases, "Develop" for dev
// builds). An empty version renders no pill at all.
export function versionPillLabel(version: string): string | null {
  if (!version) return null
  return version.toLowerCase().startsWith('dev') ? 'Develop' : 'Stable'
}

// VersionPill is the sidebar's footer chip: the app version fed by the
// existing public GET /version endpoint (no new API, issue #148). It hides
// itself when the version is empty so a dev stack without build stamps stays
// clean.
function VersionPill() {
  const [version, setVersion] = useState('')
  useEffect(() => {
    let cancel = false
    fetch('/version')
      .then((r) => (r.ok ? r.json() : Promise.reject(r.status)))
      .then((j: { version?: string }) => {
        if (!cancel) setVersion(j.version ?? '')
      })
      .catch(() => {
        if (!cancel) setVersion('')
      })
    return () => {
      cancel = true
    }
  }, [])
  const label = versionPillLabel(version)
  if (!label) return null
  return (
    <span className="version-pill" title={version} data-testid="version-pill">
      <span className="version-pill-state">{label}</span>
      <span className="version-pill-ver">{version}</span>
    </span>
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

// SectionNode renders one sidebar section: the section row (SidebarItem) and,
// while the section is the current area, its sub-links (a kind's first-class
// pages, or the Indexers group parked under Settings, issue #148).
function SectionNode({
  section,
  active,
  pathname,
  onNavigate,
  badge,
}: {
  section: Section
  active: boolean
  pathname: string
  onNavigate: (to: string) => void
  badge?: React.ReactNode
}) {
  const Icon = section.icon
  const showSubs = active && (section.subs?.length ?? 0) > 0

  return (
    <>
      <SidebarItem
        label={section.label}
        icon={<Icon className="h-5 w-5" />}
        active={active}
        badge={badge}
        onClick={() => onNavigate(section.to)}
        testId={`rail-${section.id}`}
      />
      {showSubs && (
        <div className="flex flex-col gap-0.5 pl-3">
          {(section.subs ?? []).map((l) => (
            <SubLinkNode key={l.to} link={l} pathname={pathname} onNavigate={onNavigate} />
          ))}
        </div>
      )}
    </>
  )
}

// SubLinkNode renders one sub-link row and, while the route is within the
// link's base, its nested sub-links (the Prowlarr group under the Settings
// sub-link, issue #148).
function SubLinkNode({
  link,
  pathname,
  onNavigate,
}: {
  link: NavLink
  pathname: string
  onNavigate: (to: string) => void
}) {
  const inScope = (link.base ?? [link.to]).some(
    (b) => pathname === b || pathname.startsWith(`${b}/`),
  )
  const showNested = inScope && (link.subs?.length ?? 0) > 0
  return (
    <>
      <NavLinkRow
        to={link.to}
        label={link.label}
        pathname={pathname}
        onNavigate={onNavigate}
        active={inScope}
      />
      {showNested && (
        <div className="flex flex-col gap-0.5 pl-3">
          {(link.subs ?? []).map((l2) => (
            <NavLinkRow key={l2.to} to={l2.to} label={l2.label} pathname={pathname} onNavigate={onNavigate} />
          ))}
        </div>
      )}
    </>
  )
}

// NavLinkRow is one navigation link row (a SidebarItem). Its testId defaults
// to the rail-sub-<path-without-separators> scheme the e2e suite has used
// since P1, so existing specs keep their selectors (e.g. /movies/add ->
// rail-sub-moviesadd). `active` lets a group row use a broader rule than
// exact match.
function NavLinkRow({
  to,
  label,
  pathname,
  onNavigate,
  active,
}: {
  to: string
  label: string
  pathname: string
  onNavigate: (to: string) => void
  active?: boolean
}) {
  return (
    <SidebarItem
      label={label}
      active={active ?? pathname === to}
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

// ---- content header (pill search + avatar menu) ----

// ContentHeader is the shell's header (issue #148: it replaces the retired
// .topbar). It holds the pill global search (issue #117) and, to its right,
// the (no-op) notifications bell and the UserMenu. Exported (like Sidebar)
// so the global-search bar's behavior (issue #117) is testable in isolation.
//
// #123: at mobile widths the shell passes `onMenuClick`/`menuOpen` and the
// header renders a hamburger (testId nav-toggle) that opens the nav drawer.
// Without those props (desktop, or isolated renders) no toggle renders.
export function ContentHeader({
  username,
  onLogout,
  onMenuClick,
  menuOpen = false,
}: {
  username: string
  onLogout: () => void
  onMenuClick?: () => void
  menuOpen?: boolean
}) {
  const navigate = useNavigate()
  // The global search (issue #117): the input holds a draft query and fires
  // NOTHING on keystrokes — a search only runs when the form is submitted
  // (Enter in the input, or the dedicated submit button), which navigates to
  // the first-class global results route carrying the query in the URL
  // (?q=…), so results are deep-linkable. The fan-out over the four per-kind
  // endpoints happens on the results page (pages/GlobalSearch.tsx), not here.
  const [q, setQ] = useState('')

  function submit(e: React.FormEvent) {
    e.preventDefault()
    const query = q.trim()
    if (!query) return
    navigate(`${PATHS.globalSearch}?q=${encodeURIComponent(query)}`)
  }

  return (
    <header className="content-header">
      {onMenuClick && (
        <button
          type="button"
          className="content-header-icon"
          title="Menu"
          aria-label="Menu"
          aria-expanded={menuOpen}
          aria-controls="nav-drawer"
          onClick={onMenuClick}
          data-testid="nav-toggle"
        >
          <Bars3Icon aria-hidden className="h-6 w-6" />
        </button>
      )}
      <form
        className="content-search"
        onSubmit={submit}
        role="search"
        data-testid="global-search-form"
      >
        <input
          type="search"
          placeholder="Search TV, movies, music, books…"
          value={q}
          onChange={(e) => setQ(e.target.value)}
          aria-label="Global search"
          data-testid="global-search"
        />
        <button
          type="submit"
          className="content-header-icon"
          title="Search all libraries"
          aria-label="Search"
          disabled={!q.trim()}
          data-testid="global-search-submit"
        >
          <MagnifyingGlassIcon aria-hidden className="h-5 w-5" />
        </button>
      </form>
      <div className="content-header-right">
        {/* The notifications bell is a no-op placeholder (parity with the
            Seerr reference; notification delivery has no issue yet). */}
        <button
          type="button"
          className="content-header-icon"
          title="Notifications"
          aria-label="Notifications"
          data-testid="notifications"
        >
          <BellIcon aria-hidden className="h-5 w-5" />
        </button>
        <UserMenu username={username} onLogout={onLogout} />
      </div>
    </header>
  )
}

// UserMenu is the circular avatar button (issue #148, after Seerr's
// UserDropdown): the trigger shows the admin's initial; opening reveals a
// real menu (role="menu") with the admin identity and the sign-out action.
// Semantics: aria-haspopup/aria-expanded on the trigger, focus moves into
// the menu on open, arrow keys move within the menu, Escape closes and
// returns focus to the trigger, and outside click closes.
export function UserMenu({ username, onLogout }: { username: string; onLogout: () => void }) {
  const [open, setOpen] = useState(false)
  const rootRef = useRef<HTMLDivElement>(null)
  const buttonRef = useRef<HTMLButtonElement>(null)
  const menuRef = useRef<HTMLDivElement>(null)
  const itemRefs = useRef<Array<HTMLButtonElement | null>>([])

  const close = (refocus: boolean) => {
    setOpen(false)
    if (refocus) buttonRef.current?.focus()
  }

  useEffect(() => {
    if (!open) return
    // Focus enters the menu on open (first item).
    itemRefs.current[0]?.focus()

    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        e.preventDefault()
        close(true)
        return
      }
      if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
        // Roving focus: wrap within the menu items.
        e.preventDefault()
        const items = itemRefs.current.filter((el): el is HTMLButtonElement => el != null)
        if (items.length === 0) return
        const idx = items.indexOf(document.activeElement as HTMLButtonElement)
        const delta = e.key === 'ArrowDown' ? 1 : -1
        const next = items.length > 0 ? items[(idx + delta + items.length) % items.length] : null
        next?.focus()
      }
    }
    function onPointerDown(e: PointerEvent) {
      const t = e.target as Node
      if (!rootRef.current?.contains(t)) setOpen(false)
    }
    document.addEventListener('keydown', onKey)
    document.addEventListener('pointerdown', onPointerDown)
    return () => {
      document.removeEventListener('keydown', onKey)
      document.removeEventListener('pointerdown', onPointerDown)
    }
  }, [open])

  const initial = (username.trim()[0] ?? '?').toUpperCase()

  return (
    <div className="user-menu" ref={rootRef}>
      <button
        type="button"
        ref={buttonRef}
        className="user-menu-trigger"
        aria-label={`Menu for ${username || 'user'}`}
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen((o) => !o)}
        data-testid="user-menu"
      >
        <span className="user-menu-avatar" aria-hidden>
          {initial}
        </span>
      </button>
      {open && (
        <div className="user-menu-panel" role="menu" aria-label="User menu" ref={menuRef}>
          <div className="user-menu-id">
            <span className="user-menu-avatar user-menu-avatar-lg" aria-hidden>
              {initial}
            </span>
            <span className="user-menu-name">{username || 'Admin'}</span>
          </div>
          <button
            type="button"
            role="menuitem"
            className="user-menu-item"
            data-testid="logout"
            ref={(el) => {
              itemRefs.current[0] = el
            }}
            onClick={() => {
              close(true)
              onLogout()
            }}
          >
            Sign out
          </button>
        </div>
      )}
    </div>
  )
}

// ---- mobile nav drawer (#123, kept by #148) ----

// NavDrawer is the mobile (≤768px) replacement for the fixed rail (issue
// #123): a fixed overlay holding the SAME Sidebar component (all rail-*
// testids intact), opened from the content-header hamburger. Contract:
// backdrop click closes, Escape closes, selecting a nav item closes (the
// item navigates, and the route change closes the drawer), body scroll is
// locked while open, and focus moves into the drawer on open and back to
// the toggle on close.
export function NavDrawer({ onClose }: { onClose: () => void }) {
  const location = useLocation()
  const panelRef = useRef<HTMLDivElement>(null)

  // A nav selection changes the route while the drawer is open -> close.
  const routeKey = `${location.pathname}${location.search}${location.hash}`
  const lastRoute = useRef(routeKey)
  useEffect(() => {
    if (routeKey !== lastRoute.current) {
      lastRoute.current = routeKey
      onClose()
    }
  }, [routeKey, onClose])

  // Focus moves into the drawer on open; back to the menu toggle on close.
  useEffect(() => {
    panelRef.current?.focus()
    return () => {
      document.querySelector<HTMLElement>('[data-testid="nav-toggle"]')?.focus()
    }
  }, [])

  // Body scroll locked while the drawer is open.
  useEffect(() => {
    const prev = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => {
      document.body.style.overflow = prev
    }
  }, [])

  // Escape closes (document-level so it works from any focus).
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [onClose])

  return (
    <div
      className="drawer-overlay"
      data-testid="nav-drawer-overlay"
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose()
      }}
    >
      <div
        ref={panelRef}
        id="nav-drawer"
        tabIndex={-1}
        className="drawer"
        role="dialog"
        aria-modal="true"
        aria-label="Navigation menu"
        data-testid="nav-drawer"
      >
        {/* The close button carries its own class (the drawer chrome no
            longer borrows the retired topbar-icon style, issue #148). */}
        <div className="drawer-head">
          <button
            type="button"
            className="drawer-close"
            aria-label="Close menu"
            onClick={onClose}
            data-testid="nav-drawer-close"
          >
            <XMarkIcon aria-hidden className="h-5 w-5" />
          </button>
        </div>
        <Sidebar />
      </div>
    </div>
  )
}
