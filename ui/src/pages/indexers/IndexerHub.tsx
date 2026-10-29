import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { client } from '../../api/client'
import type { Indexer, IndexerCreate, IndexerDefinition, IndexerDefinitionField, IndexerTestResult, IndexerUpdate } from '../../api/types'
import { SaveError, useSubmit } from '../settings/sections/shared'
import { EmptyState, Menu, Modal, Toolbar, TbButton } from '../../components/ui'

// IndexerHubPage is the Prowlarr PR1 Indexers hub: a FIRST-CLASS list page (the
// list IS the home — not a settings sub-section). It mirrors the Prowlarr
// Indexers IA from the parity plan §2: an icon-over-label toolbar (Add Indexer
// · Test All Indexers · Select Indexers · Options · Sort · Filter, with Sync
// App Indexers stubbed), a table with a per-row status icon + protocol/privacy
// pills + priority + sync profile + added + category badges + per-row Edit, a
// right-rail A–Z quick-jump, and a bottom summary panel (color legend +
// counters). CRUD, enable-toggle, per-row Test, and Test All hit the existing
// endpoints; the API key stays masked (has_api_key only).

const INDEXER_KINDS = ['fake', 'torznab', 'torrent-rss', 'nzb'] as const

type SortKey = 'name-asc' | 'name-desc' | 'priority-asc' | 'priority-desc'
const SORT_OPTIONS = [
  { value: 'name-asc', label: 'Name ↑' },
  { value: 'name-desc', label: 'Name ↓' },
  { value: 'priority-asc', label: 'Priority ↑' },
  { value: 'priority-desc', label: 'Priority ↓' },
] as const

type FilterKey = 'all' | 'torrent' | 'usenet'
const FILTER_OPTIONS = [
  { value: 'all', label: 'All' },
  { value: 'torrent', label: 'Torrent' },
  { value: 'usenet', label: 'Usenet' },
] as const

// Column model for the dynamic table. `togglable` columns can be hidden via the
// Options toolbar; name and actions are always present.
type ColKey =
  | 'status'
  | 'name'
  | 'protocol'
  | 'privacy'
  | 'priority'
  | 'sync'
  | 'added'
  | 'categories'
  | 'actions'
const COLUMNS: { key: ColKey; label: string; width: string; togglable: boolean }[] = [
  { key: 'status', label: 'Status', width: '1.05fr', togglable: true },
  { key: 'name', label: 'Indexer', width: '1.9fr', togglable: false },
  { key: 'protocol', label: 'Protocol', width: '0.9fr', togglable: true },
  { key: 'privacy', label: 'Privacy', width: '1fr', togglable: true },
  { key: 'priority', label: 'Priority', width: '0.7fr', togglable: true },
  { key: 'sync', label: 'Sync Profile', width: '0.9fr', togglable: true },
  { key: 'added', label: 'Added', width: '0.85fr', togglable: true },
  { key: 'categories', label: 'Categories', width: '1.4fr', togglable: true },
  { key: 'actions', label: 'Actions', width: 'auto', togglable: false },
]
const DEFAULT_VISIBLE: ColKey[] = COLUMNS.filter((c) => c.togglable).map((c) => c.key)

function protocolOf(kind: string): 'torrent' | 'usenet' {
  return kind === 'torrent-rss' ? 'torrent' : 'usenet'
}

// PR2 kind mapping: the API still requires `kind`, but the UI derives it from
// the chosen definition. generic-torznab is a torrent *site* that speaks the
// Torznab protocol, so it maps to the torznab adapter — not torrent-rss.
function kindFromDef(def: IndexerDefinition): string {
  if (def.id === 'generic-torznab') return 'torznab'
  return def.protocol === 'usenet' ? 'nzb' : 'torrent-rss'
}

type RowStatus = 'enabled' | 'enabled-redirected' | 'disabled' | 'error'
// PR1 derives row status from the enabled flag + the most recent on-demand test
// result (a failed Test flips a row to Error until it is re-tested). "Enabled +
// Redirected" is in the legend but not reachable in PR1 (no redirect field yet).
function statusOf(idx: Indexer, res: IndexerTestResult | undefined): RowStatus {
  if (!idx.enabled) return 'disabled'
  if (res && !res.ok) return 'error'
  return 'enabled'
}
const STATUS_META: Record<RowStatus, { cls: string; label: string }> = {
  enabled: { cls: 'st-enabled', label: 'Enabled' },
  'enabled-redirected': { cls: 'st-redirect', label: 'Enabled + Redirected' },
  disabled: { cls: 'st-disabled', label: 'Disabled' },
  error: { cls: 'st-error', label: 'Error' },
}
const STATUS_GLYPH: Record<RowStatus, string> = {
  enabled: '●',
  'enabled-redirected': '➜',
  disabled: '○',
  error: '✕',
}

function fmtAdded(iso: string | undefined): string {
  if (!iso) return '—'
  return iso.slice(0, 10) // YYYY-MM-DD
}
function firstLetter(name: string): string {
  const c = name.trim().charAt(0).toUpperCase()
  return c >= 'A' && c <= 'Z' ? c : '#'
}
function titleCase(s: string): string {
  return s ? s.charAt(0).toUpperCase() + s.slice(1) : s
}

function StatusIcon({ status }: { status: RowStatus }) {
  const meta = STATUS_META[status]
  return (
    <span className={`idx-status ${meta.cls}`} data-testid={`idx-status-${meta.cls.slice(3)}`}>
      <span className="dot" aria-hidden>
        {STATUS_GLYPH[status]}
      </span>
      <span className="lbl">{meta.label}</span>
    </span>
  )
}

