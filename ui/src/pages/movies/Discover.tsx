import { useCallback, useEffect, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { client } from '../../api/client'
import type { MovieSearchHit } from '../../api/types'
import { Badge, BadgeRow, EmptyState } from '../../components/ui'

// MovieDiscoverPage is the Radarr R3 Discover page: a grid of TMDB trending
// movie cards (title (year), genre chips, studio, overview excerpt) whose
// 'Add Movie' action opens the R2 Add Movie page pre-selected via
// ?tmdb=<id>. Keyless dev: the endpoint 503s and the page shows the reason
// in a greyed-out empty state (the R2 search grey-out pattern) — manual
// entry via /movies/add still works.
export function MovieDiscoverPage() {
  const navigate = useNavigate()
  const [page, setPage] = useState(1)
  const [hits, setHits] = useState<MovieSearchHit[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [unavailable, setUnavailable] = useState<string | null>(null)

  const load = useCallback((p: number) => {
    setLoading(true)
    setUnavailable(null)
    client
      .discoverMovies({ page: p })
      .then((r) => {
        setHits(r.results ?? [])
        setTotal(r.total ?? 0)
      })
      .catch((e: unknown) => {
        setHits([])
        setTotal(0)
        setUnavailable(e instanceof Error ? e.message : 'TMDB is unavailable')
      })
      .finally(() => setLoading(false))
  }, [])

  useEffect(() => {
    load(page)
  }, [page, load])

  return (
    <div className="list-page">
      <div className="dash-head">
        <h2 data-testid="discover-title">Discover</h2>
      </div>

      {unavailable ? (
        <EmptyState
          testId="discover-unavailable"
          title="Discover unavailable"
          message={`${unavailable} — add a TMDB API key in Settings to browse trending movies.`}
        >
          <Link to="/movies" data-testid="discover-unavailable-back">
            ← Back to movies
          </Link>
        </EmptyState>
      ) : loading && hits.length === 0 ? (
        <p className="dim">Loading trending movies…</p>
      ) : hits.length === 0 ? (
        <EmptyState testId="discover-empty" title="No movies" message="Nothing trending right now." />
      ) : (
        <>
          <div className="discover-grid" data-testid="discover-grid">
            {hits.map((h) => (
              <DiscoverCard key={h.id} h={h} onAdd={() => navigate(`/movies/add?tmdb=${h.id}`)} />
            ))}
          </div>
          <div className="discover-pager">
            {page > 1 && (
              <button
                type="button"
                disabled={loading}
                onClick={() => setPage(page - 1)}
                data-testid="discover-prev"
              >
                ← Previous
              </button>
            )}
            <button
              type="button"
              disabled={loading || hits.length === 0}
              onClick={() => setPage(page + 1)}
              data-testid="discover-next"
            >
              Next page →
            </button>
          </div>
          <p className="dim small" data-testid="discover-count">
            Page {page} of trending movies ({total} total)
          </p>
        </>
      )}
    </div>
  )
}

function DiscoverCard({ h, onAdd }: { h: MovieSearchHit; onAdd: () => void }) {
  return (
    <div className="card discover-card" data-testid={`discover-card-${h.id}`}>
      <span className="collection-poster" aria-hidden>
        {h.poster ? <img src={h.poster} alt="" /> : h.title.charAt(0).toUpperCase()}
      </span>
      <h3 data-testid={`discover-title-${h.id}`}>
        {h.title}
        {h.year > 0 ? <span className="dim"> ({h.year})</span> : null}
      </h3>
      <BadgeRow>
        {h.genres.map((g) => (
          <Badge key={g}>{g}</Badge>
        ))}
      </BadgeRow>
      {h.studio && (
        <p className="dim small" data-testid={`discover-studio-${h.id}`}>
          {h.studio}
        </p>
      )}
      {h.overview && (
        <p className="dim small" data-testid={`discover-overview-${h.id}`}>
          {h.overview}
        </p>
      )}
      <button type="button" onClick={onAdd} data-testid={`discover-add-${h.id}`}>
        + Add Movie
      </button>
    </div>
  )
}
