package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/brandenk514/mediarr/internal/indexers"
)

// newMigratedDB boots a Postgres container, applies all migrations, and returns
// the migrated *sql.DB.
func newMigratedDB(t *testing.T) *sql.DB {
	t.Helper()
	db := newTestPostgres(t)
	st := NewWithDB(db)
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// TestSearchActivity_RecordAndList exercises the query-history round trip:
// recording rows, newest-first ordering, categories round-trip, and totals.
func TestSearchActivity_RecordAndList(t *testing.T) {
	db := newMigratedDB(t)
	ctx := context.Background()
	r := NewSearchActivityRepo(db)

	now := time.Now().UTC()
	idA := int64(1)
	idB := int64(2)
	// idC is nil: a failed build has no resolved id.
	qs := []indexers.QueryLog{
		{Indexer: "A", IndexerID: &idA, Term: "Breaking Bad", Categories: []string{"TV"}, OK: true, ElapsedMS: 120, At: now.Add(-3 * time.Hour)},
		{Indexer: "B", IndexerID: &idB, Term: "big buck bunny", Categories: []string{}, OK: false, ElapsedMS: 45, At: now.Add(-2 * time.Hour)},
		{Indexer: "", IndexerID: nil, Term: "big buck bunny", Categories: []string{"TV"}, OK: false, ElapsedMS: 9, At: now.Add(-1 * time.Hour)},
	}
	for _, q := range qs {
		if err := r.RecordQuery(ctx, q); err != nil {
			t.Fatalf("record query: %v", err)
		}
	}

	// Newest first: the -1h row (empty indexer) should be first.
	items, total, err := r.ListQueries(ctx, indexers.QueryFilter{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 3 {
		t.Fatalf("total = %d, want 3", total)
	}
	if len(items) != 3 {
		t.Fatalf("items = %d, want 3", len(items))
	}
	if items[0].Indexer != "" {
		t.Errorf("newest-first: items[0].Indexer = %q, want \"\"", items[0].Indexer)
	}
	if items[0].IndexerID != nil {
		t.Errorf("items[0].IndexerID should be nil for a failed build, got %v", *items[0].IndexerID)
	}
	// B's categories were stored as [] and should round-trip to a non-nil empty
	// slice (the API serializes them to an empty array, never null).
	if items[1].Indexer != "B" || len(items[1].Categories) != 0 {
		t.Errorf("items[1] (B): Indexer=%q Categories=%v, want B with empty categories", items[1].Indexer, items[1].Categories)
	}
}

// TestSearchActivity_ListPagination exercises page/page_size and the total
// count (independent of paging).
func TestSearchActivity_ListPagination(t *testing.T) {
	db := newMigratedDB(t)
	ctx := context.Background()
	r := NewSearchActivityRepo(db)

	now := time.Now().UTC()
	for i := 0; i < 5; i++ {
		// Distinct terms and staggered times so ordering is deterministic.
		if err := r.RecordQuery(ctx, indexers.QueryLog{Indexer: "A", Term: "term" + string(rune('a'+i)), At: now.Add(-time.Duration(i) * time.Hour)}); err != nil {
			t.Fatalf("record: %v", err)
		}
	}

	// Page 1, size 2: the two newest (i=0, i=1).
	page1, total, err := r.ListQueries(ctx, indexers.QueryFilter{Page: 1, PageSize: 2})
	if err != nil {
		t.Fatalf("list page1: %v", err)
	}
	if total != 5 {
		t.Fatalf("total = %d, want 5", total)
	}
	if len(page1) != 2 {
		t.Fatalf("page1 = %d, want 2", len(page1))
	}
	if page1[0].Term != "term"+string(rune('a')) {
		t.Errorf("page1[0].Term = %q, want %q (newest first)", page1[0].Term, "term"+string(rune('a')))
	}

	// Page 3, size 2: the oldest single row (i=4).
	page3, _, err := r.ListQueries(ctx, indexers.QueryFilter{Page: 3, PageSize: 2})
	if err != nil {
		t.Fatalf("list page3: %v", err)
	}
	if len(page3) != 1 {
		t.Fatalf("page3 = %d, want 1", len(page3))
	}
	if page3[0].Term != "term"+string(rune('a'+4)) {
		t.Errorf("page3[0].Term = %q, want %q", page3[0].Term, "term"+string(rune('a'+4)))
	}
}

// TestSearchActivity_Filters exercises the indexer (exact) and term
// (case-insensitive substring) filters.
func TestSearchActivity_Filters(t *testing.T) {
	db := newMigratedDB(t)
	ctx := context.Background()
	r := NewSearchActivityRepo(db)
	now := time.Now().UTC()
	for _, q := range []indexers.QueryLog{
		{Indexer: "A", Term: "Breaking Bad", At: now},
		{Indexer: "B", Term: "breaking change", At: now},
		{Indexer: "B", Term: "breakdance", At: now.Add(time.Minute)},
	} {
		if err := r.RecordQuery(ctx, q); err != nil {
			t.Fatalf("record: %v", err)
		}
	}

	// Indexer = B (exact).
	b, total, err := r.ListQueries(ctx, indexers.QueryFilter{Indexer: "B", Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("filter indexer: %v", err)
	}
	if total != 2 || len(b) != 2 {
		t.Fatalf("indexer B: total=%d len=%d, want 2/2", total, len(b))
	}
	for _, it := range b {
		if it.Indexer != "B" {
			t.Errorf("indexer B filter leaked %q", it.Indexer)
		}
	}

	// Term = "break" (case-insensitive substring) matches all three.
	all, total, err := r.ListQueries(ctx, indexers.QueryFilter{Term: "break", Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("filter term: %v", err)
	}
	if total != 3 || len(all) != 3 {
		t.Fatalf("term break: total=%d len=%d, want 3/3", total, len(all))
	}
}

// TestSearchActivity_DeleteAll clears the query history (not grabs) and
// returns the deleted count.
func TestSearchActivity_DeleteAll(t *testing.T) {
	db := newMigratedDB(t)
	ctx := context.Background()
	r := NewSearchActivityRepo(db)
	now := time.Now().UTC()
	for i := 0; i < 3; i++ {
		if err := r.RecordQuery(ctx, indexers.QueryLog{Indexer: "A", Term: "t", At: now}); err != nil {
			t.Fatalf("record query: %v", err)
		}
	}
	if err := r.RecordGrab(ctx, indexers.GrabLog{Indexer: "A", Title: "x", Protocol: "torrent", At: now}); err != nil {
		t.Fatalf("record grab: %v", err)
	}

	n, err := r.DeleteAllQueries(ctx)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if n != 3 {
		t.Fatalf("deleted = %d, want 3", n)
	}
	// Queries are gone; grabs survive.
	if _, total, _ := r.ListQueries(ctx, indexers.QueryFilter{}); total != 0 {
		t.Fatalf("queries after delete = %d, want 0", total)
	}
	qt, gt, err := r.Totals(ctx)
	if err != nil {
		t.Fatalf("totals: %v", err)
	}
	if qt != 0 || gt != 1 {
		t.Errorf("totals after delete = (%d,%d), want (0,1)", qt, gt)
	}
}

// TestSearchActivity_Totals exercises the all-time counts.
func TestSearchActivity_Totals(t *testing.T) {
	db := newMigratedDB(t)
	ctx := context.Background()
	r := NewSearchActivityRepo(db)
	now := time.Now().UTC()
	for i := 0; i < 2; i++ {
		if err := r.RecordQuery(ctx, indexers.QueryLog{Indexer: "A", Term: "t", OK: i%2 == 0, At: now}); err != nil {
			t.Fatalf("record query: %v", err)
		}
	}
	if err := r.RecordGrab(ctx, indexers.GrabLog{Indexer: "A", Title: "x", Protocol: "nzb", At: now}); err != nil {
		t.Fatalf("record grab: %v", err)
	}
	qt, gt, err := r.Totals(ctx)
	if err != nil {
		t.Fatalf("totals: %v", err)
	}
	if qt != 2 || gt != 1 {
		t.Fatalf("totals = (%d,%d), want (2,1)", qt, gt)
	}
}

// TestSearchActivity_DailyCounts exercises the per-(indexer,kind,day) counts.
func TestSearchActivity_DailyCounts(t *testing.T) {
	db := newMigratedDB(t)
	ctx := context.Background()
	r := NewSearchActivityRepo(db)

	d1 := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	d2 := time.Date(2026, 10, 21, 8, 0, 0, 0, time.UTC)
	d3 := time.Date(2026, 10, 25, 23, 0, 0, 0, time.UTC)
	since := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)

	// A: 2 queries on d1, 1 on d2, 1 grab on d1. B: 1 query on d1. C: 1 grab on d3.
	records := []struct {
		q *indexers.QueryLog
		g *indexers.GrabLog
	}{
		{q: &indexers.QueryLog{Indexer: "A", Term: "t", At: d1}},
		{q: &indexers.QueryLog{Indexer: "A", Term: "t", At: d1.Add(time.Minute)}},
		{q: &indexers.QueryLog{Indexer: "A", Term: "t", At: d2}},
		{q: &indexers.QueryLog{Indexer: "B", Term: "t", At: d1}},
		{g: &indexers.GrabLog{Indexer: "A", Title: "x", Protocol: "torrent", At: d1}},
		{g: &indexers.GrabLog{Indexer: "C", Title: "x", Protocol: "nzb", At: d3}},
	}
	for _, rec := range records {
		if rec.q != nil {
			if err := r.RecordQuery(ctx, *rec.q); err != nil {
				t.Fatalf("record query: %v", err)
			}
		}
		if rec.g != nil {
			if err := r.RecordGrab(ctx, *rec.g); err != nil {
				t.Fatalf("record grab: %v", err)
			}
		}
	}

	counts, err := r.DailyCounts(ctx, since)
	if err != nil {
		t.Fatalf("daily counts: %v", err)
	}
	// Index the raw counts by (indexer, kind, UTC-date-string). A date string
	// sidesteps time.Time location-pointer differences between the scanned
	// value and values we build locally.
	type key struct{ indexer, kind, day string }
	got := make(map[key]int, len(counts))
	for _, c := range counts {
		got[key{c.Indexer, c.Kind, c.Day.UTC().Format("2006-01-02")}] = c.Count
	}
	want := map[key]int{
		{"A", "query", "2026-10-20"}: 2,
		{"A", "query", "2026-10-21"}: 1,
		{"B", "query", "2026-10-20"}: 1,
		{"A", "grab", "2026-10-20"}:  1,
		{"C", "grab", "2026-10-25"}:  1,
	}
	if len(got) != len(want) {
		t.Fatalf("daily count cells = %d, want %d (got: %v)", len(got), len(want), got)
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("daily count %v = %d, want %d", k, got[k], w)
		}
	}
}
