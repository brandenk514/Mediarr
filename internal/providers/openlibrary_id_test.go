package providers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestOpenLibrarySearchWorkByISBN pins the MT1 ISBN id lookup: the `q=isbn:`
// field query (normalized — hyphens stripped), the work-key mapping, and the
// B6-compatible empty short-circuit.
func TestOpenLibrarySearchWorkByISBN(t *testing.T) {
	url, rec := newStub(t, http.StatusOK, `{
	  "numFound": 1,
	  "docs": [{"key": "/works/OL66554W", "title": "Pride and Prejudice", "author_name": ["Jane Austen"], "first_publish_year": 1813, "cover_i": 42}]
	}`)
	o := NewOpenLibrary()
	o.BaseURL = url
	res, err := o.SearchWorkByISBN(context.Background(), "978-0-14-044933-5")
	if err != nil {
		t.Fatalf("SearchWorkByISBN: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("got %d results, want 1: %+v", len(res), res)
	}
	if res[0].ID != "OL66554W" || res[0].Title != "Pride and Prejudice" || res[0].Year != 1813 {
		t.Fatalf("bad result: %+v", res[0])
	}
	if res[0].Extra["author"] != "Jane Austen" || res[0].Extra["cover"] != "https://covers.openlibrary.org/b/id/42-M.jpg" {
		t.Fatalf("bad extra: %+v", res[0].Extra)
	}
	if rec.Path != "/search.json" {
		t.Fatalf("path = %s", rec.Path)
	}
	if rec.Query["q"][0] != "isbn:9780140449335" {
		t.Fatalf("q = %v, want isbn:9780140449335 (normalized field query, not the legacy id= param)", rec.Query["q"])
	}
}

// TestOpenLibrarySearchWorkByISBNEmpty pins the no-op for an empty ISBN (no
// HTTP call — the lookup's invalid-prefix fallback relies on it).
func TestOpenLibrarySearchWorkByISBNEmpty(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	o := NewOpenLibrary()
	o.BaseURL = srv.URL
	for _, in := range []string{"", "   "} {
		res, err := o.SearchWorkByISBN(context.Background(), in)
		if err != nil || res != nil {
			t.Fatalf("SearchWorkByISBN(%q) = %v, %v; want nil, nil", in, res, err)
		}
	}
	if called {
		t.Fatal("empty ISBN must not hit the network")
	}
}

// TestOpenLibrarySearchWorkByASIN pins the MT1 ASIN id lookup: the full-text
// q=<ASIN> query (no declared amazon_id filter exists on search.json).
func TestOpenLibrarySearchWorkByASIN(t *testing.T) {
	url, rec := newStub(t, http.StatusOK, `{
	  "numFound": 1,
	  "docs": [{"key": "/works/OL17732W", "title": "Bible", "author_name": ["Various"]}]
	}`)
	o := NewOpenLibrary()
	o.BaseURL = url
	res, err := o.SearchWorkByASIN(context.Background(), "B000S5MU0K")
	if err != nil {
		t.Fatalf("SearchWorkByASIN: %v", err)
	}
	if len(res) != 1 || res[0].ID != "OL17732W" {
		t.Fatalf("bad result: %+v", res)
	}
	if rec.Query["q"][0] != "B000S5MU0K" {
		t.Fatalf("q = %v, want B000S5MU0K (full-text ASIN term)", rec.Query["q"])
	}
}

// TestOpenLibrarySearchWorkByKey pins the MT1 key id lookup: the `q=key:`
// field query, with the full /works/ key form folded to the bare id.
func TestOpenLibrarySearchWorkByKey(t *testing.T) {
	url, rec := newStub(t, http.StatusOK, `{
	  "numFound": 1,
	  "docs": [{"key": "/works/OL59863W", "title": "The Dispossessed", "author_name": ["Ursula K. Le Guin"], "first_publish_year": 1974}]
	}`)
	o := NewOpenLibrary()
	o.BaseURL = url
	res, err := o.SearchWorkByKey(context.Background(), "/works/OL59863W")
	if err != nil {
		t.Fatalf("SearchWorkByKey: %v", err)
	}
	if len(res) != 1 || res[0].ID != "OL59863W" || res[0].Title != "The Dispossessed" {
		t.Fatalf("bad result: %+v", res)
	}
	if rec.Query["q"][0] != "key:OL59863W" {
		t.Fatalf("q = %v, want key:OL59863W (field query, not the legacy id= param)", rec.Query["q"])
	}
}

// TestOpenLibrarySearchWorkByISBN502 pins the 502-class mapping: a non-2xx
// search.json answer is ErrBadResponse (the keyless-adapter convention).
func TestOpenLibrarySearchWorkByISBN502(t *testing.T) {
	url, _ := newStub(t, http.StatusBadGateway, `{"error": "upstream"}`)
	o := NewOpenLibrary()
	o.BaseURL = url
	_, err := o.SearchWorkByISBN(context.Background(), "9780140449335")
	if !errors.Is(err, ErrBadResponse) {
		t.Fatalf("err = %v, want ErrBadResponse", err)
	}
}

// TestOpenLibrarySearchWorkByASINEmpty pins the no-op for an empty ASIN.
func TestOpenLibrarySearchWorkByASINEmpty(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	o := NewOpenLibrary()
	o.BaseURL = srv.URL
	res, err := o.SearchWorkByASIN(context.Background(), "  ")
	if err != nil || res != nil {
		t.Fatalf("SearchWorkByASIN(whitespace) = %v, %v; want nil, nil", res, err)
	}
	if called {
		t.Fatal("empty ASIN must not hit the network")
	}
}
