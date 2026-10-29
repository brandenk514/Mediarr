import { useCallback, useEffect, useState } from 'react'
import { client } from '../../api/client'
import type { SearchHistoryItem } from '../../api/types'
import { EmptyState, Toolbar } from '../../components/ui'

// PAGE_SIZE matches the server default (and max is 100); Prowlarr-style
// pagination is "Page X · Total records: N" in the table footer.
const PAGE_SIZE = 25

// SearchHistoryPage is the Prowlarr PR3 History page (plan §2 "History"):
// every interactive search run (term, indexer, elapsed, success) with
// indexer + query filters, server-side pagination, Refresh, and Clear
// (with confirmation).
export function SearchHistoryPage() {
  const [page, setPage] = useState(1)
  const [filterIndexer, setFilterIndexer] = useState('')
  const [filterQ, setFilterQ] = useState('')
  const [reloadKey, setReloadKey] = useState(0)

  const [items, setItems] = useState<SearchHistoryItem[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [clearedMsg, setClearedMsg] = useState<string | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const res = await client.searchHistory({
        page,
        page_size: PAGE_SIZE,
        indexer: filterIndexer.trim() ? filterIndexer.trim() : undefined,
        q: filterQ.trim() ? filterQ.trim() : undefined,
      })
      setItems(res.items)
      setTotal(res.total)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'failed to load history')
    } finally {
      setLoading(false)
    }
  }, [page, filterIndexer, filterQ, reloadKey])

  useEffect(() => {
    void load()
  }, [load])

  async function clearHistory() {
    if (!window.confirm('Clear all search history? This cannot be undone.')) return
    try {
      const res = await client.clearSearchHistory()
      setClearedMsg(`${res.deleted} cleared`)
      setPage(1)
      setReloadKey((k) => k + 1)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'failed to clear history')
    }
  }

  const hasNext = page * PAGE_SIZE < total
  const hasPrev = page > 1

  return (
    <div className="list-page">
      <div className="dash-head">
        <h2 data-testid="search-history-title">History</h2>
      </div>
      <Toolbar
        left={
          <>
            <input
              className="list-search sh-filter"
              placeholder="Filter: indexer name"
              value={filterIndexer}
              onChange={(e) => {
                setFilterIndexer(e.target.value)
                setPage(1)
              }}
              data-testid="history-filter-indexer"
              aria-label="Filter by indexer"
            />
            <input
              className="list-search sh-filter"
              placeholder="Filter: search term"
              value={filterQ}
              onChange={(e) => {
                setFilterQ(e.target.value)
                setPage(1)
              }}
              data-testid="history-filter-q"
              aria-label="Filter by search term"
            />
            {clearedMsg && (
              <span className="dim small" data-testid="history-cleared">
                {clearedMsg}
              </span>
            )}
          </>
        }
        right={
          <>
            <button className="ghost" onClick={() => setReloadKey((k) => k + 1)} data-testid="history-refresh">
              Refresh
            </button>
            <button className="ghost danger" onClick={() => void clearHistory()} data-testid="history-clear">
              Clear
            </button>
          </>
        }
      />

      {error && (
        <p className="error" role="alert" data-testid="history-error">
          {error}
        </p>
      )}

      {loading ? (
        <div className="card" data-testid="history-loading">
          <p className="dim">Loading history…</p>
        </div>
      ) : items.length === 0 ? (
        <EmptyState
          title="No search history"
          message="Interactive searches run from the Search page are recorded here."
          testId="history-empty"
        />
      ) : (
        <div className="sh-table" data-testid="history-table">
          <div className="sh-head" data-testid="history-head">
            <span>Status</span>
            <span>Indexer</span>
            <span>Query</span>
            <span>Categories</span>
            <span>Date</span>
            <span>Elapsed</span>
          </div>
          {items.map((h, i) => (
            <div className="sh-row" key={h.id} data-testid={`history-row-${i}`}>
              <span
                className={h.ok ? 'sh-ok' : 'sh-fail'}
                title={h.ok ? 'Success' : 'Failed'}
                data-testid={`history-status-${i}`}
              >
                {h.ok ? '✓' : '✗'}
              </span>
              <span data-testid={`history-indexer-${i}`}>{h.indexer}</span>
              <span className="sh-term" title={h.term} data-testid={`history-query-${i}`}>
                {h.term}
              </span>
              <span data-testid={`history-cats-${i}`}>
                {(h.categories ?? []).length === 0 ? (
                  '—'
                ) : (
                  (h.categories ?? []).map((c) => (
                    <span key={c} className="badge cat">
                      {c}
                    </span>
                  ))
                )}
              </span>
              <span data-testid={`history-date-${i}`}>{fmtDate(h.at)}</span>
              <span data-testid={`history-elapsed-${i}`}>{h.elapsed_ms} ms</span>
            </div>
          ))}
          <div className="sh-foot" data-testid="history-foot">
            <span className="dim small">
              Page {page} · Total records: {total}
            </span>
            <span>
              <button
                className="ghost small"
                disabled={!hasPrev}
                onClick={() => setPage((p) => Math.max(1, p - 1))}
                data-testid="history-prev"
              >
                ←
              </button>{' '}
              <button
                className="ghost small"
                disabled={!hasNext}
                onClick={() => setPage((p) => p + 1)}
                data-testid="history-next"
              >
                →
              </button>
            </span>
          </div>
        </div>
      )}
    </div>
  )
}

// fmtDate renders an ISO timestamp in local time, Prowlarr-style
// (YYYY-MM-DD HH:mm).
function fmtDate(iso: string): string {
  const d = new Date(iso)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}
