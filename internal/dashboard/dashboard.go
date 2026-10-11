// Package dashboard defines the read-only data surface for the UI dashboard
// (M6, #40): a cross-library snapshot of library sizes, active wants, queue
// depth, recent history, and the most recent queue jobs. It is a pure package
// (no I/O): it holds the wire/domain types and the Repo interface that the
// Postgres adapter implements and the HTTP layer consumes. Keeping it separate
// from both keeps the dependency rule intact — postgres and api depend on
// dashboard, never on each other.
package dashboard

import (
	"context"
	"time"
)

// Kind is a canonical media kind name used across the snapshot.
type Kind string

const (
	KindMovie Kind = "movie"
	KindTV    Kind = "tv"
	KindMusic Kind = "music"
	KindBook  Kind = "book"
)

// Library is the per-kind rollup shown as a card on the dashboard: how many
// top-level items the library holds, how many are actively wanted, and how
// many queue jobs are in flight for that kind.
type Library struct {
	// Kind is the canonical kind name (movie/tv/music/book).
	Kind Kind `json:"kind"`
	// Label is the human-facing name for the UI ("Movies", "TV", ...).
	Label string `json:"label"`
	// Count is the number of top-level library items (movies, series, artists,
	// authors).
	Count int64 `json:"count"`
	// Wants is the number of pending (unsatisfied) want rows for the kind.
	Wants int64 `json:"wants"`
	// Queue is the number of in-flight queue jobs for the kind (queued or
	// downloading; complete/failed are not "in flight").
	Queue int64 `json:"queue"`
}

// Totals is the cross-library sum of the per-kind counts.
type Totals struct {
	Libraries int64 `json:"libraries"`
	Wants     int64 `json:"wants"`
	Queue     int64 `json:"queue"`
}

// HistoryItem is one recent history row, unified across all four kinds. Name
// is the parent's human name (movie title, series title, artist name, author
// name) so the feed is readable without cross-referencing ids.
type HistoryItem struct {
	Kind   Kind      `json:"kind"`
	Name   string    `json:"name"`
	Event  string    `json:"event"`
	Detail string    `json:"detail"`
	At     time.Time `json:"at"`
}

// QueueItem is one recent queue job, unified across all four kinds.
type QueueItem struct {
	Kind         Kind      `json:"kind"`
	ReleaseTitle string    `json:"release_title"`
	Indexer      string    `json:"indexer"`
	State        string    `json:"state"`
	Progress     int       `json:"progress"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Snapshot is the full dashboard read model. It is built atomically enough for
// a dashboard: each aggregate is a single count query, and the two "recent"
// lists are bounded (the dashboard only needs the tail). A momentary
// inconsistency between a count and the tail list is not a correctness issue
// for a live overview — Postgres remains the source of truth.
type Snapshot struct {
	Libraries     []Library     `json:"libraries"`
	Totals        Totals        `json:"totals"`
	RecentHistory []HistoryItem `json:"recent_history"`
	RecentQueue   []QueueItem   `json:"recent_queue"`
	GeneratedAt   time.Time     `json:"generated_at"`
}

// Repo is the dashboard read surface. Implementations must not mutate state.
type Repo interface {
	// Snapshot returns a dashboard read model. recentHistory and recentQueue
	// are each capped at maxRecent (a non-positive value means "use the
	// implementation default").
	Snapshot(ctx context.Context, maxRecent int) (*Snapshot, error)
}
