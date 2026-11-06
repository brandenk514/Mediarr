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
	booksdom "github.com/brandenk514/mediarr/internal/domains/books"
	"github.com/brandenk514/mediarr/internal/health"
	"github.com/brandenk514/mediarr/internal/providers"
	bookssvc "github.com/brandenk514/mediarr/internal/services/books"
	"github.com/brandenk514/mediarr/internal/settings"
)

// --- MT4 API fakes ---

// mt4OLStub is a stub Open Library that answers BOTH the combined lookup's
// legs (/search.json with a `title` or `author` parameter) and the work
// documents (/works/OL…W.json — the tier-2 filter data), counting every hit
// per path. The tests never hit the real network. The three works are the
// tier-1 fixtures: OL1W (eng, 42 indexed editions), OL2W (fra, 7), OL3W
// (eng, 1 — the popularity-proxy droplet).
type mt4OLStub struct {
	mu      sync.Mutex
	srv     *httptest.Server
	byPath  map[string]int
	ol1wDoc any // the /works/OL1W.json document (the tests override it)
}

// mt4WorkDoc is the work document in the real /works/{key}.json shape.
func mt4WorkDoc(key string, editions any) map[string]any {
	return map[string]any{
		"key":      "/works/" + key,
		"title":    "The " + key,
		"authors":  []map[string]any{{"name": "MT4 Author"}},
		"covers":   []int{12345},
		"editions": editions,
	}
}

func newMT4OLStub(t *testing.T) *mt4OLStub {
	t.Helper()
	s := &mt4OLStub{byPath: map[string]int{}}
	s.ol1wDoc = mt4WorkDoc("OL1W", []map[string]any{{
		"key": "/works/OL1W/OL1M", "title": "The Dispossessed",
		"isbn_13": []string{"9780140449335"}, "languages": []string{"eng"},
		"publish_date": "1974",
	}})
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.byPath[r.URL.Path]++
		s.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/search.json":
			switch {
			case r.URL.Query().Get("title") != "":
				// The work leg: three works with the tier-1 facts.
				docs := []map[string]any{
					{
						"key": "/works/OL1W", "title": "The Dispossessed",
						"author_name":        []string{"Ursula K. Le Guin"},
						"first_publish_year": 1974, "cover_i": 12345,
						"language": []string{"eng"}, "edition_count": 42,
					},
					{
						"key": "/works/OL2W", "title": "L'Étranger",
						"author_name":        []string{"Camus"},
						"first_publish_year": 1942,
						"language":           []string{"fra"}, "edition_count": 7,
					},
					{
						"key": "/works/OL3W", "title": "A Rare Book",
						"author_name":        []string{"Nobody"},
						"first_publish_year": 1900,
						"language":           []string{"eng"}, "edition_count": 1,
					},
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"numFound": 3, "docs": docs})
			case r.URL.Query().Get("author") != "":
				// The author leg: one candidate author.
				docs := []map[string]any{{
					"key": "/works/OL1W", "title": "The Dispossessed",
					"author_name": []string{"Ursula K. Le Guin"},
				}}
				_ = json.NewEncoder(w).Encode(map[string]any{"numFound": 1, "docs": docs})
			default:
				_ = json.NewEncoder(w).Encode(map[string]any{"numFound": 0, "docs": []any{}})
			}
		case "/works/OL1W.json":
			// OL1W is the IDENTIFIED work (the tier-2 survivor); the
			// editions test swaps in the four-edition document.
			_ = json.NewEncoder(w).Encode(s.ol1wDoc)
		case "/works/OL2W.json", "/works/OL3W.json":
			// The UNIDENTIFIED works (no ISBN/ASIN on any edition).
			key := strings.TrimSuffix(r.URL.Path[len("/works/"):], ".json")
			_ = json.NewEncoder(w).Encode(mt4WorkDoc(key, []map[string]any{{
				"key": "/works/" + key + "/OL2M", "title": "Unidentified",
				"languages": []string{"eng"}, "publish_date": "1942",
			}}))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *mt4OLStub) URL() string { return s.srv.URL }

func (s *mt4OLStub) calls(path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.byPath[path]
}

