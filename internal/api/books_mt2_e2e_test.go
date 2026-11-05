package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/brandenk514/mediarr/internal/auth"
	booksdom "github.com/brandenk514/mediarr/internal/domains/books"
	"github.com/brandenk514/mediarr/internal/health"
	"github.com/brandenk514/mediarr/internal/providers"
	bookssvc "github.com/brandenk514/mediarr/internal/services/books"
)

// --- MT2 API fakes ---

// mt2OLStub is a stub Open Library that answers both the MT3 work-key
// search (/search.json, `q=key:OL59863W`) and the MT2 work document
// (/works/OL59863W.json), counting every hit per path. failDoc makes the
// doc endpoint answer 502 (the fetch-failure degradation test), and the
// served doc is swappable so the refresh test can verify a CHANGED
// edition list is actually re-fetched and stored. The tests never hit
// the real network.
type mt2OLStub struct {
	mu      sync.Mutex
	srv     *httptest.Server
	doc     any
	failDoc bool
	byPath  map[string]int
}

// mt2WorkDoc is the work document in the real /works/{key}.json shape
// (authors as name objects, covers as an id list) — the olWorkDoc subset
// the adapter actually parses, with the edition array swapped in.
func mt2WorkDoc(editions any) map[string]any {
	return map[string]any{
		"key":                "/works/OL59863W",
		"title":              "The Dispossessed",
		"authors":            []map[string]any{{"name": "Ursula K. Le Guin"}},
		"covers":             []int{12345},
		"first_publish_year": 1974,
		"editions":           editions,
	}
}

// mt2EditionsOne is the work's single original edition (the 1974 first
// printing) in the work document's editions-array shape.
var mt2EditionsOne = []map[string]any{{
	"key":             "/works/OL59863W/OL25828717M",
	"title":           "The Dispossessed: Who Owns the Future?",
	"isbn_13":         []string{"9780140449335"},
	"languages":       []string{"eng"},
	"publish_date":    "1974",
	"number_of_pages": 544,
}}

// mt2EditionsTwo is the UPDATED document: OL has since added a 2020
// paperback — the refresh test verifies the stored editions follow the
// changed document.
var mt2EditionsTwo = []map[string]any{
	mt2EditionsOne[0],
	{
		"key":             "/works/OL59863W/OL39999999M",
		"title":           "The Dispossessed",
		"isbn_13":         []string{"9780593189574"},
		"languages":       []string{"eng"},
		"publish_date":    "2020",
		"number_of_pages": 576,
	},
}

