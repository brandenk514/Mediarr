// Package postgres provides the Postgres-backed Store implementation and a
// small migration runner. It is the only package that imports *sql.DB;
// everything else programs against store.Store.
package postgres

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"sort"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib" // register "pgx" driver for database/sql

	"github.com/brandenk514/mediarr/internal/store"
)

//go:embed migrations
var migrationsFS embed.FS

// Store is the Postgres-backed store.Store implementation.
type Store struct {
	db *sql.DB
}

var _ store.Store = (*Store)(nil)

// New opens a connection pool. The caller owns Close.
func New(dsn string) (*Store, error) {
	db, err := sql.Open("pgx", dsn) // driver registered by pgx/stdlib
	if err != nil {
		return nil, fmt.Errorf("postgres: open: %w", err)
	}
	// Sensible pool defaults; override via DSN params if needed.
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(5)
	return &Store{db: db}, nil
}

// DB exposes the underlying pool for repositories that need direct SQL.
func (s *Store) DB() *sql.DB { return s.db }

// NewWithDB wraps an existing pool. Used by integration tests that own
// the connection lifecycle.
func NewWithDB(db *sql.DB) *Store { return &Store{db: db} }

// Close releases the pool.
func (s *Store) Close() error { return s.db.Close() }

// Ping verifies connectivity.
func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

// Migrate applies all pending migrations in version order. Idempotent.
func (s *Store) Migrate(ctx context.Context) error {
	files, err := listMigrations()
	if err != nil {
		return err
	}
	sort.Slice(files, func(i, j int) bool { return files[i] < files[j] })

	// Ensure the tracking table exists before we try to read it.
	if _, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`); err != nil {
		return fmt.Errorf("postgres: ensure migrations table: %w", err)
	}

	for _, f := range files {
		isUp := strings.HasSuffix(f, ".up.sql")
		if !isUp {
			continue
		}
		version := strings.TrimSuffix(f, ".up.sql")
		if applied, err := s.applied(ctx, version); err != nil {
			return err
		} else if applied {
			continue
		}
		body, err := migrationsFS.ReadFile("migrations/" + f)
		if err != nil {
			return fmt.Errorf("postgres: read %s: %w", f, err)
		}
		if _, err := s.db.ExecContext(ctx, string(body)); err != nil {
			return fmt.Errorf("postgres: apply %s: %w", f, err)
		}
		if _, err := s.db.ExecContext(ctx,
			`INSERT INTO schema_migrations (version) VALUES ($1)`, version); err != nil {
			return fmt.Errorf("postgres: record %s: %w", f, err)
		}
	}
	return nil
}

func (s *Store) applied(ctx context.Context, version string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM schema_migrations WHERE version = $1`, version).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// Version returns the highest applied migration version.
func (s *Store) Version(ctx context.Context) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx,
		`SELECT max(version) FROM schema_migrations`).Scan(&v)
	if err == sql.ErrNoRows {
		return "0000", nil
	}
	if err != nil {
		return "", err
	}
	if v == "" {
		return "0000", nil
	}
	return v, nil
}

func listMigrations() ([]string, error) {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return nil, fmt.Errorf("postgres: list migrations: %w", err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		out = append(out, e.Name())
	}
	return out, nil
}