// mt4ProfilesDoc builds the profiles section document with the seeded
// "Default" book metadata profile plus the test profiles — the same shape
// the settings section endpoint serves.
func mt4ProfilesDoc(t *testing.T, extra ...settings.BookMetadataProfile) []byte {
	t.Helper()
	doc := settings.DefaultProfiles()
	doc.BookMetadataProfiles = append(doc.BookMetadataProfiles, extra...)
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// newBooksMT4Server builds a FRESH authenticated server wired the way
// main.go does (real OL adapter at a stub + in-memory repo + MT3 store +
// the MT4 profile provider over the in-memory settings store).
// profilesDoc nil = an unsaved profiles section (the seeded defaults).
func newBooksMT4Server(t *testing.T, store booksdom.MetadataStore, stub *mt4OLStub, withOL bool, profilesDoc []byte) *booksB2Client {
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
	fake := newFakeSettingsStore()
	if profilesDoc != nil {
		if err := fake.PutSectionDocument(context.Background(), settings.SectionProfiles, profilesDoc); err != nil {
			t.Fatal(err)
		}
	}
	svc := bookssvc.New(bookssvc.Deps{
		Repo:           newKindsBooksRepo(),
		DefaultProfile: "Best",
		Searcher:       searcher,
		MetadataStore:  store,
		// MT4: the named profiles from the (fake) settings store.
		MetadataProfiles: bookssvc.NewSettingsProfileProvider(fake),
	})
	s.SetBooks(&BooksDeps{Svc: svc})
	return &booksB2Client{h: s.Handler(), token: lr.Token}
}

// --- MT4 lookup endpoint tests ---

// TestBooksMT4_LookupNoProfileUnchanged pins the pre-MT4 baseline: without
// a profile parameter the lookup returns every hit, unfiltered (byte-
// identical behavior).
func TestBooksMT4_LookupNoProfileUnchanged(t *testing.T) {
	store := newMT3MemStore()
	stub := newMT4OLStub(t)
	c := newBooksMT4Server(t, store, stub, true, nil)

	rec := c.get(t, "/api/v1/search/books/lookup?query=dispossessed")
	if rec.Code != http.StatusOK {
		t.Fatalf("lookup status = %d body=%s", rec.Code, rec.Body.String())
	}
	var got []bookLookupResult
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("results = %d, want 4 (1 author + 3 works): %+v", len(got), got)
	}
	if got[0].Kind != "author" || got[0].Name != "Ursula K. Le Guin" {
		t.Errorf("results[0] = %+v, want the author hit first", got[0])
	}
	books := map[string]bookLookupResult{}
	for i, r := range got[1:] {
		if r.Kind != "book" {
			t.Fatalf("results[%d] = %+v, want kind=book", i, r)
		}
		books[r.WorkKey] = r
	}
	if _, ok := books["OL1W"]; !ok {
		t.Error("OL1W missing from the unfiltered results")
	}
	if _, ok := books["OL2W"]; !ok {
		t.Error("OL2W missing from the unfiltered results")
	}
	if _, ok := books["OL3W"]; !ok {
		t.Error("OL3W missing from the unfiltered results")
	}
}

// TestBooksMT4_LookupProfileLanguage is the tier-1 allowed-languages filter
// through the endpoint: the French work is dropped, the English works stay,
// and the author hit is NEVER filtered (Readarr profiles filter books, not
// authors).
func TestBooksMT4_LookupProfileLanguage(t *testing.T) {
	store := newMT3MemStore()
	stub := newMT4OLStub(t)
	c := newBooksMT4Server(t, store, stub, true, mt4ProfilesDoc(t, settings.BookMetadataProfile{
		ID: 2, Name: "Eng only", AllowedLanguages: []string{"eng"},
	}))

	rec := c.get(t, "/api/v1/search/books/lookup?query=dispossessed&profile=Eng+only")
	if rec.Code != http.StatusOK {
		t.Fatalf("lookup status = %d body=%s", rec.Code, rec.Body.String())
	}
	var got []bookLookupResult
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("results = %d, want 3 (1 author + 2 English books): %+v", len(got), got)
	}
	if got[0].Kind != "author" || got[0].Name != "Ursula K. Le Guin" {
		t.Errorf("results[0] = %+v, want the author hit (never filtered)", got[0])
	}
	books := map[string]bool{}
	for _, r := range got[1:] {
		books[r.WorkKey] = true
	}
	if !books["OL1W"] || !books["OL3W"] {
		t.Errorf("books = %v, want OL1W + OL3W (the English works)", books)
	}
	if books["OL2W"] {
		t.Error("OL2W (fra) survived the [eng] allowlist")
	}
}

