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
