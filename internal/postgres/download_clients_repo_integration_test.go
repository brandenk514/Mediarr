package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"

	"github.com/brandenk514/mediarr/internal/downloadclients"
)

func TestDownloadClientsRepo_CRUD(t *testing.T) {
	db := newTestPostgres(t)
	ctx := context.Background()
	if err := NewWithDB(db).Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := NewDownloadClientsRepo(db, testVault(t))

	// --- Create + Get (api key round-trips decrypted) ---
	id, createdAt, err := repo.Create(ctx, downloadclients.Record{
		Name: "qBittorrent", Kind: downloadclients.KindQB, Enabled: true,
		Position: 1, BaseURL: "http://qb:8080", Username: "admin",
		APIKey: "k3y",
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
	if got.Name != "qBittorrent" || got.Kind != downloadclients.KindQB || got.BaseURL != "http://qb:8080" {
		t.Errorf("round-trip mismatch: %+v", got)
	}
	if got.APIKey != "k3y" {
		t.Errorf("api key round-trip = %q, want %q (decrypt at read)", got.APIKey, "k3y")
	}

	// --- The at-rest column holds ciphertext, never plaintext ---
	var atRest string
	if err := db.QueryRowContext(ctx, `SELECT api_key FROM download_clients WHERE id = $1`, id).Scan(&atRest); err != nil {
		t.Fatalf("read at-rest api_key: %v", err)
	}
	if atRest == "" {
		t.Fatal("at-rest api_key is empty, want ciphertext")
	}
	if atRest == "k3y" {
		t.Error("at-rest api_key is the plaintext; the repo must encrypt before write")
	}

	// --- List (position order, then id) ---
	id2, _, err := repo.Create(ctx, downloadclients.Record{
		Name: "SABnzbd", Kind: downloadclients.KindSAB, Enabled: true,
		Position: 2, BaseURL: "http://sab:8080", APIKey: "sabkey",
	})
	if err != nil {
		t.Fatalf("create 2: %v", err)
	}
	id3, _, err := repo.Create(ctx, downloadclients.Record{
		Name: "Transmission", Kind: downloadclients.KindTransmission, Enabled: false,
		Position: 1, BaseURL: "http://tr:9091",
	})
	if err != nil {
		t.Fatalf("create 3: %v", err)
	}
	list, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("list len = %d, want 3", len(list))
	}
	// position 1 rows (id, id3) before position 2 (id2); id3 (created last)
	// comes after id within position 1.
	wantOrder := []int64{id, id3, id2}
	for i, wantID := range wantOrder {
		if list[i].ID != wantID {
			t.Fatalf("list order = %v, want %v", idsOf(list), wantOrder)
		}
	}
	if list[1].APIKey != "" {
		t.Errorf("keyless client must read back with an empty key, got %q", list[1].APIKey)
	}
	var keylessAtRest string
	if err := db.QueryRowContext(ctx, `SELECT api_key FROM download_clients WHERE id = $1`, id3).Scan(&keylessAtRest); err != nil {
		t.Fatalf("read at-rest keyless api_key: %v", err)
	}
	if keylessAtRest != "" {
		t.Errorf("keyless client must store \"\", got %q", keylessAtRest)
	}

	// --- Update (full replace; new key re-encrypted) ---
	got.Enabled = false
	got.APIKey = "newkey"
	if err := repo.Update(ctx, *got); err != nil {
		t.Fatalf("update: %v", err)
	}
	again, err := repo.Get(ctx, id)
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if again.APIKey != "newkey" || again.Enabled {
		t.Errorf("update round-trip = %+v, want key newkey + disabled", again)
	}
	var atRest2 string
	if err := db.QueryRowContext(ctx, `SELECT api_key FROM download_clients WHERE id = $1`, id).Scan(&atRest2); err != nil {
		t.Fatalf("read at-rest api_key after update: %v", err)
	}
	if atRest2 == "" || atRest2 == "newkey" {
		t.Errorf("at-rest api_key after update must be ciphertext, got %q", atRest2)
	}

	// --- Get/Update/Delete unknown -> sql.ErrNoRows ---
	if _, err := repo.Get(ctx, 9999); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("get unknown: got %v, want sql.ErrNoRows", err)
	}
	missing := downloadclients.Record{ID: 9999, Name: "x", Kind: downloadclients.KindQB}
	if err := repo.Update(ctx, missing); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("update unknown: got %v, want sql.ErrNoRows", err)
	}
	if err := repo.Delete(ctx, 9999); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("delete unknown: got %v, want sql.ErrNoRows", err)
	}

	// --- Delete ---
	if err := repo.Delete(ctx, id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := repo.Get(ctx, id); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("get after delete: got %v, want sql.ErrNoRows", err)
	}
	list, err = repo.List(ctx)
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(list) != 2 || list[0].ID != id3 || list[1].ID != id2 {
		t.Errorf("list after delete = %v, want id3 then id2", idsOf(list))
	}
}

