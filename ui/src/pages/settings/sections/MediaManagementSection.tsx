import { Fragment, useEffect, useState } from 'react'
import { client } from '../../../api/client'
import type {
  FileManagement,
  MovieNaming,
  MusicNaming,
  NamingTemplate,
  ProfilesSettings,
  RemotePathMapping,
  RootFolder,
} from '../../../api/types'
import type { components } from '../../../api/types.gen'
import { SectionCard, SectionError, useSectionDoc, useSubmit } from './shared'

// The per-root Calibre block (B5). types.ts does not re-export it, so alias
// the generated schema here (spec-accurate: the root-folder endpoints mask
// the password on read and compute HardlinksDisabled).
type CalibreSettings = components['schemas']['CalibreSettings']
// Readarr B5 books blocks, generated from openapi.yaml (the spec was
// corrected to the backend wire shapes; the earlier hand-written bridge file
// existed only to cover that divergence and is gone with it).
type MediaManagementDoc = components['schemas']['MediaManagementSettings']
type BookNamingDoc = components['schemas']['BookNaming']
type BookFileManagementDoc = components['schemas']['BookFileManagement']
type BookSpaceHandling = BookNamingDoc['space_handling']
type BookCaseHandling = BookNamingDoc['case_handling']
type BookProperRepacks = BookFileManagementDoc['download_propers_and_repacks']
type BookFileDate = BookFileManagementDoc['file_date']
type BookRescan = BookFileManagementDoc['rescan_after_refresh']
type BookFingerprint = BookFileManagementDoc['allow_fingerprinting']

const BOOK_SPACE_HANDLINGS: BookSpaceHandling[] = ['space', 'period', 'underscore', 'dash']
const BOOK_CASE_HANDLINGS: BookCaseHandling[] = ['default', 'uppercase', 'lowercase']
const BOOK_PROPER_REPACKS: BookProperRepacks[] = ['ignore', 'include', 'upgrade']
const BOOK_FILE_DATES: BookFileDate[] = ['original', 'release']
const BOOK_RESCANS: BookRescan[] = ['no', 'changed', 'all']
const BOOK_FINGERPRINTS: BookFingerprint[] = ['no', 'yes']
const CALIBRE_OUTPUT_FORMATS = [
  'MOBI', 'EPUB', 'AZW3', 'DOCX', 'FB2', 'TXT', 'RTF', 'LIT', 'CHM', 'HTML', 'AZW', 'KFX', 'SNB', 'TTS',
] as const

// The server's factory defaults (settings.DefaultBookNaming /
// settings.DefaultBookManagement) — load-boundary fallbacks for pre-B5
// documents missing the books keys.
const DEFAULT_BOOK_NAMING: BookNamingDoc = {
  rename_books: true,
  replace_illegal_characters: true,
  colon_replacement: '',
  standard_format: '{Author Name} - {Book Title} ({Release Year})',
  author_format: '{Author Name}',
  space_handling: 'space',
  case_handling: 'default',
}
const DEFAULT_BOOK_FILE_MANAGEMENT: BookFileManagementDoc = {
  unmonitor_downloaded: true,
  ignore_deleted_files: false,
  download_propers_and_repacks: 'include',
  create_empty_author_folders: true,
  delete_empty_folders: false,
  file_date: 'original',
  watch_library_for_changes: true,
  rescan_after_refresh: 'no',
  allow_fingerprinting: 'no',
  recycle_bin: '',
  recycle_bin_cleanup_days: 14,
  set_permissions_linux: false,
  chmod_folder: '',
  chown_user: '',
  chown_group: '',
  skip_free_space_check: false,
  min_free_space_gb: 0,
  copy_using_hardlinks: true,
  import_extra_files: false,
  extra_file_extensions: '',
}