// TestBooksMT4_LookupProfilePopularity is the tier-1 minimum-popularity
// filter (the indexed-edition count proxy): the 1-edition work is dropped
// at minimum 2; the 7- and 42-edition works stay. No work-document fetch
// happens (the filter reads the search document).
func TestBooksMT4_LookupProfilePopularity(t *testing.T) {
	store := newMT3MemStore()
	stub := newMT4OLStub(t)
	c := newBooksMT4Server(t, store, stub, true, mt4ProfilesDoc(t, settings.BookMetadataProfile{
		ID: 2, Name: "Popular", MinimumPopularity: 2,
	}))

	rec := c.get(t, "/api/v1/search/books/lookup?query=dispossessed&profile=Popular")
	if rec.Code != http.StatusOK {
		t.Fatalf("lookup status = %d body=%s", rec.Code, rec.Body.String())
	}
	var got []bookLookupResult
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	books := map[string]bool{}
	for _, r := range got {
		if r.Kind == "book" {
			books[r.WorkKey] = true
		}
	}
	if !books["OL1W"] || !books["OL2W"] {
		t.Errorf("books = %v, want OL1W (42) + OL2W (7)", books)
	}
	if books["OL3W"] {
		t.Error("OL3W (1 indexed edition) survived a minimum of 2")
	}
	if n := stub.calls("/works/OL1W.json") + stub.calls("/works/OL2W.json") + stub.calls("/works/OL3W.json"); n != 0 {
		t.Errorf("work-doc calls = %d, want 0 (tier-1 filter reads the search doc only)", n)
	}
}

// TestBooksMT4_LookupProfileNoISBN is the tier-2 skip-no-ISBN/ASIN filter
// through the endpoint: each survivor's work document is fetched (3 calls),
// and only the identified work (OL1W) survives.
func TestBooksMT4_LookupProfileNoISBN(t *testing.T) {
	store := newMT3MemStore()
	stub := newMT4OLStub(t)
	c := newBooksMT4Server(t, store, stub, true, mt4ProfilesDoc(t, settings.BookMetadataProfile{
		ID: 2, Name: "Strict", SkipNoISBNASIN: true,
	}))

	rec := c.get(t, "/api/v1/search/books/lookup?query=dispossessed&profile=Strict")
	if rec.Code != http.StatusOK {
		t.Fatalf("lookup status = %d body=%s", rec.Code, rec.Body.String())
	}
	var got []bookLookupResult
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, r := range got {
		if r.Kind == "book" {
			names = append(names, r.WorkKey)
		}
	}
	if len(names) != 1 || names[0] != "OL1W" {
		t.Fatalf("book hits = %v, want only OL1W (the identified work)", names)
	}
	if n := stub.calls("/works/OL1W.json") + stub.calls("/works/OL2W.json") + stub.calls("/works/OL3W.json"); n != 3 {
		t.Errorf("work-doc calls = %d, want 3 (one per tier-1 survivor)", n)
	}
}

// TestBooksMT4_LookupDefaultProfile: the seeded "Default" profile (no
// filters) resolves and is behaviorally identical to no profile at all.
func TestBooksMT4_LookupDefaultProfile(t *testing.T) {
	store := newMT3MemStore()
	stub := newMT4OLStub(t)
	c := newBooksMT4Server(t, store, stub, true, nil) // unsaved section: the seeded defaults

	rec := c.get(t, "/api/v1/search/books/lookup?query=dispossessed&profile=Default")
	if rec.Code != http.StatusOK {
		t.Fatalf("lookup status = %d body=%s", rec.Code, rec.Body.String())
	}
	var got []bookLookupResult
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("results = %d, want 4 (Default has no filters): %+v", len(got), got)
	}
}

// TestBooksMT4_LookupUnknownProfile400: an unknown profile name is a 400
// with a clear message (both endpoints).
func TestBooksMT4_LookupUnknownProfile400(t *testing.T) {
	store := newMT3MemStore()
	stub := newMT4OLStub(t)
	c := newBooksMT4Server(t, store, stub, true, mt4ProfilesDoc(t, settings.BookMetadataProfile{
		ID: 2, Name: "Eng only", AllowedLanguages: []string{"eng"},
	}))

	rec := c.get(t, "/api/v1/search/books/lookup?query=dispossessed&profile=NoSuchProfile")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("lookup status = %d, want 400 body=%s", rec.Code, rec.Body.String())
	}
	rec = c.get(t, "/api/v1/search/books/works/OL1W/editions?profile=NoSuchProfile")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("editions status = %d, want 400 body=%s", rec.Code, rec.Body.String())
	}
}

// --- MT4 editions endpoint tests ---