// HubAz is the right-rail A–Z quick-jump for the hub: '#' (for digit/symbol
// first-letters) + A–Z, with the present letters enabled. Clicking a letter
// filters the table to it; clicking the active letter clears the filter.
function HubAz({
  available,
  active,
  onJump,
}: {
  available: Set<string>
  active: string | null
  onJump: (letter: string) => void
}) {
  const letters = ['#', ...'ABCDEFGHIJKLMNOPQRSTUVWXYZ'.split('')]
  return (
    <nav className="az-jump idx-az" aria-label="Jump to letter" data-testid="idx-az">
      {letters.map((l) => {
        const has = available.has(l)
        const isActive = active === l
        return (
          <button
            key={l}
            type="button"
            className={`${has ? 'has' : ''} ${isActive ? 'active' : ''}`}
            disabled={!has}
            onClick={has ? () => onJump(l) : undefined}
            aria-label={has ? `Jump to ${l === '#' ? 'symbols' : l}` : undefined}
            data-testid={`idx-az-${l}`}
          >
            {l}
          </button>
        )
      })}
    </nav>
  )
}

// ColumnOptions is the "Options (column visibility)" toolbar control: a popover
// of checkboxes for the togglable columns.
function ColumnOptions({
  visible,
  onToggle,
}: {
  visible: Set<ColKey>
  onToggle: (k: ColKey) => void
}) {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!open) return
    function onDown(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') setOpen(false)
    }
    document.addEventListener('mousedown', onDown)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDown)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])
  return (
    <div className="idx-colmenu" ref={ref} data-testid="idx-colmenu">
      <TbButton icon="⚙" label="Options" onClick={() => setOpen((o) => !o)} testId="idx-options" title="Show / hide columns" />
      {open && (
        <div className="idx-colmenu-panel" role="menu" aria-label="Column visibility">
          {COLUMNS.filter((c) => c.togglable).map((c) => (
            <label key={c.key} data-testid={`idx-col-${c.key}`}>
              <input
                type="checkbox"
                className="idx-sel"
                checked={visible.has(c.key)}
                onChange={() => onToggle(c.key)}
              />
              {c.label}
            </label>
          ))}
        </div>
      )}
    </div>
  )
}

