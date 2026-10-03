package movies

import (
	"context"
	"errors"
	"time"
)

// Media pipeline entities and the persistence surface the service programs
// against. This is a pure (no I/O) definition: the structs are data and Repo
// is the interface. The concrete repository lives in the postgres adapter so
// this package stays free of database imports.

// Movie is a tracked movie. v1 identifies a movie by title + year. Provider
// metadata (TMDB/IMDB ids) is added with the providers layer in a later
// milestone; until then title+year is the identity.
type Movie struct {
	ID             int64
	Title          string
	Year           int
	QualityProfile string // name of a quality profile (see BuiltInProfiles)
	Monitored      bool
	AddedAt        time.Time
}

// WantedStatus is the lifecycle of a movie's "we want a file" request.
type WantedStatus string

const (
	WantedPending   WantedStatus = "pending"
	WantedSatisfied WantedStatus = "satisfied"
)

// Wanted is the per-movie "want a file" request created when a movie is added
// or monitored.
type Wanted struct {
	MovieID      int64
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
	MovieID        int64
	ReleaseTitle   string
	Indexer        string
	DownloadClient string
	State          QueueState
	Progress       int
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// HistoryEntry is an auditable event in a movie's lifecycle.
type HistoryEntry struct {
	ID      int64
	MovieID int64
	Event   string // "searched", "download", "import", "failed", ...
	Detail  string
	At      time.Time
}

// ErrMovieNotFound is returned when a movie id does not exist.
var ErrMovieNotFound = errors.New("movies: movie not found")

// Repo is the persistence surface the movie pipeline needs. Implemented by the
// postgres adapter; tests can satisfy it with a fake. The service programs
// against this interface, never against *sql.DB.
type Repo interface {
	CreateMovie(ctx context.Context, m Movie) (int64, error)
	GetMovie(ctx context.Context, id int64) (*Movie, error)
	ListMovies(ctx context.Context) ([]Movie, error)

	EnsureWanted(ctx context.Context, movieID int64) error
	GetWanted(ctx context.Context, movieID int64) (*Wanted, error)
	ListWanted(ctx context.Context) ([]Wanted, error)
	MarkWantedSatisfied(ctx context.Context, movieID int64, releaseTitle string) error

	CreateQueue(ctx context.Context, e QueueEntry) (int64, error)
	UpdateQueue(ctx context.Context, e QueueEntry) error
	GetQueue(ctx context.Context, id int64) (*QueueEntry, error)
	ListQueue(ctx context.Context) ([]QueueEntry, error)

	AddHistory(ctx context.Context, e HistoryEntry) error
	ListHistory(ctx context.Context, limit int) ([]HistoryEntry, error)
}
