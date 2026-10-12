import { useCallback, useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { ApiError, client } from '../../api/client'
import type {
  DownloadClientView,
  Indexer,
  IndexerTestResult,
  LibraryOverride,
  LibraryOverrides,
  MediaKind,
} from '../../api/types'

// SettingsPage is the settings view (#44): the download-client configuration
// (masked secrets — the UI never sees a stored key, only a "set" indicator and a
// field to replace it), the per-library overrides (paths, quality profile,
// checksum opt-out), and the indexer management surface (create / update /
// enable-toggle / delete + on-demand test). Everything is loaded once on mount
// and saved on demand; there is no live stream here (settings don't push).
export function SettingsPage() {
  const [authLost, setAuthLost] = useState(false)
  const [fatal, setFatal] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [refreshKey, setRefreshKey] = useState(0)
  const [clientView, setClientView] = useState<DownloadClientView | null>(null)
  const [libraries, setLibraries] = useState<LibraryOverrides | null>(null)

  // Load both settings documents. Indexers are loaded in their own section so a
  // failure there doesn't blank the page.
  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setFatal(null)
    setAuthLost(false)
    Promise.all([client.getDownloadClient(), client.getLibraryOverrides()])
      .then(([dc, libs]) => {
        if (cancelled) return
        setClientView(dc)
        setLibraries(libs)
      })
      .catch((e: unknown) => {
        if (cancelled) return
        if (e instanceof ApiError && e.status === 401) setAuthLost(true)
        setFatal(e instanceof Error ? e.message : 'failed to load settings')
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [refreshKey])

  if (fatal) {
    return (
      <div className="card" data-testid="settings-error">
        <h2>Settings unavailable</h2>
        <p className="error" data-testid="settings-error-message">
          {fatal}
        </p>
        {authLost && (
          <p>
            <Link to="/login">Sign in again</Link>
          </p>
        )}
        <button onClick={() => setRefreshKey((k) => k + 1)}>Retry</button>
      </div>
    )
  }

  return (
    <div className="list-page settings">
      <div className="dash-head">
        <h2 data-testid="settings-title">Settings</h2>
        <div className="dash-head-right">
          {loading && <span className="dim small">Loading…</span>}
          <button
            className="ghost"
            onClick={() => setRefreshKey((k) => k + 1)}
            disabled={loading}
            data-testid="settings-refresh"
          >
            Refresh
          </button>
        </div>
      </div>

      <DownloadClientSection
        view={clientView}
        onSaved={setClientView}
        disabled={loading}
      />
      <LibraryOverridesSection
        overrides={libraries}
        onSaved={setLibraries}
        disabled={loading}
      />
      <IndexersSection disabled={loading} />
    </div>
  )
}

// ---------------------------------------------------------------------------
// Download client
// ---------------------------------------------------------------------------

// ClientKinds mirrors the server's fixed set (mock, qbittorrent, sabnzbd).
const CLIENT_KINDS = ['mock', 'qbittorrent', 'sabnzbd'] as const

function DownloadClientSection({
  view,
  onSaved,
  disabled,
}: {
  view: DownloadClientView | null
  onSaved: (v: DownloadClientView) => void
  disabled: boolean
}) {
  const [kind, setKind] = useState('mock')
  const [qbBase, setQbBase] = useState('')
  const [qbUsername, setQbUsername] = useState('')
  const [qbApiKey, setQbApiKey] = useState('')
  const [qbPassword, setQbPassword] = useState('')
  const [sabBase, setSabBase] = useState('')
  const [sabApiKey, setSabApiKey] = useState('')
  const [sabCompleteDir, setSabCompleteDir] = useState('')

  // Seed the form from the masked view once it arrives. The secrets are
  // *cleared* locally (we never hold them) — a placeholder shows a stored key
  // can be left blank to keep it.
  useEffect(() => {
    if (!view) return
    setKind(view.kind || 'mock')
    setQbBase(view.qb_base)
    setQbUsername(view.qb_username)
    setQbApiKey('')
    setQbPassword('')
    setSabBase(view.sab_base)
    setSabApiKey('')
    setSabCompleteDir(view.sab_complete_dir)
  }, [view])

  const { busy, error, submit } = useSubmit()
  const [saved, setSaved] = useState(false)

  function handleSave() {
    void submit(async () => {
      const req: Record<string, string> = { kind }
      if (kind === 'qbittorrent') {
        req.qb_base = qbBase
        req.qb_username = qbUsername
        req.qb_api_key = qbApiKey // empty = keep the stored key
        req.qb_password = qbPassword
      }
      if (kind === 'sabnzbd') {
        req.sab_base = sabBase
        req.sab_api_key = sabApiKey
        req.sab_complete_dir = sabCompleteDir
      }
      const updated = await client.putDownloadClient(req as never)
      onSaved(updated)
      setQbApiKey('')
      setQbPassword('')
      setSabApiKey('')
      setSaved(true)
      setTimeout(() => setSaved(false), 2000)
    })
  }

  return (
    <div className="card" data-testid="dc-card">
      <h3>Download client</h3>
      <p className="dim small">
        How downloads are fetched and imported. Secrets are encrypted at rest and never
        returned by the server — a blank key field keeps the stored key.
      </p>

      <label htmlFor="dc-kind">Client</label>
      <select id="dc-kind" value={kind} onChange={(e) => setKind(e.target.value)} data-testid="dc-kind">
        {CLIENT_KINDS.map((k) => (
          <option key={k} value={k}>
            {k}
          </option>
        ))}
      </select>

      {kind === 'qbittorrent' && (
        <div className="subform" data-testid="dc-qbit">
          <label htmlFor="dc-qb-base">Base URL</label>
          <input
            id="dc-qb-base"
            value={qbBase}
            onChange={(e) => setQbBase(e.target.value)}
            placeholder="http://localhost:8080"
            data-testid="dc-qb-base"
          />
          <label htmlFor="dc-qb-user">Username (optional if API key)</label>
          <input
            id="dc-qb-user"
            value={qbUsername}
            onChange={(e) => setQbUsername(e.target.value)}
            data-testid="dc-qb-user"
          />
          <label htmlFor="dc-qb-key">
            API key {view?.qb_api_key_set ? '(stored — blank to keep)' : '(optional)'}
          </label>
          <input
            id="dc-qb-key"
            type="password"
            value={qbApiKey}
            onChange={(e) => setQbApiKey(e.target.value)}
            placeholder={view?.qb_api_key_set ? '••••••••' : ''}
            data-testid="dc-qb-key"
          />
          <label htmlFor="dc-qb-pass">
            Password {view?.qb_password_set ? '(stored — blank to keep)' : ''}
          </label>
          <input
            id="dc-qb-pass"
            type="password"
            value={qbPassword}
            onChange={(e) => setQbPassword(e.target.value)}
            placeholder={view?.qb_password_set ? '••••••••' : ''}
            data-testid="dc-qb-pass"
          />
        </div>
      )}

      {kind === 'sabnzbd' && (
        <div className="subform" data-testid="dc-sab">
          <label htmlFor="dc-sab-base">Base URL</label>
          <input
            id="dc-sab-base"
            value={sabBase}
            onChange={(e) => setSabBase(e.target.value)}
            placeholder="http://localhost:11111"
            data-testid="dc-sab-base"
          />
          <label htmlFor="dc-sab-key">
            API key {view?.sab_api_key_set ? '(stored — blank to keep)' : ''}
          </label>
          <input
            id="dc-sab-key"
            type="password"
            value={sabApiKey}
            onChange={(e) => setSabApiKey(e.target.value)}
            placeholder={view?.sab_api_key_set ? '••••••••' : ''}
            data-testid="dc-sab-key"
          />
          <label htmlFor="dc-sab-dir">Complete directory (optional)</label>
          <input
            id="dc-sab-dir"
            value={sabCompleteDir}
            onChange={(e) => setSabCompleteDir(e.target.value)}
            data-testid="dc-sab-dir"
          />
        </div>
      )}

      {saved && (
        <p className="pill pill-ok" data-testid="dc-saved">
          Saved
        </p>
      )}
      <SaveError error={error} testId="dc-error" />
      <button onClick={handleSave} disabled={busy || disabled} data-testid="dc-save">
        {busy ? 'Saving…' : 'Save download client'}
      </button>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Per-library overrides
// ---------------------------------------------------------------------------

const LIBRARY_KINDS: MediaKind[] = ['movie', 'tv', 'music', 'book']

function LibraryOverridesSection({
  overrides,
  onSaved,
  disabled,
}: {
  overrides: LibraryOverrides | null
  onSaved: (o: LibraryOverrides) => void
  disabled: boolean
}) {
  if (!overrides) return null
  return (
    <div className="card" data-testid="lib-card">
      <h3>Library overrides</h3>
      <p className="dim small">
        Per-library settings. Blank fields fall back to the global default. Checksum
        verify is ON by default — opt out per library if you want a faster import.
      </p>
      <div className="lib-grid">
        {LIBRARY_KINDS.map((k) => (
          <LibraryColumn key={k} kind={k} override={overrides[k]} onSaved={onSaved} overrides={overrides} disabled={disabled} />
        ))}
      </div>
    </div>
  )
}

function LibraryColumn({
  kind,
  override,
  overrides,
  onSaved,
  disabled,
}: {
  kind: MediaKind
  override: LibraryOverride
  overrides: LibraryOverrides
  onSaved: (o: LibraryOverrides) => void
  disabled: boolean
}) {
  const [path, setPath] = useState(override.path)
  const [quality, setQuality] = useState(override.quality_profile)
  // Checksum tristate: '' = default (null), 'true' = verify, 'false' = skip.
  const [checksum, setChecksum] = useState<'' | 'true' | 'false'>('')
  const { busy, error, submit } = useSubmit()
  const [saved, setSaved] = useState(false)

  useEffect(() => {
    setPath(override.path)
    setQuality(override.quality_profile)
    setChecksum(
      override.checksum_verify === null || override.checksum_verify === undefined
        ? ''
        : override.checksum_verify
          ? 'true'
          : 'false',
    )
  }, [override])

  const prefix = `lib-${kind}`
  function handleSave() {
    void submit(async () => {
      const next: LibraryOverride = {
        path,
        quality_profile: quality,
        checksum_verify: checksum === '' ? null : checksum === 'true',
      }
      await client.putLibraryOverride(kind, next)
      onSaved({ ...overrides, [kind]: next })
      setSaved(true)
      setTimeout(() => setSaved(false), 2000)
    })
  }

  return (
    <div className="lib-col" data-testid={`${prefix}-col`}>
      <h4>{kind}</h4>
      <label htmlFor={`${prefix}-path`}>Path</label>
      <input
        id={`${prefix}-path`}
        value={path}
        onChange={(e) => setPath(e.target.value)}
        placeholder="default"
        data-testid={`${prefix}-path`}
      />
      <label htmlFor={`${prefix}-quality`}>Quality profile</label>
      <input
        id={`${prefix}-quality`}
        value={quality}
        onChange={(e) => setQuality(e.target.value)}
        placeholder="default"
        data-testid={`${prefix}-quality`}
      />
      <label htmlFor={`${prefix}-checksum`}>Checksum verify</label>
      <select
        id={`${prefix}-checksum`}
        value={checksum}
        onChange={(e) => setChecksum(e.target.value as '' | 'true' | 'false')}
        data-testid={`${prefix}-checksum`}
      >
        <option value="">Default (verify)</option>
        <option value="true">Verify</option>
        <option value="false">Skip</option>
      </select>
      {saved && (
        <p className="pill pill-ok" data-testid={`${prefix}-saved`}>
          Saved
        </p>
      )}
      <SaveError error={error} testId={`${prefix}-error`} />
      <button className="ghost" onClick={handleSave} disabled={busy || disabled} data-testid={`${prefix}-save`}>
        {busy ? 'Saving…' : 'Save'}
      </button>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Indexers
// ---------------------------------------------------------------------------

// INDEXER_KINDS mirrors the server's provider registry (the write endpoints
// validate a definition's kind against it, so only these can be saved).
const INDEXER_KINDS = ['fake', 'torznab', 'torrent-rss', 'nzb'] as const

function IndexersSection({ disabled }: { disabled: boolean }) {
  const [indexers, setIndexers] = useState<Indexer[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const reqId = useRef(0)

  const load = useCallback(async () => {
    const id = ++reqId.current
    setLoading(true)
    try {
      const list = await client.listIndexers()
      if (id !== reqId.current) return
      setIndexers(list)
      setError(null)
    } catch (e) {
      if (id !== reqId.current) return
      setError(e instanceof Error ? e.message : 'failed to load indexers')
    } finally {
      if (id === reqId.current) setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const [showForm, setShowForm] = useState(false)
  const [editingId, setEditingId] = useState<number | null>(null)

  return (
    <div className="card" data-testid="idx-card">
      <div className="row-head">
        <h3>Indexers</h3>
        <div className="dash-head-right">
          {!showForm && (
            <button className="ghost" onClick={() => setShowForm(true)} disabled={disabled} data-testid="idx-add">
              + Add indexer
            </button>
          )}
          <button className="ghost" onClick={() => void load()} disabled={loading} data-testid="idx-refresh">
            Refresh
          </button>
        </div>
      </div>

      {error && <SaveError error={error} testId="idx-error" />}

      {showForm && (
        <IndexerForm
          editing={editingId != null ? indexers?.find((i) => i.id === editingId) ?? null : null}
          onCancel={() => {
            setShowForm(false)
            setEditingId(null)
          }}
          onSaved={(idx) => {
            setIndexers((prev) => {
              if (!prev) return [idx]
              const i = prev.findIndex((p) => p.id === idx.id)
              if (i === -1) return [...prev, idx]
              const next = [...prev]
              next[i] = idx
              return next
            })
            setShowForm(false)
            setEditingId(null)
            void load()
          }}
        />
      )}

      {indexers && indexers.length > 0 && (
        <div className="idx-table" data-testid="idx-table">
          <div className="lt-row idx-head-row">
            <span>Name</span>
            <span>Kind</span>
            <span>Base URL</span>
            <span>API key</span>
            <span>Enabled</span>
            <span>Last test</span>
            <span>Actions</span>
          </div>
          {indexers.map((idx) => (
            <IndexerRow
              key={idx.id}
              indexer={idx}
              onTested={load}
              onChange={load}
              onEdit={() => {
                setEditingId(idx.id)
                setShowForm(true)
              }}
              onDelete={load}
            />
          ))}
        </div>
      )}
      {indexers && indexers.length === 0 && !error && (
        <p className="dim small" data-testid="idx-empty">
          No indexers configured. Add one to enable search.
        </p>
      )}
    </div>
  )
}

function IndexerRow({
  indexer,
  onTested,
  onChange,
  onEdit,
  onDelete,
}: {
  indexer: Indexer
  onTested: () => void
  onChange: () => void
  onEdit: () => void
  onDelete: () => void
}) {
  const [testResult, setTestResult] = useState<IndexerTestResult | null>(null)
  const [testBusy, setTestBusy] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const { busy: toggling, submit: toggleSubmit } = useSubmit()
  const { busy: deleting, submit: deleteSubmit } = useSubmit()

  async function handleTest() {
    setTestBusy(true)
    try {
      const res = await client.testIndexer(indexer.id)
      setTestResult(res)
      onTested()
    } finally {
      setTestBusy(false)
    }
  }

  function handleToggle() {
    void toggleSubmit(async () => {
      await client.setIndexerEnabled(indexer.id, !indexer.enabled)
      onChange()
    })
  }

  function handleDelete() {
    if (!confirmDelete) {
      setConfirmDelete(true)
      setTimeout(() => setConfirmDelete(false), 3000)
      return
    }
    void deleteSubmit(async () => {
      await client.deleteIndexer(indexer.id)
      onDelete()
    })
  }

  return (
    <div className="lt-row" data-testid={`idx-row-${indexer.id}`}>
      <span className="lt-name">{indexer.name}</span>
      <span>{indexer.kind}</span>
      <span className="dim small">{indexer.base_url}</span>
      <span>{indexer.has_api_key ? <span className="pill pill-idle">set</span> : <span className="dim">—</span>}</span>
      <span>
        <button
          className="ghost small"
          onClick={handleToggle}
          disabled={toggling}
          data-testid={`idx-toggle-${indexer.id}`}
        >
          {indexer.enabled ? 'Enabled' : 'Disabled'}
        </button>
      </span>
      <span className="dim small">
        {indexer.last_test ? indexer.last_test.slice(0, 19).replace('T', ' ') : '—'}
        {testResult && (
          <span className={testResult.ok ? ' pill pill-ok' : ' pill pill-err'}>
            {testResult.ok ? 'ok' : 'fail'}
          </span>
        )}
      </span>
      <span className="idx-actions">
        <button className="ghost small" onClick={() => void handleTest()} disabled={testBusy} data-testid={`idx-test-${indexer.id}`}>
          {testBusy ? 'Testing…' : 'Test'}
        </button>
        <button className="ghost small" onClick={onEdit} data-testid={`idx-edit-${indexer.id}`}>
          Edit
        </button>
        <button className="ghost small danger" onClick={handleDelete} disabled={deleting} data-testid={`idx-delete-${indexer.id}`}>
          {confirmDelete ? 'Confirm?' : 'Delete'}
        </button>
      </span>
    </div>
  )
}

function IndexerForm({
  editing,
  onSaved,
  onCancel,
}: {
  editing: Indexer | null
  onSaved: (idx: Indexer) => void
  onCancel: () => void
}) {
  const [name, setName] = useState(editing?.name ?? '')
  const [kind, setKind] = useState<string>(editing?.kind ?? 'fake')
  const [baseUrl, setBaseUrl] = useState(editing?.base_url ?? '')
  const [apiKey, setApiKey] = useState('')
  const [enabled, setEnabled] = useState(editing?.enabled ?? true)
  const { busy, error, submit } = useSubmit()

  function handleSave() {
    void submit(async () => {
      const req = {
        name: name.trim(),
        kind,
        base_url: baseUrl,
        api_key: apiKey, // empty on update = keep the stored key
        ...(editing ? {} : { enabled }),
      }
      const idx = editing
        ? await client.updateIndexer(editing.id, req as never)
        : await client.createIndexer(req as never)
      onSaved(idx)
    })
  }

  return (
    <div className="subform" data-testid="idx-form">
      <h4>{editing ? `Edit ${editing.name}` : 'New indexer'}</h4>
      <label htmlFor="idx-name">Name</label>
      <input
        id="idx-name"
        value={name}
        onChange={(e) => setName(e.target.value)}
        placeholder="e.g. nyaa"
        data-testid="idx-name"
      />
      <label htmlFor="idx-kind">Kind</label>
      <select id="idx-kind" value={kind} onChange={(e) => setKind(e.target.value)} data-testid="idx-kind">
        {INDEXER_KINDS.map((k) => (
          <option key={k} value={k}>
            {k}
          </option>
        ))}
      </select>
      <label htmlFor="idx-base">Base URL</label>
      <input
        id="idx-base"
        value={baseUrl}
        onChange={(e) => setBaseUrl(e.target.value)}
        placeholder="https://…/api"
        data-testid="idx-base"
      />
      <label htmlFor="idx-key">
        API key {editing ? '(blank to keep)' : ''}
      </label>
      <input
        id="idx-key"
        type="password"
        value={apiKey}
        onChange={(e) => setApiKey(e.target.value)}
        placeholder={editing && editing.has_api_key ? '••••••••' : ''}
        data-testid="idx-key"
      />
      {!editing && (
        <>
          <label className="check" htmlFor="idx-enabled">
            <input
              id="idx-enabled"
              type="checkbox"
              checked={enabled}
              onChange={(e) => setEnabled(e.target.checked)}
              data-testid="idx-enabled"
            />
            Enabled
          </label>
        </>
      )}
      <SaveError error={error} testId="idx-form-error" />
      <div className="idx-form-actions">
        <button onClick={handleSave} disabled={busy} data-testid="idx-save">
          {busy ? 'Saving…' : editing ? 'Save changes' : 'Add indexer'}
        </button>
        <button className="ghost" onClick={onCancel} data-testid="idx-cancel">
          Cancel
        </button>
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Small shared helpers
// ---------------------------------------------------------------------------

// useSubmit is a local busy/error wrapper around an async action (mirrors the
// add-flow useAddSubmit but does not re-throw — settings save errors are shown
// inline, not surfaced to a caller).
function useSubmit() {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const submit = useCallback(
    async (run: () => Promise<void>) => {
      if (busy) return
      setBusy(true)
      setError('')
      try {
        await run()
      } catch (e: unknown) {
        setError(e instanceof Error ? e.message : 'save failed')
      } finally {
        setBusy(false)
      }
    },
    [busy],
  )

  return { busy, error, submit }
}

function SaveError({ error, testId }: { error: string; testId: string }) {
  if (!error) return null
  return (
    <p className="error" role="alert" data-testid={testId}>
      {error}
    </p>
  )
}
