// Typed client for the mediarr v1 REST API (issue #39).
//
// Auth: bearer token, held in memory (see store.ts) — never in browser storage.
// Errors: non-2xx responses throw ApiError with status + message, so the UI can
// branch on 401 (expired/invalid token) vs. other failures.

import { clearToken, getToken, setToken } from './store'
import type {
  AddAlbumRequest,
  AddArtistRequest,
  AddAuthorRequest,
  AddEditionRequest,
  AddEpisodeRequest,
  AddMovieRequest,
  AddSeriesRequest,
  AddTitleRequest,
  AddTrackRequest,
  AddedId,
  Album,
  AlbumWanted,
  Artist,
  ArtistSearchResult,
  Author,
  Dashboard,
  DownloadClientRequest,
  DownloadClientView,
  Edition,
  Episode,
  EventFrame,
  Indexer,
  IndexerCreate,
  IndexerDefinition,
  IndexerStats,
  IndexerTestResult,
  IndexerUpdate,
  StatsSummary,
  SearchRequest,
  SearchRelease,
  SearchHistory,
  HistoryCleared,
  GrabRequest,
  GrabResult,
  LibraryOverride,
  LibraryOverrides,
  ListQuery,
  LoginRequest,
  LoginResponse,
  Me,
  MonitoredResult,
  Movie,
  MovieStats,
  MediaKind,
  MovieWanted,
  MusicHistoryEntry,
  MusicLibrarySummary,
  UnmappedFile,
  PageArtist,
  PageAuthor,
  PageMovie,
  PageQueueItem,
  PageSeries,
  ProfileKind,
  ProfilesResponse,
  QueueFilter,
  QueueHistoryEvent,
  ImportReport,
  ImportRequest,
  RootFolder,
  RootFolderRequest,
  SearchResult,
  Series,
  SeriesHistory,
  SeriesSearchResult,
  SeriesWanted,
  Title,
  TitleWanted,
  Track,
  TrackMonitoredResult,
  UpdateArtistRequest,
  UpdateSeriesRequest,
  GlobalHistoryItem,
  BlocklistEntry,
  BlocklistRequest,
  WantedTab,
  PageWantedItem,
  WantedEpisodeRef,
  WantedActionResult,
  ManualImportRequest,
  ManualImportReport,
  QueueGrabResult,
  QueueRemoveResult,
  CalendarEntry,
  SettingsSection,
  TagUsage,
  // Prowlarr PR4: notifications + indexer proxies
  NotificationSink,
  NotificationSinkCreate,
  NotificationSinkUpdate,
  NotificationSinkTestResult,
  IndexerProxy,
  IndexerProxyCreate,
  IndexerProxyUpdate,
  IndexerProxyTestResult,
  CustomFormatScoreRequest,
  CustomFormatScoreResult,
  ConnectTestRequest,
  ConnectTestResult,
  MetadataSourceSearchRequest,
  MetadataSourceSearchResult,
  MovieCollection,
  MovieSearchHit,
  DiscoverMovies,
  ImportRunRequest,
  ImportRunResult,
  // P6: System area
  SystemHealth,
  SystemTasks,
  SystemDisk,
  SystemAbout,
  BackupDocument,
  BackupImportRequest,
  BackupImportResult,
  SystemEvents,
  SystemUpdates,
  MigrationRequest,
  MigrationResult,
  MigrationStatus,
} from './types'

export class ApiError extends Error {
  status: number
  body: string

  constructor(status: number, body: string) {
    super(status === 401 ? 'authentication expired or invalid' : body || `request failed (${status})`)
    this.status = status
    this.body = body
  }
}

const API = '/api/v1'

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = { 'Content-Type': 'application/json' }
  const token = getToken()
  if (token) headers.Authorization = `Bearer ${token}`

  const res = await fetch(`${API}${path}`, {
    method,
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
  })

  const text = await res.text()
  if (!res.ok) throw new ApiError(res.status, tryParseError(text))
  if (text === '') return undefined as T
  return JSON.parse(text) as T
}

function tryParseError(text: string): string {
  try {
    const j = JSON.parse(text) as { error?: string }
    return j.error ?? text
  } catch {
    return text
  }
}

