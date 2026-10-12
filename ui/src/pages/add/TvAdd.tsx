import { useCallback, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { client } from '../../api/client'
import type { SearchResult } from '../../api/types'
import {
  AddError,
  MonitoredField,
  ProfilePicker,
  ResultList,
  SearchBox,
  useAddSubmit,
  useDebouncedSearch,
} from '../../components/addflow'

type Source = 'tvdb' | 'tmdb'

// TvAddPage is the TV add flow (#42): search TVDB (or TMDB) → pick a hit →
// quality profile → add. The selected source determines which provider id is
// sent with the add: a TVDB hit carries tvdb_id, a TMDB hit carries tmdb_id
// (the server stores 0 for the unknown one, matching each provider's own
// "unknown" convention).
export function TvAddPage() {
  const navigate = useNavigate()
  const [source, setSource] = useState<Source>('tvdb')
  const search = useCallback(
    (q: string) => (source === 'tvdb' ? client.searchTv(q) : client.searchTvTmdb(q)),
    [source],
  )
  const { text, setText, state } = useDebouncedSearch(search)

  const [selected, setSelected] = useState<SearchResult | null>(null)
  const [title, setTitle] = useState('')
  const [year, setYear] = useState('')
  const [profile, setProfile] = useState('')
  const [monitored, setMonitored] = useState(true)

  const { busy, error, submit } = useAddSubmit(() => {
    const req: {
      title: string
      year?: string
      quality_profile?: string
      monitored?: boolean
      tvdb_id?: string
      tmdb_id?: string
    } = { title: title.trim(), monitored }
    if (year.trim()) req.year = year.trim()
    if (profile) req.quality_profile = profile
    if (selected) {
      // Send the id from the source the hit came from (tracked on pick).
      if (source === 'tvdb') req.tvdb_id = selected.id
      else req.tmdb_id = selected.id
    }
    return client.addSeries(req).then(() => navigate('/tv'))
  })

  function pick(hit: SearchResult) {
    setSelected(hit)
    setTitle(hit.title)
    setYear(hit.year ? String(hit.year) : '')
  }

  // Switching the source invalidates any prior selection (the id belongs to
  // the other provider).
  function changeSource(next: Source) {
    setSource(next)
    setSelected(null)
  }

  return (
    <div className="add-page">
      <div className="dash-head">
        <h2 data-testid="tv-add-title">Add series</h2>
        <Link className="ghost" to="/tv" data-testid="tv-add-back">
          ← All series
        </Link>
      </div>

      <div className="segmented" role="group" aria-label="Search source">
        <button
          type="button"
          className={source === 'tvdb' ? 'seg active' : 'seg'}
          onClick={() => changeSource('tvdb')}
          data-testid="tv-source-tvdb"
        >
          TVDB
        </button>
        <button
          type="button"
          className={source === 'tmdb' ? 'seg active' : 'seg'}
          onClick={() => changeSource('tmdb')}
          data-testid="tv-source-tmdb"
        >
          TMDB
        </button>
      </div>

      <SearchBox
        label={`Search series (${source === 'tvdb' ? 'TVDB' : 'TMDB'})`}
        placeholder="Series title, e.g. Breaking Bad"
        text={text}
        onText={setText}
        state={state}
        testId="tv-search"
      />

      {state.phase === 'results' && (
        <ResultList hits={state.hits} onPick={pick} testId="tv-results" />
      )}

      <div className="card add-form" data-testid="tv-add-form">
        {selected && (
          <p className="dim small" data-testid="tv-selected">
            Selected: <strong>{selected.title}</strong>
            {selected.year ? ` (${selected.year})` : ''} · via {source.toUpperCase()}
          </p>
        )}
        <label htmlFor="tv-title">Title</label>
        <input
          id="tv-title"
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          placeholder="Series title"
          data-testid="tv-title-input"
        />
        <label htmlFor="tv-year">Year (optional)</label>
        <input
          id="tv-year"
          value={year}
          onChange={(e) => setYear(e.target.value)}
          placeholder="e.g. 2008"
          inputMode="numeric"
          data-testid="tv-year-input"
        />
        <ProfilePicker kind="tv" value={profile} onChange={setProfile} testId="tv-profile" />
        <MonitoredField
          label="Monitor for new episodes"
          checked={monitored}
          onChecked={setMonitored}
          testId="tv-monitored"
        />
        <AddError message={error} testId="tv-add-error" />
        <button
          type="button"
          disabled={busy || !title.trim()}
          onClick={() => {
            void submit().catch(() => {
              /* error surfaced in AddError */
            })
          }}
          data-testid="tv-add-button"
        >
          {busy ? 'Adding…' : 'Add series'}
        </button>
      </div>
    </div>
  )
}
