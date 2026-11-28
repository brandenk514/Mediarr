import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { client } from '../../api/client'
import type { Episode, ListQuery, PageSeries, Series } from '../../api/types'
import { AddButton } from '../../components/addflow'
import {
  FacetSelect,
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
import { PosterCard } from '../../components/primitives/poster-card'
import { firstLetter, ribbonFor } from '../../components/poster'
import { EmptyState, Toolbar, TbButton, ViewToggle } from '../../components/ui'

// TvPage is the Seerr-style TV library (#150): a dense artwork-led poster
// grid by default (real PosterURL art with an initials stand-in when
// keyless, the kind pill over the art, hover refresh/edit actions), a
// heading control cluster — explicit sort direction toggle + sort field
// dropdown + a filter button that opens a slide-over drawer with "N Active
// Filter(s)" and Clear Active Filters — and the Sonarr toolbar carried
// over. The grid sorts and filters server-side (the sort allowlist rejects
// unknown values with 400) and scrolls without pagination chrome; the
// expandable table (P1, with per-episode rows) stays fully functional
// behind the persisted view toggle.
type SortField = 'title' | 'year' | 'added'
type SortDir = 'asc' | 'desc'

// The sort dropdown fields (the direction is the separate toggle).
const SORT_FIELDS = [
  { value: 'title', label: 'Title' },
  { value: 'year', label: 'Year' },
  { value: 'added', label: 'Added' },
] as const

const MONITORED_OPTIONS = [
  { value: 'all', label: 'All' },
  { value: 'monitored', label: 'Monitored' },
  { value: 'unmonitored', label: 'Unmonitored' },
]

// The drawer's facet state (#150). The zero value filters nothing. TV's
// facet surface is the monitored filter only — the genre facets wait on
// the series enrichment (#157).
interface TvFacets {
  // (reserved for #157 genre facets)
}

const EMPTY_FACETS: TvFacets = {}

export function TvPage() {
  const navigate = useNavigate()
  const [view, setView] = usePersistentView('tv')
  // #150: the sort is field + explicit direction; flipping the direction
  // never changes the field (and vice versa).
  const [sortField, setSortField] = useState<SortField>('title')
  const [sortDir, setSortDir] = useState<SortDir>('asc')
  const [facets, setFacets] = useState<TvFacets>(EMPTY_FACETS)
  const [drawerOpen, setDrawerOpen] = useState(false)
  // Per-card refresh results (a real GET /tv/{id}) merged over the page.
  const [fresh, setFresh] = useState<Record<number, Series>>({})
  const [status, setStatus] = useState<string | null>(null)

  const sort = `${sortField}-${sortDir}`
  const filters = useMemo<Record<string, string>>(() => ({}), [])

  const load = useCallback(
    (q: ListQuery) => client.listSeries({ ...q, sort, filters }),
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
    expanded,
    toggleExpanded,
  } = usePagedList<PageSeries>(load, { mode: 'append' })

  // The drawer's active count: every set facet plus the monitored filter.
  const activeCount = useMemo(() => {
    let n = monitored === 'all' ? 0 : 1
    for (const value of Object.values(facets)) if (value) n++
    return n
  }, [facets, monitored])

  const gridItems = useMemo(() => items.map((s) => fresh[s.ID] ?? s), [items, fresh])
  const listItems = useMemo(() => (data ? data.items : []), [data])
  const filtered = activeCount > 0

  function updateAll() {
    setStatus(`Updated ${total} series just now`)
    reload()
  }

  // Per-card refresh: re-fetch this series from the API and merge it in.
  async function refreshSeries(s: Series) {
    setStatus(null)
    try {
      const freshSeries = await client.getSeries(s.ID)
      setFresh((prev) => ({ ...prev, [s.ID]: freshSeries }))
    } catch (e) {
      setStatus(e instanceof Error ? e.message : `refresh of ${s.Title} failed`)
    }
  }

  function clearFilters() {
    setFacets(EMPTY_FACETS)
    setMonitored('all')
  }

  return (
    <div className="list-page">
      <div className="dash-head">
        <h2 data-testid="tv-title">Series</h2>
        <div className="dash-head-right">
          <SortCluster
            field={sortField}
            fields={[...SORT_FIELDS]}
            dir={sortDir}
            onDir={setSortDir}
            onField={(f) => setSortField(f as SortField)}
            testId="tv"
          />
          <button
            type="button"
            className="menu-btn"
            onClick={() => setDrawerOpen(true)}
            aria-haspopup="dialog"
            data-testid="tv-filter-btn"
          >
            <span aria-hidden>⚲</span>
            {activeCount} Active Filter{activeCount === 1 ? '' : 's'}
          </button>
          <AddButton to="/tv/add" label="Add series" testId="tv-add" />
        </div>
      </div>

      <Toolbar
        left={
          <>
            <TbButton icon="⟳" label="Update All" testId="tv-update-all" onClick={updateAll} />
            <TbButton
              icon="⇄"
              label="RSS Sync"
              testId="tv-rss-sync"
              disabled
              title="RSS feeds land in a later phase"
            />
            <TbButton
              icon="☑"
              label="Select Series"
              testId="tv-select-series"
              divider
              disabled
              title="Bulk selection lands in a later phase"
            />
            <TbButton
              icon="🔍"
              label="Test Parsing"
              testId="tv-test-parsing"
              disabled
              title="Parse testing lands in a later phase"
            />
          </>
        }
        right={
          <>
            <TbButton
              icon="⋯"
              label="Options"
              testId="tv-options"
              divider
              disabled
              title="More actions land in a later phase"
            />
            <ViewToggle view={view} onChange={setView} testId="tv-view" />
          </>
        }
      />

      {status && (
        <p className="dim small" data-testid="tv-status" role="status">
          {status}
        </p>
      )}

      <FilterDrawer open={drawerOpen} onClose={() => setDrawerOpen(false)} onClear={clearFilters} activeCount={activeCount}>
        <FacetSelect
          label="Monitored"
          value={monitored}
          options={MONITORED_OPTIONS}
          onChange={(v) => setMonitored(v as MonitoredFilter)}
          testId="tv-facet-monitored"
        />
      </FilterDrawer>

      {error && !data ? (
        <div className="card" data-testid="tv-error">
          <h3>TV unavailable</h3>
          <p className="error" data-testid="tv-error-message">
            {error}
          </p>
          <button onClick={reload}>Retry</button>
        </div>
      ) : (
        <>
          {view === 'grid' && (
            <>
              {loading && !data ? (
                <p className="dim">Loading series…</p>
              ) : gridItems.length === 0 ? (
                <EmptyState
                  title="No results"
                  message={
                    filtered
                      ? 'Try adjusting or clearing your filters.'
                      : 'Add your first series to get started.'
                  }
                >
                  {!filtered && (
                    <Link to="/tv/add" data-testid="tv-empty-add">
                      + Add series
                    </Link>
                  )}
                </EmptyState>
              ) : (
                <>
                  <div className="poster-grid" data-testid="tv-grid">
                    {gridItems.map((s) => {
                      // Series has no status field: the ribbon falls back to
                      // the legacy unmonitored marker (ribbonFor handles it).
                      const ribbon = ribbonFor({ monitored: s.Monitored })
                      return (
                        <div key={s.ID} data-poster-letter={firstLetter(s.Title)}>
                          <PosterCard
                            testId={`series-card-${s.ID}`}
                            title={s.Title}
                            image={s.PosterURL || undefined}
                            meta={s.Year ? `${s.Year} · ${s.QualityProfile}` : s.QualityProfile}
                            status={ribbon ? { label: ribbon.text, tone: toneFor(ribbon.tone) } : undefined}
                            kind={{ label: 'TV' }}
                            onOpen={() => navigate(`/tv/${s.ID}`)}
                            hoverActions={
                              <>
                                <button
                                  type="button"
                                  className="icon-btn"
                                  title="Refresh series"
                                  aria-label={`Refresh ${s.Title}`}
                                  data-testid={`card-refresh-${s.ID}`}
                                  onClick={() => void refreshSeries(s)}
                                >
                                  ⟳
                                </button>
                                <button
                                  type="button"
                                  className="icon-btn"
                                  title="Edit series"
                                  aria-label={`Edit ${s.Title}`}
                                  data-testid={`card-edit-${s.ID}`}
                                  onClick={() => navigate(`/tv/${s.ID}`)}
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
                  <InfiniteSentinel onLoadMore={loadMore} disabled={!hasMore} testId="tv-sentinel" />
                  {hasMore && loading && <p className="dim small">Loading more…</p>}
                </>
              )}
            </>
          )}

          {view === 'list' && (
            <div className="list-list" data-testid="tv-list">
              {loading && !data ? (
                <p className="dim">Loading series…</p>
              ) : listItems.length === 0 ? (
                <p className="dim" data-testid="tv-empty">
                  No series match.
                </p>
              ) : (
                listItems.map((s) => (
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
            </div>
          )}

          {view === 'list' && (
            <Pagination page={page} pageSize={DEFAULT_PAGE_SIZE} total={total} onPage={setPage} />
          )}
        </>
      )}
    </div>
  )
}

// EpisodeList fetches a series' episodes only after its row is expanded (list
// view), so collapsed rows cost no child request.
function EpisodeList({ seriesId }: { seriesId: number }) {
  const [eps, setEps] = useState<Episode[] | null>(null)
  const [err, setErr] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    client
      .listEpisodes(seriesId)
      .then((e) => !cancelled && setEps(e ?? [])) // older backends return null
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
