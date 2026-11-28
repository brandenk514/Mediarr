package tv

import (
	"context"
	"errors"
	"time"
)

// Media pipeline entities and the persistence surface the TV service programs
// against. This is a pure (no I/O) definition: the structs are data and Repo is
// the interface. The concrete repository lives in the postgres adapter so this
// package stays free of database imports — the same thin-common-interface
// pattern the movies domain uses (PLAN §4: per-kind tables + a thin common
// interface with movies).
//
// Identity: v1 identifies a series by title + year (provider ids — tvdb_id /
// tmdb_id — are carried but nullable until the providers layer lands in a later
// milestone). An episode is identified within its series by (season, episode).
// Wanted items carry that episode specifier (PLAN §4 wanted_items specifier).

// Series is a tracked TV series.
type Series struct {
	ID             int64
	TVDBID         int // nullable provider id (0 = unset)
	TMDBID         int // nullable provider id (0 = unset)
	Title          string
	Year           int
	QualityProfile string // name of a quality profile (see BuiltInProfiles)
	Monitored      bool   // series-level monitor toggle
	SeriesType     string // "standard" (default), "daily", "oneshot"
	SeasonFolder   bool   // organize files under a per-season folder
	Tags           []string
	AddedAt        time.Time
	// Artwork URLs (issue #149): poster + backdrop on the detail and list
	// wire shapes, serialized with bare Go field names like the rest of
	// this struct (docs/openapi.md convention; owner decision 2026-11-27).
	// The camelCase posterUrl/backdropUrl spelling lives only on the
	// Discover Item document (#118, API-only). "" = keyless or no art (the
	// R3 keyless convention).
	PosterURL   string
	BackdropURL string
	// Enrichment (issue #157): the TV detail-page metadata card, sourced
	// from the TMDB series detail fetch and persisted on the row so list
	// reads serve it without a per-row provider call (the #118/#149 house
	// pattern). Serialized with bare Go field names; pointer/nil fields
	// are null on the wire, arrays serialize as [] (never null). Every
	// field is empty/nil on a keyless stack or when the lookup failed —
	// the R3 keyless convention: never an error, never raw "not
	// configured" text.
	//
	// Overview/Status/OriginalLanguage/Network are *string (null =
	// unknown). FirstAirDate is "" when unknown (the repo stores the
	// column, so it rides list reads too). Cast/crew come back as the
	// full lists — #152 renders them behind "View Full Crew".
	//
	// Scores is nil until a provider fills it; TMDB serves only the TMDB
	// vote percentage. ImdbID/TraktID are external id mappings (ids,
	// never ratings — TMDB does not serve IMDb/Trakt scores).
	Overview            *string
	Genres              []string
	Keywords            []string
	Status              *string
	OriginalLanguage    *string
	Network             *string
	FirstAirDate        string // "YYYY-MM-DD"; "" when unknown
	ProductionCountries []string
	Scores              *SeriesScores
	ImdbID              string
	TraktID             string
	TrailerURL          string // detail-only (not persisted); "" when none
	Cast                []SeriesCast
	Crew                []SeriesCrew
}

// SeriesPatch is a partial series update: nil fields are left unchanged.
// Used by the series update endpoint (P2 parity: quality profile, series
// type, season folder, tags, monitored).
type SeriesPatch struct {
	Monitored      *bool
	QualityProfile *string
	SeriesType     *string
	SeasonFolder   *bool
	Tags           *[]string
}

// SeriesScores are the external scores a series carries (issue #157).
// Every field is nil when unknown — the movie MovieScores convention.
// TMDB serves only the TMDB vote percentage; IMDb/Trakt appear as ids
// (Series.ImdbID / Series.TraktID), never as invented ratings.
type SeriesScores struct {
	TmdbPct *float64 // TMDB vote_average scaled 0-100
}

// SeriesCast is one credited cast member (issue #157): the display name
// and the character they play.
type SeriesCast struct {
	Name string
	Role string
}

// SeriesCrew is one crew member (issue #157): the display name and the
// job they did (Director, Writer, Creator, ...).
type SeriesCrew struct {
	Name string
	Job  string
}

// SeriesMetadata is the persisted subset of the series enrichment (issue
// #157, mirroring movies.MovieMetadata from #150): the flat zero-is-
// unknown wire shape the repository stores, so list reads can serve the
// values without a per-row provider call. It intentionally holds no
// pointer types (zero = unknown, the column-default convention).
// TrailerURL is detail-only and not persisted.
type SeriesMetadata struct {
	Genres              []string
	Keywords            []string
	OriginalLanguage    string
	Status              string
	Network             string
	FirstAirDate        string
	ProductionCountries []string
	TmdbPct             float64
}

// Episode is a single known episode within a series.
type Episode struct {
	ID        int64
	SeriesID  int64
	Season    int
	Episode   int
	Title     string
	AirDate   *time.Time // first air date (UTC); nil when unknown
	Monitored bool       // episode-level monitor toggle
	AddedAt   time.Time
}

