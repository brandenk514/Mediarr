package providers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newGRStub builds an HTML-serving stub (newStub hard-codes a JSON content
// type) and records the request; it also counts calls for the no-op tests.
func newGRStub(t *testing.T, status int, body string) (string, *recordedRequest, *int) {
	t.Helper()
	rec := &recordedRequest{}
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		*rec = recordedRequest{
			Method: r.Method,
			Path:   r.URL.Path,
			Query:  r.URL.Query(),
			Header: r.Header.Clone(),
		}
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL, rec, &calls
}

// TestGoodreadsBookISBNJSONLD is the primary path: the schema.org Book node
// in the page's JSON-LD carries the ISBN (a hyphenated string).
func TestGoodreadsBookISBNJSONLD(t *testing.T) {
	page := `<html><head>
<script type="application/ld+json">
{"@context":"https://schema.org","@graph":[
  {"@type":"WebSite","name":"Goodreads"},
  {"@type":"Book","name":"Pride and Prejudice","author":[{"@type":"Person","name":"Jane Austen"}],"isbn":"978-0-14-044933-5"}
]}
</script>
</head><body><div id="metadata_block">...</div></body></html>`
	url, rec, _ := newGRStub(t, http.StatusOK, page)
	g := NewGoodreads()
	g.BaseURL = url
	isbn, err := g.BookISBN(context.Background(), "14")
	if err != nil {
		t.Fatalf("BookISBN: %v", err)
	}
	if isbn != "9780140449335" {
		t.Fatalf("isbn = %q, want 9780140449335 (hyphens stripped)", isbn)
	}
	if rec.Path != "/book/show/14" {
		t.Fatalf("path = %s, want /book/show/14", rec.Path)
	}
	if ua := rec.Header.Get("User-Agent"); ua != goodreadsUserAgent {
		t.Fatalf("User-Agent = %q, want the browser-like UA", ua)
	}
}

// TestGoodreadsBookISBNListJSONLD: the isbn value as a list of strings.
func TestGoodreadsBookISBNListJSONLD(t *testing.T) {
	page := `<script type="application/ld+json">
{"@type":"Book","name":"Bible","isbn":["9780785212832"]}
</script>`
	url, _, _ := newGRStub(t, http.StatusOK, page)
	g := NewGoodreads()
	g.BaseURL = url
	isbn, err := g.BookISBN(context.Background(), "123")
	if err != nil {
		t.Fatalf("BookISBN: %v", err)
	}
	if isbn != "9780785212832" {
		t.Fatalf("isbn = %q", isbn)
	}
}

// TestGoodreadsBookISBNStateFallback: no usable JSON-LD, but the embedded
// application state has the isbn13 field. The closing-quote anchor keeps a
// 13-digit value from being mis-read as its first 10 digits.
func TestGoodreadsBookISBNStateFallback(t *testing.T) {
	page := `<html><head><script>var x = 1;</script></head>
<body><script>window.__APOLLO_STATE__ = {"book":{"isbn13":"9780140449335","isbn":"0140449335"}};</script></body></html>`
	url, _, _ := newGRStub(t, http.StatusOK, page)
	g := NewGoodreads()
	g.BaseURL = url
	isbn, err := g.BookISBN(context.Background(), "14")
	if err != nil {
		t.Fatalf("BookISBN: %v", err)
	}
	if isbn != "9780140449335" {
		t.Fatalf("isbn = %q, want the 13-digit value (isbn13 wins)", isbn)
	}
}

// TestGoodreadsBookISBNDetailsTable is the last-resort legacy path: the
// details-table <td>ISBN</td> row.
func TestGoodreadsBookISBNDetailsTable(t *testing.T) {
	page := `<html><body><table class="details">
<tr><td>ISBN</td><td><a href="/book/isbn/0140449335">0140449335</a></td></tr>
</table></body></html>`
	url, _, _ := newGRStub(t, http.StatusOK, page)
	g := NewGoodreads()
	g.BaseURL = url
	isbn, err := g.BookISBN(context.Background(), "14")
	if err != nil {
		t.Fatalf("BookISBN: %v", err)
	}
	if isbn != "0140449335" {
		t.Fatalf("isbn = %q, want 0140449335", isbn)
	}
}

// TestGoodreadsBookISBNWalled: a Cloudflare 403 challenge (or any non-2xx)
// degrades to ErrBadResponse — never a panic, never a break of the lookup.
func TestGoodreadsBookISBNWalled(t *testing.T) {
	page := `<html><head><title>Just a moment...</title></head><body>Cloudflare challenge</body></html>`
	url, _, _ := newGRStub(t, http.StatusForbidden, page)
	g := NewGoodreads()
	g.BaseURL = url
	_, err := g.BookISBN(context.Background(), "14")
	if !errors.Is(err, ErrBadResponse) {
		t.Fatalf("err = %v, want ErrBadResponse", err)
	}
}

// TestGoodreadsBookISBNNoISBN: a 200 page with no parseable ISBN degrades to
// ErrBadResponse (the lookup returns an empty result for it).
func TestGoodreadsBookISBNNoISBN(t *testing.T) {
	page := `<html><body><p>No identifiers on this page.</p></body></html>`
	url, _, _ := newGRStub(t, http.StatusOK, page)
	g := NewGoodreads()
	g.BaseURL = url
	_, err := g.BookISBN(context.Background(), "14")
	if !errors.Is(err, ErrBadResponse) {
		t.Fatalf("err = %v, want ErrBadResponse", err)
	}
	if !strings.Contains(err.Error(), "no isbn found") {
		t.Fatalf("err = %v, want a no-isbn message", err)
	}
}

// TestGoodreadsBookISBNInvalidID: a non-numeric id is rejected before any
// HTTP call.
func TestGoodreadsBookISBNInvalidID(t *testing.T) {
	url, _, calls := newGRStub(t, http.StatusOK, "<html></html>")
	g := NewGoodreads()
	g.BaseURL = url
	for _, id := range []string{"", "abc", "14-abc", "  "} {
		_, err := g.BookISBN(context.Background(), id)
		if !errors.Is(err, ErrBadResponse) {
			t.Fatalf("BookISBN(%q): err = %v, want ErrBadResponse", id, err)
		}
	}
	if *calls != 0 {
		t.Fatalf("calls = %d, want 0 (invalid ids never hit the network)", *calls)
	}
}
