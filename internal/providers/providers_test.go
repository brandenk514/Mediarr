package providers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newStub builds an httptest server that records the request and replays
// body (a JSON string) with the given status.
func newStub(t *testing.T, status int, body string) (string, *recordedRequest) {
	t.Helper()
	rec := &recordedRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*rec = recordedRequest{
			Method: r.Method,
			Path:   r.URL.Path,
			Query:  r.URL.Query(),
			Header: r.Header.Clone(),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL, rec
}

type recordedRequest struct {
	Method string
	Path   string
	Query  map[string][]string
	Header http.Header
}

func TestTMDBSearchMovies(t *testing.T) {
	url, rec := newStub(t, http.StatusOK, `{
	  "results": [
	    {"id": 496243, "title": "Dune: Part Two", "release_date": "2024-03-01", "overview": "Fremen rise.", "vote_average": 8.4},
	    {"id": 123, "release_date": ""}
	  ]
	}`)
	m := NewTMDB("k-123")
	m.BaseURL = url
	res, err := m.SearchMovies(context.Background(), "dune")
	if err != nil {
		t.Fatalf("SearchMovies: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("expected 1 result (blank-title row skipped), got %d", len(res))
	}
	if res[0].ID != "496243" || res[0].Title != "Dune: Part Two" || res[0].Year != 2024 {
		t.Fatalf("unexpected result: %+v", res[0])
	}
	if res[0].Extra["overview"] != "Fremen rise." || res[0].Extra["rating"] != "8.4" {
		t.Fatalf("unexpected extra: %+v", res[0].Extra)
	}
	if rec.Method != http.MethodGet || rec.Path != "/search/movie" {
		t.Fatalf("request: %s %s", rec.Method, rec.Path)
	}
	if rec.Query["query"][0] != "dune" {
		t.Fatalf("query param: %v", rec.Query["query"])
	}
	if rec.Header.Get("X-Api-Key") != "k-123" {
		t.Fatalf("X-Api-Key: %q", rec.Header.Get("X-Api-Key"))
	}
}

func TestTMDBSearchTV(t *testing.T) {
	url, rec := newStub(t, http.StatusOK, `{
	  "results": [{"id": 81798, "name": "Breaking Bad", "first_air_date": "2008-01-20", "overview": "Walter."}]
	}`)
	m := NewTMDB("k")
	m.BaseURL = url
	res, err := m.SearchTV(context.Background(), "breaking")
	if err != nil {
		t.Fatalf("SearchTV: %v", err)
	}
	if len(res) != 1 || res[0].Title != "Breaking Bad" || res[0].Year != 2008 || res[0].ID != "81798" {
		t.Fatalf("unexpected: %+v", res)
	}
	if rec.Path != "/search/tv" {
		t.Fatalf("path: %s", rec.Path)
	}
}

func TestTMDBNotConfigured(t *testing.T) {
	m := NewTMDB("")
	if _, err := m.SearchMovies(context.Background(), "x"); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("want ErrNotConfigured, got %v", err)
	}
	if _, err := m.SearchTV(context.Background(), "x"); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("want ErrNotConfigured, got %v", err)
	}
}

func TestTMDBEmptyQuery(t *testing.T) {
	m := NewTMDB("k")
	res, err := m.SearchMovies(context.Background(), "  ")
	if err != nil || res != nil {
		t.Fatalf("want (nil, nil), got (%v, %v)", res, err)
	}
}

func TestTMDBBadResponse(t *testing.T) {
	url, _ := newStub(t, http.StatusUnauthorized, `{"status_message": "Invalid API key"}`)
	m := NewTMDB("bad")
	m.BaseURL = url
	if _, err := m.SearchMovies(context.Background(), "x"); !errors.Is(err, ErrBadResponse) {
		t.Fatalf("want ErrBadResponse, got %v", err)
	}
}

func TestTVDBSearchTwoStep(t *testing.T) {
	var loginSawKey, searchSawBearer string
	var loginCalls, searchCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			loginCalls++
			loginSawKey = r.Header.Get("X-Api-Key")
			if r.Method != http.MethodPost {
				t.Errorf("login method: %s", r.Method)
			}
			w.Write([]byte(`{"token":"jwt-abc"}`))
		case "/search/series":
			searchCalls++
			searchSawBearer = r.Header.Get("Authorization")
			if r.URL.Query().Get("query") != "severance" {
				t.Errorf("query: %v", r.URL.Query())
			}
			w.Write([]byte(`{"data":[{"id":378434,"seriesName":"Severance","firstAired":"2022-02-18","overview":"Adam."}]}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)

	tvd := NewTVDB("tvdb-key")
	tvd.BaseURL = srv.URL
	res, err := tvd.Search(context.Background(), "severance")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res) != 1 || res[0].Title != "Severance" || res[0].Year != 2022 || res[0].ID != "378434" {
		t.Fatalf("unexpected: %+v", res)
	}
	if loginCalls != 1 || searchCalls != 1 {
		t.Fatalf("calls: login=%d search=%d", loginCalls, searchCalls)
	}
	if loginSawKey != "tvdb-key" {
		t.Fatalf("login X-Api-Key: %q", loginSawKey)
	}
	if searchSawBearer != "Bearer jwt-abc" {
		t.Fatalf("search Authorization: %q", searchSawBearer)
	}
}

func TestTVDBNotConfigured(t *testing.T) {
	if _, err := NewTVDB("").Search(context.Background(), "x"); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("want ErrNotConfigured, got %v", err)
	}
}

func TestTVDBLoginFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"detail":"invalid key"}`))
	}))
	t.Cleanup(srv.Close)
	tvd := NewTVDB("nope")
	tvd.BaseURL = srv.URL
	if _, err := tvd.Search(context.Background(), "x"); !errors.Is(err, ErrBadResponse) {
		t.Fatalf("want ErrBadResponse, got %v", err)
	}
}

