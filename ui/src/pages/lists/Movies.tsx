import { useCallback, useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { client } from '../../api/client'
import type { ListQuery, PageMovie } from '../../api/types'
import { AddButton } from '../../components/addflow'
import {
  DEFAULT_PAGE_SIZE,
  MonitoredToggle,
  Pagination,
  usePagedList,
} from '../../components/list'
import { PosterGrid } from '../../components/poster'
import {
  EmptyState,
  Menu,
  Toolbar,
  TbButton,
  ViewToggle,
  type ViewMode,
} from '../../components/ui'

// MoviesPage is the P7 movies library — the same Sonarr-style pattern as the
// TV page (P2): a 6-column poster grid by default (click → the movie detail
// page with its release state), the shared toolbar (icon-over-label groups,
// View / Sort / Filter menus) and an A–Z quick-jump. The P1 table is kept
// behind the list view for bulk monitoring toggles.
type SortKey = 'title-asc' | 'title-desc' | 'year-asc' | 'year-desc' | 'added-asc' | 'added-desc'

const SORT_OPTIONS = [
  { value: 'title-asc', label: 'Title ↑' },
  { value: 'title-desc', label: 'Title ↓' },
  { value: 'year-asc', label: 'Year ↑' },
  { value: 'year-desc', label: 'Year ↓' },
  { value: 'added-asc', label: 'Added ↑' },
  { value: 'added-desc', label: 'Added ↓' },
] as const

function sortMovies(items: PageMovie['items'], key: SortKey): PageMovie['items'] {
  const dir = key.endsWith('-desc') ? -1 : 1
  const arr = [...items]
  arr.sort((a, b) => {
    let cmp = 0
    if (key.startsWith('title')) cmp = a.Title.localeCompare(b.Title)
    else if (key.startsWith('year')) cmp = a.Year - b.Year
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
  const [status, setStatus] = useState<string | null>(null)

  const items = useMemo(() => (data ? sortMovies(data.items, sort) : []), [data, sort])

  function updateAll() {
    setStatus(`Updated ${total} movies just now`)
    reload()
  }

  return (
    <div className="list-page">
      <div className="dash-head">
        <h2 data-testid="movies-title">Movies</h2>
        <div className="dash-head-right">
          <AddButton to="/movies/add" label="Add movie" testId="movies-add" />
        </div>
      </div>

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
              icon="☑"
              label="Select Movies"
              testId="movies-select"
              divider
              disabled
              title="Bulk selection lands in a later phase"
            />
            <TbButton
              icon="🔍"
              label="Test Parsing"
              testId="movies-test-parsing"
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
                <EmptyState title="No movies" message="Add your first movie to get started.">
                  <Link to="/movies/add" data-testid="movies-empty-add">
                    + Add movie
                  </Link>
                </EmptyState>
              ) : (
                <PosterGrid
                  items={items.map((m) => ({
                    id: m.ID,
                    name: m.Title,
                    year: m.Year,
                    monitored: m.Monitored,
                    quality: m.QualityProfile,
                  }))}
                  testIdPrefix="movie"
                  gridTestId="movies-grid"
                  azTestId="movies-az"
                  onOpen={(i) => navigate(`/movies/${i.id}`)}
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
