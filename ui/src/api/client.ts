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
  Edition,
  Episode,
  Indexer,
  IndexerStats,
  IndexerTestResult,
  LoginRequest,
  LoginResponse,
  Me,
  MonitoredResult,
  Movie,
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
  listMovies: () => request<Movie[]>('GET', '/movies'),
  getMovie: (id: number) => request<Movie>('GET', `/movies/${id}`),
  addMovie: (req: AddMovieRequest) => request<AddedId>('POST', '/movies', req),
  runMoviePipeline: (id: number) => request<{ imported: string }>('POST', `/movies/${id}/pipeline`),

  // ---- tv ----
  listSeries: () => request<Series[]>('GET', '/tv'),
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
  listArtists: () => request<Artist[]>('GET', '/music/artists'),
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
  listAuthors: () => request<Author[]>('GET', '/books/authors'),
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
}

export type Api = typeof client