// buildListQuery serializes a ListQuery into a URL query string, omitting
// unset fields so the server applies its defaults (page 1, size 25).
function buildListQuery(q?: ListQuery): string {
  if (!q) return ''
  const params = new URLSearchParams()
  if (q.q) params.set('q', q.q)
  if (typeof q.monitored === 'boolean') params.set('monitored', String(q.monitored))
  if (q.page) params.set('page', String(q.page))
  if (q.page_size) params.set('page_size', String(q.page_size))
  const s = params.toString()
  return s ? `?${s}` : ''
}

// buildQueueQuery serializes a queue filter, omitting unset fields so the
// server applies its defaults (page 1, size 25, all kinds/states).
function buildQueueQuery(f?: QueueFilter): string {
  if (!f) return ''
  const params = new URLSearchParams()
  if (f.q) params.set('q', f.q)
  if (f.kind) params.set('kind', f.kind)
  if (f.state) params.set('state', f.state)
  if (f.page) params.set('page', String(f.page))
  if (f.page_size) params.set('page_size', String(f.page_size))
  const s = params.toString()
  return s ? `?${s}` : ''
}

export const client = {
  // ---- auth ----
  login: (req: LoginRequest) =>
    request<LoginResponse>('POST', '/auth/login', req).then((r) => {
      setToken(r.token)
      return r
    }),
  me: () => request<Me>('GET', '/auth/me'),
  logout: () => clearToken(),

  // ---- movies ----
  listMovies: (q?: ListQuery) => request<PageMovie>('GET', `/movies${buildListQuery(q)}`),
  getMovie: (id: number) => request<Movie>('GET', `/movies/${id}`),
  // null = the movie exists but has no wanted row yet (204).
  getMovieWanted: (id: number) => request<MovieWanted | null>('GET', `/movies/${id}/wanted`),
  addMovie: (req: AddMovieRequest) => request<AddedId>('POST', '/movies', req),
  setMovieMonitored: (id: number, monitored: boolean) =>
    request<MonitoredResult>('PUT', `/movies/${id}`, { monitored }),
  runMoviePipeline: (id: number) => request<{ imported: string }>('POST', `/movies/${id}/pipeline`),
  // Radarr R1: stats bar + per-edition monitoring.
  getMovieStats: () => request<MovieStats>('GET', '/movies/stats'),
  setMovieEditionMonitored: (id: number, editionId: number, monitored: boolean) =>
    request<MonitoredResult>('PUT', `/movies/${id}/editions/${editionId}`, { monitored }),
  // Radarr R3: Collections page (library grouped by TMDB collection) and the
  // Discover page (TMDB trending feed; 503 without a TMDB key).
  listMovieCollections: () => request<MovieCollection[]>('GET', '/movies/collections'),
  discoverMovies: (q: { page?: number; genre?: string; year?: number } = {}) => {
    const params = new URLSearchParams()
    if (q.page) params.set('page', String(q.page))
    if (q.genre) params.set('genre', q.genre)
    if (q.year) params.set('year', String(q.year))
    const s = params.toString()
    return request<DiscoverMovies>('GET', `/discover/movies${s ? `?${s}` : ''}`)
  },

  // ---- tv ----
  listSeries: (q?: ListQuery) => request<PageSeries>('GET', `/tv${buildListQuery(q)}`),
  getSeries: (id: number) => request<Series>('GET', `/tv/${id}`),
  addSeries: (req: AddSeriesRequest) => request<AddedId>('POST', '/tv', req),
  setSeriesMonitored: (id: number, monitored: boolean) =>
    request<MonitoredResult>('PUT', `/tv/${id}`, { monitored }),
  setSeasonMonitored: (id: number, season: number, monitored: boolean) =>
    request<MonitoredResult>('PUT', `/tv/${id}/seasons/${season}`, { monitored }),
  setEpisodeMonitored: (id: number, season: number, episode: number, monitored: boolean) =>
    request<MonitoredResult>('PUT', `/tv/${id}/episodes/${season}/${episode}`, { monitored }),
  listEpisodes: (id: number) => request<Episode[]>('GET', `/tv/${id}/episodes`),
  addEpisode: (id: number, req: AddEpisodeRequest) =>
    request<AddedId>('POST', `/tv/${id}/episodes`, req),
  runTvPipeline: (id: number) => request<{ imported: string }>('POST', `/tv/${id}/pipeline`),
  listSeriesWanted: (id: number) => request<SeriesWanted[]>('GET', `/tv/${id}/wanted`),
  // P2 parity: partial series update (quality profile, series type, season
  // folder, tags, monitored).
  updateSeries: (id: number, req: UpdateSeriesRequest) =>
    request<Series>('PATCH', `/tv/${id}`, req),
  // P2 parity: start a search for the series' missing monitored episodes.
  searchMissingEpisodes: (id: number) =>
    request<SeriesSearchResult>('POST', `/tv/${id}/episodes/search`),
  // P2 parity: series-level history (server clamps limit to 1..200, default 50).
  listSeriesHistory: (id: number, limit?: number) =>
    request<SeriesHistory>('GET', `/tv/${id}/history${limit ? `?limit=${limit}` : ''}`),
  // P2 parity: library import, phase 1 (scan -> match -> confirm).
  importLibrary: (req: ImportRequest) => request<ImportReport>('POST', '/tv/import', req),

  // ---- root folders (P2 parity) ----
  listRootFolders: () => request<RootFolder[]>('GET', '/root-folders'),
  addRootFolder: (req: RootFolderRequest) => request<AddedId>('POST', '/root-folders', req),
  updateRootFolder: (id: number, req: RootFolderRequest) =>
    request<RootFolder>('PUT', `/root-folders/${id}`, req),
  deleteRootFolder: (id: number) => request<void>('DELETE', `/root-folders/${id}`),

  // ---- music ----
  listArtists: (q?: ListQuery) => request<PageArtist>('GET', `/music/artists${buildListQuery(q)}`),
  getArtist: (id: number) => request<Artist>('GET', `/music/artists/${id}`),
  addArtist: (req: AddArtistRequest) => request<AddedId>('POST', '/music/artists', req),
  setArtistMonitored: (id: number, monitored: boolean) =>
    request<MonitoredResult>('PUT', `/music/artists/${id}`, { monitored }),
  // Lidarr L2: the full Edit Artist modal persists through the same PUT —
  // omitted fields keep their current value; a monitored change fans out to
  // the artist's known albums server-side.
  updateArtist: (id: number, req: UpdateArtistRequest) =>
    request<MonitoredResult>('PUT', `/music/artists/${id}`, req),
  listAlbums: (artistId: number) => request<Album[]>('GET', `/music/artists/${artistId}/albums`),
  getAlbum: (id: number) => request<Album>('GET', `/music/albums/${id}`),
  addAlbum: (artistId: number, req: AddAlbumRequest) =>
    request<AddedId>('POST', `/music/artists/${artistId}/albums`, req),
  setAlbumMonitored: (id: number, monitored: boolean) =>
    request<MonitoredResult>('PUT', `/music/albums/${id}`, { monitored }),
  listTracks: (albumId: number) => request<Track[]>('GET', `/music/albums/${albumId}/tracks`),
  addTrack: (albumId: number, req: AddTrackRequest) =>
    request<AddedId>('POST', `/music/albums/${albumId}/tracks`, req),
  setTrackMonitored: (albumId: number, disc: number, number: number, monitored: boolean) =>
    request<TrackMonitoredResult>('PUT', `/music/albums/${albumId}/tracks/${disc}/${number}`, {
      monitored,
    }),
  runMusicPipeline: (artistId: number) =>
    request<{ imported: string }>('POST', `/music/artists/${artistId}/pipeline`),
  listAlbumWanted: (albumId: number) => request<AlbumWanted[]>('GET', `/music/albums/${albumId}/wanted`),

  // Lidarr L1: artist history (detail toolbar) + library summary (grid
  // footer status bar).
  getArtistHistory: (id: number) => request<MusicHistoryEntry[]>('GET', `/music/artists/${id}/history`),
  getMusicLibrarySummary: () => request<MusicLibrarySummary>('GET', '/music/library/summary'),
  // Lidarr L5: the Unmapped Files page (read-only walk of the configured
  // music media root).
  listUnmappedFiles: () => request<UnmappedFile[]>('GET', '/music/unmapped'),

  // ---- books ----
  listAuthors: (q?: ListQuery) => request<PageAuthor>('GET', `/books/authors${buildListQuery(q)}`),
  getAuthor: (id: number) => request<Author>('GET', `/books/authors/${id}`),
  addAuthor: (req: AddAuthorRequest) => request<AddedId>('POST', '/books/authors', req),
  setAuthorMonitored: (id: number, monitored: boolean) =>
    request<MonitoredResult>('PUT', `/books/authors/${id}`, { monitored }),
  listTitles: (authorId: number) => request<Title[]>('GET', `/books/authors/${authorId}/titles`),
  getTitle: (id: number) => request<Title>('GET', `/books/titles/${id}`),
  addTitle: (authorId: number, req: AddTitleRequest) =>
    request<AddedId>('POST', `/books/authors/${authorId}/titles`, req),
  setTitleMonitored: (id: number, monitored: boolean) =>
    request<MonitoredResult>('PUT', `/books/titles/${id}`, { monitored }),
  listEditions: (titleId: number) => request<Edition[]>('GET', `/books/titles/${titleId}/editions`),
  getEdition: (id: number) => request<Edition>('GET', `/books/editions/${id}`),
  addEdition: (titleId: number, req: AddEditionRequest) =>
    request<AddedId>('POST', `/books/titles/${titleId}/editions`, req),
  setEditionMonitored: (id: number, monitored: boolean) =>
    request<MonitoredResult>('PUT', `/books/editions/${id}`, { monitored }),
  runBooksPipeline: (authorId: number) =>
    request<{ imported: string }>('POST', `/books/authors/${authorId}/pipeline`),
  listTitleWanted: (titleId: number) => request<TitleWanted[]>('GET', `/books/titles/${titleId}/wanted`),

  // ---- indexers ----
  listIndexers: () => request<Indexer[]>('GET', '/indexers'),
  // Prowlarr PR2: the built-in definition library (584 Prowlarr community
  // definitions). Filters are optional + combinable; the UI fetches the whole
  // list once and filters client-side (like Prowlarr).
  listIndexerDefinitions: (params?: {
    q?: string
    protocol?: 'torrent' | 'usenet'
    privacy?: 'public' | 'private' | 'semi-private'
    category?: string
  }) => {
    const p = new URLSearchParams()
    if (params?.q) p.set('q', params.q)
    if (params?.protocol) p.set('protocol', params.protocol)
    if (params?.privacy) p.set('privacy', params.privacy)
    if (params?.category) p.set('category', params.category)
    const s = p.toString()
    return request<IndexerDefinition[]>('GET', `/indexer/definitions${s ? `?${s}` : ''}`)
  },
  indexerStats: (id: number) => request<IndexerStats>('GET', `/indexers/${id}/stats`),
  testIndexer: (id: number) => request<IndexerTestResult>('POST', `/indexers/${id}/test`),
  // Prowlarr PR1 "Test All Indexers": fan-out probe over every enabled indexer.
  testAllIndexers: () => request<IndexerTestResult[]>('POST', '/indexers/test-all'),
  // Prowlarr PR3: the cross-indexer stats rollup (KPI totals + 30-day series,
  // by-indexer series) the Stats page charts client-side.
  indexerStatsSummary: () => request<StatsSummary>('GET', '/indexers/stats/summary'),
  // Prowlarr PR3: manual release search. Fans the query over the selected
  // indexers (empty/omitted indexer_ids = every enabled indexer) and returns
  // every candidate release — no cross-indexer dedup.
  searchReleases: (req: SearchRequest) => request<SearchRelease[]>('POST', '/search', req),
  // Prowlarr PR3: grab a search-result row. The server routes it to the
  // configured download client by protocol (409 when the client can't handle
  // the protocol, 503 when no client is configured).
  grabSearchRelease: (req: GrabRequest) => request<GrabResult>('POST', '/search/grab', req),
  // Prowlarr PR3: query history (server-side pagination; page 1-based,
  // page_size default 25 max 100; filters: exact indexer name + q substring).
  searchHistory: (f?: { indexer?: string; q?: string; page?: number; page_size?: number }) => {
    const params = new URLSearchParams()
    if (f?.indexer) params.set('indexer', f.indexer)
    if (f?.q) params.set('q', f.q)
    if (f?.page) params.set('page', String(f.page))
    if (f?.page_size) params.set('page_size', String(f.page_size))
    const s = params.toString()
    return request<SearchHistory>('GET', `/search/history${s ? `?${s}` : ''}`)
  },
  // Prowlarr PR3: clear all query history (the Prowlarr "Clear" action).
  clearSearchHistory: () => request<HistoryCleared>('DELETE', '/search/history'),
  // Indexer write surface (#44): create / update / enable-toggle / delete.
  // api_key is plaintext in the body but is never echoed back.
  createIndexer: (req: IndexerCreate) => request<Indexer>('POST', '/indexers', req),
  updateIndexer: (id: number, req: IndexerUpdate) => request<Indexer>('PUT', `/indexers/${id}`, req),
  setIndexerEnabled: (id: number, enabled: boolean) =>
    request<Indexer>('PATCH', `/indexers/${id}/enabled`, { enabled }),
  deleteIndexer: (id: number) => request<void>('DELETE', `/indexers/${id}`),

  // ---- dashboard (#40) ----
  // recent caps the recent-history / recent-queue feeds (server clamps 1..100).
  getDashboard: (recent?: number) =>
    request<Dashboard>('GET', `/dashboard${recent ? `?recent=${recent}` : ''}`),

  // ---- queue view (#43) ----
  // The full, filterable + paginated queue (the dedicated view's data source).
  listQueue: (f?: QueueFilter) => request<PageQueueItem>('GET', `/queue${buildQueueQuery(f)}`),
  // One parent's lifecycle history (the queue row's drill-down).
  queueHistory: (kind: MediaKind, parentId: number, limit?: number) =>
    request<QueueHistoryEvent[]>(
      'GET',
      `/queue/${kind}/${parentId}/history${limit ? `?limit=${limit}` : ''}`,
    ),
  // P3 queue actions: grab / remove a parent's jobs (Activity > Queue).
  grabQueue: (kind: MediaKind, parentId: number) =>
    request<QueueGrabResult>('POST', `/queue/${kind}/${parentId}/grab`),
  removeQueue: (kind: MediaKind, parentId: number) =>
    request<QueueRemoveResult>('DELETE', `/queue/${kind}/${parentId}`),

  // ---- P3: Activity + Wanted ----
  // Global event feed across all libraries (Activity > History).
  listGlobalHistory: (f?: { since?: string; event?: string; limit?: number }) => {
    const params = new URLSearchParams()
    if (f?.since) params.set('since', f.since)
    if (f?.event) params.set('event', f.event)
    if (f?.limit) params.set('limit', String(f.limit))
    const s = params.toString()
    return request<GlobalHistoryItem[]>('GET', `/history${s ? `?${s}` : ''}`)
  },
  // Blocklist (Activity > Blocklist).
  listBlocklist: (f?: { kind?: MediaKind; parentId?: number }) => {
    const params = new URLSearchParams()
    if (f?.kind) params.set('kind', f.kind)
    if (f?.parentId) params.set('parent_id', String(f.parentId))
    const s = params.toString()
    return request<BlocklistEntry[]>('GET', `/blocklist${s ? `?${s}` : ''}`)
  },
  addBlocklist: (req: BlocklistRequest) => request<AddedId>('POST', '/blocklist', req),
  unblockRelease: (id: number) => request<void>('DELETE', `/blocklist/${id}`),
  // Global Wanted (Missing / Cutoff Unmet) with server-side pagination.
  listWanted: (f?: { tab?: WantedTab; q?: string; page?: number; pageSize?: number }) => {
    const params = new URLSearchParams()
    if (f?.tab) params.set('tab', f.tab)
    if (f?.q) params.set('search', f.q)
    if (f?.page) params.set('page', String(f.page))
    if (f?.pageSize) params.set('pageSize', String(f.pageSize))
    const s = params.toString()
    return request<PageWantedItem>('GET', `/wanted${s ? `?${s}` : ''}`)
  },
  searchWanted: (tab: WantedTab) => request<WantedActionResult>('POST', '/wanted/search', { tab }),
  unmonitorWanted: (items: WantedEpisodeRef[]) =>
    request<WantedActionResult>('POST', '/wanted/unmonitor', { episodes: items }),
  manualImport: (req: ManualImportRequest) =>
    request<ManualImportReport>('POST', '/import', req),

  // ---- P4: Calendar (upcoming monitored air dates + iCal feed) ----
  // R4: `kind` selects the calendar source; 'tv' (default, and the
  // server-side default) is upcoming monitored air dates, 'movie' is
  // upcoming monitored release dates.
  listCalendar: (f?: { start?: string; end?: string; kind?: 'tv' | 'movie' }) => {
    const params = new URLSearchParams()
    if (f?.start) params.set('start', f.start)
    if (f?.end) params.set('end', f.end)
    if (f?.kind) params.set('kind', f.kind)
    const s = params.toString()
    return request<CalendarEntry[]>('GET', `/calendar${s ? `?${s}` : ''}`)
  },
  // iCal feed URL for a plain link / new tab: /calendar.ics carries the app
  // token as ?token= because a browser navigation can't set the Authorization
  // header, plus the optional window and kind (R4: ?kind=movie).
  calendarICS: (f?: { start?: string; end?: string; kind?: 'tv' | 'movie' }) => {
    const params = new URLSearchParams()
    if (f?.start) params.set('start', f.start)
    if (f?.end) params.set('end', f.end)
    if (f?.kind) params.set('kind', f.kind)
    const token = getToken()
    if (token) params.set('token', token)
    const s = params.toString()
    return `/calendar.ics${s ? `?${s}` : ''}`
  },

  // ---- settings (#44) ----
  // Download-client config: the read is a *masked* view (secrets are presence
  // booleans, never values); the write's empty secret means "keep existing".
  getDownloadClient: () => request<DownloadClientView>('GET', '/settings/download-client'),
  putDownloadClient: (req: DownloadClientRequest) =>
    request<DownloadClientView>('PUT', '/settings/download-client', req),
  // Per-library overrides: one document for all four libraries.
  getLibraryOverrides: () => request<LibraryOverrides>('GET', '/settings/library'),
  putLibraryOverride: (kind: MediaKind, o: LibraryOverride) =>
    request<LibraryOverride>('PUT', `/settings/library/${kind}`, o),

  // ---- settings sections (P5: the 13-section settings shell) ----
  // Generic per-section documents. The read merges stored values over the
  // section defaults (a fresh install reads complete documents); the write is
  // a full-document replace. Secrets (connect tokens, import-list API keys)
  // come back masked (empty string + *_set boolean); a blank secret in the
  // body keeps the stored value.
  getSettingsSection: <T,>(section: SettingsSection) =>
    request<T>('GET', `/settings/${section}`),
  putSettingsSection: <T,>(section: SettingsSection, doc: T) =>
    request<T>('PUT', `/settings/${section}`, doc),
  // Tag usage view: the series/episodes carrying one tag (tags section).
  getTagUsage: (id: number) => request<TagUsage>('GET', `/settings/tags/${id}/usage`),
  // P5 action endpoints (the four POSTs beyond the generic section docs).
  // The P5.7 chain endpoints answer 503 when every configured source fails —
  // an ApiError with the attempt log in the body.
  scoreCustomFormats: (req: CustomFormatScoreRequest) =>
    request<CustomFormatScoreResult>('POST', '/settings/custom-formats/score', req),
  testConnectNotification: (req: ConnectTestRequest) =>
    request<ConnectTestResult>('POST', '/settings/connect/test', req),
  searchMetadataSource: (req: MetadataSourceSearchRequest) =>
    request<MetadataSourceSearchResult>('POST', '/settings/metadata-source/search', req),
  runImportListImport: (req: ImportRunRequest) =>
    request<ImportRunResult>('POST', '/settings/import-lists/import', req),
  // Prowlarr PR4: the Notifications section — saved sinks (webhook / Plex /
  // Jellyfin / Emby). The token is vault-encrypted at rest and masked on read
  // (empty string + token_set); an empty token in the update body keeps the
  // stored value (same convention as indexer passwords).
  listNotifications: () => request<NotificationSink[]>('GET', '/settings/notifications'),
  createNotification: (req: NotificationSinkCreate) =>
    request<NotificationSink>('POST', '/settings/notifications', req),
  updateNotification: (id: number, req: NotificationSinkUpdate) =>
    request<NotificationSink>('PUT', `/settings/notifications/${id}`, req),
  deleteNotification: (id: number) => request<void>('DELETE', `/settings/notifications/${id}`),
  testNotification: (id: number) =>
    request<NotificationSinkTestResult>('POST', `/settings/notifications/${id}/test`),
  // Prowlarr PR4: the Indexer Proxies section — saved FlareSolverr instances.
  // Indexers whose tags intersect a proxy's tags are routed through it. The
  // password is masked the same way (empty + password_set; empty keeps).
  listIndexerProxies: () => request<IndexerProxy[]>('GET', '/settings/proxies'),
  createIndexerProxy: (req: IndexerProxyCreate) =>
    request<IndexerProxy>('POST', '/settings/proxies', req),
  updateIndexerProxy: (id: number, req: IndexerProxyUpdate) =>
    request<IndexerProxy>('PUT', `/settings/proxies/${id}`, req),
  deleteIndexerProxy: (id: number) => request<void>('DELETE', `/settings/proxies/${id}`),
  testIndexerProxy: (id: number) =>
    request<IndexerProxyTestResult>('POST', `/settings/proxies/${id}/test`),

  // ---- P6: System area (Status / Tasks / Backup / Updates / Events / Logs / migration) ----
  getSystemHealth: () => request<SystemHealth>('GET', '/system/health'),
  getSystemTasks: () => request<SystemTasks>('GET', '/system/tasks'),
  getSystemDisk: () => request<SystemDisk>('GET', '/system/disk'),
  getSystemAbout: () => request<SystemAbout>('GET', '/system/about'),
  // Backup export: include_queue / include_series are optional snapshots.
  getSystemBackup: (f?: { includeQueue?: boolean; includeSeries?: boolean }) => {
    const params = new URLSearchParams()
    if (f?.includeQueue) params.set('include_queue', 'true')
    if (f?.includeSeries) params.set('include_series', 'true')
    const s = params.toString()
    return request<BackupDocument>('GET', `/system/backup${s ? `?${s}` : ''}`)
  },
  putSystemBackup: (req: BackupImportRequest) =>
    request<BackupImportResult>('POST', '/system/backup', req),
  // Events: the history source feed (date / event-type filter).
  getSystemEvents: (f?: { since?: string; event?: string; limit?: number }) => {
    const params = new URLSearchParams()
    if (f?.since) params.set('since', f.since)
    if (f?.event) params.set('event', f.event)
    if (f?.limit) params.set('limit', String(f.limit))
    const s = params.toString()
    return request<SystemEvents>('GET', `/system/events${s ? `?${s}` : ''}`)
  },
  getSystemUpdates: () => request<SystemUpdates>('GET', '/system/updates'),
  // Sonarr drop-in migration.
  runMigration: (req: MigrationRequest) => request<MigrationResult>('POST', '/system/migration', req),
  getMigrationStatus: () => request<MigrationStatus>('GET', '/system/migration/status'),

  // ---- add flows (#42) ----
  // Provider search: debounced by the UI; each call is one provider round
  // trip. The endpoints answer 503 when the provider isn't configured — the
  // add-flow UI greys the search box with that reason.
  // Radarr R2: /search/movies returns enriched MovieSearchHit rows (badges
  // from the TMDB search + details payload).
  searchMovies: (q: string) => request<MovieSearchHit[]>('GET', `/search/movies?query=${encodeURIComponent(q)}`),
  searchTv: (q: string) => request<SearchResult[]>('GET', `/search/tv?query=${encodeURIComponent(q)}`),
  searchTvTmdb: (q: string) => request<SearchResult[]>('GET', `/search/tv-tmdb?query=${encodeURIComponent(q)}`),
  // Lidarr L2: /search/artists returns enriched artist hits + release-group
  // hits for the Add Artist page (L1 returned a flat SearchResult[]; the
  // music add flow is the only consumer, so the response shape moved in
  // place — see the spec's endpoint description).
  searchArtists: (q: string) =>
    request<ArtistSearchResult>('GET', `/search/artists?query=${encodeURIComponent(q)}`),
  searchBookAuthors: (q: string) =>
    request<SearchResult[]>('GET', `/search/books/authors?query=${encodeURIComponent(q)}`),
  searchBookWorks: (q: string, author: string) => {
    const params = new URLSearchParams()
    params.set('query', q)
    if (author) params.set('author', author)
    return request<SearchResult[]>('GET', `/search/books/works?${params.toString()}`)
  },
  // Quality profiles for the picker: built-in names + the kind's default.
  getProfiles: (kind: ProfileKind) =>
    request<ProfilesResponse>('GET', `/profiles?kind=${kind}`),
}