func newMT2OLStub(t *testing.T, doc any) *mt2OLStub {
	t.Helper()
	s := &mt2OLStub{doc: doc, byPath: map[string]int{}}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.byPath[r.URL.Path]++
		fail, doc := s.failDoc, s.doc
		s.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/works/OL59863W.json":
			if fail {
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			_ = json.NewEncoder(w).Encode(doc)
		case "/search.json":
			if r.URL.Query().Get("q") == "key:OL59863W" {
				docs := []map[string]any{{
					"key":                "/works/OL59863W",
					"title":              "The Dispossessed",
					"author_name":        []string{"Ursula K. Le Guin"},
					"first_publish_year": 1974,
					"cover_i":            12345,
				}}
				_ = json.NewEncoder(w).Encode(map[string]any{"numFound": 1, "docs": docs})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"numFound": 0, "docs": []any{}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *mt2OLStub) URL() string { return s.srv.URL }

// calls is the per-path hit count (the "did we call Open Library?" meter).
func (s *mt2OLStub) calls(path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.byPath[path]
}

// setDoc swaps the served work document (the refresh test).
func (s *mt2OLStub) setDoc(doc any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.doc = doc
}

// setFailDoc flips the doc endpoint's 502 (the degradation test).
func (s *mt2OLStub) setFailDoc(fail bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failDoc = fail
}

// newBooksMT2Server builds a FRESH authenticated server wired the way
// main.go does (real OL adapter at a stub + in-memory repo + MT3 store).
// withOL=false leaves the Open Library searcher unwired (nil) — the
// not-configured editions path (503).
func newBooksMT2Server(t *testing.T, store booksdom.MetadataStore, stub *mt2OLStub, withOL bool) *booksB2Client {
	t.Helper()
	s := NewServer(health.NewChecker(nil, nil))
	s.SetAuth(&AuthDeps{Repo: newFakeStore()})
	hash, err := auth.HashPassword("a-very-long-password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.authDeps.Repo.CreateUser(context.Background(), "admin", "", "admin", hash); err != nil {
		t.Fatal(err)
	}
	rec := postJSON(t, s.Handler(), "/api/v1/auth/login", loginRequest{
		Username: "admin", Password: "a-very-long-password",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d", rec.Code)
	}
	var lr loginResponse
	if err := json.NewDecoder(rec.Body).Decode(&lr); err != nil {
		t.Fatal(err)
	}

	var searcher booksdom.BookSearcher
	if withOL {
		ol := providers.NewOpenLibrary()
		ol.BaseURL = stub.URL()
		searcher = bookssvc.NewOpenLibrarySearcher(ol)
	}
	svc := bookssvc.New(bookssvc.Deps{
		Repo:           newKindsBooksRepo(),
		DefaultProfile: "Best",
		Searcher:       searcher,
		MetadataStore:  store,
	})
	s.SetBooks(&BooksDeps{Svc: svc})
	return &booksB2Client{h: s.Handler(), token: lr.Token}
}

// mt2MarshalEditions serializes a cached-editions payload for seeding a
// provenance-store row.
func mt2MarshalEditions(t *testing.T, eds []booksdom.WorkEdition) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(eds)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// --- MT2 editions endpoint tests ---

// TestBooksMT2_EditionsWriteThrough is the editions read path on an
// UNKNOWN work: the first fetch is ONE Open Library work-document call,
// the row is write-throughed into the provenance store with its fetched
// editions, and the repeat call within the TTL is served from the store
// with zero further OL calls.
func TestBooksMT2_EditionsWriteThrough(t *testing.T) {
	store := newMT3MemStore()
	stub := newMT2OLStub(t, mt2WorkDoc(mt2EditionsOne))
	c := newBooksMT2Server(t, store, stub, true)

	rec := c.get(t, "/api/v1/search/books/works/OL59863W/editions")
	if rec.Code != http.StatusOK {
		t.Fatalf("editions status = %d body=%s", rec.Code, rec.Body.String())
	}
	var got bookWorkDetails
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.WorkKey != "OL59863W" || got.Title != "The Dispossessed" || got.FirstPublishYear != 1974 {
		t.Errorf("work = %+v, want the fetched work", got)
	}
	if len(got.Authors) != 1 || got.Authors[0] != "Ursula K. Le Guin" {
		t.Errorf("authors = %v, want [Ursula K. Le Guin]", got.Authors)
	}
	if got.CoverURL != "https://covers.openlibrary.org/b/id/12345-M.jpg" {
		t.Errorf("cover_url = %q, want the covers.openlibrary.org URL", got.CoverURL)
	}
	if len(got.Editions) != 1 {
		t.Fatalf("editions = %+v, want 1", got.Editions)
	}
	e := got.Editions[0]
	if e.Key != "OL25828717M" || e.ISBN != "9780140449335" || len(e.Languages) != 1 || e.Languages[0] != "eng" || e.Year != 1974 || e.Pages != 544 {
		t.Errorf("edition = %+v, want the fetched edition", e)
	}
	if n := stub.calls("/works/OL59863W.json"); n != 1 {
		t.Errorf("doc calls = %d, want 1 (the work-details fetch)", n)
	}
	if n := stub.calls("/search.json"); n != 0 {
		t.Errorf("search calls = %d, want 0 (the editions path reads the work doc, not the search)", n)
	}
	if store.upsertCount() != 1 {
		t.Errorf("store upserts = %d, want 1 (write-through)", store.upsertCount())
	}
	row, ok := store.rows["openlibrary\x00OL59863W"]
	if !ok || len(row.EditionsJSON) == 0 {
		t.Fatalf("stored row = %+v ok=%v, want the write-through row with cached editions", row, ok)
	}

	// Repeat within the TTL: served from the store, no further OL call.
	rec = c.get(t, "/api/v1/search/books/works/OL59863W/editions")
	if rec.Code != http.StatusOK {
		t.Fatalf("editions(after) status = %d body=%s", rec.Code, rec.Body.String())
	}
	var got2 bookWorkDetails
	if err := json.NewDecoder(rec.Body).Decode(&got2); err != nil {
		t.Fatal(err)
	}
	if len(got2.Editions) != 1 || got2.Editions[0].Key != "OL25828717M" || got2.Editions[0].ISBN != "9780140449335" {
		t.Errorf("editions(after) = %+v, want the cached edition", got2.Editions)
	}
	if n := stub.calls("/works/OL59863W.json"); n != 1 {
		t.Errorf("doc calls after repeat = %d, want 1 (served from the store)", n)
	}
}

// TestBooksMT2_EditionsFreshRowCached is the MT2 cache hit through the
// HTTP layer: a fresh provenance-store row (within the TTL) serves its
// cached editions WITHOUT an Open Library call.
func TestBooksMT2_EditionsFreshRowCached(t *testing.T) {
	store := newMT3MemStore()
	eds := mt2MarshalEditions(t, []booksdom.WorkEdition{{
		Key: "OL25828717M", Title: "The Dispossessed: Who Owns the Future?",
		ISBN: "9780140449335", Languages: []string{"eng"},
		PublishDate: "1974", Year: 1974, Pages: 544,
	}})
	store.rows["openlibrary\x00OL59863W"] = booksdom.WorkMetadata{
		WorkKey: "OL59863W", Source: "openlibrary", SourceID: "OL59863W",
		Title: "The Dispossessed", Author: "Ursula K. Le Guin", Year: 1974,
		CoverURL:     "https://covers.openlibrary.org/b/id/12345-M.jpg",
		FetchedAt:    time.Now(),
		EditionsJSON: eds,
	}
	stub := newMT2OLStub(t, mt2WorkDoc(mt2EditionsOne))
	c := newBooksMT2Server(t, store, stub, true)

	rec := c.get(t, "/api/v1/search/books/works/OL59863W/editions")
	if rec.Code != http.StatusOK {
		t.Fatalf("editions status = %d body=%s", rec.Code, rec.Body.String())
	}
	var got bookWorkDetails
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.WorkKey != "OL59863W" || got.Title != "The Dispossessed" {
		t.Errorf("work = %+v, want the row's work fields", got)
	}
	if len(got.Editions) != 1 || got.Editions[0].Key != "OL25828717M" || got.Editions[0].ISBN != "9780140449335" {
		t.Errorf("editions = %+v, want the row's cached editions", got.Editions)
	}
	if n := stub.calls("/works/OL59863W.json") + stub.calls("/search.json"); n != 0 {
		t.Errorf("OL calls = %d, want 0 (fresh row served from the store)", n)
	}
}

// TestBooksMT2_RefreshUpdatesEditions is the MT2 extension of the MT3
// refresh: POST /refresh?key=<workKey> re-fetches the work (the key
// search + the work document) and the STORED editions follow the changed
// document.
func TestBooksMT2_RefreshUpdatesEditions(t *testing.T) {
	store := newMT3MemStore()
	stub := newMT2OLStub(t, mt2WorkDoc(mt2EditionsOne))
	c := newBooksMT2Server(t, store, stub, true)

	// Seed the row through the editions endpoint (write-through, v1).
	rec := c.get(t, "/api/v1/search/books/works/OL59863W/editions")
	if rec.Code != http.StatusOK {
		t.Fatalf("editions status = %d body=%s", rec.Code, rec.Body.String())
	}

	// Open Library has since changed: the work now has two editions.
	stub.setDoc(mt2WorkDoc(mt2EditionsTwo))

	rec = c.post(t, "/api/v1/search/books/refresh?key=OL59863W", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh status = %d body=%s", rec.Code, rec.Body.String())
	}
	if n := stub.calls("/works/OL59863W.json"); n != 2 {
		t.Errorf("doc calls = %d, want 2 (the refresh re-fetched the work details)", n)
	}
	if n := stub.calls("/search.json"); n != 1 {
		t.Errorf("search calls = %d, want 1 (the refresh's forced key re-fetch)", n)
	}

	// The stored row carries the UPDATED editions (the write-through).
	row, ok := store.rows["openlibrary\x00OL59863W"]
	if !ok {
		t.Fatal("stored row missing after refresh")
	}
	var stored []bookWorkEdition
	if err := json.Unmarshal(row.EditionsJSON, &stored); err != nil {
		t.Fatalf("stored editions unmarshal: %v", err)
	}
	if len(stored) != 2 || stored[1].Key != "OL39999999M" || stored[1].Year != 2020 {
		t.Errorf("stored editions = %+v, want the updated 2-edition list", stored)
	}

	// The editions endpoint now serves the updated list from the fresh
	// row — no further OL calls.
	rec = c.get(t, "/api/v1/search/books/works/OL59863W/editions")
	if rec.Code != http.StatusOK {
		t.Fatalf("editions(after refresh) status = %d body=%s", rec.Code, rec.Body.String())
	}
	var got bookWorkDetails
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got.Editions) != 2 || got.Editions[1].Key != "OL39999999M" {
		t.Errorf("editions(after refresh) = %+v, want the refreshed 2-edition list", got.Editions)
	}
	if n := stub.calls("/works/OL59863W.json"); n != 2 {
		t.Errorf("doc calls after the refresh read = %d, want 2 (served from the fresh row)", n)
	}
}

// TestBooksMT2_EditionsFetchFailureDegrades pins the MT2 degradation
// through the HTTP layer: a failed work-document fetch (the stub answers
// 502) with a STALE row present serves the row's cached editions — 200,
// never a 5xx (the add flow must not break).
func TestBooksMT2_EditionsFetchFailureDegrades(t *testing.T) {
	store := newMT3MemStore()
	cached := mt2MarshalEditions(t, []booksdom.WorkEdition{{
		Key: "OL25828717M", ISBN: "9780140449335",
		Languages: []string{"eng"}, PublishDate: "1974", Year: 1974,
	}})
	store.rows["openlibrary\x00OL59863W"] = booksdom.WorkMetadata{
		WorkKey:      "OL59863W",
		Source:       "openlibrary",
		SourceID:     "OL59863W",
		Title:        "The Dispossessed",
		Author:       "Ursula K. Le Guin",
		Year:         1974,
		FetchedAt:    time.Now().Add(-8 * 24 * time.Hour), // older than the 7d TTL
		EditionsJSON: cached,
	}
	stub := newMT2OLStub(t, mt2WorkDoc(mt2EditionsOne))
	stub.setFailDoc(true)
	c := newBooksMT2Server(t, store, stub, true)

	rec := c.get(t, "/api/v1/search/books/works/OL59863W/editions")
	if rec.Code != http.StatusOK {
		t.Fatalf("editions status = %d, want 200 (degraded, not 5xx) body=%s", rec.Code, rec.Body.String())
	}
	var got bookWorkDetails
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.WorkKey != "OL59863W" || got.Title != "The Dispossessed" {
		t.Errorf("work = %+v, want the stale row's work fields", got)
	}
	if len(got.Editions) != 1 || got.Editions[0].Key != "OL25828717M" || got.Editions[0].ISBN != "9780140449335" {
		t.Errorf("editions = %+v, want the row's cached editions", got.Editions)
	}
	if n := stub.calls("/works/OL59863W.json"); n != 1 {
		t.Errorf("doc calls = %d, want 1 (the fetch was attempted, then failed)", n)
	}
}

// TestBooksMT2_EditionsNotConfigured pins the 503 path: with no Open
// Library searcher wired (the books metadata source unconfigured) and no
// cached row, the editions endpoint answers 503 — the same unconfigured-
// component 503 the MT3 e2e test triggers by unwiring the store.
func TestBooksMT2_EditionsNotConfigured(t *testing.T) {
	store := newMT3MemStore()
	stub := newMT2OLStub(t, mt2WorkDoc(mt2EditionsOne))
	c := newBooksMT2Server(t, store, stub, false)

	rec := c.get(t, "/api/v1/search/books/works/OL59863W/editions")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("editions status = %d, want 503 body=%s", rec.Code, rec.Body.String())
	}
	if n := stub.calls("/works/OL59863W.json"); n != 0 {
		t.Errorf("doc calls = %d, want 0 (nothing to fetch from)", n)
	}
}
