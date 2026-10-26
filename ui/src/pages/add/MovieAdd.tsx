import { useCallback, useEffect, useRef, useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { client } from '../../api/client'
import type { AddMovieRequest, MovieSearchHit, RootFolder } from '../../api/types'
import { Badge, BadgeRow } from '../../components/ui'
import {
  AddError,
  ProfilePicker,
  SearchBox,
  useAddSubmit,
  useDebouncedSearch,
} from '../../components/addflow'

type MonitorMode = 'all' | 'movie_only' | 'unmonitored' | 'all_except_3d'
type MinimumAvailability = 'announced' | 'posters' | 'in_production' | 'released'

// MovieAddPage is the Radarr-parity Add Movie page (R2): two panes, not a
// modal. Left = debounced TMDB search with selectable result rows (poster,
// 'Title (year)', the badge row TMDB carries, overview, provider links).
// Right = the add form: root folder, monitor (edition-aware), minimum
// availability, quality profile, tags, start-search, and the green 'Add
// movie' button. Manual title/year entry stays available so the keyless
// (no TMDB) flow — search box greyed out — still works; the e2e relies on
// it. A selected hit pre-fills title/year and links 'More Info' to TMDB.
export function MovieAddPage() {
  const navigate = useNavigate()
  const search = useCallback((q: string) => client.searchMovies(q), [])
  const { text, setText, state } = useDebouncedSearch(search)

  const [selected, setSelected] = useState<MovieSearchHit | null>(null)
  const [title, setTitle] = useState('')
  const [year, setYear] = useState('')
  const [profile, setProfile] = useState('')
  const [monitor, setMonitor] = useState<MonitorMode>('all')
  const [minAvailability, setMinAvailability] = useState<MinimumAvailability>('released')
  const [tags, setTags] = useState<string[]>([])
  const [tagDraft, setTagDraft] = useState('')
  const [startSearch, setStartSearch] = useState(false)

  // Root folders load once on mount; the dropdown's value is the folder
  // PATH (AddMovieRequest.root_folder is a path; '' = server default).
  const [rootFolders, setRootFolders] = useState<RootFolder[] | null>(null)
  const [rootFolderPath, setRootFolderPath] = useState('')
  const [rootLoadError, setRootLoadError] = useState<string | null>(null)
  const loadRootFolders = useCallback(() => {
    client
      .listRootFolders()
      .then((rfs) => setRootFolders(rfs))
      .catch((e: unknown) => setRootLoadError(e instanceof Error ? e.message : 'failed to load root folders'))
  }, [])
  useEffect(() => {
    loadRootFolders()
  }, [loadRootFolders])

  // Radarr R3: /movies/add?tmdb=<id> (from the Discover page's 'Add
  // Movie') types the tmdb:<id> query into the search form and
  // pre-selects the first hit. Keyless: the search 503s, the preselect
  // no-ops, and manual title entry still works.
  const [searchParams] = useSearchParams()
  const tmdbParam = searchParams.get('tmdb')
  const preselectDone = useRef(false)

  useEffect(() => {
    if (!tmdbParam || !/^\d+$/.test(tmdbParam) || preselectDone.current) return
    setText(`tmdb:${tmdbParam}`)
  }, [tmdbParam, setText])

  useEffect(() => {
    if (!tmdbParam || preselectDone.current) return
    if (state.phase !== 'results' || state.hits.length === 0) return
    if (text !== `tmdb:${tmdbParam}`) return // user searched something else
    preselectDone.current = true
    pick(state.hits[0])
  }, [state, text, tmdbParam])

  const { busy, error, submit } = useAddSubmit(async () => {
    const req: AddMovieRequest = {
      title: title.trim(),
      // Radarr R5a: the generated type makes quality_profile required; the
      // factory default is sent until R5b wires up a profile picker.
      quality_profile: '4K Best Effort',
      monitor,
      minimum_availability: minAvailability,
      start_search: startSearch,
    }
    if (year.trim()) req.year = year.trim()
    if (profile) req.quality_profile = profile
    if (tags.length > 0) req.tags = tags
    if (rootFolderPath) req.root_folder = rootFolderPath
    await client.addMovie(req)
    navigate('/movies')
  })

  function pick(hit: MovieSearchHit) {
    setSelected(hit)
    setTitle(hit.title)
    setYear(hit.year ? String(hit.year) : '')
  }

  function addTag() {
    const t = tagDraft.trim().replace(/,+$/, '')
    if (!t) return
    if (!tags.includes(t)) setTags((prev) => [...prev, t])
    setTagDraft('')
  }

  const trimmedTitle = title.trim()
  const selectedFolder = (rootFolders ?? []).find((rf) => rf.Path === rootFolderPath)
  const subfolder = trimmedTitle
    ? `${trimmedTitle}${year.trim() ? ` (${year.trim()})` : ''}`
    : null

  return (
    <div className="add-page">
      <div className="dash-head">
        <h2 data-testid="movie-add-title">Add movie</h2>
        <Link className="ghost" to="/movies" data-testid="movie-add-back">
          ← All movies
        </Link>
      </div>

      <div className="two-pane">
        {/* LEFT: search + selectable results */}
        <div>
          <SearchBox
            label="Search movies (TMDB)"
            placeholder="Title or tmdb:&lt;id&gt;, e.g. Dune"
            text={text}
            onText={setText}
            state={state}
            testId="movie-search"
          />
          {state.phase === 'results' && (
            <ul className="result-list" data-testid="movie-results">
              {state.hits.length === 0 && (
                <li>
                  <p className="dim" data-testid="movie-results-empty">
                    No results — try a different term.
                  </p>
                </li>
              )}
              {state.hits.map((h) => (
                <li key={h.id}>
                  <div
                    className={`tv-result-row ${selected?.id === h.id ? 'sel' : ''}`}
                    role="button"
                    tabIndex={0}
                    onClick={() => pick(h)}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter' || e.key === ' ') {
                        e.preventDefault()
                        pick(h)
                      }
                    }}
                    aria-pressed={selected?.id === h.id}
                    data-testid={`movie-result-${h.id}`}
                  >
                    <span className="tv-result-poster" aria-hidden>
                      {h.poster ? (
                        <img src={h.poster} alt="" className="movie-result-poster-img" />
                      ) : (
                        posterInitials(h.title)
                      )}
                    </span>
                    <span className="tv-result-main">
                      <span className="result-title">
                        {h.title}
                        {h.year > 0 ? <span className="dim"> ({h.year})</span> : null}
                      </span>
                      <BadgeRow>
                        {h.certification && <Badge tone="teal">{h.certification}</Badge>}
                        {h.runtime > 0 && <Badge>{h.runtime} min</Badge>}
                        {h.tmdb_pct != null && <Badge tone="ok">★ {h.tmdb_pct}%</Badge>}
                        {h.imdb && (
                          <a
                            href={h.imdb}
                            target="_blank"
                            rel="noreferrer"
                            onClick={(e) => e.stopPropagation()}
                            data-testid={`movie-result-imdb-${h.id}`}
                          >
                            IMDb
                          </a>
                        )}
                        {h.original_language && (
                          <Badge>{h.original_language.toUpperCase()}</Badge>
                        )}
                        {h.studio && <Badge>{h.studio}</Badge>}
                        {h.genres.map((g) => (
                          <Badge key={g}>{g}</Badge>
                        ))}
                        <a
                          href={h.links.tmdb}
                          target="_blank"
                          rel="noreferrer"
                          onClick={(e) => e.stopPropagation()}
                          data-testid={`movie-result-tmdb-${h.id}`}
                        >
                          TMDB
                        </a>
                      </BadgeRow>
                      {h.overview && (
                        <span className="tv-result-overview dim small">{h.overview}</span>
                      )}
                    </span>
                  </div>
                </li>
              ))}
            </ul>
          )}
        </div>

        {/* RIGHT: the add form */}
        <div className="card add-form" data-testid="movie-add-form">
          {selected ? (
            <>
              <p className="dim small" data-testid="movie-selected">
                Selected: <strong>{selected.title}</strong>
                {selected.year ? ` (${selected.year})` : ''}
              </p>
              <a
                href={selected.links.tmdb}
                target="_blank"
                rel="noreferrer"
                className="ghost"
                data-testid="movie-more-info"
              >
                More Info
              </a>
            </>
          ) : subfolder ? (
            <p className="dim small" data-testid="movie-selected">
              Adding <strong>{trimmedTitle}</strong>
              {year.trim() ? ` (${year.trim()})` : ''}
            </p>
          ) : (
            <p className="dim small" data-testid="movie-add-empty">
              Search and select a movie to add — or type a title below.
            </p>
          )}

          <label htmlFor="movie-title">Title</label>
          <input
            id="movie-title"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            placeholder="Movie title"
            data-testid="movie-title-input"
          />
          <label htmlFor="movie-year">Year (optional)</label>
          <input
            id="movie-year"
            value={year}
            onChange={(e) => setYear(e.target.value)}
            placeholder="e.g. 2024"
            inputMode="numeric"
            data-testid="movie-year-input"
          />

          <label htmlFor="movie-root-folder">Root folder</label>
          <select
            id="movie-root-folder"
            value={rootFolderPath}
            onChange={(e) => setRootFolderPath(e.target.value)}
            data-testid="movie-root-folder"
          >
            <option value="">— Server default —</option>
            {(rootFolders ?? []).map((rf) => (
              <option key={rf.Path} value={rf.Path}>
                {rf.Name ? `${rf.Name} (${rf.Path})` : rf.Path}
                {rf.FreeSpace != null ? ` · ${formatFreeSpace(rf.FreeSpace)} free` : ''}
              </option>
            ))}
          </select>
          {rootLoadError && (
            <p className="error" data-testid="movie-root-folder-error">
              {rootLoadError}
            </p>
          )}
          {(rootFolders ?? []).length === 0 && rootFolders !== null && (
            <p className="dim small" data-testid="movie-root-folder-empty">
              No root folders registered yet.
            </p>
          )}
          <p className="dim small" data-testid="movie-root-folder-help">
            {subfolder
              ? `'${subfolder}' subfolder will be created automatically.`
              : "The movie's subfolder will be created automatically."}
            {selectedFolder?.FreeSpace != null && (
              <span data-testid="movie-root-folder-freespace">
                {' '}
                {formatFreeSpace(selectedFolder.FreeSpace)} free.
              </span>
            )}
          </p>

          <label htmlFor="movie-monitor">Monitor</label>
          <select
            id="movie-monitor"
            value={monitor}
            onChange={(e) => setMonitor(e.target.value as MonitorMode)}
            data-testid="movie-monitor"
          >
            <option value="all">All</option>
            <option value="movie_only">Movie Only</option>
            <option value="unmonitored">Unmonitored</option>
            <option value="all_except_3d">All except 3D</option>
          </select>

          <label htmlFor="movie-min-availability">Minimum availability</label>
          <select
            id="movie-min-availability"
            value={minAvailability}
            onChange={(e) => setMinAvailability(e.target.value as MinimumAvailability)}
            data-testid="movie-min-availability"
          >
            <option value="announced">Announced</option>
            <option value="posters">Posters</option>
            <option value="in_production">In Production</option>
            <option value="released">Released</option>
          </select>

          <ProfilePicker kind="movies" value={profile} onChange={setProfile} testId="movie-profile" />

          <label htmlFor="movie-tags">Tags</label>
          <input
            id="movie-tags"
            value={tagDraft}
            onChange={(e) => setTagDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' || e.key === ',') {
                e.preventDefault()
                addTag()
              }
            }}
            onBlur={addTag}
            placeholder="Add a tag, press Enter"
            data-testid="movie-tags-input"
          />
          {tags.length > 0 && (
            <span className="tag-chips" data-testid="movie-tags-chips">
              {tags.map((t) => (
                <span className="tag-chip" key={t}>
                  {t}
                  <button
                    type="button"
                    aria-label={`Remove tag ${t}`}
                    onClick={() => setTags((prev) => prev.filter((x) => x !== t))}
                    data-testid={`movie-tag-remove-${t}`}
                  >
                    ✕
                  </button>
                </span>
              ))}
            </span>
          )}

          <label className="check" htmlFor="movie-start-search">
            <input
              id="movie-start-search"
              type="checkbox"
              checked={startSearch}
              onChange={(e) => setStartSearch(e.target.checked)}
              data-testid="movie-start-search"
            />
            Start search for missing movie
          </label>

          <AddError message={error} testId="movie-add-error" />
          <button
            type="button"
            className="add-btn-green"
            disabled={busy || !trimmedTitle}
            onClick={() => {
              void submit().catch(() => {
                /* error surfaced in AddError */
              })
            }}
            data-testid="movie-add-button"
          >
            {busy ? 'Adding…' : `Add ${trimmedTitle || 'movie'}`}
          </button>
        </div>
      </div>
    </div>
  )
}

function formatFreeSpace(bytes: number): string {
  const gb = bytes / 1024 ** 3
  if (gb >= 1024) return `${(gb / 1024).toFixed(1)} TB`
  return `${gb.toFixed(1)} GB`
}

function posterInitials(title: string): string {
  const words = title.trim().split(/\s+/).filter((w) => !/^(the|a|an)$/i.test(w))
  if (words.length === 0) return '🎬'
  return words
    .slice(0, 2)
    .map((w) => w[0]!.toUpperCase())
    .join('')
}
