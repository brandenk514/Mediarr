import { useCallback, useEffect, useMemo, useState } from 'react'
import { useParams } from 'react-router-dom'
import { ApiError, client } from '../../api/client'
import type { AddEpisodeRequest, Episode, Series } from '../../api/types'
import { MonitoredToggle } from '../../components/list'
import { AddError } from '../../components/addflow'
import { Badge, BadgeRow, Modal, TbButton, TbLink } from '../../components/ui'
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

  // issue #120 part B: the in-detail Add Episode flow. Posts to the
  // existing POST /tv/{id}/episodes endpoint (client.addEpisode); there is
  // no season-add endpoint — seasons are derived from episode rows, so
  // adding an episode for a new season number creates that season.
  const [addEpisodeOpen, setAddEpisodeOpen] = useState(false)
  const [epSeason, setEpSeason] = useState('1')
  const [epNumber, setEpNumber] = useState('1')
  const [epTitle, setEpTitle] = useState('')
  const [epAirDate, setEpAirDate] = useState('')
  const [epMonitored, setEpMonitored] = useState(true)
  const [addEpisodeBusy, setAddEpisodeBusy] = useState(false)
  const [addEpisodeError, setAddEpisodeError] = useState('')

  function openAddEpisode() {
    setEpSeason('1')
    setEpNumber('1')
    setEpTitle('')
    setEpAirDate('')
    setEpMonitored(true)
    setAddEpisodeError('')
    setAddEpisodeOpen(true)
  }

  const epSeasonNum = Number(epSeason)
  const epNumberNum = Number(epNumber)
  const episodeFormValid =
    !Number.isNaN(epSeasonNum) && epSeasonNum > 0 && !Number.isNaN(epNumberNum) && epNumberNum > 0

  async function submitEpisode() {
    if (!episodeFormValid || addEpisodeBusy) return
    setAddEpisodeBusy(true)
    setAddEpisodeError('')
    const req: AddEpisodeRequest = { season: epSeasonNum, episode: epNumberNum, monitored: epMonitored }
    const title = epTitle.trim()
    if (title) req.title = title
    const airDate = epAirDate.trim()
    if (airDate) req.air_date = airDate
    try {
      await client.addEpisode(seriesId, req)
      setAddEpisodeOpen(false)
      setStatus(
        `Added S${epSeasonNum}E${epNumberNum}${title ? ` "${title}"` : ''}.`,
      )
      void load()
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'failed to add episode'
      setAddEpisodeError(msg)
      setStatus(msg)
    } finally {
      setAddEpisodeBusy(false)
    }
  }

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
          <TbButton
            icon="+"
            label="Add Episode"
            testId="series-detail-add-episode"
            title="Add an episode to this series (existing endpoint: POST /tv/{id}/episodes)"
            onClick={openAddEpisode}
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

      {/* issue #120 part B: in-detail Add Episode dialog. Posts a new
          episode row to the existing endpoint; the new season (if any)
          appears in the accordions after the list refreshes. */}
      {addEpisodeOpen && (
        <Modal
          title={`Add Episode — ${series?.Title ?? 'series'}`}
          onClose={() => setAddEpisodeOpen(false)}
          testId="add-episode-modal"
          footer={
            <>
              <button type="button" className="ghost" onClick={() => setAddEpisodeOpen(false)}>
                Cancel
              </button>
              <button
                type="button"
                data-testid="add-episode-button"
                disabled={addEpisodeBusy || !episodeFormValid}
                onClick={() => {
                  void submitEpisode()
                }}
              >
                {addEpisodeBusy ? 'Adding…' : 'Add Episode'}
              </button>
            </>
          }
        >
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '0.6rem' }}>
            <label style={{ display: 'block' }}>
              <span className="dim small">Season</span>
              <input
                type="number"
                min={1}
                value={epSeason}
                onChange={(e) => setEpSeason(e.target.value)}
                data-testid="add-episode-season"
              />
            </label>
            <label style={{ display: 'block' }}>
              <span className="dim small">Episode</span>
              <input
                type="number"
                min={1}
                value={epNumber}
                onChange={(e) => setEpNumber(e.target.value)}
                data-testid="add-episode-episode"
              />
            </label>
          </div>
          <label style={{ display: 'block', marginTop: '0.6rem' }}>
            <span className="dim small">Title (optional)</span>
            <input
              type="text"
              value={epTitle}
              onChange={(e) => setEpTitle(e.target.value)}
              placeholder="e.g. The Pilot"
              data-testid="add-episode-title"
            />
          </label>
          <label style={{ display: 'block', marginTop: '0.6rem' }}>
            <span className="dim small">Air date (optional)</span>
            <input
              type="date"
              value={epAirDate}
              onChange={(e) => setEpAirDate(e.target.value)}
              data-testid="add-episode-air-date"
            />
          </label>
          <label className="check" style={{ marginTop: '0.6rem', display: 'flex', alignItems: 'center', gap: '0.4rem' }}>
            <input
              type="checkbox"
              checked={epMonitored}
              onChange={(e) => setEpMonitored(e.target.checked)}
              data-testid="add-episode-monitored"
            />
            Monitored
          </label>
          <AddError message={addEpisodeError} testId="add-episode-error" />
        </Modal>
      )}
    </DetailShell>
  )
}
