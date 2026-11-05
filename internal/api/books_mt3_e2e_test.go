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

// --- MT3 API fakes ---

// mt3MemStore is an in-memory booksdom.MetadataStore for the MT3 API tests.
type mt3MemStore struct {
	mu      sync.Mutex
	rows    map[string]booksdom.WorkMetadata
	upserts int
	getErr  error
}

func newMT3MemStore() *mt3MemStore { return &mt3MemStore{rows: map[string]booksdom.WorkMetadata{}} }

func (s *mt3MemStore) GetWork(ctx context.Context, key string) (booksdom.WorkMetadata, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.getErr != nil {
		return booksdom.WorkMetadata{}, false, s.getErr
	}
	for _, w := range s.rows {
		if w.WorkKey == key {
			return w, true, nil
		}
	}
	return booksdom.WorkMetadata{}, false, nil
}

func (s *mt3MemStore) UpsertWork(ctx context.Context, w booksdom.WorkMetadata) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.upserts++
	s.rows[w.Source+"\x00"+w.SourceID] = w
	return nil
}

func (s *mt3MemStore) upsertCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.upserts
}

// mt3OLStub is a stub Open Library that answers the `q=key:` work-key
// lookup AND the work document (MT2: the refresh also fetches the work's
// details/editions from the work endpoint), counting every hit (the "did
// we call Open Library?" meter). (MT1 moved the id lookups off the legacy
// `id=` parameter — which the keyless production API silently ignores —
// onto the declared `q` solr query, so the stub parses `q` the same way.)
func mt3OLStub(t *testing.T, calls *int) string {
	t.Helper()
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*calls++
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/works/OL59863W.json":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"key": "/works/OL59863W", "title": "The Dispossessed",
				"author_name": []string{"Ursula K. Le Guin"}, "first_publish_year": 1974,
				"cover":         map[string]any{"small": "https://covers.openlibrary.org/b/id/12345-S.jpg"},
				"edition_count": 1, "editions": []map[string]any{
					{"key": "/works/OL59863W/OL25828717M", "isbn_13": []string{"9780140449335"}, "languages": []string{"eng"}},
				},
			})
		case "/search.json":
			q := r.URL.Query().Get("q")
			if q == "key:OL59863W" {
				docs := []map[string]any{
					{"key": "/works/OL59863W", "title": "The Dispossessed", "author_name": []string{"Ursula K. Le Guin"}, "first_publish_year": 1974, "cover_i": 12345},
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"numFound": 1, "docs": docs})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"numFound": 0, "docs": []any{}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(stub.Close)
	return stub.URL
}

// newBooksMT3Server builds a FRESH authenticated server wired the way
// main.go does (real OL adapter at a stub + in-memory repo + MT3 store).
// A nil store or searcher yields the not-configured refresh/lookup paths.
func newBooksMT3Server(t *testing.T, store booksdom.MetadataStore, olCalls *int) *booksB2Client {
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

	ol := providers.NewOpenLibrary()
	ol.BaseURL = mt3OLStub(t, olCalls)
	svc := bookssvc.New(bookssvc.Deps{
		Repo:           newKindsBooksRepo(),
		DefaultProfile: "Best",
		Searcher:       bookssvc.NewOpenLibrarySearcher(ol),
		MetadataStore:  store,
	})
	s.SetBooks(&BooksDeps{Svc: svc})
	return &booksB2Client{h: s.Handler(), token: lr.Token}
}

// --- MT3 refresh command endpoint tests ---

// TestBooksMT3_RefreshEndpoint is the refresh command end-to-end: a forced
// re-fetch (TTL bypass) that returns the refreshed provenance row and
// upserts it into the store.
func TestBooksMT3_RefreshEndpoint(t *testing.T) {
	store := newMT3MemStore()
	calls := 0
	c := newBooksMT3Server(t, store, &calls)

	rec := c.post(t, "/api/v1/search/books/refresh?key=/works/OL59863W", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh status = %d body=%s", rec.Code, rec.Body.String())
	}
	var wm bookWorkMetadata
	if err := json.NewDecoder(rec.Body).Decode(&wm); err != nil {
		t.Fatal(err)
	}
	if wm.WorkKey != "OL59863W" || wm.Source != "openlibrary" || wm.SourceID != "OL59863W" {
		t.Errorf("provenance = %+v, want the OL key under source openlibrary", wm)
	}
	if wm.Title != "The Dispossessed" || wm.Author != "Ursula K. Le Guin" || wm.Year != 1974 {
		t.Errorf("work = %+v, want the re-fetched work", wm)
	}
	if wm.CoverURL != "https://covers.openlibrary.org/b/id/12345-M.jpg" {
		t.Errorf("cover_url = %q, want the covers.openlibrary.org URL", wm.CoverURL)
	}
	if wm.FetchedAt.IsZero() {
		t.Error("fetched_at is zero, want the re-anchored timestamp")
	}
	if calls != 2 {
		t.Errorf("OL calls = %d, want 2 (forced re-fetch + the MT2 work-details/editions fetch)", calls)
	}
	if store.upsertCount() != 1 {
		t.Errorf("store upserts = %d, want 1 (refresh upserts the row)", store.upsertCount())
	}

	// The upserted row is what a fresh lookup serves (the write-through).
	rec = c.get(t, "/api/v1/search/books/lookup?query=OL59863W")
	if rec.Code != http.StatusOK {
		t.Fatalf("lookup status = %d body=%s", rec.Code, rec.Body.String())
	}
	var hits []bookLookupResult
	if err := json.NewDecoder(rec.Body).Decode(&hits); err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Kind != "book" || hits[0].Name != "The Dispossessed" {
		t.Errorf("lookup = %+v, want the cached book hit", hits)
	}
	if calls != 2 {
		t.Errorf("OL calls after cached lookup = %d, want 2 (served from the store)", calls)
	}
}

