import { useCallback, useEffect, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { ApiError, client } from '../../api/client'
import type { Movie, PageMovie } from '../../api/types'
import { MonitoredToggle } from '../../components/list'
import { posterText } from '../../components/poster'
import { Badge, BadgeRow, Toolbar, TbButton } from '../../components/ui'

// MovieDetailPage is the Radarr R1 movie detail — a single scrolling page:
// toolbar (Refresh & Scan / Search Movie / Interactive Search | Preview
// Rename / Manage Files / History | Edit / Delete), header card (poster +
// title + prev/next nav, badge row, score badges, two-row field grid,
// overview), the conditional Editions section (per-edition monitor toggle),
// the Files table, and Cast / Crew / Titles. Null fields render em-dashes;
// a keyless TMDB degrades to em-dashes, never an error. Buttons without a
// backend are disabled with a "later phase" title (the P7 pattern).
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

const DASH = '—'

// statusTone maps a Radarr status string to the badge/indicator tone.
function statusTone(status: string | undefined): string {
  switch (status) {
    case 'downloaded:monitored':
    case 'downloaded:unmonitored':
      return 'var(--ok)'
    case 'queued':
      return 'var(--warn)'
    case 'unreleased':
      return 'var(--dim)'
    case 'missing:monitored':
      return 'var(--err)'
    case 'missing:unmonitored':
      return 'var(--dim)'
    default:
      return 'var(--dim)'
  }
}

export function MovieDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const movieId = Number(id)
  const [movie, setMovie] = useState<Movie | null>(null)
  const [lib, setLib] = useState<Movie[] | null>(null)
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
      const m = await client.getMovie(movieId)
      setMovie(m)
      setError(null)
      setAuthLost(false)
      // Prev/next navigation (by library order). A failure here must not
      // break the page — the arrows simply don't render.
      client
        .listMovies({})
        .then((page: PageMovie) => setLib(page.items))
        .catch(() => setLib(null))
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

  // Prev/next in library (server) order.
  let prevId: number | null = null
  let nextId: number | null = null
  if (lib) {
    const idx = lib.findIndex((m) => m.ID === movie.ID)
    if (idx > 0) prevId = lib[idx - 1]!.ID
    if (idx >= 0 && idx < lib.length - 1) nextId = lib[idx + 1]!.ID
  }

  const scores = movie.Scores
  const editions = movie.Editions ?? []
  const files = movie.Files ?? []
  const cast = movie.Cast ?? []
  const crew = movie.Crew ?? []
  const altTitles = movie.AlternativeTitles ?? []

  return (
    <div className="list-page" data-testid="movie-detail">
      <div className="page-head">
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.8rem' }}>
          <Link to="/movies" className="ghost" data-testid="movie-detail-back">
            ← Movies
          </Link>
        </div>
      </div>

      <Toolbar
        left={
          <>
            <TbButton icon="⟳" label="Refresh & Scan" testId="movie-detail-refresh" onClick={() => void load()} />
            <TbButton
              icon="🔍"
              label="Search Movie"
              testId="movie-detail-search"
              disabled={searching}
              onClick={() => void searchReleases()}
            />
            <TbButton
              icon="🔎"
              label="Interactive Search"
              testId="movie-detail-interactive"
              disabled
              title="Interactive search lands in a later phase"
            />
            <TbButton
              icon="✎"
              label="Preview Rename"
              testId="movie-detail-rename"
              divider
              disabled
              title="Rename preview lands in a later phase"
            />
            <TbButton
              icon="🗀"
              label="Manage Files"
              testId="movie-detail-manage-files"
              disabled
              title="File management lands in a later phase"
            />
            <TbButton
              icon="🕘"
              label="History"
              testId="movie-detail-history"
              disabled
              title="Movie history lands in a later phase"
            />
            <TbButton
              icon="✏"
              label="Edit"
              testId="movie-detail-edit"
              divider
              disabled
              title="Movie editing lands in a later phase"
            />
            <TbButton
              icon="🗑"
              label="Delete"
              testId="movie-detail-delete"
              disabled
              title="Delete lands in a later phase"
            />
          </>
        }
      />

      {status && (
        <p className="dim small" data-testid="movie-detail-status" role="status">
          {status}
        </p>
      )}

      {/* Header card */}
      <div className="card" data-testid="movie-detail-header">
        <div className="detail-header">
          <div className="detail-poster" aria-hidden data-testid="movie-detail-poster">
            {posterText(movie.Title)}
          </div>
          <div style={{ flex: 1, minWidth: 0 }}>
            <div className="detail-title">
              <h2>
                {movie.Title}
                {movie.Year > 0 ? ` (${movie.Year})` : ''}
              </h2>
              {prevId !== null && (
                <button
                  type="button"
                  className="nav-arrow"
                  title="Previous movie"
                  data-testid="movie-detail-prev"
                  onClick={() => navigate(`/movies/${prevId}`)}
                >
                  ←
                </button>
              )}
              {nextId !== null && (
                <button
                  type="button"
                  className="nav-arrow"
                  title="Next movie"
                  data-testid="movie-detail-next"
                  onClick={() => navigate(`/movies/${nextId}`)}
                >
                  →
                </button>
              )}
            </div>

            <BadgeRow>
              <Badge tone={movie.Monitored ? 'ok' : 'default'} testId="badge-monitored">
                {movie.Monitored ? 'Monitored' : 'Unmonitored'}
              </Badge>
              {movie.Certification && <Badge testId="badge-certification">{movie.Certification}</Badge>}
              {movie.Year > 0 && <Badge testId="badge-year">{movie.Year}</Badge>}
              {movie.Runtime ? <Badge testId="badge-runtime">{movie.Runtime} min</Badge> : null}
            </BadgeRow>

            {(scores?.TmdbPct != null ||
              scores?.Imdb != null ||
              scores?.RottenTomatoes != null ||
              scores?.Metacritic != null) && (
              <div className="score-badges" data-testid="movie-detail-scores">
                {scores?.TmdbPct != null && <Badge tone="teal">TMDB {Math.round(scores.TmdbPct)}%</Badge>}
                {scores?.Imdb != null && <Badge tone="teal">IMDb {scores.Imdb.toFixed(1)}</Badge>}
                {scores?.RottenTomatoes != null && (
                  <Badge tone="teal">RT {Math.round(scores.RottenTomatoes)}%</Badge>
                )}
                {scores?.Metacritic != null && (
                  <Badge tone="teal">Metacritic {Math.round(scores.Metacritic)}</Badge>
                )}
              </div>
            )}

            <div className="field-grid" data-testid="movie-detail-fields">
              <div>
                <span className="fg-label">Path</span>
                <span className="fg-value" data-testid="field-path">
                  {movie.Path ?? DASH}
                </span>
              </div>
              <div>
                <span className="fg-label">Status</span>
                <span className="fg-value" data-testid="field-status">
                  <span
                    aria-hidden
                    style={{
                      display: 'inline-block',
                      width: '0.6rem',
                      height: '0.6rem',
                      borderRadius: '50%',
                      background: statusTone(movie.Status),
                      marginRight: '0.4rem',
                    }}
                  />
                  {movie.Status ?? DASH}
                </span>
              </div>
              <div>
                <span className="fg-label">Quality Profile</span>
                <span className="fg-value" data-testid="field-quality">
                  {movie.QualityProfile || DASH}
                </span>
              </div>
              <div>
                <span className="fg-label">Size</span>
                <span className="fg-value" data-testid="field-size">
                  {movie.Size ? humanBytes(movie.Size) : DASH}
                </span>
              </div>
              <div>
                <span className="fg-label">Collection</span>
                <span className="fg-value" data-testid="field-collection">
                  {movie.CollectionName ?? DASH}
                </span>
              </div>
              <div>
                <span className="fg-label">Original Language</span>
                <span className="fg-value" data-testid="field-language">
                  {movie.OriginalLanguage ?? DASH}
                </span>
              </div>
              <div>
                <span className="fg-label">Studio</span>
                <span className="fg-value" data-testid="field-studio">
                  {movie.Studio ?? DASH}
                </span>
              </div>
              <div>
                <span className="fg-label">Genres</span>
                <span className="fg-value" data-testid="field-genres">
                  {movie.Genres && movie.Genres.length > 0 ? movie.Genres.join(', ') : DASH}
                </span>
              </div>
            </div>

            <p style={{ marginTop: '0.8rem', marginBottom: 0 }} data-testid="movie-detail-overview">
              {movie.Overview ?? DASH}
            </p>

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
          </div>
        </div>
      </div>

      {/* Editions — conditional: only with more than one edition (Radarr v6). */}
      {editions.length > 1 && (
        <div className="card" data-testid="movie-detail-editions">
          <h3>Editions</h3>
          {editions.map((e) => (
            <div className="edition-row" key={e.ID}>
              <span className="ed-label">{e.Label}</span>
              <MonitoredToggle
                monitored={e.Monitored}
                onToggle={async (next) => {
                  await client.setMovieEditionMonitored(movie.ID, e.ID, next)
                  setMovie({
                    ...movie,
                    Editions: (movie.Editions ?? []).map((x) =>
                      x.ID === e.ID ? { ...x, Monitored: next } : x,
                    ),
                  })
                }}
                label={`${e.Label} edition monitoring`}
                testId={`edition-mon-${e.ID}`}
              />
              <span className="ed-meta">
                {e.Quality || DASH} · {e.Size ? humanBytes(e.Size) : DASH}
              </span>
            </div>
          ))}
        </div>
      )}

      {/* Files table */}
      <div className="card" data-testid="movie-detail-files">
        <h3>Files</h3>
        {files.length === 0 ? (
          <p className="dim" data-testid="movie-detail-files-empty">
            No files yet.
          </p>
        ) : (
          <table className="import-table" data-testid="movie-detail-files-table">
            <thead>
              <tr>
                <th>Relative Path</th>
                <th>Video Codec</th>
                <th>Audio Info</th>
                <th>Size</th>
                <th>Languages</th>
                <th>Quality</th>
                <th>Release Group</th>
                <th>Formats</th>
              </tr>
            </thead>
            <tbody>
              {files.map((f, i) => (
                <tr key={`${f.RelativePath}-${i}`} data-testid={`file-row-${i}`}>
                  <td>{f.RelativePath}</td>
                  <td>{f.VideoCodec || DASH}</td>
                  <td>{f.AudioInfo || DASH}</td>
                  <td>{humanBytes(f.Size)}</td>
                  <td>{f.Languages && f.Languages.length > 0 ? f.Languages.join(', ') : DASH}</td>
                  <td>{f.Quality || DASH}</td>
                  <td>{f.ReleaseGroup || DASH}</td>
                  <td>{f.Formats && f.Formats.length > 0 ? f.Formats.join(', ') : DASH}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      {/* Cast / Crew / Titles */}
      {cast.length > 0 && (
        <div className="card" data-testid="movie-detail-cast">
          <h3>Cast</h3>
          <div className="credit-list">
            {cast.map((c, i) => (
              <div key={`${c.Name}-${i}`}>
                <span className="credit-name">{c.Name}</span>{' '}
                <span className="credit-role">→ {c.Role || DASH}</span>
              </div>
            ))}
          </div>
        </div>
      )}

      {crew.length > 0 && (
        <div className="card" data-testid="movie-detail-crew">
          <h3>Crew</h3>
          <div className="credit-list">
            {crew.map((c, i) => (
              <div key={`${c.Name}-${i}`}>
                <span className="credit-name">{c.Name}</span>{' '}
                <span className="credit-role">→ {c.Job || DASH}</span>
              </div>
            ))}
          </div>
        </div>
      )}

      {altTitles.length > 0 && (
        <div className="card" data-testid="movie-detail-titles">
          <h3>Titles</h3>
          <div className="credit-list">
            {altTitles.map((t, i) => (
              <div key={`${t.Title}-${i}`}>
                <span className="credit-name">{t.Title}</span>{' '}
                <span className="credit-role">→ {t.Type || DASH}</span>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}
