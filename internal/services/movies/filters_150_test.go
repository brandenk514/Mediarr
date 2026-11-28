package movies

import (
	"context"
	"testing"

	dom "github.com/brandenk514/mediarr/internal/domains/movies"
	"github.com/brandenk514/mediarr/internal/paging"
)

// Issue #150: the per-kind filter drawer drives the list endpoints
// server-side. The movies facet set (the persisted provider metadata):
// genre (any-match), original language, runtime range, TMDB score range,
// certification, release-date range and the derived status. Keyless
// rows (zero/unknown metadata) simply never match a facet — filtering
// is never an error.

// seedWithMeta adds a movie and stamps persisted metadata directly on
// the mem-repo row (the shape the postgres adapter serves).
func seedWithMeta(t *testing.T, repo *memRepo, svc *Service, title string, meta dom.MovieMetadata, releaseDate string) {
	t.Helper()
	id, err := svc.AddMovie(context.Background(), AddMovieRequest{Title: title, Year: "2001"})
	if err != nil {
		t.Fatalf("AddMovie %s: %v", title, err)
	}
	if err := repo.SetMovieMetadata(context.Background(), id, meta); err != nil {
		t.Fatalf("SetMovieMetadata %s: %v", title, err)
	}
	if releaseDate != "" {
		m := repo.movies[id]
		m.ReleaseDate = releaseDate
		repo.movies[id] = m
	}
}

func listTitles(t *testing.T, svc *Service, f ListFilters) []string {
	t.Helper()
	// title-asc: the mem repo's ListMovies iterates a map (nondeterministic
	// order), so the filter tests pin a deterministic order.
	page, err := svc.ListMoviesQuery(context.Background(), paging.Query{Page: 1, PageSize: 100, Sort: "title-asc"}, f)
	if err != nil {
		t.Fatalf("ListMoviesQuery: %v", err)
	}
	out := make([]string, 0, len(page.Items))
	for _, it := range page.Items {
		out = append(out, it.Title)
	}
	return out
}

func TestListMoviesQuery_FiltersByGenre(t *testing.T) {
	repo := newMemRepo()
	svc := New(Deps{Repo: repo})
	seedWithMeta(t, repo, svc, "Action One", dom.MovieMetadata{Genres: []string{"Action"}}, "")
	seedWithMeta(t, repo, svc, "SciFi Two", dom.MovieMetadata{Genres: []string{"Action", "Science Fiction"}}, "")
	seedWithMeta(t, repo, svc, "Drama Three", dom.MovieMetadata{Genres: []string{"Drama"}}, "")
	seedWithMeta(t, repo, svc, "Keyless Four", dom.MovieMetadata{}, "")

	// Any-match, case-insensitive; keyless (no genres) never matches.
	got := listTitles(t, svc, ListFilters{Genres: []string{"action"}})
	if len(got) != 2 || got[0] != "Action One" || got[1] != "SciFi Two" {
		t.Fatalf("genre=action = %v, want [Action One, SciFi Two]", got)
	}
	// Multiple genres OR together (the drawer's multi-select).
	got = listTitles(t, svc, ListFilters{Genres: []string{"drama", "science fiction"}})
	if len(got) != 2 || got[0] != "Drama Three" || got[1] != "SciFi Two" {
		t.Fatalf("genre=drama|sci-fi = %v, want [Drama Three, SciFi Two] (title order)", got)
	}
}

func TestListMoviesQuery_FiltersByLanguage(t *testing.T) {
	repo := newMemRepo()
	svc := New(Deps{Repo: repo})
	seedWithMeta(t, repo, svc, "English", dom.MovieMetadata{OriginalLanguage: "en"}, "")
	seedWithMeta(t, repo, svc, "French", dom.MovieMetadata{OriginalLanguage: "fr"}, "")
	seedWithMeta(t, repo, svc, "Keyless", dom.MovieMetadata{}, "")

	got := listTitles(t, svc, ListFilters{Language: "fr"})
	if len(got) != 1 || got[0] != "French" {
		t.Fatalf("lang=fr = %v, want [French]", got)
	}
}

func TestListMoviesQuery_FiltersByRuntimeRange(t *testing.T) {
	repo := newMemRepo()
	svc := New(Deps{Repo: repo})
	seedWithMeta(t, repo, svc, "Short", dom.MovieMetadata{Runtime: 90}, "")
	seedWithMeta(t, repo, svc, "Epic", dom.MovieMetadata{Runtime: 180}, "")
	seedWithMeta(t, repo, svc, "Keyless", dom.MovieMetadata{}, "")

	min, max := 120, 200
	got := listTitles(t, svc, ListFilters{RuntimeMin: &min, RuntimeMax: &max})
	if len(got) != 1 || got[0] != "Epic" {
		t.Fatalf("runtime 120..200 = %v, want [Epic]", got)
	}
	// Open-ended min only.
	min = 100
	got = listTitles(t, svc, ListFilters{RuntimeMin: &min})
	if len(got) != 1 || got[0] != "Epic" {
		t.Fatalf("runtime >=100 = %v, want [Epic]", got)
	}
}

