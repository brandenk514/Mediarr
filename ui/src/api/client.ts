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
  Author,
  Dashboard,
  DownloadClientRequest,
  DownloadClientView,
  Edition,
  Episode,
  EventFrame,
  Indexer,
  IndexerCreate,
  IndexerStats,
  IndexerTestResult,
  IndexerUpdate,
  LibraryOverride,
  LibraryOverrides,
  ListQuery,
  LoginRequest,
  LoginResponse,
  Me,
  MonitoredResult,
  Movie,
  MediaKind,
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
  addMovie: (req: AddMovieRequest) => request<AddedId>('POST', '/movies', req),
  setMovieMonitored: (id: number, monitored: boolean) =>
    request<MonitoredResult>('PUT', `/movies/${id}`, { monitored }),
  runMoviePipeline: (id: number) => request<{ imported: string }>('POST', `/movies/${id}/pipeline`),

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
  indexerStats: (id: number) => request<IndexerStats>('GET', `/indexers/${id}/stats`),
  testIndexer: (id: number) => request<IndexerTestResult>('POST', `/indexers/${id}/test`),
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
  // JSON list of the upcoming monitored TV air dates in [start, end].
  listCalendar: (f?: { start?: string; end?: string }) => {
    const params = new URLSearchParams()
    if (f?.start) params.set('start', f.start)
    if (f?.end) params.set('end', f.end)
    const s = params.toString()
    return request<CalendarEntry[]>('GET', `/calendar${s ? `?${s}` : ''}`)
  },
  // iCal feed URL for a plain link / new tab: /calendar.ics carries the app
  // token as ?token= because a browser navigation can't set the Authorization
  // header, plus the optional window.
  calendarICS: (f?: { start?: string; end?: string }) => {
    const params = new URLSearchParams()
    if (f?.start) params.set('start', f.start)
    if (f?.end) params.set('end', f.end)
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

  // ---- add flows (#42) ----
  // Provider search: debounced by the UI; each call is one provider round
  // trip. The endpoints answer 503 when the provider isn't configured — the
  // add-flow UI greys the search box with that reason.
  searchMovies: (q: string) => request<SearchResult[]>('GET', `/search/movies?query=${encodeURIComponent(q)}`),
  searchTv: (q: string) => request<SearchResult[]>('GET', `/search/tv?query=${encodeURIComponent(q)}`),
  searchTvTmdb: (q: string) => request<SearchResult[]>('GET', `/search/tv-tmdb?query=${encodeURIComponent(q)}`),
  searchArtists: (q: string) =>
    request<SearchResult[]>('GET', `/search/artists?query=${encodeURIComponent(q)}`),
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