func idsOf(list []downloadclients.Record) []int64 {
	out := make([]int64, len(list))
	for i, r := range list {
		out[i] = r.ID
	}
	return out
}

func TestDownloadClientsRepo_SeedLegacySingleClient(t *testing.T) {
	db := newTestPostgres(t)
	ctx := context.Background()
	if err := NewWithDB(db).Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	vault := testVault(t)
	repo := NewDownloadClientsRepo(db, vault)

	// No legacy config -> nothing seeded.
	if n, err := repo.SeedLegacySingleClient(ctx); err != nil || n != 0 {
		t.Fatalf("seed empty: n=%d err=%v, want 0/nil", n, err)
	}

	// A configured qBittorrent legacy config seeds exactly one row with the
	// key round-tripping to the original plaintext.
	encKey, err := vault.EncryptString("legacykey")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	var st dcStored
	st.Kind = "qbittorrent"
	st.QBBase = "http://legacy-qb:8080"
	st.QBUsername = "admin"
	st.QBAPIKey = encKey
	raw, _ := json.Marshal(st)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO settings (key, value_json) VALUES ($1, $2::jsonb)
		ON CONFLICT (key) DO UPDATE SET value_json = $2::jsonb, updated_at = now()`, dcStorageKey, raw); err != nil {
		t.Fatalf("write legacy config: %v", err)
	}

	n, err := repo.SeedLegacySingleClient(ctx)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if n != 1 {
		t.Fatalf("seed inserted %d rows, want 1", n)
	}
	list, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("list len = %d, want 1", len(list))
	}
	if list[0].Name != "qBittorrent" || list[0].Kind != downloadclients.KindQB ||
		list[0].BaseURL != "http://legacy-qb:8080" || list[0].Username != "admin" ||
		!list[0].Enabled || list[0].Position != 1 {
		t.Errorf("seeded row mismatch: %+v", list[0])
	}
	if list[0].APIKey != "legacykey" {
		t.Errorf("seeded key = %q, want %q (ciphertext carried verbatim, decrypted at read)", list[0].APIKey, "legacykey")
	}
	var atRest string
	if err := db.QueryRowContext(ctx, `SELECT api_key FROM download_clients WHERE id = $1`, list[0].ID).Scan(&atRest); err != nil {
		t.Fatalf("read at-rest seeded key: %v", err)
	}
	if atRest != encKey {
		t.Errorf("seeded at-rest key must be the legacy ciphertext verbatim")
	}

	// Idempotent: a second run seeds nothing.
	if n, err := repo.SeedLegacySingleClient(ctx); err != nil || n != 0 {
		t.Fatalf("reseed: n=%d err=%v, want 0/nil", n, err)
	}

	// A mock legacy config seeds nothing.
	st.Kind = "mock"
	raw, _ = json.Marshal(st)
	if _, err := db.ExecContext(ctx, `UPDATE settings SET value_json = $2::jsonb WHERE key = $1`, dcStorageKey, raw); err != nil {
		t.Fatalf("update legacy config: %v", err)
	}
	if n, err := repo.SeedLegacySingleClient(ctx); err != nil || n != 0 {
		t.Fatalf("seed mock: n=%d err=%v, want 0/nil", n, err)
	}
}
