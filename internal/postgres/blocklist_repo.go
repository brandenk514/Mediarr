package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/brandenk514/mediarr/internal/domains/blocklist"
)

// BlocklistRepo is the Postgres adapter for blocklist.Repo (P3 Activity >
// Blocklist). It stores the blocked release and resolves the parent's human
// name at read time (the four parent tables are joined by kind, the same
// COALESCE-by-kind pattern the dashboard repo uses for recent items).
type BlocklistRepo struct {
	db *sql.DB
}

// NewBlocklistRepo wraps a *sql.DB as a blocklist.Repo.
func NewBlocklistRepo(db *sql.DB) *BlocklistRepo { return &BlocklistRepo{db: db} }

var _ blocklist.Repo = (*BlocklistRepo)(nil)

const blocklistSelectSQL = `
SELECT b.id, b.kind, b.parent_id,
       COALESCE(
         CASE b.kind
           WHEN 'tv'    THEN s.title
           WHEN 'movie' THEN m.title
           WHEN 'music' THEN a.name
           WHEN 'book'  THEN au.name
         END, '') AS parent_name,
       b.release_title, b.indexer, b.reason,
       b.album_title, b.quality, b.formats,
       b.created_at
FROM blocklist b
LEFT JOIN tv_series s    ON b.kind = 'tv'    AND s.id = b.parent_id
LEFT JOIN movies m       ON b.kind = 'movie' AND m.id = b.parent_id
LEFT JOIN music_artists a ON b.kind = 'music' AND a.id = b.parent_id
LEFT JOIN book_authors au ON b.kind = 'book'  AND au.id = b.parent_id
`

// List implements blocklist.Repo: the blocked releases matching f, newest
// first. An empty blocklist (or a filter matching nothing) is an empty,
// non-nil slice.
func (r *BlocklistRepo) List(ctx context.Context, f blocklist.Filter) ([]blocklist.Entry, error) {
	q := blocklistSelectSQL
	conds := make([]string, 0, 2)
	args := make([]any, 0, 2)
	if f.Kind != "" {
		conds = append(conds, "b.kind = $")
		args = append(args, string(f.Kind))
	}
	if f.ParentID > 0 {
		conds = append(conds, "b.parent_id = $")
		args = append(args, f.ParentID)
	}
	for i, c := range conds {
		conds[i] = fmt.Sprintf("%s%d", c, i+1)
	}
	if len(conds) > 0 {
		q += "WHERE " + strings.Join(conds, " AND ")
	}
	q += " ORDER BY b.created_at DESC, b.id DESC"

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("blocklist: query: %w", err)
	}
	defer func() { _ = rows.Close() }() // rows consumed by the loop below

	var out []blocklist.Entry
	for rows.Next() {
		var (
			id         int64
			kind       string
			parentID   int64
			parent     string
			release    string
			indexer    string
			reason     string
			albumTitle string
			quality    string
			formats    string
			createdAt  time.Time
		)
		if err := rows.Scan(&id, &kind, &parentID, &parent, &release, &indexer, &reason,
			&albumTitle, &quality, &formats, &createdAt); err != nil {
			return nil, fmt.Errorf("blocklist: scan: %w", err)
		}
		out = append(out, blocklist.Entry{
			ID:           id,
			Kind:         blocklist.Kind(kind),
			ParentID:     parentID,
			ParentName:   parent,
			ReleaseTitle: release,
			Indexer:      indexer,
			Reason:       reason,
			AlbumTitle:   albumTitle,
			Quality:      quality,
			Formats:      formats,
			CreatedAt:    createdAt,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("blocklist: iterate: %w", err)
	}
	if out == nil {
		out = []blocklist.Entry{}
	}
	return out, nil
}

// Add implements blocklist.Repo: records a blocked release. ParentName is
// ignored (it is resolved at read time).
func (r *BlocklistRepo) Add(ctx context.Context, e blocklist.Entry) (int64, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO blocklist (kind, parent_id, release_title, indexer, reason,
		                      album_title, quality, formats)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id`,
		string(e.Kind), e.ParentID, e.ReleaseTitle, e.Indexer, e.Reason,
		e.AlbumTitle, e.Quality, e.Formats,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("blocklist: insert: %w", err)
	}
	return id, nil
}

// Unblock implements blocklist.Repo: removes the entry by id, reporting
// whether a row was deleted.
func (r *BlocklistRepo) Unblock(ctx context.Context, id int64) (bool, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM blocklist WHERE id = $1`, id)
	if err != nil {
		return false, fmt.Errorf("blocklist: unblock: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("blocklist: rows affected: %w", err)
	}
	return n > 0, nil
}
