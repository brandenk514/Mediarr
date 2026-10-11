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
  Edition,
  Episode,
  EventFrame,
  Indexer,
  IndexerStats,
  IndexerTestResult,
  ListQuery,
  LoginRequest,
  LoginResponse,
  Me,
  MonitoredResult,
  Movie,
  Page,
  Series,
  SeriesWanted,
  Title,
  TitleWanted,
  Track,
  TrackMonitoredResult,
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
  listMovies: (q?: ListQuery) => request<Page<Movie>>('GET', `/movies${buildListQuery(q)}`),
  getMovie: (id: number) => request<Movie>('GET', `/movies/${id}`),
  addMovie: (req: AddMovieRequest) => request<AddedId>('POST', '/movies', req),
  setMovieMonitored: (id: number, monitored: boolean) =>
    request<MonitoredResult>('PUT', `/movies/${id}`, { monitored }),
  runMoviePipeline: (id: number) => request<{ imported: string }>('POST', `/movies/${id}/pipeline`),

  // ---- tv ----
  listSeries: (q?: ListQuery) => request<Page<Series>>('GET', `/tv${buildListQuery(q)}`),
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

  // ---- music ----
  listArtists: (q?: ListQuery) => request<Page<Artist>>('GET', `/music/artists${buildListQuery(q)}`),
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
  listAuthors: (q?: ListQuery) => request<Page<Author>>('GET', `/books/authors${buildListQuery(q)}`),
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

  // ---- dashboard (#40) ----
  // recent caps the recent-history / recent-queue feeds (server clamps 1..100).
  getDashboard: (recent?: number) =>
    request<Dashboard>('GET', `/dashboard${recent ? `?recent=${recent}` : ''}`),
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
