package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/brandenk514/mediarr/internal/indexers"
)

// SearchActivityRepo is the Postgres-backed implementation of
// indexers.SearchActivityRepo (migration 0022). It stores manual-search
// activity — query attempts and grabs — and aggregates it for the stats
// summary. Unlike IndexersRepo it holds no secrets, so no vault is needed.
type SearchActivityRepo struct {
	db *sql.DB
}

// NewSearchActivityRepo builds the activity repository over the given pool.
func NewSearchActivityRepo(db *sql.DB) *SearchActivityRepo {
	return &SearchActivityRepo{db: db}
}

var _ indexers.SearchActivityRepo = (*SearchActivityRepo)(nil)

// encodeCategories serializes a category list for the JSONB column, coercing a
// nil list to an empty array so a query with no categories stores "[]" (not
// NULL) and round-trips back to an empty slice.
func encodeCategories(cats []string) (string, error) {
	if cats == nil {
		cats = []string{}
	}
	raw, err := json.Marshal(cats)
	if err != nil {
		return "", fmt.Errorf("search-activity: marshal categories: %w", err)
	}
	return string(raw), nil
}

func orDefaultTime(t time.Time) time.Time {
	if t.IsZero() {
		return time.Now().UTC()
	}
	return t
}

// RecordQuery appends one query-history row.
func (r *SearchActivityRepo) RecordQuery(ctx context.Context, q indexers.QueryLog) error {
	cats, err := encodeCategories(q.Categories)
	if err != nil {
		return err
	}
	at := orDefaultTime(q.At)
	if _, err := r.db.ExecContext(ctx, `
		INSERT INTO search_queries (indexer_id, indexer_name, term, categories, ok, elapsed_ms, at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		q.IndexerID, q.Indexer, q.Term, cats, q.OK, q.ElapsedMS, at); err != nil {
		return fmt.Errorf("search-activity: record query: %w", err)
	}
	return nil
}

// RecordGrab appends one grab row.
func (r *SearchActivityRepo) RecordGrab(ctx context.Context, g indexers.GrabLog) error {
	at := orDefaultTime(g.At)
	if _, err := r.db.ExecContext(ctx, `
		INSERT INTO search_grabs (indexer_name, title, protocol, at)
		VALUES ($1, $2, $3, $4)`,
		g.Indexer, g.Title, g.Protocol, at); err != nil {
		return fmt.Errorf("search-activity: record grab: %w", err)
	}
	return nil
}

// buildQueryHistoryFilter assembles the WHERE clause and args for a history
// listing. Indexer is an exact match; Term is a case-insensitive substring.
// It returns the clause (or "" for no filter) and the positional args.
func buildQueryHistoryFilter(f indexers.QueryFilter) (string, []any) {
	var conds []string
	var args []any
	if f.Indexer != "" {
		args = append(args, f.Indexer)
		conds = append(conds, fmt.Sprintf("indexer_name = $%d", len(args)))
	}
	if f.Term != "" {
		args = append(args, "%"+f.Term+"%")
		conds = append(conds, fmt.Sprintf("term ILIKE $%d", len(args)))
	}
	if len(conds) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

// ListQueries returns one page of query history (newest first) and the total
// number of rows matching the filter.
func (r *SearchActivityRepo) ListQueries(ctx context.Context, f indexers.QueryFilter) ([]indexers.QueryLog, int, error) {
	where, args := buildQueryHistoryFilter(f)

	var total int
	if err := r.db.QueryRowContext(ctx, "SELECT count(*) FROM search_queries"+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("search-activity: count queries: %w", err)
	}

	// Append paging args after the filter args.
	page := f.Page
	if page < 1 {
		page = 1
	}
	size := f.PageSize
	if size <= 0 {
		size = 25
	}
	if size > 100 {
		size = 100
	}
	limitArg := len(args) + 1
	offsetArg := limitArg + 1
	args = append(args, size, (page-1)*size)

	rows, err := r.db.QueryContext(ctx, `
		SELECT id, indexer_id, indexer_name, term, categories, ok, elapsed_ms, at
		FROM search_queries`+where+`
		ORDER BY at DESC, id DESC
		LIMIT $`+strconv.Itoa(limitArg)+` OFFSET $`+strconv.Itoa(offsetArg), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("search-activity: list queries: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []indexers.QueryLog
	for rows.Next() {
		var (
			q        indexers.QueryLog
			id       *int64
			catsJSON []byte
		)
		if err := rows.Scan(&q.ID, &id, &q.Indexer, &q.Term, &catsJSON, &q.OK, &q.ElapsedMS, &q.At); err != nil {
			return nil, 0, fmt.Errorf("search-activity: scan query: %w", err)
		}
		q.IndexerID = id
		if len(catsJSON) > 0 {
			if err := json.Unmarshal(catsJSON, &q.Categories); err != nil {
				return nil, 0, fmt.Errorf("search-activity: decode categories: %w", err)
			}
		}
		out = append(out, q)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("search-activity: list query rows: %w", err)
	}
	return out, total, nil
}

// DeleteAllQueries removes every query-history row and returns the count.
func (r *SearchActivityRepo) DeleteAllQueries(ctx context.Context) (int, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM search_queries`)
	if err != nil {
		return 0, fmt.Errorf("search-activity: clear queries: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("search-activity: clear queries count: %w", err)
	}
	return int(n), nil
}

// Totals returns the all-time query-attempt and grab counts.
func (r *SearchActivityRepo) Totals(ctx context.Context) (int, int, error) {
	var totalQueries, totalGrabs int
	err := r.db.QueryRowContext(ctx, `
		SELECT (SELECT count(*) FROM search_queries),
		       (SELECT count(*) FROM search_grabs)`).Scan(&totalQueries, &totalGrabs)
	if err != nil {
		return 0, 0, fmt.Errorf("search-activity: totals: %w", err)
	}
	return totalQueries, totalGrabs, nil
}

// DailyCounts returns the per-(indexer, kind, UTC-day) counts in [since, now).
// The API layer aggregates these (including the cross-indexer totals and the
// zero-filled 30-day buckets), so this method returns only the raw, present
// (indexer, day, kind) cells.
func (r *SearchActivityRepo) DailyCounts(ctx context.Context, since time.Time) ([]indexers.DailyCount, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT indexer_name, 'query' AS kind, date_trunc('day', at) AS day, count(*)
		FROM search_queries WHERE at >= $1
		GROUP BY indexer_name, 3
		UNION ALL
		SELECT indexer_name, 'grab' AS kind, date_trunc('day', at), count(*)
		FROM search_grabs WHERE at >= $1
		GROUP BY indexer_name, 3`, since)
	if err != nil {
		return nil, fmt.Errorf("search-activity: daily counts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []indexers.DailyCount
	for rows.Next() {
		var dc indexers.DailyCount
		if err := rows.Scan(&dc.Indexer, &dc.Kind, &dc.Day, &dc.Count); err != nil {
			return nil, fmt.Errorf("search-activity: scan daily count: %w", err)
		}
		out = append(out, dc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search-activity: daily count rows: %w", err)
	}
	return out, nil
}

