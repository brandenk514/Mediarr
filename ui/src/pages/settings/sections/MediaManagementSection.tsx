import { useEffect, useState } from 'react'
import { client } from '../../../api/client'
import type {
  FileManagement,
  MediaManagementSettings,
  MovieNaming,
  MusicNaming,
  NamingTemplate,
  ProfilesSettings,
  RemotePathMapping,
  RootFolder,
} from '../../../api/types'
import { SectionCard, SectionError, useSectionDoc, useSubmit } from './shared'

// MediaManagementSection is the media-management section: naming templates
// per media kind (TV parity first), the Radarr movie-naming block (R5b:
// Rename Movies / Replace Illegal Characters / Colon Replacement / Standard
// Movie Format — separate from the per-kind 'Movie' template row, which is
// the TV-era NamingTemplate.movie), the Lidarr track-naming block (L3:
// Rename Tracks / Replace Illegal Characters / Colon Replacement / Standard
// Format / Multi-Disc Format), the file-management options (including
// Unmonitor Deleted Movies), remote path mappings (indexer/download paths ->
// local), and the root-folder list (paths owned by the P2 root-folders
// domain and the add flow; the per-root default music quality + metadata
// profile pickers write straight to that registry).
const TEMPLATE_FIELDS: { field: keyof NamingTemplate; label: string }[] = [
  { field: 'series', label: 'Series' },
  { field: 'episode', label: 'Episode' },
  { field: 'movie', label: 'Movie' },
  { field: 'album', label: 'Album' },
  { field: 'track', label: 'Track' },
  { field: 'author', label: 'Author (artist)' },
  { field: 'title', label: 'Title (book)' },
]

