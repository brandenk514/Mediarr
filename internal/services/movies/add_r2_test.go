package movies

import (
	"context"
	"errors"
	"testing"

	dom "github.com/brandenk514/mediarr/internal/domains/movies"
)

// newAddTestService builds a service over the in-memory repo with no indexers
// and no download client: the R2 add path runs, and any pipeline run fails
// fast (no client) — exactly the best-effort scenario start_search must
// survive.
func newAddTestService() (*Service, *memRepo) {
	repo := newMemRepo()
	return New(Deps{Repo: repo, DefaultProfile: "HD-1080p"}), repo
}

// TestAddMovieMonitorVariants asserts the edition-aware monitor mapping
// (Radarr R2): each mode produces the expected movie-level flag, seeded
// 'Theatrical' edition state, and wanted-row behavior.
func TestAddMovieMonitorVariants(t *testing.T) {
	for _, tc := range []struct {
		name          string
		monitor       string
		wantMonitored bool
		wantEdition   bool
		wantWanted    bool
	}{
		{name: "all (default)", monitor: "", wantMonitored: true, wantEdition: true, wantWanted: true},
		{name: "all", monitor: "all", wantMonitored: true, wantEdition: true, wantWanted: true},
		{name: "movie_only", monitor: "movie_only", wantMonitored: true, wantEdition: false, wantWanted: true},
		{name: "unmonitored", monitor: "unmonitored", wantMonitored: false, wantEdition: false, wantWanted: false},
		// 'Theatrical' does not contain '3d', so all_except_3d monitors it.
		{name: "all_except_3d", monitor: "all_except_3d", wantMonitored: true, wantEdition: true, wantWanted: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo := newAddTestService()
			id, err := svc.AddMovie(context.Background(), AddMovieRequest{
				Title:   "Dune Part Two",
				Year:    "2024",
				Monitor: tc.monitor,
			})
			if err != nil {
				t.Fatalf("add: %v", err)
			}
			m, err := repo.GetMovie(context.Background(), id)
			if err != nil {
				t.Fatalf("get movie: %v", err)
			}
			if m.Monitored != tc.wantMonitored {
				t.Errorf("movie.Monitored = %v, want %v", m.Monitored, tc.wantMonitored)
			}
			var ed *dom.Edition
			for _, e := range repo.editions {
				if e.MovieID == id {
					ed = &e
				}
			}
			if ed == nil {
				t.Fatal("no seeded 'Theatrical' edition")
			}
			if ed.Monitored != tc.wantEdition {
				t.Errorf("edition.Monitored = %v, want %v", ed.Monitored, tc.wantEdition)
			}
			w, err := repo.GetWanted(context.Background(), id)
			if tc.wantWanted {
				if err != nil || w == nil {
					t.Fatalf("wanted row missing: %v", err)
				}
			} else if w != nil && err == nil {
				t.Errorf("wanted row present for unmonitored mode, want none")
			}
		})
	}
}

// TestAddMovieAllExcept3DEditionPredicate exercises the per-edition predicate
// directly (the seeded edition can't express the 3D exclusion — a real 3D
// edition is added via CreateEdition after the add).
func TestAddMovieAllExcept3DEditionPredicate(t *testing.T) {
	plan, err := dom.MonitorPlanFor(dom.MonitorAllExcept3D)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	for label, want := range map[string]bool{
		"Theatrical":  true,
		"Extended":    true,
		"IMAX 3D":     false,
		"Director 3D": false,
		"3D Special":  false,
		"3D":          false,
	} {
		if got := plan.EditionMonitored(label); got != want {
			t.Errorf("EditionMonitored(%q) = %v, want %v", label, got, want)
		}
	}
}

