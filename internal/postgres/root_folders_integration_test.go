package postgres_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	rfdom "github.com/brandenk514/mediarr/internal/domains/rootfolders"
	"github.com/brandenk514/mediarr/internal/postgres"
	_ "github.com/jackc/pgx/v5/stdlib"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

// startRFPG spins up a Postgres container with migrations applied and returns
// a *RootFoldersRepo. Mirrors the startTVPG harness.
func startRFPG(t *testing.T) *postgres.RootFoldersRepo {
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

	deadline := time.Now().Add(30 * time.Second)
	for {
		if err := db.PingContext(ctx); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("ping: %v", err)
		}
		time.Sleep(500 * time.Millisecond)
	}

	st := postgres.NewWithDB(db)
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return postgres.NewRootFoldersRepo(db)
}

// TestRootFoldersRepo_Coverage exercises the P2 root-folder registry against
// a real Postgres container: create/list/get, update, delete, and the
// unique-path constraint.
func TestRootFoldersRepo_Coverage(t *testing.T) {
	rf := startRFPG(t)

	id, err := rf.Create(rfdom.RootFolder{Path: "/media/tv", Name: "TV"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if id == 0 {
		t.Fatal("expected a generated id")
	}

	other, err := rf.Create(rfdom.RootFolder{Path: "/media/movies", Name: ""})
	if err != nil {
		t.Fatalf("create 2: %v", err)
	}

	folders, err := rf.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(folders) != 2 || folders[0].ID != id || folders[1].ID != other {
		t.Fatalf("list = %+v, want [tv, movies] by id", folders)
	}

	got, err := rf.Get(id)
	if err != nil || got.Path != "/media/tv" || got.Name != "TV" {
		t.Fatalf("get = %+v, %v", got, err)
	}
	if _, err := rf.Get(99999); err != sql.ErrNoRows {
		t.Errorf("get missing err = %v, want sql.ErrNoRows", err)
	}

	// Update name + path.
	if err := rf.Update(rfdom.RootFolder{ID: id, Path: "/tv2", Name: "TV2"}); err != nil {
		t.Fatalf("update: %v", err)
	}
	updated, _ := rf.Get(id)
	if updated.Path != "/tv2" || updated.Name != "TV2" {
		t.Errorf("updated = %+v", *updated)
	}

	// Unique path constraint: re-inserting an existing path fails.
	if _, err := rf.Create(rfdom.RootFolder{Path: "/tv2"}); err == nil {
		t.Error("expected unique-violation error on duplicate path")
	}

	// Delete + delete-missing.
	if err := rf.Delete(id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := rf.Delete(id); err == nil || !strings.Contains(err.Error(), "no root folder") {
		t.Errorf("delete missing err = %v, want no-root-folder error", err)
	}
	folders, _ = rf.List()
	if len(folders) != 1 || folders[0].ID != other {
		t.Errorf("folders after delete = %+v, want [movies]", folders)
	}
}
