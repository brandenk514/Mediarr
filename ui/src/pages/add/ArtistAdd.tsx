import { useCallback, useEffect, useRef, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { ApiError, client } from '../../api/client'
import type {
  AddArtistRequest,
  ArtistSearchHit,
  ArtistSearchResult,
  ProfilesResponse,
  ReleaseGroupHit,
  RootFolder,
} from '../../api/types'
import { Badge, BadgeRow, Modal } from '../../components/ui'
import { MONITOR_POLICY_OPTIONS } from '../../components/music'
import { AddError, MIN_SEARCH_CHARS, SEARCH_DEBOUNCE_MS, SearchBox, useAddSubmit } from '../../components/addflow'

// Lidarr L2 Add Artist flow: a Lidarr-3.x-style search page (artist rows +
// release-group rows stacked, debounced MusicBrainz) and an "Add New Artist"
// modal (Root Folder · Monitor · Monitor New Albums · Quality Profile ·
// Tags · start-search). No cover-art pipeline in this phase — artwork is a
// placeholder initials tile.
type Phase = 'idle' | 'loading' | 'results' | 'error' | 'unavailable'

interface ArtistSearchState {
  phase: Phase
  artists: ArtistSearchHit[]
  releaseGroups: ReleaseGroupHit[]
  error: string
}

// useArtistSearch debounces the add-page search (same constants as the
// shared addflow hook) and shapes the L2 response — {artists,
// release_groups} — into the two row kinds. A 503 means MusicBrainz is
// unavailable (keyless but rate-limited) and greys the box, like the other
// add flows.
function useArtistSearch(search: (q: string) => Promise<ArtistSearchResult>) {
  const [text, setText] = useState('')
  const [state, setState] = useState<ArtistSearchState>({
    phase: 'idle',
    artists: [],
    releaseGroups: [],
    error: '',
  })
  const reqId = useRef(0)

  useEffect(() => {
    const term = text.trim()
    if (term.length < MIN_SEARCH_CHARS) {
      reqId.current += 1 // invalidate in-flight
      setState((s) =>
        s.phase === 'unavailable' ? s : { phase: 'idle', artists: [], releaseGroups: [], error: '' },
      )
      return
    }
    const timer = setTimeout(async () => {
      const id = ++reqId.current
      setState((s) => ({ ...s, phase: 'loading', error: '' }))
      try {
        const res = await search(term)
        if (id !== reqId.current) return
        setState({
          phase: 'results',
          artists: res.artists ?? [],
          releaseGroups: res.release_groups ?? [],
          error: '',
        })
      } catch (e: unknown) {
        if (id !== reqId.current) return
        if (e instanceof ApiError && e.status === 503) {
          setState({ phase: 'unavailable', artists: [], releaseGroups: [], error: e.message })
        } else {
          setState({
            phase: 'error',
            artists: [],
            releaseGroups: [],
            error: e instanceof Error ? e.message : 'search failed',
          })
        }
      }
    }, SEARCH_DEBOUNCE_MS)
    return () => clearTimeout(timer)
  }, [text, search])

  return { text, setText, state }
}

// The 7 Lidarr Monitor options (which albums are monitored when the artist
// is added) live in components/music (MONITOR_POLICY_OPTIONS) so the add
// modal and the detail Edit modal render the same list, Lidarr's
// "Lastest" spelling included.
type MonitorPolicy = 'all' | 'future' | 'missing' | 'existing' | 'first' | 'last' | 'none'

export function ArtistAddPage() {
  const navigate = useNavigate()
  const search = useCallback((q: string) => client.searchArtists(q), [])
  const { text, setText, state } = useArtistSearch(search)

  // The L2 add modal: a hit plus the form fields it pre-fills.
  const [selected, setSelected] = useState<ArtistSearchHit | null>(null)

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
        placeholder="eg. Breaking Benjamin, lidarr:&lt;musicbrainz-id&gt;"
        text={text}
        onText={setText}
        state={{ phase: state.phase, hits: state.artists, error: state.error }}
        testId="artist-search"
      />
      <p className="dim small" data-testid="artist-search-help">
        Paste the <code>lidarr:</code> prefix and a MusicBrainz id (e.g. lidarr:52615704-c35c-475d-bcd0-3b4ba1a4e151)
        to look the id up directly.
      </p>

      {state.phase === 'results' && (
        <ul className="result-list" data-testid="artist-results">
          {state.artists.length === 0 && state.releaseGroups.length === 0 && (
            <li>
              <p className="dim" data-testid="artist-results-empty">
                No results — try a different term.
              </p>
            </li>
          )}
          {state.artists.map((h) => (
            <ArtistResultRow key={`a-${h.id}`} hit={h} onPick={setSelected} />
          ))}
          {state.releaseGroups.map((g) => (
            <ReleaseGroupRow key={`g-${g.id}`} group={g} />
          ))}
        </ul>
      )}

      {selected && (
        <AddArtistModal
          hit={selected}
          onClose={() => setSelected(null)}
          onAdded={(artistId) => navigate(`/music/${artistId}`)}
        />
      )}
    </div>
  )
}

