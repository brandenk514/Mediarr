import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { ApiError, client } from '../../api/client'
import type {
  Album,
  Artist,
  MusicHistoryEntry,
  PageArtist,
  ProfilesResponse,
  Track,
  UpdateArtistRequest,
} from '../../api/types'
import { MonitoredToggle } from '../../components/list'
import {
  ALBUM_TYPE_ORDER,
  MONITOR_POLICY_OPTIONS,
  albumTypeLabel,
  fmtAudioInfo,
  fmtDuration,
  humanBytes,
  relTime,
} from '../../components/music'
import { Modal, Toolbar, TbButton } from '../../components/ui'

// ArtistDetailPage is the Lidarr L1 artist detail — a single scrolling page:
// toolbar (Refresh & Scan / Search Monitored / Interactive Search | Preview
// Rename / Preview Retag / Manage Tracks / History / Manual Import | Artist
// Monitoring / Edit / Delete | Expand All), the banner header (name, rating,
// genre, green pill row, prev/next paging), the overview (honest empty state
// until artist metadata lands), and the Albums accordion grouped by album
// type: type group -> album rows -> disc sections -> track tables (four
// levels of lazy expansion; tracks load per album on expand).
//
// L1 wiring: Refresh & Scan and Search Monitored hit the existing artist
// pipeline endpoint; History opens a modal over GET /music/artists/{id}/history;
// Artist Monitoring opens the artist + per-album toggles; Edit opens the
// L2 Edit Artist modal (Monitored · Monitor New Albums · Monitor Policy ·
// Quality Profile · Metadata Profile (L3 stub) · Path · Tags) which persists
// through PUT /music/artists/{id}. Delete is a stub — no artist delete
// endpoint exists yet. Interactive Search / Preview Rename / Preview Retag /
// Manage Tracks / Manual Import are buttons (stubs) — those music-only
// endpoints land in later phases.
const DASH = '—'

type ModalKind = 'history' | 'monitoring' | 'edit'

// albumStatus derives the album row's status from the L1 album aggregates:
// all tracks have files = Downloaded, otherwise Missing.
function albumStatus(a: Album): string {
  const t = a.TrackCount ?? 0
  const f = a.FileCount ?? 0
  return t > 0 && f >= t ? 'Downloaded' : 'Missing'
}

