import { useCallback, useEffect, useState } from 'react'
import { NavLink, useParams } from 'react-router-dom'

type SystemPage = 'status' | 'tasks' | 'backup' | 'updates' | 'events' | 'logs'

const PAGES: { slug: SystemPage; label: string }[] = [
  { slug: 'status', label: 'Status' },
  { slug: 'tasks', label: 'Tasks' },
  { slug: 'backup', label: 'Backup' },
  { slug: 'updates', label: 'Updates' },
  { slug: 'events', label: 'Events' },
  { slug: 'logs', label: 'Log Files' },
]

interface Healthz {
  version?: string
  commit?: string
  go_version?: string
  started_at?: string
}

interface ReadyCheck {
  name: string
  ok: boolean
  latency?: string
}

// System is the system area (route established in P1). P1 wires the Status
// page from the public /version, /healthz and /readyz endpoints (no backend
// change); Tasks / Backup / Updates / Events / Log Files ship in P6.
export function SystemPage() {
  const { page = 'status' } = useParams<{ page: string }>()
  return <SystemShell page={page as SystemPage} />
}

// SystemShell renders the system sub-page nav + the active sub-page.
function SystemShell({ page = 'status' }: { page?: SystemPage }) {
  const current: SystemPage = PAGES.some((p) => p.slug === page) ? page : 'status'
  return (
    <div style={{ display: 'grid', gridTemplateColumns: '210px 1fr', gap: '1.2rem', alignItems: 'start' }}>
      <nav className="settings-nav" aria-label="System" data-testid="system-nav">
        {PAGES.map((p) => (
          <NavLink
            key={p.slug}
            to={`/system/${p.slug}`}
            className={p.slug === current ? 'active' : ''}
            data-testid={`system-nav-${p.slug}`}
          >
            {p.label}
          </NavLink>
        ))}
      </nav>
      <div style={{ minWidth: 0 }}>
        {current === 'status' ? <StatusPanel /> : <OtherSystemPanel slug={current} label={PAGES.find((p) => p.slug === current)!.label} />}
      </div>
    </div>
  )
}

function OtherSystemPanel({ slug, label }: { slug: SystemPage; label: string }) {
  return (
    <div className="empty-state" data-testid={`system-empty-${slug}`}>
      <h3>{label}</h3>
      <p>{label} lands with the System + migration build (P6).</p>
    </div>
  )
}

function StatusPanel() {
  const [version, setVersion] = useState('')
  const [health, setHealth] = useState<Healthz | null>(null)
  const [checks, setChecks] = useState<ReadyCheck[]>([])
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    try {
      const [v, h, r] = await Promise.all([
        fetch('/version').then((x) => x.json()).catch(() => ({ version: '' })),
        fetch('/healthz').then((x) => x.json()).catch(() => null),
        fetch('/readyz').then((x) => x.json()).catch(() => null),
      ])
      setVersion((v as { version?: string }).version ?? '')
      setHealth(h as Healthz)
      setChecks(((r as { checks?: ReadyCheck[] } | null)?.checks ?? []))
      setError(null)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'failed to load status')
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  if (error && !health) {
    return (
      <div className="card" data-testid="system-status-error">
        <h2>Status unavailable</h2>
        <p className="error">{error}</p>
        <button onClick={() => void load()}>Retry</button>
      </div>
    )
  }

  return (
    <div data-testid="system-status">
      <div className="card" style={{ marginBottom: '1rem' }}>
        <h3 style={{ margin: '0 0 0.5rem' }}>About</h3>
        <p className="dim small" style={{ margin: 0 }}>
          mediarr {version || (health?.version ?? '')} · {health?.go_version ?? ''}
          {health?.commit ? ` · ${health.commit}` : ''}
        </p>
      </div>
      <div className="card" data-testid="system-health-table">
        <h3 style={{ margin: '0 0 0.5rem' }}>Health</h3>
        {checks.length === 0 ? (
          <p className="dim">No health checks reported.</p>
        ) : (
          <div className="list-table">
            <div className="lt-head">
              <span>Check</span>
              <span>Status</span>
              <span>Latency</span>
            </div>
            {checks.map((c) => (
              <div className="lt-row" key={c.name}>
                <span>{c.name}</span>
                <span className={`pill ${c.ok ? 'pill-ok' : 'pill-err'}`}>{c.ok ? 'ok' : 'unavailable'}</span>
                <span className="lt-dim">{c.latency ?? '—'}</span>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}

export { PAGES as SYSTEM_PAGES }
