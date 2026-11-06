package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/brandenk514/mediarr/internal/auth"
	"github.com/brandenk514/mediarr/internal/health"
	"github.com/brandenk514/mediarr/internal/providers"
	bookssvc "github.com/brandenk514/mediarr/internal/services/books"
	"github.com/brandenk514/mediarr/internal/settings"
)

// --- MT5 API fakes ---

// mt5OLStub is a stub Open Library whose combined-lookup legs return the
// configured docs (per leg parameter), counting every hit per path. The
// MT5 gap-fill tests set both legs empty; the OL-wins test sets the title
// leg to a real work.
type mt5OLStub struct {
	mu       sync.Mutex
	srv      *httptest.Server
	byPath   map[string]int
	titleDoc map[string]any
	authDoc  map[string]any
}

func newMT5OLStub(t *testing.T, titleDocs, authorDocs []map[string]any) *mt5OLStub {
	t.Helper()
	s := &mt5OLStub{byPath: map[string]int{}}
	if titleDocs != nil {
		s.titleDoc = map[string]any{"numFound": len(titleDocs), "docs": titleDocs}
	} else {
		s.titleDoc = map[string]any{"numFound": 0, "docs": []any{}}
	}
	if authorDocs != nil {
		s.authDoc = map[string]any{"numFound": len(authorDocs), "docs": authorDocs}
	} else {
		s.authDoc = map[string]any{"numFound": 0, "docs": []any{}}
	}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.byPath[r.URL.Path]++
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/search.json" && r.URL.Query().Get("title") != "":
			_ = json.NewEncoder(w).Encode(s.titleDoc)
		case r.URL.Path == "/search.json" && r.URL.Query().Get("author") != "":
			_ = json.NewEncoder(w).Encode(s.authDoc)
		case r.URL.Path == "/search.json":
			// The id-lookup legs (isbn:/asin:) hit /search.json with their
			// own parameters: empty result (the OL source of truth).
			_ = json.NewEncoder(w).Encode(map[string]any{"numFound": 0, "docs": []any{}})
		case strings.HasPrefix(r.URL.Path, "/works/") && strings.HasSuffix(r.URL.Path, ".json"):
			// A work document (id-lookup / key queries): empty editions.
			key := strings.TrimSuffix(r.URL.Path[len("/works/"):], ".json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"key": "/works/" + key, "title": "The " + key,
				"authors": []map[string]any{{"name": "MT5 Author"}}, "editions": []any{},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *mt5OLStub) URL() string { return s.srv.URL }

func (s *mt5OLStub) calls(path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.byPath[path]
}

// mt5GBStub is a stub Google Books API serving a real-shaped volumes
// response (two volumes: a BOOK with an ISBN_13 and an AUDIOBOOK — the
// plan's audiobook note: the mapping must survive it), counting calls and
// recording the last q.
type mt5GBStub struct {
	mu    sync.Mutex
	srv   *httptest.Server
	calls int
	lastQ string
}

func newMT5GBStub(t *testing.T) *mt5GBStub {
	t.Helper()
	s := &mt5GBStub{}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.calls++
		s.lastQ = r.URL.Query().Get("q")
		s.mu.Unlock()
		if r.URL.Path != "/volumes" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.URL.Query().Get("key") != "mt5-test-key" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"kind": "books#volumes", "totalItems": 2,
			"items": []map[string]any{
				{
					"id": "volEng",
					"volumeInfo": map[string]any{
						"title": "The Dispossessed", "subtitle": "An Ecotone Novel",
						"authors":   []string{"Ursula K. Le Guin"},
						"publisher": "Ace", "publishedDate": "1974-06-04",
						"language": "eng", "printType": "BOOK",
						"industryIdentifiers": []map[string]any{
							{"type": "ISBN_13", "identifier": "9780441478792"},
						},
					},
					"saleInfo": map[string]any{"country": "US", "availability": 1},
				},
				{
					"id": "volFra",
					"volumeInfo": map[string]any{
						"title": "La Possedée", "authors": []string{"Ursula K. Le Guin"},
						"publisher": "Gallimard", "publishedDate": "1977",
						"language": "fra", "printType": "BOOK",
					},
					"saleInfo": map[string]any{"country": "FR", "availability": 1},
				},
			},
		})
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *mt5GBStub) URL() string { return s.srv.URL }

func (s *mt5GBStub) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *mt5GBStub) LastQ() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastQ
}