export function ArtistDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const artistId = Number(id)
  const [artist, setArtist] = useState<Artist | null>(null)
  const [albums, setAlbums] = useState<Album[] | null>(null)
  const [lib, setLib] = useState<Artist[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [authLost, setAuthLost] = useState(false)
  const [status, setStatus] = useState<string | null>(null)
  const [searching, setSearching] = useState(false)
  const [openGroups, setOpenGroups] = useState<Set<string>>(() => new Set())
  const [openAlbums, setOpenAlbums] = useState<Set<number>>(() => new Set())
  const [tracksByAlbum, setTracksByAlbum] = useState<Record<number, Track[]>>({})
  const [loadingTracks, setLoadingTracks] = useState<Set<number>>(() => new Set())
  const [modal, setModal] = useState<ModalKind | null>(null)
  const [history, setHistory] = useState<MusicHistoryEntry[] | null>(null)
  const [historyError, setHistoryError] = useState<string | null>(null)
  const [edit, setEdit] = useState<{
    monitored: boolean
    monitorPolicy: NonNullable<UpdateArtistRequest['monitor_policy']>
    monitorNewAlbums: NonNullable<UpdateArtistRequest['monitor_new_albums']>
    profile: string
    path: string
    tags: string[]
    tagDraft: string
  } | null>(null)
  // The Edit modal's Quality Profile picker (L2): music profiles loaded
  // lazily when the modal opens.
  const [editProfiles, setEditProfiles] = useState<ProfilesResponse | null>(null)

  const load = useCallback(async () => {
    if (!artistId || Number.isNaN(artistId)) {
      setError('Unknown artist.')
      return
    }
    try {
      const [a, albs] = await Promise.all([client.getArtist(artistId), client.listAlbums(artistId)])
      setArtist(a)
      setAlbums(albs ?? []) // older backends return null for an empty list
      setError(null)
      setAuthLost(false)
      // Prev/next navigation (by library order). A failure here must not
      // break the page — the arrows simply don't render.
      client
        .listArtists({})
        .then((page: PageArtist) => setLib(page.items))
        .catch(() => setLib(null))
    } catch (e) {
      if (e instanceof ApiError && e.status === 404) {
        setError(`Artist ${artistId} was not found.`)
        return
      }
      if (e instanceof ApiError && e.status === 401) setAuthLost(true)
      setError(e instanceof Error ? e.message : 'failed to load artist')
    }
  }, [artistId])

  useEffect(() => {
    void load()
  }, [load])

  // History loads lazily when the modal opens (L1 detail endpoint).
  useEffect(() => {
    if (modal !== 'history') return
    setHistory(null)
    setHistoryError(null)
    client
      .getArtistHistory(artistId)
      .then(setHistory)
      .catch((e: unknown) => setHistoryError(e instanceof Error ? e.message : 'failed to load history'))
  }, [modal, artistId])

  // The Edit modal's Quality Profile picker loads the music profiles lazily
  // when the modal opens (L2).
  useEffect(() => {
    if (modal !== 'edit') return
    setEditProfiles(null)
    client
      .getProfiles('music')
      .then(setEditProfiles)
      .catch(() => setEditProfiles(null))
  }, [modal])

  // Open one more toolbar action (refresh/search). Both wire to the existing
  // per-artist pipeline endpoint (the only search backend in L1).
  async function runPipeline(action: 'refresh' | 'search') {
    if (searching) return
    setSearching(true)
    setStatus(null)
    try {
      const res = await client.runMusicPipeline(artistId)
      setStatus(
        action === 'search'
          ? `Search finished — imported ${res.imported || 'a file'}.`
          : `Refresh & scan finished — imported ${res.imported || 'a file'}.`,
      )
      await load()
    } catch (e) {
      setStatus(e instanceof Error ? e.message : 'pipeline failed')
    } finally {
      setSearching(false)
    }
  }

  // Tracks are lazy-loaded per album: whenever an album is open (row click
  // or Expand All) and its tracks are not loaded yet, fetch them.
  useEffect(() => {
    const pending = [...openAlbums].filter(
      (id) => tracksByAlbum[id] === undefined && !loadingTracks.has(id),
    )
    if (pending.length === 0) return
    setLoadingTracks((prev) => new Set([...prev, ...pending]))
    Promise.all(
      pending.map(async (id) => {
        try {
          const t = await client.listTracks(id)
          return { id, t: t ?? [] }
        } catch (e) {
          setStatus(e instanceof Error ? e.message : 'failed to load tracks')
          return { id, t: [] as Track[] }
        }
      }),
    ).then((results) => {
      setTracksByAlbum((prev) => {
        const next = { ...prev }
        for (const r of results) next[r.id] = r.t
        return next
      })
      setLoadingTracks((prev) => {
        const next = new Set(prev)
        for (const id of pending) next.delete(id)
        return next
      })
    })
  }, [openAlbums, tracksByAlbum, loadingTracks])

  function toggleGroup(key: string) {
    setOpenGroups((prev) => {
      const next = new Set(prev)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })
  }

  function expandAll() {
    if (!albums) return
    setOpenGroups(new Set(groups.map((g) => g.key)))
    setOpenAlbums(new Set(albums.map((a) => a.ID)))
  }

  // Group albums by album type (empty = album), in Lidarr's group order.
  const groups = useMemo(() => {
    if (!albums) return []
    const byType = new Map<string, Album[]>()
    for (const a of albums) {
      const key = a.AlbumType || 'album'
      const arr = byType.get(key) ?? []
      arr.push(a)
      byType.set(key, arr)
    }
    const out: { key: string; label: string; albums: Album[] }[] = []
    for (const k of ALBUM_TYPE_ORDER) {
      if (byType.has(k)) out.push({ key: k, label: albumTypeLabel(k), albums: byType.get(k)! })
    }
    // Unknown types (a future enum value) still render, after the known ones.
    for (const k of byType.keys()) {
      if (!(ALBUM_TYPE_ORDER as readonly string[]).includes(k)) {
        out.push({ key: k, label: albumTypeLabel(k), albums: byType.get(k)! })
      }
    }
    return out
  }, [albums])

  if (error && !artist) {
    return (
      <div className="card" data-testid="artist-detail-error">
        <h2>Artist unavailable</h2>
        <p className="error" data-testid="artist-detail-error-message">
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

  if (!artist || !albums) {
    return <p className="dim" data-testid="artist-detail-loading">Loading artist…</p>
  }

  // Prev/next in library (server) order.
  let prevId: number | null = null
  let nextId: number | null = null
  if (lib) {
    const idx = lib.findIndex((a) => a.ID === artist.ID)
    if (idx > 0) prevId = lib[idx - 1]!.ID
    if (idx >= 0 && idx < lib.length - 1) nextId = lib[idx + 1]!.ID
  }

  const links = artist.Links ?? []

  return (
    <div className="list-page" data-testid="artist-detail">
      <div className="page-head">
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.8rem' }}>
          <Link to="/music" className="ghost" data-testid="artist-detail-back">
            ← Artists
          </Link>
        </div>
      </div>

      <Toolbar
        left={
          <>
            <TbButton icon="⟳" label="Refresh & Scan" testId="artist-detail-refresh" onClick={() => void runPipeline('refresh')} />
            <TbButton
              icon="🔍"
              label="Search Monitored"
              testId="artist-detail-search"
              disabled={searching}
              onClick={() => void runPipeline('search')}
            />
            <TbButton
              icon="🔎"
              label="Interactive Search"
              testId="artist-detail-interactive"
              disabled
              title="Interactive search lands in a later phase"
            />
            <TbButton
              icon="✎"
              label="Preview Rename"
              testId="artist-detail-rename"
              divider
              disabled
              title="Rename preview lands in a later phase"
            />
            <TbButton
              icon="🏷"
              label="Preview Retag"
              testId="artist-detail-retag"
              disabled
              title="Retag preview lands in a later phase"
            />
            <TbButton
              icon="♫"
              label="Manage Tracks"
              testId="artist-detail-manage-tracks"
              disabled
              title="Track management lands in a later phase"
            />
            <TbButton icon="🕘" label="History" testId="artist-detail-history" onClick={() => setModal('history')} />
            <TbButton
              icon="⇥"
              label="Manual Import"
              testId="artist-detail-manual-import"
              disabled
              title="Manual import lands in a later phase"
            />
            <TbButton
              icon="◎"
              label="Artist Monitoring"
              testId="artist-detail-monitoring"
              divider
              onClick={() => setModal('monitoring')}
            />
            <TbButton
              icon="✏"
              label="Edit"
              testId="artist-detail-edit"
              onClick={() => {
                // '' is the wire form of "all"/"all_albums" for artists added
                // before the UI offers a choice — coerce so the selects show
                // a selected option.
                setEdit({
                  monitored: artist.Monitored,
                  monitorPolicy: artist.MonitorPolicy || 'all',
                  monitorNewAlbums: artist.MonitorNewAlbums || 'all_albums',
                  profile: artist.QualityProfile ?? '',
                  path: artist.Path ?? '',
                  tags: artist.Tags ?? [],
                  tagDraft: '',
                })
                setModal('edit')
              }}
            />
            <TbButton
              icon="🗑"
              label="Delete"
              testId="artist-detail-delete"
              disabled
              title="Artist deletion lands in a later phase"
            />
          </>
        }
        right={
          <TbButton icon="⊞" label="Expand All" testId="artist-detail-expand-all" divider onClick={expandAll} />
        }
      />

      {status && (
        <p className="dim small" data-testid="artist-detail-status" role="status">
          {status}
        </p>
      )}

      {/* Banner header */}
      <div className="artist-banner" data-testid="artist-detail-banner">
        <h2 data-testid="artist-detail-name">{artist.Name}</h2>
        {(artist.Rating && artist.Rating > 0 || artist.Genre) && (
          <div className="ab-meta" data-testid="artist-detail-meta">
            {artist.Rating && artist.Rating > 0 && (
              <span data-testid="artist-detail-rating">♥ {Math.round(artist.Rating)}%</span>
            )}
            {artist.Genre && <span data-testid="artist-detail-genre">{artist.Genre}</span>}
          </div>
        )}
        <div className="ab-pills">
          {artist.Path && (
            <span className="pill-green" data-testid="pill-path">
              {artist.Path}
            </span>
          )}
          {(artist.TotalSize ?? 0) > 0 && (
            <span className="pill-green" data-testid="pill-size">
              {humanBytes(artist.TotalSize!)}
            </span>
          )}
          {artist.QualityProfile && (
            <span className="pill-green" data-testid="pill-profile">
              {artist.QualityProfile}
            </span>
          )}
          <span className="pill-green" data-testid="pill-monitored">
            {artist.Monitored ? 'Monitored' : 'Unmonitored'}
          </span>
          {artist.Status && (
            <span className="pill-green" data-testid="pill-status">
              {artist.Status[0]!.toUpperCase() + artist.Status.slice(1)}
            </span>
          )}
          {links.map((l) => (
            <a
              key={l.Name}
              className="pill-green"
              href={l.URL}
              target="_blank"
              rel="noreferrer"
              data-testid={`pill-link-${l.Name}`}
            >
              {l.Name}
            </a>
          ))}
        </div>
        <div className="banner-nav">
          {prevId !== null && (
            <button
              type="button"
              className="nav-arrow"
              title="Previous artist"
              aria-label="Previous artist"
              data-testid="artist-detail-prev"
              onClick={() => navigate(`/music/${prevId}`)}
            >
              ↑
            </button>
          )}
          {nextId !== null && (
            <button
              type="button"
              className="nav-arrow"
              title="Next artist"
              aria-label="Next artist"
              data-testid="artist-detail-next"
              onClick={() => navigate(`/music/${nextId}`)}
            >
              ↓
            </button>
          )}
        </div>
      </div>

      {/* Overview — the API carries no artist bio in L1; honest empty state. */}
      <p className="dim" data-testid="artist-detail-overview">
        No overview yet — artist metadata lands in a later phase.
      </p>

      {/* Albums accordion, grouped by album type. */}
      <div data-testid="artist-detail-albums">
        {groups.length === 0 ? (
          <p className="dim" data-testid="artist-detail-no-albums">
            No albums yet.
          </p>
        ) : (
          groups.map((g) => {
            const open = openGroups.has(g.key)
            const fileCount = g.albums.reduce((n, a) => n + (a.FileCount ?? 0), 0)
            const trackCount = g.albums.reduce((n, a) => n + (a.TrackCount ?? 0), 0)
            const size = g.albums.reduce((n, a) => n + (a.Size ?? 0), 0)
            return (
              <div className="type-group" key={g.key} data-testid={`type-group-${g.key}`}>
                <button
                  type="button"
                  className="type-group-head"
                  aria-expanded={open}
                  onClick={() => toggleGroup(g.key)}
                  data-testid={`type-toggle-${g.key}`}
                >
                  <span>{open ? '▾' : '▸'}</span>
                  <span>{g.label}</span>
                  <span className="tg-count" data-testid={`type-count-${g.key}`}>
                    {fileCount}/{trackCount}
                  </span>
                  {size > 0 && <span className="tg-size" data-testid={`type-size-${g.key}`}>{humanBytes(size)}</span>}
                </button>
                {open &&
                  g.albums.map((al) => (
                    <AlbumSection
                      key={al.ID}
                      album={al}
                      open={openAlbums.has(al.ID)}
                      tracks={tracksByAlbum[al.ID]}
                      loadingTracks={loadingTracks.has(al.ID)}
                      onToggle={() =>
                        setOpenAlbums((prev) => {
                          const next = new Set(prev)
                          if (next.has(al.ID)) next.delete(al.ID)
                          else next.add(al.ID)
                          return next
                        })
                      }
                    />
                  ))}
              </div>
            )
          })
        )}
        {/* Helper line — Lidarr's bottom hint; Metadata Profile / Search are
            non-functional in L1 (L2/L3 wire them). */}
        {groups.length > 0 && (
          <p className="helper-line" data-testid="artist-detail-helper">
            Missing Albums, Singles, or Other Types? Modify or create a new{' '}
            <a data-testid="helper-metadata-profile" onClick={(e) => e.preventDefault()}>
              Metadata Profile
            </a>{' '}
            or manually{' '}
            <a data-testid="helper-search" onClick={(e) => e.preventDefault()}>
              Search
            </a>{' '}
            for new items!
          </p>
        )}
      </div>

      {/* History modal */}
      {modal === 'history' && (
        <Modal title="History" onClose={() => setModal(null)} testId="history-modal">
          {historyError ? (
            <p className="error">{historyError}</p>
          ) : history === null ? (
            <p className="dim">Loading history…</p>
          ) : history.length === 0 ? (
            <p className="dim">No history yet.</p>
          ) : (
            history.map((h, i) => (
              <div className="history-row" key={`${h.Event}-${h.At}-${i}`} data-testid={`history-${i}`}>
                <span className="hr-event">{h.Event}</span>
                <span className="hr-detail">{h.Detail}</span>
                <span className="hr-at">{relTime(h.At)}</span>
              </div>
            ))
          )}
        </Modal>
      )}

      {/* Artist monitoring modal: artist-level toggle + per-album toggles. */}
      {modal === 'monitoring' && (
        <Modal title="Artist Monitoring" onClose={() => setModal(null)} testId="monitoring-modal">
          <div style={{ marginBottom: '0.6rem' }}>
            <MonitoredToggle
              monitored={artist.Monitored}
              onToggle={async (next) => {
                await client.setArtistMonitored(artist.ID, next)
                setArtist({ ...artist, Monitored: next })
              }}
              label={`${artist.Name} monitoring`}
              testId="monitoring-artist"
            />
          </div>
          {albums.length === 0 ? (
            <p className="dim">No albums to toggle.</p>
          ) : (
            albums.map((al) => (
              <div key={al.ID} style={{ display: 'flex', alignItems: 'center', gap: '0.6rem', padding: '0.3rem 0' }}>
                <span style={{ flex: 1, minWidth: 0 }} data-testid={`monitoring-album-${al.ID}`}>
                  {al.Name}
                </span>
                <MonitoredToggle
                  monitored={al.Monitored}
                  onToggle={async (next) => {
                    await client.setAlbumMonitored(al.ID, next)
                    setAlbums((prev) =>
                      (prev ?? []).map((x) => (x.ID === al.ID ? { ...x, Monitored: next } : x)),
                    )
                  }}
                  label={`${al.Name} monitoring`}
                  testId={`monitoring-album-mon-${al.ID}`}
                />
              </div>
            ))
          )}
        </Modal>
      )}

      {/* Edit modal (L2). Persists through PUT /music/artists/{id}
          (UpdateArtistRequest): Monitored · Monitor New Albums · Monitor
          Policy · Quality Profile · Metadata Profile (L3 stub — disabled) ·
          Path · Tags, in the Lidarr Edit Artist order. A monitored change
          fans out to the artist's known albums server-side. */}
      {modal === 'edit' && edit && (
        <Modal
          title="Edit Artist"
          onClose={() => setModal(null)}
          testId="edit-modal"
          footer={
            <>
              <button type="button" className="ghost" onClick={() => setModal(null)}>
                Cancel
              </button>
              <button
                type="button"
                data-testid="edit-save"
                onClick={() => {
                  void (async () => {
                    await client.updateArtist(artist.ID, {
                      monitored: edit.monitored,
                      monitor_policy: edit.monitorPolicy,
                      monitor_new_albums: edit.monitorNewAlbums,
                      quality_profile: edit.profile,
                      path: edit.path,
                      tags: edit.tags,
                    })
                    setModal(null)
                    setArtist({ ...artist, Monitored: edit.monitored })
                    setStatus('Artist updated.')
                    void load()
                  })()
                }}
              >
                Save
              </button>
            </>
          }
        >
          <label className="check" htmlFor="edit-monitored" style={{ marginBottom: '0.6rem' }}>
            <input
              id="edit-monitored"
              type="checkbox"
              checked={edit.monitored}
              onChange={(e) => setEdit({ ...edit, monitored: e.target.checked })}
              data-testid="edit-monitored"
            />
            Monitored
          </label>
          <p className="dim small" style={{ marginTop: '0' }}>
            Download monitored albums from this artist
          </p>

          <label style={{ display: 'block', marginBottom: '0.6rem' }}>
            <span className="dim small">Monitor New Albums</span>
            <select
              value={edit.monitorNewAlbums}
              onChange={(e) => setEdit({ ...edit, monitorNewAlbums: e.target.value as 'all_albums' })}
              data-testid="edit-monitor-new-albums"
            >
              <option value="all_albums">All Albums</option>
            </select>
          </label>
          <p className="dim small" style={{ marginTop: '0', marginBottom: '0.6rem' }}>
            Which new albums should be monitored
          </p>

          <label style={{ display: 'block', marginBottom: '0.6rem' }}>
            <span className="dim small">Monitor Policy</span>
            <select
              value={edit.monitorPolicy}
              onChange={(e) =>
                setEdit({ ...edit, monitorPolicy: e.target.value as NonNullable<UpdateArtistRequest['monitor_policy']> })
              }
              data-testid="edit-monitor-policy"
            >
              {MONITOR_POLICY_OPTIONS.map((o) => (
                <option key={o.value} value={o.value}>
                  {o.label}
                </option>
              ))}
            </select>
          </label>
          <p className="dim small" style={{ marginTop: '0', marginBottom: '0.6rem' }}>
            Which albums should be monitored after the artist is added (one-time adjustment)
          </p>

          <label style={{ display: 'block', marginBottom: '0.6rem' }}>
            <span className="dim small">Quality Profile</span>
            <select
              value={edit.profile}
              onChange={(e) => setEdit({ ...edit, profile: e.target.value })}
              data-testid="edit-profile"
            >
              {(editProfiles?.profiles ?? []).map((p) => (
                <option key={p} value={p}>
                  {p}
                  {editProfiles?.default === p ? ' (default)' : ''}
                </option>
              ))}
              {editProfiles !== null && (editProfiles.profiles ?? []).length === 0 && (
                <option value="">— No profiles —</option>
              )}
            </select>
          </label>

          <label style={{ display: 'block', marginBottom: '0.6rem' }}>
            <span className="dim small">Metadata Profile</span>
            <select value="Standard" disabled data-testid="edit-metadata-profile">
              <option value="Standard">Standard</option>
            </select>
          </label>
          <p className="dim small" style={{ marginTop: '0', marginBottom: '0.6rem' }}>
            Metadata profiles land in a later phase (L3).
          </p>

          <label style={{ display: 'block', marginBottom: '0.6rem' }}>
            <span className="dim small">Path</span>
            <input
              type="text"
              value={edit.path}
              onChange={(e) => setEdit({ ...edit, path: e.target.value })}
              data-testid="edit-path"
            />
          </label>

          <label style={{ display: 'block', marginBottom: '0.6rem' }}>
            <span className="dim small">Tags</span>
            <input
              type="text"
              value={edit.tagDraft}
              onChange={(e) => setEdit({ ...edit, tagDraft: e.target.value })}
              onKeyDown={(e) => {
                if (e.key === 'Enter' || e.key === ',') {
                  e.preventDefault()
                  const t = edit.tagDraft.trim().replace(/,+$/, '')
                  if (t && !edit.tags.includes(t)) setEdit({ ...edit, tags: [...edit.tags, t], tagDraft: '' })
                }
              }}
              onBlur={() => {
                const t = edit.tagDraft.trim().replace(/,+$/, '')
                if (t && !edit.tags.includes(t)) setEdit({ ...edit, tags: [...edit.tags, t], tagDraft: '' })
              }}
              placeholder="Add a tag, press Enter"
              data-testid="edit-tags-input"
            />
          </label>
          {edit.tags.length > 0 && (
            <span className="tag-chips" data-testid="edit-tags-chips">
              {edit.tags.map((t) => (
                <span className="tag-chip" key={t}>
                  {t}
                  <button
                    type="button"
                    aria-label={`Remove tag ${t}`}
                    onClick={() => setEdit({ ...edit, tags: edit.tags.filter((x) => x !== t) })}
                    data-testid={`edit-tag-remove-${t}`}
                  >
                    ✕
                  </button>
                </span>
              ))}
            </span>
          )}
        </Modal>
      )}
    </div>
  )
}

// AlbumSection is one album inside a type group: the row (title, release
// date, rating, status), and when expanded the album sub-header, the disc
// sections ('CD 1 — 12/12'), and the track tables.
function AlbumSection({
  album,
  open,
  tracks,
  loadingTracks,
  onToggle,
}: {
  album: Album
  open: boolean
  tracks: Track[] | undefined
  loadingTracks: boolean
  onToggle: () => void
}) {
  const releaseDate = album.ReleaseDate || (album.Year > 0 ? String(album.Year) : DASH)
  return (
    <>
      <div className="album-row" data-testid={`album-${album.ID}`}>
        <button
          type="button"
          className="ghost"
          onClick={onToggle}
          aria-expanded={open}
          style={{ background: 'none', padding: 0, color: 'var(--text)' }}
          data-testid={`album-toggle-${album.ID}`}
        >
          {open ? '▾' : '▸'}
        </button>
        <span className="ar-title">{album.Name}</span>
        <span className="lt-dim" data-testid={`album-date-${album.ID}`}>
          {releaseDate}
        </span>
        {(album.Rating ?? 0) > 0 && (
          <span className="lt-dim" data-testid={`album-rating-${album.ID}`}>
            ♥ {Math.round(album.Rating!)}%
          </span>
        )}
        <span className="lt-dim" data-testid={`album-status-${album.ID}`}>
          {albumStatus(album)}
        </span>
      </div>
      {open && (
        <>
          <div className="album-sub" data-testid={`album-sub-${album.ID}`}>
            {(album.Duration ?? 0) > 0 && <span>{fmtDuration(album.Duration)}</span>}
            {(album.Rating ?? 0) > 0 && <span>♥ {Math.round(album.Rating!)}%</span>}
            {album.ReleaseDate && <span>{album.ReleaseDate}</span>}
            {(album.Size ?? 0) > 0 && <span>{humanBytes(album.Size!)}</span>}
            <span data-testid={`album-monitored-${album.ID}`}>
              {album.Monitored ? 'Monitored' : 'Unmonitored'}
            </span>
          </div>
          {loadingTracks ? (
            <p className="dim" style={{ padding: '0.4rem 0.8rem' }} data-testid={`album-loading-${album.ID}`}>
              Loading tracks…
            </p>
          ) : !tracks ? null : tracks.length === 0 ? (
            <p className="dim" style={{ padding: '0.4rem 0.8rem' }} data-testid={`album-no-tracks-${album.ID}`}>
              No tracks.
            </p>
          ) : (
            <DiscSections albumId={album.ID} tracks={tracks} />
          )}
        </>
      )}
    </>
  )
}

// DiscSections groups the track list into per-disc sections ('CD 1 — 12/12'
// = downloaded/total for that disc; single-disc albums show one section) and
// renders the track table: Track · Title · Duration · Audio Info · Status.
function DiscSections({ albumId, tracks }: { albumId: number; tracks: Track[] }) {
  const byDisc = new Map<number, Track[]>()
  for (const t of tracks) {
    const arr = byDisc.get(t.Disc) ?? []
    arr.push(t)
    byDisc.set(t.Disc, arr)
  }
  const discs = [...byDisc.entries()].sort((a, b) => a[0] - b[0])
  return (
    <>
      {discs.map(([disc, discTracks]) => {
        const downloaded = discTracks.filter((t) => (t.Status ?? '') !== '').length
        return (
          <div key={disc}>
            <div className="disc-head" data-testid={`disc-${albumId}-${disc}`}>
              CD {disc} — {downloaded}/{discTracks.length}
            </div>
            <table className="track-table" data-testid={`track-table-${albumId}-${disc}`}>
              <thead>
                <tr>
                  <th>Track</th>
                  <th>Title</th>
                  <th>Duration</th>
                  <th>Audio Info</th>
                  <th>Status</th>
                </tr>
              </thead>
              <tbody>
                {discTracks.map((t) => (
                  <tr key={t.ID} data-testid={`track-${albumId}-${t.Disc}-${t.Number}`}>
                    <td className="dim">{t.Number}</td>
                    <td>{t.Title || DASH}</td>
                    <td className="dim">{fmtDuration(t.Duration)}</td>
                    <td className="dim" data-testid={`track-audio-${albumId}-${t.Number}`}>
                      {fmtAudioInfo(t)}
                    </td>
                    <td className="dim" data-testid={`track-status-${albumId}-${t.Number}`}>
                      {t.Status || DASH}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )
      })}
    </>
  )
}
