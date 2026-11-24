package providers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Unified Discover feed tests (issue #118, A2): the MusicBrainz
// release-group / release feeds, the Open Library trending +
// first_publish_year feeds, and the Google Books keyed feed. All against
// in-process httptest stubs — no network, no tokens.

// ---- MusicBrainz ----

const mbTrendingJSON = `{
  "count": 3,
  "release-groups": [
    {
      "id": "rg-old",
      "title": "Old Group",
      "first-release-date": "2026-10-25",
      "artist-credit": [{"name": "Old Artist"}]
    },
    {
      "id": "rg-yearonly",
      "title": "Year Only",
      "first-release-date": "2026",
      "artist-credit": [{"name": "Year Artist"}]
    },
    {
      "id": "rg-new",
      "title": "New Group",
      "first-release-date": "2026-11-20",
      "artist-credit": [{"name": "New Artist"}]
    },
    {
      "id": "rg-new",
      "title": "New Group (dup)",
      "first-release-date": "2026-11-20",
      "artist-credit": [{"name": "New Artist"}]
    }
  ]
}`

func TestMusicBrainzTrendingReleaseGroups(t *testing.T) {
	url, rec := newStub(t, http.StatusOK, mbTrendingJSON)
	m := NewMusicBrainz()
	m.BaseURL = url

	since := time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)
	until := time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC)
	items, err := m.TrendingReleaseGroups(context.Background(), since, until)
	if err != nil {
		t.Fatalf("TrendingReleaseGroups: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("got %d items, want 3 (dup by id dropped): %+v", len(items), items)
	}
	// Date-descending: the fully dated groups first (most recent), then
	// the year-only group (partial dates sort at year start so they
	// cannot out-rank a dated release in the same year).
	if items[0].SourceID != "rg-new" || items[0].Title != "New Group" {
		t.Fatalf("items[0] = %+v, want New Group (most recent)", items[0])
	}
	if items[1].SourceID != "rg-old" {
		t.Fatalf("items[1] = %+v, want Old Group next", items[1])
	}
	if items[2].SourceID != "rg-yearonly" {
		t.Fatalf("items[2] = %+v, want the year-only group last", items[2])
	}
	for _, it := range items {
		if it.Kind != "music" {
			t.Fatalf("kind = %q, want music", it.Kind)
		}
		if it.PosterURL != "" {
			t.Fatalf("poster = %q, want empty (MusicBrainz has no feed art)", it.PosterURL)
		}
	}
	if items[0].Subtitle != "New Artist" {
		t.Fatalf("subtitle = %q, want the first artist-credit name", items[0].Subtitle)
	}
	if items[0].Year != 2026 {
		t.Fatalf("year = %d, want 2026", items[0].Year)
	}
	// The Lucene field is the UNHYPHENATED firstreleasedate (the
	// hyphenated form is silently ignored by MusicBrainz and filters
	// nothing).
	if got := rec.Query["query"][0]; got != "firstreleasedate:[2026-10-25 TO 2026-11-24]" {
		t.Fatalf("query = %q", got)
	}
	if rec.Path != "/release-group" {
		t.Fatalf("path = %s", rec.Path)
	}
	if rec.Query["fmt"][0] != "json" || rec.Query["limit"][0] != "25" {
		t.Fatalf("fmt/limit = %q/%q, want json/25", rec.Query["fmt"][0], rec.Query["limit"][0])
	}
}

const mbReleasesJSON = `{
  "count": 2,
  "releases": [
    {
      "id": "rel-1",
      "title": "Together Again",
      "date": "2026-11",
      "artist-credit": [{"name": "Artist X"}],
      "release-group": {"id": "rg-x", "title": "Together Again (EP)"}
    },
    {
      "id": "rel-2",
      "title": "",
      "date": "2026-11",
      "artist-credit": [{"name": "Artist Y"}]
    }
  ]
}`

