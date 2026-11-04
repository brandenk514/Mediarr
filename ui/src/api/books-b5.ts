// Readarr B5 — wire types for the books settings surface.
//
// SPEC DIVERGENCE (reported, do not "fix" by regenerating from openapi.yaml):
// openapi.yaml documents three of these schemas in a different shape than the
// shipped backend actually serves/accepts:
//
//   BookNaming          spec: standard_format / standard_template /
//                       standard_replace / audiobook_format / audiobook_template /
//                       audiobook_replace (literal Readarr names).
//                       backend (internal/settings/books_settings.go):
//                       rename_books, replace_illegal_characters,
//                       colon_replacement, standard_format, author_format,
//                       space_handling, case_handling.
//
//   BookFileManagement  spec: seven *_extension fields.
//                       backend: the 20-field Readarr mirror
//                       (unmonitor_downloaded … extra_file_extensions).
//
//   BookImportList      spec: list_id is a string, per-list `exclusions`,
//                       no `enabled`.
//                       backend: list_id is an int, `enabled` is present,
//                       exclusions are document-level only.
//
// The shapes below match the backend EXACTLY (verified against the Go structs
// and the B5 API tests: internal/settings/books_settings.go,
// internal/settings/books_importlists.go, internal/api/rootfolders_b5_test.go).
// They are what the server decodes on PUT and returns on GET, so the UI must
// send these — a spec-shaped document is rejected (e.g. missing
// author_format fails book_naming validation). CalibreSettings,
// BookTagWrite, the RootFolder additions (Calibre/HardlinksDisabled) and the
// calibre_enabled flag DO match the spec and use the generated types.
// Delete this file once openapi.yaml is corrected and types are regenerated.
import type { components } from './types.gen'

// The shared (spec-accurate) media-management sub-blocks.
type NamingTemplate = components['schemas']['NamingTemplate']
type MovieNaming = components['schemas']['MovieNaming']
type MusicNaming = components['schemas']['MusicNaming']
type FileManagement = components['schemas']['FileManagement']
type RemotePathMapping = components['schemas']['RemotePathMapping']

// ---------------------------------------------------------------------------
// Media management (books) — naming block
// ---------------------------------------------------------------------------

export type BookSpaceHandling = 'space' | 'period' | 'underscore' | 'dash'
export type BookCaseHandling = 'default' | 'uppercase' | 'lowercase'

export const BOOK_SPACE_HANDLINGS: BookSpaceHandling[] = ['space', 'period', 'underscore', 'dash']
export const BOOK_CASE_HANDLINGS: BookCaseHandling[] = ['default', 'uppercase', 'lowercase']

export interface BooksBookNaming {
  /** Rename book files to the standard template; false keeps release names. */
  rename_books: boolean
  /** Replace characters the filesystem rejects. */
  replace_illegal_characters: boolean
  /** Single character (or empty) used for ':' in titles. */
  colon_replacement: string
  /** Book file-naming template (brace tokens), e.g. "{Author Name} - {Book Title} ({Release Year})". */
  standard_format: string
  /** Author-folder template (brace tokens), e.g. "{Author Name}". */
  author_format: string
  /** How whitespace in rendered names is handled. */
  space_handling: BookSpaceHandling
  /** Case transform applied to rendered names. */
  case_handling: BookCaseHandling
}

// The server's factory defaults (settings.DefaultBookNaming).
export const DEFAULT_BOOK_NAMING: BooksBookNaming = {
  rename_books: true,
  replace_illegal_characters: true,
  colon_replacement: '',
  standard_format: '{Author Name} - {Book Title} ({Release Year})',
  author_format: '{Author Name}',
  space_handling: 'space',
  case_handling: 'default',
}

// ---------------------------------------------------------------------------
// Media management (books) — file-management block
// ---------------------------------------------------------------------------

export const BOOK_PROPER_REPACKS = ['ignore', 'include', 'upgrade'] as const
export const BOOK_FILE_DATES = ['original', 'release'] as const
export const BOOK_RESCANS = ['no', 'changed', 'all'] as const
export const BOOK_FINGERPRINTS = ['no', 'yes'] as const

export type BookProperRepacks = (typeof BOOK_PROPER_REPACKS)[number]
export type BookFileDate = (typeof BOOK_FILE_DATES)[number]
export type BookRescan = (typeof BOOK_RESCANS)[number]
export type BookFingerprint = (typeof BOOK_FINGERPRINTS)[number]

