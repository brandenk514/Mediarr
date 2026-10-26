package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/brandenk514/mediarr/internal/auth"
	"github.com/brandenk514/mediarr/internal/health"
	"github.com/brandenk514/mediarr/internal/providers"
)

// newSearchTestServer returns an authenticated server with SearchDeps wired
// to the given adapters (nil adapter = not configured) plus a bearer token.
func newSearchTestServer(t *testing.T, deps *SearchDeps) (*Server, string) {
	t.Helper()
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
		t.Fatalf("login status = %d body=%s", rec.Code, rec.Body.String())
	}
	var lr loginResponse
	if err := json.NewDecoder(rec.Body).Decode(&lr); err != nil {
		t.Fatal(err)
	}
	s.SetSearch(deps)
	return s, lr.Token
}

// stubProvider is a configurable provider stub: the test sets base (the
// httptest URL), a canned JSON body, and an optional error status.
func stubProvider(t *testing.T, body string, status int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status != 0 {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":"boom"}`))
			return
		}
		_, _ = w.Write([]byte(body))
	}))
}

func TestSearchMovies_HappyPath(t *testing.T) {
	// The stub answers every path (search, genre list, movie detail) with
	// the same search payload; the detail parse then yields no badges, so
	// the hit keeps its search-payload fields and empty badge fields.
	body := `{"results":[{"id":603,"title":"Dune: Part Two","release_date":"2024-03-01","overview":"Fremen.","vote_average":7.8,"vote_count":1234,"poster_path":"/abc.jpg","original_language":"en","genre_ids":[879]}]}`
	stub := stubProvider(t, body, 0)
	defer stub.Close()
	deps := &SearchDeps{TMDB: providers.NewTMDB("k")}
	deps.TMDB.BaseURL = stub.URL
	s, token := newSearchTestServer(t, deps)

	rec := getJSON(t, s.Handler(), "/api/v1/search/movies?query=dune", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var got []movieSearchHit
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 hit, got %d", len(got))
	}
	h := got[0]
	if h.ID != "603" || h.Title != "Dune: Part Two" || h.Year != 2024 {
		t.Fatalf("unexpected hit: %+v", h)
	}
	if h.Overview != "Fremen." || h.Poster != "https://image.tmdb.org/t/p/w342/abc.jpg" || h.OriginalLanguage != "en" {
		t.Fatalf("search-payload fields = %+v", h)
	}
	if h.TmdbPct == nil || *h.TmdbPct != 780 {
		t.Fatalf("tmdb_pct = %v, want 780", h.TmdbPct)
	}
	if h.Links.TMDB != "https://www.themoviedb.org/movie/603" {
		t.Fatalf("links.tmdb = %q", h.Links.TMDB)
	}
	if h.Genres == nil {
		t.Fatal("genres must be a non-nil []")
	}
}

func TestSearchMovies_NotConfigured(t *testing.T) {
	s, token := newSearchTestServer(t, &SearchDeps{})
	rec := getJSON(t, s.Handler(), "/api/v1/search/movies?query=dune", token)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "not configured") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestSearchMovies_MissingQuery(t *testing.T) {
	body := `{"results":[]}`
	stub := stubProvider(t, body, 0)
	defer stub.Close()
	deps := &SearchDeps{TMDB: providers.NewTMDB("k")}
	deps.TMDB.BaseURL = stub.URL
	s, token := newSearchTestServer(t, deps)

	// No query param: the adapter returns empty (it treats blank q as
	// nothing to search), so the endpoint should be 200 with [].
	rec := getJSON(t, s.Handler(), "/api/v1/search/movies", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var got []movieSearchHit
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("want empty, got %v", got)
	}
}

