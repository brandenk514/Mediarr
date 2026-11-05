package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/brandenk514/mediarr/internal/auth"
	booksdom "github.com/brandenk514/mediarr/internal/domains/books"
	"github.com/brandenk514/mediarr/internal/health"
	"github.com/brandenk514/mediarr/internal/providers"
	bookssvc "github.com/brandenk514/mediarr/internal/services/books"
)

// --- MT1 API fakes ---

// mt1OLStub is a stub Open Library /search.json that answers the MT1 id
// lookups (the declared q= solr queries: isbn:/ASIN/key:) and degrades the
// plain-text legs (author=/title=) to empty results. failISBN makes the
// isbn: leg answer 502 (the upstream-failure test).
func mt1OLStub(t *testing.T, calls *int, failISBN bool) string {
	t.Helper()
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*calls++
		if r.URL.Path != "/search.json" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("q") {
		case "isbn:9780140449335":
			if failISBN {
				w.WriteHeader(http.StatusBadGateway)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": "upstream"})
				return
			}
			docs := []map[string]any{
				{"key": "/works/OL66554W", "title": "Pride and Prejudice", "author_name": []string{"Jane Austen"}, "first_publish_year": 1813, "cover_i": 111},
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"numFound": 1, "docs": docs})
		case "B000S5MU0K":
			docs := []map[string]any{
				{"key": "/works/OL17732W", "title": "Bible", "author_name": []string{"Various"}, "first_publish_year": 1871},
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"numFound": 1, "docs": docs})
		case "key:OL59863W":
			docs := []map[string]any{
				{"key": "/works/OL59863W", "title": "The Dispossessed", "author_name": []string{"Ursula K. Le Guin"}, "first_publish_year": 1974, "cover_i": 12345},
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"numFound": 1, "docs": docs})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"numFound": 0, "docs": []any{}})
		}
	}))
	t.Cleanup(stub.Close)
	return stub.URL
}

// mt1GRStub is a stub Goodreads book page (/book/show/<id>) carrying the
// ISBN in a JSON-LD schema.org Book node — the MT1 chain's first leg.
func mt1GRStub(t *testing.T, calls *int) string {
	t.Helper()
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*calls++
		w.Header().Set("Content-Type", "text/html")
		if r.URL.Path != "/book/show/14" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`<script type="application/ld+json">{"@type":"Book","name":"Pride and Prejudice","isbn":"978-0-14-044933-5"}</script>`))
	}))
	t.Cleanup(stub.Close)
	return stub.URL
}

// mt1GRWalledStub is the Cloudflare-walled Goodreads page (a 403 challenge):
// the MT1 degradation test.
func mt1GRWalledStub(t *testing.T, calls *int) string {
	t.Helper()
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*calls++
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`<html><head><title>Just a moment...</title></head></html>`))
	}))
	t.Cleanup(stub.Close)
	return stub.URL
}

// newBooksMT1Server builds a FRESH authenticated server wired the way
// main.go does (real OL adapter at a stub + in-memory repo + MT3 store +
// Goodreads resolver). grURL "" leaves the Goodreads resolver unwired
// (nil); failISBN makes the isbn: OL leg fail with a 502.
func newBooksMT1Server(t *testing.T, store booksdom.MetadataStore, olCalls, grCalls *int, failISBN bool, grURL string) *booksB2Client {
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

	var gr bookssvc.GoodreadsResolver
	if grURL != "" {
		g := providers.NewGoodreads()
		g.BaseURL = grURL
		gr = g
	}

	ol := providers.NewOpenLibrary()
	ol.BaseURL = mt1OLStub(t, olCalls, failISBN)
	svc := bookssvc.New(bookssvc.Deps{
		Repo:              newKindsBooksRepo(),
		DefaultProfile:    "Best",
		Searcher:          bookssvc.NewOpenLibrarySearcher(ol),
		MetadataStore:     store,
		GoodreadsResolver: gr,
	})
	s.SetBooks(&BooksDeps{Svc: svc})
	return &booksB2Client{h: s.Handler(), token: lr.Token}
}

// --- MT1 lookup endpoint tests ---

