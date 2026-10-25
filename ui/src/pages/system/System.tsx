import { useCallback, useEffect, useState } from 'react'
import { NavLink, useParams } from 'react-router-dom'
import { ApiError, client } from '../../api/client'
import type { DiskRoot, SystemAbout, SystemDisk, SystemHealth, SystemTasks, TaskInfo } from '../../api/types'
import { BackupPanel } from './BackupPanel'
import { EventsPanel } from './EventsPanel'
import { LogsPanel } from './LogsPanel'
import { MigrationPanel } from './MigrationPanel'
import { UpdatesPanel } from './UpdatesPanel'

type SystemPage = 'status' | 'tasks' | 'backup' | 'updates' | 'events' | 'logs' | 'import'

const PAGES: { slug: SystemPage; label: string }[] = [
  { slug: 'status', label: 'Status' },
  { slug: 'tasks', label: 'Tasks' },
  { slug: 'backup', label: 'Backup' },
  { slug: 'updates', label: 'Updates' },
  { slug: 'events', label: 'Events' },
  { slug: 'logs', label: 'Log Files' },
  { slug: 'import', label: 'Sonarr Import' },
]

// System is the system area (route established in P1). P6 wires every
// sub-page to the structured /api/v1/system/* endpoints: Status (health
// table + disk space + About), Tasks (the worker inventory), Backup,
// Updates, Events and Log Files, plus the Sonarr drop-in migration.
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
        {current === 'status' && <StatusPanel />}
        {current === 'tasks' && <TasksPanel />}
        {current === 'backup' && <BackupPanel />}
        {current === 'updates' && <UpdatesPanel />}
        {current === 'events' && <EventsPanel />}
        {current === 'logs' && <LogsPanel />}
        {current === 'import' && <MigrationPanel />}
      </div>
    </div>
  )
}

// errMsg renders an error string, preferring the backend's message for a
// 401 (auth expired) and falling back to a generic line otherwise.
function errMsg(e: unknown, fallback: string): string {
  if (e instanceof ApiError) return e.message
  if (e instanceof Error) return e.message
  return fallback
}

// ---- Status: health table + disk space + About (P6 structured endpoints) ----

function StatusPanel() {
  const [health, setHealth] = useState<SystemHealth | null>(null)
  const [disk, setDisk] = useState<SystemDisk | null>(null)
  const [about, setAbout] = useState<SystemAbout | null>(null)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    try {
      const [h, d, a] = await Promise.all([client.getSystemHealth(), client.getSystemDisk(), client.getSystemAbout()])
      setHealth(h)
      setDisk(d)
      setAbout(a)
      setError(null)
    } catch (e) {
      setError(errMsg(e, 'failed to load system status'))
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
      {health && (
        <div className="card" data-testid="system-health-table">
          <h3 style={{ margin: '0 0 0.5rem' }}>Health</h3>
          {health.checks.length === 0 ? (
            <p className="dim">No health checks reported.</p>
          ) : (
            <div className="list-table">
              <div className="lt-head">
                <span>Check</span>
                <span>Message</span>
                <span>Severity</span>
                <span>Docs</span>
              </div>
              {health.checks.map((c) => (
                <div className="lt-row" key={c.check} data-testid={`health-row-${c.check}`}>
                  <span>{c.check}</span>
                  <span className="lt-dim">{c.message}</span>
                  <span className={`pill health-${c.severity}`}>{c.severity}</span>
                  <span>
                    {c.wiki_link ? (
                      <a href={c.wiki_link} target="_blank" rel="noreferrer" data-testid={`health-link-${c.check}`}>
                        docs
                      </a>
                    ) : (
                      <span className="lt-dim">—</span>
                    )}
                  </span>
                </div>
              ))}
            </div>
          )}
        </div>
      )}
      {disk && (
        <div className="card" style={{ marginTop: '1rem' }} data-testid="system-disk">
          <h3 style={{ margin: '0 0 0.5rem' }}>Disk Space</h3>
          {disk.roots.length === 0 ? (
            <p className="dim">No media roots configured.</p>
          ) : (
            <div className="list-table">
              <div className="lt-head">
                <span>Path</span>
                <span>Used / Total</span>
                <span>Usage</span>
              </div>
              {disk.roots.map((r: DiskRoot) => (
                <DiskRow key={r.path} r={r} />
              ))}
            </div>
          )}
        </div>
      )}
      {about && (
        <div className="card" style={{ marginTop: '1rem' }} data-testid="system-about">
          <h3 style={{ margin: '0 0 0.5rem' }}>About</h3>
          <dl className="about-grid">
            <AboutRow label="Version" value={about.version} />
            <AboutRow label="Commit" value={about.commit} />
            <AboutRow label="Build time" value={about.build_time} />
            <AboutRow label="Go" value={about.go_version} />
            <AboutRow label="Mode" value={about.mode} />
            <AboutRow label="Started" value={formatDateTime(about.started_at)} />
            <AboutRow label="Uptime" value={about.uptime} />
          </dl>
        </div>
      )}
    </div>
  )
}

