package music

import (
	"context"
	"errors"
	"time"
)

// Media pipeline entities and the persistence surface the music service
// programs against. This is a pure (no I/O) definition: the structs are data
// and Repo is the interface. The concrete repository lives in the postgres
// adapter so this package stays free of database imports — the same
// thin-common-interface pattern the movies and TV domains use (PLAN §4:
// per-kind tables + a thin common interface).
//
// Identity: v1 identifies an artist by name (the MusicBrainz id is carried
// but nullable until the provider lands in M5). An album is identified within
// its artist by (name, year); a track within its album by (disc, number).
// Wanted items carry the album or track specifier so monitoring works at both
// album and track granularity (PLAN §4).

// Artist is a tracked music artist.
type Artist struct {
	ID        int64
	MBID      string // MusicBrainz artist id ("" = unset)
	Name      string
	Monitored bool // artist-level monitor toggle
	AddedAt   time.Time
}

// Album is a known album within an artist.
type Album struct {
	ID              int64
	ArtistID        int64
	MBID            string // MusicBrainz release id ("" = unset)
	Name            string
	Year            int
	QualityProfile  string // name of a quality profile (see BuiltInProfiles)
	Monitored       bool   // album-level monitor toggle
	VerifyChecksums bool   // per-library checksum opt-out (PLAN §15.5); default true
	Checksum        string // expected sha256 of the whole-album file, when known
	AddedAt         time.Time
}

// Track is a single known track within an album.
type Track struct {
	ID        int64
	AlbumID   int64
	MBID      string // MusicBrainz track id ("" = unset)
	Disc      int    // disc number (1-based); single-disc albums use 1
	Number    int    // track number on its disc (1-based)
	Title     string
	Checksum  string // expected sha256 of the file, when known
	Monitored bool   // track-level monitor toggle
	AddedAt   time.Time
}

// WantedStatus is the lifecycle of a wanted request.
type WantedStatus string

const (
	WantedPending   WantedStatus = "pending"
	WantedSatisfied WantedStatus = "satisfied"
)

// Wanted is a "we want a file" request at album or track granularity.
// TrackID == 0 is a whole-album request; TrackID > 0 is a specific track.
type Wanted struct {
	AlbumID      int64
	TrackID      int64 // 0 = whole album
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

// QueueEntry is one download job in the queue (an album release).
type QueueEntry struct {
	ID             int64
	ArtistID       int64
	AlbumID        int64
	ReleaseTitle   string
	Indexer        string
	DownloadClient string
	State          QueueState
	Progress       int
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// HistoryEntry is an auditable event in an artist's lifecycle.
type HistoryEntry struct {
	ID       int64
	ArtistID int64
	Event    string // "added", "monitored", "searched", "download", "import", "checksum", ...
	Detail   string
	At       time.Time
}

// ErrArtistNotFound is returned when an artist id does not exist.
var ErrArtistNotFound = errors.New("music: artist not found")

// ErrAlbumNotFound is returned when an album does not exist.
var ErrAlbumNotFound = errors.New("music: album not found")

// ErrTrackNotFound is returned when a track does not exist.
var ErrTrackNotFound = errors.New("music: track not found")

// ErrCollision is returned when an import target file already exists on disk;
// the existing file is never overwritten (PLAN §6/§8).
var ErrCollision = errors.New("music: import: destination file already exists")

// ErrChecksumMismatch is returned when an imported file's hash does not match
// the expected checksum.
var ErrChecksumMismatch = errors.New("music: import: checksum mismatch")

// Repo is the persistence surface the music pipeline needs. Implemented by the
// postgres adapter; tests can satisfy it with a fake. The service programs
// against this interface, never against *sql.DB.
type Repo interface {
	// Artists
	CreateArtist(ctx context.Context, a Artist) (int64, error)
	GetArtist(ctx context.Context, id int64) (*Artist, error)
	ListArtists(ctx context.Context) ([]Artist, error)
	SetArtistMonitored(ctx context.Context, id int64, monitored bool) error

	// Albums
	CreateAlbum(ctx context.Context, a Album) (int64, error)
	GetAlbum(ctx context.Context, id int64) (*Album, error)
	ListAlbums(ctx context.Context, artistID int64) ([]Album, error)
	SetAlbumMonitored(ctx context.Context, id int64, monitored bool) error

	// Tracks
	CreateTrack(ctx context.Context, t Track) (int64, error)
	GetTrack(ctx context.Context, albumID int64, disc, number int) (*Track, error)
	ListTracks(ctx context.Context, albumID int64) ([]Track, error)
	SetTrackMonitored(ctx context.Context, albumID int64, disc, number int, monitored bool) error

	// Wanted (album or track granularity)
	EnsureWantedAlbum(ctx context.Context, albumID int64) error
	EnsureWantedTrack(ctx context.Context, albumID, trackID int64) error
	GetWanted(ctx context.Context, albumID, trackID int64) (*Wanted, error)
	ListWanted(ctx context.Context, albumID int64) ([]Wanted, error)
	MarkWantedSatisfied(ctx context.Context, albumID, trackID int64, releaseTitle string) error

	// Queue
	CreateQueue(ctx context.Context, e QueueEntry) (int64, error)
	UpdateQueue(ctx context.Context, e QueueEntry) error
	GetQueue(ctx context.Context, id int64) (*QueueEntry, error)
	ListQueue(ctx context.Context, artistID int64) ([]QueueEntry, error)

	// History
	AddHistory(ctx context.Context, e HistoryEntry) error
	ListHistory(ctx context.Context, artistID int64, limit int) ([]HistoryEntry, error)
}