func TestMusicBrainzNewReleases(t *testing.T) {
	url, rec := newStub(t, http.StatusOK, mbReleasesJSON)
	m := NewMusicBrainz()
	m.BaseURL = url

	since := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	until := time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC)
	items, err := m.NewReleases(context.Background(), since, until)
	if err != nil {
		t.Fatalf("NewReleases: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1 (untitled dropped): %+v", len(items), items)
	}
	it := items[0]
	if it.SourceID != "rel-1" || it.Title != "Together Again" || it.Kind != "music" {
		t.Fatalf("item = %+v", it)
	}
	// The release id is the source id (the embedded release-group is
	// display-only context, not the identity).
	if it.Subtitle != "Artist X" {
		t.Fatalf("subtitle = %q, want the top-level artist-credit", it.Subtitle)
	}
	// Month-only dates are partial: the year is extracted, the full date
	// stays zero.
	if it.Year != 2026 || !it.Date.IsZero() {
		t.Fatalf("year/date = %d/%v, want 2026/zero", it.Year, it.Date)
	}
	if got := rec.Query["query"][0]; got != "date:[2026-11-10 TO 2026-11-24]" {
		t.Fatalf("query = %q", got)
	}
	if rec.Path != "/release" {
		t.Fatalf("path = %s", rec.Path)
	}
}

// TestMusicBrainzFeedRetriesOnBusy: a transient 503 "busy" answer is
// retried (bounded), then the 200 is served.
func TestMusicBrainzFeedRetriesOnBusy(t *testing.T) {
	busy := `{"error": "The MusicBrainz web server is currently busy. Please try again later."}`
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		if calls <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(busy))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(mbReleasesJSON))
	}))
	t.Cleanup(srv.Close)
	m := NewMusicBrainz()
	m.BaseURL = srv.URL

	items, err := m.NewReleases(context.Background(),
		time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("NewReleases: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3 (2 retries after the busy answers)", calls)
	}
}

// TestMusicBrainzFeedNoRetryOnNon503: a non-503 failure returns on the
// first attempt (no retry for hard errors).
func TestMusicBrainzFeedNoRetryOnNon503(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error": "boom"}`))
	}))
	t.Cleanup(srv.Close)
	m := NewMusicBrainz()
	m.BaseURL = srv.URL

	_, err := m.TrendingReleaseGroups(context.Background(),
		time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC))
	if !errors.Is(err, ErrBadResponse) {
		t.Fatalf("err = %v, want ErrBadResponse", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1 (no retry on non-503)", calls)
	}
}

// ---- Open Library ----

func TestOpenLibraryTrendingWeekly(t *testing.T) {
	url, rec := newStub(t, http.StatusOK, `{
  "query": "trending weekly",
  "days": 7,
  "works": [
    {
      "key": "/works/OL17930368W",
      "title": "Atomic Habits",
      "author_name": ["James Clear"],
      "cover_i": 12539702,
      "first_publish_year": 2018
    },
    {
      "key": "",
      "title": "No Key",
      "author_name": ["X"],
      "cover_i": 0,
      "first_publish_year": 0
    }
  ]
}`)
	o := NewOpenLibrary()
	o.BaseURL = url

	items, err := o.TrendingWeekly(context.Background())
	if err != nil {
		t.Fatalf("TrendingWeekly: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1 (keyless row dropped): %+v", len(items), items)
	}
	it := items[0]
	if it.SourceID != "OL17930368W" {
		t.Fatalf("sourceId = %q, want the normalized OLID", it.SourceID)
	}
	if it.Title != "Atomic Habits" || it.Year != 2018 || it.Subtitle != "James Clear" {
		t.Fatalf("item = %+v", it)
	}
	if it.PosterURL != "https://covers.openlibrary.org/b/id/12539702-M.jpg" {
		t.Fatalf("poster = %q, want the covers.openlibrary.org URL", it.PosterURL)
	}
	// The .json suffix is MANDATORY — the bare /trending/weekly path
	// answers HTML, not JSON.
	if rec.Path != "/trending/weekly.json" {
		t.Fatalf("path = %s, want /trending/weekly.json", rec.Path)
	}
}

