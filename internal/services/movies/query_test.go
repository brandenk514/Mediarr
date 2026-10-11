package movies

import (
	"context"
	"testing"

	dom "github.com/brandenk514/mediarr/internal/domains/movies"
	"github.com/brandenk514/mediarr/internal/paging"
)

// newQueryService builds a Service over memRepo for the paging/toggle unit
// tests. Only the Repo dep is needed for ListMoviesQuery and
// SetMovieMonitored.
func newQueryService(t *testing.T) (*Service, *memRepo) {
	t.Helper()
	repo := newMemRepo()
	svc := New(Deps{Repo: repo, DefaultProfile: "HD-1080p"})
	return svc, repo
}

func seedMovies(repo *memRepo, titles ...string) []int64 {
	var ids []int64
	for _, title := range titles {
		id := repo.nextMovie
		repo.nextMovie++
		repo.movies[id] = dom.Movie{ID: id, Title: title, Monitored: true, QualityProfile: "HD-1080p"}
		ids = append(ids, id)
	}
	return ids
}

func TestSetMovieMonitored_ToggleOff_ClearsFlag(t *testing.T) {
	svc, repo := newQueryService(t)
	ids := seedMovies(repo, "Dune")

	if err := svc.SetMovieMonitored(context.Background(), ids[0], false); err != nil {
		t.Fatalf("toggle off: %v", err)
	}
	m, _ := repo.GetMovie(context.Background(), ids[0])
	if m.Monitored {
		t.Error("movie should be unmonitored")
	}
	// No new wanted row and no unwanted churn: the original pending wanted
	// from seeding (none here) is untouched.
}

func TestSetMovieMonitored_ToggleOn_EnsuresWanted(t *testing.T) {
	svc, repo := newQueryService(t)
	ids := seedMovies(repo, "Dune")
	m, _ := repo.GetMovie(context.Background(), ids[0])
	m.Monitored = false
	repo.movies[ids[0]] = *m

	if err := svc.SetMovieMonitored(context.Background(), ids[0], true); err != nil {
		t.Fatalf("toggle on: %v", err)
	}
	got, _ := repo.GetMovie(context.Background(), ids[0])
	if !got.Monitored {
		t.Error("movie should be monitored")
	}
	if _, err := repo.GetWanted(context.Background(), ids[0]); err != nil {
		t.Errorf("expected a pending wanted row after toggle-on, got %v", err)
	}
}

func TestSetMovieMonitored_UnknownMovie_404(t *testing.T) {
	svc, _ := newQueryService(t)
	err := svc.SetMovieMonitored(context.Background(), 999, true)
	if err != dom.ErrMovieNotFound {
		t.Errorf("err = %v, want ErrMovieNotFound", err)
	}
}

func TestListMoviesQuery_FiltersAndPaginates(t *testing.T) {
	svc, repo := newQueryService(t)
	seedMovies(repo, "Dune", "dune part two", "Inception", "Oppenheimer", "The Matrix")

	// Q filter (case-insensitive) narrows to the two dune titles.
	page, err := svc.ListMoviesQuery(context.Background(), paging.Query{Q: "dune"})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 {
		t.Errorf("dune filter total = %d, want 2", page.Total)
	}

	// Monitored filter: all seeded are monitored, so true -> all 5.
	page, err = svc.ListMoviesQuery(context.Background(), paging.Query{Monitored: func() *bool { b := true; return &b }()})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 5 {
		t.Errorf("monitored=true total = %d, want 5", page.Total)
	}

	// Pagination: page 2 of size 2 over 5 items -> 2 items.
	page, err = svc.ListMoviesQuery(context.Background(), paging.Query{Page: 2, PageSize: 2})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 5 || len(page.Items) != 2 || page.Page != 2 {
		t.Errorf("page2/size2 => total=%d len=%d page=%d, want 5/2/2", page.Total, len(page.Items), page.Page)
	}
}
