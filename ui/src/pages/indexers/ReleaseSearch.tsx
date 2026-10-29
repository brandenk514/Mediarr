import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { ApiError, client } from '../../api/client'
import type { Indexer, SearchRelease, SearchRequest } from '../../api/types'
import { EmptyState, Toolbar } from '../../components/ui'

// ReleaseSearchPage is the Prowlarr PR3 Search page (plan §2 "Search"): a
// manual multi-indexer search form — Query, Indexers (multi, from the enabled
// indexers), Categories (multi, the union of the enabled indexers' category
// sets), "Search all indexers" (checked ⇒ no indexer_ids sent), and Search —
// above a results table of candidate releases with a per-row Grab that routes
// the release to the configured download client.
export function ReleaseSearchPage() {
  // ---- form state ----
  const [query, setQuery] = useState('')
  const [searchAll, setSearchAll] = useState(true)
  // Selected indexer ids as strings (the MultiSelect works on string values;
  // they map back to numbers in the search request).
  const [selectedIdx, setSelectedIdx] = useState<Set<string>>(new Set())
  const [selectedCats, setSelectedCats] = useState<Set<string>>(new Set())

  // Enabled indexers feed the Indexers multi-select (and the category union).
  const [indexers, setIndexers] = useState<Indexer[] | null>(null)
  const [indexersError, setIndexersError] = useState<string | null>(null)

  const loadIndexers = useCallback(async () => {
    try {
      const list = await client.listIndexers()
      setIndexers(list)
      setIndexersError(null)
    } catch (e) {
      setIndexersError(e instanceof Error ? e.message : 'failed to load indexers')
    }
  }, [])

  useEffect(() => {
    void loadIndexers()
  }, [loadIndexers])

  const enabledIndexers = useMemo(() => (indexers ?? []).filter((i) => i.enabled), [indexers])
  const categoryOptions = useMemo(() => {
    const s = new Set<string>()
    for (const i of enabledIndexers) for (const c of i.categories ?? []) s.add(c)
    return [...s].sort()
  }, [enabledIndexers])

  function toggleIndexer(value: string) {
    setSelectedIdx((prev) => {
      const n = new Set(prev)
      if (n.has(value)) n.delete(value)
      else n.add(value)
      return n
    })
  }
  function toggleCategory(c: string) {
    setSelectedCats((prev) => {
      const n = new Set(prev)
      if (n.has(c)) n.delete(c)
      else n.add(c)
      return n
    })
  }

  // ---- search + results ----
  const [searching, setSearching] = useState(false)
  const [searchError, setSearchError] = useState<string | null>(null)
  const [results, setResults] = useState<SearchRelease[] | null>(null)
  const [searched, setSearched] = useState(false)
  const [hasQuery, setHasQuery] = useState(false)

  const canSearch = query.trim().length > 0

  async function runSearch() {
    if (!canSearch) return
    setSearching(true)
    setSearchError(null)
    try {
      const req: SearchRequest = { query: query.trim() }
      // "Search all indexers" checked ⇒ send no indexer_ids (server searches
      // every enabled indexer); unchecked ⇒ only the selected ones.
      if (!searchAll) req.indexer_ids = [...selectedIdx].map(Number)
      if (selectedCats.size > 0) req.categories = [...selectedCats].sort()
      const rows = await client.searchReleases(req)
      setResults(rows)
      setSearched(true)
      setHasQuery(true)
    } catch (e) {
      setSearchError(e instanceof Error ? e.message : 'search failed')
    } finally {
      setSearching(false)
    }
  }

  // ---- grab state (per result row, keyed by row index — no dedup) ----
  const [grabState, setGrabState] = useState<Record<number, 'busy' | 'done'>>({})
  const [grabError, setGrabError] = useState<Record<number, string>>({})

  async function grab(row: SearchRelease, i: number) {
    setGrabState((prev) => ({ ...prev, [i]: 'busy' }))
    setGrabError((prev) => ({ ...prev, [i]: '' }))
    try {
      await client.grabSearchRelease({
        title: row.title,
        indexer: row.indexer,
        protocol: row.protocol,
        url: row.url,
        size_bytes: row.size_bytes,
        seeders: row.seeders,
      })
      setGrabState((prev) => ({ ...prev, [i]: 'done' }))
    } catch (e) {
      // 409: the configured client can't handle this protocol.
      // 503: no download client configured.
      const msg = e instanceof ApiError ? `(${e.status}) ${e.message}` : e instanceof Error ? e.message : 'grab failed'
      setGrabError((prev) => ({ ...prev, [i]: msg }))
    }
  }

  return (
    <div className="list-page">
      <div className="dash-head">
        <h2 data-testid="release-search-title">Search</h2>
      </div>
      <Toolbar
        left={
          <>
            <span className="tb-note dim small" data-testid="release-search-hint">
              {searched
                ? hasQuery
                  ? `${results?.length ?? 0} release${(results?.length ?? 0) === 1 ? '' : 's'}`
                  : 'No releases'
                : 'Manual multi-indexer search'}
            </span>
          </>
        }
        right={
          <button className="ghost" onClick={() => void loadIndexers()} data-testid="release-search-refresh">
            Refresh
          </button>
        }
      />

      {/* Search form (Prowlarr: Query · Indexers · Categories · Search all · Search). */}
      <div className="card search-form" data-testid="release-search-form">
        <label htmlFor="release-search-query" className="search-field-label">
          Query
        </label>
        <input
          id="release-search-query"
          className="list-search search-query"
          placeholder="Search terms (release name, partial match)"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          data-testid="release-search-query"
          aria-label="Search query"
        />
        <div className="search-controls">
          <MultiSelect
            label="Indexers"
            icon="🧲"
            options={enabledIndexers.map((i) => ({ value: String(i.id), label: i.name }))}
            selected={selectedIdx}
            onToggle={toggleIndexer}
            disabled={searchAll}
            testId="release-search-indexers"
          />
          <MultiSelect
            label="Categories"
            icon="⌗"
            options={categoryOptions.map((c) => ({ value: c, label: c }))}
            selected={selectedCats}
            onToggle={toggleCategory}
            testId="release-search-categories"
          />
          <label className="check" htmlFor="release-search-all">
            <input
              id="release-search-all"
              type="checkbox"
              className="idx-sel"
              checked={searchAll}
              onChange={(e) => setSearchAll(e.target.checked)}
              data-testid="release-search-all"
            />
            Search all indexers
          </label>
          <button
            onClick={() => void runSearch()}
            disabled={!canSearch || searching}
            data-testid="release-search-btn"
          >
            {searching ? 'Searching…' : 'Search'}
          </button>
        </div>
        {indexersError && (
          <p className="error small" data-testid="release-search-indexers-error">
            {indexersError}
          </p>
        )}
      </div>

      {searchError && (
        <p className="error" role="alert" data-testid="release-search-error">
          {searchError}
        </p>
      )}

      {/* Results table. */}
      {searching ? (
        <div className="card" data-testid="release-search-loading">
          <p className="dim">Searching indexers…</p>
        </div>
      ) : !searched ? (
        <EmptyState
          title="Search your indexers"
          message="Enter a query and press Search — results fan out over the selected (or all enabled) indexers."
          testId="release-search-idle"
        />
      ) : (results?.length ?? 0) === 0 ? (
        <EmptyState
          title="No releases found"
          message={`No indexers returned a release for “${query.trim()}”.`}
          testId="release-search-empty"
        />
      ) : (
        <div className="release-table" data-testid="release-search-results">
          <div className="release-head" data-testid="release-head">
            <span>Title</span>
            <span>Indexer</span>
            <span>Size</span>
            <span>Seeders</span>
            <span>Protocol</span>
            <span>Category</span>
            <span>Actions</span>
          </div>
          {(results ?? []).map((row, i) => (
            <ReleaseRow
              key={i}
              row={row}
              rowIndex={i}
              state={grabState[i] ?? 'idle'}
              error={grabError[i] ?? ''}
              onGrab={() => void grab(row, i)}
            />
          ))}
        </div>
      )}
    </div>
  )
}

