// Integration test for the down-migration mechanism (card #12 acceptance:
// "Testcontainers migration up/down passes"). It boots a real Postgres
// container, migrates up (all versions applied), asserts the schema exists,
// migrates fully down, asserts the schema is gone (including the tracking
// table), confirms a second down is a no-op, then migrates up again to prove
// the schema can be re-applied from a fully reverted database.
package postgres

import (
	"context"
	"testing"
)

func TestMigrate_DownRevertsAndReapplies(t *testing.T) {
	db := newTestPostgres(t)
	ctx := context.Background()
	st := NewWithDB(db)

	hasTable := func(name string) (bool, error) {
		var n int
		err := db.QueryRowContext(ctx,
			`SELECT count(*) FROM information_schema.tables
			 WHERE table_schema='public' AND table_name=$1`, name,
		).Scan(&n)
		return n > 0, err
	}

	// 1. Migrate up — every version applied, schema present.
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	v, err := st.Version(ctx)
	if err != nil {
		t.Fatalf("version after up: %v", err)
	}
	if v != "0003_tv" {
		t.Fatalf("version = %q, want 0003_tv (highest applied)", v)
	}
	for _, tbl := range []string{"users", "movies", "tv_series", "schema_migrations"} {
		ok, err := hasTable(tbl)
		if err != nil {
			t.Fatalf("check %s: %v", tbl, err)
		}
		if !ok {
			t.Fatalf("table %s missing after migrate up", tbl)
		}
	}

	// 2. Migrate down — every version reverted, schema fully gone.
	if err := st.MigrateDown(ctx); err != nil {
		t.Fatalf("migrate down: %v", err)
	}
	v, err = st.Version(ctx)
	if err != nil {
		t.Fatalf("version after down: %v", err)
	}
	if v != "0000" {
		t.Fatalf("version = %q, want 0000 (nothing applied)", v)
	}
	for _, tbl := range []string{"users", "settings", "api_tokens", "movies", "wanted", "queue", "history",
		"tv_series", "tv_episodes", "tv_wanted", "tv_queue", "tv_history", "schema_migrations"} {
		ok, err := hasTable(tbl)
		if err != nil {
			t.Fatalf("check %s: %v", tbl, err)
		}
		if ok {
			t.Errorf("table %s still present after migrate down", tbl)
		}
	}

	// 3. A second full down on an already-empty database is a no-op.
	if err := st.MigrateDown(ctx); err != nil {
		t.Fatalf("second migrate down (idempotency): %v", err)
	}

	// 4. Re-apply from scratch — the reverted database migrates up cleanly.
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("re-migrate up after down: %v", err)
	}
	v, err = st.Version(ctx)
	if err != nil {
		t.Fatalf("version after re-up: %v", err)
	}
	if v != "0003_tv" {
		t.Fatalf("version after re-up = %q, want 0003_tv", v)
	}
	if ok, _ := hasTable("tv_series"); !ok {
		t.Error("tv_series missing after re-migrate up")
	}
}
