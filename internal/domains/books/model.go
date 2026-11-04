// Package books is the pure domain model for the books library (M4): the
// author → title → edition hierarchy plus the pipeline state (wanted, queue,
// history) that drives the search → download → import flow. It is pure: no
// database, file, or network I/O. Concrete behaviour lives in the repository
// (postgres) and the service (services/books), which implement/program the
// interfaces here — the same thin-common-interface pattern the movies, TV,
// and music domains use (PLAN §4: per-kind tables + a thin common interface).
//
// Identity: v1 identifies an author by name. A title is identified within its
// author by name; an edition within its title by (format, isbn). Wanted items
// carry the title or edition specifier so monitoring works at both title and
// edition granularity (mirroring music's album/track granularity).
package books

import (
	"context"
	"errors"
	"time"
)

// Author is a tracked book author.
type Author struct {
	ID        int64
	Name      string
	Monitored bool // author-level monitor toggle
	// Readarr B2 add-flow fields:
	MonitorPolicy   string   // which titles get monitored (PolicyAll/...); "" = all
	QualityProfile  string   // the author's default book quality profile name; "" = none
	MetadataProfile string   // the author's metadata profile name; "" = none
	RootFolder      string   // root folder path for the author's files; "" = server default
	Tags            []string // free-form tag labels; empty = none
	AddedAt         time.Time
}

// Title is a book title (a logical book, e.g. "The Dispossessed") within an
// author. A title has zero or more editions (physical/digital variants).
type Title struct {
	ID        int64
	AuthorID  int64
	Name      string
	Monitored bool // title-level monitor toggle
	AddedAt   time.Time
}

// Edition is a specific variant of a title, identified by its ISBN within the
// title. ISBN, Publisher, Year, and Pages are optional (""/0) because a title
// may have editions without all metadata.
//
// Readarr B1: the edition is the (format, language, monitored) triple — the
// Books analogue of Radarr's edition model. Language is the edition's
// language code (e.g. "en"); "" = unknown. Data entry stays manual in B1
// (the user adds editions); the model and API are ready to hold provider-fed
// editions once the MT2 resolution subtask lands.
type Edition struct {
	ID        int64
	TitleID   int64
	Format    string // epub / mobi / azw3 (see format.go)
	Language  string // edition language code (e.g. "en"); "" = unknown
	ISBN      string // "" = unset
	Publisher string // "" = unset
	Year      int    // 0 = unset
	Pages     int    // 0 = unset
	Monitored bool   // edition-level monitor toggle
	AddedAt   time.Time
}

// BookFile is an imported e-book file and its media info (Readarr B1). A file
// belongs to a title; EditionID optionally ties it to the edition it matches
// (0 = no known edition match). The import pipeline records the file after a
// successful import; there is no manual file entry in B1 (unmapped-file
// import is B3).
type BookFile struct {
	ID        int64
	TitleID   int64
	EditionID int64 // 0 = not tied to a specific edition
	Path      string
	Format    string // one of the e-book format codes (e.g. "epub")
	Size      int64  // bytes; 0 = unknown
	Pages     int    // 0 = unknown
	Language  string // edition language; "" = unknown
	AddedAt   time.Time
}

// Series is a named collection of books (a book can belong to many series; a
// series can contain many books — Readarr's "Series", the basis of B4's
// bookshelves). Membership is entered manually in B1.
type Series struct {
	ID      int64
	Name    string
	AddedAt time.Time
}

// Book is one row of the flat Books index: a title joined with its author's
// name (GET /api/v1/books).
type Book struct {
	Title
	AuthorName string
}

// SeriesBook is a book in a series, with its author's name (the series read
// surface: list the books in a series).
type SeriesBook struct {
	Title
	AuthorName string
}

// AuthorSeries is a series the author's books belong to, with the names of
// the member titles by this author (the author detail's series links).
type AuthorSeries struct {
	Series
	Titles []string
}

// WantedStatus is the lifecycle of a wanted request.
type WantedStatus string

const (
	WantedPending   WantedStatus = "pending"
	WantedSatisfied WantedStatus = "satisfied"
)

