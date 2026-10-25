package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	hf "github.com/brandenk514/mediarr/internal/domains/historyfeed"
)

// HistoryRepo is the Postgres adapter for historyfeed.Repo (P3 Activity >
// History). The four kinds keep separate *_history tables (one per kind,
// each keyed by its parent id), so the global feed is a symmetric UNION
// ALL — the same shape as the queue repo's cross-kind list — with the
// media item's human name joined in per kind.
type HistoryRepo struct {
	db *sql.DB
}

// NewHistoryRepo wraps a *sql.DB as a historyfeed.Repo.
func NewHistoryRepo(db *sql.DB) *HistoryRepo { return &HistoryRepo{db: db} }

var _ hf.Repo = (*HistoryRepo)(nil)

// List implements historyfeed.Repo: the feed events matching f, newest
// first. The four kinds keep separate *_history tables, so the base query is
// a constant symmetric UNION ALL (no user value is concatenated into the
// SQL — gosec G202). The since/event/limit narrowing is applied in memory via
// historyfeed.Filter.Apply, mirroring the queue repo's in-memory filtering.
func (r *HistoryRepo) List(ctx context.Context, f hf.Filter) ([]hf.Item, error) {
	f.Normalize()

	const q = `
SELECT kind, parent_id, name, event, detail, at, id FROM (
SELECT 'movie'::text AS kind, h.movie_id AS parent_id, m.title AS name,
       h.event, h.detail, h.at, h.id
FROM history h JOIN movies m ON m.id = h.movie_id
UNION ALL
SELECT 'tv', h.series_id, s.title,
       h.event, h.detail, h.at, h.id
FROM tv_history h JOIN tv_series s ON s.id = h.series_id
UNION ALL
SELECT 'music', h.artist_id, a.name,
       h.event, h.detail, h.at, h.id
FROM music_history h JOIN music_artists a ON a.id = h.artist_id
UNION ALL
SELECT 'book', h.author_id, au.name,
       h.event, h.detail, h.at, h.id
FROM book_history h JOIN book_authors au ON au.id = h.author_id
) feed ORDER BY at DESC, id DESC`

	rows, err := r.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("historyfeed: query: %w", err)
	}
	defer func() { _ = rows.Close() }() // rows consumed by the loop below

	var out []hf.Item
	for rows.Next() {
		var (
			kind     string
			parentID int64
			name     string
			event    string
			detail   string
			at       time.Time
			rowID    int64
		)
		if err := rows.Scan(&kind, &parentID, &name, &event, &detail, &at, &rowID); err != nil {
			return nil, fmt.Errorf("historyfeed: scan: %w", err)
		}
		out = append(out, hf.Item{
			Kind:     kind,
			ParentID: parentID,
			Name:     name,
			Event:    event,
			Detail:   detail,
			At:       at,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("historyfeed: iterate: %w", err)
	}
	// Narrow in memory (since/event/limit) — the domain owns the semantics and
	// is unit-tested independently of this adapter.
	return f.Apply(out), nil
}
