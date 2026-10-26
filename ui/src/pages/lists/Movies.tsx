import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { client } from '../../api/client'
import type { ListQuery, Movie, MovieStats, PageMovie } from '../../api/types'
import { AddButton } from '../../components/addflow'
import {
  DEFAULT_PAGE_SIZE,
  MonitoredToggle,
  Pagination,
  usePagedList,
} from '../../components/list'
import { PosterGrid, type PosterItem } from '../../components/poster'
import {
  EmptyState,
  Menu,
  Toolbar,
  TbButton,
  ViewToggle,
  type ViewMode,
} from '../../components/ui'

// MoviesPage is the Radarr R1 movies library: a 6-up poster grid with
// status ribbons (Downloaded / Missing / Queued / Unreleased) and per-card
// hover controls (Refresh movie / Edit movie), Radarr-v6 status filter
// chips, the stats bar (Movies · Movie Files · Monitored · Unmonitored ·
// Total File Size) and the Radarr toolbar (Update All, RSS Sync | Search
// All, Manual Import | Edit Movies). Buttons without a backend behind them
// are disabled with a "later phase" title (the P7 pattern); Update All and
// per-card Refresh hit real endpoints. The list view (Title/Year/Quality/
// Monitor) and the A–Z jump carry over from P7.
type SortKey = 'title-asc' | 'title-desc' | 'year-asc' | 'year-desc' | 'added-asc' | 'added-desc'

const SORT_OPTIONS = [
  { value: 'title-asc', label: 'Title ↑' },
  { value: 'title-desc', label: 'Title ↓' },
  { value: 'year-asc', label: 'Year ↑' },
  { value: 'year-desc', label: 'Year ↓' },
  { value: 'added-asc', label: 'Added ↑' },
  { value: 'added-desc', label: 'Added ↓' },
] as const

// Radarr v6 status chips; value = the movie's Status string to keep.
const STATUS_CHIPS: { value: string; label: string }[] = [
  { value: 'downloaded:monitored', label: 'Downloaded (Monitored)' },
  { value: 'downloaded:unmonitored', label: 'Downloaded (Unmonitored)' },
  { value: 'missing:monitored', label: 'Missing (Monitored)' },
  { value: 'missing:unmonitored', label: 'Missing (Unmonitored)' },
  { value: 'queued', label: 'Queued' },
  { value: 'unreleased', label: 'Unreleased' },
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

function sortMovies(items: Movie[], key: SortKey): Movie[] {
  const dir = key.endsWith('-desc') ? -1 : 1
  const arr = [...items]
  arr.sort((a, b) => {
    let cmp = 0
    if (key.startsWith('title')) cmp = a.Title.localeCompare(b.Title)
    else if (key.startsWith('year')) cmp = (a.Year ?? 0) - (b.Year ?? 0)
    else cmp = String(a.AddedAt).localeCompare(String(b.AddedAt))
    if (cmp === 0) cmp = a.ID - b.ID // stable tiebreak
    return cmp * dir
  })
  return arr
}

export function MoviesPage() {
  const navigate = useNavigate()
  const load = useCallback((q: ListQuery) => client.listMovies(q), [])
  const {
    data,
    loading,
    error,
    monitored,
    setMonitored,
    page,
    setPage,
    total,
    reload,
  } = usePagedList<PageMovie>(load)

  const [view, setView] = useState<ViewMode>('grid')
  const [sort, setSort] = useState<SortKey>('title-asc')
  const [chip, setChip] = useState<string | null>(null)
  const [stats, setStats] = useState<MovieStats | null>(null)
  // Per-card refresh results (a real GET /movies/{id}) merged over the page.
  const [fresh, setFresh] = useState<Record<number, Movie>>({})
  const [status, setStatus] = useState<string | null>(null)

  const loadStats = useCallback(() => {
    client
      .getMovieStats()
      .then(setStats)
      .catch(() => setStats(null)) // stats are cosmetic; a failure must not break the grid
  }, [])

  useEffect(() => {
    loadStats()
  }, [loadStats, data])

  const baseItems = useMemo(() => (data ? data.items : []), [data])

  const items = useMemo(() => {
    const merged = baseItems.map((m) => fresh[m.ID] ?? m)
    const filtered = chip ? merged.filter((m) => m.Status === chip) : merged
    return sortMovies(filtered, sort)
  }, [baseItems, fresh, chip, sort])

  const gridItems: PosterItem[] = items.map((m) => ({
    id: m.ID,
    name: m.Title,
    year: m.Year,
    monitored: m.Monitored,
    quality: m.QualityProfile,
    status: m.Status,
  }))

  function updateAll() {
    setStatus(`Updated ${total} movies just now`)
    reload()
    loadStats()
  }

  // Per-card refresh: re-fetch this movie from the API and merge it in.
  async function refreshMovie(item: PosterItem) {
    setStatus(null)
    try {
      const m = await client.getMovie(item.id)
      setFresh((prev) => ({ ...prev, [item.id]: m }))
    } catch (e) {
      setStatus(e instanceof Error ? e.message : `refresh of ${item.name} failed`)
    }
  }

  return (
    <div className="list-page">
      <div className="dash-head">
        <h2 data-testid="movies-title">Movies</h2>
        <div className="dash-head-right">
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
            <Menu
              label="Sort"
              icon="↕"
              options={[...SORT_OPTIONS]}
              selected={sort}
              onSelect={(v) => setSort(v as SortKey)}
              testId="movies-sort"
            />
            <Menu
              label="Filter"
              icon="⚲"
              options={[
                { value: 'all', label: 'All' },
                { value: 'monitored', label: 'Monitored' },
                { value: 'unmonitored', label: 'Unmonitored' },
              ]}
              selected={monitored}
              onSelect={(v) => setMonitored(v as 'all' | 'monitored' | 'unmonitored')}
              testId="movies-filter"
            />
          </>
        }
      />

      {status && (
        <p className="dim small" data-testid="movies-status" role="status">
          {status}
        </p>
      )}

      <div className="status-chips" data-testid="movies-chips">
        {STATUS_CHIPS.map((c) => (
          <button
            key={c.value}
            type="button"
            className={`chip ${chip === c.value ? 'active' : ''}`}
            data-testid={`movies-chip-${c.value.replace(/[:()]/g, '-').toLowerCase()}`}
            onClick={() => setChip(chip === c.value ? null : c.value)}
          >
            {c.label}
          </button>
        ))}
      </div>

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
              ) : items.length === 0 ? (
                <EmptyState
                  title={chip ? 'No movies with this status' : 'No movies'}
                  message={
                    chip
                      ? 'Try a different status filter.'
                      : 'Add your first movie to get started.'
                  }
                >
                  {!chip && (
                    <Link to="/movies/add" data-testid="movies-empty-add">
                      + Add movie
                    </Link>
                  )}
                </EmptyState>
              ) : (
                <PosterGrid
                  items={gridItems}
                  testIdPrefix="movie"
                  gridTestId="movies-grid"
                  azTestId="movies-az"
                  onOpen={(i) => navigate(`/movies/${i.id}`)}
                  onRefresh={(i) => void refreshMovie(i)}
                  onEdit={(i) => navigate(`/movies/${i.id}`)}
                />
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
              ) : items.length === 0 ? (
                <p className="dim" data-testid="movies-empty">
                  No movies match.
                </p>
              ) : (
                items.map((m) => (
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

          <Pagination page={page} pageSize={DEFAULT_PAGE_SIZE} total={total} onPage={setPage} />
        </>
      )}
    </div>
  )
}
