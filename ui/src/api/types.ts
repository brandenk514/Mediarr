// Wire types for the mediarr v1 API, generated from openapi.yaml (M8 #77).
//
// types.gen.ts is the openapi-typescript output — the single source of
// truth for every request/response shape. This file is a thin re-export
// shim so call sites keep their short names, plus the two client
// ergonomics (ListQuery, QueueFilter) that build URL query strings
// rather than wire documents.
import type { components } from './types.gen'

export type Error = components["schemas"]["Error"]
export type VersionInfo = components["schemas"]["VersionInfo"]
export type ReadinessCheck = components["schemas"]["ReadinessCheck"]
export type ReadinessReport = components["schemas"]["ReadinessReport"]
export type LoginRequest = components["schemas"]["LoginRequest"]
export type LoginResponse = components["schemas"]["LoginResponse"]
export type Me = components["schemas"]["Me"]
export type PageMovie = components["schemas"]["PageMovie"]
export type PageSeries = components["schemas"]["PageSeries"]
export type PageArtist = components["schemas"]["PageArtist"]
export type PageAuthor = components["schemas"]["PageAuthor"]
export type PageQueueItem = components["schemas"]["PageQueueItem"]
export type AddedId = components["schemas"]["AddedId"]
export type MonitoredResult = components["schemas"]["MonitoredResult"]
export type SeasonMonitoredResult = components["schemas"]["SeasonMonitoredResult"]
export type EpisodeMonitoredResult = components["schemas"]["EpisodeMonitoredResult"]
export type TrackMonitoredResult = components["schemas"]["TrackMonitoredResult"]
export type ImportedResult = components["schemas"]["ImportedResult"]
export type Movie = components["schemas"]["Movie"]
export type MovieStats = components["schemas"]["MovieStats"]
export type MovieFile = components["schemas"]["MovieFile"]
export type AddMovieRequest = components["schemas"]["AddMovieRequest"]
export type Series = components["schemas"]["Series"]
export type AddSeriesRequest = components["schemas"]["AddSeriesRequest"]
export type UpdateSeriesRequest = components["schemas"]["UpdateSeriesRequest"]
export type SeriesSearchResult = components["schemas"]["SeriesSearchResult"]
export type SeriesHistory = components["schemas"]["SeriesHistory"]
export type SeriesHistoryItem = components["schemas"]["SeriesHistoryItem"]
export type ImportRequest = components["schemas"]["ImportRequest"]
export type ImportCandidate = components["schemas"]["ImportCandidate"]
export type ImportReport = components["schemas"]["ImportReport"]
export type RootFolder = components["schemas"]["RootFolder"]
export type RootFolderRequest = components["schemas"]["RootFolderRequest"]
export type Episode = components["schemas"]["Episode"]
export type AddEpisodeRequest = components["schemas"]["AddEpisodeRequest"]
export type WantedStatus = components["schemas"]["WantedStatus"]
export type MovieWanted = components["schemas"]["MovieWanted"]
export type SeriesWanted = components["schemas"]["SeriesWanted"]
export type Artist = components["schemas"]["Artist"]
export type AddArtistRequest = components["schemas"]["AddArtistRequest"]
export type Album = components["schemas"]["Album"]
export type AddAlbumRequest = components["schemas"]["AddAlbumRequest"]
export type Track = components["schemas"]["Track"]
export type AddTrackRequest = components["schemas"]["AddTrackRequest"]
export type AlbumWanted = components["schemas"]["AlbumWanted"]
export type Author = components["schemas"]["Author"]
export type AddAuthorRequest = components["schemas"]["AddAuthorRequest"]
export type BookTitle = components["schemas"]["BookTitle"]
export type AddTitleRequest = components["schemas"]["AddTitleRequest"]
export type Edition = components["schemas"]["Edition"]
export type AddEditionRequest = components["schemas"]["AddEditionRequest"]
export type TitleWanted = components["schemas"]["TitleWanted"]
export type MediaKind = components["schemas"]["MediaKind"]
export type QueueState = components["schemas"]["QueueState"]
export type IndexerHealthStatus = components["schemas"]["IndexerHealthStatus"]
export type DashboardLibrary = components["schemas"]["DashboardLibrary"]
export type DashboardTotals = components["schemas"]["DashboardTotals"]
export type HistoryItem = components["schemas"]["HistoryItem"]
export type DashboardQueueItem = components["schemas"]["DashboardQueueItem"]
export type IndexerHealth = components["schemas"]["IndexerHealth"]
export type Dashboard = components["schemas"]["Dashboard"]
export type QueueViewItem = components["schemas"]["QueueViewItem"]
export type QueueHistoryEvent = components["schemas"]["QueueHistoryEvent"]
export type Indexer = components["schemas"]["Indexer"]
export type IndexerCreate = components["schemas"]["IndexerCreate"]
export type IndexerUpdate = components["schemas"]["IndexerUpdate"]
export type IndexerEnabled = components["schemas"]["IndexerEnabled"]
export type IndexerTestResult = components["schemas"]["IndexerTestResult"]
export type IndexerStats = components["schemas"]["IndexerStats"]
export type DownloadClientView = components["schemas"]["DownloadClientView"]
export type DownloadClientRequest = components["schemas"]["DownloadClientRequest"]
export type LibraryOverride = components["schemas"]["LibraryOverride"]
export type LibraryOverrides = components["schemas"]["LibraryOverrides"]
export type SearchResult = components["schemas"]["SearchResult"]
export type MovieSearchHit = components["schemas"]["MovieSearchHit"]
export type MovieCollection = components["schemas"]["MovieCollection"]
export type DiscoverMovies = components["schemas"]["DiscoverMovies"]
export type ProfileKind = components["schemas"]["ProfileKind"]
export type ProfilesResponse = components["schemas"]["ProfilesResponse"]
export type EventFrame = components["schemas"]["EventFrame"]
export type EventPayload = components["schemas"]["EventPayload"]
export type QueueUpdatedMovie = components["schemas"]["QueueUpdatedMovie"]
export type QueueUpdatedTV = components["schemas"]["QueueUpdatedTV"]
export type QueueUpdatedMusic = components["schemas"]["QueueUpdatedMusic"]
export type QueueUpdatedBook = components["schemas"]["QueueUpdatedBook"]
export type HistoryAppendedMovie = components["schemas"]["HistoryAppendedMovie"]
export type HistoryAppendedTV = components["schemas"]["HistoryAppendedTV"]
export type HistoryAppendedMusic = components["schemas"]["HistoryAppendedMusic"]
export type HistoryAppendedBook = components["schemas"]["HistoryAppendedBook"]

