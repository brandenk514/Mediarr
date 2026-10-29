package indexers

import (
	"context"
	"time"
)

// QueryLog is one recorded manual-search attempt — a search_queries row. It is
// written once per attempted indexer by POST /api/v1/search, including failed
// indexers (a failure still leaves a row so the history reflects the attempt).
type QueryLog struct {
	ID         int64
	IndexerID  *int64 // nil when the indexer could not be built (no resolved id)
	Indexer    string // indexer name ("" for a failed build)
	Term       string
	Categories []string
	OK         bool
	ElapsedMS  int64
	At         time.Time
}

// GrabLog is one recorded grab — a search_grabs row. It is written on a
// successful POST /api/v1/search/grab and feeds the stats totals and the grabs
// time series.
type GrabLog struct {
	Indexer  string
	Title    string
	Protocol string
	At       time.Time
}

// QueryFilter narrows a history listing (GET /api/v1/search/history). All
// fields are optional; empty/zero means "no constraint".
type QueryFilter struct {
	// Indexer is an exact indexer-name match ("" = any indexer).
	Indexer string
	// Term is a case-insensitive substring match on the query term ("" = any).
	Term string
	// Page is 1-based (values < 1 are clamped to 1).
	Page int
	// PageSize is the page size (clamped to a sane bound by the caller).
	PageSize int
}

// DailyCount is a raw (indexer, kind, UTC-day) count for the stats window. It
// is the unit the stats aggregation consumes: Indexer is "" for the aggregate
// series and a concrete name for a per-indexer series; Kind is "query" or
// "grab".
type DailyCount struct {
	Indexer string
	Kind    string
	Day     time.Time // UTC, truncated to the day
	Count   int
}

// SearchActivityRepo persists manual-search query history and grabs, and
// aggregates them for the stats summary. It is deliberately separate from
// IndexerRepo: it stores *activity* (per-attempt rows), not the indexer
// definitions. Retention/expiry of the query history is PR6's "Clean Up
// History" task; for now only a bulk DeleteAllQueries (the Prowlarr "Clear"
// action) is exposed.
type SearchActivityRepo interface {
	// RecordQuery appends one query-history row.
	RecordQuery(ctx context.Context, q QueryLog) error
	// RecordGrab appends one grab row.
	RecordGrab(ctx context.Context, g GrabLog) error
	// ListQueries returns one page of query history (newest first) plus the
	// total number of rows matching the filter (across all pages).
	ListQueries(ctx context.Context, f QueryFilter) ([]QueryLog, int, error)
	// DeleteAllQueries removes every query-history row and returns how many
	// were deleted (the Prowlarr "Clear" action). It does not touch grabs.
	DeleteAllQueries(ctx context.Context) (int, error)
	// Totals returns the all-time counts of query attempts and grabs.
	Totals(ctx context.Context) (totalQueries, totalGrabs int, err error)
	// DailyCounts returns the per-(indexer,kind,UTC-day) counts in [since,
	// now). An Indexer of "" is the aggregate across all indexers. The caller
	// is responsible for zero-filling the day buckets (see the API layer's
	// stats aggregation).
	DailyCounts(ctx context.Context, since time.Time) ([]DailyCount, error)
}