export function MediaManagementSection() {
  const { doc, loading, error, authLost, refresh } = useSectionDoc<MediaManagementSettings>('media-management')

  const [naming, setNaming] = useState<NamingTemplate | null>(null)
  const [movieNaming, setMovieNaming] = useState<MovieNaming | null>(null)
  // Lidarr L3: the track-naming block. A missing music_naming (pre-L3
  // documents) seeds null — the block renders its fallbacks and saves
  // pass the stored value through non-destructively.
  const [musicNaming, setMusicNaming] = useState<MusicNaming | null>(null)
  const [fileMgmt, setFileMgmt] = useState<FileManagement | null>(null)
  const [mappings, setMappings] = useState<RemotePathMapping[]>([])
  const [mapSource, setMapSource] = useState('')
  const [mapDest, setMapDest] = useState('')
  const [roots, setRoots] = useState<RootFolder[]>([])
  // Lidarr L3: the per-root profile picker options — music profile names
  // from the profiles endpoint (kind=music) and metadata profile names from
  // the profiles section document.
  const [musicProfileNames, setMusicProfileNames] = useState<string[]>([])
  const [metadataProfileNames, setMetadataProfileNames] = useState<string[]>([])
  const [saved, setSaved] = useState(false)
  const { busy, submit } = useSubmit()

  useEffect(() => {
    if (!doc) return
    setNaming(doc.naming)
    setMovieNaming(doc.movie_naming)
    setMusicNaming(doc.music_naming ?? null)
    // The server defaults unmonitor_deleted_movies to true (R5a); normalise a
    // missing stored value so the toggle round-trips the default explicitly.
    setFileMgmt({
      ...doc.file_management,
      unmonitor_deleted_movies: doc.file_management.unmonitor_deleted_movies ?? true,
    })
    setMappings(doc.remote_path_mappings)
  }, [doc])

  // Root folders are the P2 domain (the add flow); paths are read-only here,
  // but the per-root default profile pickers (L3) write to the registry.
  useEffect(() => {
    let cancelled = false
    client
      .listRootFolders()
      .then((r) => {
        if (!cancelled) setRoots(r)
      })
      .catch(() => {
        if (!cancelled) setRoots([])
      })
    return () => {
      cancelled = true
    }
  }, [])

  // Picker options for the per-root profile selects (Lidarr L3).
  useEffect(() => {
    let cancelled = false
    client
      .getProfiles('music')
      .then((r) => {
        if (!cancelled) setMusicProfileNames(r.profiles)
      })
      .catch(() => {
        if (!cancelled) setMusicProfileNames([])
      })
    client
      .getSettingsSection<ProfilesSettings>('profiles')
      .then((p) => {
        if (!cancelled) setMetadataProfileNames((p.metadata_profiles ?? []).map((m) => m.name))
      })
      .catch(() => {
        if (!cancelled) setMetadataProfileNames([])
      })
    return () => {
      cancelled = true
    }
  }, [])

  if (error) {
    return (
      <SectionError
        message={error}
        authLost={authLost}
        onRetry={refresh}
        testId="media-management-error"
      />
    )
  }

  function patchTemplate(field: keyof NamingTemplate, value: string) {
    if (!naming) return
    setNaming({ ...naming, [field]: value })
  }

  function patchMovieNaming(patch: Partial<MovieNaming>) {
    if (!movieNaming) return
    setMovieNaming({ ...movieNaming, ...patch })
  }

  function patchMusicNaming(patch: Partial<MusicNaming>) {
    if (!musicNaming) return
    setMusicNaming({ ...musicNaming, ...patch })
  }

  function patchFileMgmt(patch: Partial<FileManagement>) {
    if (!fileMgmt) return
    setFileMgmt({ ...fileMgmt, ...patch })
  }

  function addMapping() {
    if (!mapSource.trim()) return
    setMappings((prev) => [...prev, { source: mapSource.trim(), destination: mapDest.trim() }])
    setMapSource('')
    setMapDest('')
  }

  function removeMapping(i: number) {
    setMappings((prev) => prev.filter((_, idx) => idx !== i))
  }

  // Lidarr L3: apply a per-root default profile pick immediately — the
  // root-folders registry is written per change (no local staging), sending
  // BOTH fields (an omitted-or-"" field is a deliberate clear on the server,
  // so the untouched field's current value goes along).
  function applyRootProfile(
    r: RootFolder,
    field: 'default_quality_profile' | 'default_metadata_profile',
    value: string,
  ) {
    const req = {
      default_quality_profile: field === 'default_quality_profile' ? value : (r.DefaultQualityProfile ?? ''),
      default_metadata_profile:
        field === 'default_metadata_profile' ? value : (r.DefaultMetadataProfile ?? ''),
    }
    void client
      .updateRootFolder(r.ID, req)
      .then((updated) => {
        setRoots((prev) => prev.map((x) => (x.ID === r.ID ? updated : x)))
      })
      .catch(() => {
        /* registry write failed; the row keeps its previous pick */
      })
  }

  function handleSave() {
    if (!doc) return
    if (!naming || !fileMgmt || !movieNaming) return
    void submit(async () => {
      const next: MediaManagementSettings = {
        naming,
        movie_naming: movieNaming,
        // Lidarr L3: send the local block when present; a pre-L3 doc (no
        // music_naming) passes its stored value through non-destructively.
        music_naming: musicNaming ?? doc.music_naming,
        file_management: fileMgmt,
        remote_path_mappings: mappings,
      }
      await client.putSettingsSection<MediaManagementSettings>('media-management', next)
      setSaved(true)
      setTimeout(() => setSaved(false), 2000)
    })
  }

  return (
    <SectionCard
      title="Media Management"
      hint="Naming templates, file management, remote path mappings, and the root-folder list."
      testId="media-management-card"
    >
      <h4>Naming</h4>
      <div className="row wrap">
        {TEMPLATE_FIELDS.map(({ field, label }) => (
          <div key={field} className="col">
            <label htmlFor={`mm-${field}`}>{label}</label>
            <input
              id={`mm-${field}`}
              value={naming?.[field] ?? ''}
              onChange={(e) => patchTemplate(field, e.target.value)}
              data-testid={`mm-${field}`}
            />
          </div>
        ))}
      </div>

      <h4>Movie naming</h4>
      <div data-testid="mm-movie-naming">
        <label className="check" htmlFor="mm-movie-rename">
          <input
            id="mm-movie-rename"
            type="checkbox"
            checked={movieNaming?.rename_movies ?? true}
            onChange={(e) => patchMovieNaming({ rename_movies: e.target.checked })}
            data-testid="mm-movie-rename"
          />
          Rename Movies
        </label>
        <label className="check" htmlFor="mm-movie-illegal">
          <input
            id="mm-movie-illegal"
            type="checkbox"
            checked={movieNaming?.replace_illegal_characters ?? true}
            onChange={(e) => patchMovieNaming({ replace_illegal_characters: e.target.checked })}
            data-testid="mm-movie-illegal"
          />
          Replace Illegal Characters
        </label>
        <label htmlFor="mm-movie-colon">Colon Replacement</label>
        <input
          id="mm-movie-colon"
          value={movieNaming?.colon_replacement ?? ''}
          maxLength={1}
          placeholder="e.g. '"
          onChange={(e) => patchMovieNaming({ colon_replacement: e.target.value.slice(-1) })}
          data-testid="mm-movie-colon"
        />
        <label htmlFor="mm-movie-format">Standard Movie Format</label>
        <input
          id="mm-movie-format"
          value={movieNaming?.standard_format ?? ''}
          onChange={(e) => patchMovieNaming({ standard_format: e.target.value })}
          data-testid="mm-movie-format"
        />
      </div>

      <h4>Track naming</h4>
      <div data-testid="mm-music-naming">
        <label className="check" htmlFor="mm-track-rename">
          <input
            id="mm-track-rename"
            type="checkbox"
            checked={musicNaming?.rename_tracks ?? true}
            onChange={(e) => patchMusicNaming({ rename_tracks: e.target.checked })}
            data-testid="mm-track-rename"
          />
          Rename Tracks
        </label>
        <label className="check" htmlFor="mm-track-illegal">
          <input
            id="mm-track-illegal"
            type="checkbox"
            checked={musicNaming?.replace_illegal_characters ?? true}
            onChange={(e) => patchMusicNaming({ replace_illegal_characters: e.target.checked })}
            data-testid="mm-track-illegal"
          />
          Replace Illegal Characters
        </label>
        <label htmlFor="mm-track-colon">Colon Replacement</label>
        <input
          id="mm-track-colon"
          value={musicNaming?.colon_replacement ?? ''}
          maxLength={1}
          placeholder="e.g. '"
          onChange={(e) => patchMusicNaming({ colon_replacement: e.target.value.slice(-1) })}
          data-testid="mm-track-colon"
        />
        <label htmlFor="mm-track-format">Standard Format</label>
        <input
          id="mm-track-format"
          value={musicNaming?.standard_format ?? ''}
          onChange={(e) => patchMusicNaming({ standard_format: e.target.value })}
          data-testid="mm-track-format"
        />
        <label htmlFor="mm-track-multidisc">Multi-Disc Format</label>
        <input
          id="mm-track-multidisc"
          value={musicNaming?.multi_disc_format ?? ''}
          onChange={(e) => patchMusicNaming({ multi_disc_format: e.target.value })}
          data-testid="mm-track-multidisc"
        />
      </div>

      <h4>File management</h4>
      <label htmlFor="mm-age">Minimum file age (hours)</label>
      <input
        id="mm-age"
        type="number"
        value={fileMgmt?.min_file_age_hours ?? 0}
        onChange={(e) => patchFileMgmt({ min_file_age_hours: Number(e.target.value) || 0 })}
        data-testid="mm-age"
      />
      <label className="check" htmlFor="mm-empty">
        <input
          id="mm-empty"
          type="checkbox"
          checked={fileMgmt?.remove_empty_dirs ?? false}
          onChange={(e) => patchFileMgmt({ remove_empty_dirs: e.target.checked })}
          data-testid="mm-empty"
        />
        Remove empty directories
      </label>
      <label className="check" htmlFor="mm-extras">
        <input
          id="mm-extras"
          type="checkbox"
          checked={fileMgmt?.import_extra_files ?? false}
          onChange={(e) => patchFileMgmt({ import_extra_files: e.target.checked })}
          data-testid="mm-extras"
        />
        Import extra files (subtitles, images)
      </label>
      <label className="check" htmlFor="mm-rename">
        <input
          id="mm-rename"
          type="checkbox"
          checked={fileMgmt?.rename_series ?? false}
          onChange={(e) => patchFileMgmt({ rename_series: e.target.checked })}
          data-testid="mm-rename"
        />
        Rename series on import
      </label>
      <label className="check" htmlFor="mm-unmonitor">
        <input
          id="mm-unmonitor"
          type="checkbox"
          checked={fileMgmt?.unmonitor_deleted_movies ?? true}
          onChange={(e) => patchFileMgmt({ unmonitor_deleted_movies: e.target.checked })}
          data-testid="mm-unmonitor"
        />
        Unmonitor Deleted Movies
      </label>

      <h4>Remote path mappings</h4>
      <table className="table" data-testid="mm-mappings">
        <thead>
          <tr>
            <th>Source (indexer / download path)</th>
            <th>Destination (local)</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          {mappings.map((m, i) => (
            <tr key={i}>
              <td>{m.source}</td>
              <td>{m.destination}</td>
              <td>
                <button onClick={() => removeMapping(i)} data-testid={`mm-map-del-${i}`}>
                  Remove
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      <div className="row">
        <input
          placeholder="Source path"
          value={mapSource}
          onChange={(e) => setMapSource(e.target.value)}
          data-testid="mm-map-source"
        />
        <input
          placeholder="Destination path"
          value={mapDest}
          onChange={(e) => setMapDest(e.target.value)}
          data-testid="mm-map-dest"
        />
        <button onClick={addMapping} disabled={!mapSource.trim()} data-testid="mm-map-add">
          Add mapping
        </button>
      </div>

      <h4>Root folders</h4>
      <p className="dim small">
        Paths are managed by the add flow (the P2 root-folders domain); the per-root default music
        quality + metadata profile picks write straight to the root-folders registry.
      </p>
      <table className="table" data-testid="mm-roots">
        <thead>
          <tr>
            <th>Path</th>
            <th>Name</th>
            <th>Default Quality Profile (music)</th>
            <th>Default Metadata Profile</th>
          </tr>
        </thead>
        <tbody>
          {roots.map((r) => (
            <tr key={r.ID}>
              <td>{r.Path}</td>
              <td>{r.Name}</td>
              <td>
                <select
                  value={r.DefaultQualityProfile ?? ''}
                  onChange={(e) => applyRootProfile(r, 'default_quality_profile', e.target.value)}
                  data-testid={`mm-root-quality-${r.ID}`}
                >
                  <option value="">(none)</option>
                  {musicProfileNames.map((n) => (
                    <option key={n} value={n}>
                      {n}
                    </option>
                  ))}
                </select>
              </td>
              <td>
                <select
                  value={r.DefaultMetadataProfile ?? ''}
                  onChange={(e) => applyRootProfile(r, 'default_metadata_profile', e.target.value)}
                  data-testid={`mm-root-metadata-${r.ID}`}
                >
                  <option value="">(none)</option>
                  {metadataProfileNames.map((n) => (
                    <option key={n} value={n}>
                      {n}
                    </option>
                  ))}
                </select>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      {saved && (
        <p className="pill pill-ok" data-testid="media-management-saved">
          Saved
        </p>
      )}
      <button
        onClick={handleSave}
        disabled={busy || loading || !naming || !fileMgmt || !movieNaming}
        data-testid="media-management-save"
      >
        {busy ? 'Saving…' : 'Save media management'}
      </button>
    </SectionCard>
  )
}
