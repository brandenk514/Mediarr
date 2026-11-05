package books

import (
	"context"
	"errors"
	"testing"
	"time"

	dom "github.com/brandenk514/mediarr/internal/domains/books"
)

// --- MT3 fakes ---

// mt3KeySearcher is a fake dom.BookSearcher that ALSO implements the
// workKeySearcher surface, with per-method call counters (the cache
// assertions).
type mt3KeySearcher struct {
	authors     []dom.BookSearchHit
	works       []dom.BookSearchHit
	keyWorks    map[string][]dom.BookSearchHit
	keyErr      error
	authorCalls int
	workCalls   int
	keyCalls    int
}

func (s *mt3KeySearcher) SearchAuthors(ctx context.Context, q string) ([]dom.BookSearchHit, error) {
	s.authorCalls++
	return s.authors, nil
}

func (s *mt3KeySearcher) SearchWorks(ctx context.Context, author, title string) ([]dom.BookSearchHit, error) {
	s.workCalls++
	return s.works, nil
}

func (s *mt3KeySearcher) SearchWorkByKey(ctx context.Context, key string) ([]dom.BookSearchHit, error) {
	s.keyCalls++
	if s.keyErr != nil {
		return nil, s.keyErr
	}
	return s.keyWorks[key], nil
}

// mt3PlainSearcher implements only dom.BookSearcher (no key surface): the
// MT3 degradation paths.
type mt3PlainSearcher struct {
	authors     []dom.BookSearchHit
	works       []dom.BookSearchHit
	authorCalls int
	workCalls   int
}

func (s *mt3PlainSearcher) SearchAuthors(ctx context.Context, q string) ([]dom.BookSearchHit, error) {
	s.authorCalls++
	return s.authors, nil
}

func (s *mt3PlainSearcher) SearchWorks(ctx context.Context, author, title string) ([]dom.BookSearchHit, error) {
	s.workCalls++
	return s.works, nil
}

// mt3Store is an in-memory dom.MetadataStore with error injection.
type mt3Store struct {
	rows      map[string]dom.WorkMetadata // (source, source_id) -> row
	getErr    error
	upsertErr error
	upserts   int
}

func newMT3Store() *mt3Store { return &mt3Store{rows: map[string]dom.WorkMetadata{}} }

func (s *mt3Store) GetWork(ctx context.Context, workKey string) (dom.WorkMetadata, bool, error) {
	if s.getErr != nil {
		return dom.WorkMetadata{}, false, s.getErr
	}
	for _, w := range s.rows {
		if w.WorkKey == workKey {
			return w, true, nil
		}
	}
	return dom.WorkMetadata{}, false, nil
}

func (s *mt3Store) UpsertWork(ctx context.Context, w dom.WorkMetadata) error {
	if s.upsertErr != nil {
		return s.upsertErr
	}
	s.upserts++
	s.rows[w.Source+"\x00"+w.SourceID] = w
	return nil
}

func mt3Row(key, title, author string, year int, fetched time.Time) dom.WorkMetadata {
	return dom.WorkMetadata{
		WorkKey: key, Source: dom.SourceOpenLibrary, SourceID: key,
		Title: title, Author: author, Year: year,
		CoverURL:  "https://covers.openlibrary.org/b/id/1-M.jpg",
		FetchedAt: fetched,
	}
}

// --- MT3 lookup cache tests ---

