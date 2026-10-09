// Package books is the 100% pure domain model for the books media kind
// (issue #24, PLAN §4 / §15.4). It defines the author / title / edition
// hierarchy that the store and services program against, mirroring the music
// domain's artist / album / track model. This is a pure (no I/O) definition:
// the structs are data and Repo is the interface. The concrete repository
// lives in the postgres adapter so this package stays free of database imports
// — the same thin-common-interface pattern the movies, TV, and music domains
// use (PLAN §4: per-kind tables + a thin common interface).
//
// Identity: v1 identifies a book at the TITLE level (author + name). An
// EDITION is a specific publication of that title — a format (epub / mobi /
// azw3, PLAN §15.4) plus, when known, the ISBN / publisher / year / pages that
// disambiguate it from other editions of the same title. The edition is the
// unit the format matcher (#25) and the wanted/monitor granularity (#27) will
// operate on. Monitoring is carried on each level so a whole author, a whole
// title, or a single edition can be tracked.
//
// NOTE: wanted / queue / history entities and their Repo methods are
// deliberately NOT defined here; they land with the wanted/monitor (#27) and
// pipeline (#28) work, matching how this milestone slice stays focused on the
// data model.
package books

import (
	"context"
	"errors"
	"time"
)

// Author is a tracked book author (a "series" of titles), the parent of the
// hierarchy.
type Author struct {
	ID        int64
	Name      string
	Monitored bool // author-level monitor toggle
	AddedAt   time.Time
}

// Title is a specific book by an author — the identity unit. An edition is a
// particular publication of the same title (a format + publisher/year). The
// natural key is (author_id, name).
type Title struct {
	ID        int64
	AuthorID  int64
	Name      string
	Monitored bool // title-level monitor toggle
	AddedAt   time.Time
}

// Edition is a specific publication of a title: which format (epub / mobi /
// azw3, PLAN §15.4) and, when known, the ISBN / publisher / year / page count
// that disambiguate it from other editions of the same title. The Format string
// is validated by the format matcher (issue #25), not here, so the model stays
// decoupled from the accepted-format list.
type Edition struct {
	ID        int64
	TitleID   int64
	Format    string
	Publisher string
	ISBN      string
	Year      int
	Pages     int
	Monitored bool // edition-level monitor toggle
	AddedAt   time.Time
}

// Sentinel errors returned by domain-level validation. The Postgres repository
// maps its not-found SQL errors onto these same sentinels so callers get a
// consistent, typed contract regardless of the underlying store.
var (
	ErrAuthorNotFound  = errors.New("books: author not found")
	ErrTitleNotFound   = errors.New("books: title not found")
	ErrEditionNotFound = errors.New("books: edition not found")
)

// Repo is the persistence surface the books pipeline needs. Implemented by the
// postgres adapter; tests can satisfy it with a fake. The service programs
// against this interface, never against *sql.DB. This is the data-model surface
// for issue #24 (author / title / edition CRUD); the wanted / queue / history
// methods are added in #27 / #28.
type Repo interface {
	// Authors
	CreateAuthor(ctx context.Context, a Author) (int64, error)
	GetAuthor(ctx context.Context, id int64) (*Author, error)
	ListAuthors(ctx context.Context) ([]Author, error)
	SetAuthorMonitored(ctx context.Context, id int64, monitored bool) error

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
}
