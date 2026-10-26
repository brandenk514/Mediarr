package providers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// newTMDBRouter builds a stub TMDB server dispatching on path prefix and
// recording every path it serves (Radarr R3 tests drive several endpoints
// in one call: genres + trending + details).
func newTMDBRouter(t *testing.T, routes map[string]http.HandlerFunc) (*TMDB, *[]string) {
	t.Helper()
	var mu sync.Mutex
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		for prefix, h := range routes {
			if len(r.URL.Path) >= len(prefix) && r.URL.Path[:len(prefix)] == prefix {
				h(w, r)
				return
			}
		}
		t.Errorf("stub: no route for %s", r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	m := NewTMDB("k-test")
	m.BaseURL = srv.URL
	return m, &paths
}

func jsonRoute(t *testing.T, status int, body string) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

// TestTMDBDiscoverMovieHits_Filters exercises the R3 Discover feed end to
// end against a stub: trending base URL, page/genre/year query mapping,
// total passthrough, badge enrichment, and per-hit degradation.
func TestTMDBDiscoverMovieHits_Filters(t *testing.T) {
	routes := map[string]http.HandlerFunc{
		"/genre/movie/list": jsonRoute(t, http.StatusOK, `{
			"genres": [{"id": 28, "name": "Action"}, {"id": 12, "name": "Adventure"}]
		}`),
		"/trending/movie/week": jsonRoute(t, http.StatusOK, `{
			"results": [
				{"id": 1, "title": "Alpha", "release_date": "2024-05-01", "genre_ids": [28], "vote_average": 8.0},
				{"id": 2, "title": "Beta", "release_date": "2023-05-01", "genre_ids": [12]}
			],
			"total_results": 42
		}`),
		"/movie/1": jsonRoute(t, http.StatusOK, `{
			"runtime": 140,
			"production_companies": [{"name": "Studio One"}],
			"release_dates": {"results": [{"iso_3166_1": "US", "release_dates": [{"certification": "PG-13"}]}]}
		}`),
		"/movie/2": jsonRoute(t, http.StatusNotFound, `{"status_message": "Not found"}`),
	}
	m, paths := newTMDBRouter(t, routes)

	hits, total, err := m.DiscoverMovieHits(context.Background(), DiscoverQuery{Page: 3, Genre: "Action", Year: 2024})
	if err != nil {
		t.Fatalf("DiscoverMovieHits: %v", err)
	}
	if total != 42 {
		t.Fatalf("total = %d, want 42", total)
	}
	if len(hits) != 2 {
		t.Fatalf("len(hits) = %d, want 2", len(hits))
	}
	// Enriched hit (detail succeeded).
	if hits[0].Title != "Alpha" || hits[0].Runtime != 140 || hits[0].Studio != "Studio One" || hits[0].Certification != "PG-13" {
		t.Fatalf("hit 0 badges: %+v", hits[0])
	}
	if len(hits[0].Genres) != 1 || hits[0].Genres[0] != "Action" {
		t.Fatalf("hit 0 genres: %v", hits[0].Genres)
	}
	// Degraded hit (detail 404): badge fields empty, no error.
	if hits[1].Title != "Beta" || hits[1].Runtime != 0 || hits[1].Studio != "" || hits[1].Certification != "" {
		t.Fatalf("hit 1 must degrade, got %+v", hits[1])
	}
	if len(hits[1].Genres) != 1 || hits[1].Genres[0] != "Adventure" {
		t.Fatalf("hit 1 genres: %v (id-mapped names survive detail failure)", hits[1].Genres)
	}
	if len(*paths) == 0 {
		t.Fatal("no requests recorded")
	}
}

// TestTMDBDiscoverMovieHits_QueryMapping re-checks the exact query params the
// provider puts on /trending/movie/week.
func TestTMDBDiscoverMovieHits_QueryMapping(t *testing.T) {
	var q map[string][]string
	routes := map[string]http.HandlerFunc{
		"/genre/movie/list": jsonRoute(t, http.StatusOK, `{"genres": [{"id": 28, "name": "Action"}]}`),
		"/trending/movie/week": func(w http.ResponseWriter, r *http.Request) {
			q = r.URL.Query()
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"results": [], "total_results": 0}`))
		},
	}
	m, _ := newTMDBRouter(t, routes)

	_, _, err := m.DiscoverMovieHits(context.Background(), DiscoverQuery{Page: 0, Genre: "Action", Year: 1999})
	if err != nil {
		t.Fatalf("DiscoverMovieHits: %v", err)
	}
	if q["page"][0] != "1" {
		t.Errorf("page = %q, want 1 (clamped)", q["page"][0])
	}
	if q["with_genres"][0] != "28" {
		t.Errorf("with_genres = %v, want 28", q["with_genres"])
	}
	if q["primary_release_date.gte"][0] != "1999-01-01" || q["primary_release_date.lte"][0] != "1999-12-31" {
		t.Errorf("year window = %v/%v", q["primary_release_date.gte"], q["primary_release_date.lte"])
	}
	if q["include_adult"][0] != "false" {
		t.Errorf("include_adult = %v", q["include_adult"])
	}
}

// TestTMDBDiscoverMovieHits_UnknownGenre: a genre name TMDB doesn't know is
// a caller error (the API maps it to 400), not a page failure.
func TestTMDBDiscoverMovieHits_UnknownGenre(t *testing.T) {
	routes := map[string]http.HandlerFunc{
		"/genre/movie/list": jsonRoute(t, http.StatusOK, `{"genres": [{"id": 28, "name": "Action"}]}`),
	}
	m, _ := newTMDBRouter(t, routes)
	_, _, err := m.DiscoverMovieHits(context.Background(), DiscoverQuery{Genre: "NotAGenre"})
	if !errors.Is(err, ErrUnknownGenre) {
		t.Fatalf("err = %v, want ErrUnknownGenre", err)
	}
}

// TestTMDBDiscoverMovieHits_NotConfigured: keyless dev returns the
// not-configured sentinel (the API maps it to 503).
func TestTMDBDiscoverMovieHits_NotConfigured(t *testing.T) {
	m := NewTMDB("")
	if _, _, err := m.DiscoverMovieHits(context.Background(), DiscoverQuery{}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("err = %v, want ErrNotConfigured", err)
	}
}

// TestTMDBGetCollectionParts: the R3 missing-count source.
func TestTMDBGetCollectionParts(t *testing.T) {
	routes := map[string]http.HandlerFunc{
		"/collection/4468": jsonRoute(t, http.StatusOK, `{
			"parts": [
				{"id": 491299, "title": "Dune", "release_date": "2021-09-15"},
				{"id": 593500, "title": "Dune: Part Two", "release_date": ""}
			]
		}`),
		"/collection/9999": jsonRoute(t, http.StatusNotFound, `{"status_message": "Not found"}`),
	}
	m, _ := newTMDBRouter(t, routes)

	parts, err := m.GetCollectionParts(context.Background(), "4468")
	if err != nil {
		t.Fatalf("GetCollectionParts: %v", err)
	}
	if len(parts) != 2 {
		t.Fatalf("len = %d, want 2", len(parts))
	}
	if parts[0].ID != "491299" || parts[0].Title != "Dune" || parts[0].Year != 2021 {
		t.Fatalf("part 0: %+v", parts[0])
	}
	if parts[1].Year != 0 {
		t.Fatalf("part 1 year = %d, want 0 (unknown date)", parts[1].Year)
	}

	if _, err := m.GetCollectionParts(context.Background(), "9999"); err == nil {
		t.Fatal("404 collection must error")
	}
	if _, err := m.GetCollectionParts(context.Background(), "  "); err == nil {
		t.Fatal("blank id must error")
	}
}

// TestTMDBGetCollectionParts_NotConfigured: keyless dev → sentinel (the
// service treats it as "no data": missing_count 0).
func TestTMDBGetCollectionParts_NotConfigured(t *testing.T) {
	m := NewTMDB("")
	if _, err := m.GetCollectionParts(context.Background(), "1"); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("err = %v, want ErrNotConfigured", err)
	}
}
