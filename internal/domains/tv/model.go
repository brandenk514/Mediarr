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
	AddedAt        time.Time
}

// Episode is a single known episode within a series.
type Episode struct {
	ID        int64
	SeriesID  int64
	Season    int
	Episode   int
	Title     string
	Monitored bool // episode-level monitor toggle
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

	// Episodes
	CreateEpisode(ctx context.Context, e Episode) (int64, error)
	GetEpisode(ctx context.Context, seriesID int64, season, episode int) (*Episode, error)
	ListEpisodes(ctx context.Context, seriesID int64) ([]Episode, error)
	SetEpisodeMonitored(ctx context.Context, seriesID int64, season, episode int, monitored bool) error

	// Wanted (episode granularity)
	EnsureWanted(ctx context.Context, seriesID int64, season, episode int) error
	GetWanted(ctx context.Context, seriesID int64, season, episode int) (*Wanted, error)
	ListWanted(ctx context.Context, seriesID int64) ([]Wanted, error)
	MarkWantedSatisfied(ctx context.Context, seriesID int64, season, episode int, releaseTitle string) error

	// Queue
	CreateQueue(ctx context.Context, e QueueEntry) (int64, error)
	UpdateQueue(ctx context.Context, e QueueEntry) error
	GetQueue(ctx context.Context, id int64) (*QueueEntry, error)
	ListQueue(ctx context.Context, seriesID int64) ([]QueueEntry, error)

	// History
	AddHistory(ctx context.Context, e HistoryEntry) error
	ListHistory(ctx context.Context, seriesID int64, limit int) ([]HistoryEntry, error)
}
