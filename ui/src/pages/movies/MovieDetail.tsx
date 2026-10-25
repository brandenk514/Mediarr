import { useCallback, useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { ApiError, client } from '../../api/client'
import type { Movie, MovieWanted } from '../../api/types'
import { MonitoredToggle } from '../../components/list'
import { Badge, BadgeRow, Toolbar, TbButton } from '../../components/ui'

// MovieDetail is the P7 movie page — the same detail pattern as the TV series
// page (P2): a back link + title header, a Sonarr toolbar (Refresh & Scan /
// a search action / Options), a badge header with the movie's metadata, and
// the content below. Where TV shows seasons/episodes, a movie is flat, so the
// content is the movie's release state: the wanted row that tracks its
// "we want a file" lifecycle (pending → satisfied by a release).
export function MovieDetailPage() {
  const { id } = useParams<{ id: string }>()
  const movieId = Number(id)
  const [movie, setMovie] = useState<Movie | null>(null)
  const [wanted, setWanted] = useState<MovieWanted | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [authLost, setAuthLost] = useState(false)
  const [status, setStatus] = useState<string | null>(null)
  const [searching, setSearching] = useState(false)

  const load = useCallback(async () => {
    if (!movieId || Number.isNaN(movieId)) {
      setError('Unknown movie.')
      return
    }
    try {
      const [m, w] = await Promise.all([client.getMovie(movieId), client.getMovieWanted(movieId)])
      setMovie(m)
      setWanted(w)
      setError(null)
      setAuthLost(false)
    } catch (e) {
      if (e instanceof ApiError && e.status === 404) {
        setError(`Movie ${movieId} was not found.`)
        return
      }
      if (e instanceof ApiError && e.status === 401) setAuthLost(true)
      setError(e instanceof Error ? e.message : 'failed to load movie')
    }
  }, [movieId])

  useEffect(() => {
    void load()
  }, [load])

  async function searchReleases() {
    if (searching) return
    setSearching(true)
    setStatus(null)
    try {
      const res = await client.runMoviePipeline(movieId)
      setStatus(`Pipeline finished — imported ${res.imported || 'a file'}.`)
      await load()
    } catch (e) {
      setStatus(e instanceof Error ? e.message : 'release search failed')
    } finally {
      setSearching(false)
    }
  }

  if (error && !movie) {
    return (
      <div className="card" data-testid="movie-detail-error">
        <h2>Movie unavailable</h2>
        <p className="error" data-testid="movie-detail-error-message">
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

  if (!movie) {
    return <p className="dim" data-testid="movie-detail-loading">Loading movie…</p>
  }

  return (
    <div className="list-page" data-testid="movie-detail">
      <div className="page-head">
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.8rem' }}>
          <Link to="/movies" className="ghost" data-testid="movie-detail-back">
            ← Movies
          </Link>
          <h2>
            {movie.Title}
            {movie.Year > 0 ? ` (${movie.Year})` : ''}
          </h2>
        </div>
      </div>

      <Toolbar
        left={
          <>
            <TbButton icon="⟳" label="Refresh & Scan" testId="movie-detail-refresh" onClick={() => void load()} />
            <TbButton
              icon="🔍"
              label="Search Releases"
              testId="movie-detail-search"
              disabled={searching}
              onClick={() => void searchReleases()}
            />
          </>
        }
        right={
          <>
            <TbButton
              icon="⋯"
              label="Options"
              testId="movie-detail-options"
              divider
              disabled
              title="More actions land in a later phase"
            />
          </>
        }
      />

      {status && (
        <p className="dim small" data-testid="movie-detail-status" role="status">
          {status}
        </p>
      )}

      <div className="card" data-testid="movie-detail-header">
        <BadgeRow>
          <Badge tone={movie.Monitored ? 'ok' : 'default'} testId="badge-monitored">
            {movie.Monitored ? 'Monitored' : 'Unmonitored'}
          </Badge>
          {movie.QualityProfile && <Badge testId="badge-quality">{movie.QualityProfile}</Badge>}
          {movie.Year > 0 && <Badge testId="badge-year">{movie.Year}</Badge>}
        </BadgeRow>
        <div style={{ marginTop: '0.6rem' }}>
          <MonitoredToggle
            monitored={movie.Monitored}
            onToggle={async (next) => {
              await client.setMovieMonitored(movie.ID, next)
              setMovie({ ...movie, Monitored: next })
            }}
            label={`${movie.Title} monitoring`}
            testId={`movie-mon-${movieId}`}
          />
        </div>
        <p className="dim" style={{ margin: '0.6rem 0 0' }} data-testid="movie-detail-meta">
          Metadata is provided by TMDB.
        </p>
      </div>

      <div className="card" data-testid="movie-detail-release">
        <h3>Release</h3>
        {wanted ? (
          wanted.Status === 'satisfied' ? (
            <>
              <p data-testid="movie-detail-release-state">Imported</p>
              <p className="lt-name" data-testid="movie-detail-release-title">
                {wanted.ReleaseTitle || '—'}
              </p>
              {wanted.SatisfiedAt && (
                <p className="dim small">
                  Satisfied {new Date(wanted.SatisfiedAt).toLocaleString()}
                </p>
              )}
            </>
          ) : (
            <p data-testid="movie-detail-release-state">
              Awaiting release — the movie is monitored; a search is looking for a
              matching file.
            </p>
          )
        ) : (
          <p className="dim" data-testid="movie-detail-release-state">
            No release request yet.
          </p>
        )}
      </div>
    </div>
  )
}
