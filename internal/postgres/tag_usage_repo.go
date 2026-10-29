package postgres

import (
	"context"
	"database/sql"
	"fmt"
)

// TagUsageRepo answers the tags section's usage view (P5). TV series are the
// only tagged entity today: tv_series.tags is a TEXT[] of tag labels, and the
// usage counts are series carrying the label plus the episodes of those
// series.
type TagUsageRepo struct {
	db *sql.DB
}

// NewTagUsageRepo builds the repo over the pool.
func NewTagUsageRepo(db *sql.DB) *TagUsageRepo {
	return &TagUsageRepo{db: db}
}

// CountByTag returns (series, episodes) carrying the tag label.
func (r *TagUsageRepo) CountByTag(ctx context.Context, label string) (int, int, error) {
	var series, episodes int
	if err := r.db.QueryRowContext(ctx,
		`SELECT count(*) FROM tv_series WHERE tags @> ARRAY[$1]::text[]`, label,
	).Scan(&series); err != nil {
		return 0, 0, fmt.Errorf("tag usage: count series: %w", err)
	}
	if err := r.db.QueryRowContext(ctx, `
		SELECT count(*)
		FROM tv_episodes e
		JOIN tv_series s ON s.id = e.series_id
		WHERE s.tags @> ARRAY[$1]::text[]`, label,
	).Scan(&episodes); err != nil {
		return 0, 0, fmt.Errorf("tag usage: count episodes: %w", err)
	}
	return series, episodes, nil
}

// IndexersByTag returns (count, names) of the indexers whose tags array
// carries the tag label (Prowlarr PR4). indexers.tags is a JSONB array of
// strings, so the containment check is a JSONB @> against a one-element array.
// Names are sorted a-to-z for a stable UI list.
func (r *TagUsageRepo) IndexersByTag(ctx context.Context, label string) (int, []string, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT name FROM indexers WHERE tags @> to_jsonb(ARRAY[$1]::text[]) ORDER BY name`, label)
	if err != nil {
		return 0, nil, fmt.Errorf("tag usage: list indexers: %w", err)
	}
	defer func() { _ = rows.Close() }() // rows consumed by the loop below
	names := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return 0, nil, fmt.Errorf("tag usage: scan indexer: %w", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return 0, nil, fmt.Errorf("tag usage: indexer rows: %w", err)
	}
	return len(names), names, nil
}
