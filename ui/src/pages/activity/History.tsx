import { useCallback, useEffect, useState } from 'react'
import { client } from '../../api/client'
import type { Dashboard } from '../../api/types'
import { Toolbar, TbButton } from '../../components/ui'
import { HistoryFeed } from '../dashboard/HistoryFeed'

// History is the global event feed at /activity/history (route established in
// P1). P1 renders the app's recent activity (the dashboard's history feed);
// the full feed with date + event-type filters and pagination ships in P3.
export function HistoryPage() {
  const [data, setData] = useState<Dashboard | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const d = await client.getDashboard(100)
      setData(d)
      setError(null)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'failed to load history')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  if (error && !data) {
    return (
      <div className="card" data-testid="history-error">
        <h2>History unavailable</h2>
        <p className="error" data-testid="history-error-message">
          {error}
        </p>
        <button onClick={() => void load()}>Retry</button>
      </div>
    )
  }

  return (
    <div className="list-page" data-testid="history">
      <div className="page-head">
        <h2>History</h2>
      </div>
      <Toolbar
        left={<TbButton icon="⟳" label="Refresh" testId="history-refresh" onClick={() => void load()} />}
      />
      {loading && !data ? (
        <p className="dim">Loading history…</p>
      ) : (
        <div className="card" data-testid="history-feed-wrap">
          <HistoryFeed items={data?.recent_history ?? null} />
        </div>
      )}
    </div>
  )
}