// ArtistResultRow is one artist hit. An already-added artist renders as a
// jump link to its music detail page (Lidarr behavior) instead of opening
// the add modal.
function ArtistResultRow({ hit, onPick }: { hit: ArtistSearchHit; onPick: (h: ArtistSearchHit) => void }) {
  const overview = [hit.disambiguation, ...hit.genres].filter(Boolean).join(' · ')

  if (hit.already_added) {
    return (
      <li>
        <Link
          className="tv-result-row"
          to={hit.library_id ? `/music/${hit.library_id}` : '/music'}
          data-testid={`artist-result-${hit.id}`}
        >
          <span className="tv-result-poster" aria-hidden>
            {artistInitials(hit.title)}
          </span>
          <span className="tv-result-main">
            <span className="result-title">{hit.title}</span>
            <BadgeRow>
              {hit.type && <Badge tone="teal">{hit.type}</Badge>}
              <Badge tone="ok">In library</Badge>
            </BadgeRow>
            {overview && <span className="tv-result-overview dim small">{overview}</span>}
          </span>
        </Link>
      </li>
    )
  }

  return (
    <li>
      <div
        className="tv-result-row"
        role="button"
        tabIndex={0}
        onClick={() => onPick(hit)}
        onKeyDown={(e) => {
          if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault()
            onPick(hit)
          }
        }}
        data-testid={`artist-result-${hit.id}`}
      >
        <span className="tv-result-poster" aria-hidden>
          {artistInitials(hit.title)}
        </span>
        <span className="tv-result-main">
          <span className="result-title">{hit.title}</span>
          <BadgeRow>
            {hit.score > 0 && <Badge tone="ok">♥ {hit.score}%</Badge>}
            {hit.type && <Badge tone="teal">{hit.type}</Badge>}
            {hit.country && <Badge>{hit.country}</Badge>}
          </BadgeRow>
          {overview && <span className="tv-result-overview dim small">{overview}</span>}
        </span>
      </div>
    </li>
  )
}

// ReleaseGroupRow is a MusicBrainz release-group hit (display-only in L2 —
// adding a release group lands with the music import flow): title, year when
// known, "N releases", the normalized type chip, and a small marker.
function ReleaseGroupRow({ group }: { group: ReleaseGroupHit }) {
  return (
    <li>
      <div className="tv-result-row release-group-row" data-testid={`release-group-${group.id}`}>
        <span className="tv-result-poster" aria-hidden>
          🎵
        </span>
        <span className="tv-result-main">
          <span className="result-title">
            {group.title}
            {group.year > 0 ? <span className="dim"> ({group.year})</span> : null}
          </span>
          <BadgeRow>
            <Badge>{group.type}</Badge>
            {group.release_count > 0 && <Badge>{group.release_count} releases</Badge>}
            {group.artist && <Badge tone="teal">{group.artist}</Badge>}
            <span className="dim small" data-testid={`release-group-marker-${group.id}`}>
              release group
            </span>
          </BadgeRow>
        </span>
      </div>
    </li>
  )
}