func TestMusicBrainzSearch(t *testing.T) {
	url, rec := newStub(t, http.StatusOK, `{
	  "artists": [
	    {"id": "d0d8af4e-6a71-4a33-9186-9d0df7cf4345", "name": "Miles Davis", "country": "United States", "begin-date": "1926-05-26"},
	    {"id": "x", "name": ""}
	  ]
	}`)
	mb := NewMusicBrainz()
	mb.BaseURL = url
	res, err := mb.Search(context.Background(), "miles davis")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("expected 1 (blank name skipped), got %d", len(res))
	}
	got := res[0]
	if got.ID != "d0d8af4e-6a71-4a33-9186-9d0df7cf4345" || got.Title != "Miles Davis" || got.Year != 1926 {
		t.Fatalf("unexpected: %+v", got)
	}
	if got.Extra["country"] != "United States" {
		t.Fatalf("extra: %+v", got.Extra)
	}
	if rec.Path != "/artist" || rec.Query["query"][0] != "artist:miles davis" || rec.Query["fmt"][0] != "json" {
		t.Fatalf("request: %s %v", rec.Path, rec.Query)
	}
	if ua := rec.Header.Get("User-Agent"); ua == "" {
		t.Fatal("User-Agent must be set (MusicBrainz requires a contact-bearing UA)")
	}
}

func TestMusicBrainzEmptyQuery(t *testing.T) {
	res, err := NewMusicBrainz().Search(context.Background(), "")
	if err != nil || res != nil {
		t.Fatalf("want (nil, nil), got (%v, %v)", res, err)
	}
}

func TestOpenLibrarySearchAuthorsDedupes(t *testing.T) {
	url, rec := newStub(t, http.StatusOK, `{
	  "numFound": 2,
	  "docs": [
	    {"key": "/works/OL1W", "title": "A", "author_name": ["Bob Author", "Bob Author"]},
	    {"key": "/works/OL2W", "title": "B", "author_name": ["Alice Writer"]}
	  ]
	}`)
	ol := NewOpenLibrary()
	ol.BaseURL = url
	res, err := ol.SearchAuthors(context.Background(), "bob")
	if err != nil {
		t.Fatalf("SearchAuthors: %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("expected 2 deduped authors, got %d: %+v", len(res), res)
	}
	if res[0].Title != "Bob Author" || res[0].ID != "Bob Author" {
		t.Fatalf("first author: %+v", res[0])
	}
	if rec.Path != "/search.json" || rec.Query["author"][0] != "bob" {
		t.Fatalf("request: %s %v", rec.Path, rec.Query)
	}
}

func TestOpenLibrarySearchWorks(t *testing.T) {
	url, rec := newStub(t, http.StatusOK, `{
	  "numFound": 1,
	  "docs": [
	    {"key": "/works/OL8189755W", "title": "Dune", "author_name": ["Frank Herbert"], "first_publish_year": 1965}
	  ]
	}`)
	ol := NewOpenLibrary()
	ol.BaseURL = url
	res, err := ol.SearchWorks(context.Background(), "Frank Herbert", "dune")
	if err != nil {
		t.Fatalf("SearchWorks: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("expected 1 work, got %d", len(res))
	}
	got := res[0]
	if got.ID != "OL8189755W" || got.Title != "Dune" || got.Year != 1965 {
		t.Fatalf("unexpected: %+v", got)
	}
	if got.Extra["author"] != "Frank Herbert" {
		t.Fatalf("extra: %+v", got.Extra)
	}
	if rec.Query["title"][0] != "dune" || rec.Query["author"][0] != "Frank Herbert" {
		t.Fatalf("query: %v", rec.Query)
	}
}

func TestBaseBadJSON(t *testing.T) {
	url, _ := newStub(t, http.StatusOK, `not-json`)
	mb := NewMusicBrainz()
	mb.BaseURL = url
	if _, err := mb.Search(context.Background(), "x"); !errors.Is(err, ErrBadResponse) {
		t.Fatalf("want ErrBadResponse, got %v", err)
	}
}
