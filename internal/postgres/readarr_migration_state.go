package postgres

import (
	"context"
	"database/sql"
	"fmt"
)

// ReadarrMigrationStateRepo is the Postgres-backed resume state for the
// Readarr drop-in migration (migration 0031): which source books a run over
// a given source instance has already imported. The system package's
// ReadarrMigrationState seam is adapted to it in main.go.
type ReadarrMigrationStateRepo struct {
	db *sql.DB
}

// NewReadarrMigrationStateRepo builds the resume-state repository over the
// given pool.
func NewReadarrMigrationStateRepo(db *sql.DB) *ReadarrMigrationStateRepo {
	return &ReadarrMigrationStateRepo{db: db}
}

// IsMigrated reports whether bookKey was already migrated for sourceURL.
func (r *ReadarrMigrationStateRepo) IsMigrated(ctx context.Context, sourceURL, bookKey string) (bool, error) {
	var n int
	err := r.db.QueryRowContext(ctx,
		`SELECT count(*) FROM readarr_migration_state WHERE source_url = $1 AND book_key = $2`,
		sourceURL, bookKey).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("readarr migration state: is_migrated: %w", err)
	}
	return n > 0, nil
}

// MarkMigrated records bookKey as migrated for sourceURL (idempotent).
func (r *ReadarrMigrationStateRepo) MarkMigrated(ctx context.Context, sourceURL, bookKey string) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO readarr_migration_state (source_url, book_key)
		VALUES ($1, $2)
		ON CONFLICT (source_url, book_key) DO NOTHING`,
		sourceURL, bookKey)
	if err != nil {
		return fmt.Errorf("readarr migration state: mark_migrated: %w", err)
	}
	return nil
}

// MigratedKeys returns the set of book keys already migrated for sourceURL.
func (r *ReadarrMigrationStateRepo) MigratedKeys(ctx context.Context, sourceURL string) (map[string]bool, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT book_key FROM readarr_migration_state WHERE source_url = $1`, sourceURL)
	if err != nil {
		return nil, fmt.Errorf("readarr migration state: migrated_keys: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]bool{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, fmt.Errorf("readarr migration state: scan: %w", err)
		}
		out[k] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("readarr migration state: rows: %w", err)
	}
	return out, nil
}

// Clear drops all state rows for sourceURL (a fresh, non-resume import).
func (r *ReadarrMigrationStateRepo) Clear(ctx context.Context, sourceURL string) error {
	if _, err := r.db.ExecContext(ctx,
		`DELETE FROM readarr_migration_state WHERE source_url = $1`, sourceURL); err != nil {
		return fmt.Errorf("readarr migration state: clear: %w", err)
	}
	return nil
}
