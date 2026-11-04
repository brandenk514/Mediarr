package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/brandenk514/mediarr/internal/auth"
	"github.com/brandenk514/mediarr/internal/health"
	"github.com/brandenk514/mediarr/internal/providers"
	bookssvc "github.com/brandenk514/mediarr/internal/services/books"
)

// booksB2Client issues authenticated requests against a FRESH server (the mux
// double-register guard).
type booksB2Client struct {
	h     http.Handler
	token string
}

func (c *booksB2Client) get(t *testing.T, path string) *httptest.ResponseRecorder {
	return getJSON(t, c.h, path, c.token)
}

func (c *booksB2Client) post(t *testing.T, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	rec := httptest.NewRecorder()
	c.h.ServeHTTP(rec, req)
	return rec
}

func (c *booksB2Client) put(t *testing.T, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPut, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	rec := httptest.NewRecorder()
	c.h.ServeHTTP(rec, req)
	return rec
}

// newBooksB2Server builds a FRESH authenticated server with a books service
// backed by an in-memory repo and the REAL Open Library adapter pointed at an
// httptest stub (Readarr B2 combined lookup + add flow e2e).
func newBooksB2Server(t *testing.T, repo *kindsBooksRepo) *booksB2Client {
	t.Helper()
	// Stub Open Library /search.json (author and work searches share the doc
	// shape for the stub).
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search.json" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		docs := []map[string]any{
			{"key": "/works/A1W", "title": "The Dispossessed", "author_name": []string{"Ursula K. Le Guin"}, "first_publish_year": 1974},
			{"key": "/works/A2W", "title": "Other Book", "author_name": []string{"Nobody Famous"}, "first_publish_year": 2000},
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"numFound": 2, "docs": docs})
	}))
	t.Cleanup(stub.Close)

	store := newFakeStore()
	s := NewServer(health.NewChecker(nil, nil))
	s.SetAuth(&AuthDeps{Repo: store})
	hash, err := auth.HashPassword("a-very-long-password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateUser(context.Background(), "admin", "", "admin", hash); err != nil {
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
	ol.BaseURL = stub.URL
	svc := bookssvc.New(bookssvc.Deps{
		Repo: repo, DefaultProfile: "Best",
		Searcher: bookssvc.NewOpenLibrarySearcher(ol),
	})
	s.SetBooks(&BooksDeps{Svc: svc})
	return &booksB2Client{h: s.Handler(), token: lr.Token}
}

