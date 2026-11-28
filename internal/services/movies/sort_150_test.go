package movies

import (
	"context"
	"errors"
	"testing"

	dom "github.com/brandenk514/mediarr/internal/domains/movies"
	"github.com/brandenk514/mediarr/internal/paging"
)

// Issue #150: the movies list endpoint validates ?sort= server-side
// (unknown field/direction → paging.ErrInvalidSort, the API maps it to
// 400) and gains the Seerr-style sorts the grid control cluster offers:
// Release Date and TMDB Rating on top of the existing title/year/added.

func seedMeta(t *testing.T, repo *memRepo, id int64, genres []string, runtime int, lang, cert string, pct *float64) {
	t.Helper()
	m := repo.movies[id]
	m.Genres = genres
	if runtime > 0 {
		m.Runtime = &runtime
	}
	if lang != "" {
		m.OriginalLanguage = &lang
	}
	if cert != "" {
		m.Certification = &cert
	}
	if pct != nil {
		m.Scores = &dom.MovieScores{TmdbPct: pct}
	}
	repo.movies[id] = m
}

func TestListMoviesQuery_UnknownSortRejected(t *testing.T) {
	repo := newMemRepo()
	svc := New(Deps{Repo: repo})
	for _, sort := range []string{"bogus-asc", "title-sideways"} {
		_, err := svc.ListMoviesQuery(context.Background(), paging.Query{Sort: sort})
		if !errors.Is(err, paging.ErrInvalidSort) {
			t.Errorf("sort %q: err = %v, want paging.ErrInvalidSort", sort, err)
		}
	}
}

func TestListMoviesQuery_SortsByReleaseDate(t *testing.T) {
	repo := newMemRepo()
	svc := New(Deps{Repo: repo})
	ids := make([]int64, 3)
	titles := []string{"Recent", "Unknown", "Old"}
	for i, title := range titles {
		id, err := svc.AddMovie(context.Background(), AddMovieRequest{Title: title, Year: "2001"})
		if err != nil {
			t.Fatalf("AddMovie: %v", err)
		}
		ids[i] = id
	}
	// Release dates: Recent 2022-01-01, Old 1999-12-31, Unknown "".
	m := repo.movies[ids[0]]
	m.ReleaseDate = "2022-01-01"
	repo.movies[ids[0]] = m
	m = repo.movies[ids[2]]
	m.ReleaseDate = "1999-12-31"
	repo.movies[ids[2]] = m

	page, err := svc.ListMoviesQuery(context.Background(), paging.Query{Sort: "release_date-asc"})
	if err != nil {
		t.Fatalf("ListMoviesQuery: %v", err)
	}
	var got []string
	for _, it := range page.Items {
		got = append(got, it.Title)
	}
	// Ascending: "" first, then oldest → newest.
	if len(got) != 3 || got[0] != "Unknown" || got[1] != "Old" || got[2] != "Recent" {
		t.Fatalf("release_date-asc = %v, want [Unknown, Old, Recent]", got)
	}

	page, err = svc.ListMoviesQuery(context.Background(), paging.Query{Sort: "release_date-desc"})
	if err != nil {
		t.Fatalf("ListMoviesQuery: %v", err)
	}
	got = nil
	for _, it := range page.Items {
		got = append(got, it.Title)
	}
	// Descending: newest → oldest, "" last (the Seerr default browse).
	if len(got) != 3 || got[0] != "Recent" || got[1] != "Old" || got[2] != "Unknown" {
		t.Fatalf("release_date-desc = %v, want [Recent, Old, Unknown]", got)
	}
}

func TestListMoviesQuery_SortsByRating(t *testing.T) {
	repo := newMemRepo()
	svc := New(Deps{Repo: repo})
	ids := make([]int64, 3)
	titles := []string{"High", "None", "Mid"}
	for i, title := range titles {
		id, err := svc.AddMovie(context.Background(), AddMovieRequest{Title: title, Year: "2001"})
		if err != nil {
			t.Fatalf("AddMovie: %v", err)
		}
		ids[i] = id
	}
	high, mid := 90.0, 70.0
	seedMeta(t, repo, ids[0], nil, 0, "", "", &high)
	seedMeta(t, repo, ids[2], nil, 0, "", "", &mid)

	page, err := svc.ListMoviesQuery(context.Background(), paging.Query{Sort: "rating-desc"})
	if err != nil {
		t.Fatalf("ListMoviesQuery: %v", err)
	}
	var got []string
	for _, it := range page.Items {
		got = append(got, it.Title)
	}
	// Descending: best → worst, unknown (nil score) last.
	if len(got) != 3 || got[0] != "High" || got[1] != "Mid" || got[2] != "None" {
		t.Fatalf("rating-desc = %v, want [High, Mid, None]", got)
	}
}
