package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"testing"
	"time"

	"github.com/brandenk514/mediarr/internal/indexers"
	"github.com/brandenk514/mediarr/internal/secrets"
)

// testVault returns a Vault with a fresh 32-byte key, mirroring how the app
// builds one from MEDARR_ENCRYPTION_KEY.
func testVault(t *testing.T) *secrets.Vault {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("gen key: %v", err)
	}
	v, err := secrets.NewVaultFromBytes(key)
	if err != nil {
		t.Fatalf("NewVaultFromBytes: %v", err)
	}
	return v
}

func TestIndexersRepo_CRUD(t *testing.T) {
	db := newTestPostgres(t)
	ctx := context.Background()
	if err := NewWithDB(db).Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := NewIndexersRepo(db, testVault(t))

	// --- Create + Get (round-trip of name/kind/url/key) -------------------
	id, err := repo.Create(ctx, indexers.Definition{
		Name: "nyaa", Kind: "torznab", BaseURL: "https://nyaa.example/api",
		APIKey: "sekrit-key-123", SettingsJSON: `{"categories":"1,2"}`, Enabled: true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if id <= 0 {
		t.Fatalf("create returned non-positive id %d", id)
	}

	got, err := repo.Get(ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "nyaa" || got.Kind != "torznab" || got.BaseURL != "https://nyaa.example/api" {
		t.Errorf("round-trip mismatch: %+v", got)
	}
	if got.APIKey != "sekrit-key-123" {
		t.Errorf("APIKey round-trip = %q, want %q (decrypt at read)", got.APIKey, "sekrit-key-123")
	}
	if got.SettingsJSON != `{"categories":"1,2"}` {
		t.Errorf("SettingsJSON = %q", got.SettingsJSON)
	}
	if !got.Enabled {
		t.Errorf("Enabled = false, want true")
	}

	// --- Duplicate name is rejected (UNIQUE) -------------------------------
	if _, err := repo.Create(ctx, indexers.Definition{Name: "nyaa", Kind: "torznab"}); err == nil {
		t.Error("expected duplicate-name create to fail, got nil")
	}

	// --- GetByName ----------------------------------------------------------
	byName, err := repo.GetByName(ctx, "nyaa")
	if err != nil {
		t.Fatalf("getbyname: %v", err)
	}
	if byName.ID != id {
		t.Errorf("getbyname id = %d, want %d", byName.ID, id)
	}

	// --- Keyless indexer: NULL ciphertext, empty plaintext on read --------
	id2, err := repo.Create(ctx, indexers.Definition{Name: "public-rss", Kind: "torrent-rss"})
	if err != nil {
		t.Fatalf("create keyless: %v", err)
	}
	k2, err := repo.Get(ctx, id2)
	if err != nil {
		t.Fatalf("get keyless: %v", err)
	}
	if k2.APIKey != "" {
		t.Errorf("keyless indexer APIKey = %q, want empty", k2.APIKey)
	}
}

func TestIndexersRepo_UpdateReencryptsKey(t *testing.T) {
	db := newTestPostgres(t)
	ctx := context.Background()
	if err := NewWithDB(db).Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := NewIndexersRepo(db, testVault(t))

	id, _ := repo.Create(ctx, indexers.Definition{Name: "r", Kind: "torznab", APIKey: "old"})
	def, _ := repo.Get(ctx, id)
	def.APIKey = "new"
	def.Kind = "nzb"
	def.Enabled = false
	if err := repo.Update(ctx, *def); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ := repo.Get(ctx, id)
	if got.APIKey != "new" {
		t.Errorf("updated APIKey = %q, want new", got.APIKey)
	}
	if got.Kind != "nzb" || got.Enabled {
		t.Errorf("updated kind/enabled = %q/%v, want nzb/false", got.Kind, got.Enabled)
	}
}

func TestIndexersRepo_EnableDisableAndList(t *testing.T) {
	db := newTestPostgres(t)
	ctx := context.Background()
	if err := NewWithDB(db).Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := NewIndexersRepo(db, testVault(t))

	a, _ := repo.Create(ctx, indexers.Definition{Name: "a", Kind: "torznab", Enabled: true})
	b, _ := repo.Create(ctx, indexers.Definition{Name: "b", Kind: "nzb", Enabled: true})
	_, _ = repo.Create(ctx, indexers.Definition{Name: "c", Kind: "fake", Enabled: false})

	// List returns all three.
	all, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("list len = %d, want 3", len(all))
	}

	// Disable b; ListEnabled then excludes both b and c.
	if err := repo.SetEnabled(ctx, b, false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	en, err := repo.ListEnabled(ctx)
	if err != nil {
		t.Fatalf("listenabled: %v", err)
	}
	names := map[string]bool{}
	for _, d := range en {
		names[d.Name] = true
	}
	if !names["a"] || names["b"] || names["c"] {
		t.Errorf("ListEnabled = %v, want exactly [a]", names)
	}

	// Re-enable b.
	if err := repo.SetEnabled(ctx, b, true); err != nil {
		t.Fatalf("enable: %v", err)
	}
	en2, _ := repo.ListEnabled(ctx)
	if len(en2) != 2 {
		t.Errorf("ListEnabled after re-enable = %d, want 2", len(en2))
	}

	// Delete a; it is gone from both lists.
	if err := repo.Delete(ctx, a); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := repo.Get(ctx, a); err == nil {
		t.Errorf("get after delete should error")
	}
	rest, _ := repo.List(ctx)
	if len(rest) != 2 {
		t.Errorf("list after delete = %d, want 2", len(rest))
	}
	// Deleting a missing id is an error.
	if err := repo.Delete(ctx, 99999); err == nil {
		t.Errorf("delete missing id should error")
	}
}

func TestIndexersRepo_SetLastTest(t *testing.T) {
	db := newTestPostgres(t)
	ctx := context.Background()
	if err := NewWithDB(db).Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := NewIndexersRepo(db, testVault(t))
	id, _ := repo.Create(ctx, indexers.Definition{Name: "h", Kind: "torznab"})

	// Initially NULL.
	fresh, _ := repo.Get(ctx, id)
	if fresh.LastTest != nil {
		t.Errorf("fresh LastTest = %v, want nil", fresh.LastTest)
	}

	// Record a probe time.
	probe := time.Now().UTC().Truncate(time.Second)
	if err := repo.SetLastTest(ctx, id, &probe); err != nil {
		t.Fatalf("setlasttest: %v", err)
	}
	after, _ := repo.Get(ctx, id)
	if after.LastTest == nil {
		t.Fatal("LastTest is nil after SetLastTest")
	}
	if !after.LastTest.Equal(probe) {
		t.Errorf("LastTest = %v, want %v", after.LastTest, probe)
	}

	// Clearing it (nil) reverts to NULL.
	if err := repo.SetLastTest(ctx, id, nil); err != nil {
		t.Fatalf("clear lasttest: %v", err)
	}
	cleared, _ := repo.Get(ctx, id)
	if cleared.LastTest != nil {
		t.Errorf("LastTest = %v after clear, want nil", cleared.LastTest)
	}
}

func TestIndexersRepo_EncryptedAtRest(t *testing.T) {
	db := newTestPostgres(t)
	ctx := context.Background()
	if err := NewWithDB(db).Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := NewIndexersRepo(db, testVault(t))
	const key = "the-secret-api-key"
	id, _ := repo.Create(ctx, indexers.Definition{Name: "enc", Kind: "torznab", APIKey: key})

	// Inspect the raw column: it must be ciphertext, not the plaintext key.
	var stored string
	if err := db.QueryRowContext(ctx,
		`SELECT api_key_encrypted FROM indexers WHERE id = $1`, id).Scan(&stored); err != nil {
		t.Fatalf("read raw column: %v", err)
	}
	if stored == "" {
		t.Fatal("api_key_encrypted is empty; a key must be stored")
	}
	if stored == key {
		t.Fatal("FAIL: api_key stored in PLAINTEXT — it must be encrypted at rest")
	}
	// The ciphertext must be valid hex (AES-256-GCM hex encoding) and decode to
	// a length greater than the plaintext (GCM nonce + tag + padded ct).
	if _, err := hex.DecodeString(stored); err != nil {
		t.Errorf("ciphertext is not valid hex: %v", err)
	}
	if len(stored) < len(key) {
		t.Errorf("ciphertext (%d) shorter than plaintext (%d); not really encrypted",
			len(stored), len(key))
	}
	// But the repo round-trips it back to the exact plaintext.
	got, _ := repo.Get(ctx, id)
	if got.APIKey != key {
		t.Errorf("round-tripped APIKey = %q, want %q", got.APIKey, key)
	}
}
