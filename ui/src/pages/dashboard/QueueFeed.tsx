import { progressLabel } from '../../lib/format'
import type { QueueItem } from '../../api/types'

// QueueFeed renders the recent queue jobs. Updated live via the
// /api/v1/events WebSocket (the dashboard re-reads the snapshot on a
// queue.updated frame).
export function QueueFeed({ items }: { items: QueueItem[] }) {
  if (items.length === 0) {
    return <p className="dim small">No recent queue activity.</p>
  }
  return (
    <ul className="feed" data-testid="queue-feed">
      {items.map((q, i) => {
        const pct = progressLabel(q.progress)
        return (
          <li key={`${q.release_title}-${i}`} className="feed-item" data-testid={`queue-${i}`}>
            <span className={`pill pill-${q.state}`}>{q.state}</span>
            <span className="feed-main">
              <span className="feed-title">{q.release_title}</span>
              <span className="dim small">
                {q.kind} · {q.indexer}
                {pct ? ` · ${pct}` : ''}
              </span>
            </span>
          </li>
        )
      })}
    </ul>
  )
}
