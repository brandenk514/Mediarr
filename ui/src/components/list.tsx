// Shared building blocks for the four per-kind list views (#41):
//  - usePagedList: server-side filtered + paginated data loading with a
//    debounced search box and optimistic child-collapsing on query change.
//  - ListControls: the search box + monitored filter + refresh row.
//  - MonitoredToggle: optimistic on/off pill that rolls back on API failure.
//  - Pagination: the page controls.
//
// Each per-kind page renders its own expandable row header inline (rather than
// through a shared wrapper) because the row header pairs a chevron + title
// button with a MonitoredToggle sibling — a button cannot legally nest inside
// another button, so the two controls must be siblings.
import { useCallback, useEffect, useRef, useState } from 'react'
import type { ListQuery } from '../api/types'

export type MonitoredFilter = 'all' | 'monitored' | 'unmonitored'

export const DEFAULT_PAGE_SIZE = 25
export const SEARCH_DEBOUNCE_MS = 300

// usePagedList drives one server-side filtered, paginated list. `load` takes
// the full wire query (q/monitored/page/page_size) and returns the envelope.
// P is the generated page type (PageMovie, PageSeries, ...) — the generic
// constraint only names the envelope fields this hook reads. Search is
// debounced; changing the search resets the page to 1 and collapses any
// expanded children so a stale expanded row can't outlive its item.
//
// mode (#150): 'page' (default) keeps the existing page semantics — `data`
// is the current page and the view renders Pagination chrome. 'append' is
// the grid's infinite scroll: `items` accumulates pages as `loadMore`
// fires (no pagination chrome), a query/sort/filter change or a reload
// restarts from page 1, and `hasMore` reports whether more pages exist.
// In append mode `load` must be memoized (useCallback) with dependencies
// that only change when the request CONTENT changes (sort/facets) — its
// identity is what signals a content change to the restart logic.
export function usePagedList<P extends { items: unknown[]; total: number }>(
  load: (q: ListQuery) => Promise<P>,
  opts?: { mode?: 'page' | 'append' },
) {
  const mode = opts?.mode ?? 'page'
  const [text, setText] = useState('')
  const [q, setQ] = useState('')
  const [monitored, setMonitored] = useState<MonitoredFilter>('all')
  const [page, setPage] = useState(1)
  const [data, setData] = useState<P | null>(null)
  const [accum, setAccum] = useState<P['items']>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [expanded, setExpanded] = useState<Set<number>>(() => new Set())
  const [bump, setBump] = useState(0)
  const reqId = useRef(0)
  const lastPage = useRef(0)

  // Debounce the free-text search box into `q`.
  useEffect(() => {
    const timer = setTimeout(() => {
      setQ(text.trim())
      setPage(1)
    }, SEARCH_DEBOUNCE_MS)
    return () => clearTimeout(timer)
  }, [text])

  // Append mode: whenever the request CONTENT changes (a new load identity —
  // the page's sort/facet change — a new debounced query, a monitored filter)
  // or the mode flips, restart the accumulation from page 1. Without this, a
  // mid-scroll change keeps the stale cursor and the grid resumes from the
  // wrong page with a hole in the list.
  useEffect(() => {
    if (mode === 'append') setPage(1)
  }, [load, q, monitored, mode])

  useEffect(() => {
    let cancelled = false
    const id = ++reqId.current
    const query: ListQuery = { page, page_size: DEFAULT_PAGE_SIZE }
    if (q) query.q = q
    if (monitored !== 'all') query.monitored = monitored === 'monitored'
    setLoading(true)
    load(query)
      .then((p) => {
        if (cancelled || id !== reqId.current) return
        setData(p)
        setError(null)
        if (mode === 'append') {
          // Decide append-vs-replace BEFORE the setAccum updater runs: the
          // updater executes later (at render time), after lastPage.current
          // has already been advanced below, so reading the ref inside it
          // would always see the already-updated value.
          const isFirstPage = page === 1
          const isNextPage = page === lastPage.current + 1
          if (!isFirstPage && !isNextPage) {
            // Stale out-of-sequence response (e.g. a sort flip fired this
            // fetch from the old cursor before the restart above landed):
            // drop it — the restart reloads page 1 and rebuilds the list.
            return
          }
          // Only a strictly-next page appends; a fresh start (or an empty
          // accumulation) replaces so the grid never shows duplicated rows.
          setAccum((prev) => (isFirstPage || prev.length === 0 ? p.items : [...prev, ...p.items]))
          lastPage.current = page
        }
      })
      .catch((e: unknown) => {
        if (cancelled || id !== reqId.current) return
        setError(e instanceof Error ? e.message : 'failed to load list')
      })
      .finally(() => {
        if (!cancelled && id === reqId.current) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [load, q, monitored, page, bump, mode])

  const reload = useCallback(() => {
    // append mode restarts the accumulation from page 1 on a reload.
    if (mode === 'append') setPage(1)
    setBump((b) => b + 1)
  }, [mode])
  const toggleExpanded = useCallback((id: number) => {
    setExpanded((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }, [])

  const total = data?.total ?? 0
  const items = mode === 'append' ? accum : ((data?.items ?? []) as P['items'])

  return {
    text,
    setText,
    q,
    setQ,
    monitored,
    // In append mode a monitored change restarts the grid from page 1.
    setMonitored: useCallback(
      (m: MonitoredFilter) => {
        setMonitored(m)
        if (mode === 'append') setPage(1)
      },
      [mode],
    ),
    page,
    setPage,
    data,
    items,
    // append mode: the infinite-scroll surface.
    hasMore: mode === 'append' && items.length < total,
    loadMore: useCallback(() => setPage((p) => p + 1), []),
    loading,
    error,
    reload,
    expanded,
    toggleExpanded,
    total,
  }
}

// ListControls renders the search box, monitored filter segments, and the
// refresh button. The monitored filter is a three-way segmented control:
// All / Monitored / Unmonitored.
export function ListControls({
  text,
  onText,
  monitored,
  onMonitored,
  onRefresh,
  total,
  busy,
}: {
  text: string
  onText: (s: string) => void
  monitored: MonitoredFilter
  onMonitored: (m: MonitoredFilter) => void
  onRefresh: () => void
  total: number
  busy: boolean
}) {
  return (
    <div className="list-controls" data-testid="list-controls">
      <input
        className="list-search"
        type="search"
        placeholder="Search…"
        value={text}
        onChange={(e) => onText(e.target.value)}
        data-testid="list-search"
        aria-label="Search"
      />
      <div className="segmented" role="group" aria-label="Filter by monitored">
        {(['all', 'monitored', 'unmonitored'] as const).map((m) => (
          <button
            key={m}
            type="button"
            className={m === monitored ? 'seg active' : 'seg'}
            onClick={() => onMonitored(m)}
            data-testid={`filter-${m}`}
          >
            {m === 'all' ? 'All' : m === 'monitored' ? 'Monitored' : 'Unmonitored'}
          </button>
        ))}
      </div>
      <span className="dim small list-count">
        {total} {total === 1 ? 'item' : 'items'}
      </span>
      <button className="ghost" onClick={onRefresh} disabled={busy} data-testid="list-refresh">
        Refresh
      </button>
    </div>
  )
}

// MonitoredToggle is the optimistic on/off control. It calls `set` and, if the
// request rejects, rolls the visual state back and surfaces the error — so a
// flaky network never leaves the UI showing a monitor state the server
// didn't apply.
export function MonitoredToggle({
  monitored,
  onToggle,
  label,
  testId,
}: {
  monitored: boolean
  onToggle: (next: boolean) => Promise<void>
  label?: string
  testId?: string
}) {
  const [busy, setBusy] = useState(false)
  const [display, setDisplay] = useState(monitored)
  useEffect(() => {
    setDisplay(monitored)
  }, [monitored])

  async function handleClick() {
    if (busy) return
    const next = !display
    setBusy(true)
    setDisplay(next) // optimistic
    try {
      await onToggle(next)
    } catch (e) {
      setDisplay(monitored) // roll back
      console.error('monitored toggle failed:', e)
    } finally {
      setBusy(false)
    }
  }

  return (
    <button
      type="button"
      className={`mon-toggle ${display ? 'on' : 'off'} ${busy ? 'busy' : ''}`}
      onClick={() => void handleClick()}
      disabled={busy}
      aria-pressed={display}
      aria-label={label ?? (display ? 'Monitored — click to unmonitor' : 'Unmonitored — click to monitor')}
      data-testid={testId ?? (display ? 'monitored-on' : 'monitored-off')}
    >
      {display ? 'monitored' : 'off'}
    </button>
  )
}

// Pagination is the bottom page control. It renders nothing when there is at
// most one page, so flat single-page lists stay clean.
export function Pagination({
  page,
  pageSize,
  total,
  onPage,
}: {
  page: number
  pageSize: number
  total: number
  onPage: (page: number) => void
}) {
  const pages = Math.max(1, Math.ceil(total / pageSize))
  if (pages <= 1) return null
  return (
    <div className="pagination" data-testid="pagination">
      <button className="ghost" disabled={page <= 1} onClick={() => onPage(page - 1)} data-testid="page-prev">
        ← Prev
      </button>
      <span className="dim small">
        page {page} of {pages}
      </span>
      <button className="ghost" disabled={page >= pages} onClick={() => onPage(page + 1)} data-testid="page-next">
        Next →
      </button>
    </div>
  )
}
