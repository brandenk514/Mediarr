import { useCallback, useEffect, useState } from 'react'
import { client } from '../../api/client'
import type { Episode, ListQuery, Series } from '../../api/types'
import {
  DEFAULT_PAGE_SIZE,
  ListControls,
  MonitoredToggle,
  Pagination,
  usePagedList,
} from '../../components/list'

// TvPage is the per-kind list view for TV (#41): a server-side filtered,
// paginated series table where each series expands (lazily) to its episodes,
// each with its own monitored toggle.
export function TvPage() {
  const load = useCallback((q: ListQuery) => client.listSeries(q), [])
  const {
    data,
    loading,
    error,
    text,
    setText,
    monitored,
    setMonitored,
    page,
    setPage,
    total,
    reload,
    expanded,
    toggleExpanded,
  } = usePagedList<Series>(load)

  return (
    <div className="list-page">
      <div className="dash-head">
        <h2 data-testid="tv-title">TV</h2>
        <div className="dash-head-right">
          <ListControls
            text={text}
            onText={setText}
            monitored={monitored}
            onMonitored={setMonitored}
            onRefresh={reload}
            total={total}
            busy={loading}
          />
        </div>
      </div>

      {error && !data ? (
        <div className="card" data-testid="tv-error">
          <h3>TV unavailable</h3>
          <p className="error" data-testid="tv-error-message">
            {error}
          </p>
          <button onClick={reload}>Retry</button>
        </div>
      ) : (
        <div className="list-list" data-testid="tv-list">
          {loading && !data ? (
            <p className="dim">Loading series…</p>
          ) : data && data.items.length === 0 ? (
            <p className="dim" data-testid="tv-empty">
              No series match.
            </p>
          ) : (
            data?.items.map((s) => (
              <div className="list-item" key={s.ID} data-testid={`series-${s.ID}`}>
                <div className="item-head">
                  <button
                    type="button"
                    className={`row-head ${expanded.has(s.ID) ? 'open' : ''}`}
                    onClick={() => toggleExpanded(s.ID)}
                    aria-expanded={expanded.has(s.ID)}
                    data-testid={`series-toggle-${s.ID}`}
                  >
                    <span className={`chevron ${expanded.has(s.ID) ? 'chevron-open' : ''}`}>
                      {expanded.has(s.ID) ? '▾' : '▸'}
                    </span>
                    <span className="lt-name" title={s.Title}>
                      {s.Title}
                    </span>
                    {s.Year ? <span className="lt-dim">{s.Year}</span> : null}
                  </button>
                  <MonitoredToggle
                    monitored={s.Monitored}
                    onToggle={async (next) => {
                      await client.setSeriesMonitored(s.ID, next)
                      reload()
                    }}
                    label={`${s.Title} monitoring`}
                    testId={`series-mon-${s.ID}`}
                  />
                </div>
                {expanded.has(s.ID) && <EpisodeList seriesId={s.ID} />}
              </div>
            ))
          )}
          <Pagination page={page} pageSize={DEFAULT_PAGE_SIZE} total={total} onPage={setPage} />
        </div>
      )}
    </div>
  )
}

// EpisodeList fetches a series' episodes only after its row is expanded, so
// collapsed rows cost no child request.
function EpisodeList({ seriesId }: { seriesId: number }) {
  const [eps, setEps] = useState<Episode[] | null>(null)
  const [err, setErr] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    client
      .listEpisodes(seriesId)
      .then((e) => !cancelled && setEps(e))
      .catch((e: unknown) => !cancelled && setErr(e instanceof Error ? e.message : 'failed to load episodes'))
    return () => {
      cancelled = true
    }
  }, [seriesId])

  if (err) return <p className="error">Episode error: {err}</p>
  if (!eps) return <p className="dim">Loading episodes…</p>
  if (eps.length === 0) return <p className="dim">No episodes yet.</p>
  return (
    <div className="child-list" data-testid={`episodes-${seriesId}`}>
      {eps.map((ep) => (
        <div className="child-row" key={ep.ID}>
          <span className="child-label">
            S{ep.Season}E{ep.Episode}
          </span>
          <span className="lt-name" title={ep.Title}>
            {ep.Title || '—'}
          </span>
          <MonitoredToggle
            monitored={ep.Monitored}
            onToggle={async (next) => {
              await client.setEpisodeMonitored(seriesId, ep.Season, ep.Episode, next)
            }}
            label={`S${ep.Season}E${ep.Episode} monitoring`}
            testId={`episode-mon-${ep.ID}`}
          />
        </div>
      ))}
    </div>
  )
}
