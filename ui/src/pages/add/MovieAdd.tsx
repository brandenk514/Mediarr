import { useCallback, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { client } from '../../api/client'
import type { SearchResult } from '../../api/types'
import {
  AddError,
  ProfilePicker,
  ResultList,
  SearchBox,
  useAddSubmit,
  useDebouncedSearch,
} from '../../components/addflow'

// MovieAddPage is the movie add flow (#42): search TMDB → pick a hit →
// quality profile → add. A selected hit pre-fills title/year and stays
// editable (the API is idempotent, so re-adding an existing title+year is a
// no-op that returns the existing id). New movies are monitored by default —
// the movies endpoint has no monitored field (PLAN §4 movie-level monitoring
// is implicit at add time).
export function MovieAddPage() {
  const navigate = useNavigate()
  const search = useCallback((q: string) => client.searchMovies(q), [])
  const { text, setText, state } = useDebouncedSearch(search)

  const [selected, setSelected] = useState<SearchResult | null>(null)
  const [title, setTitle] = useState('')
  const [year, setYear] = useState('')
  const [profile, setProfile] = useState('')

  const { busy, error, submit } = useAddSubmit(() => {
    const req: { title: string; year?: string; quality_profile?: string } = { title: title.trim() }
    if (year.trim()) req.year = year.trim()
    if (profile) req.quality_profile = profile
    return client.addMovie(req).then(() => navigate('/movies'))
  })

  function pick(hit: SearchResult) {
    setSelected(hit)
    setTitle(hit.title)
    setYear(hit.year ? String(hit.year) : '')
  }

  return (
    <div className="add-page">
      <div className="dash-head">
        <h2 data-testid="movie-add-title">Add movie</h2>
        <Link className="ghost" to="/movies" data-testid="movie-add-back">
          ← All movies
        </Link>
      </div>

      <SearchBox
        label="Search movies (TMDB)"
        placeholder="Title, e.g. Dune"
        text={text}
        onText={setText}
        state={state}
        testId="movie-search"
      />

      {state.phase === 'results' && (
        <ResultList hits={state.hits} onPick={pick} testId="movie-results" />
      )}

      <div className="card add-form" data-testid="movie-add-form">
        {selected && (
          <p className="dim small" data-testid="movie-selected">
            Selected: <strong>{selected.title}</strong>
            {selected.year ? ` (${selected.year})` : ''}
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
        <ProfilePicker kind="movies" value={profile} onChange={setProfile} testId="movie-profile" />
        <AddError message={error} testId="movie-add-error" />
        <button
          type="button"
          disabled={busy || !title.trim()}
          onClick={() => {
            void submit().catch(() => {
              /* error surfaced in AddError */
            })
          }}
          data-testid="movie-add-button"
        >
          {busy ? 'Adding…' : 'Add movie'}
        </button>
      </div>
    </div>
  )
}