// TestAddMoviePersistsR2Fields asserts tags, root folder and minimum
// availability land on the stored movie.
func TestAddMoviePersistsR2Fields(t *testing.T) {
	svc, repo := newAddTestService()
	id, err := svc.AddMovie(context.Background(), AddMovieRequest{
		Title:               "Dune Part Two",
		Year:                "2024",
		Monitor:             "movie_only",
		MinimumAvailability: "announced",
		Tags:                []string{"sci-fi", "watchlist"},
		RootFolder:          "/media/movies",
	})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	m, err := repo.GetMovie(context.Background(), id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(m.Tags) != 2 || m.Tags[0] != "sci-fi" || m.Tags[1] != "watchlist" {
		t.Errorf("tags = %v, want [sci-fi watchlist]", m.Tags)
	}
	if m.RootFolder != "/media/movies" {
		t.Errorf("root folder = %q, want /media/movies", m.RootFolder)
	}
	if m.MinimumAvailability != "announced" {
		t.Errorf("minimum availability = %q, want announced", m.MinimumAvailability)
	}
}

// TestAddMovieMinimumAvailabilityDefault asserts an omitted
// minimum_availability defaults to 'released' (Radarr R2).
func TestAddMovieMinimumAvailabilityDefault(t *testing.T) {
	svc, repo := newAddTestService()
	id, err := svc.AddMovie(context.Background(), AddMovieRequest{
		Title: "Dune Part Two", Year: "2024",
	})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	m, err := repo.GetMovie(context.Background(), id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if m.MinimumAvailability != "released" {
		t.Errorf("minimum availability = %q, want released (the add default)", m.MinimumAvailability)
	}
}

// TestAddMovieInvalidMonitor asserts an unknown monitor mode is rejected with
// the sentinel the API maps to 400.
func TestAddMovieInvalidMonitor(t *testing.T) {
	svc, _ := newAddTestService()
	_, err := svc.AddMovie(context.Background(), AddMovieRequest{Title: "X", Monitor: "bogus"})
	if !errors.Is(err, ErrInvalidAddParams) {
		t.Fatalf("err = %v, want ErrInvalidAddParams", err)
	}
}

// TestAddMovieInvalidMinimumAvailability asserts an unknown availability is
// rejected with the sentinel the API maps to 400.
func TestAddMovieInvalidMinimumAvailability(t *testing.T) {
	svc, _ := newAddTestService()
	_, err := svc.AddMovie(context.Background(), AddMovieRequest{
		Title: "X", MinimumAvailability: "previews",
	})
	if !errors.Is(err, ErrInvalidAddParams) {
		t.Fatalf("err = %v, want ErrInvalidAddParams", err)
	}
}

// TestAddMovieStartSearchBestEffort asserts start_search runs the pipeline
// after the add and that a pipeline failure (no download client here) does
// NOT fail the add: the id is returned and a start-search-failed history
// entry is recorded.
func TestAddMovieStartSearchBestEffort(t *testing.T) {
	svc, repo := newAddTestService()
	id, err := svc.AddMovie(context.Background(), AddMovieRequest{
		Title:       "Dune Part Two",
		Year:        "2024",
		StartSearch: true,
	})
	if err != nil {
		t.Fatalf("add with start_search must not fail: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero id")
	}
	var found bool
	for _, h := range repo.history {
		if h.MovieID == id && h.Event == "start-search-failed" {
			found = true
		}
	}
	if !found {
		t.Errorf("history = %+v, want a start-search-failed entry (pipeline has no client)", repo.history)
	}
}

// TestAddMovieNoStartSearch asserts the default add (no start_search) leaves
// the pipeline untouched: no search/no-client history entries.
func TestAddMovieNoStartSearch(t *testing.T) {
	svc, repo := newAddTestService()
	if _, err := svc.AddMovie(context.Background(), AddMovieRequest{
		Title: "Dune Part Two", Year: "2024",
	}); err != nil {
		t.Fatalf("add: %v", err)
	}
	for _, h := range repo.history {
		switch h.Event {
		case "no-client", "search", "searched", "no-match":
			t.Errorf("unexpected pipeline history entry %q without start_search", h.Event)
		}
	}
}
