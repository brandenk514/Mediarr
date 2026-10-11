import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { ApiError, client, connectEvents } from '../api/client'
import type { Dashboard } from '../api/types'
import { IndexerHealthCard } from './dashboard/IndexerHealthCard'
import { HistoryFeed } from './dashboard/HistoryFeed'
import { LibraryCard } from './dashboard/LibraryCard'
import { LiveBadge } from './dashboard/LiveBadge'
import { QueueFeed } from './dashboard/QueueFeed'

// Dashboard is the M6 overview (#40): library counts per kind, active wants,
// queue depth, recent history, indexer health at a glance, and a live
// WebSocket link that re-reads the snapshot when the pipelines push
// queue.updated / history.appended frames.
export function DashboardPage() {
  const [data, setData] = useState<Dashboard | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [authLost, setAuthLost] = useState(false)
  const [loading, setLoading] = useState(true)
  const [live, setLive] = useState<'connecting' | 'open' | 'closed'>('connecting')
  const [updatedAt, setUpdatedAt] = useState<Date | null>(null)

  const refresh = useCallback(async () => {
    try {
      const d = await client.getDashboard(20)
      setData(d)
      setUpdatedAt(new Date())
      setError(null)
      setAuthLost(false)
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) setAuthLost(true)
      setError(e instanceof Error ? e.message : 'failed to load dashboard')
    } finally {
      setLoading(false)
    }
  }, [])

  // Initial load.
  useEffect(() => {
    void refresh()
  }, [refresh])

  // Live updates: subscribe to the events socket; a queue/history frame
  // triggers a snapshot re-read (debounced so a burst coalesces into one
  // fetch). The socket's onStatus drives the LiveBadge.
  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | undefined
    const close = connectEvents(
      (frame) => {
        if (frame.type !== 'queue.updated' && frame.type !== 'history.appended') return
        if (timer) return
        timer = setTimeout(() => {
          timer = undefined
          void refresh()
        }, 500)
      },
      (status) => setLive(status),
    )
    return () => {
      if (timer) clearTimeout(timer)
      close()
    }
  }, [refresh])

  if (loading) {
    return (
      <div className="card">
        <p className="dim">Loading dashboard…</p>
      </div>
    )
  }

  if (error && !data) {
    return (
      <div className="card" data-testid="dashboard-error">
        <h2>Dashboard unavailable</h2>
        <p className="error" data-testid="dashboard-error-message">
          {error}
        </p>
        {authLost && (
          <p>
            <Link to="/login">Sign in again</Link>
          </p>
        )}
        <button onClick={() => void refresh()}>Retry</button>
      </div>
    )
  }

  if (!data) return null
  const d = data

  return (
    <>
      <div className="dash-head">
        <h2>Dashboard</h2>
        <div className="dash-head-right">
          <LiveBadge status={live} />
          {error && <span className="error small" data-testid="refresh-error">{error}</span>}
          {updatedAt && (
            <span className="dim small">
              updated {updatedAt.toLocaleTimeString()}
            </span>
          )}
          <button className="ghost" onClick={() => void refresh()} data-testid="refresh">
            Refresh
          </button>
        </div>
      </div>

      <div className="dash-grid libs">
        {d.libraries.map((lib) => (
          <LibraryCard key={lib.kind} library={lib} />
        ))}
      </div>

      <div className="dash-grid indexers">
        {d.indexers.map((ix) => (
          <IndexerHealthCard key={ix.name} indexer={ix} />
        ))}
      </div>

      <div className="dash-columns">
        <section className="card">
          <h3>Recent queue</h3>
          <QueueFeed items={d.recent_queue} />
        </section>
        <section className="card">
          <h3>Recent history</h3>
          <HistoryFeed items={d.recent_history} />
        </section>
      </div>
    </>
  )
}