export function IndexerHubPage() {
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

  // ---- Prowlarr PR2: definition library (fetched once, filtered client-side) ----
  const [definitions, setDefinitions] = useState<IndexerDefinition[] | null>(null)
  const [defsLoading, setDefsLoading] = useState(true)
  const [defsError, setDefsError] = useState<string | null>(null)

  const loadDefinitions = useCallback(async () => {
    setDefsLoading(true)
    setDefsError(null)
    try {
      const defs = await client.listIndexerDefinitions()
      setDefinitions(defs)
    } catch (e) {
      setDefsError(e instanceof Error ? e.message : 'failed to load indexer definitions')
    } finally {
      setDefsLoading(false)
    }
  }, [])

  useEffect(() => {
    void loadDefinitions()
  }, [loadDefinitions])

  // ---- list state ---------------------------------------------------------
  const [sort, setSort] = useState<SortKey>('name-asc')
  const [filter, setFilter] = useState<FilterKey>('all')
  const [search, setSearch] = useState('')
  const [azLetter, setAzLetter] = useState<string | null>(null)
  const [visible, setVisible] = useState<Set<ColKey>>(new Set(DEFAULT_VISIBLE))

  // ---- test-all + per-row test results -----------------------------------
  const [testResults, setTestResults] = useState<Record<number, IndexerTestResult>>({})
  const [testAllBusy, setTestAllBusy] = useState(false)
  const [testAllMsg, setTestAllMsg] = useState<string | null>(null)

  // ---- select mode + bulk actions ----------------------------------------
  const [selectMode, setSelectMode] = useState(false)
  const [selected, setSelected] = useState<Set<number>>(new Set())
  const { busy: bulkBusy, submit: bulkSubmit } = useSubmit()

  // ---- add / edit modal (PR2 two-stage: picker -> form) -------------------
  const [showPicker, setShowPicker] = useState(false)
  const [showForm, setShowForm] = useState(false)
  const [editingId, setEditingId] = useState<number | null>(null)
  // The definition driving the form: a picked def (create) or the resolved
  // def (edit). null = legacy single-stage form (pre-PR2 indexers).
  const [selectedDef, setSelectedDef] = useState<IndexerDefinition | null>(null)
  const [syncNote, setSyncNote] = useState<string | null>(null)

  const displayed = useMemo(() => {
    if (!indexers) return []
    let items = [...indexers]
    if (filter !== 'all') items = items.filter((i) => protocolOf(i.kind) === filter)
    if (search.trim()) {
      const q = search.toLowerCase()
      items = items.filter(
        (i) =>
          i.name.toLowerCase().includes(q) ||
          i.kind.toLowerCase().includes(q) ||
          (i.base_url ?? '').toLowerCase().includes(q),
      )
    }
    if (azLetter) items = items.filter((i) => firstLetter(i.name) === azLetter)
    const dir = sort.endsWith('-desc') ? -1 : 1
    items.sort((a, b) => {
      let cmp = 0
      if (sort.startsWith('name')) cmp = a.name.localeCompare(b.name)
      else cmp = (a.priority ?? 25) - (b.priority ?? 25)
      if (cmp === 0) cmp = a.id - b.id
      return cmp * dir
    })
    return items
  }, [indexers, filter, search, azLetter, sort])

  const summary = useMemo(() => {
    if (!indexers) return null
    return {
      total: indexers.length,
      enabled: indexers.filter((i) => i.enabled).length,
      torrent: indexers.filter((i) => protocolOf(i.kind) === 'torrent').length,
      usenet: indexers.filter((i) => protocolOf(i.kind) === 'usenet').length,
    }
  }, [indexers])

  const azAvailable = useMemo(() => {
    const s = new Set<string>()
    for (const i of indexers ?? []) s.add(firstLetter(i.name))
    return s
  }, [indexers])

  const shownCols = useMemo(
    () => COLUMNS.filter((c) => !c.togglable || visible.has(c.key)),
    [visible],
  )
  const gridCols = shownCols.map((c) => c.width).join(' ')

  async function runTest(id: number) {
    try {
      const res = await client.testIndexer(id)
      setTestResults((prev) => ({ ...prev, [id]: res }))
    } finally {
      void load()
    }
  }

  async function runTestAll() {
    setTestAllBusy(true)
    setTestAllMsg(null)
    try {
      const results = await client.testAllIndexers()
      setTestResults((prev) => {
        const next = { ...prev }
        for (const r of results) next[r.id] = r
        return next
      })
      const ok = results.filter((r) => r.ok).length
      setTestAllMsg(`Tested ${results.length} indexer(s): ${ok} ok, ${results.length - ok} failed`)
    } catch (e) {
      setTestAllMsg(e instanceof Error ? e.message : 'Test All Indexers failed')
    } finally {
      setTestAllBusy(false)
      void load()
    }
  }

  function toggleVisible(k: ColKey) {
    setVisible((prev) => {
      const n = new Set(prev)
      if (n.has(k)) n.delete(k)
      else n.add(k)
      return n
    })
  }

  function openAdd() {
    setEditingId(null)
    setSelectedDef(null)
    setSyncNote(null)
    setShowPicker(true)
  }
  function openEdit(idx: Indexer) {
    setEditingId(idx.id)
    setSyncNote(null)
    // PR2: if the indexer was added from a library definition, resolve it so the
    // form can re-render the contract. A null/unknown definition_name falls back
    // to the legacy single-stage form (pre-PR2 indexers must keep working).
    const defName = idx.definition_name
    const def = defName
      ? (definitions ?? []).find((d) => d.id === defName || d.name === defName)
      : undefined
    setSelectedDef(def ?? null)
    setShowForm(true)
  }
  function pickDefinition(def: IndexerDefinition) {
    setShowPicker(false)
    setEditingId(null)
    setSelectedDef(def)
    setSyncNote(null)
    setShowForm(true)
  }
  function closeForm() {
    setShowForm(false)
    setEditingId(null)
    setSelectedDef(null)
  }

  function toggleSelect(id: number) {
    setSelected((prev) => {
      const n = new Set(prev)
      if (n.has(id)) n.delete(id)
      else n.add(id)
      return n
    })
  }
  function toggleSelectAll() {
    setSelected((prev) => (prev.size === displayed.length ? new Set() : new Set(displayed.map((i) => i.id))))
  }

  function bulkRun(fn: () => Promise<void>) {
    void bulkSubmit(async () => {
      await fn()
      setSelected(new Set())
      setSelectMode(false)
      void load()
    })
  }

  const editing = editingId != null ? indexers?.find((i) => i.id === editingId) ?? null : null

  return (
    <div className="list-page">
      <div className="dash-head">
        <h2 data-testid="idx-hub-title">Indexers</h2>
        <div className="dash-head-right">
          <button className="ghost" onClick={() => void load()} disabled={loading} data-testid="idx-refresh">
            Refresh
          </button>
        </div>
      </div>

      <Toolbar
        left={
          <>
            <TbButton icon="+" label="Add Indexer" onClick={openAdd} testId="idx-add" />
            <TbButton
              icon="🧪"
              label="Test All Indexers"
              onClick={() => void runTestAll()}
              disabled={testAllBusy || !indexers || indexers.length === 0}
              divider
              testId="idx-test-all"
            />
            <TbButton
              icon="☑"
              label="Select Indexers"
              onClick={() => {
                setSelectMode((m) => !m)
                setSelected(new Set())
              }}
              divider
              testId="idx-select"
            />
          </>
        }
        right={
          <>
            <ColumnOptions visible={visible} onToggle={toggleVisible} />
            <Menu
              label="Sort"
              icon="↕"
              options={[...SORT_OPTIONS]}
              selected={sort}
              onSelect={(v) => setSort(v as SortKey)}
              testId="idx-sort"
            />
            <Menu
              label="Filter"
              icon="⚲"
              options={[...FILTER_OPTIONS]}
              selected={filter}
              onSelect={(v) => setFilter(v as FilterKey)}
              testId="idx-filter"
            />
            <TbButton
              icon="⇆"
              label="Sync App Indexers"
              onClick={() =>
                setSyncNote(
                  'Sync App Indexers is replaced by per-kind sync in the definition library (a later Prowlarr phase); there is no cross-app indexer sync in PR1.',
                )
              }
              divider
              testId="idx-sync"
              title="Stub — replaced by per-kind sync"
            />
          </>
        }
      />

      {syncNote && (
        <p className="dim small" role="note" data-testid="idx-sync-note">
          {syncNote}
        </p>
      )}
      {testAllMsg && (
        <p className="dim small" role="status" data-testid="idx-test-all-msg">
          {testAllMsg}
        </p>
      )}

      <div className="list-controls" style={{ marginTop: '0.5rem' }}>
        <input
          className="list-search"
          placeholder="Search indexers…"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          data-testid="idx-search"
          aria-label="Search indexers"
        />
        <span className="list-count" data-testid="idx-count">
          {loading
            ? 'Loading…'
            : `${displayed.length} of ${indexers?.length ?? 0} indexers`}
        </span>
      </div>

      {error && !indexers ? (
        <div className="card" data-testid="idx-hub-error">
          <h3>Indexers unavailable</h3>
          <p className="error" data-testid="idx-hub-error-message">
            {error}
          </p>
          <button onClick={() => void load()}>Retry</button>
        </div>
      ) : (
        <>
          <div className="idx-hub-main" data-testid="idx-hub-main">
            <div className="idx-hub-table">
              <div className="idx-hub-head" style={{ gridTemplateColumns: gridCols }} data-testid="idx-head">
                {shownCols.map((c) => (
                  <span key={c.key} data-testid={`idx-head-${c.key}`}>
                    {c.key === 'name' && selectMode && (
                      <input
                        type="checkbox"
                        className="idx-sel"
                        checked={displayed.length > 0 && selected.size === displayed.length}
                        onChange={toggleSelectAll}
                        aria-label="Select all"
                        data-testid="idx-select-all"
                      />
                    )}
                    {c.label}
                  </span>
                ))}
              </div>
              {loading && !indexers ? (
                <p className="dim">Loading indexers…</p>
              ) : displayed.length === 0 ? (
                <EmptyState
                  title="No indexers"
                  message="Add your first indexer to enable search and downloads."
                  testId="idx-empty"
                >
                  <button onClick={openAdd} data-testid="idx-empty-add">
                    + Add Indexer
                  </button>
                </EmptyState>
              ) : (
                displayed.map((idx) => (
                  <IndexerRow
                    key={idx.id}
                    indexer={idx}
                    cols={shownCols}
                    gridCols={gridCols}
                    selectMode={selectMode}
                    selected={selected.has(idx.id)}
                    onToggleSelect={() => toggleSelect(idx.id)}
                    statusRes={testResults[idx.id]}
                    onTest={() => void runTest(idx.id)}
                    onEdit={() => openEdit(idx)}
                    onToggleEnable={async (next) => {
                      await client.setIndexerEnabled(idx.id, next)
                      void load()
                    }}
                    onDelete={async () => {
                      await client.deleteIndexer(idx.id)
                      void load()
                    }}
                  />
                ))
              )}
            </div>

            <HubAz
              available={azAvailable}
              active={azLetter}
              onJump={(l) => setAzLetter((prev) => (prev === l ? null : l))}
            />
          </div>

          {selectMode && selected.size > 0 && (
            <div className="idx-bulkbar" data-testid="idx-bulkbar">
              <span data-testid="idx-selected-count">
                {selected.size} selected
              </span>
              <button className="ghost small" disabled={bulkBusy} onClick={() => bulkRun(async () => { for (const id of selected) await client.setIndexerEnabled(id, true) })} data-testid="idx-bulk-enable">
                Enable
              </button>
              <button className="ghost small" disabled={bulkBusy} onClick={() => bulkRun(async () => { for (const id of selected) await client.setIndexerEnabled(id, false) })} data-testid="idx-bulk-disable">
                Disable
              </button>
              <button className="ghost small danger" disabled={bulkBusy} onClick={() => bulkRun(async () => { for (const id of selected) await client.deleteIndexer(id) })} data-testid="idx-bulk-delete">
                Delete
              </button>
            </div>
          )}

          {summary && (
            <div className="idx-summary" data-testid="idx-summary">
              <span className="legend lg-enabled">
                <span className="dot" aria-hidden /> Enabled
              </span>
              <span className="legend lg-redirect">
                <span className="dot" aria-hidden /> Enabled + Redirected
              </span>
              <span className="legend lg-disabled">
                <span className="dot" aria-hidden /> Disabled
              </span>
              <span className="legend lg-error">
                <span className="dot" aria-hidden /> Error
              </span>
              <span className="sep" aria-hidden>
                |
              </span>
              <span>
                Indexers <b data-testid="idx-sum-total">{summary.total}</b>
              </span>
              <span>
                Enabled <b data-testid="idx-sum-enabled">{summary.enabled}</b>
              </span>
              <span>
                Torrent <b data-testid="idx-sum-torrent">{summary.torrent}</b>
              </span>
              <span>
                Usenet <b data-testid="idx-sum-usenet">{summary.usenet}</b>
              </span>
            </div>
          )}
        </>
      )}

      {showPicker && (
        <DefinitionPicker
          definitions={definitions}
          loading={defsLoading}
          error={defsError}
          onPick={pickDefinition}
          onCancel={() => setShowPicker(false)}
        />
      )}
      {showForm && (
        <IndexerForm
          def={selectedDef}
          editing={editing}
          onCancel={closeForm}
          onSaved={(idx) => {
            setIndexers((prev) => {
              if (!prev) return [idx]
              const i = prev.findIndex((p) => p.id === idx.id)
              if (i === -1) return [...prev, idx]
              const next = [...prev]
              next[i] = idx
              return next
            })
            closeForm()
            void load()
          }}
        />
      )}
    </div>
  )
}