// TestBooksMT1_LookupISBN is the isbn: syntax end-to-end: it resolves
// through the OL isbn: search, returns the kind=book hit, and write-throughs
// the resolved work into the provenance store (the MT1 single-source rule).
func TestBooksMT1_LookupISBN(t *testing.T) {
	store := newMT3MemStore()
	olCalls, grCalls := 0, 0
	c := newBooksMT1Server(t, store, &olCalls, &grCalls, false, "")

	rec := c.get(t, "/api/v1/search/books/lookup?query=isbn:9780140449335")
	if rec.Code != http.StatusOK {
		t.Fatalf("lookup status = %d body=%s", rec.Code, rec.Body.String())
	}
	var hits []bookLookupResult
	if err := json.NewDecoder(rec.Body).Decode(&hits); err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("got %d hits, want 1: %+v", len(hits), hits)
	}
	if hits[0].Kind != "book" || hits[0].Name != "Pride and Prejudice" || hits[0].Author != "Jane Austen" || hits[0].Year != 1813 {
		t.Fatalf("hit = %+v, want the ISBN-resolved work", hits[0])
	}
	if hits[0].Added {
		t.Fatalf("id-resolved work must not be marked added: %+v", hits[0])
	}
	if olCalls != 1 {
		t.Errorf("OL calls = %d, want 1 (the isbn: leg)", olCalls)
	}
	if grCalls != 0 {
		t.Errorf("goodreads calls = %d, want 0 (not the goodreads leg)", grCalls)
	}
	if store.upsertCount() != 1 {
		t.Errorf("store upserts = %d, want 1 (write-through)", store.upsertCount())
	}
	if row, ok := store.rows["openlibrary\x00OL66554W"]; !ok || row.Title != "Pride and Prejudice" {
		t.Errorf("provenance row = %+v ok=%v, want the OL key under source openlibrary", row, ok)
	}
}

// TestBooksMT1_LookupASIN is the asin: syntax end-to-end (the full-text
// ASIN term on search.json).
func TestBooksMT1_LookupASIN(t *testing.T) {
	store := newMT3MemStore()
	olCalls, grCalls := 0, 0
	c := newBooksMT1Server(t, store, &olCalls, &grCalls, false, "")

	rec := c.get(t, "/api/v1/search/books/lookup?query=asin:B000S5MU0K")
	if rec.Code != http.StatusOK {
		t.Fatalf("lookup status = %d body=%s", rec.Code, rec.Body.String())
	}
	var hits []bookLookupResult
	if err := json.NewDecoder(rec.Body).Decode(&hits); err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Kind != "book" || hits[0].Name != "Bible" {
		t.Fatalf("hits = %+v, want the ASIN-resolved work", hits)
	}
	if olCalls != 1 || store.upsertCount() != 1 {
		t.Errorf("OL calls = %d, upserts = %d; want 1/1", olCalls, store.upsertCount())
	}
	if _, ok := store.rows["openlibrary\x00OL17732W"]; !ok {
		t.Errorf("provenance row missing for the ASIN-resolved work (ok=%v)", ok)
	}
}

// TestBooksMT1_LookupGoodreads is the goodreads: chain end-to-end: Goodreads
// id → ISBN (the page stub) → work (the OL isbn: leg), write-throughed.
func TestBooksMT1_LookupGoodreads(t *testing.T) {
	store := newMT3MemStore()
	olCalls, grCalls := 0, 0
	c := newBooksMT1Server(t, store, &olCalls, &grCalls, false, mt1GRStub(t, &grCalls))

	rec := c.get(t, "/api/v1/search/books/lookup?query=goodreads:14")
	if rec.Code != http.StatusOK {
		t.Fatalf("lookup status = %d body=%s", rec.Code, rec.Body.String())
	}
	var hits []bookLookupResult
	if err := json.NewDecoder(rec.Body).Decode(&hits); err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Kind != "book" || hits[0].Name != "Pride and Prejudice" {
		t.Fatalf("hits = %+v, want the chained work", hits)
	}
	if grCalls != 1 {
		t.Errorf("goodreads calls = %d, want 1 (the chain's first leg)", grCalls)
	}
	if olCalls != 1 {
		t.Errorf("OL calls = %d, want 1 (the chained isbn: leg)", olCalls)
	}
	if store.upsertCount() != 1 {
		t.Errorf("store upserts = %d, want 1 (write-through)", store.upsertCount())
	}
}

