// Package blocklist is the pure domain for the blocked (rejected) releases
// the Activity > Blocklist page lists and manages (P3). It holds the entry
// shape, the list filter, and the Repo interface the Postgres adapter
// implements and the service (internal/services/blocklist) programs against.
// No I/O lives here.
package blocklist

import (
	"context"
	"errors"
	"time"
)

// Kind validates the media kind of a blocklist entry. The four canonical
// kind names are shared with the queue/dashboard packages (declared here so
// the domain stays self-contained, the same way queue.Kind is).
type Kind string

// Errors surfaced by the blocklist service.
var (
	// ErrInvalidKind is a kind other than the four canonical media kinds.
	ErrInvalidKind = errors.New("blocklist: invalid kind")
	// ErrEmptyTitle is a blocklist entry with no release title.
	ErrEmptyTitle = errors.New("blocklist: release title is required")
	// ErrInvalidID is a non-positive entry id.
	ErrInvalidID = errors.New("blocklist: invalid id")
)

const (
	KindMovie Kind = "movie"
	KindTV    Kind = "tv"
	KindMusic Kind = "music"
	KindBook  Kind = "book"
)

// Valid reports whether k is one of the four canonical media kinds.
func (k Kind) Valid() bool {
	switch k {
	case KindMovie, KindTV, KindMusic, KindBook:
		return true
	}
	return false
}

// Entry is one blocked release. ParentName is resolved by the repository at
// read time (it is not stored — the media item it names may still change).
type Entry struct {
	ID           int64  `json:"id"`
	Kind         Kind   `json:"kind"`
	ParentID     int64  `json:"parent_id"`
	ParentName   string `json:"parent_name"`
	ReleaseTitle string `json:"release_title"`
	Indexer      string `json:"indexer"`
	Reason       string `json:"reason"`
	// L4 music display columns (the Lidarr blocklist row: Album Title /
	// Quality / Formats; Artist is ParentName). Stored for music rows; ""
	// for every other kind, so the row shape is stable on the wire.
	AlbumTitle string    `json:"album_title"`
	Quality    string    `json:"quality"`
	Formats    string    `json:"formats"`
	CreatedAt  time.Time `json:"created_at"`
}

// Filter narrows a blocklist listing. Empty fields mean "no constraint".
type Filter struct {
	Kind     Kind
	ParentID int64
}

// Apply returns the entries that pass the filter (newest-first order of the
// input is preserved; it does not sort). A zero filter passes everything.
func (f Filter) Apply(in []Entry) []Entry {
	out := make([]Entry, 0, len(in))
	for _, e := range in {
		if f.Kind != "" && e.Kind != f.Kind {
			continue
		}
		if f.ParentID > 0 && e.ParentID != f.ParentID {
			continue
		}
		out = append(out, e)
	}
	return out
}

// Repo is the blocklist persistence surface.
type Repo interface {
	// List returns the blocked releases, newest first, narrowed by f.
	List(ctx context.Context, f Filter) ([]Entry, error)
	// Add records a blocked release and returns its new id.
	Add(ctx context.Context, e Entry) (int64, error)
	// Unblock removes a blocked release by id. It reports whether a row was
	// deleted (false = unknown id).
	Unblock(ctx context.Context, id int64) (bool, error)
}
