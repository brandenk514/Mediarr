import { useCallback, useEffect, useMemo, useState } from 'react'
import { useParams } from 'react-router-dom'
import { ApiError, client } from '../../api/client'
import type { Episode, Series } from '../../api/types'
import { MonitoredToggle } from '../../components/list'
import { Badge, BadgeRow, TbButton, TbLink } from '../../components/ui'
import { DetailShell } from '../detail/DetailShell'

// SeriesDetail is the Sonarr-parity series page (P2) rendered through the
// unified detail shell (issue #120 part A): a badge header (year, quality
// profile, series type, monitored state, tags — the metadata the API
// carries), a metadata credit line, and per-season accordions whose episode
// rows keep the per-episode monitored toggles. The toolbar wires the P2
// search endpoint (Search Monitored) alongside Refresh & Scan, and the
// issue #120 part B Add action that opens the existing TV add flow
// (/tv/add, the demoted deep page).
export function SeriesDetailPage() {
  const { id } = useParams<{ id: string }>()
  const seriesId = Number(id)
  const [series, setSeries] = useState<Series | null>(null)
  const [episodes, setEpisodes] = useState<Episode[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [authLost, setAuthLost] = useState(false)
  const [status, setStatus] = useState<string | null>(null)
  const [openSeasons, setOpenSeasons] = useState<Set<number>>(() => new Set())
  const [searching, setSearching] = useState(false)

  const load = useCallback(async () => {
    if (!seriesId || Number.isNaN(seriesId)) {
      setError('Unknown series.')
      return
    }
    try {
      const [s, eps] = await Promise.all([client.getSeries(seriesId), client.listEpisodes(seriesId)])
      setSeries(s)
      setEpisodes(eps ?? []) // older backends return null for an empty list
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

  async function searchMonitored() {
    if (searching) return
    setSearching(true)
    setStatus(null)
    try {
      const res = await client.searchMissingEpisodes(seriesId)
      setStatus(
        res.searched > 0
          ? `Started search for ${res.searched} missing episode${res.searched === 1 ? '' : 's'}.`
          : 'Nothing missing to search for — all monitored episodes are satisfied.',
      )
    } catch (e) {
      setStatus(e instanceof Error ? e.message : 'episode search failed')
    } finally {
      setSearching(false)
    }
  }

  function toggleSeason(season: number) {
    setOpenSeasons((prev) => {
      const next = new Set(prev)
      if (next.has(season)) next.delete(season)
      else next.add(season)
      return next
    })
  }

  // Group episodes by season, preserving order.
  const seasons = useMemo(() => {
    if (!episodes) return []
    const bySeason = new Map<number, Episode[]>()
    for (const ep of episodes) {
      const arr = bySeason.get(ep.Season) ?? []
      arr.push(ep)
      bySeason.set(ep.Season, arr)
    }
    return [...bySeason.entries()].sort((a, b) => a[0] - b[0])
  }, [episodes])

  return (
    <DetailShell
      kind="tv"
      pageTestId="series-detail"
      loading={!series || !episodes}
      loadingText="Loading series…"
      loadingTestId="series-detail-loading"
      error={!series ? error : null}
      errorTitle="Series unavailable"
      errorTestId="series-detail-error"
      errorTestIdMessage="series-detail-error-message"
      authLost={authLost}
      onRetry={() => void load()}
      back={{ to: '/tv', label: '← Series', testId: 'series-detail-back' }}
      actionsLeft={
        <>
          <TbLink icon="+" label="Add" to="/tv/add" testId="detail-add" />
          <TbButton icon="⟳" label="Refresh & Scan" testId="detail-refresh" onClick={() => void load()} />
          <TbButton
            icon="🔍"
            label="Search Monitored"
            testId="detail-search"
            disabled={searching}
            onClick={() => void searchMonitored()}
          />
        </>
      }
      actionsRight={
        <>
          <TbButton
            icon="⋯"
            label="Options"
            testId="detail-options"
            divider
            disabled
            title="More actions land in a later phase"
          />
        </>
      }
      status={status}
      statusTestId="series-detail-status"
      title={series?.Title ?? ''}
      heading={series?.Title}
      heroTestId="series-detail-header"
      meta={
        series ? (
          <>
            <BadgeRow>
              <Badge tone={series.Monitored ? 'ok' : 'default'} testId="badge-monitored">
                {series.Monitored ? 'Monitored' : 'Unmonitored'}
              </Badge>
              {series.QualityProfile && <Badge testId="badge-quality">{series.QualityProfile}</Badge>}
              <Badge tone="teal" testId="badge-type">
                {series.SeriesType ? series.SeriesType[0]!.toUpperCase() + series.SeriesType.slice(1) : 'Standard'}
              </Badge>
              {series.Year > 0 && <Badge>{series.Year}</Badge>}
              {series.SeasonFolder && <Badge testId="badge-season-folder">Season folder</Badge>}
              {(series.Tags ?? []).map((t) => (
                <Badge key={t} tone="warn" testId={`badge-tag-${t}`}>
                  {t}
                </Badge>
              ))}
            </BadgeRow>
            <p className="dim" style={{ margin: '0.6rem 0 0' }} data-testid="series-detail-meta">
              Metadata is provided by TheTVDB.
            </p>
          </>
        ) : null
      }
    >
      {seasons.length === 0 ? (
        <p className="dim" data-testid="series-detail-no-episodes">
          No episodes yet.
        </p>
      ) : (
        seasons.map(([season, eps]) => {
          const open = openSeasons.has(season)
          return (
            <div className="list-table" key={season} data-testid={`season-${season}`}>
              <div className="lt-head">
                <button
                  type="button"
                  className={`row-head ${open ? 'open' : ''}`}
                  style={{ gridColumn: '1 / -1', background: 'none', width: '100%' }}
                  onClick={() => toggleSeason(season)}
                  aria-expanded={open}
                  data-testid={`season-toggle-${season}`}
                >
                  <span className={`chevron ${open ? 'chevron-open' : ''}`}>{open ? '▾' : '▸'}</span>
                  <span style={{ fontWeight: 600, color: 'var(--text)' }}>
                    Season {season}
                  </span>
                  <span className="lt-dim">— {eps.length} episode{eps.length === 1 ? '' : 's'}</span>
                </button>
              </div>
              {open &&
                eps.map((ep) => (
                  <div className="lt-row" key={ep.ID} data-testid={`episode-${ep.ID}`}>
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
          )
        })
      )}
    </DetailShell>
  )
}
