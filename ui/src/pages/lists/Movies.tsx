import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { client } from '../../api/client'
import type { ListQuery, Movie, MovieStats, PageMovie } from '../../api/types'
import { AddButton } from '../../components/addflow'
import {
  FacetCheckboxGroup,
  FacetDateRange,
  FacetNumberRange,
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
import { PosterCard } from '../../components/primitives/poster-card'
import { firstLetter, ribbonFor } from '../../components/poster'
import { EmptyState, Toolbar, TbButton, ViewToggle } from '../../components/ui'

// MoviesPage is the Seerr-style movie library (#150): a dense artwork-led
// poster grid by default (real PosterURL art with an initials stand-in
// when keyless, the kind pill + status badge over the art, hover
// refresh/edit actions), a heading control cluster — explicit sort
// direction toggle + sort field dropdown + a filter button that opens a
// slide-over drawer (NOT a funnel dropdown) with "N Active Filter(s)" and
// Clear Active Filters — and the Radarr toolbar/stats bar carried over.
// The grid sorts and filters server-side (the sort allowlist rejects
// unknown values with 400) and scrolls without pagination chrome; the
// table view stays fully functional behind the persisted view toggle.
type SortField = 'title' | 'year' | 'added' | 'release_date' | 'rating'
type SortDir = 'asc' | 'desc'

// The sort dropdown fields (the direction is the separate toggle).
const SORT_FIELDS = [
  { value: 'title', label: 'Title' },
  { value: 'release_date', label: 'Release Date' },
  { value: 'rating', label: 'TMDB Rating' },
  { value: 'year', label: 'Year' },
  { value: 'added', label: 'Added' },
] as const

// The filter-drawer facet state (#150). The zero value filters nothing.
interface MovieFacets {
  genres: string[]
  lang: string
  runtimeMin: number | null
  runtimeMax: number | null
  scoreMin: number | null
  scoreMax: number | null
  cert: string
  from: string
  to: string
  status: string
}

const EMPTY_FACETS: MovieFacets = {
  genres: [],
  lang: '',
  runtimeMin: null,
  runtimeMax: null,
  scoreMin: null,
  scoreMax: null,
  cert: '',
  from: '',
  to: '',
  status: '',
}

// The canonical TMDB movie genres (the drawer's genre checkboxes; the
// match itself happens server-side against persisted genres).
const TMDB_GENRES = [
  'Action',
  'Adventure',
  'Animation',
  'Comedy',
  'Crime',
  'Documentary',
  'Drama',
  'Family',
  'Fantasy',
  'History',
  'Horror',
  'Music',
  'Mystery',
  'Romance',
  'Science Fiction',
  'TV Movie',
  'Thriller',
  'War',
  'Western',
]

const CERTIFICATIONS = ['G', 'PG', 'PG-13', 'R', 'NC-17']

// Radarr v6 status chips; value = the movie's Status string (the drawer's
// status facet drives the server-side filter).
const STATUS_OPTIONS: { value: string; label: string }[] = [
  { value: 'downloaded:monitored', label: 'Downloaded (Monitored)' },
  { value: 'downloaded:unmonitored', label: 'Downloaded (Unmonitored)' },
  { value: 'missing:monitored', label: 'Missing (Monitored)' },
  { value: 'missing:unmonitored', label: 'Missing (Unmonitored)' },
  { value: 'queued', label: 'Queued' },
  { value: 'unreleased', label: 'Unreleased' },
]

const MONITORED_OPTIONS = [
  { value: 'all', label: 'All' },
  { value: 'monitored', label: 'Monitored' },
  { value: 'unmonitored', label: 'Unmonitored' },
]

function humanBytes(n: number): string {
  if (!n) return '0 B'
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB']
  let v = n
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v >= 100 ? Math.round(v) : v.toFixed(1)} ${units[i]}`
}

export function MoviesPage() {
  const navigate = useNavigate()
  const [view, setView] = usePersistentView('movies')
  // #150: the sort is field + explicit direction; flipping the direction
  // never changes the field (and vice versa).
  const [sortField, setSortField] = useState<SortField>('title')
  const [sortDir, setSortDir] = useState<SortDir>('asc')
  const [facets, setFacets] = useState<MovieFacets>(EMPTY_FACETS)
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [stats, setStats] = useState<MovieStats | null>(null)
  // Per-card refresh results (a real GET /movies/{id}) merged over the page.
  const [fresh, setFresh] = useState<Record<number, Movie>>({})
  const [status, setStatus] = useState<string | null>(null)

  const sort = `${sortField}-${sortDir}`
  // The facet params serialize into the wire query (empty values are
  // omitted by buildListQuery, so the server treats them as unset).
  const filters = useMemo<Record<string, string>>(
    () => ({
      genre: facets.genres.join(','),
      lang: facets.lang,
      runtime_min: facets.runtimeMin === null ? '' : String(facets.runtimeMin),
      runtime_max: facets.runtimeMax === null ? '' : String(facets.runtimeMax),
      score_min: facets.scoreMin === null ? '' : String(facets.scoreMin),
      score_max: facets.scoreMax === null ? '' : String(facets.scoreMax),
      cert: facets.cert,
      from: facets.from,
      to: facets.to,
      status: facets.status,
    }),
    [facets],
  )

  const load = useCallback(
    (q: ListQuery) => client.listMovies({ ...q, sort, filters }),
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
  } = usePagedList<PageMovie>(load, { mode: 'append' })

  const loadStats = useCallback(() => {
    client
      .getMovieStats()
      .then(setStats)
      .catch(() => setStats(null)) // stats are cosmetic; a failure must not break the grid
  }, [])

  useEffect(() => {
    loadStats()
  }, [loadStats, data])

  // The drawer's active count: every set facet plus the monitored filter.
  const activeCount = useMemo(() => {
    let n = monitored === 'all' ? 0 : 1
    if (facets.genres.length > 0) n++
    if (facets.lang) n++
    if (facets.runtimeMin !== null || facets.runtimeMax !== null) n++
    if (facets.scoreMin !== null || facets.scoreMax !== null) n++
    if (facets.cert) n++
    if (facets.from || facets.to) n++
    if (facets.status) n++
    return n
  }, [facets, monitored])

  const gridItems = useMemo(() => items.map((m) => fresh[m.ID] ?? m), [items, fresh])
  const listItems = useMemo(() => (data ? data.items : []), [data])
  const filtered = activeCount > 0

  function updateAll() {
    setStatus(`Updated ${total} movies just now`)
    reload()
    loadStats()
  }

  // Per-card refresh: re-fetch this movie from the API and merge it in.
  async function refreshMovie(m: Movie) {
    setStatus(null)
    try {
      const freshMovie = await client.getMovie(m.ID)
      setFresh((prev) => ({ ...prev, [m.ID]: freshMovie }))
    } catch (e) {
      setStatus(e instanceof Error ? e.message : `refresh of ${m.Title} failed`)
    }
  }

  function clearFilters() {
    setFacets(EMPTY_FACETS)
    setMonitored('all')
  }

  const facet = <K extends keyof MovieFacets>(key: K, value: MovieFacets[K]) =>
    setFacets((prev) => ({ ...prev, [key]: value }))

  return (
    <div className="list-page">
      <div className="dash-head">
        <h2 data-testid="movies-title">Movies</h2>
        <div className="dash-head-right">
          <SortCluster
            field={sortField}
            fields={[...SORT_FIELDS]}
            dir={sortDir}
            onDir={setSortDir}
            onField={(f) => setSortField(f as SortField)}
            testId="movies"
          />
          <button
            type="button"
            className="menu-btn"
            onClick={() => setDrawerOpen(true)}
            aria-haspopup="dialog"
            data-testid="movies-filter-btn"
          >
            <span aria-hidden>⚲</span>
            {activeCount} Active Filter{activeCount === 1 ? '' : 's'}
          </button>
          <AddButton to="/movies/add" label="Add movie" testId="movies-add" />
        </div>
      </div>

      {stats && (
        <div className="stats-bar" data-testid="movies-stats">
          <span>
            Movies <b data-testid="stats-movies">{stats.MovieCount}</b>
          </span>
          <span>
            Movie Files <b data-testid="stats-files">{stats.FileCount}</b>
          </span>
          <span>
            Monitored <b data-testid="stats-monitored">{stats.MonitoredCount}</b>
          </span>
          <span>
            Unmonitored <b data-testid="stats-unmonitored">{stats.UnmonitoredCount}</b>
          </span>
          <span>
            Total File Size <b data-testid="stats-size">{humanBytes(stats.TotalSize)}</b>
          </span>
        </div>
      )}

      <Toolbar
        left={
          <>
            <TbButton icon="⟳" label="Update All" testId="movies-update-all" onClick={updateAll} />
            <TbButton
              icon="⇄"
              label="RSS Sync"
              testId="movies-rss-sync"
              disabled
              title="RSS feeds land in a later phase"
            />
            <TbButton
              icon="🔍"
              label="Search All"
              testId="movies-search-all"
              divider
              disabled
              title="Bulk search lands in a later phase"
            />
            <TbButton
              icon="⇥"
              label="Manual Import"
              testId="movies-manual-import"
              disabled
              title="Library import lands in a later phase"
            />
            <TbButton
              icon="✎"
              label="Edit Movies"
              testId="movies-bulk-edit"
              divider
              disabled
              title="Bulk editing lands in a later phase"
            />
          </>
        }
        right={
          <>
            <TbButton
              icon="⋯"
              label="Options"
              testId="movies-options"
              divider
              disabled
              title="More actions land in a later phase"
            />
            <ViewToggle view={view} onChange={setView} testId="movies-view" />
          </>
        }
      />

      {status && (
        <p className="dim small" data-testid="movies-status" role="status">
          {status}
        </p>
      )}

      <FilterDrawer open={drawerOpen} onClose={() => setDrawerOpen(false)} onClear={clearFilters} activeCount={activeCount}>
        <FacetSelect
          label="Monitored"
          value={monitored}
          options={MONITORED_OPTIONS}
          onChange={(v) => setMonitored(v as MonitoredFilter)}
          testId="movies-facet-monitored"
        />
        <FacetCheckboxGroup
          label="Genres"
          options={TMDB_GENRES.map((g) => ({ value: g, label: g }))}
          values={facets.genres}
          onToggle={(values) => facet('genres', values)}
          testId="movies-facet-genres"
        />
        <FacetText
          label="Original Language (ISO)"
          value={facets.lang}
          onChange={(v) => facet('lang', v.trim())}
          placeholder="en"
          testId="movies-facet-lang"
        />
        <FacetNumberRange
          label="Runtime (min)"
          min={facets.runtimeMin}
          max={facets.runtimeMax}
          onMin={(v) => facet('runtimeMin', v)}
          onMax={(v) => facet('runtimeMax', v)}
          testId="movies-facet-runtime"
        />
        <FacetNumberRange
          label="TMDB Score (0-100)"
          min={facets.scoreMin}
          max={facets.scoreMax}
          onMin={(v) => facet('scoreMin', v)}
          onMax={(v) => facet('scoreMax', v)}
          testId="movies-facet-score"
        />
        <FacetSelect
          label="Content Rating"
          value={facets.cert}
          options={CERTIFICATIONS.map((c) => ({ value: c, label: c }))}
          onChange={(v) => facet('cert', v)}
          testId="movies-facet-cert"
        />
        <FacetDateRange
          label="Release Date"
          from={facets.from}
          to={facets.to}
          onFrom={(v) => facet('from', v)}
          onTo={(v) => facet('to', v)}
          testId="movies-facet-date"
        />
        <FacetSelect
          label="Status"
          value={facets.status}
          options={STATUS_OPTIONS}
          onChange={(v) => facet('status', v)}
          testId="movies-facet-status"
        />
      </FilterDrawer>

      {error && !data ? (
        <div className="card" data-testid="movies-error">
          <h3>Movies unavailable</h3>
          <p className="error" data-testid="movies-error-message">
            {error}
          </p>
          <button onClick={reload}>Retry</button>
        </div>
      ) : (
        <>
          {view === 'grid' && (
            <>
              {loading && !data ? (
                <p className="dim">Loading movies…</p>
              ) : gridItems.length === 0 ? (
                <EmptyState
                  title="No results"
                  message={filtered ? 'Try adjusting or clearing your filters.' : 'Add your first movie to get started.'}
                >
                  {!filtered && (
                    <Link to="/movies/add" data-testid="movies-empty-add">
                      + Add movie
                    </Link>
                  )}
                </EmptyState>
              ) : (
                <>
                  <div className="poster-grid" data-testid="movies-grid">
                    {gridItems.map((m) => {
                      const ribbon = ribbonFor({ monitored: m.Monitored, status: m.Status })
                      return (
                        <div key={m.ID} data-poster-letter={firstLetter(m.Title)}>
                          <PosterCard
                            testId={`movie-card-${m.ID}`}
                            title={m.Title}
                            image={m.PosterURL || undefined}
                            meta={m.Year ? `${m.Year} · ${m.QualityProfile}` : m.QualityProfile}
                            status={ribbon ? { label: ribbon.text, tone: toneFor(ribbon.tone) } : undefined}
                            kind={{ label: 'Movie' }}
                            onOpen={() => navigate(`/movies/${m.ID}`)}
                            hoverActions={
                              <>
                                <button
                                  type="button"
                                  className="icon-btn"
                                  title="Refresh movie"
                                  aria-label={`Refresh ${m.Title}`}
                                  data-testid={`card-refresh-${m.ID}`}
                                  onClick={() => void refreshMovie(m)}
                                >
                                  ⟳
                                </button>
                                <button
                                  type="button"
                                  className="icon-btn"
                                  title="Edit movie"
                                  aria-label={`Edit ${m.Title}`}
                                  data-testid={`card-edit-${m.ID}`}
                                  onClick={() => navigate(`/movies/${m.ID}`)}
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
                  <InfiniteSentinel onLoadMore={loadMore} disabled={!hasMore} testId="movies-sentinel" />
                  {hasMore && loading && <p className="dim small">Loading more…</p>}
                </>
              )}
            </>
          )}

          {view === 'list' && (
            <div className="list-table" data-testid="movies-table">
              <div className="lt-head">
                <span>Title</span>
                <span>Year</span>
                <span>Quality</span>
                <span>Monitor</span>
              </div>
              {loading && !data ? (
                <p className="dim">Loading movies…</p>
              ) : listItems.length === 0 ? (
                <p className="dim" data-testid="movies-empty">
                  No movies match.
                </p>
              ) : (
                listItems.map((m) => (
                  <div className="lt-row" key={m.ID} data-testid={`movie-${m.ID}`}>
                    <span className="lt-name" title={m.Title}>
                      <Link to={`/movies/${m.ID}`} data-testid={`movie-link-${m.ID}`}>
                        {m.Title}
                      </Link>
                    </span>
                    <span className="lt-dim">{m.Year || '—'}</span>
                    <span className="lt-dim">{m.QualityProfile}</span>
                    <MonitoredToggle
                      monitored={m.Monitored}
                      onToggle={async (next) => {
                        await client.setMovieMonitored(m.ID, next)
                        reload()
                      }}
                      label={`${m.Title} monitoring`}
                      testId={`movie-mon-${m.ID}`}
                    />
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