// ---- P3: Activity + Wanted (global history, blocklist, wanted, manual import) ----
export type GlobalHistoryItem = components["schemas"]["GlobalHistoryItem"]
export type BlocklistEntry = components["schemas"]["BlocklistEntry"]
export type BlocklistRequest = components["schemas"]["BlocklistRequest"]
export type WantedTab = components["schemas"]["WantedTab"]
export type WantedItem = components["schemas"]["WantedItem"]
export type PageWantedItem = components["schemas"]["PageWantedItem"]
export type WantedSearchRequest = components["schemas"]["WantedSearchRequest"]
export type WantedEpisodeRef = components["schemas"]["WantedEpisodeRef"]
export type WantedUnmonitorRequest = components["schemas"]["WantedUnmonitorRequest"]
export type WantedActionResult = components["schemas"]["WantedActionResult"]
export type ManualImportRequest = components["schemas"]["ManualImportRequest"]
export type ManualImportReport = components["schemas"]["ManualImportReport"]
export type QueueGrabResult = components["schemas"]["QueueGrabResult"]
export type QueueRemoveResult = components["schemas"]["QueueRemoveResult"]

// ---- P4: Calendar (upcoming monitored air dates + iCal feed) ----
export type CalendarEntry = components["schemas"]["CalendarEntry"]

