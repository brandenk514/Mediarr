package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/brandenk514/mediarr/internal/notify"
)

func TestNotificationsRepo_CRUD(t *testing.T) {
	db := newTestPostgres(t)
	ctx := context.Background()
	if err := NewWithDB(db).Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := NewNotificationsRepo(db, testVault(t))

	// --- Create + Get (token round-trips decrypted) ---
	id, createdAt, err := repo.Create(ctx, notify.SinkRecord{
		Name: "ntfy home", Kind: notify.KindWebhook, Enabled: true,
		URL: "http://ntfy.example:2000/mediarr", Token: "s3cret",
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
	if got.Name != "ntfy home" || got.Kind != notify.KindWebhook || got.URL != "http://ntfy.example:2000/mediarr" {
		t.Errorf("round-trip mismatch: %+v", got)
	}
	if got.Token != "s3cret" {
		t.Errorf("token round-trip = %q, want %q (decrypt at read)", got.Token, "s3cret")
	}

	// --- The at-rest column holds ciphertext, never plaintext ---
	var atRest *string
	if err := db.QueryRowContext(ctx, `SELECT token FROM notifications WHERE id = $1`, id).Scan(&atRest); err != nil {
		t.Fatalf("read at-rest token: %v", err)
	}
	if atRest == nil {
		t.Fatal("at-rest token is NULL, want ciphertext")
	}
	if *atRest == "s3cret" {
		t.Error("at-rest token is the plaintext; the repo must encrypt before write")
	}

	// --- List (creation order) ---
	id2, _, err := repo.Create(ctx, notify.SinkRecord{
		Name: "plex", Kind: notify.KindPlex, Enabled: false,
		URL: "http://plex.example:32400", SectionID: "5",
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
	if list[1].Token != "" {
		t.Errorf("tokenless sink must read back with an empty token, got %q", list[1].Token)
	}
	var nullTok *string
	if err := db.QueryRowContext(ctx, `SELECT token FROM notifications WHERE id = $1`, id2).Scan(&nullTok); err != nil {
		t.Fatalf("read at-rest token 2: %v", err)
	}
	if nullTok != nil {
		t.Errorf("tokenless sink must store NULL, got %q", *nullTok)
	}

	// --- Update (full replace; new token re-encrypted) ---
	got.Enabled = false
	got.Token = "newtok"
	if err := repo.Update(ctx, *got); err != nil {
		t.Fatalf("update: %v", err)
	}
	again, err := repo.Get(ctx, id)
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if again.Token != "newtok" || again.Enabled {
		t.Errorf("update round-trip = %+v, want token newtok + disabled", again)
	}
	var atRest2 *string
	if err := db.QueryRowContext(ctx, `SELECT token FROM notifications WHERE id = $1`, id).Scan(&atRest2); err != nil {
		t.Fatalf("read at-rest token after update: %v", err)
	}
	if atRest2 == nil || *atRest2 == "newtok" {
		t.Errorf("at-rest token after update must be ciphertext, got %v", atRest2)
	}

	// --- Get unknown -> sql.ErrNoRows; Update unknown -> sql.ErrNoRows ---
	if _, err := repo.Get(ctx, 9999); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("get unknown: got %v, want sql.ErrNoRows", err)
	}
	missing := notify.SinkRecord{ID: 9999, Name: "x", Kind: notify.KindWebhook, URL: "http://x"}
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