// TestBooksB2_LookupAndAddFlow is the Readarr B2 end-to-end: a combined plain-
// text lookup (authors AND works, kind-tagged with `added`), then the add
// author flow with the new B2 fields + best-effort start_search, then the
// lookup re-run showing the now-in-library hits flagged added.
func TestBooksB2_LookupAndAddFlow(t *testing.T) {
	repo := newKindsBooksRepo()
	c := newBooksB2Server(t, repo)

	// 1. Combined lookup against an empty library: 2 author + 2 book hits,
	//    none added. Authors come first, then books.
	rec := c.get(t, "/api/v1/search/books/lookup?q=ursula")
	if rec.Code != http.StatusOK {
		t.Fatalf("lookup status = %d body=%s", rec.Code, rec.Body.String())
	}
	var hits []bookLookupResult
	if err := json.NewDecoder(rec.Body).Decode(&hits); err != nil {
		t.Fatal(err)
	}
	if len(hits) != 4 {
		t.Fatalf("lookup hits = %d, want 4: %+v", len(hits), hits)
	}
	if hits[0].Kind != "author" || hits[0].Name != "Ursula K. Le Guin" || hits[0].Added {
		t.Errorf("first hit = %+v, want author Ursula (not added)", hits[0])
	}
	if hits[2].Kind != "book" || hits[2].Name != "The Dispossessed" || hits[2].Author != "Ursula K. Le Guin" || hits[2].Year != 1974 {
		t.Errorf("first book hit = %+v, want The Dispossessed / Ursula / 1974", hits[2])
	}

	// 2. Add the author with the B2 fields + best-effort start_search.
	rec = c.post(t, "/api/v1/books/authors", map[string]any{
		"name": "Ursula K. Le Guin", "monitored": true,
		"monitor_policy": "first", "quality_profile": "Best",
		"metadata_profile": "Default", "root_folder": "/media/books",
		"tags": []string{"sci-fi"}, "start_search": true,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("add author status = %d body=%s", rec.Code, rec.Body.String())
	}
	var id struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&id); err != nil {
		t.Fatal(err)
	}

	// Add a monitored title (the add flow's second step).
	rec = c.post(t, fmt.Sprintf("/api/v1/books/authors/%d/titles", id.ID),
		map[string]any{"name": "The Dispossessed", "monitored": true})
	if rec.Code != http.StatusCreated {
		t.Fatalf("add title status = %d body=%s", rec.Code, rec.Body.String())
	}

	// 3. The author's B2 fields round-trip (PascalCase wire, Author schema).
	rec = c.get(t, fmt.Sprintf("/api/v1/books/authors/%d", id.ID))
	if rec.Code != http.StatusOK {
		t.Fatalf("get author status = %d", rec.Code)
	}
	var author struct {
		MonitorPolicy   string   `json:"MonitorPolicy"`
		QualityProfile  string   `json:"QualityProfile"`
		MetadataProfile string   `json:"MetadataProfile"`
		RootFolder      string   `json:"RootFolder"`
		Tags            []string `json:"Tags"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&author); err != nil {
		t.Fatal(err)
	}
	if author.MonitorPolicy != "first" || author.QualityProfile != "Best" ||
		author.MetadataProfile != "Default" || author.RootFolder != "/media/books" {
		t.Errorf("author B2 fields = %+v", author)
	}
	if len(author.Tags) != 1 || author.Tags[0] != "sci-fi" {
		t.Errorf("tags = %v, want [sci-fi]", author.Tags)
	}

	// 4. Edit the author via PUT (the Edit Author modal path). It returns the
	//    updated monitored state (MonitoredResult), mirroring music.
	rec = c.put(t, fmt.Sprintf("/api/v1/books/authors/%d", id.ID),
		map[string]any{"quality_profile": "Good", "monitored": true})
	if rec.Code != http.StatusOK {
		t.Fatalf("update author status = %d body=%s", rec.Code, rec.Body.String())
	}
	var mon struct {
		ID        int64 `json:"id"`
		Monitored bool  `json:"monitored"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&mon); err != nil {
		t.Fatal(err)
	}
	if mon.ID != id.ID || !mon.Monitored {
		t.Errorf("update author = %+v, want {id:%d monitored:true}", mon, id.ID)
	}
	// The edit applied: quality profile is now Good on the author row.
	rec = c.get(t, fmt.Sprintf("/api/v1/books/authors/%d", id.ID))
	if rec.Code != http.StatusOK {
		t.Fatalf("get author(after edit) status = %d", rec.Code)
	}
	var edited struct {
		QualityProfile string `json:"QualityProfile"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&edited); err != nil {
		t.Fatal(err)
	}
	if edited.QualityProfile != "Good" {
		t.Errorf("quality profile after edit = %q, want Good", edited.QualityProfile)
	}

	// 5. Re-run the lookup: the author and book are now added with ids.
	rec = c.get(t, "/api/v1/search/books/lookup?q=ursula")
	if rec.Code != http.StatusOK {
		t.Fatalf("lookup(after) status = %d", rec.Code)
	}
	var after []bookLookupResult
	if err := json.NewDecoder(rec.Body).Decode(&after); err != nil {
		t.Fatal(err)
	}
	var aHit, bHit *bookLookupResult
	for i := range after {
		if after[i].Kind == "author" && after[i].Name == "Ursula K. Le Guin" {
			aHit = &after[i]
		}
		if after[i].Kind == "book" && after[i].Name == "The Dispossessed" {
			bHit = &after[i]
		}
	}
	if aHit == nil || !aHit.Added || aHit.LibraryID != id.ID || aHit.AuthorID != id.ID {
		t.Errorf("author not flagged added: %+v (want added, library_id=%d, author_id=%d)", aHit, id.ID, id.ID)
	}
	if bHit == nil || !bHit.Added || bHit.LibraryID == 0 || bHit.AuthorID != id.ID {
		t.Errorf("book not flagged added: %+v (want added, library_id!=0, author_id=%d)", bHit, id.ID)
	}
}

// TestBooksB2_LookupEmptyQuery verifies the degenerate endpoint path: a blank
// q returns an empty list (200), not an error.
func TestBooksB2_LookupEmptyQuery(t *testing.T) {
	c := newBooksB2Server(t, newKindsBooksRepo())
	rec := c.get(t, "/api/v1/search/books/lookup?q=")
	if rec.Code != http.StatusOK {
		t.Fatalf("lookup(empty) status = %d", rec.Code)
	}
	var hits []bookLookupResult
	if err := json.NewDecoder(rec.Body).Decode(&hits); err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Errorf("lookup(empty) = %v hits, want 0", hits)
	}
}
