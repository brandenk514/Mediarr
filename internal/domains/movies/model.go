package movies

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Media pipeline entities and the persistence surface the service programs
// against. This is a pure (no I/O) definition: the structs are data and Repo
// is the interface. The concrete repository lives in the postgres adapter so
// this package stays free of database imports.

// Movie is a tracked movie. v1 identifies a movie by title + year. Provider
// metadata (TMDB/IMDB ids) is added with the providers layer in a later
// milestone; until then title+year is the identity.
//
// R1 (Radarr parity): the enrichment fields (Certification .. AlternativeTitles)
// are display-only, filled by the metadata provider (TMDB) on the detail read
// path — they stay zero/nil without a provider key, and the UI renders
// em-dashes. Editions/Files/Status/Path/Size are the Radarr-v6-style embedded
// file state (a fresh movie has one seeded 'Theatrical' edition and no files).
type Movie struct {
	ID             int64
	Title          string
	Year           int
	QualityProfile string // name of a quality profile (see BuiltInProfiles)
	Monitored      bool
	AddedAt        time.Time

	// Enrichment (TMDB detail fetch; nil/empty without a provider).
	Certification     *string
	Runtime           *int // minutes
	Studio            *string
	Genres            []string
	OriginalLanguage  *string // ISO language code
	CollectionName    *string
	CollectionID      string // TMDB collection id; '' when unknown (persisted, R3)
	Overview          *string
	Scores            *MovieScores
	Cast              []MovieCast
	Crew              []MovieCrew
	AlternativeTitles []AlternativeTitle

	// File/edition state (R1).
	Path     *string     // relative file path under the media root; nil when no file
	Status   MovieStatus // derived; see DeriveMovieStatus
	Size     *int64      // total bytes of the movie's files; nil when no file
	Editions []Edition
	Files    []MovieFile

	// Add-flow fields (R2). Empty when unset: pre-R2 movies report empty
	// tags, '' root folder and '' minimum availability.
	Tags                []string
	RootFolder          string // root folder path; '' = server default
	MinimumAvailability string // see the Availability* constants; '' = unset

	// SeedEditionMonitored is the monitored state the repository gives the
	// 'Theatrical' edition it seeds on create (R2 edition-aware monitor).
	// Nil keeps the R1 default (monitored). An input to create, not part of
	// the API payload.
	SeedEditionMonitored *bool `json:"-"`

	// ReleaseDate is the provider release date (enrichment only, not part of
	// the API payload); it feeds the 'unreleased' status derivation.
	ReleaseDate *time.Time `json:"-"`
}

// Minimum-availability values for the R2 add flow (and ” when unset).
const (
	AvailabilityAnnounced    = "announced"
	AvailabilityPosters      = "posters"
	AvailabilityInProduction = "in_production"
	AvailabilityReleased     = "released"
)

// MonitorMode is the edition-aware monitor selector of the R2 add flow.
type MonitorMode string

const (
	MonitorAll         MonitorMode = "all"
	MonitorMovieOnly   MonitorMode = "movie_only"
	MonitorUnmonitored MonitorMode = "unmonitored"
	MonitorAllExcept3D MonitorMode = "all_except_3d"
)

// MonitorPlan is one resolved add: the movie-level monitored flag plus the
// per-edition monitored predicate (Radarr v6 edition semantics).
type MonitorPlan struct {
	MovieMonitored bool
	// EditionMonitored decides the monitored state of seeded/created editions
	// by their label (case-insensitive '3d' check for all_except_3d).
	EditionMonitored func(label string) bool
}

// MonitorPlanFor resolves a monitor mode. An empty mode defaults to all
// (the add-flow default); any other unknown mode errors so the API can map
// it to a 400.
func MonitorPlanFor(mode MonitorMode) (MonitorPlan, error) {
	switch mode {
	case "", MonitorAll:
		return MonitorPlan{MovieMonitored: true, EditionMonitored: func(string) bool { return true }}, nil
	case MonitorMovieOnly:
		return MonitorPlan{MovieMonitored: true, EditionMonitored: func(string) bool { return false }}, nil
	case MonitorUnmonitored:
		return MonitorPlan{MovieMonitored: false, EditionMonitored: func(string) bool { return false }}, nil
	case MonitorAllExcept3D:
		return MonitorPlan{
			MovieMonitored: true,
			EditionMonitored: func(label string) bool {
				return !strings.Contains(strings.ToLower(label), "3d")
			},
		}, nil
	default:
		return MonitorPlan{}, fmt.Errorf("movies: unknown monitor mode %q", mode)
	}
}

// MovieScores are external ratings; every field nil when unknown.
type MovieScores struct {
	TmdbPct        *float64 // vote average scaled to 0-100
	Imdb           *float64 // 0-10 scale
	RottenTomatoes *float64 // 0-100
	Metacritic     *float64 // 0-100
}

// MovieCast is one credited cast member.
type MovieCast struct {
	Name string
	Role string
}

// MovieCrew is one key crew member.
type MovieCrew struct {
	Name string
	Job  string
}

// AlternativeTitle is an also-known-as title.
type AlternativeTitle struct {
	Title string
	Type  string
}

// MovieStatus is a movie's derived status (Radarr v6 status chips).
type MovieStatus string