// newBooksMT5Server builds a FRESH authenticated server wired the way
// main.go does (real OL adapter at a stub + in-memory repo + MT3 store +
// the MT4 profile provider + the MT5 Google Books leg at its own stub).
// gbKey "" = the GB leg is NOT configured (empty key = disabled).
func newBooksMT5Server(t *testing.T, olStub *mt5OLStub, gbStub *mt5GBStub, gbKey string, profilesDoc []byte) *booksB2Client {
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
	ol.BaseURL = olStub.URL()
	fake := newFakeSettingsStore()
	if profilesDoc != nil {
		if err := fake.PutSectionDocument(context.Background(), settings.SectionProfiles, profilesDoc); err != nil {
			t.Fatal(err)
		}
	}
	gb := providers.NewGoogleBooks(gbKey)
	if gbStub != nil {
		gb.BaseURL = gbStub.URL()
	}
	svc := bookssvc.New(bookssvc.Deps{
		Repo:             newKindsBooksRepo(),
		DefaultProfile:   "Best",
		Searcher:         bookssvc.NewOpenLibrarySearcher(ol),
		MetadataStore:    newMT3MemStore(),
		MetadataProfiles: bookssvc.NewSettingsProfileProvider(fake),
		// MT5: the optional Google Books secondary leg.
		GoogleBooks: bookssvc.NewGoogleBooksSearcher(gb),
	})
	s.SetBooks(&BooksDeps{Svc: svc})
	return &booksB2Client{h: s.Handler(), token: lr.Token}
}

// --- MT5 lookup endpoint tests ---

// TestBooksMT5_GBGapFill is the MT5 contract through the endpoint: a
// plain-text query whose OL leg is empty falls back to Google Books, and
// the WIRE SHAPE IS UNCHANGED — GB hits are kind=book with name, author
// and an EMPTY work_key (the UI's manual-add fallback handles them), and
// the response body carries NO source field (the source marker is
// server-side only).
func TestBooksMT5_GBGapFill(t *testing.T) {
	olStub := newMT5OLStub(t, nil, nil) // both legs empty
	gbStub := newMT5GBStub(t)
	c := newBooksMT5Server(t, olStub, gbStub, "mt5-test-key", nil)

	rec := c.get(t, "/api/v1/search/books/lookup?query=the+dispossessed")
	if rec.Code != http.StatusOK {
		t.Fatalf("lookup status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	var got []bookLookupResult
	if err := json.NewDecoder(strings.NewReader(body)).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("results = %d, want 2 GB hits: %+v", len(got), got)
	}
	for _, r := range got {
		if r.Kind != "book" {
			t.Errorf("kind = %q, want book (wire shape unchanged)", r.Kind)
		}
		if r.WorkKey != "" {
			t.Errorf("work_key = %q, want empty for a GB hit (manual-add fallback)", r.WorkKey)
		}
		if r.Added || r.LibraryID != 0 || r.AuthorID != 0 {
			t.Errorf("r = %+v, want a fresh (not-added) hit", r)
		}
	}
	if got[0].Name != "The Dispossessed" || got[0].Author != "Ursula K. Le Guin" || got[0].Year != 1974 {
		t.Errorf("got[0] = %+v, want the BOOK volume mapped", got[0])
	}
	if got[1].Name != "La Possedée" || got[1].Author != "Ursula K. Le Guin" {
		t.Errorf("got[1] = %+v, want the second volume", got[1])
	}
	// The wire body must NOT gain a source field.
	if strings.Contains(body, `"source"`) {
		t.Errorf("wire body contains a source field: %s", body)
	}
	// The GB stub saw the plain query exactly once.
	if gbStub.count() != 1 || gbStub.LastQ() != "the dispossessed" {
		t.Errorf("GB calls = %d (last %q), want 1 with the plain query", gbStub.count(), gbStub.LastQ())
	}
	// The OL legs were consulted (the GB leg only runs after OL).
	if olStub.calls("/search.json") != 2 {
		t.Errorf("OL search calls = %d, want 2 (author + work legs)", olStub.calls("/search.json"))
	}
}

// TestBooksMT5_OLWithHitsNeverCallsGB: OL-with-hits + GB configured => OL
// only (GB never supplements — it fills the empty gap, nothing else).
func TestBooksMT5_OLWithHitsNeverCallsGB(t *testing.T) {
	olStub := newMT5OLStub(t, []map[string]any{{
		"key": "/works/OL1W", "title": "The Dispossessed",
		"author_name": []string{"Ursula K. Le Guin"}, "first_publish_year": 1974,
	}}, nil)
	gbStub := newMT5GBStub(t)
	c := newBooksMT5Server(t, olStub, gbStub, "mt5-test-key", nil)

	rec := c.get(t, "/api/v1/search/books/lookup?query=the+dispossessed")
	if rec.Code != http.StatusOK {
		t.Fatalf("lookup status = %d body=%s", rec.Code, rec.Body.String())
	}
	var got []bookLookupResult
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Kind != "book" || got[0].WorkKey != "OL1W" {
		t.Fatalf("results = %+v, want the single OL hit (work key OL1W)", got)
	}
	if gbStub.count() != 0 {
		t.Errorf("GB calls = %d, want 0 (OL-with-hits => OL only)", gbStub.count())
	}
}

// TestBooksMT5_IDLookupNeverCallsGB: an id-lookup query (isbn:) never
// runs the GB leg — ids are unambiguous and OL is their source of truth.
func TestBooksMT5_IDLookupNeverCallsGB(t *testing.T) {
	olStub := newMT5OLStub(t, nil, nil)
	gbStub := newMT5GBStub(t)
	c := newBooksMT5Server(t, olStub, gbStub, "mt5-test-key", nil)

	rec := c.get(t, "/api/v1/search/books/lookup?query=isbn%3A9780441478792")
	if rec.Code != http.StatusOK {
		t.Fatalf("lookup status = %d body=%s", rec.Code, rec.Body.String())
	}
	var got []bookLookupResult
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("results = %+v, want none (empty OL isbn result, no GB)", got)
	}
	if gbStub.count() != 0 {
		t.Errorf("GB calls = %d, want 0 (id-lookup never runs the GB leg)", gbStub.count())
	}
}