function DiskRow({ r }: { r: DiskRoot }) {
  const pct = Math.round(r.usage_pct * 100)
  return (
    <div className="lt-row" data-testid={`disk-row-${r.path.replace(/[^a-z0-9]/gi, '')}`}>
      <span title={r.path}>{r.path}</span>
      <span className="lt-dim">
        {r.exists ? `${humanBytes(r.used_bytes)} / ${humanBytes(r.total_bytes)}` : 'not present'}
      </span>
      <span>
        {r.exists ? (
          <span className="bar" data-testid={`disk-bar-${r.path.replace(/[^a-z0-9]/gi, '')}`}>
            <span className="bar-fill" style={{ width: `${pct}%` }} />
            <span className="bar-label">{pct}%</span>
          </span>
        ) : (
          <span className="lt-dim">—</span>
        )}
      </span>
    </div>
  )
}

function AboutRow({ label, value }: { label: string; value?: string }) {
  return (
    <div className="about-row">
      <dt className="lt-dim">{label}</dt>
      <dd>{value || '—'}</dd>
    </div>
  )
}

// ---- Tasks: the worker inventory (running + scheduled + planned) ----

function TasksPanel() {
  const [tasks, setTasks] = useState<SystemTasks | null>(null)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    try {
      setTasks(await client.getSystemTasks())
      setError(null)
    } catch (e) {
      setError(errMsg(e, 'failed to load tasks'))
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  if (error && !tasks) {
    return (
      <div className="card" data-testid="system-tasks-error">
        <h2>Tasks unavailable</h2>
        <p className="error">{error}</p>
        <button onClick={() => void load()}>Retry</button>
      </div>
    )
  }

  return (
    <div className="card" data-testid="system-tasks">
      <h3 style={{ margin: '0 0 0.5rem' }}>
        Tasks
        {tasks && (
          <span className="dim small">
            {' '}
            · {tasks.running} running · {tasks.scheduled} scheduled
          </span>
        )}
      </h3>
      {!tasks || tasks.tasks.length === 0 ? (
        <p className="dim">No tasks registered.</p>
      ) : (
        <div className="list-table">
          <div className="lt-head">
            <span>Task</span>
            <span>Kind</span>
            <span>Schedule</span>
            <span>Last run</span>
            <span>State</span>
          </div>
          {tasks.tasks.map((t: TaskInfo) => (
            <TaskRow key={t.name} t={t} />
          ))}
        </div>
      )}
    </div>
  )
}

function TaskRow({ t }: { t: TaskInfo }) {
  const state = t.running ? 'running' : t.last_error ? 'error' : t.last_run ? 'idle' : 'planned'
  return (
    <div className="lt-row" data-testid={`task-row-${t.name}`}>
      <span title={t.description}>{t.name}</span>
      <span className={`pill task-${t.kind}`}>{t.kind}</span>
      <span className="lt-dim">{t.interval || '—'}</span>
      <span className="lt-dim">{t.last_run ? `${formatDateTime(t.last_run)}${t.last_duration ? ` · ${t.last_duration}` : ''}` : '—'}</span>
      <span>
        <span className={`pill task-state-${state}`}>{state}</span>
        {t.last_error ? <span className="lt-dim" title={t.last_error}> ⚠</span> : null}
      </span>
    </div>
  )
}

// ---- formatting helpers ----

function humanBytes(n: number): string {
  if (!n || n <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']
  const i = Math.min(units.length - 1, Math.floor(Math.log2(n) / 10))
  return `${(n / 2 ** (10 * i)).toFixed(i === 0 ? 0 : 1)} ${units[i]}`
}

function formatDateTime(iso: string): string {
  if (!iso) return '—'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleString()
}

export { PAGES as SYSTEM_PAGES }
