package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/brandenk514/mediarr/internal/providers"
)

// Tests for the Radarr R2 movie search: detail badge enrichment and the
// tmdb:<id> direct-lookup query form, against a fake TMDB (httptest).

// tmdbStub serves a path→handler map so one stub can answer /search/movie,
// /genre/movie/list and /movie/{id} differently.
func tmdbStub(t *testing.T, handlers map[string]http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h, ok := handlers[r.URL.Path]; ok {
			h(w, r)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
}

func tmdbDeps(base string) *SearchDeps {
	deps := &SearchDeps{TMDB: providers.NewTMDB("k")}
	deps.TMDB.BaseURL = base
	return deps
}

func decodeMovieHits(t *testing.T, rec *httptest.ResponseRecorder) []movieSearchHit {
	t.Helper()
	var got []movieSearchHit
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("unmarshal hits: %v", err)
	}
	return got
}

// TestSearchMovies_EnrichmentBadges asserts a plain search enriches the top
// hits with detail badges (runtime, studio, imdb, US certification, genre
// names) and that a detail failure degrades that hit to empty badge fields
// instead of failing the search.
func TestSearchMovies_EnrichmentBadges(t *testing.T) {
	stub := tmdbStub(t, map[string]http.HandlerFunc{
		"/search/movie": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"results":[
				{"id":603,"title":"Dune: Part Two","release_date":"2024-03-01","overview":"Fremen.","vote_average":7.8,"vote_count":1234,"poster_path":"/a.jpg","original_language":"en","genre_ids":[879]},
				{"id":604,"title":"Other Movie","release_date":"2023-05-02","overview":"Other.","vote_average":0,"poster_path":"/b.jpg"}
			]}`))
		},
		"/genre/movie/list": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"genres":[{"id":879,"name":"Science Fiction"},{"id":18,"name":"Drama"}]}`))
		},
		"/movie/603": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"title":"Dune: Part Two","runtime":166,"original_language":"en",
				"vote_average":7.8,"vote_count":1234,"release_date":"2024-03-01","imdb_id":"tt15239678",
				"production_companies":[{"name":"Legendary"}],
				"genres":[{"name":"Science Fiction"},{"name":"Action"}],
				"release_dates":{"results":[{"iso_3166_1":"US","release_dates":[{"certification":"PG-13"}]}]}}`))
		},
		"/movie/604": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"boom"}`))
		},
	})
	defer stub.Close()
	s, token := newSearchTestServer(t, tmdbDeps(stub.URL))

	rec := getJSON(t, s.Handler(), "/api/v1/search/movies?query=dune", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s (a detail failure must not fail the search)", rec.Code, rec.Body.String())
	}
	got := decodeMovieHits(t, rec)
	if len(got) != 2 {
		t.Fatalf("want 2 hits, got %d", len(got))
	}
	h := got[0]
	if h.ID != "603" || h.Title != "Dune: Part Two" || h.Year != 2024 {
		t.Fatalf("hit 1 = %+v", h)
	}
	if h.Runtime != 166 || h.Certification != "PG-13" || h.Studio != "Legendary" || h.Imdb != "https://www.imdb.com/title/tt15239678" {
		t.Fatalf("detail badges = %+v", h)
	}
	if len(h.Genres) != 2 || h.Genres[0] != "Science Fiction" || h.Genres[1] != "Action" {
		t.Fatalf("genres = %v, want the detail's genre names", h.Genres)
	}
	if h.TmdbPct == nil || *h.TmdbPct != 780 {
		t.Fatalf("tmdb_pct = %v, want 780", h.TmdbPct)
	}

	// The second hit's detail fetch failed: search-payload fields stay,
	// detail badges degrade to empty.
	d := got[1]
	if d.ID != "604" || d.Title != "Other Movie" || d.Year != 2023 || d.Overview != "Other." {
		t.Fatalf("hit 2 search-payload = %+v", d)
	}
	if d.Runtime != 0 || d.Certification != "" || d.Studio != "" || d.Imdb != "" {
		t.Fatalf("hit 2 detail badges should degrade to empty, got %+v", d)
	}
	if d.TmdbPct != nil {
		t.Fatalf("hit 2 tmdb_pct = %v, want null (no vote)", *d.TmdbPct)
	}
}

// TestSearchMovies_TMDBDirectID asserts the tmdb:<id> query form returns one
// fully-enriched hit from /movie/{id} (no /search/movie call).
func TestSearchMovies_TMDBDirectID(t *testing.T) {
	var sawSearch bool
	stub := tmdbStub(t, map[string]http.HandlerFunc{
		"/search/movie": func(w http.ResponseWriter, r *http.Request) {
			sawSearch = true
			w.WriteHeader(http.StatusNotFound)
		},
		"/movie/603": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"title":"Dune: Part Two","runtime":166,"original_language":"en",
				"vote_average":7.8,"vote_count":1234,"release_date":"2024-03-01","imdb_id":"tt15239678",
				"production_companies":[{"name":"Legendary"}],
				"genres":[{"name":"Science Fiction"}],
				"release_dates":{"results":[{"iso_3166_1":"US","release_dates":[{"certification":"PG-13"}]}]}}`))
		},
	})
	defer stub.Close()
	s, token := newSearchTestServer(t, tmdbDeps(stub.URL))

	rec := getJSON(t, s.Handler(), "/api/v1/search/movies?query=tmdb:603", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	got := decodeMovieHits(t, rec)
	if len(got) != 1 {
		t.Fatalf("want 1 hit, got %d", len(got))
	}
	h := got[0]
	if h.ID != "603" || h.Title != "Dune: Part Two" || h.Year != 2024 {
		t.Fatalf("hit = %+v", h)
	}
	if h.Runtime != 166 || h.Certification != "PG-13" || h.Studio != "Legendary" || h.Imdb != "https://www.imdb.com/title/tt15239678" {
		t.Fatalf("badges = %+v", h)
	}
	if sawSearch {
		t.Fatal("tmdb:<id> must not hit /search/movie")
	}
}

// TestSearchMovies_TMDBDirectID_Unknown asserts an id TMDB does not know
// maps to 502 (bad upstream answer), not a silent empty list.
func TestSearchMovies_TMDBDirectID_Unknown(t *testing.T) {
	stub := tmdbStub(t, map[string]http.HandlerFunc{
		"/movie/999": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"status":404,"status_message":"The resource you requested could not be found."}`))
		},
	})
	defer stub.Close()
	s, token := newSearchTestServer(t, tmdbDeps(stub.URL))

	rec := getJSON(t, s.Handler(), "/api/v1/search/movies?query=tmdb:999", token)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 (body=%s)", rec.Code, rec.Body.String())
	}
}

// TestSearchMovies_TMDBDirectID_Empty asserts a bare tmdb: prefix is a 400.
func TestSearchMovies_TMDBDirectID_Empty(t *testing.T) {
	stub := tmdbStub(t, nil)
	defer stub.Close()
	s, token := newSearchTestServer(t, tmdbDeps(stub.URL))

	rec := getJSON(t, s.Handler(), "/api/v1/search/movies?query=tmdb:", token)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body=%s)", rec.Code, rec.Body.String())
	}
}

// TestSearchMovies_TMDBDirectID_NotConfigured asserts the keyless 503
// behavior holds for the direct-lookup form too.
func TestSearchMovies_TMDBDirectID_NotConfigured(t *testing.T) {
	s, token := newSearchTestServer(t, &SearchDeps{})
	rec := getJSON(t, s.Handler(), "/api/v1/search/movies?query=tmdb:603", token)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "not configured") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}
