package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/brandenk514/mediarr/internal/auth"
	moviesdom "github.com/brandenk514/mediarr/internal/domains/movies"
	"github.com/brandenk514/mediarr/internal/health"
	"github.com/brandenk514/mediarr/internal/providers"
	moviesvc "github.com/brandenk514/mediarr/internal/services/movies"
)

// fakeCollectionParter fakes the TMDB collection-parts surface (Radarr R3)
// for collections-endpoint tests: per-collection part lists, an optional
// injected error, and a call log.
type fakeCollectionParter struct {
	parts map[string][]providers.CollectionPart
	err   error
	calls []string
}

func (f *fakeCollectionParter) GetCollectionParts(ctx context.Context, id string) ([]providers.CollectionPart, error) {
	f.calls = append(f.calls, id)
	if f.err != nil {
		return nil, f.err
	}
	return f.parts[id], nil
}

// newCollectionsTestServer builds an authenticated movies server with a
// collection-parter wired into the service (Radarr R3). A nil parter models
// the keyless dev config.
func newCollectionsTestServer(t *testing.T, repo *listMoviesRepo, parter *fakeCollectionParter) (*Server, string) {
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
	if parter != nil {
		deps.CollectionParts = parter
	}
	svc := moviesvc.New(deps)
	s.SetMovies(&MoviesDeps{Svc: svc})
	return s, lr.Token
}

func ptrStr(s string) *string { return &s }

// TestListMovieCollections_Grouping covers the R3 collections endpoint
// grouping rules: id grouping, name fallback (id "unknown"), ungrouped
// exclusion, name sorting, and best-effort missing counts.
func TestListMovieCollections_Grouping(t *testing.T) {
	repo := newListMoviesRepo()
	repo.seed(moviesdom.Movie{
		Title: "Dune", Year: 2021, CollectionID: "4468",
		CollectionName: ptrStr("Dune Saga"),
	})
	repo.seed(moviesdom.Movie{
		Title: "Dune: Part Two", Year: 2024, CollectionID: "4468",
		CollectionName: ptrStr("Dune Saga"),
	})
	// Name only (no TMDB id) → name-fallback group with id "unknown".
	repo.seed(moviesdom.Movie{
		Title: "The Batman", Year: 2022,
		CollectionName: ptrStr("DC Extended Universe"),
	})
	// Ungrouped → skipped entirely.
	repo.seed(moviesdom.Movie{Title: "Parasite", Year: 2019})

	parter := &fakeCollectionParter{parts: map[string][]providers.CollectionPart{
		"4468": {
			{ID: "491299", Title: "Dune", Year: 2021},
			{ID: "593500", Title: "Dune: Part Two", Year: 2024},
			{ID: "999999", Title: "Dune: Part Three", Year: 2026}, // not in library
		},
	}}
	s, token := newCollectionsTestServer(t, repo, parter)

	rec := getJSON(t, s.Handler(), "/api/v1/movies/collections", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var cols []movieCollection
	if err := json.Unmarshal(rec.Body.Bytes(), &cols); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(cols) != 2 {
		t.Fatalf("len = %d, want 2 (ungrouped skipped): %s", len(cols), rec.Body.String())
	}
	// Sorted by name: "DC Extended Universe" < "Dune Saga".
	if cols[0].ID != "unknown" || cols[0].Name != "DC Extended Universe" {
		t.Errorf("cols[0] = %+v, want unknown/DC Extended Universe", cols[0])
	}
	if cols[0].MovieCount != 1 || cols[0].MissingCount != 0 {
		t.Errorf("cols[0] counts = %d/%d, want 1/0", cols[0].MovieCount, cols[0].MissingCount)
	}
	if cols[1].ID != "4468" || cols[1].Name != "Dune Saga" {
		t.Errorf("cols[1] = %+v, want 4468/Dune Saga", cols[1])
	}
	if cols[1].MovieCount != 2 {
		t.Errorf("dune movie_count = %d, want 2", cols[1].MovieCount)
	}
	if cols[1].MissingCount != 1 {
		t.Errorf("dune missing_count = %d, want 1 (Part Three not in library)", cols[1].MissingCount)
	}
	// Members are full list-read objects: editions present.
	if len(cols[1].Movies) != 2 || len(cols[1].Movies[0].Editions) == 0 {
		t.Errorf("members missing editions: %+v", cols[1].Movies)
	}
	// Best-effort: only the known collection id was fetched.
	if len(parter.calls) != 1 || parter.calls[0] != "4468" {
		t.Errorf("parts calls = %v, want [4468]", parter.calls)
	}
}

// TestListMovieCollections_MissingCount_TMDBFailure: a parts fetch failure
// degrades missing_count to 0 — never an endpoint error.
func TestListMovieCollections_MissingCount_TMDBFailure(t *testing.T) {
	repo := newListMoviesRepo()
	repo.seed(moviesdom.Movie{
		Title: "Dune", Year: 2021, CollectionID: "4468",
		CollectionName: ptrStr("Dune Saga"),
	})
	parter := &fakeCollectionParter{err: context.DeadlineExceeded}
	s, token := newCollectionsTestServer(t, repo, parter)

	rec := getJSON(t, s.Handler(), "/api/v1/movies/collections", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", rec.Code, rec.Body.String())
	}
	var cols []movieCollection
	if err := json.Unmarshal(rec.Body.Bytes(), &cols); err != nil {
		t.Fatal(err)
	}
	if len(cols) != 1 || cols[0].MissingCount != 0 {
		t.Fatalf("cols = %+v, want 1 collection with missing_count 0", cols)
	}
}

// TestListMovieCollections_Keyless: a nil parter (no TMDB key) still works —
// groups form, missing counts stay 0.
func TestListMovieCollections_Keyless(t *testing.T) {
	repo := newListMoviesRepo()
	repo.seed(moviesdom.Movie{
		Title: "Dune", Year: 2021, CollectionID: "4468",
		CollectionName: ptrStr("Dune Saga"),
	})
	repo.seed(moviesdom.Movie{
		Title: "Dune: Part Two", Year: 2024,
		CollectionName: ptrStr("Dune Saga"), // name only
	})
	s, token := newCollectionsTestServer(t, repo, nil)

	rec := getJSON(t, s.Handler(), "/api/v1/movies/collections", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var cols []movieCollection
	if err := json.Unmarshal(rec.Body.Bytes(), &cols); err != nil {
		t.Fatal(err)
	}
	// id group + name group keep separate groups (no shared id).
	if len(cols) != 2 {
		t.Fatalf("len = %d, want 2", len(cols))
	}
	for _, c := range cols {
		if c.MissingCount != 0 {
			t.Errorf("%s missing_count = %d, want 0 (keyless)", c.ID, c.MissingCount)
		}
	}
}

// TestListMovieCollections_Empty: an empty library returns an empty array,
// not null.
func TestListMovieCollections_Empty(t *testing.T) {
	repo := newListMoviesRepo()
	s, token := newCollectionsTestServer(t, repo, nil)

	rec := getJSON(t, s.Handler(), "/api/v1/movies/collections", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); body != "[]\n" {
		t.Fatalf("body = %q, want []", body)
	}
}

// TestListMovieCollections_Auth: unauthenticated requests are rejected.
func TestListMovieCollections_Auth(t *testing.T) {
	repo := newListMoviesRepo()
	s, _ := newCollectionsTestServer(t, repo, nil)

	rec := getJSON(t, s.Handler(), "/api/v1/movies/collections", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
