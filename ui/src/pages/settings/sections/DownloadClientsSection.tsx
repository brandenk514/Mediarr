import { useCallback, useEffect, useState } from 'react'
import { ApiError, client } from '../../../api/client'
import type {
  DownloadClient,
  DownloadClientCreate,
  LibraryOverride,
  LibraryOverrides,
  MediaKind,
} from '../../../api/types'
import { Modal } from '../../../components/ui'
import { SaveError, SectionCard, SectionError, useSubmit } from './shared'

// DownloadClientsSection is the Prowlarr PR5 "Download Clients" section of the
// P5 settings shell: the MULTI-client download-client list (one row per saved
// client: name, kind, enabled, last test) with add/edit/test/delete + Test All
// Clients, plus the per-library overrides (paths, quality profile, checksum
// opt-out). It is one of the two sections in the shell that keep their own
// dedicated endpoints rather than the generic per-section document.
//
// Secrets are masked on read: the server never returns a stored key (api_key is
// always "" and api_key_set reports presence). A blank key field on edit keeps
// the stored key (the masked-edit round-trip).
const EXPLAINER =
  'If you intend to do searches directly within Mediarr, you need to add Download ' +
  'Clients. For searches from your Apps, the download clients configured there are ' +
  'used instead. Download clients are for in-app searches only and do not sync to ' +
  'other apps.'

const CLIENT_KINDS = ['qbittorrent', 'sabnzbd', 'transmission'] as const
type ClientKind = (typeof CLIENT_KINDS)[number]

