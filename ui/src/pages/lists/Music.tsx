import { useCallback, useEffect, useState } from 'react'
import { client } from '../../api/client'
import type { Album, Artist, ListQuery, PageArtist, Track } from '../../api/types'
import { AddButton, MonitoredField, ProfilePicker } from '../../components/addflow'
import {
  DEFAULT_PAGE_SIZE,
  MonitoredToggle,
  Pagination,
  usePagedList,
} from '../../components/list'
import { Menu, Toolbar, TbButton } from '../../components/ui'

// MusicPage is the P7 music library — Lidarr-shaped within the shared shell.
// P1 built the three-level lazy expansion (artist → album → track, each level
// fetched on expand); P7 layers the shared toolbar pattern (Update All /
// RSS Sync / Select / Options + Filter menu, same as TV and Movies) on top
// and adds the lossless quality dimension the plan asks for: each album row
// shows its lossless quality profile, and every artist has an "Add album"
// form prefilled with the music kind's built-in lossless profiles (FLAC /
// ALAC / …).
type MonitoredFilter = 'all' | 'monitored' | 'unmonitored'

const FILTER_OPTIONS: { value: MonitoredFilter; label: string }[] = [
  { value: 'all', label: 'All' },
  { value: 'monitored', label: 'Monitored' },
  { value: 'unmonitored', label: 'Unmonitored' },
]

export function MusicPage() {
  const load = useCallback((q: ListQuery) => client.listArtists(q), [])
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
    expanded,
    toggleExpanded,
  } = usePagedList<PageArtist>(load)

  const [status, setStatus] = useState<string | null>(null)

  function updateAll() {
    setStatus(`Updated ${total} artists just now`)
    reload()
  }

  return (
    <div className="list-page">
      <div className="dash-head">
        <h2 data-testid="music-title">Music</h2>
        <div className="dash-head-right">
          <AddButton to="/music/add" label="Add artist" testId="music-add" />
        </div>
      </div>

      <Toolbar
        left={
          <>
            <TbButton icon="⟳" label="Update All" testId="music-update-all" onClick={updateAll} />
            <TbButton
              icon="⇄"
              label="RSS Sync"
              testId="music-rss-sync"
              disabled
              title="RSS feeds land in a later phase"
            />
            <TbButton
              icon="☑"
              label="Select Artists"
              testId="music-select"
              divider
              disabled
              title="Bulk selection lands in a later phase"
            />
          </>
        }
        right={
          <>
            <TbButton
              icon="⋯"
              label="Options"
              testId="music-options"
              divider
              disabled
              title="More actions land in a later phase"
            />
            <Menu
              label="Filter"
              icon="⚲"
              options={[...FILTER_OPTIONS]}
              selected={monitored}
              onSelect={(v) => setMonitored(v as MonitoredFilter)}
              testId="music-filter"
            />
          </>
        }
      />

      {status && (
        <p className="dim small" data-testid="music-status" role="status">
          {status}
        </p>
      )}

      {error && !data ? (
        <div className="card" data-testid="music-error">
          <h3>Music unavailable</h3>
          <p className="error" data-testid="music-error-message">
            {error}
          </p>
          <button onClick={reload}>Retry</button>
        </div>
      ) : (
        <div className="list-list" data-testid="music-list">
          {loading && !data ? (
            <p className="dim">Loading artists…</p>
          ) : data && data.items.length === 0 ? (
            <p className="dim" data-testid="music-empty">
              No artists match.
            </p>
          ) : (
            data?.items.map((a) => (
              <div className="list-item" key={a.ID} data-testid={`artist-${a.ID}`}>
                <div className="item-head">
                  <button
                    type="button"
                    className={`row-head ${expanded.has(a.ID) ? 'open' : ''}`}
                    onClick={() => toggleExpanded(a.ID)}
                    aria-expanded={expanded.has(a.ID)}
                    data-testid={`artist-toggle-${a.ID}`}
                  >
                    <span className={`chevron ${expanded.has(a.ID) ? 'chevron-open' : ''}`}>
                      {expanded.has(a.ID) ? '▾' : '▸'}
                    </span>
                    <span className="lt-name" title={a.Name}>
                      {a.Name}
                    </span>
                  </button>
                  <MonitoredToggle
                    monitored={a.Monitored}
                    onToggle={async (next) => {
                      await client.setArtistMonitored(a.ID, next)
                      reload()
                    }}
                    label={`${a.Name} monitoring`}
                    testId={`artist-mon-${a.ID}`}
                  />
                </div>
                {expanded.has(a.ID) && (
                  <>
                    <AlbumList artistId={a.ID} />
                    <AddAlbumForm artist={a} onAdded={reload} />
                  </>
                )}
              </div>
            ))
          )}
          <Pagination page={page} pageSize={DEFAULT_PAGE_SIZE} total={total} onPage={setPage} />
        </div>
      )}
    </div>
  )
}

