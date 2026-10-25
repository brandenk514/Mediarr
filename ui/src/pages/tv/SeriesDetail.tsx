import { useCallback, useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { ApiError, client } from '../../api/client'
import type { Episode, Series } from '../../api/types'
import { MonitoredToggle } from '../../components/list'
import { Badge, Toolbar, TbButton } from '../../components/ui'

// SeriesDetail is the full series page at /tv/:id (route established in P1;
// the Sonarr-identical toolbar / season accordions / per-episode status are
// filled in by P2). It renders the series header (badges) and its episodes
// grouped by season, all from existing endpoints (no backend change).
export function SeriesDetailPage() {
  const { id } = useParams<{ id: string }>()
  const seriesId = Number(id)
  const [series, setSeries] = useState<Series | null>(null)
  const [episodes, setEpisodes] = useState<Episode[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [authLost, setAuthLost] = useState(false)

  const load = useCallback(async () => {
    if (!seriesId || Number.isNaN(seriesId)) {
      setError('Unknown series.')
      return
    }
    try {
      const [s, eps] = await Promise.all([client.getSeries(seriesId), client.listEpisodes(seriesId)])
      setSeries(s)
      setEpisodes(eps)
      setError(null)
      setAuthLost(false)
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) setAuthLost(true)
      setError(e instanceof Error ? e.message : 'failed to load series')
    }
  }, [seriesId])

  useEffect(() => {
    void load()
  }, [load])

  if (error && !series) {
    return (
      <div className="card" data-testid="series-detail-error">
        <h2>Series unavailable</h2>
        <p className="error" data-testid="series-detail-error-message">
          {error}
        </p>
        {authLost && (
          <p>
            <Link to="/login">Sign in again</Link>
          </p>
        )}
        <button onClick={() => void load()}>Retry</button>
      </div>
    )
  }

  if (!series || !episodes) {
    return <p className="dim" data-testid="series-detail-loading">Loading series…</p>
  }

  // Group episodes by season, preserving order.
  const bySeason = new Map<number, Episode[]>()
  for (const ep of episodes) {
    const arr = bySeason.get(ep.Season) ?? []
    arr.push(ep)
    bySeason.set(ep.Season, arr)
  }
  const seasons = [...bySeason.entries()].sort((a, b) => a[0] - b[0])

  return (
    <div className="list-page" data-testid="series-detail">
      <div className="page-head">
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.8rem' }}>
          <Link to="/tv" className="ghost" data-testid="series-detail-back">
            ← Series
          </Link>
          <h2>{series.Title}</h2>
        </div>
      </div>

      <Toolbar
        left={
          <>
            <TbButton icon="⟳" label="Refresh & Scan" testId="detail-refresh" onClick={() => void load()} />
            <TbButton icon="🔍" label="Search Monitored" testId="detail-search" disabled divider />
          </>
        }
        right={
          <>
            <TbButton icon="👁" label="Options" testId="detail-options" divider />
          </>
        }
      />

      <div className="card" data-testid="series-detail-header">
        <div className="badge-row" style={{ marginBottom: '0.6rem' }}>
          {series.Year && <Badge tone="teal">{series.Year}</Badge>}
          <Badge tone={series.Monitored ? 'ok' : 'default'}>
            {series.Monitored ? 'Monitored' : 'Unmonitored'}
          </Badge>
          {series.QualityProfile && <Badge>{series.QualityProfile}</Badge>}
        </div>
        <p className="dim" style={{ margin: 0 }}>
          Overview and metadata arrive with the series detail build (P2).
        </p>
      </div>

      {seasons.length === 0 ? (
        <p className="dim" data-testid="series-detail-no-episodes">
          No episodes yet.
        </p>
      ) : (
        seasons.map(([season, eps]) => (
          <div className="list-table" key={season} data-testid={`season-${season}`}>
            <div className="lt-head">
              <span style={{ gridColumn: '1 / -1', fontWeight: 600, color: 'var(--text)' }}>
                Season {season} — {eps.length} episode{eps.length === 1 ? '' : 's'}
              </span>
            </div>
            {eps.map((ep) => (
              <div className="lt-row" key={ep.ID}>
                <span className="lt-dim">
                  S{ep.Season}E{ep.Episode}
                </span>
                <span className="lt-name" title={ep.Title}>
                  {ep.Title || '—'}
                </span>
                <span style={{ textAlign: 'right' }}>
                  <MonitoredToggle
                    monitored={ep.Monitored}
                    onToggle={async (next) => {
                      await client.setEpisodeMonitored(seriesId, ep.Season, ep.Episode, next)
                      void load()
                    }}
                    label={`S${ep.Season}E${ep.Episode} monitoring`}
                    testId={`episode-mon-${ep.ID}`}
                  />
                </span>
              </div>
            ))}
          </div>
        ))
      )}
    </div>
  )
}