func TestOpenLibraryNewReleasesByYear(t *testing.T) {
	url, rec := newStub(t, http.StatusOK, `{
  "numFound": 1,
  "docs": [
    {
      "key": "/works/OL9000W",
      "title": "First Published 2026",
      "author_name": ["A One", "B Two"],
      "cover_i": 0,
      "first_publish_year": 2026
    }
  ]
}`)
	o := NewOpenLibrary()
	o.BaseURL = url

	items, err := o.NewReleasesByYear(context.Background(), 2026)
	if err != nil {
		t.Fatalf("NewReleasesByYear: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1: %+v", len(items), items)
	}
	if items[0].SourceID != "OL9000W" || items[0].Subtitle != "A One" {
		t.Fatalf("item = %+v", items[0])
	}
	if items[0].PosterURL != "" {
		t.Fatalf("poster = %q, want empty (cover_i = 0)", items[0].PosterURL)
	}
	// The filter is the work-level first_publish_year field query —
	// never sort=new (date-ADDED-to-OL) and not publish_year (matches
	// any edition, so reissues pollute).
	if rec.Path != "/search.json" {
		t.Fatalf("path = %s, want /search.json", rec.Path)
	}
	if rec.Query["q"][0] != "first_publish_year:2026" {
		t.Fatalf("q = %q", rec.Query["q"][0])
	}
	if _, has := rec.Query["sort"]; has {
		t.Fatalf("must not set sort: %v", rec.Query["sort"])
	}
	if rec.Query["limit"][0] != "25" {
		t.Fatalf("limit = %q, want 25", rec.Query["limit"][0])
	}
}

// ---- Google Books ----

// TestGoogleBooksNewReleasesKeyless: an unset key is ErrNotConfigured
// with NO network call (the discover service treats that as the keyless
// fallback state).
func TestGoogleBooksNewReleasesKeyless(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	g := NewGoogleBooks("")
	g.BaseURL = srv.URL

	_, err := g.NewReleasesBooks(context.Background())
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("err = %v, want ErrNotConfigured", err)
	}
	if called {
		t.Fatal("keyless must not hit the network")
	}
}

func TestGoogleBooksNewReleases(t *testing.T) {
	url, rec := newStub(t, http.StatusOK, `{
  "items": [
    {
      "id": "vol-1",
      "volumeInfo": {
        "title": "Book One",
        "authors": ["Auth One"],
        "publishedDate": "2026-01-15",
        "imageLinks": {"thumbnail": "https://books.example/thumb1"}
      }
    },
    {
      "id": "vol-2",
      "volumeInfo": {}
    }
  ]
}`)
	g := NewGoogleBooks("test-key-123")
	g.BaseURL = url

	items, err := g.NewReleasesBooks(context.Background())
	if err != nil {
		t.Fatalf("NewReleasesBooks: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1 (untitled dropped): %+v", len(items), items)
	}
	it := items[0]
	if it.SourceID != "vol-1" || it.Title != "Book One" || it.Kind != "book" {
		t.Fatalf("item = %+v", it)
	}
	if it.Subtitle != "Auth One" {
		t.Fatalf("subtitle = %q", it.Subtitle)
	}
	if it.Year != 2026 {
		t.Fatalf("year = %d, want 2026", it.Year)
	}
	if it.PosterURL != "https://books.example/thumb1" {
		t.Fatalf("poster = %q, want the imageLinks thumbnail", it.PosterURL)
	}
	if got := rec.Query["q"][0]; got != gbDiscoverQuery {
		t.Fatalf("q = %q, want %q", got, gbDiscoverQuery)
	}
	if rec.Query["orderBy"][0] != "newest" {
		t.Fatalf("orderBy = %q, want newest", rec.Query["orderBy"][0])
	}
	if rec.Query["key"][0] != "test-key-123" {
		t.Fatalf("key = %q, want the API key", rec.Query["key"][0])
	}
}