// AddArtistModal is the Lidarr "Add New Artist" modal. Field order is the
// Lidarr 3.x order: Root Folder → Monitor → Monitor New Albums → Quality
// Profile → Tags, with the footer checkbox + green Add button. The root
// folder select is display-only (the API picks the server root folder); the
// '<Artist>' subfolder is created automatically.
function AddArtistModal({
  hit,
  onClose,
  onAdded,
}: {
  hit: ArtistSearchHit
  onClose: () => void
  onAdded: (artistId: number) => void
}) {
  const [rootFolders, setRootFolders] = useState<RootFolder[] | null>(null)
  const [rootFolderPath, setRootFolderPath] = useState('')
  const [monitorPolicy, setMonitorPolicy] = useState<MonitorPolicy>('all')
  const [monitorNewAlbums, setMonitorNewAlbums] = useState<'all_albums'>('all_albums')
  const [profile, setProfile] = useState('')
  const [profiles, setProfiles] = useState<ProfilesResponse | null>(null)
  const [tags, setTags] = useState<string[]>([])
  const [tagDraft, setTagDraft] = useState('')
  const [startSearch, setStartSearch] = useState(false)

  useEffect(() => {
    let cancelled = false
    client
      .listRootFolders()
      .then((rfs) => {
        if (cancelled) return
        setRootFolders(rfs)
        setRootFolderPath((p) => p || rfs[0]?.Path || '')
      })
      .catch(() => !cancelled && setRootFolders([]))
    return () => {
      cancelled = true
    }
  }, [])

  useEffect(() => {
    let cancelled = false
    client
      .getProfiles('music')
      .then((p) => {
        if (cancelled) return
        setProfiles(p)
        setProfile((v) => v || p.default || '')
      })
      .catch(() => {
        /* an empty profile list still sends '' (server default) */
      })
    return () => {
      cancelled = true
    }
  }, [])

  const { busy, error, submit } = useAddSubmit(async () => {
    const req: AddArtistRequest = {
      name: hit.title,
      mbid: hit.id,
      monitor_policy: monitorPolicy,
      monitor_new_albums: monitorNewAlbums,
    }
    if (profile) req.quality_profile = profile
    if (tags.length > 0) req.tags = tags
    const added = await client.addArtist(req)
    // The per-artist pipeline is the Lidarr "Start search for missing
    // albums" action; fire it after the add so the row exists first.
    if (startSearch) {
      void client.runMusicPipeline(added.id).catch(() => {
        /* best-effort: the artist is added even if the pipeline fails */
      })
    }
    onAdded(added.id)
  })

  function addTag() {
    const t = tagDraft.trim().replace(/,+$/, '')
    if (!t) return
    if (!tags.includes(t)) setTags((prev) => [...prev, t])
    setTagDraft('')
  }

  const selectedFolder = (rootFolders ?? []).find((rf) => rf.Path === rootFolderPath)

  return (
    <Modal
      title="Add New Artist"
      onClose={onClose}
      testId="add-artist-modal"
      footer={
        <>
          <label
            className="check"
            htmlFor="artist-start-search"
            style={{ marginRight: 'auto' }}
          >
            <input
              id="artist-start-search"
              type="checkbox"
              checked={startSearch}
              onChange={(e) => setStartSearch(e.target.checked)}
              data-testid="artist-start-search"
            />
            Start search for missing albums
          </label>
          <button
            type="button"
            className="add-btn-green"
            disabled={busy}
            onClick={() => {
              void submit().catch(() => {
                /* error surfaced in AddError */
              })
            }}
            data-testid="artist-add-button"
          >
            {busy ? 'Adding…' : `Add ${hit.title}`}
          </button>
        </>
      }
    >
      <div className="add-artist-modal">
        {/* LEFT: artwork placeholder (no cover-art pipeline in L2) */}
        <div className="add-artist-artwork" aria-hidden data-testid="add-artist-artwork">
          {artistInitials(hit.title)}
        </div>

        {/* RIGHT: name + the Lidarr field order */}
        <div className="add-artist-fields">
          <h3 data-testid="add-artist-name">{hit.title}</h3>

          <label htmlFor="artist-root-folder" className="dim small">
            Root Folder
          </label>
          <select
            id="artist-root-folder"
            value={rootFolderPath}
            onChange={(e) => setRootFolderPath(e.target.value)}
            data-testid="add-artist-root-folder"
          >
            {(rootFolders ?? []).map((rf) => (
              <option key={rf.Path} value={rf.Path}>
                {rf.Name ? `[${rf.Name}] ${rf.Path}` : rf.Path}
                {rf.FreeSpace != null ? ` · ${formatFreeSpace(rf.FreeSpace)} Free` : ''}
              </option>
            ))}
            {rootFolders !== null && rootFolders.length === 0 && <option value="">— No root folders —</option>}
          </select>
          <p className="dim small" data-testid="add-artist-root-folder-help">
            '{hit.title}' subfolder will be created automatically
            {selectedFolder?.FreeSpace != null ? ` · ${formatFreeSpace(selectedFolder.FreeSpace)} Free` : ''}
          </p>

          <label htmlFor="artist-monitor-policy" className="dim small">
            Monitor
          </label>
          <select
            id="artist-monitor-policy"
            value={monitorPolicy}
            onChange={(e) => setMonitorPolicy(e.target.value as MonitorPolicy)}
            data-testid="add-artist-monitor-policy"
          >
            {MONITOR_POLICY_OPTIONS.map((o) => (
              <option key={o.value} value={o.value}>
                {o.label}
              </option>
            ))}
          </select>
          <p className="dim small" data-testid="add-artist-monitor-policy-help">
            Which albums should be monitored after the artist is added (one-time adjustment)
          </p>

          <label htmlFor="artist-monitor-new-albums" className="dim small">
            Monitor New Albums
          </label>
          <select
            id="artist-monitor-new-albums"
            value={monitorNewAlbums}
            onChange={(e) => setMonitorNewAlbums(e.target.value as 'all_albums')}
            data-testid="add-artist-monitor-new-albums"
          >
            <option value="all_albums">All Albums</option>
          </select>
          <p className="dim small" data-testid="add-artist-monitor-new-albums-help">
            Which new albums should be monitored
          </p>

          <label htmlFor="artist-quality-profile" className="dim small">
            Quality Profile
          </label>
          <select
            id="artist-quality-profile"
            value={profile}
            onChange={(e) => setProfile(e.target.value)}
            data-testid="add-artist-quality-profile"
          >
            {(profiles?.profiles ?? []).map((p) => (
              <option key={p} value={p}>
                {p}
                {profiles?.default === p ? ' (default)' : ''}
              </option>
            ))}
            {profiles !== null && (profiles.profiles ?? []).length === 0 && (
              <option value="">— No profiles —</option>
            )}
          </select>

          <label htmlFor="artist-tags" className="dim small">
            Tags
          </label>
          <input
            id="artist-tags"
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
            data-testid="add-artist-tags-input"
          />
          {tags.length > 0 && (
            <span className="tag-chips" data-testid="add-artist-tags-chips">
              {tags.map((t) => (
                <span className="tag-chip" key={t}>
                  {t}
                  <button
                    type="button"
                    aria-label={`Remove tag ${t}`}
                    onClick={() => setTags((prev) => prev.filter((x) => x !== t))}
                    data-testid={`add-artist-tag-remove-${t}`}
                  >
                    ✕
                  </button>
                </span>
              ))}
            </span>
          )}

          <AddError message={error} testId="add-artist-error" />
        </div>
      </div>
    </Modal>
  )
}

function artistInitials(title: string): string {
  const words = title.trim().split(/\s+/).filter((w) => !/^(the|a|an)$/i.test(w))
  if (words.length === 0) return '♪'
  return words
    .slice(0, 2)
    .map((w) => w[0]!.toUpperCase())
    .join('')
}

function formatFreeSpace(bytes: number): string {
  const gb = bytes / 1024 ** 3
  if (gb >= 1024) return `${(gb / 1024).toFixed(1)} TiB`
  return `${gb.toFixed(1)} GiB`
}