// ReleaseRow is one result row: title, indexer, size, seeders, protocol,
// category, and the per-row Grab action (Grabbed ✓ on success, the error
// message on 409/503).
function ReleaseRow({
  row,
  rowIndex,
  state,
  error,
  onGrab,
}: {
  row: SearchRelease
  rowIndex: number
  state: 'idle' | 'busy' | 'done'
  error: string
  onGrab: () => void
}) {
  return (
    <div className="release-row" data-testid={`release-row-${rowIndex}`}>
      <span className="release-title" title={row.title}>
        {row.title}
      </span>
      <span data-testid={`release-indexer-${rowIndex}`}>{row.indexer}</span>
      <span>{fmtBytes(row.size_bytes)}</span>
      <span data-testid={`release-seeders-${rowIndex}`}>{row.seeders ?? 0}</span>
      <span className="idx-prot">{row.protocol}</span>
      <span>{row.category || '—'}</span>
      <span className="release-actions">
        <button className="ghost small" onClick={onGrab} disabled={state === 'busy'} data-testid={`release-grab-${rowIndex}`}>
          {state === 'busy' ? 'Grabbing…' : state === 'done' ? 'Grabbed ✓' : 'Grab'}
        </button>
        {error && (
          <span className="error small" data-testid={`release-error-${rowIndex}`}>
            {error}
          </span>
        )}
      </span>
    </div>
  )
}

