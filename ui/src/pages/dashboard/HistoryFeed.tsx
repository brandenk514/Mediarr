import { timeAgo } from '../../lib/format'
import type { HistoryItem } from '../../api/types'

// HistoryFeed renders the recent activity feed. Updated live via the
// /api/v1/events WebSocket (the dashboard re-reads the snapshot on a
// history.appended frame).
export function HistoryFeed({ items, now }: { items: HistoryItem[] | null; now?: number }) {
  // The API can emit null for an empty feed; treat it as empty instead of
  // throwing (a render error here unmounts the whole app).
  const list = items ?? []
  if (list.length === 0) {
    return <p className="dim small">No recent activity.</p>
  }
  return (
    <ul className="feed" data-testid="history-feed">
      {list.map((h, i) => (
        <li key={`${h.name}-${h.event}-${i}`} className="feed-item" data-testid={`history-${i}`}>
          <span className="dim small feed-time">{timeAgo(h.at, now)}</span>
          <span className="feed-main">
            <span className="feed-title">{h.name}</span>
            <span className="dim small">
              {h.kind} · {h.event}
              {h.detail ? ` — ${h.detail}` : ''}
            </span>
          </span>
        </li>
      ))}
    </ul>
  )
}
