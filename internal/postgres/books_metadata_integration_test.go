package postgres_test

import (
	"context"
	"database/sql"
	"encoding/json"
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

// TestBookMetadataStore_EditionsRoundTrip is the MT2 cache against real
// Postgres: the work row's cached editions round-trip through the JSONB
// column (a details fetch stores them, the re-upsert refreshes them, and a
// row written WITHOUT editions reads back the empty list — the '[]'
// default, never NULL).
func TestBookMetadataStore_EditionsRoundTrip(t *testing.T) {
	_, db := startBooksPG(t)
	store := postgres.NewBookMetadataStore(db)
	ctx := context.Background()

	// A plain (search-resolved) row carries no editions: the column is '[]'.
	plain := books.WorkMetadata{
		WorkKey: "OL66554W", Source: books.SourceOpenLibrary, SourceID: "OL66554W",
		Title: "Pride and Prejudice", Author: "Jane Austen", Year: 1813,
		FetchedAt: time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC),
	}
	if err := store.UpsertWork(ctx, plain); err != nil {
		t.Fatalf("upsert plain: %v", err)
	}
	got, ok, err := store.GetWork(ctx, "OL66554W")
	if err != nil || !ok {
		t.Fatalf("get = (ok=%v, err=%v)", ok, err)
	}
	if string(got.EditionsJSON) != "[]" {
		t.Errorf("editions = %s, want [] (a search resolution carries none)", got.EditionsJSON)
	}

	// A details-fetch upsert refreshes the SAME row with its editions.
	details := got
	details.EditionsJSON = []byte(`[{"key":"OL25828717M","title":"Pride and Prejudice","isbn":"9780141439518","formats":["EPUB"],"languages":["eng"],"publishers":["HarperCollins"],"publish_date":"2018-11-15","year":2018,"pages":487}]`)
	if err := store.UpsertWork(ctx, details); err != nil {
		t.Fatalf("upsert editions: %v", err)
	}
	got, ok, err = store.GetWork(ctx, "OL66554W")
	if err != nil || !ok {
		t.Fatalf("get after editions = (ok=%v, err=%v)", ok, err)
	}
	var eds []books.WorkEdition
	if err := json.Unmarshal(got.EditionsJSON, &eds); err != nil {
		t.Fatalf("unmarshal editions: %v", err)
	}
	if len(eds) != 1 || eds[0].Key != "OL25828717M" || eds[0].ISBN != "9780141439518" ||
		eds[0].Year != 2018 || eds[0].Pages != 487 {
		t.Errorf("editions = %+v, want the cached edition", eds)
	}
	if n := rowCount(t, db, "book_work_metadata"); n != 1 {
		t.Errorf("rows = %d, want 1 (the editions refresh upserts the row)", n)
	}

	// A second upsert WITHOUT editions stores '[]' again (refreshed, not
	// stale-carried).
	if err := store.UpsertWork(ctx, plain); err != nil {
		t.Fatalf("upsert plain (again): %v", err)
	}
	got, ok, err = store.GetWork(ctx, "OL66554W")
	if err != nil || !ok {
		t.Fatalf("get = (ok=%v, err=%v)", ok, err)
	}
	if string(got.EditionsJSON) != "[]" {
		t.Errorf("editions = %s, want [] (a plain re-upsert stores no editions)", got.EditionsJSON)
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
