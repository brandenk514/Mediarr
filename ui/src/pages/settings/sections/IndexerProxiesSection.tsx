import { useCallback, useEffect, useState } from 'react'
import { ApiError, client } from '../../../api/client'
import type { IndexerProxy, TagsSettings } from '../../../api/types'
import { SaveError, SectionCard, SectionError, useSubmit } from './shared'
import { Link } from 'react-router-dom'

// IndexerProxiesSection is the Prowlarr PR4 "Indexer Proxies" section (the
// shell's old `indexers` slug): the saved FlareSolverr instances with CRUD, a
// per-instance health Test, and the tag-based routing — indexers whose tags
// intersect a proxy's tags are routed through it. The password is masked on
// read (presence only; a blank field keeps the stored one).
export function IndexerProxiesSection() {
  const [authLost, setAuthLost] = useState(false)
  const [fatal, setFatal] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [proxies, setProxies] = useState<IndexerProxy[]>([])
  // The tag labels the routing checkboxes offer (from the tags document).
  const [tagLabels, setTagLabels] = useState<string[]>([])
  // Per-proxy test results: 'busy' | 'ok:<version>' | an error message.
  const [testState, setTestState] = useState<Record<number, string>>({})
  // null = closed; the proxy id being edited, or 'create'.
  const [formOpen, setFormOpen] = useState(false)
  const [editingId, setEditingId] = useState<number | null>(null)
  const [name, setName] = useState('')
  const [url, setUrl] = useState('')
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [tags, setTags] = useState<string[]>([])
  const { busy: saving, error: saveError, submit: saveSubmit } = useSubmit()

  const load = useCallback(() => {
    let cancelled = false
    setLoading(true)
    setFatal(null)
    setAuthLost(false)
    // The tags document is a routing-picker convenience: a failure there just
    // leaves the picker empty — it is not fatal to the proxies list.
    Promise.all([
      client.listIndexerProxies(),
      client
        .getSettingsSection<TagsSettings>('tags')
        .then((d) => (d.tags ?? []).map((t) => t.label))
        .catch(() => [] as string[]),
    ])
      .then(([list, labels]) => {
        if (cancelled) return
        setProxies(list)
        setTagLabels(labels)
      })
      .catch((e: unknown) => {
        if (cancelled) return
        if (e instanceof ApiError && e.status === 401) setAuthLost(true)
        setFatal(e instanceof Error ? e.message : 'failed to load indexer proxies')
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
    return <SectionError message={fatal} authLost={authLost} onRetry={load} testId="prox-error" />
  }

  function openCreate() {
    setEditingId(null)
    setName('')
    setUrl('')
    setUsername('')
    setPassword('')
    setTags([])
    setFormOpen(true)
  }

  function openEdit(proxy: IndexerProxy) {
    setEditingId(proxy.id)
    setName(proxy.name)
    setUrl(proxy.url)
    setUsername(proxy.username ?? '')
    setPassword('') // blank = keep the stored password
    setTags(proxy.tags ?? [])
    setFormOpen(true)
  }

  function toggleTag(label: string) {
    setTags((prev) => (prev.includes(label) ? prev.filter((t) => t !== label) : [...prev, label]))
  }

  function handleSave() {
    void saveSubmit(async () => {
      const body = {
        name: name.trim(),
        url,
        username,
        password,
        tags,
      }
      if (editingId != null) await client.updateIndexerProxy(editingId, body)
      else await client.createIndexerProxy(body)
      setFormOpen(false)
      load()
    })
  }

  async function handleTest(id: number) {
    setTestState((prev) => ({ ...prev, [id]: 'busy' }))
    try {
      const res = await client.testIndexerProxy(id)
      setTestState((prev) => ({
        ...prev,
        [id]: res.ok ? `ok${res.version ? `: ${res.version}` : ''}` : 'unreachable',
      }))
    } catch (e: unknown) {
      setTestState((prev) => ({
        ...prev,
        [id]: e instanceof Error ? e.message : 'test failed',
      }))
    }
  }

  async function handleDelete(id: number) {
    await client.deleteIndexerProxy(id)
    load()
  }

  return (
    <SectionCard
      title="Indexer Proxies"
      hint="FlareSolverr instances. Indexers whose tags intersect a proxy's tags are routed through it."
      testId="prox-card"
    >
      <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
        <button onClick={openCreate} data-testid="prox-add">
          + Add proxy
        </button>
      </div>

      {loading ? (
        <p className="dim">Loading indexer proxies…</p>
      ) : proxies.length === 0 ? (
        <div className="empty-state" data-testid="prox-empty">
          <h3>No indexer proxies</h3>
          <p>Add a FlareSolverr instance and tag it to route matching indexers through it.</p>
          <button onClick={openCreate} data-testid="prox-empty-add">
            + Add proxy
          </button>
        </div>
      ) : (
        <table className="table" data-testid="prox-table">
          <thead>
            <tr>
              <th>Name</th>
              <th>URL</th>
              <th>Username</th>
              <th>Password</th>
              <th>Tags</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {proxies.map((p) => (
              <tr key={p.id} data-testid={`prox-row-${p.id}`}>
                <td>{p.name}</td>
                <td>{p.url}</td>
                <td>{p.username || '—'}</td>
                <td data-testid={`prox-password-${p.id}`}>{p.password_set ? 'set' : '—'}</td>
                <td data-testid={`prox-tags-${p.id}`}>
                  {p.tags && p.tags.length ? p.tags.join(', ') : '—'}
                </td>
                <td>
                  <button
                    className="ghost small"
                    onClick={() => void handleTest(p.id)}
                    disabled={testState[p.id] === 'busy'}
                    data-testid={`prox-test-${p.id}`}
                  >
                    Test
                  </button>
                  <button className="ghost small" onClick={() => openEdit(p)} data-testid={`prox-edit-${p.id}`}>
                    Edit
                  </button>
                  <button
                    className="ghost small danger"
                    onClick={() => void handleDelete(p.id)}
                    data-testid={`prox-del-${p.id}`}
                  >
                    Delete
                  </button>
                  {testState[p.id] && (
                    <span
                      className={testState[p.id].startsWith('ok') ? 'pill pill-ok' : 'error small'}
                      data-testid={`prox-test-result-${p.id}`}
                    >
                      {testState[p.id] === 'busy' ? 'Testing…' : testState[p.id]}
                    </span>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      {formOpen && (
        <div className="card" style={{ marginTop: '0.75rem' }} data-testid="prox-form">
          <h3>{editingId != null ? 'Edit proxy' : 'Add proxy'}</h3>
          <label htmlFor="prox-name">Name</label>
          <input id="prox-name" value={name} onChange={(e) => setName(e.target.value)} data-testid="prox-f-name" />
          <label htmlFor="prox-url">URL</label>
          <input
            id="prox-url"
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            placeholder="http://flaresolverr:8191"
            data-testid="prox-f-url"
          />
          <label htmlFor="prox-username">Username</label>
          <input
            id="prox-username"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            data-testid="prox-f-username"
          />
          <label htmlFor="prox-password">Password {editingId != null ? '(blank to keep)' : ''}</label>
          <input
            id="prox-password"
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            placeholder={editingId != null ? '••••••••' : ''}
            data-testid="prox-f-password"
          />
          <label>Tags (indexers carrying a matching tag are routed here)</label>
          <div data-testid="prox-f-tags">
            {tagLabels.length === 0 ? (
              <p className="dim small">No tags defined yet — create tags in the Tags section to enable routing.</p>
            ) : (
              tagLabels.map((label) => (
                <label className="check" key={label} data-testid={`prox-tag-${label}`}>
                  <input
                    type="checkbox"
                    checked={tags.includes(label)}
                    onChange={() => toggleTag(label)}
                  />
                  {label}
                </label>
              ))
            )}
          </div>
          <SaveError error={saveError} testId="prox-form-error" />
          <button onClick={handleSave} disabled={saving || !name.trim() || !url} data-testid="prox-save">
            {saving ? 'Saving…' : editingId != null ? 'Save changes' : 'Add proxy'}
          </button>
          <button className="ghost" onClick={() => setFormOpen(false)} data-testid="prox-cancel">
            Cancel
          </button>
        </div>
      )}

      <p className="dim small">
        Assign tags to indexers in the{' '}
        <Link to="/indexers" data-testid="prox-hub-link">
          Indexers hub
        </Link>{' '}
        — their traffic follows the proxy routing.
      </p>
    </SectionCard>
  )
}
