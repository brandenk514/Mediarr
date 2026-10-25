import { useCallback, useEffect, useState } from 'react'
import { client } from '../../api/client'
import type { GlobalHistoryItem } from '../../api/types'

// EventsPanel is System > Events: the instance-wide event log (the global
// history feed — every media-kind event), with an event-type filter.
const PAGE_SIZE = 100

export function EventsPanel() {
  const [events, setEvents] = useState<GlobalHistoryItem[]>([])
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  const [filter, setFilter] = useState('')
  const [filterApplied, setFilterApplied] = useState('')

  const load = useCallback(
    async (event?: string) => {
      setError(null)
      setLoading(true)
      try {
        const envelope = await client.getSystemEvents(
          event ? { event, limit: PAGE_SIZE } : { limit: PAGE_SIZE },
        )
        setEvents(envelope.events ?? [])
      } catch (e) {
        setError(e instanceof Error ? e.message : 'failed to load events')
      } finally {
        setLoading(false)
      }
    },
    [],
  )

  useEffect(() => {
    void load()
  }, [load])

  if (error) {
    return (
      <div className="card" data-testid="system-events-error">
        <h2>Events unavailable</h2>
        <p className="error">{error}</p>
        <button onClick={() => void load(filterApplied || undefined)}>Retry</button>
      </div>
    )
  }

  return (
    <div className="card" data-testid="system-events">
      <h3 style={{ margin: '0 0 0.5rem' }}>
        Events
        {filterApplied && (
          <span className="dim small">
            {' '}
            · filtered by “{filterApplied}”
          </span>
        )}
      </h3>
      <p className="dim" style={{ marginTop: 0 }}>
        The most recent {PAGE_SIZE} instance-wide events, newest first.
      </p>

      <div className="list-controls">
        <input
          data-testid="events-filter"
          placeholder="Filter by event type (e.g. Import)"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          style={{ maxWidth: '18rem' }}
        />
        <button
          data-testid="events-filter-btn"
          onClick={() => {
            const f = filter.trim()
            setFilterApplied(f)
            void load(f || undefined)
          }}
        >
          Filter
        </button>
        {filterApplied && (
          <button
            className="ghost"
            data-testid="events-clear-filter"
            onClick={() => {
              setFilter('')
              setFilterApplied('')
              void load()
            }}
          >
            Clear
          </button>
        )}
      </div>

      {loading ? (
        <p className="dim">Loading event log…</p>
      ) : events.length === 0 ? (
        <p className="dim">
          No events{filterApplied ? ` for “${filterApplied}”` : ''} yet.
        </p>
      ) : (
        <div className="list-table">
          <div className="lt-head">
            <span>When</span>
            <span>Media</span>
            <span>Event</span>
            <span>Name</span>
            <span>Detail</span>
          </div>
          {events.map((ev, i) => (
            <div className="lt-row" data-testid={`event-row-${i}`} key={`${ev.at}-${ev.event}-${i}`}>
              <span className="lt-dim">{new Date(ev.at).toLocaleString()}</span>
              <span>
                <span className={`pill pill-${ev.kind}`}>{ev.kind}</span>
              </span>
              <span>{ev.event}</span>
              <span>{ev.name}</span>
              <span className="lt-dim">{ev.detail}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
