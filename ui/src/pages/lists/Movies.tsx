import { useCallback } from 'react'
import { client } from '../../api/client'
import type { ListQuery, PageMovie } from '../../api/types'
import { AddButton } from '../../components/addflow'
import {
  DEFAULT_PAGE_SIZE,
  ListControls,
  MonitoredToggle,
  Pagination,
  usePagedList,
} from '../../components/list'

// MoviesPage is the per-kind list view for movies (#41): a server-side
// filtered, paginated table with an optimistic monitored toggle per row.
// Movies are flat (no children to expand).
export function MoviesPage() {
  const load = useCallback((q: ListQuery) => client.listMovies(q), [])
  const { data, loading, error, text, setText, monitored, setMonitored, page, setPage, total, reload } = usePagedList<PageMovie>(load)

  return (
    <div className="list-page">
      <div className="dash-head">
        <h2 data-testid="movies-title">Movies</h2>
        <div className="dash-head-right">
          <AddButton to="/movies/add" label="Add movie" testId="movies-add" />
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
        <div className="card" data-testid="movies-error">
          <h3>Movies unavailable</h3>
          <p className="error" data-testid="movies-error-message">
            {error}
          </p>
          <button onClick={reload}>Retry</button>
        </div>
      ) : (
        <>
          <div className="list-table" data-testid="movies-table">
            <div className="lt-head">
              <span>Title</span>
              <span>Year</span>
              <span>Quality</span>
              <span>Monitor</span>
            </div>
            {loading && !data ? (
              <p className="dim">Loading movies…</p>
            ) : data && data.items.length === 0 ? (
              <p className="dim" data-testid="movies-empty">
                No movies match.
              </p>
            ) : (
              data?.items.map((m) => (
                <div className="lt-row" key={m.ID} data-testid={`movie-${m.ID}`}>
                  <span className="lt-name" title={m.Title}>
                    {m.Title}
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
          <Pagination page={page} pageSize={DEFAULT_PAGE_SIZE} total={total} onPage={setPage} />
        </>
      )}
    </div>
  )
}