function IndexerRow({
  indexer,
  cols,
  gridCols,
  selectMode,
  selected,
  onToggleSelect,
  statusRes,
  onTest,
  onEdit,
  onToggleEnable,
  onDelete,
}: {
  indexer: Indexer
  cols: { key: ColKey; label: string; width: string; togglable: boolean }[]
  gridCols: string
  selectMode: boolean
  selected: boolean
  onToggleSelect: () => void
  statusRes: IndexerTestResult | undefined
  onTest: () => void
  onEdit: () => void
  onToggleEnable: (next: boolean) => Promise<void>
  onDelete: () => Promise<void>
}) {
  const [testBusy, setTestBusy] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const { busy: toggling, submit: toggleSubmit } = useSubmit()
  const { busy: deleting, submit: deleteSubmit } = useSubmit()

  async function handleTest() {
    setTestBusy(true)
    try {
      onTest()
    } finally {
      setTestBusy(false)
    }
  }
  function handleToggle() {
    void toggleSubmit(async () => {
      await onToggleEnable(!indexer.enabled)
    })
  }
  function handleDelete() {
    if (!confirmDelete) {
      setConfirmDelete(true)
      setTimeout(() => setConfirmDelete(false), 3000)
      return
    }
    void deleteSubmit(async () => {
      await onDelete()
    })
  }

  const status = statusOf(indexer, statusRes)
  const prot = protocolOf(indexer.kind)
  const privacy = indexer.privacy || 'public'
  const privacyCls = privacy === 'public' ? 'idx-priv-public' : privacy === 'private' ? 'idx-priv-private' : 'idx-priv-semi'

  return (
    <div
      className={`idx-hub-row ${selected ? 'sel' : ''}`}
      style={{ gridTemplateColumns: gridCols }}
      data-testid={`idx-row-${indexer.id}`}
    >
      {cols.map((c) => {
        switch (c.key) {
          case 'status':
            return <StatusIcon key={c.key} status={status} />
          case 'name':
            return (
              <span className="idx-hub-name" key={c.key} title={indexer.name}>
                {selectMode && (
                  <input
                    type="checkbox"
                    className="idx-sel"
                    checked={selected}
                    onChange={onToggleSelect}
                    aria-label={`Select ${indexer.name}`}
                    data-testid={`idx-sel-${indexer.id}`}
                  />
                )}
                {indexer.name}
              </span>
            )
          case 'protocol':
            return (
              <span className="idx-prot" key={c.key} data-testid={`idx-proto-${indexer.id}`}>
                {prot}
              </span>
            )
          case 'privacy':
            return (
              <span className={`idx-priv ${privacyCls}`} key={c.key} data-testid={`idx-priv-${indexer.id}`}>
                {privacy === 'semi-private' ? 'Semi-Private' : titleCase(privacy)}
              </span>
            )
          case 'priority':
            return (
              <span key={c.key} data-testid={`idx-prio-${indexer.id}`}>
                {indexer.priority ?? 25}
              </span>
            )
          case 'sync':
            return (
              <span className="lt-dim" key={c.key}>
                {indexer.sync_profile || 'standard'}
              </span>
            )
          case 'added':
            return (
              <span className="lt-dim" key={c.key}>
                {fmtAdded(indexer.added)}
              </span>
            )
          case 'categories':
            return (
              <span key={c.key} data-testid={`idx-cats-${indexer.id}`}>
                {indexer.categories && indexer.categories.length ? (
                  indexer.categories.map((cat) => (
                    <span className="idx-cat" key={cat}>
                      {cat}
                    </span>
                  ))
                ) : (
                  <span className="lt-dim">—</span>
                )}
              </span>
            )
          case 'actions':
            return (
              <span className="idx-hub-actions" key={c.key}>
                <button
                  className="ghost small"
                  onClick={handleToggle}
                  disabled={toggling}
                  data-testid={`idx-toggle-${indexer.id}`}
                >
                  {indexer.enabled ? 'Disable' : 'Enable'}
                </button>
                <button
                  className="ghost small"
                  onClick={() => void handleTest()}
                  disabled={testBusy}
                  data-testid={`idx-test-${indexer.id}`}
                >
                  {testBusy ? 'Testing…' : 'Test'}
                </button>
                <button className="ghost small" onClick={onEdit} data-testid={`idx-edit-${indexer.id}`}>
                  Edit
                </button>
                <button
                  className="ghost small danger"
                  onClick={handleDelete}
                  disabled={deleting}
                  data-testid={`idx-delete-${indexer.id}`}
                >
                  {confirmDelete ? 'Confirm?' : 'Delete'}
                </button>
              </span>
            )
        }
      })}
    </div>
  )
}