export type Api = typeof client

// SystemLogLine is one line in the System > Log Files SSE stream. It mirrors
// the Go system.LogLine JSON shape; the OpenAPI op serves text/event-stream
// (not a JSON schema), so this is a local wire type rather than a generated
// one.
export type SystemLogLine = {
  time: string
  level: string
  message: string
}

// LogStreamStatus reports the log-stream phase to the viewer.
export type LogStreamStatus = { state: 'connecting' | 'open' | 'error' | 'closed'; error?: string }

// openSystemLogsStream opens the /api/v1/system/logs SSE stream. Browsers
// cannot set the Authorization header on an EventSource, so this uses fetch +
// ReadableStream with the token in the header (as every other call). onLines
// receives each frame's line batch (the backlog first, then live lines);
// onStatus reports the phase. Returns a close function; call it on unmount.
export function openSystemLogsStream(
  onLines: (lines: SystemLogLine[]) => void,
  onStatus: (s: LogStreamStatus) => void,
): () => void {
  const controller = new AbortController()
  let closed = false

  const headers: Record<string, string> = { Accept: 'text/event-stream' }
  const token = getToken()
  if (token) headers.Authorization = `Bearer ${token}`

  onStatus({ state: 'connecting' })

  fetch(`${API}/system/logs`, { headers, signal: controller.signal })
    .then(async (res) => {
      if (!res.ok || !res.body) {
        throw new Error(`log stream failed (${res.status})`)
      }
      onStatus({ state: 'open' })
      const reader = res.body.getReader()
      const decoder = new TextDecoder()
      let buf = ''
      for (;;) {
        const { done, value } = await reader.read()
        if (done) break
        buf += decoder.decode(value, { stream: true })
        // SSE frames are separated by a blank line.
        let idx
        while ((idx = buf.indexOf('\n\n')) !== -1) {
          const frame = buf.slice(0, idx)
          buf = buf.slice(idx + 2)
          const data = frame
            .split('\n')
            .filter((l) => l.startsWith('data: '))
            .map((l) => l.slice(6))
            .join('\n')
          if (!data) continue
          try {
            onLines(JSON.parse(data) as SystemLogLine[])
          } catch {
            // A malformed frame must not kill the stream.
          }
        }
      }
    })
    .catch((e: unknown) => {
      if (!closed) onStatus({ state: 'error', error: e instanceof Error ? e.message : String(e) })
    })
    .finally(() => {
      if (!closed) onStatus({ state: 'closed' })
    })

  return () => {
    closed = true
    controller.abort()
  }
}