// MultiSelect is the compact checkbox dropdown for the Indexers / Categories
// form controls (toggle button + outside-click-closing panel). `disabled`
// greys the control out (the Indexers picker is ignored while "Search all
// indexers" is checked).
function MultiSelect({
  label,
  icon,
  options,
  selected,
  onToggle,
  disabled,
  testId,
}: {
  label: string
  icon?: string
  options: { value: string; label: string }[]
  selected: Set<string>
  onToggle: (value: string) => void
  disabled?: boolean
  testId: string
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
    <div className={`menu ${disabled ? 'menu-disabled' : ''}`} ref={ref} data-testid={testId}>
      <button
        type="button"
        className="menu-btn"
        onClick={() => !disabled && setOpen((o) => !o)}
        disabled={disabled}
        data-testid={`${testId}-btn`}
      >
        {icon && <span aria-hidden>{icon}</span>}
        {label}
        {selected.size > 0 && <span className="menu-count">{selected.size}</span>}
        <span className="rail-caret" aria-hidden>
          ▾
        </span>
      </button>
      {open && (
        <div className="menu-panel" role="menu" data-testid={`${testId}-panel`}>
          {options.length === 0 ? (
            <p className="dim small" style={{ padding: '0.5rem' }}>
              None available
            </p>
          ) : (
            options.map((o) => (
              <label key={o.value} className="multi-opt">
                <input
                  type="checkbox"
                  className="idx-sel"
                  checked={selected.has(o.value)}
                  onChange={() => onToggle(o.value)}
                  data-testid={`${testId}-opt-${o.value}`}
                />
                {o.label}
              </label>
            ))
          )}
        </div>
      )}
    </div>
  )
}

// fmtBytes renders a human release size (Prowlarr style: 1.4 GB, 256 MB).
function fmtBytes(n?: number): string {
  if (!n || n <= 0) return '—'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let v = n
  let u = 0
  while (v >= 1024 && u < units.length - 1) {
    v /= 1024
    u++
  }
  return `${v.toFixed(u === 0 ? 0 : 1)} ${units[u]}`
}