// LegacyIndexerForm is the ORIGINAL single-stage add/edit form (Prowlarr PR1):
// name, kind, base url, api key, enabled, search toggles, and privacy /
// priority / sync profile. It is kept intact for the legacy path — editing a
// pre-PR2 indexer (definition_name null) or any indexer whose definition can't
// be resolved still uses this exact form.
function LegacyIndexerForm({
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
  const [rss, setRss] = useState(editing?.rss ?? true)
  const [autoSearch, setAutoSearch] = useState(editing?.auto_search ?? true)
  const [interactiveSearch, setInteractiveSearch] = useState(editing?.interactive_search ?? true)
  const [privacy, setPrivacy] = useState<string>(editing?.privacy ?? 'public')
  const [priority, setPriority] = useState<string>(String(editing?.priority ?? 25))
  const [syncProfile, setSyncProfile] = useState(editing?.sync_profile ?? 'standard')
  const { busy, error, submit } = useSubmit()

  function handleSave() {
    void submit(async () => {
      const req = {
        name: name.trim(),
        kind,
        base_url: baseUrl,
        api_key: apiKey, // empty on update = keep the stored key
        rss,
        auto_search: autoSearch,
        interactive_search: interactiveSearch,
        privacy,
        priority: Number(priority) || 25,
        sync_profile: syncProfile,
        ...(editing ? {} : { enabled }),
      }
      const idx = editing
        ? await client.updateIndexer(editing.id, req as never)
        : await client.createIndexer(req as never)
      onSaved(idx)
    })
  }

  return (
    <Modal
      title={editing ? `Edit Indexer — ${editing.name}` : 'Add Indexer'}
      onClose={onCancel}
      testId="idx-form-modal"
      className="modal-wide"
      footer={
        <>
          <button onClick={handleSave} disabled={busy} data-testid="idx-save">
            {busy ? 'Saving…' : editing ? 'Save changes' : 'Add indexer'}
          </button>
          <button className="ghost" onClick={onCancel} data-testid="idx-cancel">
            Cancel
          </button>
        </>
      }
    >
      <div className="subform" data-testid="idx-form" style={{ display: 'block' }}>
        <label htmlFor="idx-name">Name</label>
        <input id="idx-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="e.g. nyaa" data-testid="idx-name" />
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
        <label htmlFor="idx-key">API key {editing ? '(blank to keep)' : ''}</label>
        <input
          id="idx-key"
          type="password"
          value={apiKey}
          onChange={(e) => setApiKey(e.target.value)}
          placeholder={editing && editing.has_api_key ? '••••••••' : ''}
          data-testid="idx-key"
        />
        {!editing && (
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
        )}
        {/* Prowlarr PR1 hub fields. */}
        <label htmlFor="idx-privacy">Privacy</label>
        <select id="idx-privacy" value={privacy} onChange={(e) => setPrivacy(e.target.value)} data-testid="idx-privacy">
          <option value="public">public</option>
          <option value="private">private</option>
          <option value="semi-private">semi-private</option>
        </select>
        <label htmlFor="idx-priority">Priority</label>
        <input
          id="idx-priority"
          type="number"
          value={priority}
          onChange={(e) => setPriority(e.target.value)}
          data-testid="idx-priority"
        />
        <label htmlFor="idx-sync">Sync Profile</label>
        <input
          id="idx-sync"
          value={syncProfile}
          onChange={(e) => setSyncProfile(e.target.value)}
          data-testid="idx-sync"
        />
        {/* Per-indexer search toggles (Radarr R5a). */}
        <label className="check" htmlFor="idx-rss">
          <input id="idx-rss" type="checkbox" checked={rss} onChange={(e) => setRss(e.target.checked)} data-testid="idx-rss" />
          RSS
        </label>
        <label className="check" htmlFor="idx-auto-search">
          <input
            id="idx-auto-search"
            type="checkbox"
            checked={autoSearch}
            onChange={(e) => setAutoSearch(e.target.checked)}
            data-testid="idx-auto-search"
          />
          Automatic search
        </label>
        <label className="check" htmlFor="idx-interactive-search">
          <input
            id="idx-interactive-search"
            type="checkbox"
            checked={interactiveSearch}
            onChange={(e) => setInteractiveSearch(e.target.checked)}
            data-testid="idx-interactive-search"
          />
          Interactive search
        </label>
        <SaveError error={error} testId="idx-form-error" />
      </div>
    </Modal>
  )
}

// ---- Prowlarr PR2: two-stage definition-library form + picker ----

// initBaseUrl seeds the Base Url field: the stored base_url when editing, else
// the definition's first candidate URL (empty when the definition has none).
function initBaseUrl(def: IndexerDefinition, editing: Indexer | null): string {
  if (editing?.base_url) return editing.base_url
  const urls = def.urls ?? []
  return urls.length >= 1 ? urls[0] : ''
}

// initFieldValues seeds every non-info, non-baseurl contract field: a stored
// value when editing (passwords arrive masked ''), else the field default, else
// the type's empty value.
function initFieldValues(
  def: IndexerDefinition,
  editing: Indexer | null,
): Record<string, string | number | boolean> {
  const stored = editing?.config_fields ?? {}
  const out: Record<string, string | number | boolean> = {}
  for (const f of def.fields) {
    if (f.type === 'info') continue
    if (f.name === 'baseurl') continue // represented by the Base Url field
    const s = stored[f.name]
    if (f.type === 'checkbox') {
      out[f.name] = typeof s === 'boolean' ? s : typeof f.default === 'boolean' ? f.default : false
    } else if (f.type === 'number') {
      if (typeof s === 'number') out[f.name] = s
      else if (typeof f.default === 'number') out[f.name] = f.default
      else out[f.name] = 0
    } else if (f.type === 'select') {
      const opts = f.options ?? {}
      if (typeof s === 'string' && opts[s] !== undefined) out[f.name] = s
      else if (typeof f.default === 'string' && opts[f.default] !== undefined) out[f.name] = f.default
      else out[f.name] = Object.keys(opts)[0] ?? ''
    } else {
      // textbox / password
      if (typeof s === 'string' || typeof s === 'number') out[f.name] = String(s)
      else if (typeof f.default === 'string') out[f.name] = f.default
      else out[f.name] = ''
    }
  }
  return out
}

// IndexerForm is the PR2 form-modal dispatcher: a chosen definition renders the
// new contract-driven form; a null definition (legacy indexer) renders the
// original single-stage LegacyIndexerForm unchanged.
function IndexerForm({
  def,
  editing,
  onSaved,
  onCancel,
}: {
  def: IndexerDefinition | null
  editing: Indexer | null
  onSaved: (idx: Indexer) => void
  onCancel: () => void
}) {
  if (!def) {
    return <LegacyIndexerForm editing={editing} onSaved={onSaved} onCancel={onCancel} />
  }
  return <DefinitionIndexerForm def={def} editing={editing} onSaved={onSaved} onCancel={onCancel} />
}

// DefinitionIndexerForm renders the generic hub fields + the definition's
// contract fields (in order, advanced tucked behind a toggle) and submits
// config_fields + definition_name alongside the hub fields.
function DefinitionIndexerForm({
  def,
  editing,
  onSaved,
  onCancel,
}: {
  def: IndexerDefinition
  editing: Indexer | null
  onSaved: (idx: Indexer) => void
  onCancel: () => void
}) {
  const isEdit = editing != null
  const [name, setName] = useState(editing?.name ?? '')
  const [baseUrl, setBaseUrl] = useState<string>(() => initBaseUrl(def, editing))
  const [enabled, setEnabled] = useState(editing?.enabled ?? true)
  const [rss, setRss] = useState(editing?.rss ?? true)
  const [autoSearch, setAutoSearch] = useState(editing?.auto_search ?? true)
  const [interactiveSearch, setInteractiveSearch] = useState(editing?.interactive_search ?? true)
  const [privacy, setPrivacy] = useState<string>(editing?.privacy ?? def.privacy ?? 'public')
  const [priority, setPriority] = useState<string>(String(editing?.priority ?? 25))
  const [syncProfile, setSyncProfile] = useState(editing?.sync_profile ?? 'standard')
  const [fieldValues, setFieldValues] = useState<Record<string, string | number | boolean>>(() =>
    initFieldValues(def, editing),
  )
  const [showAdvanced, setShowAdvanced] = useState(false)
  const { busy, error, submit } = useSubmit()

  // Candidate base URLs for the Base Url field. When there are several the
  // field is a select; the current value stays selectable even if it's a custom
  // base URL not in the definition's list.
  const urlOptions = useMemo(() => {
    const opts = [...new Set(def.urls ?? [])]
    if (baseUrl && !opts.includes(baseUrl)) opts.unshift(baseUrl)
    return opts
  }, [def.urls, baseUrl])

  const baseFields = useMemo(() => def.fields.filter((f) => !f.advanced), [def.fields])
  const advFields = useMemo(() => def.fields.filter((f) => f.advanced), [def.fields])

  function setField(name: string, value: string | number | boolean) {
    setFieldValues((prev) => ({ ...prev, [name]: value }))
  }

  function renderField(field: IndexerDefinitionField) {
    const { name, type, label, hint, required, options } = field
    if (type === 'info') {
      if (!label && !hint) return null
      return (
        <div className="idx-info" key={name} data-testid={`idx-field-${name}`}>
          {label && <span className="idx-info-title">{label}</span>}
          {hint && <span>{hint}</span>}
        </div>
      )
    }
    if (type === 'checkbox') {
      return (
        <label className="check" key={name} htmlFor={`idx-f-${name}`}>
          <input
            id={`idx-f-${name}`}
            type="checkbox"
            checked={fieldValues[name] === true}
            onChange={(e) => setField(name, e.target.checked)}
            data-testid={`idx-field-${name}`}
          />
          {label ?? name}
        </label>
      )
    }
    if (type === 'select') {
      const opts = options ?? {}
      return (
        <div key={name}>
          <label htmlFor={`idx-f-${name}`}>
            {label ?? name}
            {required && <span> *</span>}
          </label>
          <select
            id={`idx-f-${name}`}
            value={String(fieldValues[name] ?? '')}
            onChange={(e) => setField(name, e.target.value)}
            data-testid={`idx-field-${name}`}
          >
            {Object.entries(opts).map(([v, l]) => (
              <option key={v} value={v}>
                {l}
              </option>
            ))}
          </select>
          {hint && <p className="dim small">{hint}</p>}
        </div>
      )
    }
    // textbox / password / number
    const inputType = type === 'password' ? 'password' : type === 'number' ? 'number' : 'text'
    const value =
      type === 'number' ? (fieldValues[name] === undefined ? '' : String(fieldValues[name])) : String(fieldValues[name] ?? '')
    const placeholder = type === 'password' && isEdit ? '••••••••' : undefined
    return (
      <div key={name}>
        <label htmlFor={`idx-f-${name}`}>
          {label ?? name}
          {required && <span> *</span>}
          {type === 'password' && isEdit ? ' (blank to keep)' : ''}
        </label>
        <input
          id={`idx-f-${name}`}
          type={inputType}
          value={value}
          placeholder={placeholder}
          onChange={(e) =>
            setField(name, type === 'number' ? (e.target.value === '' ? '' : Number(e.target.value)) : e.target.value)
          }
          data-testid={`idx-field-${name}`}
        />
        {hint && <p className="dim small">{hint}</p>}
      </div>
    )
  }

  function validate(): string | null {
    if (!baseUrl.trim()) return 'Base Url is required'
    for (const f of def.fields) {
      if (f.type === 'info' || f.type === 'password') continue
      if (f.name === 'baseurl') continue
      if (!f.required) continue
      const v = fieldValues[f.name]
      if (v === undefined || v === '' || v === null) return `${f.label ?? f.name} is required`
    }
    return null
  }

  function handleSave() {
    void submit(async () => {
      const err = validate()
      if (err) throw new Error(err)
      const cfg: Record<string, string | number | boolean> = {}
      for (const f of def.fields) {
        if (f.type === 'info') continue
        if (f.name === 'baseurl') {
          cfg.baseurl = baseUrl
          continue
        }
        if (f.type === 'number') {
          const v = fieldValues[f.name]
          cfg[f.name] = typeof v === 'number' ? v : 0
        } else {
          const v = fieldValues[f.name]
          cfg[f.name] = typeof v === 'boolean' ? v : String(v ?? '')
        }
      }
      const base = {
        name: name.trim(),
        kind: kindFromDef(def),
        base_url: baseUrl,
        config_fields: cfg,
        definition_name: def.id,
        priority: Number(priority) || 25,
        privacy,
        sync_profile: syncProfile,
        rss,
        auto_search: autoSearch,
        interactive_search: interactiveSearch,
      }
      const idx = isEdit
        ? await client.updateIndexer(editing!.id, base as IndexerUpdate)
        : await client.createIndexer({ ...base, enabled } as IndexerCreate)
      onSaved(idx)
    })
  }

  return (
    <Modal
      title={isEdit ? `Edit Indexer — ${editing!.name}` : 'Add Indexer'}
      onClose={onCancel}
      testId="idx-form-modal"
      className="modal-wide"
      footer={
        <>
          <button onClick={handleSave} disabled={busy} data-testid="idx-save">
            {busy ? 'Saving…' : isEdit ? 'Save changes' : 'Add indexer'}
          </button>
          <button className="ghost" onClick={onCancel} data-testid="idx-cancel">
            Cancel
          </button>
        </>
      }
    >
      <div className="idx-form subform" data-testid="idx-form" style={{ display: 'block' }}>
        <label htmlFor="idx-name">Name</label>
        <input id="idx-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="e.g. nyaa" data-testid="idx-name" />
        {!isEdit && (
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
        )}
        <label htmlFor="idx-privacy">Privacy</label>
        <select id="idx-privacy" value={privacy} onChange={(e) => setPrivacy(e.target.value)} data-testid="idx-privacy">
          <option value="public">public</option>
          <option value="private">private</option>
          <option value="semi-private">semi-private</option>
        </select>
        <label htmlFor="idx-priority">Priority</label>
        <input
          id="idx-priority"
          type="number"
          value={priority}
          onChange={(e) => setPriority(e.target.value)}
          data-testid="idx-priority"
        />
        <label htmlFor="idx-sync">Sync Profile</label>
        <input
          id="idx-sync"
          value={syncProfile}
          onChange={(e) => setSyncProfile(e.target.value)}
          data-testid="idx-sync"
        />
        {/* Per-indexer search toggles (Radarr R5a). */}
        <label className="check" htmlFor="idx-rss">
          <input id="idx-rss" type="checkbox" checked={rss} onChange={(e) => setRss(e.target.checked)} data-testid="idx-rss" />
          RSS
        </label>
        <label className="check" htmlFor="idx-auto-search">
          <input
            id="idx-auto-search"
            type="checkbox"
            checked={autoSearch}
            onChange={(e) => setAutoSearch(e.target.checked)}
            data-testid="idx-auto-search"
          />
          Automatic search
        </label>
        <label className="check" htmlFor="idx-interactive-search">
          <input
            id="idx-interactive-search"
            type="checkbox"
            checked={interactiveSearch}
            onChange={(e) => setInteractiveSearch(e.target.checked)}
            data-testid="idx-interactive-search"
          />
          Interactive search
        </label>

        {/* Definition contract: Base Url (from the definition's candidate URLs)
            + the contract fields in order, advanced tucked behind a toggle. */}
        <label htmlFor="idx-base">Base Url</label>
        {urlOptions.length > 1 ? (
          <select
            id="idx-base"
            value={baseUrl}
            onChange={(e) => setBaseUrl(e.target.value)}
            data-testid="idx-base"
          >
            {urlOptions.map((u) => (
              <option key={u} value={u}>
                {u}
              </option>
            ))}
          </select>
        ) : (
          <input
            id="idx-base"
            value={baseUrl}
            onChange={(e) => setBaseUrl(e.target.value)}
            placeholder="https://…/api"
            data-testid="idx-base"
          />
        )}

        {baseFields.map(renderField)}

        {advFields.length > 0 && (
          <>
            <button
              type="button"
              className="ghost small"
              onClick={() => setShowAdvanced((s) => !s)}
              data-testid="idx-show-advanced"
            >
              {showAdvanced ? 'Hide Advanced' : 'Show Advanced'}
            </button>
            {showAdvanced && (
              <div className="idx-adv" data-testid="idx-advanced">
                {advFields.map(renderField)}
              </div>
            )}
          </>
        )}

        <SaveError error={error} testId="idx-form-error" />
      </div>
    </Modal>
  )
}

// DefinitionPicker is the first stage of the add flow: a searchable,
// filterable list of the built-in definitions. Picking a row opens the form
// prefilled with that definition. Filtering is client-side over the fetched
// list (584 rows — instant, and matches Prowlarr).
function DefinitionPicker({
  definitions,
  loading,
  error,
  onPick,
  onCancel,
}: {
  definitions: IndexerDefinition[] | null
  loading: boolean
  error: string | null
  onPick: (def: IndexerDefinition) => void
  onCancel: () => void
}) {
  const [search, setSearch] = useState('')
  const [protocol, setProtocol] = useState<string>('all')
  const [privacy, setPrivacy] = useState<string>('all')
  const [category, setCategory] = useState<string>('all')

  const categories = useMemo(() => {
    const s = new Set<string>()
    for (const d of definitions ?? []) for (const c of d.categories) s.add(c)
    return [...s].sort()
  }, [definitions])

  const rows = useMemo(() => {
    let items = definitions ?? []
    const q = search.trim().toLowerCase()
    if (q) items = items.filter((d) => d.name.toLowerCase().includes(q) || d.id.toLowerCase().includes(q))
    if (protocol !== 'all') items = items.filter((d) => d.protocol === protocol)
    if (privacy !== 'all') items = items.filter((d) => d.privacy === privacy)
    if (category !== 'all') items = items.filter((d) => d.categories.includes(category))
    return items
  }, [definitions, search, protocol, privacy, category])

  return (
    <Modal
      title="Add Indexer — Choose a definition"
      onClose={onCancel}
      testId="idx-picker-modal"
      className="modal-wide"
      footer={
        <button className="ghost" onClick={onCancel} data-testid="idx-picker-cancel">
          Cancel
        </button>
      }
    >
      <div className="idx-picker" data-testid="idx-picker">
        <input
          className="list-search"
          placeholder="Search definitions…"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          aria-label="Search definitions"
          data-testid="idx-picker-search"
        />
        <div className="idx-picker-controls">
          <div className="idx-picker-filters">
            <label>
              Protocol
              <select value={protocol} onChange={(e) => setProtocol(e.target.value)} data-testid="idx-picker-protocol">
                <option value="all">All</option>
                <option value="torrent">Torrent</option>
                <option value="usenet">Usenet</option>
              </select>
            </label>
            <label>
              Privacy
              <select value={privacy} onChange={(e) => setPrivacy(e.target.value)} data-testid="idx-picker-privacy">
                <option value="all">All</option>
                <option value="public">Public</option>
                <option value="private">Private</option>
                <option value="semi-private">Semi-private</option>
              </select>
            </label>
            <label>
              Category
              <select value={category} onChange={(e) => setCategory(e.target.value)} data-testid="idx-picker-category">
                <option value="all">All</option>
                {categories.map((c) => (
                  <option key={c} value={c}>
                    {c}
                  </option>
                ))}
              </select>
            </label>
          </div>
          <span className="idx-def-count" data-testid="idx-picker-count">
            {loading
              ? 'Loading definitions…'
              : `${rows.length} of ${definitions?.length ?? 0} definitions`}
          </span>
        </div>
        {error && !definitions ? (
          <p className="error" data-testid="idx-picker-error">
            {error}
          </p>
        ) : (
          <div className="idx-picker-list">
            {rows.map((d) => {
              const privCls =
                d.privacy === 'public' ? 'idx-priv-public' : d.privacy === 'private' ? 'idx-priv-private' : 'idx-priv-semi'
              return (
                <button
                  key={d.id}
                  type="button"
                  className="idx-def-row"
                  onClick={() => onPick(d)}
                  data-testid={`idx-def-${d.id}`}
                >
                  <span className="idx-def-name" title={d.name}>
                    {d.name}
                  </span>
                  <span className="idx-prot">{d.protocol}</span>
                  <span className={`idx-priv ${privCls}`}>
                    {d.privacy === 'semi-private' ? 'Semi-Private' : titleCase(d.privacy)}
                  </span>
                </button>
              )
            })}
            {rows.length === 0 && (
              <p className="dim" style={{ padding: '0.75rem' }}>
                No definitions match.
              </p>
            )}
          </div>
        )}
      </div>
    </Modal>
  )
}