// connectEvents opens the /api/v1/events WebSocket (#40: "WebSocket
// subscription for live queue/history updates"). Browsers cannot set the
// Authorization header on a WebSocket handshake, so the bearer token goes as
// ?token= (the server's documented browser path).
//
// The helper auto-reconnects with a capped backoff so a blip (server restart,
// proxy restart) doesn't silently kill live updates. onStatus reports the
// connection phase for UI state ("connecting" → "open" → "closed"); onEvent
// receives every frame, including the "hello" greeting. Returns a close
// function; call it on unmount to stop reconnecting.
export function connectEvents(
  onEvent: (frame: EventFrame) => void,
  onStatus?: (status: 'connecting' | 'open' | 'closed') => void,
): () => void {
  let ws: WebSocket | null = null
  let closedByCaller = false
  let retry = 0

  function report(status: 'connecting' | 'open' | 'closed') {
    onStatus?.(status)
  }

  function connect() {
    if (closedByCaller) return
    report('connecting')
    const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const path = '/api/v1/events'
    const token = getToken()
    const url = token
      ? `${proto}//${window.location.host}${path}?token=${encodeURIComponent(token)}`
      : `${proto}//${window.location.host}${path}`
    ws = new WebSocket(url)

    ws.onopen = () => {
      retry = 0
      report('open')
    }
    ws.onmessage = (msg: MessageEvent) => {
      try {
        const frame = JSON.parse(String(msg.data)) as EventFrame
        onEvent(frame)
      } catch {
        // A malformed frame (shouldn't happen; the server always writes JSON)
        // must not kill the stream.
      }
    }
    ws.onclose = () => {
      if (closedByCaller) return
      // Backoff: 0s, 1s, 2s, 4s, ... capped at 15s.
      const delay = Math.min(15000, 1000 * 2 ** Math.min(retry, 4))
      retry += 1
      report('connecting')
      setTimeout(connect, delay)
    }
  }

  connect()

  return () => {
    closedByCaller = true
    report('closed')
    ws?.close()
  }
}
