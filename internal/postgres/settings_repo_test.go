package postgres

import (
	"context"
	"strings"
	"testing"

	"github.com/brandenk514/mediarr/internal/settings"
)

// TestSettingsRepo_DownloadClient is the #44 at-rest-encryption contract against
// a real Postgres container: the download-client config round-trips (secrets
// decrypted back to plaintext on read) while the settings table holds *ciphertext*
// for the credentials — never the raw key (PLAN §7, the same guarantee as the
// indexer repo).
func TestSettingsRepo_DownloadClient(t *testing.T) {
	db := newTestPostgres(t)
	ctx := context.Background()
	if err := NewWithDB(db).Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	vault := testVault(t)
	repo := NewSettingsRepo(db, vault)

	// Default: an empty store yields a mock client, all secrets empty.
	def, err := repo.GetDownloadClient(ctx)
	if err != nil {
		t.Fatalf("default get: %v", err)
	}
	if def.Kind != "mock" || def.QBAPIKey != "" || def.SABAPIKey != "" {
		t.Fatalf("expected a default mock client, got %+v", def)
	}

	// Save a qbittorrent config with a real credential.
	const secret = "qbit-super-secret-api-key"
	if err := repo.PutDownloadClient(ctx, &settings.DownloadClient{
		Kind:     "qbittorrent",
		QBBase:   "https://qbit.example",
		QBAPIKey: secret,
	}); err != nil {
		t.Fatalf("put: %v", err)
	}

	// Read it back: the secret is decrypted to plaintext.
	got, err := repo.GetDownloadClient(ctx)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Kind != "qbittorrent" || got.QBBase != "https://qbit.example" {
		t.Errorf("non-secret fields wrong: %+v", got)
	}
	if got.QBAPIKey != secret {
		t.Errorf("expected the API key to decrypt to the original, got %q", got.QBAPIKey)
	}

	// The at-rest value must be ciphertext, not the plaintext secret. The vault
	// (AES-GCM) produces non-hex... actually it produces hex, so assert the
	// stored value is not the plaintext and does not equal it.
	var atRest string
	err = db.QueryRowContext(ctx, `SELECT value_json FROM settings WHERE key = 'download_client'`).Scan(&atRest)
	if err != nil {
		t.Fatalf("read at-rest: %v", err)
	}
	if strings.Contains(atRest, secret) {
		t.Fatalf("at-rest settings table contains the plaintext secret — encryption is not applied")
	}
	// The at-rest JSON must be present and non-empty.
	if atRest == "" || strings.Trim(atRest, "{}") == "" {
		t.Fatalf("expected a non-empty at-rest document, got %q", atRest)
	}
}

// TestSettingsRepo_Library is the #44 per-library override contract against a
// real Postgres container: an override (including the checksum tristate)
// round-trips, and a zero override clears the row.
func TestSettingsRepo_Library(t *testing.T) {
	db := newTestPostgres(t)
	ctx := context.Background()
	if err := NewWithDB(db).Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := NewSettingsRepo(db, testVault(t))

	// Default: an unsaved library is a zero override.
	def, err := repo.GetLibrary(ctx, settings.LibraryMovie)
	if err != nil {
		t.Fatalf("default get: %v", err)
	}
	if def == nil || !def.IsZero() {
		t.Fatalf("expected a zero override, got %+v", def)
	}

	// Save an override with an explicit checksum opt-out (the tristate's
	// non-nil-false branch).
	off := false
	if err := repo.PutLibrary(ctx, settings.LibraryMusic, &settings.LibraryOverride{
		Path:           "/media/music",
		QualityProfile: "Lossless",
		ChecksumVerify: &off,
	}); err != nil {
		t.Fatalf("put: %v", err)
	}
	got, err := repo.GetLibrary(ctx, settings.LibraryMusic)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Path != "/media/music" || got.QualityProfile != "Lossless" {
		t.Errorf("non-checksum fields wrong: %+v", got)
	}
	if got.ChecksumVerify == nil || *got.ChecksumVerify != false {
		t.Errorf("expected the checksum opt-out (false) to round-trip, got %v", got.ChecksumVerify)
	}

	// A zero override clears the row: the row is removed and a read is zero.
	if err := repo.PutLibrary(ctx, settings.LibraryMusic, &settings.LibraryOverride{}); err != nil {
		t.Fatalf("clear put: %v", err)
	}
	var n int
	err = db.QueryRowContext(ctx, `SELECT count(*) FROM settings WHERE key = 'library.music'`).Scan(&n)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("expected the cleared override row to be removed, got %d rows", n)
	}
}

// TestSettingsRepo_Errors is the #44 domain-error contract: a Put with an
// invalid client kind, and a Get/Put with an invalid library kind, surface the
// sentinel errors.
func TestSettingsRepo_Errors(t *testing.T) {
	db := newTestPostgres(t)
	ctx := context.Background()
	if err := NewWithDB(db).Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := NewSettingsRepo(db, testVault(t))

	if err := repo.PutDownloadClient(ctx, &settings.DownloadClient{Kind: "aria2"}); err == nil {
		t.Error("expected PutDownloadClient to reject an unknown client kind")
	}
	if _, err := repo.GetLibrary(ctx, settings.LibraryKind("anime")); err == nil {
		t.Error("expected GetLibrary to reject an unknown library kind")
	}
	if err := repo.PutLibrary(ctx, settings.LibraryKind("anime"), &settings.LibraryOverride{}); err == nil {
		t.Error("expected PutLibrary to reject an unknown library kind")
	}
}
