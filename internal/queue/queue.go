// Package queue is the read-only data surface for the dedicated queue view
// (M6 #43): the *full* download/import queue across all four kinds, filtered
// and paginated server-side, plus the per-parent history a queue row drills
// into. It is a pure package (no I/O): it holds the wire/domain types and the
// Repo interface that the Postgres adapter implements and the HTTP layer
// consumes. It mirrors internal/dashboard in shape but is a separate concern:
// the dashboard shows a bounded "recent" tail, the queue view shows the whole
// set with filters, and the two must never depend on each other.
package queue

import (
	"context"
	"strings"
	"time"
)

// Kind is a canonical media kind name. It matches dashboard.Kind's values so
// a queue row and a dashboard row for the same job share a kind string, but it
// is declared here so the queue package stays self-contained.
type Kind string

const (
	KindMovie Kind = "movie"
	KindTV    Kind = "tv"
	KindMusic Kind = "music"
	KindBook  Kind = "book"
)

// Item is one download/import job, unified across the four kinds. ParentID is
// the top-level media item the job belongs to (movie id, series id, artist
// id, author id); the queue view drills into that parent's history from it.
type Item struct {
	Kind           Kind   `json:"kind"`
	ParentID       int64  `json:"parent_id"`
	ParentName     string `json:"parent_name"`
	ReleaseTitle   string `json:"release_title"`
	Indexer        string `json:"indexer"`
	State          string `json:"state"`
	Progress       int    `json:"progress"`
	DownloadClient string `json:"download_client"`
	// L4 music display columns (the Lidarr queue row: Artist / Album Title /
	// Quality / Formats / Track Title / Date; Artist is ParentName, Date is
	// UpdatedAt). Populated for music rows; every other kind keeps "" — the
	// fields are always present on the wire so the row shape is stable.
	AlbumTitle string    `json:"album_title"`
	Quality    string    `json:"quality"`
	Formats    string    `json:"formats"`
	TrackTitle string    `json:"track_title"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// HistoryEvent is one lifecycle event for a parent media item — the content of
// a queue row's "history" drill-down. It is the same (event, detail, at)
// triple the per-kind history tables store, surfaced without the kind/parent
// that the drill-down already knows.
type HistoryEvent struct {
	Event  string    `json:"event"`
	Detail string    `json:"detail"`
	At     time.Time `json:"at"`
}

// Filter narrows and pages a queue listing. Empty Kind/State mean "all"; an
// empty Q matches everything. Page is 1-based.
type Filter struct {
	Kind     string
	State    string
	Q        string
	Page     int
	PageSize int
}

// DefaultPageSize is used when a caller does not pass ?page_size.
const DefaultPageSize = 25

// MaxPageSize bounds ?page_size so a hostile or buggy client cannot request an
// unreasonably large page (mirrors paging.MaxPageSize).
const MaxPageSize = 100

// Normalize fills defaults (page 1, size DefaultPageSize) and clamps PageSize
// to [1, MaxPageSize] in place. Call it before offset math so out-of-range
// input can never panic or underflow.
func (f *Filter) Normalize() {
	if f.Page <= 0 {
		f.Page = 1
	}
	if f.PageSize <= 0 {
		f.PageSize = DefaultPageSize
	}
	if f.PageSize > MaxPageSize {
		f.PageSize = MaxPageSize
	}
}

// Apply narrows items to those matching the filter and returns the requested
// page plus the total number of matches (before paging). Q is a
// case-insensitive substring match against the release title or the parent
// name. The returned page is always non-nil (an empty result encodes as []
// rather than null).
func Apply(items []Item, f Filter) (page []Item, total int) {
	f.Normalize()
	f.Kind = strings.ToLower(f.Kind)
	f.State = strings.ToLower(f.State)
	needle := strings.ToLower(strings.TrimSpace(f.Q))

	matched := make([]Item, 0, len(items))
	for _, it := range items {
		if f.Kind != "" && string(it.Kind) != f.Kind {
			continue
		}
		if f.State != "" && strings.ToLower(it.State) != f.State {
			continue
		}
		if needle != "" &&
			!strings.Contains(strings.ToLower(it.ReleaseTitle), needle) &&
			!strings.Contains(strings.ToLower(it.ParentName), needle) {
			continue
		}
		matched = append(matched, it)
	}
	total = len(matched)
	start := (f.Page - 1) * f.PageSize
	if start >= total {
		return []Item{}, total
	}
	end := start + f.PageSize
	if end > total {
		end = total
	}
	return matched[start:end], total
}

// Repo is the queue read/write surface. List/History must not mutate state;
// the P3 queue actions (Grab/Remove) are the only mutators.
type Repo interface {
	// List returns every queue job across all four kinds, joined to the
	// parent's human name, ordered newest-updated-first. Callers filter and
	// page the result with Apply.
	List(ctx context.Context) ([]Item, error)
	// History returns the lifecycle events for one parent media item, newest
	// first, capped at limit (a non-positive limit means "use the
	// implementation default").
	History(ctx context.Context, kind Kind, parentID int64, limit int) ([]HistoryEvent, error)
	// Grab promotes every *queued* job for the parent to downloading (the
	// client hand-off) and returns how many jobs moved.
	Grab(ctx context.Context, kind Kind, parentID int64) (int, error)
	// Remove deletes every queue job for the parent, in any state, and
	// returns how many jobs were removed.
	Remove(ctx context.Context, kind Kind, parentID int64) (int, error)
}
