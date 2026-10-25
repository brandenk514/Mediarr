import { useCallback, useEffect, useState } from 'react'
import { client } from '../../api/client'
import type { ChangelogEntry } from '../../api/types'

// UpdatesPanel is System > Updates: the running version plus the changelog
// (newest first). The entry matching the running version is highlighted.
export function UpdatesPanel() {
  const [current, setCurrent] = useState('')
  const [entries, setEntries] = useState<ChangelogEntry[]>([])
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  const load = useCallback(async () => {
    setError(null)
    setLoading(true)
    try {
      const u = await client.getSystemUpdates()
      setCurrent(u.current ?? '')
      setEntries(u.changelog ?? [])
    } catch (e) {
      setError(e instanceof Error ? e.message : 'failed to load updates')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  if (error) {
    return (
      <div className="card" data-testid="system-updates-error">
        <h2>Updates unavailable</h2>
        <p className="error">{error}</p>
        <button onClick={() => void load()}>Retry</button>
      </div>
    )
  }

  if (loading) {
    return (
      <div className="card" data-testid="system-updates">
        <h3 style={{ margin: '0 0 0.5rem' }}>Updates</h3>
        <p className="dim">Loading changelog…</p>
      </div>
    )
  }

  return (
    <div data-testid="system-updates">
      <div className="card">
        <h3 style={{ margin: '0 0 0.5rem' }}>
          Updates{' '}
          <span className="dim small">
            · running <strong data-testid="updates-current-version">v{current || 'unknown'}</strong>
          </span>
        </h3>
        {entries.length === 0 ? (
          <p className="dim">No changelog available.</p>
        ) : (
          entries.map((entry) => {
            const isCurrent = `v${entry.version}` === `v${current}`
            return (
              <div
                key={entry.version}
                className="card"
                style={{ marginBottom: '0.75rem' }}
                data-testid={`changelog-entry-${entry.version}`}
              >
                <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
                  <strong>v{entry.version}</strong>
                  {isCurrent && <span className="pill pill-ok">current</span>}
                  {entry.date && <span className="dim small">{entry.date}</span>}
                </div>
                {entry.entries?.length ? (
                  <ul style={{ margin: '0.5rem 0 0', paddingLeft: '1.25rem' }}>
                    {entry.entries.map((line, i) => (
                      <li key={i} className="small">
                        {line}
                      </li>
                    ))}
                  </ul>
                ) : (
                  <p className="dim">No entries recorded.</p>
                )}
              </div>
            )
          })
        )}
      </div>
    </div>
  )
}
