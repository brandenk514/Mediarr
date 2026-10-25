import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { client } from '../../api/client'
import type { GlobalHistoryItem, MediaKind } from '../../api/types'
import { EmptyState, TbButton, Toolbar } from '../../components/ui'
import { timeAgo } from '../../lib/format'

// History is the global event feed at /activity/history (route established in
// P1). P3: real cross-media feed (added / monitored / searched / imported / …)
// with a date filter + event-type filter and a media link per event.
function mediaPath(kind: MediaKind, id: number): string {
  switch (kind) {
    case 'tv':
      return `/tv/${id}`
    case 'movie':
      return '/movies'
    case 'music':
      return '/music'
    case 'book':
      return '/books'
    default:
      return '/'
  }
}

const EVENT_TYPES = [
  'added',
  'monitored',
  'unmonitored',
  'search-requested',
  'searched',
  'download',
  'imported',
  'import',
]

export function HistoryPage() {
  const [items, setItems] = useState<GlobalHistoryItem[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [since, setSince] = useState('')
  const [event, setEvent] = useState('')

  const load = useCallback(async () => {
    setLoading(true)
    try {
      // `since` is an RFC 3339 bound; a bare <input date> gives YYYY-MM-DD, so
      // normalise to start-of-day UTC before sending.
      const rows = await client.listGlobalHistory({
        since: since ? `${since}T00:00:00Z` : undefined,
        event: event || undefined,
        limit: 100,
      })
      setItems(rows)
      setError(null)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'failed to load history')
    } finally {
      setLoading(false)
    }
  }, [since, event])

  useEffect(() => {
    void load()
  }, [load])

  return (
    <div className="list-page" data-testid="history">
      <div className="page-head">
        <h2>History</h2>
      </div>
      <Toolbar
        left={
          <>
            <TbButton icon="⟳" label="Refresh" testId="history-refresh" onClick={() => void load()} />
            <input
              type="date"
              value={since}
              onChange={(e) => setSince(e.target.value)}
              data-testid="history-since"
              aria-label="Since date"
              style={{ width: 'auto' }}
            />
            <select
              value={event}
              onChange={(e) => setEvent(e.target.value)}
              data-testid="history-event"
              aria-label="Filter by event type"
              style={{ width: 'auto' }}
            >
              <option value="">All events</option>
              {EVENT_TYPES.map((t) => (
                <option key={t} value={t}>
                  {t}
                </option>
              ))}
            </select>
          </>
        }
      />
      {error ? (
        <div className="card" data-testid="history-error">
          <p className="error" data-testid="history-error-message">
            {error}
          </p>
          <button onClick={() => void load()}>Retry</button>
        </div>
      ) : loading && !items ? (
        <p className="dim">Loading history…</p>
      ) : !items || items.length === 0 ? (
        <EmptyState
          title="No history yet"
          message="Lifecycle events (added, monitored, searched, imported, …) across all libraries appear here."
          testId="history-empty"
        />
      ) : (
        <div className="card" data-testid="history-feed-wrap">
          <ul className="feed" data-testid="history-feed">
            {items.map((h, i) => (
              <li key={`${h.kind}-${h.parent_id}-${h.event}-${h.at}-${i}`} className="feed-item" data-testid={`history-item-${i}`}>
                <span className="pill pill-idle">{h.event}</span>
                <span className="feed-main">
                  <span className="feed-title">
                    <Link to={mediaPath(h.kind, h.parent_id)} data-testid={`history-link-${i}`}>
                      {h.name}
                    </Link>
                  </span>
                  <span className="dim small">
                    {h.detail ? `${h.detail} · ` : ''}
                    {timeAgo(h.at)}
                  </span>
                </span>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  )
}