// TestBooksLookup_WorkKeyFreshServedFromStore: a fresh provenance row is
// served from the store WITHOUT any Open Library call (the MT3 cache hit).
func TestBooksLookup_WorkKeyFreshServedFromStore(t *testing.T) {
	srch := &mt3KeySearcher{keyWorks: map[string][]dom.BookSearchHit{
		"OL59863W": {{ID: "OL59863W", Name: "STALE TITLE", Author: "Stale Author"}},
	}}
	store := newMT3Store()
	store.rows["openlibrary\x00OL59863W"] = mt3Row("OL59863W", "The Dispossessed", "Ursula K. Le Guin", 1974, time.Now())

	svc := New(Deps{Repo: newMemRepo(), Searcher: srch, MetadataStore: store})
	results, err := svc.BooksLookup(context.Background(), "OL59863W")
	if err != nil {
		t.Fatalf("BooksLookup: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1: %+v", len(results), results)
	}
	r := results[0]
	if r.Kind != dom.LookupBook || r.Name != "The Dispossessed" || r.Author != "Ursula K. Le Guin" || r.Year != 1974 {
		t.Errorf("result = %+v, want the STORE row (not the stale provider data)", r)
	}
	if srch.keyCalls != 0 || srch.authorCalls != 0 || srch.workCalls != 0 {
		t.Errorf("searcher calls = key:%d author:%d work:%d, want 0/0/0 (cache hit, no OL call)",
			srch.keyCalls, srch.authorCalls, srch.workCalls)
	}
	if store.upserts != 0 {
		t.Errorf("upserts = %d, want 0 (fresh row is not re-fetched)", store.upserts)
	}
}

// TestBooksLookup_WorkKeyExpiredRefetches: an expired row triggers a
// re-fetch through the key lookup, and the re-fetch upserts the row
// (re-anchoring fetched_at) and is what gets served.
func TestBooksLookup_WorkKeyExpiredRefetches(t *testing.T) {
	srch := &mt3KeySearcher{keyWorks: map[string][]dom.BookSearchHit{
		"OL59863W": {{ID: "OL59863W", Name: "The Dispossessed", Author: "Ursula K. Le Guin", Year: 1975, CoverURL: "https://covers.openlibrary.org/b/id/2-M.jpg"}},
	}}
	store := newMT3Store()
	store.rows["openlibrary\x00OL59863W"] = mt3Row("OL59863W", "STALE TITLE", "Stale Author", 1900, time.Now().Add(-30*24*time.Hour))

	svc := New(Deps{Repo: newMemRepo(), Searcher: srch, MetadataStore: store})
	results, err := svc.BooksLookup(context.Background(), "OL59863W")
	if err != nil {
		t.Fatalf("BooksLookup: %v", err)
	}
	if len(results) != 1 || results[0].Name != "The Dispossessed" || results[0].Year != 1975 {
		t.Fatalf("results = %+v, want the re-fetched work", results)
	}
	if srch.keyCalls != 1 {
		t.Errorf("keyCalls = %d, want 1 (expired row re-fetches)", srch.keyCalls)
	}
	if store.upserts != 1 {
		t.Fatalf("upserts = %d, want 1 (re-fetch upserts the row)", store.upserts)
	}
	// The re-anchored row is now fresh.
	again, err := svc.BooksLookup(context.Background(), "OL59863W")
	if err != nil {
		t.Fatalf("BooksLookup(after): %v", err)
	}
	if len(again) != 1 || again[0].Name != "The Dispossessed" {
		t.Fatalf("results(after) = %+v, want the refreshed store row", again)
	}
	if srch.keyCalls != 1 {
		t.Errorf("keyCalls after second lookup = %d, want 1 (refreshed row is fresh)", srch.keyCalls)
	}
}

// TestBooksLookup_WorkKeyMissingFetchesAndUpserts: no store row at all —
// the key lookup re-fetches and upserts.
func TestBooksLookup_WorkKeyMissingFetchesAndUpserts(t *testing.T) {
	srch := &mt3KeySearcher{keyWorks: map[string][]dom.BookSearchHit{
		"OL59863W": {{ID: "OL59863W", Name: "The Dispossessed", Author: "Ursula K. Le Guin", Year: 1974}},
	}}
	store := newMT3Store()
	svc := New(Deps{Repo: newMemRepo(), Searcher: srch, MetadataStore: store})

	if _, err := svc.BooksLookup(context.Background(), "/works/OL59863W"); err != nil {
		t.Fatalf("BooksLookup: %v", err) // full-key form normalizes to the bare id
	}
	if srch.keyCalls != 1 {
		t.Errorf("keyCalls = %d, want 1", srch.keyCalls)
	}
	if store.upserts != 1 {
		t.Fatalf("upserts = %d, want 1", store.upserts)
	}
	w, ok, _ := store.GetWork(context.Background(), "OL59863W")
	if !ok || w.Source != dom.SourceOpenLibrary || w.SourceID != "OL59863W" || w.Title != "The Dispossessed" {
		t.Errorf("stored row = (ok=%v, %+v), want the resolved work with provenance", ok, w)
	}
}