func TestSearchTV_HappyPath(t *testing.T) {
	// TVDB requires the two-step login + search, so this stub serves both.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			if r.Header.Get("X-Api-Key") != "k" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"token":"jwt-abc"}`))
		case "/search/series":
			if r.Header.Get("Authorization") != "Bearer jwt-abc" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"data":[{"id":82350,"seriesName":"Breaking Bad","firstAired":"2008-01-20","overview":"Chem."}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	deps := &SearchDeps{TVDB: providers.NewTVDB("k")}
	deps.TVDB.BaseURL = srv.URL
	s, token := newSearchTestServer(t, deps)

	rec := getJSON(t, s.Handler(), "/api/v1/search/tv?query=breaking", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var got []searchResult
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "82350" || got[0].Title != "Breaking Bad" || got[0].Year != 2008 {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestSearchTV_NotConfigured(t *testing.T) {
	s, token := newSearchTestServer(t, &SearchDeps{})
	rec := getJSON(t, s.Handler(), "/api/v1/search/tv?query=x", token)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestSearchTVTMDB_HappyPath(t *testing.T) {
	body := `{"results":[{"id":1396,"name":"Breaking Bad","first_air_date":"2008-01-20","overview":"x"}]}`
	stub := stubProvider(t, body, 0)
	defer stub.Close()
	deps := &SearchDeps{TMDB: providers.NewTMDB("k")}
	deps.TMDB.BaseURL = stub.URL
	s, token := newSearchTestServer(t, deps)

	rec := getJSON(t, s.Handler(), "/api/v1/search/tv-tmdb?query=breaking", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var got []searchResult
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "1396" || got[0].Title != "Breaking Bad" {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestSearchArtists_HappyPath(t *testing.T) {
	body := `{"artists":[{"id":"52615704-c35c-475d-bcd0-3b4ba1a4e151","name":"Radiohead","begin-date":"1985-02-01","country":"UK"}]}`
	stub := stubProvider(t, body, 0)
	defer stub.Close()
	deps := &SearchDeps{MusicBrainz: providers.NewMusicBrainz()}
	deps.MusicBrainz.BaseURL = stub.URL
	s, token := newSearchTestServer(t, deps)

	rec := getJSON(t, s.Handler(), "/api/v1/search/artists?query=radiohead", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var got []searchResult
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "Radiohead" || got[0].Year != 1985 {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestSearchBookAuthors_HappyPath(t *testing.T) {
	body := `{"numFound":1,"docs":[{"key":"/works/OL8189755W","title":"The Dispossessed","author_name":["Ursula K. Le Guin"],"first_publish_year":1974}]}`
	stub := stubProvider(t, body, 0)
	defer stub.Close()
	deps := &SearchDeps{OpenLibrary: providers.NewOpenLibrary()}
	deps.OpenLibrary.BaseURL = stub.URL
	s, token := newSearchTestServer(t, deps)

	rec := getJSON(t, s.Handler(), "/api/v1/search/books/authors?query=le+guin", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var got []searchResult
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "Ursula K. Le Guin" {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestSearchBookWorks_HappyPath(t *testing.T) {
	body := `{"numFound":1,"docs":[{"key":"/works/OL8189755W","title":"The Dispossessed","author_name":["Ursula K. Le Guin"],"first_publish_year":1974}]}`
	stub := stubProvider(t, body, 0)
	defer stub.Close()
	deps := &SearchDeps{OpenLibrary: providers.NewOpenLibrary()}
	deps.OpenLibrary.BaseURL = stub.URL
	s, token := newSearchTestServer(t, deps)

	rec := getJSON(t, s.Handler(), "/api/v1/search/books/works?query=dispossessed&author=le+guin", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var got []searchResult
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "OL8189755W" || got[0].Title != "The Dispossessed" || got[0].Year != 1974 {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestProfiles_AllKinds(t *testing.T) {
	s, token := newSearchTestServer(t, &SearchDeps{})
	for kind, wantDefault := range map[string]string{
		"movies": "HD-1080p",
		"tv":     "HD-1080p",
		"music":  "Lossless",
		"books":  "Best",
	} {
		rec := getJSON(t, s.Handler(), "/api/v1/profiles?kind="+kind, token)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d body=%s", kind, rec.Code, rec.Body.String())
		}
		var got profileResponse
		if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		if got.Default != wantDefault {
			t.Fatalf("%s: default = %q, want %q", kind, got.Default, wantDefault)
		}
		if len(got.Profiles) == 0 {
			t.Fatalf("%s: no profiles", kind)
		}
	}
}

func TestProfiles_InvalidKind(t *testing.T) {
	s, token := newSearchTestServer(t, &SearchDeps{})
	rec := getJSON(t, s.Handler(), "/api/v1/profiles?kind=nonsense", token)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// TestSearchMovies_BadResponse confirms a non-2xx upstream maps to 502.
func TestSearchMovies_BadResponse(t *testing.T) {
	stub := stubProvider(t, `{"error":"rate limited"}`, http.StatusTooManyRequests)
	defer stub.Close()
	deps := &SearchDeps{TMDB: providers.NewTMDB("k")}
	deps.TMDB.BaseURL = stub.URL
	s, token := newSearchTestServer(t, deps)

	rec := getJSON(t, s.Handler(), "/api/v1/search/movies?query=dune", token)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body=%s", rec.Code, rec.Body.String())
	}
}

// TestSearchRoutesRequireAuth confirms the search routes are behind auth.
func TestSearchRoutesRequireAuth(t *testing.T) {
	s, _ := newSearchTestServer(t, &SearchDeps{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search/movies?query=x", nil)
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