export function DownloadClientsSection() {
  const [authLost, setAuthLost] = useState(false)
  const [fatal, setFatal] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [clients, setClients] = useState<DownloadClient[]>([])
  const [libraries, setLibraries] = useState<LibraryOverrides | null>(null)
  // Per-client last test outcome: 'busy' | 'ok' | a failure message.
  const [testState, setTestState] = useState<Record<number, string>>({})
  const [testAllBusy, setTestAllBusy] = useState(false)

  const load = useCallback(() => {
    let cancelled = false
    setLoading(true)
    setFatal(null)
    setAuthLost(false)
    Promise.all([client.listDownloadClients(), client.getLibraryOverrides()])
      .then(([list, libs]) => {
        if (cancelled) return
        setClients(list)
        setLibraries(libs)
      })
      .catch((e: unknown) => {
        if (cancelled) return
        if (e instanceof ApiError && e.status === 401) setAuthLost(true)
        setFatal(e instanceof Error ? e.message : 'failed to load download clients')
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [])

  useEffect(() => {
    const cleanup = load()
    return cleanup
  }, [load])

  // ---- client form (add / edit) ----
  const [formOpen, setFormOpen] = useState(false)
  const [editingId, setEditingId] = useState<number | null>(null)
  const [name, setName] = useState('')
  const [kind, setKind] = useState<ClientKind>('qbittorrent')
  const [enabled, setEnabled] = useState(true)
  const [baseUrl, setBaseUrl] = useState('')
  const [username, setUsername] = useState('')
  const [apiKey, setApiKey] = useState('')
  const [completeDir, setCompleteDir] = useState('')
  // Whether the client being edited already has a stored key (so the field can
  // read "set — blank to keep" instead of "blank = keyless").
  const [editingHasKey, setEditingHasKey] = useState(false)
  const { busy: saving, error: saveError, submit: saveSubmit } = useSubmit()

  function openCreate() {
    setEditingId(null)
    setName('')
    setKind('qbittorrent')
    setEnabled(true)
    setBaseUrl('')
    setUsername('')
    setApiKey('')
    setCompleteDir('')
    setEditingHasKey(false)
    setFormOpen(true)
  }

  function openEdit(c: DownloadClient) {
    setEditingId(c.id)
    setName(c.name)
    setKind(c.kind)
    setEnabled(c.enabled)
    setBaseUrl(c.base_url)
    setUsername(c.username)
    setApiKey('') // blank = keep the stored key (masked round-trip)
    setCompleteDir(c.complete_dir)
    setEditingHasKey(c.api_key_set)
    setFormOpen(true)
  }

  function closeForm() {
    setFormOpen(false)
  }

  function handleSave() {
    void saveSubmit(async () => {
      const body: DownloadClientCreate = {
        name: name.trim(),
        kind,
        enabled,
        base_url: baseUrl,
        username,
        api_key: apiKey, // empty = keyless on create / keep stored on edit
        complete_dir: completeDir,
      }
      if (editingId != null) await client.updateDownloadClient(editingId, body)
      else await client.createDownloadClient(body)
      setFormOpen(false)
      load()
    })
  }

  async function handleTest(id: number) {
    setTestState((prev) => ({ ...prev, [id]: 'busy' }))
    try {
      const res = await client.testDownloadClient(id)
      setTestState((prev) => ({ ...prev, [id]: res.ok ? 'ok' : res.message || 'failed' }))
    } catch (e: unknown) {
      setTestState((prev) => ({ ...prev, [id]: e instanceof Error ? e.message : 'test failed' }))
    }
  }

  async function handleTestAll() {
    if (clients.length === 0) return
    setTestAllBusy(true)
    try {
      const results = await client.testAllDownloadClients()
      setTestState((prev) => {
        const next = { ...prev }
        for (const r of results) next[r.id] = r.ok ? 'ok' : r.message || 'failed'
        return next
      })
    } catch (e: unknown) {
      setTestAllBusy(false)
      setFatal(e instanceof Error ? e.message : 'test all failed')
      return
    } finally {
      setTestAllBusy(false)
    }
  }

  async function handleDelete(id: number) {
    await client.deleteDownloadClient(id)
    load()
  }

  if (fatal) {
    return <SectionError message={fatal} authLost={authLost} onRetry={load} testId="dc-error" />
  }

  return (
    <>
      <SectionCard title="Download Clients" hint={EXPLAINER} testId="dc-card">
        <div
          style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}
          data-testid="dcl-toolbar"
        >
          <button
            className="ghost"
            onClick={() => void handleTestAll()}
            disabled={testAllBusy || loading || clients.length === 0}
            data-testid="dcl-test-all"
          >
            {testAllBusy ? 'Testing…' : 'Test All Clients'}
          </button>
          <button onClick={openCreate} data-testid="dcl-add">
            + Add client
          </button>
        </div>

        {loading ? (
          <p className="dim">Loading download clients…</p>
        ) : clients.length === 0 ? (
          <div className="empty-state" data-testid="dc-empty">
            <h3>No download clients</h3>
            <p>Add a download client so in-app searches can route grabs to it.</p>
            <button onClick={openCreate} data-testid="dc-empty-add">
              + Add client
            </button>
          </div>
        ) : (
          <table className="table" data-testid="dcl-table">
            <thead>
              <tr>
                <th>Name</th>
                <th>Kind</th>
                <th>Enabled</th>
                <th>Last test</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {clients.map((c) => (
                <tr key={c.id} data-testid={`dcl-row-${c.id}`}>
                  <td data-testid={`dcl-name-${c.id}`}>{c.name}</td>
                  <td data-testid={`dcl-kind-${c.id}`}>{c.kind}</td>
                  <td data-testid={`dcl-enabled-${c.id}`}>
                    <span className={c.enabled ? 'pill pill-ok' : 'pill'}>
                      {c.enabled ? 'Enabled' : 'Disabled'}
                    </span>
                  </td>
                  <td data-testid={`dcl-last-test-${c.id}`}>
                    {testState[c.id] ? (
                      <span className={testState[c.id] === 'ok' ? 'pill pill-ok' : 'error small'}>
                        {testState[c.id] === 'busy' ? 'Testing…' : testState[c.id]}
                      </span>
                    ) : (
                      '—'
                    )}
                  </td>
                  <td>
                    <button
                      className="ghost small"
                      onClick={() => void handleTest(c.id)}
                      disabled={testState[c.id] === 'busy'}
                      data-testid={`dcl-test-${c.id}`}
                    >
                      Test
                    </button>
                    <button className="ghost small" onClick={() => openEdit(c)} data-testid={`dcl-edit-${c.id}`}>
                      Edit
                    </button>
                    <button
                      className="ghost small danger"
                      onClick={() => void handleDelete(c.id)}
                      data-testid={`dcl-del-${c.id}`}
                    >
                      Delete
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </SectionCard>

      <LibraryOverridesPane overrides={libraries} onSaved={setLibraries} disabled={loading} />

      {formOpen && (
        <Modal
          title={editingId != null ? 'Edit download client' : 'Add download client'}
          onClose={closeForm}
          testId="dcl-modal"
          footer={
            <>
              <button onClick={handleSave} disabled={saving || !name.trim()} data-testid="dcl-save">
                {saving ? 'Saving…' : editingId != null ? 'Save changes' : 'Add client'}
              </button>
              <button className="ghost" onClick={closeForm} data-testid="dcl-cancel">
                Cancel
              </button>
            </>
          }
        >
          <label htmlFor="dcl-name">Name</label>
          <input
            id="dcl-name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="e.g. qBittorrent"
            data-testid="dcl-f-name"
          />

          <label htmlFor="dcl-kind">Kind</label>
          <select
            id="dcl-kind"
            value={kind}
            onChange={(e) => setKind(e.target.value as ClientKind)}
            data-testid="dcl-f-kind"
          >
            {CLIENT_KINDS.map((k) => (
              <option key={k} value={k}>
                {k}
              </option>
            ))}
          </select>

          <label htmlFor="dcl-enabled" className="check">
            <input
              id="dcl-enabled"
              type="checkbox"
              checked={enabled}
              onChange={(e) => setEnabled(e.target.checked)}
              data-testid="dcl-f-enabled"
            />
            Enabled
          </label>

          <label htmlFor="dcl-base">Base URL</label>
          <input
            id="dcl-base"
            value={baseUrl}
            onChange={(e) => setBaseUrl(e.target.value)}
            placeholder={kind === 'sabnzbd' ? 'http://localhost:11111' : 'http://localhost:8080'}
            data-testid="dcl-f-base"
          />

          <label htmlFor="dcl-user">Username</label>
          <input
            id="dcl-user"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            data-testid="dcl-f-user"
          />

          <label htmlFor="dcl-key">
            API key {editingId != null && editingHasKey ? '(set — blank to keep)' : '(optional)'}
          </label>
          <input
            id="dcl-key"
            type="password"
            value={apiKey}
            onChange={(e) => setApiKey(e.target.value)}
            placeholder={editingId != null && editingHasKey ? '••••••••' : ''}
            data-testid="dcl-f-key"
          />

          <label htmlFor="dcl-dir">Complete directory (optional)</label>
          <input
            id="dcl-dir"
            value={completeDir}
            onChange={(e) => setCompleteDir(e.target.value)}
            placeholder="blank = the server downloads dir"
            data-testid="dcl-f-dir"
          />

          <SaveError error={saveError} testId="dcl-form-error" />
        </Modal>
      )}
    </>
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
          <LibraryColumn
            key={k}
            kind={k}
            override={overrides[k]}
            overrides={overrides}
            onSaved={onSaved}
            disabled={disabled}
          />
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