// AddAlbumForm is the per-artist "add an album" form (Lidarr's artist →
// album flow, P7). Lossless quality: the quality profile select is the music
// kind's built-in lossless profiles, prefilled with the kind default.
function AddAlbumForm({ artist, onAdded }: { artist: Artist; onAdded: () => void }) {
  const [name, setName] = useState('')
  const [year, setYear] = useState('')
  const [quality, setQuality] = useState('')
  const [monitored, setMonitored] = useState(true)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [done, setDone] = useState<string | null>(null)

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    if (busy || !name.trim()) return
    setBusy(true)
    setError(null)
    setDone(null)
    try {
      const req: Parameters<typeof client.addAlbum>[1] = {
        name: name.trim(),
        quality_profile: quality || undefined,
        monitored,
      }
      const y = Number(year)
      if (year && !Number.isNaN(y)) req.year = y
      await client.addAlbum(artist.ID, req)
      setName('')
      setYear('')
      setQuality('')
      setMonitored(true)
      setDone(`Added “${req.name}”.`)
      onAdded()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'failed to add album')
    } finally {
      setBusy(false)
    }
  }

  return (
    <form
      className="search-block"
      onSubmit={(e) => void submit(e)}
      data-testid={`artist-add-album-${artist.ID}`}
    >
      <div className="search-row">
        <input
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="Album name"
          aria-label="Album name"
          data-testid={`album-name-${artist.ID}`}
          required
        />
        <input
          value={year}
          onChange={(e) => setYear(e.target.value)}
          placeholder="Year"
          aria-label="Album year"
          inputMode="numeric"
          maxLength={4}
          data-testid={`album-year-${artist.ID}`}
        />
        <button
          type="submit"
          disabled={busy || !name.trim()}
          data-testid={`album-submit-${artist.ID}`}
        >
          Add album
        </button>
      </div>
      <div className="search-row">
        <ProfilePicker
          kind="music"
          value={quality}
          onChange={setQuality}
          testId={`add-album-quality-${artist.ID}`}
        />
        <MonitoredField
          checked={monitored}
          onChecked={setMonitored}
          label="Monitor album"
          testId={`album-monitored-${artist.ID}`}
        />
      </div>
      {error && (
        <p className="error" data-testid={`album-error-${artist.ID}`}>
          {error}
        </p>
      )}
      {done && (
        <p className="dim small" role="status" data-testid={`album-status-${artist.ID}`}>
          {done}
        </p>
      )}
    </form>
  )
}

// AlbumList fetches an artist's albums on expand; each album expands to tracks.
function AlbumList({ artistId }: { artistId: number }) {
  const [albums, setAlbums] = useState<Album[] | null>(null)
  const [err, setErr] = useState<string | null>(null)
  const [open, setOpen] = useState<Set<number>>(() => new Set())

  useEffect(() => {
    let cancelled = false
    client
      .listAlbums(artistId)
      .then((a) => !cancelled && setAlbums(a))
      .catch((e: unknown) => !cancelled && setErr(e instanceof Error ? e.message : 'failed to load albums'))
    return () => {
      cancelled = true
    }
  }, [artistId])

  function toggleAlbum(id: number) {
    setOpen((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  if (err) return <p className="error">Album error: {err}</p>
  if (!albums) return <p className="dim">Loading albums…</p>
  if (albums.length === 0) return <p className="dim">No albums yet.</p>
  return (
    <div className="child-list" data-testid={`albums-${artistId}`}>
      {albums.map((al) => (
        <div className="child-item" key={al.ID}>
          <div className="item-head">
            <button
              type="button"
              className={`row-head small ${open.has(al.ID) ? 'open' : ''}`}
              onClick={() => toggleAlbum(al.ID)}
              aria-expanded={open.has(al.ID)}
              data-testid={`album-toggle-${al.ID}`}
            >
              <span className={`chevron ${open.has(al.ID) ? 'chevron-open' : ''}`}>
                {open.has(al.ID) ? '▾' : '▸'}
              </span>
              <span className="lt-name">{al.Name}</span>
              {al.Year ? <span className="lt-dim">{al.Year}</span> : null}
              {al.QualityProfile ? (
                <span className="lt-dim" data-testid={`album-${al.ID}-quality`}>
                  {al.QualityProfile}
                </span>
              ) : null}
            </button>
            <MonitoredToggle
              monitored={al.Monitored}
              onToggle={async (next) => {
                await client.setAlbumMonitored(al.ID, next)
              }}
              label={`${al.Name} monitoring`}
              testId={`album-mon-${al.ID}`}
            />
          </div>
          {open.has(al.ID) && <TrackList albumId={al.ID} />}
        </div>
      ))}
    </div>
  )
}

// TrackList fetches an album's tracks on expand.
function TrackList({ albumId }: { albumId: number }) {
  const [tracks, setTracks] = useState<Track[] | null>(null)
  const [err, setErr] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    client
      .listTracks(albumId)
      .then((t) => !cancelled && setTracks(t))
      .catch((e: unknown) => !cancelled && setErr(e instanceof Error ? e.message : 'failed to load tracks'))
    return () => {
      cancelled = true
    }
  }, [albumId])

  if (err) return <p className="error">Track error: {err}</p>
  if (!tracks) return <p className="dim">Loading tracks…</p>
  if (tracks.length === 0) return <p className="dim">No tracks yet.</p>
  return (
    <div className="child-list nested" data-testid={`tracks-${albumId}`}>
      {tracks.map((tr) => (
        <div className="child-row" key={tr.ID}>
          <span className="child-label">
            {tr.Disc}.{tr.Number}
          </span>
          <span className="lt-name">{tr.Title || '—'}</span>
          <MonitoredToggle
            monitored={tr.Monitored}
            onToggle={async (next) => {
              await client.setTrackMonitored(albumId, tr.Disc, tr.Number, next)
            }}
            label={`track ${tr.Number} monitoring`}
            testId={`track-mon-${tr.ID}`}
          />
        </div>
      ))}
    </div>
  )
}
