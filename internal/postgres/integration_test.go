// Integration tests for the Postgres store and migrations. They require a
// running Docker daemon (testcontainers). To run:
//
//	go test ./internal/postgres/...
//
// A throwaway postgres:16 container is started per test, migrations are
// applied, and the resulting schema is asserted.
package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

// newTestPostgres boots a Postgres container and returns a *sql.DB.
func newTestPostgres(t *testing.T) *sql.DB {
	t.Helper()
	ctx := context.Background()

	c, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("mediarr"),
		tcpostgres.WithUsername("mediarr"),
		tcpostgres.WithPassword("mediarr-test"),
	)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}
	t.Cleanup(func() { _ = c.Terminate(context.Background()) })

	dsn, err := c.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := db.PingContext(ctx); err != nil {
		// The postgres image opens its port before the entrypoint finishes
		// creating the user/database. Retry until the server accepts
		// connections, up to 30s.
		deadline := time.Now().Add(30 * time.Second)
		for {
			err := db.PingContext(ctx)
			if err == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("ping: %v", err)
			}
			time.Sleep(500 * time.Millisecond)
		}
	}
	return db
}

func TestMigrate_CreatesCoreTables(t *testing.T) {
	db := newTestPostgres(t)
	ctx := context.Background()

	st := NewWithDB(db)
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	for _, tbl := range []string{"users", "api_tokens", "settings", "schema_migrations"} {
		var n int
		err := db.QueryRowContext(ctx,
			`SELECT count(*) FROM information_schema.tables
			 WHERE table_schema='public' AND table_name=$1`, tbl,
		).Scan(&n)
		if err != nil {
			t.Fatalf("query table %s: %v", tbl, err)
		}
		if n == 0 {
			t.Errorf("table %q not created by migration", tbl)
		}
	}
}

func TestMigrate_Idempotent(t *testing.T) {
	db := newTestPostgres(t)
	ctx := context.Background()
	st := NewWithDB(db)

	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	// Second run must be a no-op and succeed.
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("second migrate (idempotency): %v", err)
	}
}

func TestVersion_TracksAppliedMigration(t *testing.T) {
	db := newTestPostgres(t)
	ctx := context.Background()
	st := NewWithDB(db)

	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	v, err := st.Version(ctx)
	if err != nil {
		t.Fatalf("version after migrate: %v", err)
	}
	if v != "0004_music" {
		t.Errorf("version = %q, want 0004_music (highest applied)", v)
	}
}

func TestPing_Healthy(t *testing.T) {
	db := newTestPostgres(t)
	st := NewWithDB(db)
	if err := st.Ping(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
}