// Wanted is a "we want a file" request at title or edition granularity.
// EditionID == 0 is a whole-title request; EditionID > 0 is a specific edition.
type Wanted struct {
	TitleID      int64
	EditionID    int64 // 0 = whole title
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

// QueueEntry is one download job in the queue (a book release).
type QueueEntry struct {
	ID             int64
	AuthorID       int64
	TitleID        int64
	ReleaseTitle   string
	Indexer        string
	DownloadClient string
	State          QueueState
	Progress       int
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// HistoryEntry is an auditable event in an author's library lifecycle.
type HistoryEntry struct {
	ID       int64
	AuthorID int64
	Event    string // "added", "monitored", "searched", "download", "import", ...
	Detail   string
	At       time.Time
}

// ErrAuthorNotFound is returned when an author id does not exist.
var ErrAuthorNotFound = errors.New("books: author not found")

// ErrTitleNotFound is returned when a title does not exist.
var ErrTitleNotFound = errors.New("books: title not found")

// ErrEditionNotFound is returned when an edition does not exist.
var ErrEditionNotFound = errors.New("books: edition not found")

// ErrSeriesNotFound is returned when a series id does not exist.
var ErrSeriesNotFound = errors.New("books: series not found")

// ErrCollision is returned when an import target file already exists on disk;
// the existing file is never overwritten (PLAN §6/§8).
var ErrCollision = errors.New("books: import: destination file already exists")

// Repo is the persistence surface the books pipeline needs. Implemented by the
// postgres adapter; tests can satisfy it with a fake. The service programs
// against this interface, never against *sql.DB.
type Repo interface {
	// Authors
	CreateAuthor(ctx context.Context, a Author) (int64, error)
	GetAuthor(ctx context.Context, id int64) (*Author, error)
	ListAuthors(ctx context.Context) ([]Author, error)
	SetAuthorMonitored(ctx context.Context, id int64, monitored bool) error
	// UpdateAuthor writes the author's B2 editable fields (the full merged
	// author; the service passes current values plus the edits).
	UpdateAuthor(ctx context.Context, a Author) error

	// Titles
	CreateTitle(ctx context.Context, t Title) (int64, error)
	GetTitle(ctx context.Context, id int64) (*Title, error)
	ListTitles(ctx context.Context, authorID int64) ([]Title, error)
	SetTitleMonitored(ctx context.Context, id int64, monitored bool) error

	// Editions
	CreateEdition(ctx context.Context, e Edition) (int64, error)
	GetEdition(ctx context.Context, id int64) (*Edition, error)
	ListEditions(ctx context.Context, titleID int64) ([]Edition, error)
	SetEditionMonitored(ctx context.Context, id int64, monitored bool) error

	// Book files (imported e-book files and their media info)
	CreateBookFile(ctx context.Context, f BookFile) (int64, error)
	ListBookFilesByTitle(ctx context.Context, titleID int64) ([]BookFile, error)
	ListBookFilesByEdition(ctx context.Context, editionID int64) ([]BookFile, error)

	// Flat Books index (every book across all authors, with author names)
	ListBooks(ctx context.Context) ([]Book, error)

	// Series (books <-> series links)
	CreateSeries(ctx context.Context, s Series) (int64, error) // idempotent by name
	GetSeries(ctx context.Context, id int64) (*Series, error)
	ListSeries(ctx context.Context) ([]Series, error)
	ListSeriesBooks(ctx context.Context, seriesID int64) ([]SeriesBook, error)
	ListTitleSeries(ctx context.Context, titleID int64) ([]Series, error)
	ListAuthorSeries(ctx context.Context, authorID int64) ([]AuthorSeries, error)
	LinkTitleToSeries(ctx context.Context, titleID, seriesID int64) error // idempotent

	// Wanted (title or edition granularity)
	EnsureWantedTitle(ctx context.Context, titleID int64) error
	EnsureWantedEdition(ctx context.Context, titleID, editionID int64) error
	GetWanted(ctx context.Context, titleID, editionID int64) (*Wanted, error)
	ListWanted(ctx context.Context, titleID int64) ([]Wanted, error)
	MarkWantedSatisfied(ctx context.Context, titleID, editionID int64, releaseTitle string) error

	// Queue
	CreateQueue(ctx context.Context, e QueueEntry) (int64, error)
	UpdateQueue(ctx context.Context, e QueueEntry) error
	GetQueue(ctx context.Context, id int64) (*QueueEntry, error)
	ListQueue(ctx context.Context, authorID int64) ([]QueueEntry, error)

	// History
	AddHistory(ctx context.Context, e HistoryEntry) error
	ListHistory(ctx context.Context, authorID int64, limit int) ([]HistoryEntry, error)
}