// TestBooksMT5_NotConfiguredNoLeg: an empty Google Books key = the leg is
// disabled — the lookup answers with the OL-only result, no error, and
// the (absent) GB endpoint is never touched.
func TestBooksMT5_NotConfiguredNoLeg(t *testing.T) {
	olStub := newMT5OLStub(t, nil, nil)
	gbStub := newMT5GBStub(t)
	c := newBooksMT5Server(t, olStub, gbStub, "", nil)

	rec := c.get(t, "/api/v1/search/books/lookup?query=the+dispossessed")
	if rec.Code != http.StatusOK {
		t.Fatalf("lookup status = %d body=%s (want 200, not a 503)", rec.Code, rec.Body.String())
	}
	var got []bookLookupResult
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("results = %+v, want none (leg disabled)", got)
	}
	if gbStub.count() != 0 {
		t.Errorf("GB calls = %d, want 0 (not configured)", gbStub.count())
	}
}

// TestBooksMT5_ProfileFiltersApplyToGBHits: the MT4 profile filters apply
// to the COMBINED output — a GB hit with a non-allowlisted language is
// dropped through the endpoint (the MT4+MT5 interaction).
func TestBooksMT5_ProfileFiltersApplyToGBHits(t *testing.T) {
	olStub := newMT5OLStub(t, nil, nil)
	gbStub := newMT5GBStub(t)
	c := newBooksMT5Server(t, olStub, gbStub, "mt5-test-key", mt4ProfilesDoc(t, settings.BookMetadataProfile{
		ID: 2, Name: "Eng only", AllowedLanguages: []string{"eng"},
	}))

	rec := c.get(t, "/api/v1/search/books/lookup?query=the+dispossessed&profile=Eng+only")
	if rec.Code != http.StatusOK {
		t.Fatalf("lookup status = %d body=%s", rec.Code, rec.Body.String())
	}
	var got []bookLookupResult
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("results = %d, want 1 (the fra GB hit dropped): %+v", len(got), got)
	}
	if got[0].Name != "The Dispossessed" {
		t.Errorf("got[0] = %+v, want the eng GB hit", got[0])
	}
	if got[0].WorkKey != "" {
		t.Errorf("work_key = %q, want empty for a GB hit", got[0].WorkKey)
	}
}

// TestBooksMT5_GBFailureIsBestEffort: a failing GB leg never breaks the
// lookup — the OL-only (empty) result is returned, not a 502.
func TestBooksMT5_GBFailureIsBestEffort(t *testing.T) {
	olStub := newMT5OLStub(t, nil, nil)
	// A GB stub that 500s.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	c := newBooksMT5Server(t, olStub, &mt5GBStub{srv: srv}, "mt5-test-key", nil)

	rec := c.get(t, "/api/v1/search/books/lookup?query=the+dispossessed")
	if rec.Code != http.StatusOK {
		t.Fatalf("lookup status = %d body=%s, want 200 (best-effort leg)", rec.Code, rec.Body.String())
	}
	var got []bookLookupResult
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("results = %+v, want none", got)
	}
}
