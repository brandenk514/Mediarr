import { useEffect, useState } from 'react'
import { client } from '../../../api/client'
import type { ConnectSettings, ConnectTestResult, Notification } from '../../../api/types'
import { SectionCard, SectionError, SaveError, useSectionDoc, useSubmit } from './shared'

// ConnectSection is the connect (notifications) section of the P5 settings
// shell: the notification feed (webhook / Plex / Jellyfin / Emby sinks) with
// per-notification test dispatch through the /connect/test endpoint. Tokens
// are masked by the server — the UI only ever sees a token_set indicator.
const NOTIF_KINDS = ['webhook', 'plex', 'jellyfin', 'emby'] as const

export function ConnectSection() {
  const { doc, loading, error, authLost, refresh } = useSectionDoc<ConnectSettings>('connect')
  const [notifs, setNotifs] = useState<Notification[]>([])
  const [saved, setSaved] = useState(false)
  const { busy, error: saveError, submit } = useSubmit()

  // New-notification draft (kind + url + section_id + token; a blank token
  // keeps the stored value on edit, starts unset on add).
  const [editId, setEditId] = useState<number | null>(null)
  const [name, setName] = useState('')
  const [kind, setKind] = useState<(typeof NOTIF_KINDS)[number]>('webhook')
  const [url, setUrl] = useState('')
  const [sectionId, setSectionId] = useState('')
  const [token, setToken] = useState('')

  // Per-notification test outcome (id -> result).
  const [testResults, setTestResults] = useState<Record<number, ConnectTestResult>>({})
  const [testing, setTesting] = useState<number | null>(null)

  useEffect(() => {
    if (doc) setNotifs(doc.notifications ?? [])
  }, [doc])

  if (error) {
    return <SectionError message={error} authLost={authLost} onRetry={refresh} testId="connect-error" />
  }

  function beginEdit(n: Notification) {
    setEditId(n.id)
    setName(n.name)
    setKind(n.kind as (typeof NOTIF_KINDS)[number])
    setUrl(n.options.url)
    setSectionId(n.options.section_id)
    setToken('')
  }

  function clearDraft() {
    setEditId(null)
    setName('')
    setKind('webhook')
    setUrl('')
    setSectionId('')
    setToken('')
  }

  function submitDraft() {
    const n = name.trim()
    if (!n || !url.trim()) return
    if (editId !== null) {
      setNotifs((ns) =>
        ns.map((x) =>
          x.id === editId
            ? {
                ...x,
                name: n,
                kind,
                options: { ...x.options, url: url.trim(), section_id: sectionId, token },
              }
            : x,
        ),
      )
    } else {
      const id = notifs.reduce((m, x) => Math.max(m, x.id), 0) + 1
      setNotifs((ns) => [...ns, { id, name: n, kind, enabled: true, options: { url: url.trim(), section_id: sectionId, token, token_set: token !== '' } }])
    }
    clearDraft()
  }

  function handleSave() {
    void submit(async () => {
      const next: ConnectSettings = { notifications: notifs }
      await client.putSettingsSection<ConnectSettings>('connect', next)
      setSaved(true)
      setTimeout(() => setSaved(false), 2000)
    })
  }

  async function runTest(n: Notification) {
    if (testing !== null) return
    setTesting(n.id)
    try {
      const res = await client.testConnectNotification({
        id: n.id,
        kind: n.kind,
        options: n.options,
      })
      setTestResults((rs) => ({ ...rs, [n.id]: res }))
    } catch (e) {
      setTestResults((rs) => ({
        ...rs,
        [n.id]: { ok: false, message: e instanceof Error ? e.message : 'test failed' },
      }))
    } finally {
      setTesting(null)
    }
  }

  return (
    <>
      <SectionCard
        title="Notifications"
        hint="One entry per sink. The test button fires a test event through the dispatcher; the endpoint reports the delivery outcome."
        testId="connect-card"
      >
        {notifs.map((n) => (
          <div className="subform" key={n.id} data-testid={`connect-item-${n.id}`}>
            <span className="pill" data-testid={`connect-kind-${n.id}`}>
              {n.kind}
            </span>
            <span data-testid={`connect-name-${n.id}`}>{n.name}</span>
            <label className="check">
              <input
                type="checkbox"
                checked={n.enabled}
                onChange={(e) =>
                  setNotifs((ns) => ns.map((x) => (x.id === n.id ? { ...x, enabled: e.target.checked } : x)))
                }
                data-testid={`connect-enabled-${n.id}`}
              />
              enabled
            </label>
            <span className="dim small" data-testid={`connect-url-${n.id}`}>
              {n.options.url} {n.options.token_set ? '(token stored)' : ''}
            </span>
            <button className="ghost" onClick={() => beginEdit(n)} data-testid={`connect-edit-${n.id}`}>
              Edit
            </button>
            <button className="ghost" onClick={() => setNotifs((ns) => ns.filter((x) => x.id !== n.id))} data-testid={`connect-del-${n.id}`}>
              Delete
            </button>
            <button className="ghost" onClick={() => runTest(n)} disabled={testing !== null} data-testid={`connect-test-${n.id}`}>
              {testing === n.id ? 'Testing…' : 'Test'}
            </button>
            {testResults[n.id] && (
              <p
                className={testResults[n.id].ok ? 'pill pill-ok' : 'error'}
                data-testid={`connect-test-result-${n.id}`}
              >
                {testResults[n.id].ok ? 'ok: ' : 'failed: '}
                {testResults[n.id].message}
              </p>
            )}
          </div>
        ))}
        {notifs.length === 0 && <p className="dim small">No notifications configured.</p>}
        {saved && (
          <p className="pill pill-ok" data-testid="connect-saved">
            Saved
          </p>
        )}
        <SaveError error={saveError} testId="connect-error-save" />
        <button onClick={handleSave} disabled={busy || loading} data-testid="connect-save">
          {busy ? 'Saving…' : 'Save notifications'}
        </button>
      </SectionCard>

      <SectionCard title={editId !== null ? `Edit notification ${editId}` : 'Add a notification'} testId="connect-add-card">
        <label htmlFor="connect-new-name">Name</label>
        <input id="connect-new-name" value={name} onChange={(e) => setName(e.target.value)} data-testid="connect-new-name" />
        <label htmlFor="connect-new-kind">Sink</label>
        <select id="connect-new-kind" value={kind} onChange={(e) => setKind(e.target.value as never)} data-testid="connect-new-kind">
          {NOTIF_KINDS.map((k) => (
            <option key={k} value={k}>
              {k}
            </option>
          ))}
        </select>
        <label htmlFor="connect-new-url">
          {kind === 'webhook' ? 'Webhook URL' : 'Base URL'}
        </label>
        <input
          id="connect-new-url"
          value={url}
          onChange={(e) => setUrl(e.target.value)}
          placeholder={kind === 'webhook' ? 'https://hooks.example.com/notify' : 'http://localhost:32400'}
          data-testid="connect-new-url"
        />
        <label htmlFor="connect-new-section">
          {kind === 'plex' ? 'Plex section id (optional)' : 'Section id (Jellyfin/Emby; blank refreshes all)'}
        </label>
        <input id="connect-new-section" value={sectionId} onChange={(e) => setSectionId(e.target.value)} data-testid="connect-new-section" />
        <label htmlFor="connect-new-token">
          Token {editId !== null && notifs.find((n) => n.id === editId)?.options.token_set ? '(stored — blank to keep)' : '(optional)'}
        </label>
        <input id="connect-new-token" type="password" value={token} onChange={(e) => setToken(e.target.value)} data-testid="connect-new-token" />
        <div>
          <button className="ghost" onClick={submitDraft} data-testid="connect-add">
            {editId !== null ? 'Apply changes' : 'Add notification'}
          </button>{' '}
          {editId !== null && (
            <button className="ghost" onClick={clearDraft} data-testid="connect-edit-cancel">
              Cancel
            </button>
          )}
        </div>
      </SectionCard>
    </>
  )
}
