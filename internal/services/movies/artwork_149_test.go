package movies

import (
	"context"
	"testing"

	dom "github.com/brandenk514/mediarr/internal/domains/movies"
	"github.com/brandenk514/mediarr/internal/providers"
)

// Issue #149 Stage A (movies): PosterURL + BackdropURL on the detail
// document, PosterURL persisted on the row so list items carry it
// without a per-row provider call. Keyless degrades to "".

func TestEnricher_MapsArtworkURLs(t *testing.T) {
	stub := &stubDetailer{
		searchResults: []providers.Result{{ID: "157336", Title: "Inception", Year: 2010}},
		detail: &providers.MovieDetail{
			TmdbID:       "157336",
			PosterPath:   "/abc123.jpg",
			BackdropPath: "/def456.jpg",
		},
	}
	e := &TMDBEnricher{Client: stub}
	m := &dom.Movie{Title: "Inception", Year: 2010}

	if err := e.EnrichMovie(context.Background(), m); err != nil {
		t.Fatalf("EnrichMovie: %v", err)
	}
	// Upstream TMDB URLs (the w342 poster / w1280 backdrop sizes), NOT
	// proxy URLs — the proxy is a separate route (Stage B).
	if want := "https://image.tmdb.org/t/p/w342/abc123.jpg"; m.PosterURL != want {
		t.Fatalf("PosterURL = %q, want %q", m.PosterURL, want)
	}
	if want := "https://image.tmdb.org/t/p/w1280/def456.jpg"; m.BackdropURL != want {
		t.Fatalf("BackdropURL = %q, want %q", m.BackdropURL, want)
	}
}

func TestEnricher_KeylessArtworkEmpty(t *testing.T) {
	// Keyless: no hits / no detail → "" everywhere, never an error.
	e := &TMDBEnricher{Client: &stubDetailer{}}
	m := &dom.Movie{Title: "Unknown", Year: 1999}
	if err := e.EnrichMovie(context.Background(), m); err != nil {
		t.Fatalf("EnrichMovie: %v", err)
	}
	if m.PosterURL != "" || m.BackdropURL != "" {
		t.Fatalf("keyless artwork = %q/%q, want empty/empty", m.PosterURL, m.BackdropURL)
	}

	// Detail with no artwork paths → "" too.
	e2 := &TMDBEnricher{Client: &stubDetailer{
		searchResults: []providers.Result{{ID: "157336", Title: "Inception", Year: 2010}},
		detail:        &providers.MovieDetail{TmdbID: "157336"},
	}}
	m2 := &dom.Movie{Title: "Inception", Year: 2010}
	if err := e2.EnrichMovie(context.Background(), m2); err != nil {
		t.Fatalf("EnrichMovie: %v", err)
	}
	if m2.PosterURL != "" || m2.BackdropURL != "" {
		t.Fatalf("artwork-less detail = %q/%q, want empty/empty", m2.PosterURL, m2.BackdropURL)
	}
}

func TestGetMovie_PersistsArtworkForList(t *testing.T) {
	// The detail read enriches and persists the artwork on the row; the
	// LIST read (no enrichment) must then serve PosterURL from persisted
	// data — no second provider call.
	stub := &countingDetailer{
		searchResults: []providers.Result{{ID: "157336", Title: "Inception", Year: 2010}},
		detail: &providers.MovieDetail{
			TmdbID:       "157336",
			PosterPath:   "/poster.jpg",
			BackdropPath: "/backdrop.jpg",
		},
	}
	repo := newMemRepo()
	svc := New(Deps{Repo: repo, Enricher: &TMDBEnricher{Client: stub}})

	id, err := svc.AddMovie(context.Background(), AddMovieRequest{Title: "Inception", Year: "2010"})
	if err != nil {
		t.Fatalf("AddMovie: %v", err)
	}
	detail, err := svc.GetMovie(context.Background(), id)
	if err != nil {
		t.Fatalf("GetMovie: %v", err)
	}
	if want := "https://image.tmdb.org/t/p/w342/poster.jpg"; detail.PosterURL != want {
		t.Fatalf("detail PosterURL = %q, want %q", detail.PosterURL, want)
	}
	if want := "https://image.tmdb.org/t/p/w1280/backdrop.jpg"; detail.BackdropURL != want {
		t.Fatalf("detail BackdropURL = %q, want %q", detail.BackdropURL, want)
	}

	list, err := svc.ListMovies(context.Background())
	if err != nil {
		t.Fatalf("ListMovies: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("list len = %d, want 1", len(list))
	}
	if want := "https://image.tmdb.org/t/p/w342/poster.jpg"; list[0].PosterURL != want {
		t.Fatalf("list PosterURL = %q, want %q (persisted)", list[0].PosterURL, want)
	}
	if stub.detailCalls != 1 {
		t.Fatalf("provider detail calls = %d, want 1 (list must not enrich)", stub.detailCalls)
	}
}

// countingDetailer wraps stubDetailer with a detail-call counter.
type countingDetailer struct {
	stubDetailer
	detailCalls int
}

func (s *countingDetailer) GetMovieDetail(ctx context.Context, id string) (*providers.MovieDetail, error) {
	s.detailCalls++
	return s.stubDetailer.GetMovieDetail(ctx, id)
}
