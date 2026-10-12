import { useCallback, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { client } from '../../api/client'
import type { SearchResult } from '../../api/types'
import {
  AddError,
  MonitoredField,
  ResultList,
  SearchBox,
  useAddSubmit,
  useDebouncedSearch,
} from '../../components/addflow'

// ArtistAddPage is the music add flow (#42): search MusicBrainz → pick an
// artist → add. The artist's MBID travels with the add so later album imports
// can join on the provider id. Artists are flat (albums are added later from
// the list view), so there is no quality-profile picker here — profiles apply
// at the album level.
export function ArtistAddPage() {
  const navigate = useNavigate()
  const search = useCallback((q: string) => client.searchArtists(q), [])
  const { text, setText, state } = useDebouncedSearch(search)

  const [selected, setSelected] = useState<SearchResult | null>(null)
  const [name, setName] = useState('')
  const [monitored, setMonitored] = useState(true)

  const { busy, error, submit } = useAddSubmit(() => {
    return client
      .addArtist({ name: name.trim(), mbid: selected?.id ?? '', monitored })
      .then(() => navigate('/music'))
  })

  function pick(hit: SearchResult) {
    setSelected(hit)
    setName(hit.title)
  }

  return (
    <div className="add-page">
      <div className="dash-head">
        <h2 data-testid="artist-add-title">Add artist</h2>
        <Link className="ghost" to="/music" data-testid="artist-add-back">
          ← All artists
        </Link>
      </div>

      <SearchBox
        label="Search artists (MusicBrainz)"
        placeholder="Artist, e.g. Radiohead"
        text={text}
        onText={setText}
        state={state}
        testId="artist-search"
      />

      {state.phase === 'results' && (
        <ResultList hits={state.hits} onPick={pick} testId="artist-results" />
      )}

      <div className="card add-form" data-testid="artist-add-form">
        {selected && (
          <p className="dim small" data-testid="artist-selected">
            Selected: <strong>{selected.title}</strong>
            {selected.extra.country ? ` · ${selected.extra.country}` : ''}
          </p>
        )}
        <label htmlFor="artist-name">Artist name</label>
        <input
          id="artist-name"
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="Artist name"
          data-testid="artist-name-input"
        />
        <MonitoredField
          label="Monitor for new releases"
          checked={monitored}
          onChecked={setMonitored}
          testId="artist-monitored"
        />
        <AddError message={error} testId="artist-add-error" />
        <button
          type="button"
          disabled={busy || !name.trim()}
          onClick={() => {
            void submit().catch(() => {
              /* error surfaced in AddError */
            })
          }}
          data-testid="artist-add-button"
        >
          {busy ? 'Adding…' : 'Add artist'}
        </button>
      </div>
    </div>
  )
}