const (
	StatusDownloadedMonitored   MovieStatus = "downloaded:monitored"
	StatusDownloadedUnmonitored MovieStatus = "downloaded:unmonitored"
	StatusMissingMonitored      MovieStatus = "missing:monitored"
	StatusMissingUnmonitored    MovieStatus = "missing:unmonitored"
	StatusQueued                MovieStatus = "queued"
	StatusUnreleased            MovieStatus = "unreleased"
)

// DeriveMovieStatus computes a movie's status from its wanted/queue/file
// state and (optionally) the provider release date. Precedence:
//  1. downloaded — the wanted row is satisfied (a file was imported),
//     split by the movie's monitored flag;
//  2. queued — an active (queued/downloading) queue entry exists;
//  3. unreleased — the provider release date is in the future (no key, no
//     fetch, or a past date simply skips this case);
//  4. missing — everything else, split by the monitored flag.
func DeriveMovieStatus(wantedSatisfied, inQueue, monitored bool, releaseDate *time.Time, now time.Time) MovieStatus {
	if wantedSatisfied {
		if monitored {
			return StatusDownloadedMonitored
		}
		return StatusDownloadedUnmonitored
	}
	if inQueue {
		return StatusQueued
	}
	if releaseDate != nil && releaseDate.After(now) {
		return StatusUnreleased
	}
	if monitored {
		return StatusMissingMonitored
	}
	return StatusMissingUnmonitored
}

// MovieEnricher fills the display-only enrichment fields on a movie from a
// metadata provider (TMDB detail + credits + external ids). Optional: the
// read paths run without one (nil Enricher in the service deps), in which
// case the enrichment fields stay empty and the UI degrades to em-dashes.
// Implementations must be tolerant of partial data and never fail the read
// for a missing key.
type MovieEnricher interface {
	EnrichMovie(ctx context.Context, m *Movie) error
}

// Edition is a distinct edition of a movie (Radarr v6 editions). A freshly
// added movie is seeded with one 'Theatrical' edition, monitored, with the
// movie's quality profile.
type Edition struct {
	ID        int64
	MovieID   int64
	Label     string
	Monitored bool
	Quality   string // quality profile name for this edition
	Size      int64  // total bytes of the edition's files; 0 when none
	CreatedAt time.Time
}

// MovieFile is an imported movie file (Radarr v6 movie-file shape).
type MovieFile struct {
	ID           int64
	MovieID      int64
	EditionID    *int64 // nil = unassigned to an edition
	RelativePath string // relative to the media root
	VideoCodec   string
	AudioInfo    string
	Size         int64
	Languages    []string
	Quality      string // parsed quality label (e.g. 1080p)
	ReleaseGroup string
	Formats      []string
}

// MovieStats is the Movies-page stats bar.
type MovieStats struct {
	MovieCount       int64
	FileCount        int64
	MonitoredCount   int64
	UnmonitoredCount int64
	TotalSize        int64
}

// MovieCollection is a library view of one TMDB collection (Radarr R3
// collections page): the library's member movies plus a best-effort count of
// collection parts that are not in the library.
type MovieCollection struct {
	// ID is the TMDB collection id, or "unknown" when the library only knows
	// the collection by name (no id was persisted).
	ID string
	// Name is the collection name; "" when unknown.
	Name string
	// Movies are the library members (full list-read objects).
	Movies []Movie
	// MissingCount is the number of collection parts not in the library;
	// 0 when the collection id is unknown or TMDB is unavailable (keyless).
	MissingCount int
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

// ErrEditionNotFound is returned when an edition id does not exist (or does
// not belong to the movie it was requested for).
var ErrEditionNotFound = errors.New("movies: edition not found")

// Repo is the persistence surface the movie pipeline needs. Implemented by the
// postgres adapter; tests can satisfy it with a fake. The service programs
// against this interface, never against *sql.DB.
type Repo interface {
	CreateMovie(ctx context.Context, m Movie) (int64, error)
	GetMovie(ctx context.Context, id int64) (*Movie, error)
	ListMovies(ctx context.Context) ([]Movie, error)
	SetMovieMonitored(ctx context.Context, id int64, monitored bool) error

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

	// CreateEdition inserts an edition for a movie and returns its id.
	// (movie_id, label) is unique — the create path uses DO NOTHING so a
	// re-create is a no-op.
	CreateEdition(ctx context.Context, e Edition) (int64, error)

	// GetEdition returns the edition with the given id, or ErrEditionNotFound.
	GetEdition(ctx context.Context, id int64) (*Edition, error)

	// ListEditions returns every edition (all movies), ordered by
	// movie_id then id. The service groups them per movie in memory.
	ListEditions(ctx context.Context) ([]Edition, error)

	// SetEditionMonitored toggles an edition's monitored flag.
	SetEditionMonitored(ctx context.Context, id int64, monitored bool) error

	// CreateMovieFile records an imported movie file and returns its id.
	CreateMovieFile(ctx context.Context, f MovieFile) (int64, error)

	// ListMovieFiles returns every movie file (all movies), ordered by
	// movie_id then id.
	ListMovieFiles(ctx context.Context) ([]MovieFile, error)

	// GetMovieStats computes the Movies-page stats bar in one query.
	GetMovieStats(ctx context.Context) (MovieStats, error)

	// SetMovieCollection persists the TMDB collection a movie belongs to
	// (Radarr R3). Called by the enricher when the provider reports a
	// collection. Best-effort: a failure must never fail the read that
	// triggered it. ''/'' clears an unknown collection.
	SetMovieCollection(ctx context.Context, id int64, collectionID, collectionName string) error
}
