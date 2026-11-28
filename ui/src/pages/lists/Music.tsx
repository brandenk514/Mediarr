import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { client } from '../../api/client'
import type { Artist, ListQuery, MusicLibrarySummary, PageArtist } from '../../api/types'
import { AddButton } from '../../components/addflow'
import {
  FacetSelect,
  FacetText,
  FilterDrawer,
  InfiniteSentinel,
  SortCluster,
  toneFor,
  usePersistentView,
} from '../../components/gridcontrols'
import {
  DEFAULT_PAGE_SIZE,
  MonitoredToggle,
  Pagination,
  usePagedList,
  type MonitoredFilter,
} from '../../components/list'
import {
  FALLBACK_PROFILE_LABEL,
  MUSIC_RIBBON_LEGEND,
  deriveMusicRibbon,
  humanBytes,
} from '../../components/music'
import { PosterCard } from '../../components/primitives/poster-card'
import { firstLetter } from '../../components/poster'
import { EmptyState, Toolbar, TbButton, ViewToggle } from '../../components/ui'

// MusicPage is the Seerr-style artists library (#150): a dense artwork-led
// poster grid by default (cover art with an initials stand-in when keyless,
// the kind pill + the five-state Lidarr ribbon as a badge over the art,
// hover refresh/edit actions), a heading control cluster — explicit sort
// direction toggle + sort field dropdown + a filter button that opens a
// slide-over drawer with "N Active Filter(s)" and Clear Active Filters —
// and the Lidarr toolbar/library summary carried over. The grid sorts and
// filters server-side (the sort allowlist rejects unknown values with 400)
// and scrolls without pagination chrome; the table view stays fully
// functional behind the persisted view toggle.
type SortField = 'title' | 'added' | 'rating'
type SortDir = 'asc' | 'desc'

// The sort dropdown fields (the direction is the separate toggle).
const SORT_FIELDS = [
  { value: 'title', label: 'Name' },
  { value: 'added', label: 'Added' },
  { value: 'rating', label: 'Rating' },
] as const

// The filter-drawer facets (#150). The zero value filters nothing. Music's
// facet surface is the artist's enriched metadata: the MusicBrainz genre
// (free text — artists carry an arbitrary genre string) and the release
// status the Lidarr ribbon already renders.
interface MusicFacets {
  genre: string
  status: string
}

const EMPTY_FACETS: MusicFacets = { genre: '', status: '' }

// The artist status facet values (the wire's closed status enum).
const STATUS_OPTIONS = [
  { value: 'continuing', label: 'Continuing' },
  { value: 'ended', label: 'Ended' },
  { value: 'inactive', label: 'Inactive' },
]

const MONITORED_OPTIONS = [
  { value: 'all', label: 'All' },
  { value: 'monitored', label: 'Monitored' },
  { value: 'unmonitored', label: 'Unmonitored' },
]

