package providers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// gbFixture is a real-shaped Google Books volumes response: two items —
// a full BOOK volume (the identifier/edition data) and an AUDIOBOOK
// entry (the plan's "especially useful for audiobooks" note: the mapping
// must survive a non-BOOK printType; nothing is filtered by it).
const gbFixture = `{
  "kind": "books#volumes",
  "totalItems": 2,
  "items": [
    {
      "id": "AbCdefGhIjK",
      "etag": "abc123",
      "selfLink": "https://www.googleapis.com/books/v1/volumes/AbCdefGhIjK",
      "volumeInfo": {
        "title": "The Dispossessed",
        "subtitle": "An Ecotone Novel",
        "authors": ["Ursula K. Le Guin"],
        "publisher": "Ace",
        "publishedDate": "1974-06-04",
        "pageCount": 368,
        "industryIdentifiers": [
          {"type": "ISBN_13", "identifier": "9780441478792"},
          {"type": "ISBN_10", "identifier": "0441478792"}
        ],
        "language": "en",
        "printType": "BOOK"
      },
      "saleInfo": {
        "country": "US",
        "price": 18.0,
        "isAvailable": true,
        "isEpub": true,
        "availability": 1
      }
    },
    {
      "id": "zYxWvUstRq",
      "volumeInfo": {
        "title": "The Dispossessed",
        "authors": ["Ursula K. Le Guin", "Michael J. Fox"],
        "publisher": "Random House Audio",
        "publishedDate": "2004",
        "language": "en",
        "printType": "AUDIOBOOK"
      },
      "saleInfo": {"country": "US", "isAvailable": true, "availability": 1}
    }
  ]
}`