export interface BooksBookFileManagement {
  /** Stop monitoring a book once a file for it has been imported. */
  unmonitor_downloaded: boolean
  /** Ignore book files that are deleted on disk. */
  ignore_deleted_files: boolean
  /** Propers/repacks handling: ignore | include | upgrade. */
  download_propers_and_repacks: BookProperRepacks
  /** Create an author's folder even when it has no imported books yet. */
  create_empty_author_folders: boolean
  /** Remove author folders that end up empty. */
  delete_empty_folders: boolean
  /** File mtime on import: original | release date. */
  file_date: BookFileDate
  /** Rescan a root folder for changes. */
  watch_library_for_changes: boolean
  /** Rescan after refresh: no | changed | all. */
  rescan_after_refresh: BookRescan
  /** Write the file-fingerprint tag: no | yes. */
  allow_fingerprinting: BookFingerprint
  /** Recycle-bin path for deleted files ("" = direct delete). */
  recycle_bin: string
  /** How long recycle-bin items stay before purge (days). */
  recycle_bin_cleanup_days: number
  /** Apply the Linux permissions on import. */
  set_permissions_linux: boolean
  /** chmod mode for imported folders. */
  chmod_folder: string
  /** chown user for imported files. */
  chown_user: string
  /** chown group for imported files. */
  chown_group: string
  /** Skip the destination free-space check when importing. */
  skip_free_space_check: boolean
  /** Minimum free space (GB) required at the destination. */
  min_free_space_gb: number
  /** Hard-link instead of copy when possible (auto-off per root with Calibre). */
  copy_using_hardlinks: boolean
  /** Copy extra (non-book) files on import. */
  import_extra_files: boolean
  /** Allowed extra-file extensions ("" = all). */
  extra_file_extensions: string
}

// The server's factory defaults (settings.DefaultBookManagement).
export const DEFAULT_BOOK_FILE_MANAGEMENT: BooksBookFileManagement = {
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

// The full media-management document as the B5 backend serves it: the shared
// (spec-accurate) blocks plus the divergent books blocks above.
export interface BooksMediaManagementSettings {
  naming: NamingTemplate
  movie_naming: MovieNaming
  music_naming?: MusicNaming
  book_naming: BooksBookNaming
  book_management: BooksBookFileManagement
  /** Global Calibre feature flag (off by default); per-root config lives on the roots. */
  calibre_enabled: boolean
  file_management: FileManagement
  remote_path_mappings: RemotePathMapping[]
}

// ---------------------------------------------------------------------------
// Books import lists (goodreads / lazylibrarian / readarr)
// ---------------------------------------------------------------------------

export type BookImportListKind = 'goodreads' | 'lazylibrarian' | 'readarr'
export const BOOK_IMPORT_LIST_KINDS: BookImportListKind[] = ['goodreads', 'lazylibrarian', 'readarr']

export interface BooksBookImportList {
  id: number
  name: string
  kind: BookImportListKind
  /** Whether this list is enabled (backend field; absent from the spec). */
  enabled: boolean
  /** Goodreads site host (goodreads), or the http(s) instance URL (lazylibrarian / readarr). */
  base_url: string
  /** Goodreads list id (goodreads only; the backend type is an int). */
  list_id: number
  /** lazylibrarian / readarr API key; masked on read, empty string when unset. */
  api_key: string
  /** True when an API key is stored for this list. */
  api_key_set: boolean
  /** Readarr-instance filter: profile ids (readarr kind only; empty = all). */
  profile_ids: number[]
  /** Readarr-instance filter: tag ids (readarr kind only; empty = all). */
  tag_ids: number[]
  /** Readarr-instance filter: root-folder paths (readarr kind only; empty = all). */
  root_folder_paths: string[]
}

export interface BooksBookImportListExclusion {
  /** The source's book foreign id to skip. */
  foreign_id: string
  /** The book title/name to skip. */
  name: string
}

export interface BooksBookImportListsSettings {
  lists: BooksBookImportList[]
  /** Books no list should ever import (document-level). */
  exclusions: BooksBookImportListExclusion[]
}

// ---------------------------------------------------------------------------
// Metadata (books) — tag writing. Matches the spec; the generated
// MetadataSettings.book_tag_write is used directly by the section.
// ---------------------------------------------------------------------------

export const BOOK_TAG_TYPES = ['no', 'all', 'book'] as const
export const AUDIO_TAG_TYPES = ['no', 'all', 'audio'] as const
export type BookTagType = (typeof BOOK_TAG_TYPES)[number]
export type AudioTagType = (typeof AUDIO_TAG_TYPES)[number]

// The server's defaults (settings.DefaultBookTagWrite).
export const DEFAULT_BOOK_TAG_WRITE = { book_tag_type: 'all' as BookTagType, audio_tag_type: 'no' as AudioTagType }

// ---------------------------------------------------------------------------
// Calibre (per root folder) — output formats the backend accepts.
// (settings.CalibreConvertFormats)
// ---------------------------------------------------------------------------

export const CALIBRE_OUTPUT_FORMATS = [
  'MOBI',
  'EPUB',
  'AZW3',
  'DOCX',
  'FB2',
  'TXT',
  'RTF',
  'LIT',
  'CHM',
  'HTML',
  'AZW',
  'KFX',
  'SNB',
  'TTS',
] as const