export function MusicPage() {
  const navigate = useNavigate()
  const [view, setView] = usePersistentView('music')
  // #150: the sort is field + explicit direction; flipping the direction
  // never changes the field (and vice versa).
  const [sortField, setSortField] = useState<SortField>('title')
  const [sortDir, setSortDir] = useState<SortDir>('asc')
  const [facets, setFacets] = useState<MusicFacets>(EMPTY_FACETS)
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [summary, setSummary] = useState<MusicLibrarySummary | null>(null)
  // Per-card refresh results (a real GET /music/artists/{id}) merged over
  // the page so a refreshed ribbon shows without a full refetch.
  const [fresh, setFresh] = useState<Record<number, Artist>>({})
  const [status, setStatus] = useState<string | null>(null)

  const sort = `${sortField}-${sortDir}`
  // The facet params serialize into the wire query (empty values are
  // omitted by buildListQuery, so the server treats them as unset).
  const filters = useMemo<Record<string, string>>(() => ({
    genre: facets.genre,
    status: facets.status,
  }), [facets])

  const load = useCallback(
    (q: ListQuery) => client.listArtists({ ...q, sort, filters }),
    [sort, filters],
  )
  const {
    items,
    data,
    loading,
    error,
    monitored,
    setMonitored,
    page,
    setPage,
    hasMore,
    loadMore,
    reload,
    total,
  } = usePagedList<PageArtist>(load, { mode: 'append' })

  // Library summary for the footer status bar (Lidarr-specific; no Sonarr
  // equivalent). Cosmetic — a failure must not break the grid.
  const loadSummary = useCallback(() => {
    client
      .getMusicLibrarySummary()
      .then(setSummary)
      .catch(() => setSummary(null))
  }, [])

  useEffect(() => {
    loadSummary()
  }, [loadSummary, data])

  // The drawer's active count: every set facet plus the monitored filter.
  const activeCount = useMemo(() => {
    let n = monitored === 'all' ? 0 : 1
    if (facets.genre) n++
    if (facets.status) n++
    return n
  }, [facets, monitored])

  const gridItems = useMemo(() => items.map((a) => fresh[a.ID] ?? a), [items, fresh])
  const listItems = useMemo(() => (data ? data.items : []), [data])
  const filtered = activeCount > 0

  function updateAll() {
    setStatus(`Updated ${total} artists just now`)
    reload()
    loadSummary()
  }

  // Per-card refresh: re-fetch this artist from the API and merge it in
  // (the ribbon re-derives from the fresh aggregate counts).
  async function refreshArtist(a: Artist) {
    setStatus(null)
    try {
      const freshArtist = await client.getArtist(a.ID)
      setFresh((prev) => ({ ...prev, [a.ID]: freshArtist }))
    } catch (e) {
      setStatus(e instanceof Error ? e.message : `refresh of ${a.Name} failed`)
    }
  }

  function clearFilters() {
    setFacets(EMPTY_FACETS)
    setMonitored('all')
  }

  const facet = <K extends keyof MusicFacets>(key: K, value: MusicFacets[K]) =>
    setFacets((prev) => ({ ...prev, [key]: value }))

  return (
    <div className="list-page">
      <div className="dash-head">
        <h2 data-testid="music-title">Artists</h2>
        <div className="dash-head-right">
          <SortCluster
            field={sortField}
            fields={[...SORT_FIELDS]}
            dir={sortDir}
            onDir={setSortDir}
            onField={(f) => setSortField(f as SortField)}
            testId="music"
          />
          <button
            type="button"
            className="menu-btn"
            onClick={() => setDrawerOpen(true)}
            aria-haspopup="dialog"
            data-testid="music-filter-btn"
          >
            <span aria-hidden>⚲</span>
            {activeCount} Active Filter{activeCount === 1 ? '' : 's'}
          </button>
          <AddButton to="/music/add" label="Add artist" testId="music-add" />
        </div>
      </div>

      <Toolbar
        left={
          <>
            <TbButton icon="⟳" label="Update All" testId="music-update-all" onClick={updateAll} />
            <TbButton
              icon="⇄"
              label="RSS Sync"
              testId="music-rss-sync"
              disabled
              title="RSS feeds land in a later phase"
            />
            <TbButton
              icon="☑"
              label="Select Artists"
              testId="music-select"
              divider
              disabled
              title="Bulk selection lands in a later phase"
            />
          </>
        }
        right={
          <>
            <TbButton
              icon="⋯"
              label="Options"
              testId="music-options"
              divider
              disabled
              title="More actions land in a later phase"
            />
            <ViewToggle view={view} onChange={setView} testId="music-view" />
          </>
        }
      />

      {status && (
        <p className="dim small" data-testid="music-status" role="status">
          {status}
        </p>
      )}

      <FilterDrawer open={drawerOpen} onClose={() => setDrawerOpen(false)} onClear={clearFilters} activeCount={activeCount}>
        <FacetSelect
          label="Monitored"
          value={monitored}
          options={MONITORED_OPTIONS}
          onChange={(v) => setMonitored(v as MonitoredFilter)}
          testId="music-facet-monitored"
        />
        <FacetText
          label="Genre"
          value={facets.genre}
          onChange={(v) => facet('genre', v.trim())}
          placeholder="Metal"
          testId="music-facet-genre"
        />
        <FacetSelect
          label="Status"
          value={facets.status}
          options={STATUS_OPTIONS}
          onChange={(v) => facet('status', v)}
          testId="music-facet-status"
        />
      </FilterDrawer>

      {error && !data ? (
        <div className="card" data-testid="music-error">
          <h3>Artists unavailable</h3>
          <p className="error" data-testid="music-error-message">
            {error}
          </p>
          <button onClick={reload}>Retry</button>
        </div>
      ) : (
        <>
          {view === 'grid' && (
            <>
              {loading && !data ? (
                <p className="dim" data-testid="music-loading">
                  Loading artists…
                </p>
              ) : gridItems.length === 0 ? (
                <EmptyState
                  title="No results"
                  message={
                    filtered
                      ? 'Try adjusting or clearing your filters.'
                      : 'Add your first artist to get started.'
                  }
                >
                  {!filtered && (
                    <Link to="/music/add" data-testid="music-empty-add">
                      + Add artist
                    </Link>
                  )}
                </EmptyState>
              ) : (
                <>
                  <div className="poster-grid" data-testid="music-grid">
                    {gridItems.map((a) => {
                      const ribbon = deriveMusicRibbon(a)
                      return (
                        <div key={a.ID} data-poster-letter={firstLetter(a.Name)}>
                          <PosterCard
                            testId={`artist-card-${a.ID}`}
                            title={a.Name}
                            image={a.PosterURL || undefined}
                            meta={a.QualityProfile || FALLBACK_PROFILE_LABEL}
                            status={{ label: ribbon.text, tone: toneFor(ribbon.tone) }}
                            kind={{ label: 'Music' }}
                            onOpen={() => navigate(`/music/${a.ID}`)}
                            hoverActions={
                              <>
                                <button
                                  type="button"
                                  className="icon-btn"
                                  title="Refresh artist"
                                  aria-label={`Refresh ${a.Name}`}
                                  data-testid={`card-refresh-${a.ID}`}
                                  onClick={() => void refreshArtist(a)}
                                >
                                  ⟳
                                </button>
                                <button
                                  type="button"
                                  className="icon-btn"
                                  title="Edit artist"
                                  aria-label={`Edit ${a.Name}`}
                                  data-testid={`card-edit-${a.ID}`}
                                  onClick={() => navigate(`/music/${a.ID}`)}
                                >
                                  ✎
                                </button>
                              </>
                            }
                          />
                        </div>
                      )
                    })}
                  </div>
                  {/* #150: no pagination chrome — the sentinel loads the
                      next server page as the grid's end scrolls in. */}
                  <InfiniteSentinel onLoadMore={loadMore} disabled={!hasMore} testId="music-sentinel" />
                  {hasMore && loading && <p className="dim small">Loading more…</p>}
                </>
              )}
            </>
          )}

          {view === 'list' && (
            <div className="list-table" data-testid="music-table">
              <div className="lt-head">
                <span>Artist</span>
                <span>Monitor</span>
                <span>Quality</span>
                <span>Status</span>
                <span>Tracks</span>
                <span>Files</span>
                <span>Size</span>
              </div>
              {loading && !data ? (
                <p className="dim">Loading artists…</p>
              ) : listItems.length === 0 ? (
                <p className="dim" data-testid="music-empty">
                  No artists match.
                </p>
              ) : (
                listItems.map((a) => (
                  <div className="lt-row" key={a.ID} data-testid={`artist-${a.ID}`}>
                    <span className="lt-name" title={a.Name}>
                      <Link to={`/music/${a.ID}`} data-testid={`artist-link-${a.ID}`}>
                        {a.Name}
                      </Link>
                    </span>
                    <span>
                      <MonitoredToggle
                        monitored={a.Monitored}
                        onToggle={async (next) => {
                          await client.setArtistMonitored(a.ID, next)
                          reload()
                        }}
                        label={`${a.Name} monitoring`}
                        testId={`artist-mon-${a.ID}`}
                      />
                    </span>
                    <span className="lt-dim">{a.QualityProfile || '—'}</span>
                    <span className="lt-dim">{deriveMusicRibbon(a).state.replace('-', ' ')}</span>
                    <span className="lt-dim">{a.TrackCount ?? 0}</span>
                    <span className="lt-dim">{a.FileCount ?? 0}</span>
                    <span className="lt-dim">
                      {a.TotalSize ? humanBytes(a.TotalSize) : '—'}
                    </span>
                  </div>
                ))
              )}
            </div>
          )}

          {view === 'list' && (
            <Pagination page={page} pageSize={DEFAULT_PAGE_SIZE} total={total} onPage={setPage} />
          )}

          {/* Library footer: summary status bar + ribbon legend (Lidarr's
              "library footer"). */}
          {summary && (
            <div className="stats-bar" data-testid="music-summary">
              <span>
                Artist <b data-testid="summary-artists">{summary.artists}</b>
              </span>
              <span>
                Inactive <b data-testid="summary-inactive">{summary.inactive}</b>
              </span>
              <span>
                Continuing <b data-testid="summary-continuing">{summary.continuing}</b>
              </span>
              <span>
                Monitored <b data-testid="summary-monitored">{summary.monitored}</b>
              </span>
              <span>
                Unmonitored <b data-testid="summary-unmonitored">{summary.unmonitored}</b>
              </span>
              <span>
                Tracks <b data-testid="summary-tracks">{summary.tracks}</b>
              </span>
              <span>
                Files <b data-testid="summary-files">{summary.files}</b>
              </span>
              <span>
                Total File Size <b data-testid="summary-size">{humanBytes(summary.total_file_size)}</b>
              </span>
            </div>
          )}

          <div className="ribbon-legend" data-testid="music-legend">
            {MUSIC_RIBBON_LEGEND.map((l) => (
              <span key={l.state} data-testid={`legend-${l.state}`}>
                <span className={`legend-swatch ${l.tone}`} aria-hidden />
                {l.label}
              </span>
            ))}
          </div>
        </>
      )}
    </div>
  )
}