// newGBStub stands up a stub Google Books API recording the `q` and
// `key` query parameters; it serves fixture (or a custom body) for
// /volumes.
func newGBStub(t *testing.T, body string, calls *[]string) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/volumes" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if calls != nil {
			*calls = append(*calls, "q="+r.URL.Query().Get("q")+" key="+r.URL.Query().Get("key"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(s.Close)
	return s
}

func newTestGB(t *testing.T, body string, calls *[]string) *GoogleBooks {
	t.Helper()
	gb := NewGoogleBooks("test-key")
	gb.BaseURL = newGBStub(t, body, calls).URL
	return gb
}

func TestGoogleBooks_FreeTextMapping(t *testing.T) {
	var calls []string
	gb := newTestGB(t, gbFixture, &calls)

	res, err := gb.SearchBooks(context.Background(), "the dispossessed le guin")
	if err != nil {
		t.Fatalf("SearchBooks: %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("hits = %d, want 2 (BOOK + AUDIOBOOK — no printType filtering)", len(res))
	}
	book := res[0]
	if book.ID != "AbCdefGhIjK" {
		t.Errorf("ID = %q, want the volume id AbCdefGhIjK", book.ID)
	}
	if book.Title != "The Dispossessed" {
		t.Errorf("Title = %q", book.Title)
	}
	if book.Year != 1974 {
		t.Errorf("Year = %d, want 1974 (from publishedDate 1974-06-04)", book.Year)
	}
	if book.Extra["author"] != "Ursula K. Le Guin" {
		t.Errorf("author extra = %q", book.Extra["author"])
	}
	if book.Extra["languages"] != "en" {
		t.Errorf("languages extra = %q, want en", book.Extra["languages"])
	}
	if book.Extra["publisher"] != "Ace" {
		t.Errorf("publisher extra = %q", book.Extra["publisher"])
	}
	if book.Extra["print_type"] != "BOOK" {
		t.Errorf("print_type extra = %q", book.Extra["print_type"])
	}
	// ISBN_13 wins over ISBN_10.
	if book.Extra["isbn"] != "9780441478792" {
		t.Errorf("isbn extra = %q, want the ISBN_13", book.Extra["isbn"])
	}
	if book.Extra["sale_info"] != "1" {
		t.Errorf("sale_info extra = %q, want the availability code 1", book.Extra["sale_info"])
	}
	// The AUDIOBOOK entry survives the mapping (plan: "especially useful
	// for audiobooks" — the mapping must not drop it).
	audio := res[1]
	if audio.Title != "The Dispossessed" || audio.Extra["print_type"] != "AUDIOBOOK" {
		t.Errorf("audiobook hit = %+v, want title + AUDIOBOOK printType", audio)
	}
	if audio.Year != 2004 {
		t.Errorf("audiobook Year = %d, want 2004 (bare-year publishedDate)", audio.Year)
	}
	// No ISBN on the audiobook: no isbn extra.
	if _, ok := audio.Extra["isbn"]; ok {
		t.Error("audiobook should have no isbn extra")
	}
	// The key parameter is sent (Google convention).
	if len(calls) != 1 || calls[0] != "q=the dispossessed le guin key=test-key" {
		t.Errorf("calls = %v, want the free-text q + the API key", calls)
	}
}

func TestGoogleBooks_ISBNQueryForms(t *testing.T) {
	// Bare 13 digits => the volumes API's isbn: form.
	var bare []string
	gb := newTestGB(t, `{"kind":"books#volumes","totalItems":0,"items":[]}`, &bare)
	if _, err := gb.SearchBooks(context.Background(), "9780441478792"); err != nil {
		t.Fatalf("bare isbn: %v", err)
	}
	if bare[0] != "q=isbn:9780441478792 key=test-key" {
		t.Errorf("bare = %q, want q=isbn:9780441478792", bare[0])
	}

	// Prefixed passes through unchanged.
	var pref []string
	gb2 := newTestGB(t, `{"kind":"books#volumes","totalItems":0,"items":[]}`, &pref)
	if _, err := gb2.SearchBooks(context.Background(), "isbn:9780441478792"); err != nil {
		t.Fatalf("isbn: %v", err)
	}
	if pref[0] != "q=isbn:9780441478792 key=test-key" {
		t.Errorf("prefixed = %q", pref[0])
	}
}

func TestGoogleBooks_AsinReturnsEmptyNoCall(t *testing.T) {
	// Google Books has no ASIN identifier: both the asin: prefix and the
	// bare B+9 form return an empty result with NO HTTP call.
	for _, q := range []string{"asin:B000S5MU0K", "B000S5MU0K", "b000s5mu0k"} {
		var calls []string
		gb := newTestGB(t, gbFixture, &calls)
		res, err := gb.SearchBooks(context.Background(), q)
		if err != nil {
			t.Fatalf("%q: %v", q, err)
		}
		if len(res) != 0 {
			t.Errorf("%q: hits = %d, want 0", q, len(res))
		}
		if len(calls) != 0 {
			t.Errorf("%q: calls = %v, want none (no HTTP)", q, calls)
		}
	}
}

func TestGoogleBooks_NotConfigured(t *testing.T) {
	var calls []string
	gb := NewGoogleBooks("")
	gb.BaseURL = newGBStub(t, gbFixture, &calls).URL
	if gb.Configured() {
		t.Fatal("Configured() = true with an empty key")
	}
	_, err := gb.SearchBooks(context.Background(), "test")
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("err = %v, want ErrNotConfigured (the direct-use 503 mapping)", err)
	}
	if len(calls) != 0 {
		t.Errorf("calls = %v, want none", calls)
	}
}

func TestGoogleBooks_MalformedJSON502(t *testing.T) {
	var calls []string
	gb := newTestGB(t, `{not json`, &calls)
	_, err := gb.SearchBooks(context.Background(), "test")
	if !errors.Is(err, ErrBadResponse) {
		t.Fatalf("err = %v, want ErrBadResponse (the 502 mapping)", err)
	}
}

func TestGoogleBooks_Non2xx502(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(s.Close)
	gb := NewGoogleBooks("test-key")
	gb.BaseURL = s.URL
	_, err := gb.SearchBooks(context.Background(), "test")
	if !errors.Is(err, ErrBadResponse) {
		t.Fatalf("err = %v, want ErrBadResponse", err)
	}
}

func TestGoogleBooks_YearParsing(t *testing.T) {
	for in, want := range map[string]int{
		"1974-06-04": 1974,
		"2004":       2004,
		"1942-06":    1942,
		"circa 1900": 1900,
		"":           0,
		"unknown":    0,
	} {
		if got := gbYear(in); got != want {
			t.Errorf("gbYear(%q) = %d, want %d", in, got, want)
		}
	}
}