// TestBooksMT3_RefreshErrors covers the refresh command's status map:
// missing key (400), OL miss (404), and the not-configured paths (503).
func TestBooksMT3_RefreshErrors(t *testing.T) {
	store := newMT3MemStore()
	calls := 0
	c := newBooksMT3Server(t, store, &calls)

	rec := c.post(t, "/api/v1/search/books/refresh?key=", nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("empty key: status = %d, want 400", rec.Code)
	}
	rec = c.post(t, "/api/v1/search/books/refresh?key=OL404W", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("OL miss: status = %d, want 404 (body=%s)", rec.Code, rec.Body.String())
	}

	// No store -> 503 (a refresh is a store operation by definition).
	calls = 0
	c2 := newBooksMT3Server(t, nil, &calls)
	rec = c2.post(t, "/api/v1/search/books/refresh?key=OL59863W", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("no store: status = %d, want 503", rec.Code)
	}
}

// TestBooksMT3_LookupCachedKey: a fresh store row is served by the lookup
// WITHOUT an Open Library call (the MT3 cache hit through the HTTP layer).
func TestBooksMT3_LookupCachedKey(t *testing.T) {
	store := newMT3MemStore()
	store.rows["openlibrary\x00OL59863W"] = booksdom.WorkMetadata{
		WorkKey: "OL59863W", Source: "openlibrary", SourceID: "OL59863W",
		Title: "The Dispossessed", Author: "Ursula K. Le Guin", Year: 1974,
		FetchedAt: time.Now(),
	}
	calls := 0
	c := newBooksMT3Server(t, store, &calls)

	rec := c.get(t, "/api/v1/search/books/lookup?query=OL59863W")
	if rec.Code != http.StatusOK {
		t.Fatalf("lookup status = %d body=%s", rec.Code, rec.Body.String())
	}
	var hits []bookLookupResult
	if err := json.NewDecoder(rec.Body).Decode(&hits); err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Kind != "book" || hits[0].Name != "The Dispossessed" || hits[0].Year != 1974 {
		t.Errorf("lookup = %+v, want the store-served book hit", hits)
	}
	if hits[0].WorkKey != "OL59863W" {
		t.Errorf("work_key = %q, want \"OL59863W\" (the store-served hit carries the key)", hits[0].WorkKey)
	}
	if calls != 0 {
		t.Errorf("OL calls = %d, want 0 (fresh row served from the store)", calls)
	}
}

// TestBooksMT3_LookupKeyWriteThrough: a store-miss key lookup re-fetches
// from Open Library once and upserts the row; a repeat lookup is cached.
func TestBooksMT3_LookupKeyWriteThrough(t *testing.T) {
	store := newMT3MemStore()
	calls := 0
	c := newBooksMT3Server(t, store, &calls)

	rec := c.get(t, "/api/v1/search/books/lookup?query=OL59863W")
	if rec.Code != http.StatusOK {
		t.Fatalf("lookup status = %d body=%s", rec.Code, rec.Body.String())
	}
	var hits []bookLookupResult
	if err := json.NewDecoder(rec.Body).Decode(&hits); err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Name != "The Dispossessed" {
		t.Errorf("lookup = %+v, want the re-fetched book hit", hits)
	}
	if calls != 1 {
		t.Errorf("OL calls = %d, want 1 (store miss re-fetches)", calls)
	}
	if store.upsertCount() != 1 {
		t.Errorf("store upserts = %d, want 1 (write-through)", store.upsertCount())
	}

	// Second lookup: cached, no further Open Library call.
	rec = c.get(t, "/api/v1/search/books/lookup?query=OL59863W")
	if rec.Code != http.StatusOK {
		t.Fatalf("lookup(after) status = %d body=%s", rec.Code, rec.Body.String())
	}
	if calls != 1 {
		t.Errorf("OL calls after repeat lookup = %d, want 1 (cached)", calls)
	}
}
