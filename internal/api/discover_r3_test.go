package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/brandenk514/mediarr/internal/auth"
	"github.com/brandenk514/mediarr/internal/health"
	"github.com/brandenk514/mediarr/internal/providers"
	moviesvc "github.com/brandenk514/mediarr/internal/services/movies"
)

// fakeDiscoverer fakes the TMDB trending-feed surface (Radarr R3) for
// discover-endpoint tests: a canned hit list, total, optional error, and the
// last query the handler sent down.
type fakeDiscoverer struct {
	hits  []providers.MovieSearchHit
	total int
	err   error
	last  providers.DiscoverQuery
}

func (f *fakeDiscoverer) DiscoverMovieHits(ctx context.Context, q providers.DiscoverQuery) ([]providers.MovieSearchHit, int, error) {
	f.last = q
	if f.err != nil {
		return nil, 0, f.err
	}
	return f.hits, f.total, nil
}

// newDiscoverTestServer builds an authenticated movies server with the R3
// Discover route wired. A nil discoverer models keyless dev (no TMDB key).
func newDiscoverTestServer(t *testing.T, repo *listMoviesRepo, disc moviesvc.TMDBDiscoverer) (*Server, string) {
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
	deps := moviesvc.Deps{Repo: repo, DefaultProfile: "HD-1080p"}
	if disc != nil {
		deps.TMDB = disc
	}
	svc := moviesvc.New(deps)
	s.SetMovies(&MoviesDeps{Svc: svc})
	s.SetDiscover(&DiscoverDeps{Svc: svc})
	return s, lr.Token
}

func sampleHits() []providers.MovieSearchHit {
	return []providers.MovieSearchHit{
		{ID: "1", Title: "Alpha", Year: 2024, Runtime: 140, Studio: "Studio One", Genres: []string{"Action"}, VoteAverage: 8.0},
		{ID: "2", Title: "Beta", Year: 2023, Genres: []string{"Drama"}},
	}
}

// TestDiscoverMovies_Filters: page/genre/year reach the provider, and the
// response is {results, page, total} with the hits in /search/movies wire
// shape.
func TestDiscoverMovies_Filters(t *testing.T) {
	disc := &fakeDiscoverer{hits: sampleHits(), total: 57}
	s, token := newDiscoverTestServer(t, newListMoviesRepo(), disc)

	rec := getJSON(t, s.Handler(), "/api/v1/discover/movies?page=2&genre=Action&year=2024", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp discoverMoviesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Results) != 2 || resp.Page != 2 || resp.Total != 57 {
		t.Fatalf("resp = %+v", resp)
	}
	if resp.Results[0].ID != "1" || resp.Results[0].Runtime != 140 || resp.Results[0].Studio != "Studio One" {
		t.Fatalf("hit 0 wire shape: %+v", resp.Results[0])
	}
	if resp.Results[0].TmdbPct == nil || *resp.Results[0].TmdbPct != 800 {
		t.Fatalf("hit 0 tmdb_pct = %v, want 800", resp.Results[0].TmdbPct)
	}
	if disc.last.Page != 2 || disc.last.Genre != "Action" || disc.last.Year != 2024 {
		t.Fatalf("provider query = %+v", disc.last)
	}
}

// TestDiscoverMovies_Defaults: no query params → page 1; a nil hit list
// serializes as [] (never null).
func TestDiscoverMovies_Defaults(t *testing.T) {
	disc := &fakeDiscoverer{}
	s, token := newDiscoverTestServer(t, newListMoviesRepo(), disc)

	rec := getJSON(t, s.Handler(), "/api/v1/discover/movies", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp discoverMoviesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Page != 1 || resp.Total != 0 {
		t.Fatalf("page/total = %d/%d, want 1/0", resp.Page, resp.Total)
	}
	if len(resp.Results) != 0 {
		t.Fatalf("results = %v, want empty", resp.Results)
	}
	if disc.last.Page != 1 {
		t.Fatalf("provider page = %d, want 1", disc.last.Page)
	}
}

// TestDiscoverMovies_Keyless: no TMDB key → 503 with a reason (the UI
// greys the page out with it).
func TestDiscoverMovies_Keyless(t *testing.T) {
	s, token := newDiscoverTestServer(t, newListMoviesRepo(), nil)

	rec := getJSON(t, s.Handler(), "/api/v1/discover/movies", token)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body=%s, want 503", rec.Code, rec.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["error"] == "" {
		t.Fatalf("body = %q, want an error reason", rec.Body.String())
	}
}

// TestDiscoverMovies_UnknownGenre: a genre name TMDB doesn't know is a 400
// (caller error), not a page failure.
func TestDiscoverMovies_UnknownGenre(t *testing.T) {
	disc := &fakeDiscoverer{err: providers.ErrUnknownGenre}
	s, token := newDiscoverTestServer(t, newListMoviesRepo(), disc)

	rec := getJSON(t, s.Handler(), "/api/v1/discover/movies?genre=NotAGenre", token)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
	}
}

// TestDiscoverMovies_Auth: unauthenticated requests are rejected.
func TestDiscoverMovies_Auth(t *testing.T) {
	s, _ := newDiscoverTestServer(t, newListMoviesRepo(), nil)

	rec := getJSON(t, s.Handler(), "/api/v1/discover/movies", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
