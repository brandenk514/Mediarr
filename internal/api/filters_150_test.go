package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	moviesdom "github.com/brandenk514/mediarr/internal/domains/movies"
	musicdom "github.com/brandenk514/mediarr/internal/domains/music"
)

// Issue #150: the filter drawer's facets drive the list endpoints
// server-side via new query params, and malformed values are 400s
// (never silently ignored).

func TestListMovies_FacetParams(t *testing.T) {
	repo := newListMoviesRepo()
	repo.seed(moviesdom.Movie{Title: "Action One", Year: 2001, Monitored: true,
		Genres: []string{"Action"}, OriginalLanguage: strPtr("en"), Certification: strPtr("PG-13")})
	repo.seed(moviesdom.Movie{Title: "Drama Two", Year: 2002, Monitored: true,
		Genres: []string{"Drama"}, OriginalLanguage: strPtr("fr"), Certification: strPtr("R")})
	s, token := newListTestServer(t, repo)

	type page struct {
		Total int `json:"total"`
	}
	get := func(q string) int {
		t.Helper()
		rec := getJSON(t, s.Handler(), "/api/v1/movies"+q, token)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: status = %d, want 200", q, rec.Code)
		}
		var p page
		if err := json.NewDecoder(rec.Body).Decode(&p); err != nil {
			t.Fatal(err)
		}
		return p.Total
	}

	if got := get("?genre=action"); got != 1 {
		t.Errorf("genre=action total = %d, want 1", got)
	}
	if got := get("?genre=drama,action"); got != 2 {
		t.Errorf("genre=drama,action total = %d, want 2 (OR)", got)
	}
	if got := get("?lang=fr"); got != 1 {
		t.Errorf("lang=fr total = %d, want 1", got)
	}
	if got := get("?cert=pg-13"); got != 1 {
		t.Errorf("cert=pg-13 total = %d, want 1", got)
	}
	if got := get("?status=missing:monitored"); got != 2 {
		t.Errorf("status=missing:monitored total = %d, want 2", got)
	}
}

func TestListMovies_FacetParams400OnMalformed(t *testing.T) {
	repo := newListMoviesRepo()
	repo.seed(moviesdom.Movie{Title: "Any", Year: 2001, Monitored: true})
	s, token := newListTestServer(t, repo)

	for _, q := range []string{
		"?from=banana",     // not a date
		"?to=2020-13-99",   // not a real date
		"?runtime_min=abc", // not an int
		"?runtime_max=-x",  // not an int
		"?score_min=blah",  // not a float
		"?score_max=1.2.3", // not a float
	} {
		rec := getJSON(t, s.Handler(), "/api/v1/movies"+q, token)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("GET %s: status = %d, want 400", q, rec.Code)
		}
	}
}

func TestListMovies_ReleaseDateRange(t *testing.T) {
	repo := newListMoviesRepo()
	repo.seed(moviesdom.Movie{Title: "Old", Year: 1999, Monitored: true, ReleaseDate: "1999-05-01"})
	repo.seed(moviesdom.Movie{Title: "Recent", Year: 2023, Monitored: true, ReleaseDate: "2023-05-01"})
	s, token := newListTestServer(t, repo)

	type page struct {
		Total int `json:"total"`
	}
	get := func(q string) int {
		t.Helper()
		rec := getJSON(t, s.Handler(), "/api/v1/movies"+q, token)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: status = %d, want 200", q, rec.Code)
		}
		var p page
		if err := json.NewDecoder(rec.Body).Decode(&p); err != nil {
			t.Fatal(err)
		}
		return p.Total
	}
	if got := get("?from=2000-01-01"); got != 1 {
		t.Errorf("from 2000 total = %d, want 1 (Recent)", got)
	}
	if got := get("?to=2000-01-01"); got != 1 {
		t.Errorf("to 2000 total = %d, want 1 (Old)", got)
	}
	if got := get("?from=1990-01-01&to=2030-12-31"); got != 2 {
		t.Errorf("1990..2030 total = %d, want 2", got)
	}
}

func strPtr(s string) *string { return &s }

func TestListArtists_FacetParams(t *testing.T) {
	// The kindsServer fixture wires music; artists are seeded straight
	// into its in-memory repo.
	music := newKindsMusicRepo()
	f := newKindsServer(t, music, newKindsBooksRepo())
	ctx := context.Background()
	if _, err := music.CreateArtist(ctx, musicdom.Artist{Name: "Metal Band", Genre: "Metal", Status: "continuing"}); err != nil {
		t.Fatal(err)
	}
	if _, err := music.CreateArtist(ctx, musicdom.Artist{Name: "Jazz Trio", Genre: "Jazz", Status: "ended"}); err != nil {
		t.Fatal(err)
	}

	type page struct {
		Total int `json:"total"`
	}
	get := func(q string) int {
		t.Helper()
		rec := getJSON(t, f.s.Handler(), "/api/v1/music/artists"+q, f.token)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: status = %d, want 200", q, rec.Code)
		}
		var p page
		if err := json.NewDecoder(rec.Body).Decode(&p); err != nil {
			t.Fatal(err)
		}
		return p.Total
	}
	if got := get("?genre=metal"); got != 1 {
		t.Errorf("genre=metal total = %d, want 1", got)
	}
	if got := get("?status=ended"); got != 1 {
		t.Errorf("status=ended total = %d, want 1", got)
	}
	if got := get(""); got != 2 {
		t.Errorf("no filters total = %d, want 2", got)
	}
}
