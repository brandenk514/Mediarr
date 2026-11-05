package postgres_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	books "github.com/brandenk514/mediarr/internal/domains/books"
	"github.com/brandenk514/mediarr/internal/postgres"
)

// TestBookMetadataStore_UpsertAndGet is the MT3 resolved-work store against
// real Postgres: the provenance row round-trips, a re-fetch refreshes the
// same (source, source_id) row (re-anchoring fetched_at), and an unknown
// work key is a clean miss.
func TestBookMetadataStore_UpsertAndGet(t *testing.T) {
	_, db := startBooksPG(t)
	store := postgres.NewBookMetadataStore(db)
	ctx := context.Background()

	// Unknown work key: a clean miss (ok=false, no error).
	if _, ok, err := store.GetWork(ctx, "OL404W"); err != nil || ok {
		t.Fatalf("get missing = (ok=%v, err=%v), want (false, nil)", ok, err)
	}

	w := books.WorkMetadata{
		WorkKey:   "OL59863W",
		Source:    books.SourceOpenLibrary,
		SourceID:  "OL59863W",
		Title:     "The Dispossessed",
		Author:    "Ursula K. Le Guin",
		Year:      1974,
		CoverURL:  "https://covers.openlibrary.org/b/id/6979680-M.jpg",
		FetchedAt: time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC),
	}
	if err := store.UpsertWork(ctx, w); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, ok, err := store.GetWork(ctx, "OL59863W")
	if err != nil || !ok {
		t.Fatalf("get = (ok=%v, err=%v), want (true, nil)", ok, err)
	}
	if got.Source != books.SourceOpenLibrary || got.SourceID != "OL59863W" ||
		got.WorkKey != "OL59863W" || got.Title != "The Dispossessed" ||
		got.Author != "Ursula K. Le Guin" || got.Year != 1974 || got.CoverURL != w.CoverURL {
		t.Errorf("got = %+v, want %+v", got, w)
	}
	if !got.FetchedAt.Equal(w.FetchedAt) {
		t.Errorf("fetched_at = %v, want %v", got.FetchedAt, w.FetchedAt)
	}

	// A re-fetch upserts the same (source, source_id) row: updated fields,
	// re-anchored fetched_at, no duplicate row.
	refreshed := w
	refreshed.Title = "The Dispossessed (revised)"
	refreshed.Year = 1975
	refreshed.FetchedAt = w.FetchedAt.Add(24 * time.Hour)
	if err := store.UpsertWork(ctx, refreshed); err != nil {
		t.Fatalf("upsert refresh: %v", err)
	}
	got, ok, err = store.GetWork(ctx, "OL59863W")
	if err != nil || !ok {
		t.Fatalf("get after refresh = (ok=%v, err=%v)", ok, err)
	}
	if got.Title != "The Dispossessed (revised)" || got.Year != 1975 {
		t.Errorf("refreshed row = %+v, want title/Year updated", got)
	}
	if !got.FetchedAt.Equal(refreshed.FetchedAt) {
		t.Errorf("fetched_at = %v, want %v (re-anchored)", got.FetchedAt, refreshed.FetchedAt)
	}
	if n := rowCount(t, db, "book_work_metadata"); n != 1 {
		t.Errorf("rows = %d, want 1 (upsert, not insert)", n)
	}
}

// TestBookMetadataStore_MultipleSources is the B6 forward-compat case: a
// Readarr-sourced row and an OpenLibrary row for the same OL work key both
// persist (work_key is indexed, not unique), and GetWork deterministically
// returns the newest-fetched.
func TestBookMetadataStore_MultipleSources(t *testing.T) {
	_, db := startBooksPG(t)
	store := postgres.NewBookMetadataStore(db)
	ctx := context.Background()

	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	ol := books.WorkMetadata{
		WorkKey: "OL59863W", Source: books.SourceOpenLibrary, SourceID: "OL59863W",
		Title: "The Dispossessed", Author: "Ursula K. Le Guin", Year: 1974,
		FetchedAt: now,
	}
	readarr := books.WorkMetadata{
		WorkKey: "OL59863W", Source: "readarr", SourceID: "readarr-101",
		Title: "The Dispossessed", Author: "Ursula K. Le Guin", Year: 1974,
		FetchedAt: now.Add(time.Hour), // newer fetch wins
	}
	for _, w := range []books.WorkMetadata{ol, readarr} {
		if err := store.UpsertWork(ctx, w); err != nil {
			t.Fatalf("upsert %s: %v", w.Source, err)
		}
	}
	got, ok, err := store.GetWork(ctx, "OL59863W")
	if err != nil || !ok {
		t.Fatalf("get = (ok=%v, err=%v)", ok, err)
	}
	if got.Source != "readarr" || got.SourceID != "readarr-101" {
		t.Errorf("get = %+v, want the readarr row (newest fetch)", got)
	}
	if n := rowCount(t, db, "book_work_metadata"); n != 2 {
		t.Errorf("rows = %d, want 2", n)
	}
}

// rowCount returns the row count of a table (test helper for schema-level
// assertions on the shared raw *sql.DB).
func rowCount(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	err := db.QueryRowContext(context.Background(),
		`SELECT count(*) FROM `+table).Scan(&n)
	if err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}