// TestBooksLookup_TextWriteThrough: the plain-text lookup upserts every
// resolved work hit into the provenance store (the write-through).
func TestBooksLookup_TextWriteThrough(t *testing.T) {
	srch := &mt3KeySearcher{
		authors: []dom.BookSearchHit{{ID: "", Name: "Ursula K. Le Guin"}},
		works: []dom.BookSearchHit{
			{ID: "OL59863W", Name: "The Dispossessed", Author: "Ursula K. Le Guin", Year: 1974, CoverURL: "https://covers.openlibrary.org/b/id/1-M.jpg"},
			{ID: "", Name: "No Key Work", Author: "Nobody", Year: 2000}, // keyless hit: skipped
		},
	}
	store := newMT3Store()
	svc := New(Deps{Repo: newMemRepo(), Searcher: srch, MetadataStore: store})

	results, err := svc.BooksLookup(context.Background(), "dispossessed")
	if err != nil {
		t.Fatalf("BooksLookup: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("results = %d, want 3 (1 author + 2 works): %+v", len(results), results)
	}
	if store.upserts != 1 {
		t.Fatalf("upserts = %d, want 1 (only the keyed work)", store.upserts)
	}
	w, ok, _ := store.GetWork(context.Background(), "OL59863W")
	if !ok || w.Title != "The Dispossessed" || w.Author != "Ursula K. Le Guin" ||
		w.Year != 1974 || w.CoverURL != "https://covers.openlibrary.org/b/id/1-M.jpg" {
		t.Errorf("stored row = (ok=%v, %+v), want title/author/year/cover from the hit", ok, w)
	}
}

// TestBooksLookup_StoreFailureDegrades: a store read failure must not break
// the key lookup — it degrades to the provider path.
func TestBooksLookup_StoreFailureDegrades(t *testing.T) {
	srch := &mt3KeySearcher{keyWorks: map[string][]dom.BookSearchHit{
		"OL59863W": {{ID: "OL59863W", Name: "The Dispossessed", Author: "Ursula K. Le Guin", Year: 1974}},
	}}
	store := newMT3Store()
	store.getErr = errors.New("boom")
	svc := New(Deps{Repo: newMemRepo(), Searcher: srch, MetadataStore: store})

	results, err := svc.BooksLookup(context.Background(), "OL59863W")
	if err != nil {
		t.Fatalf("BooksLookup: %v", err)
	}
	if len(results) != 1 || results[0].Name != "The Dispossessed" {
		t.Errorf("results = %+v, want the provider hit (store failure degraded)", results)
	}
	if srch.keyCalls != 1 {
		t.Errorf("keyCalls = %d, want 1", srch.keyCalls)
	}
}

// TestBooksLookup_NoStoreUncached: without a store the key lookup goes
// straight to the provider (pre-MT3 behavior), and an OL key miss (empty
// provider answer) is an empty result, not an error.
func TestBooksLookup_NoStoreUncached(t *testing.T) {
	srch := &mt3KeySearcher{keyWorks: map[string][]dom.BookSearchHit{}}
	svc := New(Deps{Repo: newMemRepo(), Searcher: srch})

	results, err := svc.BooksLookup(context.Background(), "OL404W")
	if err != nil {
		t.Fatalf("BooksLookup: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("results = %+v, want empty (OL has no such work)", results)
	}
	if srch.keyCalls != 1 {
		t.Errorf("keyCalls = %d, want 1", srch.keyCalls)
	}
}

// TestBooksLookup_PlainSearcherDegrades: a searcher without the key surface
// serves a fresh store row (the store hit needs no key lookup) and degrades
// to the plain-text path on a store miss.
func TestBooksLookup_PlainSearcherDegrades(t *testing.T) {
	store := newMT3Store()
	store.rows["openlibrary\x00OL59863W"] = mt3Row("OL59863W", "The Dispossessed", "Ursula K. Le Guin", 1974, time.Now())
	srch := &mt3PlainSearcher{authors: []dom.BookSearchHit{{Name: "Some Author"}}}
	svc := New(Deps{Repo: newMemRepo(), Searcher: srch, MetadataStore: store})

	// Fresh row: served from the store, no searcher calls at all.
	results, err := svc.BooksLookup(context.Background(), "OL59863W")
	if err != nil {
		t.Fatalf("BooksLookup: %v", err)
	}
	if len(results) != 1 || results[0].Name != "The Dispossessed" {
		t.Fatalf("results = %+v, want the store row", results)
	}
	if srch.authorCalls != 0 || srch.workCalls != 0 {
		t.Errorf("searcher calls = author:%d work:%d, want 0/0", srch.authorCalls, srch.workCalls)
	}

	// Store miss: degrades to the plain-text path.
	results, err = svc.BooksLookup(context.Background(), "OL99999W")
	if err != nil {
		t.Fatalf("BooksLookup(miss): %v", err)
	}
	if len(results) != 1 || results[0].Kind != dom.LookupAuthor {
		t.Errorf("results(miss) = %+v, want the degraded text-search hit", results)
	}
}

// --- MT3 refresh command tests ---

// TestRefreshWorkMetadata_BypassesTTL: even a FRESH store row is re-fetched
// on refresh (the TTL is deliberately bypassed); the row is upserted with a
// re-anchored fetched_at and the refreshed row is returned.
func TestRefreshWorkMetadata_BypassesTTL(t *testing.T) {
	srch := &mt3KeySearcher{keyWorks: map[string][]dom.BookSearchHit{
		"OL59863W": {{ID: "OL59863W", Name: "The Dispossessed (2nd ed.)", Author: "Ursula K. Le Guin", Year: 1976}},
	}}
	store := newMT3Store()
	store.rows["openlibrary\x00OL59863W"] = mt3Row("OL59863W", "The Dispossessed", "Ursula K. Le Guin", 1974, time.Now())
	svc := New(Deps{Repo: newMemRepo(), Searcher: srch, MetadataStore: store})

	before := time.Now()
	w, err := svc.RefreshWorkMetadata(context.Background(), "/works/OL59863W")
	if err != nil {
		t.Fatalf("RefreshWorkMetadata: %v", err)
	}
	if w.Title != "The Dispossessed (2nd ed.)" || w.Year != 1976 {
		t.Errorf("refreshed = %+v, want the re-fetched data", w)
	}
	if srch.keyCalls != 1 {
		t.Errorf("keyCalls = %d, want 1 (refresh bypasses the fresh row)", srch.keyCalls)
	}
	if store.upserts != 1 {
		t.Errorf("upserts = %d, want 1", store.upserts)
	}
	if w.FetchedAt.Before(before) || w.FetchedAt.IsZero() {
		t.Errorf("fetched_at = %v, want re-anchored to now", w.FetchedAt)
	}
}

// TestRefreshWorkMetadata_Errors covers the refresh command's error map:
// empty key, missing searcher, missing store, and an OL miss.
func TestRefreshWorkMetadata_Errors(t *testing.T) {
	ctx := context.Background()
	keyHit := &mt3KeySearcher{keyWorks: map[string][]dom.BookSearchHit{}}
	store := newMT3Store()

	cases := []struct {
		name string
		deps Deps
		key  string
		want error
	}{
		{"empty key", Deps{Repo: newMemRepo(), Searcher: keyHit, MetadataStore: store}, "", ErrWorkKeyRequired},
		{"no searcher", Deps{Repo: newMemRepo(), MetadataStore: store}, "OL1W", ErrSearcherNotConfigured},
		{"no store", Deps{Repo: newMemRepo(), Searcher: keyHit}, "OL1W", ErrMetadataStoreNotConfigured},
		{"plain searcher (no key surface)", Deps{Repo: newMemRepo(), Searcher: &mt3PlainSearcher{}, MetadataStore: store}, "OL1W", ErrSearcherNotConfigured},
		{"OL miss", Deps{Repo: newMemRepo(), Searcher: keyHit, MetadataStore: store}, "OL404W", ErrWorkNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := New(tc.deps)
			_, err := svc.RefreshWorkMetadata(ctx, tc.key)
			if !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
		})
	}
}
