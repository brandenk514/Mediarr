import { useCallback, useEffect, useState } from 'react'
import { client } from '../../api/client'
import type { Album, Artist, ListQuery, Track } from '../../api/types'
import {
  DEFAULT_PAGE_SIZE,
  ListControls,
  MonitoredToggle,
  Pagination,
  usePagedList,
} from '../../components/list'

// MusicPage is the per-kind list view for music (#41): a server-side filtered,
// paginated artist table. Each artist expands to its albums, and each album
// expands to its tracks — both fetched lazily on expand.
export function MusicPage() {
  const load = useCallback((q: ListQuery) => client.listArtists(q), [])
  const {
    data,
    loading,
    error,
    text,
    setText,
    monitored,
    setMonitored,
    page,
    setPage,
    total,
    reload,
    expanded,
    toggleExpanded,
  } = usePagedList<Artist>(load)

  return (
    <div className="list-page">
      <div className="dash-head">
        <h2 data-testid="music-title">Music</h2>
        <div className="dash-head-right">
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
                {expanded.has(a.ID) && <AlbumList artistId={a.ID} />}
              </div>
            ))
          )}
          <Pagination page={page} pageSize={DEFAULT_PAGE_SIZE} total={total} onPage={setPage} />
        </div>
      )}
    </div>
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
