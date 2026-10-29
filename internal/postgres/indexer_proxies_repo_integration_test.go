package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/brandenk514/mediarr/internal/indexers"
)

func TestIndexerProxiesRepo_CRUD(t *testing.T) {
	db := newTestPostgres(t)
	ctx := context.Background()
	if err := NewWithDB(db).Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := NewIndexerProxiesRepo(db, testVault(t))

	// --- Create + Get (password round-trips decrypted; tags round-trip) ---
	id, createdAt, err := repo.Create(ctx, indexers.Proxy{
		Name: "flare-1", URL: "http://fs1:8191", Username: "u", Password: "pw123",
		Tags: []string{"private", "usenet"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if id <= 0 || createdAt.IsZero() {
		t.Fatalf("create returned id=%d createdAt=%v", id, createdAt)
	}

	got, err := repo.Get(ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "flare-1" || got.URL != "http://fs1:8191" || got.Username != "u" {
		t.Errorf("round-trip mismatch: %+v", got)
	}
	if got.Password != "pw123" {
		t.Errorf("password round-trip = %q, want %q (decrypt at read)", got.Password, "pw123")
	}
	if len(got.Tags) != 2 || got.Tags[0] != "private" || got.Tags[1] != "usenet" {
		t.Errorf("tags round-trip = %v", got.Tags)
	}

	// --- The at-rest column holds ciphertext, never plaintext ---
	var atRest *string
	if err := db.QueryRowContext(ctx, `SELECT password FROM indexer_proxies WHERE id = $1`, id).Scan(&atRest); err != nil {
		t.Fatalf("read at-rest password: %v", err)
	}
	if atRest == nil {
		t.Fatal("at-rest password is NULL, want ciphertext")
	}
	if *atRest == "pw123" {
		t.Error("at-rest password is the plaintext; the repo must encrypt before write")
	}

	// --- List (creation order; tags never null) ---
	id2, _, err := repo.Create(ctx, indexers.Proxy{
		Name: "flare-open", URL: "http://fs2:8191",
	})
	if err != nil {
		t.Fatalf("create 2: %v", err)
	}
	list, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("list len = %d, want 2", len(list))
	}
	if list[0].ID != id || list[1].ID != id2 {
		t.Errorf("list not in creation order: %+v", list)
	}
	if list[1].Tags == nil {
		t.Error("tags must decode to a non-nil (possibly empty) list")
	}
	if len(list[1].Tags) != 0 || list[1].Password != "" {
		t.Errorf("passwordless proxy must read back empty: %+v", list[1])
	}
	var nullPw *string
	if err := db.QueryRowContext(ctx, `SELECT password FROM indexer_proxies WHERE id = $1`, id2).Scan(&nullPw); err != nil {
		t.Fatalf("read at-rest password 2: %v", err)
	}
	if nullPw != nil {
		t.Errorf("passwordless proxy must store NULL, got %q", *nullPw)
	}

	// --- Update (full replace; new password re-encrypted, tags replaced) ---
	got.Password = "newpw"
	got.Tags = []string{"x"}
	if err := repo.Update(ctx, *got); err != nil {
		t.Fatalf("update: %v", err)
	}
	again, err := repo.Get(ctx, id)
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if again.Password != "newpw" {
		t.Errorf("update round-trip password = %q, want newpw", again.Password)
	}
	if len(again.Tags) != 1 || again.Tags[0] != "x" {
		t.Errorf("update round-trip tags = %v, want [x]", again.Tags)
	}
	var atRest2 *string
	if err := db.QueryRowContext(ctx, `SELECT password FROM indexer_proxies WHERE id = $1`, id).Scan(&atRest2); err != nil {
		t.Fatalf("read at-rest password after update: %v", err)
	}
	if atRest2 == nil || *atRest2 == "newpw" {
		t.Errorf("at-rest password after update must be ciphertext, got %v", atRest2)
	}

	// --- Get unknown -> sql.ErrNoRows; Update unknown -> sql.ErrNoRows ---
	if _, err := repo.Get(ctx, 9999); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("get unknown: got %v, want sql.ErrNoRows", err)
	}
	missing := indexers.Proxy{ID: 9999, Name: "x", URL: "http://x"}
	if err := repo.Update(ctx, missing); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("update unknown: got %v, want sql.ErrNoRows", err)
	}

	// --- Delete (idempotent) ---
	if err := repo.Delete(ctx, id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := repo.Get(ctx, id); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("get after delete: got %v, want sql.ErrNoRows", err)
	}
	if err := repo.Delete(ctx, id); err != nil {
		t.Errorf("delete twice: %v", err)
	}
	list, err = repo.List(ctx)
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(list) != 1 || list[0].ID != id2 {
		t.Errorf("list after delete = %+v, want just id2", list)
	}
}