// ---- P5: Settings sections (the 13-section settings shell) ----
export type SettingsSection = components["schemas"]["SettingsSection"]
export type SettingsSectionDocument = components["schemas"]["SettingsSectionDocument"]
export type GeneralSettings = components["schemas"]["GeneralSettings"]
export type UISettings = components["schemas"]["UISettings"]
export type QualityLevel = components["schemas"]["QualityLevel"]
export type QualitySettings = components["schemas"]["QualitySettings"]
export type QualityProfileItem = components["schemas"]["QualityProfileItem"]
export type QualityProfile = components["schemas"]["QualityProfile"]
export type ProfilesSettings = components["schemas"]["ProfilesSettings"]
export type TagInfo = components["schemas"]["TagInfo"]
export type TagsSettings = components["schemas"]["TagsSettings"]
export type TagUsage = components["schemas"]["TagUsage"]
export type NamingTemplate = components["schemas"]["NamingTemplate"]
export type FileManagement = components["schemas"]["FileManagement"]
export type RemotePathMapping = components["schemas"]["RemotePathMapping"]
export type MediaManagementSettings = components["schemas"]["MediaManagementSettings"]
export type CFRule = components["schemas"]["CFRule"]
export type CustomFormat = components["schemas"]["CustomFormat"]
export type CustomFormatsSettings = components["schemas"]["CustomFormatsSettings"]
export type CustomFormatScoreRequest = components["schemas"]["CustomFormatScoreRequest"]
export type CustomFormatScoreResult = components["schemas"]["CustomFormatScoreResult"]
export type Notification = components["schemas"]["Notification"]
export type ConnectSettings = components["schemas"]["ConnectSettings"]
export type ConnectTestRequest = components["schemas"]["ConnectTestRequest"]
export type ConnectTestResult = components["schemas"]["ConnectTestResult"]
export type MetadataSettings = components["schemas"]["MetadataSettings"]
export type MetadataSourceSettings = components["schemas"]["MetadataSourceSettings"]
export type MetadataSourceSearchRequest = components["schemas"]["MetadataSourceSearchRequest"]
export type MetadataSourceSearchResult = components["schemas"]["MetadataSourceSearchResult"]
export type ProviderAttempt = components["schemas"]["ProviderAttempt"]
export type ImportList = components["schemas"]["ImportList"]
export type ImportListsSettings = components["schemas"]["ImportListsSettings"]
export type ImportRunRequest = components["schemas"]["ImportRunRequest"]
export type ImportRunItem = components["schemas"]["ImportRunItem"]
export type ImportRunResult = components["schemas"]["ImportRunResult"]

// ---- P6: System area (Status / Tasks / Backup / Updates / Events / Logs / migration) ----
export type HealthCheck = components["schemas"]["HealthCheck"]
export type SystemHealth = components["schemas"]["SystemHealth"]
export type TaskInfo = components["schemas"]["TaskInfo"]
export type SystemTasks = components["schemas"]["SystemTasks"]
export type DiskRoot = components["schemas"]["DiskRoot"]
export type SystemDisk = components["schemas"]["SystemDisk"]
export type SystemAbout = components["schemas"]["SystemAbout"]
export type BackupDocument = components["schemas"]["BackupDocument"]
export type BackupImportRequest = components["schemas"]["BackupImportRequest"]
export type BackupImportResult = components["schemas"]["BackupImportResult"]
export type SystemEvents = components["schemas"]["SystemEvents"]
export type SystemUpdates = components["schemas"]["SystemUpdates"]
export type ChangelogEntry = components["schemas"]["ChangelogEntry"]
export type MigrationItem = components["schemas"]["MigrationItem"]
export type MigrationCounts = components["schemas"]["MigrationCounts"]
export type MigrationResult = components["schemas"]["MigrationResult"]
export type MigrationInclude = components["schemas"]["MigrationInclude"]
export type MigrationRequest = components["schemas"]["MigrationRequest"]
export type MigrationStatus = components["schemas"]["MigrationStatus"]

// Books' domain type is books.Title; the spec names it BookTitle so the
// document reads clearly at a glance.
export type Title = components["schemas"]["BookTitle"]

// The dashboard's recent-queue feed item (the queue view uses QueueViewItem).
export type QueueItem = components["schemas"]["DashboardQueueItem"]

// ListQuery is the optional list input (q / monitored / page / page_size).
export interface ListQuery {
  q?: string
  monitored?: boolean
  page?: number
  page_size?: number
}

// QueueFilter is the optional queue-list input (q / kind / state / page / page_size).
export interface QueueFilter {
  q?: string
  kind?: MediaKind
  state?: QueueState
  page?: number
  page_size?: number
}