func TestListMoviesQuery_FiltersByScoreRange(t *testing.T) {
	repo := newMemRepo()
	svc := New(Deps{Repo: repo})
	seedWithMeta(t, repo, svc, "Good", dom.MovieMetadata{TmdbPct: 85}, "")
	seedWithMeta(t, repo, svc, "Mid", dom.MovieMetadata{TmdbPct: 55}, "")
	seedWithMeta(t, repo, svc, "Keyless", dom.MovieMetadata{}, "")

	min := 70.0
	got := listTitles(t, svc, ListFilters{ScoreMin: &min})
	if len(got) != 1 || got[0] != "Good" {
		t.Fatalf("score >=70 = %v, want [Good]", got)
	}
	max := 60.0
	got = listTitles(t, svc, ListFilters{ScoreMax: &max})
	if len(got) != 1 || got[0] != "Mid" {
		t.Fatalf("score <=60 = %v, want [Mid]", got)
	}
}

func TestListMoviesQuery_FiltersByCertification(t *testing.T) {
	repo := newMemRepo()
	svc := New(Deps{Repo: repo})
	seedWithMeta(t, repo, svc, "Teen", dom.MovieMetadata{Certification: "PG-13"}, "")
	seedWithMeta(t, repo, svc, "Adults", dom.MovieMetadata{Certification: "R"}, "")
	seedWithMeta(t, repo, svc, "Keyless", dom.MovieMetadata{}, "")

	got := listTitles(t, svc, ListFilters{Certification: "PG-13"})
	if len(got) != 1 || got[0] != "Teen" {
		t.Fatalf("cert=PG-13 = %v, want [Teen]", got)
	}
}

func TestListMoviesQuery_FiltersByReleaseDateRange(t *testing.T) {
	repo := newMemRepo()
	svc := New(Deps{Repo: repo})
	seedWithMeta(t, repo, svc, "Old", dom.MovieMetadata{}, "1999-05-01")
	seedWithMeta(t, repo, svc, "Recent", dom.MovieMetadata{}, "2023-05-01")
	seedWithMeta(t, repo, svc, "Unknown", dom.MovieMetadata{}, "")

	got := listTitles(t, svc, ListFilters{ReleaseFrom: "2000-01-01"})
	if len(got) != 1 || got[0] != "Recent" {
		t.Fatalf("from 2000 = %v, want [Recent]", got)
	}
	got = listTitles(t, svc, ListFilters{ReleaseTo: "2000-01-01"})
	if len(got) != 1 || got[0] != "Old" {
		t.Fatalf("to 2000 = %v, want [Old]", got)
	}
	got = listTitles(t, svc, ListFilters{ReleaseFrom: "1990-01-01", ReleaseTo: "2030-01-01"})
	if len(got) != 2 || got[0] != "Old" || got[1] != "Recent" {
		t.Fatalf("1990..2030 = %v, want [Old, Recent]", got)
	}
}

func TestListMoviesQuery_FiltersByStatus(t *testing.T) {
	repo := newMemRepo()
	svc := New(Deps{Repo: repo})
	downloadedID, err := svc.AddMovie(context.Background(), AddMovieRequest{Title: "Have", Year: "2001"})
	if err != nil {
		t.Fatalf("AddMovie: %v", err)
	}
	if _, err := svc.AddMovie(context.Background(), AddMovieRequest{Title: "Miss", Year: "2001"}); err != nil {
		t.Fatalf("AddMovie 2: %v", err)
	}
	// Satisfy the first movie's wanted row: status downloaded:monitored.
	if err := repo.MarkWantedSatisfied(context.Background(), downloadedID, "r"); err != nil {
		t.Fatalf("MarkWantedSatisfied: %v", err)
	}

	got := listTitles(t, svc, ListFilters{Status: "downloaded:monitored"})
	if len(got) != 1 || got[0] != "Have" {
		t.Fatalf("status=downloaded:monitored = %v, want [Have]", got)
	}
	got = listTitles(t, svc, ListFilters{Status: "missing:monitored"})
	if len(got) != 1 || got[0] != "Miss" {
		t.Fatalf("status=missing:monitored = %v, want [Miss]", got)
	}
}

func TestListMoviesQuery_CombinedFacets(t *testing.T) {
	repo := newMemRepo()
	svc := New(Deps{Repo: repo})
	seedWithMeta(t, repo, svc, "Match", dom.MovieMetadata{Genres: []string{"Action"}, OriginalLanguage: "en", TmdbPct: 90}, "2022-01-01")
	seedWithMeta(t, repo, svc, "WrongGenre", dom.MovieMetadata{Genres: []string{"Drama"}, OriginalLanguage: "en", TmdbPct: 90}, "2022-01-01")
	seedWithMeta(t, repo, svc, "WrongLang", dom.MovieMetadata{Genres: []string{"Action"}, OriginalLanguage: "fr", TmdbPct: 90}, "2022-01-01")

	min := 70.0
	got := listTitles(t, svc, ListFilters{
		Genres:      []string{"action"},
		Language:    "en",
		ScoreMin:    &min,
		ReleaseFrom: "2020-01-01",
	})
	if len(got) != 1 || got[0] != "Match" {
		t.Fatalf("combined facets = %v, want [Match]", got)
	}
}
