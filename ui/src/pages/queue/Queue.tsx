import { useCallback, useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { ApiError, client, connectEvents } from '../../api/client'
import type { EventFrame, MediaKind, PageQueueItem, QueueHistoryEvent, QueueViewItem, QueueState } from '../../api/types'
import { DEFAULT_PAGE_SIZE } from '../../components/list'
import { Pagination } from '../../components/list'
import { progressLabel, timeAgo } from '../../lib/format'
import { LiveBadge } from '../dashboard/LiveBadge'

// QueueFilter is the UI's local filter state: an empty string means "all"
// (the server treats an absent/empty kind/state as unfiltered).
type KindFilter = MediaKind | ''
type StateFilter = QueueState | ''

const KINDS: MediaKind[] = ['movie', 'tv', 'music', 'book']
const STATES: QueueState[] = ['queued', 'downloading', 'complete', 'failed']

// historyPillClass maps a lifecycle-event name to a pill color class so a row's
// history reads at a glance (terminal-success ok, in-flight warn, failure err,
// everything else idle). Unknown event names fall back to idle rather than
// leaking a bogus class.
function historyPillClass(event: string): string {
  const e = event.toLowerCase()
  if (e.includes('import') || e === 'complete' || e.includes('download') && !e.includes('downloading')) {
    return 'pill-ok'
  }
  if (e.includes('fail') || e === 'error') return 'pill-err'
  if (e.includes('download') || e.includes('search') || e === 'queued' || e === 'added') return 'pill-warn'
  return 'pill-idle'
}

// QueuePage is the dedicated queue view (#43): the *full* download/import
// queue across all four kinds — filtered (kind, state, search) and paginated
// server-side — with a live WebSocket link that re-reads the list when the
// pipelines push queue.updated frames, and a per-row history drill-down that
// expands a job's lifecycle events for its parent.
export function QueuePage() {
  const [data, setData] = useState<PageQueueItem | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [authLost, setAuthLost] = useState(false)
  const [loading, setLoading] = useState(true)
  const [live, setLive] = useState<'connecting' | 'open' | 'closed'>('connecting')

  // Filters (local UI state; the server filters + pages).
  const [text, setText] = useState('')
  const [q, setQ] = useState('')
  const [kind, setKind] = useState<KindFilter>('')
  const [state, setState] = useState<StateFilter>('')
  const [page, setPage] = useState(1)
  const reqId = useRef(0)

  // History drill-down: the currently expanded row (one at a time).
  const [expanded, setExpanded] = useState<string | null>(null) // `${kind}:${parent_id}`
  const [history, setHistory] = useState<QueueHistoryEvent[]>([])
  const [historyLoading, setHistoryLoading] = useState(false)
  const [historyError, setHistoryError] = useState<string | null>(null)

  const loadQueue = useCallback(async () => {
    const id = ++reqId.current
    setLoading(true)
    try {
      const p = await client.listQueue({
        q: q || undefined,
        kind: kind || undefined,
        state: state || undefined,
        page,
        page_size: DEFAULT_PAGE_SIZE,
      })
      if (id !== reqId.current) return
      setData(p)
      setError(null)
      setAuthLost(false)
    } catch (e) {
      if (id !== reqId.current) return
      if (e instanceof ApiError && e.status === 401) setAuthLost(true)
      setError(e instanceof Error ? e.message : 'failed to load queue')
    } finally {
      if (id === reqId.current) setLoading(false)
    }
  }, [q, kind, state, page])

  // Debounce the search box into `q`, resetting the page on each change.
  useEffect(() => {
    const timer = setTimeout(() => {
      setQ(text.trim())
      setPage(1)
    }, 300)
    return () => clearTimeout(timer)
  }, [text])

  // Initial load + reload when filters/page change.
  useEffect(() => {
    void loadQueue()
  }, [loadQueue])

  // Live updates: on a queue.updated frame, re-read the list (debounced so a
  // burst coalesces into one fetch) and re-fetch the expanded row's history so
  // a just-changed job shows its newest event.
  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | undefined
    const close = connectEvents(
      (frame: EventFrame) => {
        if (frame.type !== 'queue.updated' && frame.type !== 'history.appended') return
        if (timer) return
        timer = setTimeout(() => {
          timer = undefined
          void loadQueue()
        }, 500)
      },
      (status) => setLive(status),
    )
    return () => {
      if (timer) clearTimeout(timer)
      close()
    }
  }, [loadQueue])

  // Load the expanded row's parent history when a row is opened.
  useEffect(() => {
    if (!expanded) {
      setHistory([])
      setHistoryError(null)
      return
    }
    const [k, idStr] = expanded.split(':')
    const parentId = Number(idStr)
    if (!k || !parentId || Number.isNaN(parentId)) return
    let cancelled = false
    setHistoryLoading(true)
    setHistoryError(null)
    client.queueHistory(k as MediaKind, parentId, 50)
      .then((ev) => {
        if (!cancelled) setHistory(ev)
      })
      .catch((e: unknown) => {
        if (!cancelled) setHistoryError(e instanceof Error ? e.message : 'failed to load history')
      })
      .finally(() => {
        if (!cancelled) setHistoryLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [expanded])

  function toggleHistory(item: QueueViewItem) {
    const key = `${item.kind}:${item.parent_id}`
    setExpanded((prev) => (prev === key ? null : key))
  }

  if (error && !data) {
    return (
      <div className="card" data-testid="queue-error">
        <h2>Queue unavailable</h2>
        <p className="error" data-testid="queue-error-message">
          {error}
        </p>
        {authLost && (
          <p>
            <Link to="/login">Sign in again</Link>
          </p>
        )}
        <button onClick={() => void loadQueue()}>Retry</button>
      </div>
    )
  }

  return (
    <div className="list-page">
      <div className="dash-head">
        <h2 data-testid="queue-title">Queue</h2>
        <div className="dash-head-right">
          <LiveBadge status={live} />
          {error && data && (
            <span className="error small" data-testid="queue-refresh-error">
              {error}
            </span>
          )}
          <button
            className="ghost"
            onClick={() => void loadQueue()}
            disabled={loading}
            data-testid="queue-refresh"
          >
            Refresh
          </button>
        </div>
      </div>

      <div className="list-controls" data-testid="queue-controls">
        <input
          className="list-search"
          type="search"
          placeholder="Search releases or media…"
          value={text}
          onChange={(e) => setText(e.target.value)}
          data-testid="queue-search"
          aria-label="Search the queue"
        />
        <div className="segmented" role="group" aria-label="Filter by kind">
          <button
            type="button"
            className={kind === '' ? 'seg active' : 'seg'}
            onClick={() => {
              setKind('')
              setPage(1)
            }}
            data-testid="queue-kind-all"
          >
            All
          </button>
          {KINDS.map((k) => (
            <button
              key={k}
              type="button"
              className={kind === k ? 'seg active' : 'seg'}
              onClick={() => {
                setKind(k)
                setPage(1)
              }}
              data-testid={`queue-kind-${k}`}
            >
              {k}
            </button>
          ))}
        </div>
        <div className="segmented" role="group" aria-label="Filter by state">
          <button
            type="button"
            className={state === '' ? 'seg active' : 'seg'}
            onClick={() => {
              setState('')
              setPage(1)
            }}
            data-testid="queue-state-all"
          >
            All
          </button>
          {STATES.map((s) => (
            <button
              key={s}
              type="button"
              className={state === s ? 'seg active' : 'seg'}
              onClick={() => {
                setState(s)
                setPage(1)
              }}
              data-testid={`queue-state-${s}`}
            >
              {s}
            </button>
          ))}
        </div>
        <span className="dim small list-count" data-testid="queue-count">
          {data?.total ?? 0} {data?.total === 1 ? 'job' : 'jobs'}
        </span>
      </div>

      <div className="list-table" data-testid="queue-table">
        <div className="lt-head">
          <span>Release</span>
          <span>Media</span>
          <span>Indexer</span>
          <span>State</span>
          <span>Updated</span>
        </div>
        {loading && !data ? (
          <p className="dim">Loading queue…</p>
        ) : data && data.items.length === 0 ? (
          <p className="dim" data-testid="queue-empty">
            No queue jobs match.
          </p>
        ) : (
          data?.items.map((item) => {
            const key = `${item.kind}:${item.parent_id}`
            const isOpen = expanded === key
            const pct = progressLabel(item.progress)
            return (
              <div key={`${item.release_title}-${item.parent_id}-${item.state}`} data-testid={`queue-row-${item.kind}-${item.parent_id}`}>
                <div className="lt-row">
                  <button
                    type="button"
                    className="lt-name lt-expand"
                    title={item.release_title}
                    onClick={() => toggleHistory(item)}
                    data-testid={`queue-expand-${item.kind}-${item.parent_id}`}
                    aria-expanded={isOpen}
                  >
                    {isOpen ? '▾' : '▸'} {item.release_title}
                  </button>
                  <span className="lt-dim" data-testid={`queue-media-${item.kind}-${item.parent_id}`}>
                    {item.parent_name}
                    {pct ? ` · ${pct}` : ''}
                  </span>
                  <span className="lt-dim">{item.indexer || '—'}</span>
                  <span className={`pill pill-${item.state}`}>{item.state}</span>
                  <span className="lt-dim">{timeAgo(item.updated_at)}</span>
                </div>
                {isOpen && (
                  <div className="queue-history" data-testid={`queue-history-${item.kind}-${item.parent_id}`}>
                    {historyLoading ? (
                      <p className="dim small">Loading history…</p>
                    ) : historyError ? (
                      <p className="error small" data-testid="queue-history-error">
                        {historyError}
                      </p>
                    ) : history.length === 0 ? (
                      <p className="dim small">No history for {item.parent_name} yet.</p>
                    ) : (
                      <ul className="feed">
                        {history.map((h, i) => (
                          <li key={`${h.event}-${h.at}-${i}`} className="feed-item">
                            <span className={`pill ${historyPillClass(h.event)}`}>{h.event}</span>
                            <span className="feed-main">
                              <span className="feed-title">{h.detail || h.event}</span>
                              <span className="dim small">{timeAgo(h.at)}</span>
                            </span>
                          </li>
                        ))}
                      </ul>
                    )}
                  </div>
                )}
              </div>
            )
          })
        )}
      </div>
      <Pagination
        page={page}
        pageSize={DEFAULT_PAGE_SIZE}
        total={data?.total ?? 0}
        onPage={(p) => setPage(p)}
      />
    </div>
  )
}