// mt4EditionsDoc builds the four-edition work document: E1 passes every
// per-edition filter; E2/E3/E4 each fail one (no ISBN/ASIN, wrong language,
// missing release date).
func mt4EditionsDoc() any {
	return mt4WorkDoc("OL1W", []map[string]any{
		{
			"key": "/works/OL1W/E1", "title": "Full",
			"isbn_13": []string{"9780140449335"}, "languages": []string{"eng"},
			"publish_date": "1974",
		},
		{
			"key": "/works/OL1W/E2", "title": "No IDs",
			"languages": []string{"eng"}, "publish_date": "2020",
		},
		{
			"key": "/works/OL1W/E3", "title": "French",
			"isbn_13": []string{"9780140449336"}, "languages": []string{"fra"},
			"publish_date": "1980",
		},
		{
			"key": "/works/OL1W/E4", "title": "No Date",
			"isbn_13": []string{"9780140449337"}, "languages": []string{"eng"},
		},
	})
}

// TestBooksMT4_EditionsProfile is the per-edition profile subset through
// the endpoint: E2 (no ISBN/ASIN), E3 (wrong language) and E4 (missing
// release date) are dropped; E1 remains. Without the profile all four come
// back (the wire shape is unchanged — the filters only DROP editions).
func TestBooksMT4_EditionsProfile(t *testing.T) {
	store := newMT3MemStore()
	stub := newMT4OLStub(t)
	stub.ol1wDoc = mt4EditionsDoc()
	c := newBooksMT4Server(t, store, stub, true, mt4ProfilesDoc(t, settings.BookMetadataProfile{
		ID: 2, Name: "Strict",
		SkipNoISBNASIN:         true,
		SkipMissingReleaseDate: true,
		AllowedLanguages:       []string{"eng"},
	}))

	// No profile: all four editions (pre-MT4 behavior).
	rec := c.get(t, "/api/v1/search/books/works/OL1W/editions")
	if rec.Code != http.StatusOK {
		t.Fatalf("editions status = %d body=%s", rec.Code, rec.Body.String())
	}
	var base bookWorkDetails
	if err := json.NewDecoder(rec.Body).Decode(&base); err != nil {
		t.Fatal(err)
	}
	if len(base.Editions) != 4 {
		t.Fatalf("editions = %d, want 4 (no profile = unfiltered): %+v", len(base.Editions), base.Editions)
	}

	// With the profile: only E1 survives the per-edition filters. (The row
	// was cached by the unfiltered call — the filtered view reuses it, 0
	// further OL calls.)
	rec = c.get(t, "/api/v1/search/books/works/OL1W/editions?profile=Strict")
	if rec.Code != http.StatusOK {
		t.Fatalf("editions(profile) status = %d body=%s", rec.Code, rec.Body.String())
	}
	var got bookWorkDetails
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got.Editions) != 1 || got.Editions[0].Key != "E1" || got.Editions[0].ISBN != "9780140449335" {
		t.Fatalf("filtered editions = %+v, want only E1 (language/year/ISBN per-edition filters)", got.Editions)
	}
	// The work-level fields are untouched (the filters drop editions, not
	// work data).
	if got.WorkKey != "OL1W" || got.Title != "The OL1W" {
		t.Errorf("work = %+v, want the unfiltered work fields", got)
	}
	// The cached row keeps the UNFILTERED set (the cache is the source of
	// truth; the filter is a read-time view).
	row, ok := store.rows["openlibrary\x00OL1W"]
	if !ok || len(row.EditionsJSON) == 0 {
		t.Fatal("no cached row (the unfiltered call write-throughed)")
	}
	var cached []bookWorkEdition
	if err := json.Unmarshal(row.EditionsJSON, &cached); err != nil {
		t.Fatalf("cached editions unmarshal: %v", err)
	}
	if len(cached) != 4 {
		t.Errorf("cached editions = %d, want 4 (the filter never rewrites the cache)", len(cached))
	}
}

// TestBooksMT4_GBFreeSanity: everything works with no Google Books
// configuration at all (nothing is wired — the OL-only lookup and editions
// paths are the whole surface). The test exists so MT5's wiring has a
// pinned "GB absent" baseline on this branch.
func TestBooksMT4_GBFreeSanity(t *testing.T) {
	store := newMT3MemStore()
	stub := newMT4OLStub(t)
	c := newBooksMT4Server(t, store, stub, true, nil)

	rec := c.get(t, "/api/v1/search/books/lookup?query=dispossessed")
	if rec.Code != http.StatusOK {
		t.Fatalf("lookup status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = c.get(t, "/api/v1/search/books/works/OL1W/editions")
	if rec.Code != http.StatusOK {
		t.Fatalf("editions status = %d body=%s", rec.Code, rec.Body.String())
	}
}
