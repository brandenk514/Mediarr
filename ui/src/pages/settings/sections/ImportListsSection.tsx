import { useEffect, useState } from 'react'
import { client } from '../../../api/client'
import type { ImportList, ImportListsSettings, ImportRunResult } from '../../../api/types'
import { SectionCard, SectionError, SaveError, useSectionDoc, useSubmit } from './shared'

// ImportListsSection is the import-lists section of the P5 settings shell:
// the Trakt list and Sonarr-instance bridge configs, the Radarr R5b drop-in
// bridge (kind=radarr: host + port + masked API key), plus the run action
// (dry-run or real import through the /import-lists/import endpoint, which
// reports per-item imported / exists / skipped / error). API keys are
// masked by the server — a blank field keeps the stored key.
const LIST_KINDS = ['trakt', 'sonarr', 'radarr'] as const

// RADARR_DEFAULT_PORT is the stock Radarr port, used when a radarr list
// saves with a blank port (the server requires a 1-65535 value).
const RADARR_DEFAULT_PORT = 7878

export function ImportListsSection() {
  const { doc, loading, error, authLost, refresh } = useSectionDoc<ImportListsSettings>('import-lists')
  const [lists, setLists] = useState<ImportList[]>([])
  const [saved, setSaved] = useState(false)
  const { busy, error: saveError, submit } = useSubmit()

  // Draft form state.
  const [editId, setEditId] = useState<number | null>(null)
  const [name, setName] = useState('')
  const [kind, setKind] = useState<(typeof LIST_KINDS)[number]>('trakt')
  const [url, setUrl] = useState('')
  // Radarr R5b draft fields (kind=radarr): the Radarr instance to import
  // from; the api key round-trips masked (blank keeps the stored value).
  const [host, setHost] = useState('')
  const [port, setPort] = useState('')
  const [apiKey, setApiKey] = useState('')

  // Run state: which list is running + the last report.
  const [runningId, setRunningId] = useState<number | null>(null)
  const [dryRun, setDryRun] = useState(true)
  const [runResult, setRunResult] = useState<ImportRunResult | null>(null)
  const [runErr, setRunErr] = useState('')

  useEffect(() => {
    if (doc) setLists(doc.lists ?? [])
  }, [doc])

  if (error) {
    return <SectionError message={error} authLost={authLost} onRetry={refresh} testId="import-lists-error" />
  }

  function beginEdit(l: ImportList) {
    setEditId(l.id)
    setName(l.name)
    setKind(l.kind as (typeof LIST_KINDS)[number])
    setUrl(l.url ?? '')
    setHost(l.host ?? '')
    setPort(l.port ? String(l.port) : '')
    setApiKey('')
  }

  function clearDraft() {
    setEditId(null)
    setName('')
    setKind('trakt')
    setUrl('')
    setHost('')
    setPort('')
    setApiKey('')
  }

  function submitDraft() {
    const n = name.trim()
    if (!n) return
    const isRadarr = kind === 'radarr'
    // The address fields depend on the kind: trakt/sonarr ride the URL,
    // radarr rides host+port (the server validates accordingly).
    if (isRadarr) {
      if (!host.trim()) return
      const portNum = Number(port) || RADARR_DEFAULT_PORT
      const address = { url: '', host: host.trim(), port: portNum }
      if (editId !== null) {
        setLists((ls) =>
          ls.map((x) =>
            x.id === editId
              ? { ...x, ...address, name: n, kind, api_key: apiKey, api_key_set: x.api_key_set || apiKey !== '' }
              : x,
          ),
        )
      } else {
        const id = lists.reduce((m, x) => Math.max(m, x.id), 0) + 1
        setLists((ls) => [...ls, { id, name: n, kind, ...address, enabled: true, api_key: apiKey, api_key_set: apiKey !== '' }])
      }
      clearDraft()
      return
    }
    if (!url.trim()) return
    if (editId !== null) {
      setLists((ls) =>
        ls.map((x) =>
          x.id === editId
            ? { ...x, name: n, kind, url: url.trim(), api_key: apiKey, api_key_set: x.api_key_set || apiKey !== '' }
            : x,
        ),
      )
    } else {
      const id = lists.reduce((m, x) => Math.max(m, x.id), 0) + 1
      setLists((ls) => [...ls, { id, name: n, kind, url: url.trim(), enabled: true, api_key: apiKey, api_key_set: apiKey !== '' }])
    }
    clearDraft()
  }

  function handleSave() {
    void submit(async () => {
      const next: ImportListsSettings = { lists }
      await client.putSettingsSection<ImportListsSettings>('import-lists', next)
      setSaved(true)
      setTimeout(() => setSaved(false), 2000)
    })
  }

  async function runImport(id: number) {
    if (runningId !== null) return
    setRunningId(id)
    setRunErr('')
    setRunResult(null)
    try {
      const res = await client.runImportListImport({ id, dry_run: dryRun })
      setRunResult(res)
    } catch (e) {
      setRunErr(e instanceof Error ? e.message : 'import run failed')
    } finally {
      setRunningId(null)
    }
  }

  return (
    <>
      <SectionCard
        title="Import lists"
        hint="Trakt lists, Sonarr instances, and Radarr instances (drop-in bridges). Dry-run reports what would be added without touching the libraries."
        testId="import-lists-card"
      >
        <label className="check">
          <input
            type="checkbox"
            checked={dryRun}
            onChange={(e) => setDryRun(e.target.checked)}
            data-testid="il-dry-run"
          />
          dry run (report only, add nothing)
        </label>
        {lists.map((l) => (
          <div className="subform" key={l.id} data-testid={`il-item-${l.id}`}>
            <span className="pill" data-testid={`il-kind-${l.id}`}>
              {l.kind}
            </span>
            <span data-testid={`il-name-${l.id}`}>{l.name}</span>
            <label className="check">
              <input
                type="checkbox"
                checked={l.enabled}
                onChange={(e) =>
                  setLists((ls) => ls.map((x) => (x.id === l.id ? { ...x, enabled: e.target.checked } : x)))
                }
                data-testid={`il-enabled-${l.id}`}
              />
              enabled
            </label>
            <span className="dim small" data-testid={`il-url-${l.id}`}>
              {l.kind === 'radarr' ? `${l.host}:${l.port}` : l.url} {l.api_key_set ? '(key stored)' : ''}
            </span>
            <button className="ghost" onClick={() => beginEdit(l)} data-testid={`il-edit-${l.id}`}>
              Edit
            </button>
            <button className="ghost" onClick={() => setLists((ls) => ls.filter((x) => x.id !== l.id))} data-testid={`il-del-${l.id}`}>
              Delete
            </button>
            <button className="ghost" onClick={() => runImport(l.id)} disabled={runningId !== null} data-testid={`il-run-${l.id}`}>
              {runningId === l.id ? 'Running…' : dryRun ? 'Dry-run' : 'Import'}
            </button>
          </div>
        ))}
        {lists.length === 0 && <p className="dim small">No import lists configured.</p>}
        {runErr && (
          <p className="error" data-testid="il-run-error">
            {runErr}
          </p>
        )}
        {runResult && (
          <div data-testid="il-run-result">
            <span className="pill" data-testid="il-run-summary">
              {runResult.dry_run ? 'dry run' : 'import'}: {runResult.total} item
              {runResult.total === 1 ? '' : 's'}
            </span>
            {runResult.items.map((it) => (
              <span className="pill" key={`${it.external_id}-${it.title}`} data-testid={`il-run-item-${it.status}`}>
                {it.title || it.external_id} [{it.kind}] {it.status}
                {it.message ? ` — ${it.message}` : ''}
              </span>
            ))}
            {runResult.items.length === 0 && <span className="dim small"> list was empty</span>}
          </div>
        )}
        {saved && (
          <p className="pill pill-ok" data-testid="il-saved">
            Saved
          </p>
        )}
        <SaveError error={saveError} testId="il-error-save" />
        <button onClick={handleSave} disabled={busy || loading} data-testid="il-save">
          {busy ? 'Saving…' : 'Save import lists'}
        </button>
      </SectionCard>

      <SectionCard title={editId !== null ? `Edit list ${editId}` : 'Add an import list'} testId="il-add-card">
        <label htmlFor="il-new-name">Name</label>
        <input id="il-new-name" value={name} onChange={(e) => setName(e.target.value)} data-testid="il-new-name" />
        <label htmlFor="il-new-kind">Source</label>
        <select id="il-new-kind" value={kind} onChange={(e) => setKind(e.target.value as never)} data-testid="il-new-kind">
          {LIST_KINDS.map((k) => (
            <option key={k} value={k}>
              {k}
            </option>
          ))}
        </select>
        {kind !== 'radarr' && (
          <>
            <label htmlFor="il-new-url">
              {kind === 'trakt' ? 'Trakt list URL' : 'Sonarr instance base URL'}
            </label>
            <input
              id="il-new-url"
              value={url}
              onChange={(e) => setUrl(e.target.value)}
              placeholder={kind === 'trakt' ? 'https://trakt.tv/users/u/lists/slug' : 'http://sonarr.local:8989'}
              data-testid="il-new-url"
            />
          </>
        )}
        {kind === 'radarr' && (
          <>
            {/* Radarr R5b: the drop-in bridge points at a Radarr instance by
                host+port; the stock port is pre-filled when left blank. */}
            <label htmlFor="il-new-host">Radarr host</label>
            <input
              id="il-new-host"
              value={host}
              onChange={(e) => setHost(e.target.value)}
              placeholder="radarr.home.lan"
              data-testid="il-new-host"
            />
            <label htmlFor="il-new-port">Port</label>
            <input
              id="il-new-port"
              type="number"
              value={port}
              onChange={(e) => setPort(e.target.value)}
              placeholder={String(RADARR_DEFAULT_PORT)}
              data-testid="il-new-port"
            />
          </>
        )}
        {kind !== 'trakt' && (
          <label htmlFor="il-new-key">
            {kind === 'radarr' ? 'Radarr API key' : 'API key'}{' '}
            {editId !== null && lists.find((l) => l.id === editId)?.api_key_set ? '(stored — blank to keep)' : ''}
          </label>
        )}
        {kind !== 'trakt' && (
          <input id="il-new-key" type="password" value={apiKey} onChange={(e) => setApiKey(e.target.value)} data-testid="il-new-key" />
        )}
        <div>
          <button className="ghost" onClick={submitDraft} data-testid="il-add">
            {editId !== null ? 'Apply changes' : 'Add list'}
          </button>{' '}
          {editId !== null && (
            <button className="ghost" onClick={clearDraft} data-testid="il-edit-cancel">
              Cancel
            </button>
          )}
        </div>
      </SectionCard>
    </>
  )
}