// TestBooksMT1_LookupGoodreadsWalled pins the MT1 degradation through the
// HTTP layer: a Cloudflare-walled Goodreads page answers the lookup 200 with
// an EMPTY result list (never a 5xx) — the rest of the lookup is not broken.
func TestBooksMT1_LookupGoodreadsWalled(t *testing.T) {
	store := newMT3MemStore()
	olCalls, grCalls := 0, 0
	c := newBooksMT1Server(t, store, &olCalls, &grCalls, false, mt1GRWalledStub(t, &grCalls))

	rec := c.get(t, "/api/v1/search/books/lookup?query=goodreads:14")
	if rec.Code != http.StatusOK {
		t.Fatalf("lookup status = %d, want 200 (a Goodreads outage must not 5xx) body=%s", rec.Code, rec.Body.String())
	}
	var hits []bookLookupResult
	if err := json.NewDecoder(rec.Body).Decode(&hits); err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("got %d hits, want 0 (no ISBN -> no work): %+v", len(hits), hits)
	}
	if olCalls != 0 {
		t.Errorf("OL calls = %d, want 0 (no ISBN to chain into)", olCalls)
	}
}

// TestBooksMT1_LookupGoodreadsNotWired pins the unwired-resolver degradation:
// 200 with an empty result list.
func TestBooksMT1_LookupGoodreadsNotWired(t *testing.T) {
	store := newMT3MemStore()
	olCalls, grCalls := 0, 0
	c := newBooksMT1Server(t, store, &olCalls, &grCalls, false, "")

	rec := c.get(t, "/api/v1/search/books/lookup?query=goodreads:14")
	if rec.Code != http.StatusOK {
		t.Fatalf("lookup status = %d, want 200", rec.Code)
	}
	var hits []bookLookupResult
	if err := json.NewDecoder(rec.Body).Decode(&hits); err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("got %d hits, want 0: %+v", len(hits), hits)
	}
}

// TestBooksMT1_LookupInvalidPrefixPlainText pins the invalid-value fallback
// through the HTTP layer: isbn:<non-ISBN> is not an id query — the raw query
// runs as a plain-text search (the author + work legs), 200 with empty
// results, and the id legs are never hit.
func TestBooksMT1_LookupInvalidPrefixPlainText(t *testing.T) {
	store := newMT3MemStore()
	olCalls, grCalls := 0, 0
	c := newBooksMT1Server(t, store, &olCalls, &grCalls, false, "")

	rec := c.get(t, "/api/v1/search/books/lookup?query=isbn:abc")
	if rec.Code != http.StatusOK {
		t.Fatalf("lookup status = %d, want 200 (plain-text fallback) body=%s", rec.Code, rec.Body.String())
	}
	var hits []bookLookupResult
	if err := json.NewDecoder(rec.Body).Decode(&hits); err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("got %d hits, want 0 (the stub's plain-text legs answer empty): %+v", len(hits), hits)
	}
	if olCalls != 2 {
		t.Errorf("OL calls = %d, want 2 (the author + work plain-text legs, not the isbn: leg)", olCalls)
	}
}

// TestBooksMT1_LookupOLidPrefix pins the olid: syntax through the HTTP
// layer: the MT3 cached work-key path (store miss → re-fetch, write-through;
// the repeat lookup is served from the store with no further OL call).
func TestBooksMT1_LookupOLidPrefix(t *testing.T) {
	store := newMT3MemStore()
	olCalls, grCalls := 0, 0
	c := newBooksMT1Server(t, store, &olCalls, &grCalls, false, "")

	rec := c.get(t, "/api/v1/search/books/lookup?query=olid:OL59863W")
	if rec.Code != http.StatusOK {
		t.Fatalf("lookup status = %d body=%s", rec.Code, rec.Body.String())
	}
	var hits []bookLookupResult
	if err := json.NewDecoder(rec.Body).Decode(&hits); err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Kind != "book" || hits[0].Name != "The Dispossessed" {
		t.Fatalf("hits = %+v, want the key-resolved work", hits)
	}
	if olCalls != 1 {
		t.Errorf("OL calls = %d, want 1 (store miss re-fetches)", olCalls)
	}

	rec = c.get(t, "/api/v1/search/books/lookup?query=olid:OL59863W")
	if rec.Code != http.StatusOK {
		t.Fatalf("lookup(after) status = %d", rec.Code)
	}
	if olCalls != 1 {
		t.Errorf("OL calls after repeat = %d, want 1 (served from the store)", olCalls)
	}
}

// TestBooksMT1_LookupISBN502 pins the upstream-failure map: an OL 502 on the
// unambiguous isbn: leg surfaces as a 502 (never a silent empty list).
func TestBooksMT1_LookupISBN502(t *testing.T) {
	store := newMT3MemStore()
	olCalls, grCalls := 0, 0
	c := newBooksMT1Server(t, store, &olCalls, &grCalls, true, "")

	rec := c.get(t, "/api/v1/search/books/lookup?query=isbn:9780140449335")
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("lookup status = %d, want 502 body=%s", rec.Code, rec.Body.String())
	}
}