// WantedStatus is the lifecycle of a wanted request.
type WantedStatus string

const (
	WantedPending   WantedStatus = "pending"
	WantedSatisfied WantedStatus = "satisfied"
)

// Wanted is the per-episode "we want a file" request. It is keyed by the
// episode specifier (series, season, episode) so monitoring works at both
// episode and season granularity (PLAN §4).
type Wanted struct {
	SeriesID     int64
	Season       int
	Episode      int
	Status       WantedStatus
	ReleaseTitle string // the release that satisfied it (when satisfied)
	Quality      string // quality of the satisfying release ("" until satisfied)
	CreatedAt    time.Time
	SatisfiedAt  *time.Time
}

// QueueState is the state of a download-queue entry.
type QueueState string

const (
	QueueQueued      QueueState = "queued"
	QueueDownloading QueueState = "downloading"
	QueueComplete    QueueState = "complete"
	QueueFailed      QueueState = "failed"
)

// QueueEntry is one download job in the queue.
type QueueEntry struct {
	ID             int64
	SeriesID       int64
	Season         int
	Episode        int
	ReleaseTitle   string
	Indexer        string
	DownloadClient string
	State          QueueState
	Progress       int
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// HistoryEntry is an auditable event in a series' lifecycle.
type HistoryEntry struct {
	ID       int64
	SeriesID int64
	Event    string // "added", "monitored", "searched", "download", "import", "collision", ...
	Detail   string
	At       time.Time
}

// ErrSeriesNotFound is returned when a series id does not exist.
var ErrSeriesNotFound = errors.New("tv: series not found")

// ErrEpisodeNotFound is returned when an episode does not exist.
var ErrEpisodeNotFound = errors.New("tv: episode not found")

// ErrCollision is returned when an import target file already exists on disk;
// the existing file is never overwritten (PLAN §6/§8).
var ErrCollision = errors.New("tv: import: destination file already exists")

// Repo is the persistence surface the TV pipeline needs. Implemented by the
// postgres adapter; tests can satisfy it with a fake. The service programs
// against this interface, never against *sql.DB.
type Repo interface {
	// Series
	CreateSeries(ctx context.Context, s Series) (int64, error)
	GetSeries(ctx context.Context, id int64) (*Series, error)
	ListSeries(ctx context.Context) ([]Series, error)
	SetSeriesMonitored(ctx context.Context, id int64, monitored bool) error
	UpdateSeries(ctx context.Context, id int64, p SeriesPatch) error
	// SetSeriesArtwork persists the upstream poster/backdrop URLs the
	// artwork enrichment reported (issue #149), so list reads serve
	// PosterURL without a per-row provider call. Best-effort: a failure
	// must never fail the read that triggered it. ''/'' = keyless/no art.
	SetSeriesArtwork(ctx context.Context, id int64, posterURL, backdropURL string) error
	// SetSeriesMetadata persists the provider metadata the enrichment
	// reported (issue #157: genres, keywords, original language, status,
	// network, first air date, production countries, TMDB score), so
	// list reads serve it without a per-row provider call (the #150
	// movie pattern). Best-effort: a failure must never fail the read
	// that triggered it. Zero values = the provider reported nothing
	// (keyless dev).
	SetSeriesMetadata(ctx context.Context, id int64, meta SeriesMetadata) error

	// Episodes
	CreateEpisode(ctx context.Context, e Episode) (int64, error)
	GetEpisode(ctx context.Context, seriesID int64, season, episode int) (*Episode, error)
	ListEpisodes(ctx context.Context, seriesID int64) ([]Episode, error)
	SetEpisodeMonitored(ctx context.Context, seriesID int64, season, episode int, monitored bool) error

	// Wanted (episode granularity)
	EnsureWanted(ctx context.Context, seriesID int64, season, episode int) error
	GetWanted(ctx context.Context, seriesID int64, season, episode int) (*Wanted, error)
	ListWanted(ctx context.Context, seriesID int64) ([]Wanted, error)
	// MarkWantedSatisfied flips the episode's wanted row to satisfied,
	// recording the release that satisfied it and the release's quality
	// label (e.g. "1080p"; "" when unknown) — the quality is what the
	// Cutoff Unmet wanted tab compares against the series cutoff.
	MarkWantedSatisfied(ctx context.Context, seriesID int64, season, episode int, releaseTitle, quality string) error

	// Queue
	CreateQueue(ctx context.Context, e QueueEntry) (int64, error)
	UpdateQueue(ctx context.Context, e QueueEntry) error
	GetQueue(ctx context.Context, id int64) (*QueueEntry, error)
	ListQueue(ctx context.Context, seriesID int64) ([]QueueEntry, error)

	// History
	AddHistory(ctx context.Context, e HistoryEntry) error
	ListHistory(ctx context.Context, seriesID int64, limit int) ([]HistoryEntry, error)
}
