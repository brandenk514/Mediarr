package movies

import (
	"context"
	"fmt"
	"testing"
	"time"

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

// TestListMoviesQuery_ServerSideSort is the #70 regression: the grid's
// added/year/title sorts used to run client-side over the single fetched page,
// so with more than one page the newest/oldest rows could never surface. The
// server must now order the full filtered set BEFORE paging.
func TestListMoviesQuery_ServerSideSort(t *testing.T) {
	svc, repo := newQueryService(t)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// Seed 5 movies in insertion order (id 1..5). id 5 is the newest by both
	// AddedAt and Year, and sits LAST in the natural (insertion) order — so
	// under the old client-side sort it would be beyond page 1 and invisible.
	for i := 1; i <= 5; i++ {
		id := repo.nextMovie
		repo.nextMovie++
		repo.movies[id] = dom.Movie{
			ID:             id,
			Title:          fmt.Sprintf("Movie %d", i),
			Year:           2020 + i,
			Monitored:      true,
			QualityProfile: "HD-1080p",
			AddedAt:        base.Add(time.Duration(i) * 24 * time.Hour),
		}
	}

	// added-desc: the two newest (id 5, id 4) must lead page 1 even though
	// they are the last two in insertion order.
	page, err := svc.ListMoviesQuery(context.Background(), paging.Query{Sort: "added-desc", Page: 1, PageSize: 2})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 5 || len(page.Items) != 2 {
		t.Fatalf("added-desc => total=%d len=%d, want 5/2", page.Total, len(page.Items))
	}
	if page.Items[0].ID != 5 || page.Items[1].ID != 4 {
		t.Errorf("added-desc page1 ids = %d,%d, want 5,4", page.Items[0].ID, page.Items[1].ID)
	}

	// year-asc: the two oldest (id 1, id 2) lead page 1.
	page, err = svc.ListMoviesQuery(context.Background(), paging.Query{Sort: "year-asc", Page: 1, PageSize: 2})
	if err != nil {
		t.Fatal(err)
	}
	if page.Items[0].ID != 1 || page.Items[1].ID != 2 {
		t.Errorf("year-asc page1 ids = %d,%d, want 1,2", page.Items[0].ID, page.Items[1].ID)
	}

	// year-desc: the two highest years (id 5, id 4) lead page 1.
	page, err = svc.ListMoviesQuery(context.Background(), paging.Query{Sort: "year-desc", Page: 1, PageSize: 2})
	if err != nil {
		t.Fatal(err)
	}
	if page.Items[0].ID != 5 || page.Items[1].ID != 4 {
		t.Errorf("year-desc page1 ids = %d,%d, want 5,4", page.Items[0].ID, page.Items[1].ID)
	}

	// title-asc is a stable, human-friendly ordering (case-insensitive).
	page, err = svc.ListMoviesQuery(context.Background(), paging.Query{Sort: "title-asc", Page: 1, PageSize: 2})
	if err != nil {
		t.Fatal(err)
	}
	if page.Items[0].Title != "Movie 1" || page.Items[1].Title != "Movie 2" {
		t.Errorf("title-asc page1 = %q,%q, want Movie 1,Movie 2", page.Items[0].Title, page.Items[1].Title)
	}

	// added-asc: the two oldest (id 1, id 2) lead page 1.
	page, err = svc.ListMoviesQuery(context.Background(), paging.Query{Sort: "added-asc", Page: 1, PageSize: 2})
	if err != nil {
		t.Fatal(err)
	}
	if page.Items[0].ID != 1 || page.Items[1].ID != 2 {
		t.Errorf("added-asc page1 ids = %d,%d, want 1,2", page.Items[0].ID, page.Items[1].ID)
	}
}

// TestListMoviesQuery_UnknownSortKeepsNaturalOrder pins that an unrecognized
// ?sort= value does not 500 or reorder — it keeps the natural order.
func TestListMoviesQuery_UnknownSortKeepsNaturalOrder(t *testing.T) {
	svc, repo := newQueryService(t)
	seedMovies(repo, "Dune", "Inception")

	page, err := svc.ListMoviesQuery(context.Background(), paging.Query{Sort: "banana-desc", Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || len(page.Items) != 2 {
		t.Errorf("unknown sort => total=%d len=%d, want 2/2", page.Total, len(page.Items))
	}
}
