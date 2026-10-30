import { useCallback, useEffect, useState } from 'react'
import { ApiError, client } from '../../../api/client'
import type { NotificationSink } from '../../../api/types'
import { SaveError, SectionCard, SectionError, useSubmit } from './shared'

// Prowlarr PR4: the sink kinds the server accepts.
const SINK_KINDS: NotificationSink['kind'][] = ['webhook', 'plex', 'jellyfin', 'emby']

// NotificationsSection is the Prowlarr PR4 Notifications section: the saved
// notification sinks (webhook / Plex / Jellyfin / Emby) with CRUD, a per-sink
// Test, and the masked token (presence only — a blank token in the form keeps
// the stored one, the same convention as indexer passwords).
export function NotificationsSection() {
  const [authLost, setAuthLost] = useState(false)
  const [fatal, setFatal] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [sinks, setSinks] = useState<NotificationSink[]>([])
  // Per-sink test results: 'busy' | 'ok' | an error message.
  const [testState, setTestState] = useState<Record<number, string>>({})
  // null = closed; the sink being edited (its id) or a create.
  const [formOpen, setFormOpen] = useState(false)
  const [editingId, setEditingId] = useState<number | null>(null)
  const [name, setName] = useState('')
  const [kind, setKind] = useState<NotificationSink['kind']>('webhook')
  const [url, setUrl] = useState('')
  const [sectionId, setSectionId] = useState('')
  const [token, setToken] = useState('')
  const [userAgent, setUserAgent] = useState('')
  const [enabled, setEnabled] = useState(true)
  const { busy: saving, error: saveError, submit: saveSubmit } = useSubmit()

  const load = useCallback(() => {
    let cancelled = false
    setLoading(true)
    setFatal(null)
    setAuthLost(false)
    client
      .listNotifications()
      .then((list) => {
        if (!cancelled) setSinks(list)
      })
      .catch((e: unknown) => {
        if (cancelled) return
        if (e instanceof ApiError && e.status === 401) setAuthLost(true)
        setFatal(e instanceof Error ? e.message : 'failed to load notifications')
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [])

  useEffect(() => load(), [load])

  if (fatal) {
    return (
      <SectionError message={fatal} authLost={authLost} onRetry={load} testId="notif-error" />
    )
  }

  function openCreate() {
    setEditingId(null)
    setName('')
    setKind('webhook')
    setUrl('')
    setSectionId('')
    setToken('')
    setUserAgent('')
    setEnabled(true)
    setFormOpen(true)
  }

  function openEdit(sink: NotificationSink) {
    setEditingId(sink.id)
    setName(sink.name)
    setKind(sink.kind)
    setUrl(sink.url)
    setSectionId(sink.section_id ?? '')
    setToken('') // blank = keep the stored token
    setUserAgent(sink.user_agent ?? '')
    setEnabled(sink.enabled)
    setFormOpen(true)
  }

  function handleSave() {
    void saveSubmit(async () => {
      const body = {
        name: name.trim(),
        kind,
        url,
        section_id: kind === 'plex' ? sectionId : '',
        token,
        user_agent: userAgent,
        enabled,
      }
      if (editingId != null) await client.updateNotification(editingId, body)
      else await client.createNotification(body)
      setFormOpen(false)
      load()
    })
  }

  async function handleTest(id: number) {
    setTestState((prev) => ({ ...prev, [id]: 'busy' }))
    try {
      const res = await client.testNotification(id)
      setTestState((prev) => ({ ...prev, [id]: res.ok ? 'ok' : 'failed' }))
    } catch (e: unknown) {
      setTestState((prev) => ({
        ...prev,
        [id]: e instanceof Error ? e.message : 'test failed',
      }))
    }
  }

  async function handleDelete(id: number) {
    await client.deleteNotification(id)
    load()
  }

  return (
    <SectionCard
      title="Notifications"
      hint="Saved notification sinks: webhook and Plex / Jellyfin / Emby connections. A blank token keeps the stored one."
      testId="notif-card"
    >
      <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
        <button onClick={openCreate} data-testid="notif-add">
          + Add notification
        </button>
      </div>

      {loading ? (
        <p className="dim">Loading notifications…</p>
      ) : sinks.length === 0 ? (
        <div className="empty-state" data-testid="notif-empty">
          <h3>No notifications</h3>
          <p>Add a webhook or media-server connection to get notified about activity.</p>
          <button onClick={openCreate} data-testid="notif-empty-add">
            + Add notification
          </button>
        </div>
      ) : (
        <table className="table" data-testid="notif-table">
          <thead>
            <tr>
              <th>Name</th>
              <th>Kind</th>
              <th>URL</th>
              <th>Token</th>
              <th>Enabled</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {sinks.map((s) => (
              <tr key={s.id} data-testid={`notif-row-${s.id}`}>
                <td>{s.name}</td>
                <td>{s.kind}</td>
                <td>{s.url}</td>
                <td data-testid={`notif-token-${s.id}`}>{s.token_set ? 'set' : '—'}</td>
                <td>{s.enabled ? 'yes' : 'no'}</td>
                <td>
                  <button
                    className="ghost small"
                    onClick={() => void handleTest(s.id)}
                    disabled={testState[s.id] === 'busy'}
                    data-testid={`notif-test-${s.id}`}
                  >
                    Test
                  </button>
                  <button className="ghost small" onClick={() => openEdit(s)} data-testid={`notif-edit-${s.id}`}>
                    Edit
                  </button>
                  <button
                    className="ghost small danger"
                    onClick={() => void handleDelete(s.id)}
                    data-testid={`notif-del-${s.id}`}
                  >
                    Delete
                  </button>
                  {testState[s.id] && (
                    <span
                      className={testState[s.id] === 'ok' ? 'pill pill-ok' : 'error small'}
                      data-testid={`notif-test-result-${s.id}`}
                    >
                      {testState[s.id] === 'busy'
                        ? 'Testing…'
                        : testState[s.id] === 'ok'
                          ? 'Delivered'
                          : testState[s.id]}
                    </span>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      {formOpen && (
        <div className="card" style={{ marginTop: '0.75rem' }} data-testid="notif-form">
          <h3>{editingId != null ? 'Edit notification' : 'Add notification'}</h3>
          <label htmlFor="notif-name">Name</label>
          <input id="notif-name" value={name} onChange={(e) => setName(e.target.value)} data-testid="notif-f-name" />
          <label htmlFor="notif-kind">Kind</label>
          <select
            id="notif-kind"
            value={kind}
            onChange={(e) => setKind(e.target.value as NotificationSink['kind'])}
            data-testid="notif-f-kind"
          >
            {SINK_KINDS.map((k) => (
              <option key={k} value={k}>
                {k}
              </option>
            ))}
          </select>
          <label htmlFor="notif-url">URL</label>
          <input id="notif-url" value={url} onChange={(e) => setUrl(e.target.value)} data-testid="notif-f-url" />
          {kind === 'plex' && (
            <>
              <label htmlFor="notif-section">Plex library section ID (blank = root refresh)</label>
              <input
                id="notif-section"
                value={sectionId}
                onChange={(e) => setSectionId(e.target.value)}
                data-testid="notif-f-section"
              />
            </>
          )}
          <label htmlFor="notif-token">Token {editingId != null ? '(blank to keep)' : ''}</label>
          <input
            id="notif-token"
            type="password"
            value={token}
            onChange={(e) => setToken(e.target.value)}
            placeholder={editingId != null ? '••••••••' : ''}
            data-testid="notif-f-token"
          />
          <label htmlFor="notif-user-agent">User-Agent (blank = mediarr default)</label>
          <input
            id="notif-user-agent"
            value={userAgent}
            onChange={(e) => setUserAgent(e.target.value)}
            data-testid="notif-f-user-agent"
          />
          <label className="check" htmlFor="notif-enabled">
            <input
              id="notif-enabled"
              type="checkbox"
              checked={enabled}
              onChange={(e) => setEnabled(e.target.checked)}
              data-testid="notif-f-enabled"
            />
            Enabled
          </label>
          <SaveError error={saveError} testId="notif-form-error" />
          <button onClick={handleSave} disabled={saving || !name.trim() || !url} data-testid="notif-save">
            {saving ? 'Saving…' : editingId != null ? 'Save changes' : 'Add notification'}
          </button>
          <button className="ghost" onClick={() => setFormOpen(false)} data-testid="notif-cancel">
            Cancel
          </button>
        </div>
      )}
    </SectionCard>
  )
}
