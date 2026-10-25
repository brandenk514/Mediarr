import { useCallback, useEffect, useRef, useState } from 'react'
import { client } from '../../../api/client'
import type { Indexer, IndexerTestResult } from '../../../api/types'
import { SaveError, useSubmit } from './shared'

// IndexersSection is the indexers section of the P5 settings shell: the
// indexer management surface (create / update / enable-toggle / delete +
// on-demand test). It owns its dedicated /indexers endpoints (one of the two
// sections in the 13-section shell that keep their own endpoints rather than
// the generic per-section document).
//
// INDEXER_KINDS mirrors the server's provider registry (the write endpoints
// validate a definition's kind against it, so only these can be saved).
const INDEXER_KINDS = ['fake', 'torznab', 'torrent-rss', 'nzb'] as const

export function IndexersSection() {
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
  const disabled = loading

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