// MediaManagementSection is the media-management section: naming templates
// per media kind (TV parity first), the Radarr movie-naming block (R5b:
// Rename Movies / Replace Illegal Characters / Colon Replacement / Standard
// Movie Format — separate from the per-kind 'Movie' template row, which is
// the TV-era NamingTemplate.movie), the Lidarr track-naming block (L3:
// Rename Tracks / Replace Illegal Characters / Colon Replacement / Standard
// Format / Multi-Disc Format), the Readarr book-naming + book file-management
// blocks and the global Calibre feature flag (B5; the books blocks are
// generated from the spec), the file-management
// options (including Unmonitor Deleted Movies), remote path mappings
// (indexer/download paths -> local), and the root-folder list (paths owned by
// the P2 root-folders domain and the add flow; the per-root default music
// quality + metadata profile pickers write straight to that registry).
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
  const { doc, loading, error, authLost, refresh } = useSectionDoc<MediaManagementDoc>('media-management')

  const [naming, setNaming] = useState<NamingTemplate | null>(null)
  const [movieNaming, setMovieNaming] = useState<MovieNaming | null>(null)
  // Lidarr L3: the track-naming block. A missing music_naming (pre-L3
  // documents) seeds null — the block renders its fallbacks and saves
  // pass the stored value through non-destructively.
  const [musicNaming, setMusicNaming] = useState<MusicNaming | null>(null)
  // Readarr B5: the books naming + file-management blocks and the global
  // Calibre feature flag. A pre-B5 document (missing keys) seeds the
  // factory defaults at the load boundary, mirroring the music_naming
  // fallback pattern.
  const [bookNaming, setBookNaming] = useState<BookNamingDoc>(DEFAULT_BOOK_NAMING)
  const [bookMgmt, setBookMgmt] = useState<BookFileManagementDoc>(DEFAULT_BOOK_FILE_MANAGEMENT)
  const [calibreEnabled, setCalibreEnabled] = useState(false)
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
  // Readarr B5: the per-root Calibre editor. Only one root's editor is open
  // at a time; the draft is a copy of the stored block (password blanked —
  // it is write-only and the server masks it on read) or the factory default
  // when the root has no Calibre config yet.
  const [calibreEditId, setCalibreEditId] = useState<number | null>(null)
  const [calibreDraft, setCalibreDraft] = useState<CalibreSettings | null>(null)
  const [calibreTags, setCalibreTags] = useState('')
  const [calibreError, setCalibreError] = useState('')
  // Readarr B5: the seeded book profile names for the Calibre per-root
  // quality / metadata profile selects.
  const [bookQualityProfileNames, setBookQualityProfileNames] = useState<string[]>([])
  const [bookMetadataProfileNames, setBookMetadataProfileNames] = useState<string[]>([])
  const [saved, setSaved] = useState(false)
  const { busy, submit } = useSubmit()

  useEffect(() => {
    if (!doc) return
    setNaming(doc.naming)
    setMovieNaming(doc.movie_naming)
    setMusicNaming(doc.music_naming ?? null)
    // Readarr B5: seed the books blocks from the loaded document, falling
    // back to the factory defaults for documents that predate the fields.
    setBookNaming(doc.book_naming ?? DEFAULT_BOOK_NAMING)
    setBookMgmt(doc.book_management ?? DEFAULT_BOOK_FILE_MANAGEMENT)
    setCalibreEnabled(doc.calibre_enabled ?? false)
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
        if (cancelled) return
        setMetadataProfileNames((p.metadata_profiles ?? []).map((m) => m.name))
        // Readarr B5: the seeded book metadata profile names for the
        // per-root Calibre picks.
        setBookMetadataProfileNames((p.book_metadata_profiles ?? []).map((m) => m.name))
      })
      .catch(() => {
        if (!cancelled) {
          setMetadataProfileNames([])
          setBookMetadataProfileNames([])
        }
      })
    // Readarr B5: the seeded book quality profile names for the per-root
    // Calibre pick.
    client
      .getProfiles('books')
      .then((r) => {
        if (!cancelled) setBookQualityProfileNames(r.profiles)
      })
      .catch(() => {
        if (!cancelled) setBookQualityProfileNames([])
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

  // Readarr B5: the books naming / file-management patches (the blocks are
  // always non-null — they seed the factory defaults at the load boundary).
  function patchBookNaming(patch: Partial<BookNamingDoc>) {
    setBookNaming({ ...bookNaming, ...patch })
  }

  function patchBookMgmt(patch: Partial<BookFileManagementDoc>) {
    setBookMgmt({ ...bookMgmt, ...patch })
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

  // Readarr B5: open (or close) the per-root Calibre editor. The draft is a
  // copy of the stored block with the password blanked — the key is
  // write-only (masked on read), so a blank password on save keeps the
  // stored key and a typed value replaces it.
  function openCalibre(r: RootFolder) {
    if (calibreEditId === r.ID) {
      setCalibreEditId(null)
      setCalibreDraft(null)
      return
    }
    const base: CalibreSettings =
      r.Calibre ??
      ({
        enabled: false,
        host: '',
        port: 0,
        url_base: '',
        username: '',
        password: '',
        password_set: false,
        library: '',
        use_ssl: false,
        output_format: '',
        output_profile: 0,
        monitor: false,
        quality_profile: '',
        metadata_profile: '',
        default_tags: [],
      } as CalibreSettings)
    setCalibreDraft({ ...base, password: '' })
    setCalibreTags((base.default_tags ?? []).join(', '))
    setCalibreError('')
    setCalibreEditId(r.ID)
  }

  function patchCalibre(patch: Partial<CalibreSettings>) {
    setCalibreDraft((d) => (d ? { ...d, ...patch } : d))
  }

  // Readarr B5: save the per-root Calibre block to the registry (per-change
  // write, like the L3 profile picks). The enabled-state mirrors the
  // backend rules: host/port/username are required when enabled.
  function saveCalibre() {
    if (calibreEditId === null || !calibreDraft) return
    if (calibreDraft.enabled) {
      if (calibreDraft.host.trim() === '') {
        setCalibreError('host is required when Calibre is enabled')
        return
      }
      if (calibreDraft.port < 1 || calibreDraft.port > 65535) {
        setCalibreError('port must be between 1 and 65535')
        return
      }
      if (calibreDraft.username.trim() === '') {
        setCalibreError('username is required when Calibre is enabled')
        return
      }
    }
    const tags = [
      ...new Set(
        calibreTags
          .split(',')
          .map((s) => s.trim())
          .filter((s) => s !== '')
          .map(Number)
          .filter((n) => Number.isInteger(n)),
      ),
    ]
    const block: CalibreSettings = {
      ...calibreDraft,
      host: calibreDraft.host.trim(),
      username: calibreDraft.username.trim(),
      default_tags: tags,
    }
    void client
      .updateRootFolder(calibreEditId, { calibre: block })
      .then((updated) => {
        setRoots((prev) => prev.map((x) => (x.ID === calibreEditId ? updated : x)))
        setCalibreEditId(null)
        setCalibreDraft(null)
      })
      .catch((e: unknown) => {
        setCalibreError(e instanceof Error ? e.message : 'failed to save Calibre config')
      })
  }

  function handleSave() {
    if (!doc) return
    if (!naming || !fileMgmt || !movieNaming) return
    void submit(async () => {
      const next: MediaManagementDoc = {
        naming,
        movie_naming: movieNaming,
        // Lidarr L3: send the local block when present; a pre-L3 doc (no
        // music_naming) passes its stored value through non-destructively.
        music_naming: musicNaming ?? doc.music_naming,
        // Readarr B5: the books blocks round-trip from the local state
        // (seeded from the document at the load boundary).
        book_naming: bookNaming,
        book_management: bookMgmt,
        calibre_enabled: calibreEnabled,
        file_management: fileMgmt,
        remote_path_mappings: mappings,
      }
      await client.putSettingsSection<MediaManagementDoc>('media-management', next)
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

      {/* Readarr B5: the books naming block (BookNaming from the spec). */}
      <h4>Book naming</h4>
      <div data-testid="mm-book-naming">
        <label className="check" htmlFor="mm-book-rename">
          <input
            id="mm-book-rename"
            type="checkbox"
            checked={bookNaming.rename_books}
            onChange={(e) => patchBookNaming({ rename_books: e.target.checked })}
            data-testid="mm-book-rename"
          />
          Rename Books
        </label>
        <label className="check" htmlFor="mm-book-illegal">
          <input
            id="mm-book-illegal"
            type="checkbox"
            checked={bookNaming.replace_illegal_characters}
            onChange={(e) => patchBookNaming({ replace_illegal_characters: e.target.checked })}
            data-testid="mm-book-illegal"
          />
          Replace Illegal Characters
        </label>
        <label htmlFor="mm-book-colon">Colon Replacement</label>
        <input
          id="mm-book-colon"
          value={bookNaming.colon_replacement}
          maxLength={1}
          placeholder="e.g. '"
          onChange={(e) => patchBookNaming({ colon_replacement: e.target.value.slice(-1) })}
          data-testid="mm-book-colon"
        />
        <label htmlFor="mm-book-format">Standard Format</label>
        <input
          id="mm-book-format"
          value={bookNaming.standard_format}
          onChange={(e) => patchBookNaming({ standard_format: e.target.value })}
          data-testid="mm-book-format"
        />
        <label htmlFor="mm-book-author">Author Format</label>
        <input
          id="mm-book-author"
          value={bookNaming.author_format}
          onChange={(e) => patchBookNaming({ author_format: e.target.value })}
          data-testid="mm-book-author"
        />
        <label htmlFor="mm-book-space">Space Handling</label>
        <select
          id="mm-book-space"
          value={bookNaming.space_handling}
          onChange={(e) => patchBookNaming({ space_handling: e.target.value as BookSpaceHandling })}
          data-testid="mm-book-space"
        >
          {BOOK_SPACE_HANDLINGS.map((s) => (
            <option key={s} value={s}>
              {s}
            </option>
          ))}
        </select>
        <label htmlFor="mm-book-case">Case Handling</label>
        <select
          id="mm-book-case"
          value={bookNaming.case_handling}
          onChange={(e) => patchBookNaming({ case_handling: e.target.value as BookCaseHandling })}
          data-testid="mm-book-case"
        >
          {BOOK_CASE_HANDLINGS.map((s) => (
            <option key={s} value={s}>
              {s}
            </option>
          ))}
        </select>
      </div>

      {/* Readarr B5: the books file-management block (the backend's
          BookFileManagement — 20 fields, divergent from the spec). */}
      <h4>Book file management</h4>
      <div data-testid="mm-book-management">
        <label className="check" htmlFor="mm-book-unmonitor">
          <input
            id="mm-book-unmonitor"
            type="checkbox"
            checked={bookMgmt.unmonitor_downloaded}
            onChange={(e) => patchBookMgmt({ unmonitor_downloaded: e.target.checked })}
            data-testid="mm-book-unmonitor"
          />
          Unmonitor Downloaded Books
        </label>
        <label className="check" htmlFor="mm-book-ignore-deleted">
          <input
            id="mm-book-ignore-deleted"
            type="checkbox"
            checked={bookMgmt.ignore_deleted_files}
            onChange={(e) => patchBookMgmt({ ignore_deleted_files: e.target.checked })}
            data-testid="mm-book-ignore-deleted"
          />
          Ignore Deleted Files
        </label>
        <label htmlFor="mm-book-propers">Download Propers and Repacks</label>
        <select
          id="mm-book-propers"
          value={bookMgmt.download_propers_and_repacks}
          onChange={(e) =>
            patchBookMgmt({ download_propers_and_repacks: e.target.value as BookProperRepacks })
          }
          data-testid="mm-book-propers"
        >
          {BOOK_PROPER_REPACKS.map((s) => (
            <option key={s} value={s}>
              {s}
            </option>
          ))}
        </select>
        <label className="check" htmlFor="mm-book-create-empty">
          <input
            id="mm-book-create-empty"
            type="checkbox"
            checked={bookMgmt.create_empty_author_folders}
            onChange={(e) => patchBookMgmt({ create_empty_author_folders: e.target.checked })}
            data-testid="mm-book-create-empty"
          />
          Create Empty Author Folders
        </label>
        <label className="check" htmlFor="mm-book-delete-empty">
          <input
            id="mm-book-delete-empty"
            type="checkbox"
            checked={bookMgmt.delete_empty_folders}
            onChange={(e) => patchBookMgmt({ delete_empty_folders: e.target.checked })}
            data-testid="mm-book-delete-empty"
          />
          Delete Empty Folders
        </label>
        <label htmlFor="mm-book-file-date">File Date</label>
        <select
          id="mm-book-file-date"
          value={bookMgmt.file_date}
          onChange={(e) => patchBookMgmt({ file_date: e.target.value as BookFileDate })}
          data-testid="mm-book-file-date"
        >
          {BOOK_FILE_DATES.map((s) => (
            <option key={s} value={s}>
              {s}
            </option>
          ))}
        </select>
        <label className="check" htmlFor="mm-book-watch">
          <input
            id="mm-book-watch"
            type="checkbox"
            checked={bookMgmt.watch_library_for_changes}
            onChange={(e) => patchBookMgmt({ watch_library_for_changes: e.target.checked })}
            data-testid="mm-book-watch"
          />
          Watch Library for Changes
        </label>
        <label htmlFor="mm-book-rescan">Rescan After Refresh</label>
        <select
          id="mm-book-rescan"
          value={bookMgmt.rescan_after_refresh}
          onChange={(e) => patchBookMgmt({ rescan_after_refresh: e.target.value as BookRescan })}
          data-testid="mm-book-rescan"
        >
          {BOOK_RESCANS.map((s) => (
            <option key={s} value={s}>
              {s}
            </option>
          ))}
        </select>
        <label htmlFor="mm-book-fingerprint">Allow Fingerprinting</label>
        <select
          id="mm-book-fingerprint"
          value={bookMgmt.allow_fingerprinting}
          onChange={(e) =>
            patchBookMgmt({ allow_fingerprinting: e.target.value as BookFingerprint })
          }
          data-testid="mm-book-fingerprint"
        >
          {BOOK_FINGERPRINTS.map((s) => (
            <option key={s} value={s}>
              {s}
            </option>
          ))}
        </select>
        <label htmlFor="mm-book-recycle">Recycle Bin</label>
        <input
          id="mm-book-recycle"
          value={bookMgmt.recycle_bin}
          placeholder="/tmp/recycle"
          onChange={(e) => patchBookMgmt({ recycle_bin: e.target.value })}
          data-testid="mm-book-recycle"
        />
        <label htmlFor="mm-book-recycle-days">Recycle Bin Cleanup Days</label>
        <input
          id="mm-book-recycle-days"
          type="number"
          value={bookMgmt.recycle_bin_cleanup_days}
          onChange={(e) => patchBookMgmt({ recycle_bin_cleanup_days: Number(e.target.value) || 0 })}
          data-testid="mm-book-recycle-days"
        />
        <label className="check" htmlFor="mm-book-perm-linux">
          <input
            id="mm-book-perm-linux"
            type="checkbox"
            checked={bookMgmt.set_permissions_linux}
            onChange={(e) => patchBookMgmt({ set_permissions_linux: e.target.checked })}
            data-testid="mm-book-perm-linux"
          />
          Set Permissions (Linux)
        </label>
        <label htmlFor="mm-book-chmod">chmod Folder</label>
        <input
          id="mm-book-chmod"
          value={bookMgmt.chmod_folder}
          placeholder="755"
          onChange={(e) => patchBookMgmt({ chmod_folder: e.target.value })}
          data-testid="mm-book-chmod"
        />
        <label htmlFor="mm-book-chown-user">chown User</label>
        <input
          id="mm-book-chown-user"
          value={bookMgmt.chown_user}
          onChange={(e) => patchBookMgmt({ chown_user: e.target.value })}
          data-testid="mm-book-chown-user"
        />
        <label htmlFor="mm-book-chown-group">chown Group</label>
        <input
          id="mm-book-chown-group"
          value={bookMgmt.chown_group}
          onChange={(e) => patchBookMgmt({ chown_group: e.target.value })}
          data-testid="mm-book-chown-group"
        />
        <label className="check" htmlFor="mm-book-skip-freespace">
          <input
            id="mm-book-skip-freespace"
            type="checkbox"
            checked={bookMgmt.skip_free_space_check}
            onChange={(e) => patchBookMgmt({ skip_free_space_check: e.target.checked })}
            data-testid="mm-book-skip-freespace"
          />
          Skip Free Space Check When Importing
        </label>
        <label htmlFor="mm-book-minfree">Minimum Free Space (GB)</label>
        <input
          id="mm-book-minfree"
          type="number"
          value={bookMgmt.min_free_space_gb}
          onChange={(e) => patchBookMgmt({ min_free_space_gb: Number(e.target.value) || 0 })}
          data-testid="mm-book-minfree"
        />
        <label className="check" htmlFor="mm-book-hardlinks">
          <input
            id="mm-book-hardlinks"
            type="checkbox"
            checked={bookMgmt.copy_using_hardlinks}
            onChange={(e) => patchBookMgmt({ copy_using_hardlinks: e.target.checked })}
            data-testid="mm-book-hardlinks"
          />
          Copy Using Hardlinks
        </label>
        <label className="check" htmlFor="mm-book-extras">
          <input
            id="mm-book-extras"
            type="checkbox"
            checked={bookMgmt.import_extra_files}
            onChange={(e) => patchBookMgmt({ import_extra_files: e.target.checked })}
            data-testid="mm-book-extras"
          />
          Import Extra Files
        </label>
        <label htmlFor="mm-book-extra-exts">Extra File Extensions</label>
        <input
          id="mm-book-extra-exts"
          value={bookMgmt.extra_file_extensions}
          placeholder="txt, html"
          onChange={(e) => patchBookMgmt({ extra_file_extensions: e.target.value })}
          data-testid="mm-book-extra-exts"
        />
      </div>

      {/* Readarr B5: the global Calibre feature flag. The per-root Content
          Server config lives on the root folders below. */}
      <h4>Calibre</h4>
      <div data-testid="mm-calibre">
        <label className="check" htmlFor="mm-calibre-enabled">
          <input
            id="mm-calibre-enabled"
            type="checkbox"
            checked={calibreEnabled}
            onChange={(e) => setCalibreEnabled(e.target.checked)}
            data-testid="mm-calibre-enabled"
          />
          Enable Calibre (per-root Content Server config is on the root folders)
        </label>
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
            <th>Calibre</th>
          </tr>
        </thead>
        <tbody>
          {roots.map((r) => (
            <Fragment key={r.ID}>
              <tr>
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
                <td data-testid={`rfc-cell-${r.ID}`}>
                  {/* Readarr B5: an enabled per-root Calibre forces hard
                      links OFF for this root (the server computes it). */}
                  {r.HardlinksDisabled && (
                    <span className="pill" data-testid={`rfc-hardlinks-${r.ID}`}>
                      hardlinks off (Calibre)
                    </span>
                  )}{' '}
                  <button
                    className="ghost"
                    onClick={() => openCalibre(r)}
                    data-testid={`rfc-toggle-${r.ID}`}
                  >
                    {calibreEditId === r.ID ? 'Hide Calibre' : 'Calibre…'}
                  </button>
                </td>
              </tr>
              {calibreEditId === r.ID && calibreDraft && (
                <tr data-testid="rfc-row">
                  <td colSpan={5}>
                    <div className="subform" data-testid="rfc-form">
                      <label className="check" htmlFor="rfc-enabled">
                        <input
                          id="rfc-enabled"
                          type="checkbox"
                          checked={calibreDraft.enabled}
                          onChange={(e) => patchCalibre({ enabled: e.target.checked })}
                          data-testid="rfc-enabled"
                        />
                        Use Calibre (Content Server)
                      </label>
                      <label htmlFor="rfc-host">Host</label>
                      <input
                        id="rfc-host"
                        value={calibreDraft.host}
                        onChange={(e) => patchCalibre({ host: e.target.value })}
                        data-testid="rfc-host"
                      />
                      <label htmlFor="rfc-port">Port</label>
                      <input
                        id="rfc-port"
                        type="number"
                        value={calibreDraft.port}
                        onChange={(e) => patchCalibre({ port: Number(e.target.value) || 0 })}
                        data-testid="rfc-port"
                      />
                      <label htmlFor="rfc-urlbase">URL base</label>
                      <input
                        id="rfc-urlbase"
                        value={calibreDraft.url_base}
                        onChange={(e) => patchCalibre({ url_base: e.target.value })}
                        placeholder="/calibre"
                        data-testid="rfc-urlbase"
                      />
                      <label htmlFor="rfc-user">Username</label>
                      <input
                        id="rfc-user"
                        value={calibreDraft.username}
                        onChange={(e) => patchCalibre({ username: e.target.value })}
                        data-testid="rfc-user"
                      />
                      <label htmlFor="rfc-pass">
                        Password{' '}
                        {calibreDraft.password_set && '(stored — blank to keep)'}
                      </label>
                      <input
                        id="rfc-pass"
                        type="password"
                        value={calibreDraft.password}
                        onChange={(e) => patchCalibre({ password: e.target.value })}
                        data-testid="rfc-pass"
                      />
                      <label htmlFor="rfc-library">Library name</label>
                      <input
                        id="rfc-library"
                        value={calibreDraft.library}
                        onChange={(e) => patchCalibre({ library: e.target.value })}
                        data-testid="rfc-library"
                      />
                      <label className="check" htmlFor="rfc-ssl">
                        <input
                          id="rfc-ssl"
                          type="checkbox"
                          checked={calibreDraft.use_ssl}
                          onChange={(e) => patchCalibre({ use_ssl: e.target.checked })}
                          data-testid="rfc-ssl"
                        />
                        Use SSL
                      </label>
                      <label className="check" htmlFor="rfc-monitor">
                        <input
                          id="rfc-monitor"
                          type="checkbox"
                          checked={calibreDraft.monitor}
                          onChange={(e) => patchCalibre({ monitor: e.target.checked })}
                          data-testid="rfc-monitor"
                        />
                        Auto-import into Calibre on import
                      </label>
                      <label htmlFor="rfc-output-format">Convert to (comma-separated formats)</label>
                      <input
                        id="rfc-output-format"
                        list="rfc-formats"
                        value={calibreDraft.output_format}
                        onChange={(e) => patchCalibre({ output_format: e.target.value })}
                        placeholder="MOBI, EPUB"
                        data-testid="rfc-output-format"
                      />
                      <datalist id="rfc-formats">
                        {CALIBRE_OUTPUT_FORMATS.map((f) => (
                          <option key={f} value={f} />
                        ))}
                      </datalist>
                      <label htmlFor="rfc-output-profile">Output profile (0 = default)</label>
                      <input
                        id="rfc-output-profile"
                        type="number"
                        value={calibreDraft.output_profile}
                        onChange={(e) =>
                          patchCalibre({ output_profile: Number(e.target.value) || 0 })
                        }
                        data-testid="rfc-output-profile"
                      />
                      <label htmlFor="rfc-quality">Quality profile (this root's books)</label>
                      <select
                        id="rfc-quality"
                        value={calibreDraft.quality_profile}
                        onChange={(e) => patchCalibre({ quality_profile: e.target.value })}
                        data-testid="rfc-quality"
                      >
                        <option value="">(app default)</option>
                        {bookQualityProfileNames.map((n) => (
                          <option key={n} value={n}>
                            {n}
                          </option>
                        ))}
                      </select>
                      <label htmlFor="rfc-metadata">Metadata profile (this root's books)</label>
                      <select
                        id="rfc-metadata"
                        value={calibreDraft.metadata_profile}
                        onChange={(e) => patchCalibre({ metadata_profile: e.target.value })}
                        data-testid="rfc-metadata"
                      >
                        <option value="">(app default)</option>
                        {bookMetadataProfileNames.map((n) => (
                          <option key={n} value={n}>
                            {n}
                          </option>
                        ))}
                      </select>
                      <label htmlFor="rfc-tags">Default tag ids (comma-separated)</label>
                      <input
                        id="rfc-tags"
                        value={calibreTags}
                        onChange={(e) => setCalibreTags(e.target.value)}
                        placeholder="1, 2"
                        data-testid="rfc-tags"
                      />
                      {calibreError && (
                        <p className="pill pill-bad" data-testid="rfc-error">
                          {calibreError}
                        </p>
                      )}
                      <div>
                        <button onClick={saveCalibre} data-testid="rfc-save">
                          Save Calibre
                        </button>{' '}
                        <button
                          className="ghost"
                          onClick={() => {
                            setCalibreEditId(null)
                            setCalibreDraft(null)
                          }}
                          data-testid="rfc-cancel"
                        >
                          Cancel
                        </button>
                      </div>
                    </div>
                  </td>
                </tr>
              )}
            </Fragment>
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
