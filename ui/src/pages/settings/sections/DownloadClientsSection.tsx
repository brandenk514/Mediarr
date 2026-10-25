import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { ApiError, client } from '../../../api/client'
import type {
  DownloadClientView,
  LibraryOverride,
  LibraryOverrides,
  MediaKind,
} from '../../../api/types'
import { SaveError, useSubmit } from './shared'

// DownloadClientsSection is the download-clients section of the P5 settings
// shell: the download-client configuration (masked secrets — the UI never sees
// a stored key, only a "set" indicator and a field to replace it) plus the
// per-library overrides (paths, quality profile, checksum opt-out). Both ride
// the dedicated settings endpoints (one of the two sections in the shell that
// keep their own endpoints rather than the generic per-section document).
export function DownloadClientsSection() {
  const [authLost, setAuthLost] = useState(false)
  const [fatal, setFatal] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [refreshKey, setRefreshKey] = useState(0)
  const [clientView, setClientView] = useState<DownloadClientView | null>(null)
  const [libraries, setLibraries] = useState<LibraryOverrides | null>(null)

  // Load both settings documents.
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
        <h3>Download clients unavailable</h3>
        <p className="error" data-testid="settings-error-message">
          {fatal}
        </p>
        {authLost && (
          <p>
            <Link to="/login">Sign in again</Link>
          </p>
        )}
        <button onClick={() => setRefreshKey((k) => k + 1)} data-testid="settings-refresh">
          Retry
        </button>
      </div>
    )
  }

  return (
    <>
      <div className="dash-head-right" style={{ justifyContent: 'flex-end' }}>
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
      <DownloadClientPane view={clientView} onSaved={setClientView} disabled={loading} />
      <LibraryOverridesPane overrides={libraries} onSaved={setLibraries} disabled={loading} />
    </>
  )
}

// ClientKinds mirrors the server's fixed set (mock, qbittorrent, sabnzbd).
const CLIENT_KINDS = ['mock', 'qbittorrent', 'sabnzbd'] as const

function DownloadClientPane({
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

function LibraryOverridesPane({
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
