// Wire types for the mediarr v1 API.
//
// IMPORTANT: the Go domain structs carry NO json tags, so encoding/json emits
// the *exact Go field names* (e.g. `ID`, `Title`, `Monitored`, `AddedAt`).
// These interfaces mirror that wire format verbatim — do NOT rename to camelCase
// to "clean them up"; the client would then fail to parse real responses.

export type QueueState = 'queued' | 'downloading' | 'complete' | 'failed'
export type WantedStatus = 'pending' | 'satisfied'

// ---- auth ----

export interface LoginRequest {
  username: string
  password: string
}

export interface LoginResponse {
  token: string
  user: string
  role: string
  token_prefix: string
}

export interface Me {
  username: string
  role: string
}

// ---- movies ----

export interface Movie {
  ID: number
  Title: string
  Year: number
  QualityProfile: string
  Monitored: boolean
  AddedAt: string
}

export interface AddMovieRequest {
  title: string
  year?: string
  quality_profile?: string
}

export interface AddedId {
  id: number
}

export interface MonitoredResult {
  id: number
  monitored: boolean
}

export interface TrackMonitoredResult {
  disc: number
  number: number
  monitored: boolean
}

// ---- tv ----

export interface Series {
  ID: number
  TVDBID: number
  TMDBID: number
  Title: string
  Year: number
  QualityProfile: string
  Monitored: boolean
  AddedAt: string
}

export interface AddSeriesRequest {
  title: string
  year?: string
  quality_profile?: string
  monitored?: boolean
}

export interface Episode {
  ID: number
  SeriesID: number
  Season: number
  Episode: number
  Title: string
  Monitored: boolean
  AddedAt: string
}

export interface AddEpisodeRequest {
  season: number
  episode: number
  title?: string
  monitored?: boolean
}

export interface SeriesWanted {
  SeriesID: number
  Season: number
  Episode: number
  Status: WantedStatus
  ReleaseTitle: string
  CreatedAt: string
  SatisfiedAt: string | null
}

// ---- music ----

export interface Artist {
  ID: number
  MBID: string
  Name: string
  Monitored: boolean
  AddedAt: string
}

export interface AddArtistRequest {
  name: string
  monitored?: boolean
}

export interface Album {
  ID: number
  ArtistID: number
  MBID: string
  Name: string
  Year: number
  QualityProfile: string
  Monitored: boolean
  VerifyChecksums: boolean
  Checksum: string
  AddedAt: string
}

export interface AddAlbumRequest {
  name: string
  year?: number
  quality_profile?: string
  monitored?: boolean
}

export interface Track {
  ID: number
  AlbumID: number
  MBID: string
  Disc: number
  Number: number
  Title: string
  Checksum: string
  Monitored: boolean
  AddedAt: string
}

export interface AddTrackRequest {
  disc: number
  number: number
  title?: string
  monitored?: boolean
}

export interface AlbumWanted {
  AlbumID: number
  TrackID: number
  Status: WantedStatus
  ReleaseTitle: string
  CreatedAt: string
  SatisfiedAt: string | null
}

// ---- books ----

export interface Author {
  ID: number
  Name: string
  Monitored: boolean
  AddedAt: string
}

export interface AddAuthorRequest {
  name: string
  monitored?: boolean
}

export interface Title {
  ID: number
  AuthorID: number
  Name: string
  Year: number
  QualityProfile: string
  Monitored: boolean
  AddedAt: string
}

export interface AddTitleRequest {
  name: string
  year?: number
  quality_profile?: string
  monitored?: boolean
}

export interface Edition {
  ID: number
  TitleID: number
  Format: string
  Publisher: string
  ISBN: string
  Year: number
  Pages: number
  Monitored: boolean
  AddedAt: string
}

export interface AddEditionRequest {
  format: string
  isbn?: string
  publisher?: string
  year?: number
  pages?: number
  monitored?: boolean
}

export interface TitleWanted {
  TitleID: number
  EditionID: number
  Status: WantedStatus
  ReleaseTitle: string
  CreatedAt: string
  SatisfiedAt: string | null
}

// ---- indexers ----

export interface Indexer {
  id: number
  name: string
  kind: string
  base_url: string
  enabled: boolean
  has_api_key: boolean
  last_test: string | null
}

export interface IndexerStats {
  name: string
  calls: number
  successes: number
  failures: number
  last_success: string | null
  last_error: string
  last_latency_ns: number
  avg_latency_ns: number
  total_latency_ns: number
}

export interface IndexerTestResult {
  ok: boolean
  error?: string
}
