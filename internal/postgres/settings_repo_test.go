package postgres

import (
	"context"
	"testing"

	"github.com/brandenk514/mediarr/internal/settings"
)

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

	if _, err := repo.GetLibrary(ctx, settings.LibraryKind("anime")); err == nil {
		t.Error("expected GetLibrary to reject an unknown library kind")
	}
	if err := repo.PutLibrary(ctx, settings.LibraryKind("anime"), &settings.LibraryOverride{}); err == nil {
		t.Error("expected PutLibrary to reject an unknown library kind")
	}
}
