package tv

import (
	"context"
	"errors"
	"testing"

	dom "github.com/brandenk514/mediarr/internal/domains/tv"
	"github.com/brandenk514/mediarr/internal/providers"
)

// Issue #149 Stage A (TV): PosterURL + BackdropURL on the series detail
// document, PosterURL persisted on the row so list items carry it without a
// per-row provider call. Keyless degrades to "".

// fakeArtworker is an in-memory Artworker for the #149 tests.
type fakeArtworker struct {
	search     []providers.Result
	art        map[string]*providers.SeriesArtwork // tmdbID -> artwork
	searchErr  error
	artErr     error
	searchCall bool
	artCalls   int
}

func (f *fakeArtworker) SearchTV(ctx context.Context, q string) ([]providers.Result, error) {
	f.searchCall = true
	return f.search, f.searchErr
}

func (f *fakeArtworker) GetSeriesArtwork(ctx context.Context, tmdbID string) (*providers.SeriesArtwork, error) {
	f.artCalls++
	if f.artErr != nil {
		return nil, f.artErr
	}
	return f.art[tmdbID], nil
}

func TestEnrichSeriesArtwork_MapsURLs(t *testing.T) {
	art := &fakeArtworker{art: map[string]*providers.SeriesArtwork{
		"1399": {PosterPath: "/got.jpg", BackdropPath: "/got-wide.jpg"},
	}}
	s := &dom.Series{TMDBID: 1399}
	enrichSeriesArtwork(context.Background(), art, s)
	if want := "https://image.tmdb.org/t/p/w342/got.jpg"; s.PosterURL != want {
		t.Fatalf("PosterURL = %q, want %q", s.PosterURL, want)
	}
	if want := "https://image.tmdb.org/t/p/w1280/got-wide.jpg"; s.BackdropURL != want {
		t.Fatalf("BackdropURL = %q, want %q", s.BackdropURL, want)
	}
}

func TestEnrichSeriesArtwork_NilAndFailureSafe(t *testing.T) {
	// Nil Artworker (keyless): no panic, fields stay "".
	s := &dom.Series{TMDBID: 1399}
	enrichSeriesArtwork(context.Background(), nil, s)
	if s.PosterURL != "" || s.BackdropURL != "" {
		t.Fatalf("nil artworker artwork = %q/%q, want empty/empty", s.PosterURL, s.BackdropURL)
	}

	// Provider error: fields stay "" (artwork never fails the read).
	bad := &fakeArtworker{artErr: errors.New("tmdb down"), art: map[string]*providers.SeriesArtwork{}}
	s2 := &dom.Series{TMDBID: 1399}
	enrichSeriesArtwork(context.Background(), bad, s2)
	if s2.PosterURL != "" || s2.BackdropURL != "" {
		t.Fatalf("error artwork = %q/%q, want empty/empty", s2.PosterURL, s2.BackdropURL)
	}

	// Artless series: provider returns empty paths -> "".
	bare := &fakeArtworker{art: map[string]*providers.SeriesArtwork{
		"1399": {},
	}}
	s3 := &dom.Series{TMDBID: 1399}
	enrichSeriesArtwork(context.Background(), bare, s3)
	if s3.PosterURL != "" || s3.BackdropURL != "" {
		t.Fatalf("artless artwork = %q/%q, want empty/empty", s3.PosterURL, s3.BackdropURL)
	}
}

func TestEnrichSeriesArtwork_SearchFallback(t *testing.T) {
	// No TMDB id stored: resolve via title search, then fetch artwork.
	art := &fakeArtworker{
		search: []providers.Result{{ID: "1399", Title: "Game of Thrones", Year: 2011}},
		art: map[string]*providers.SeriesArtwork{
			"1399": {PosterPath: "/got.jpg"},
		},
	}
	s := &dom.Series{Title: "Game of Thrones", Year: 2011}
	enrichSeriesArtwork(context.Background(), art, s)
	if !art.searchCall {
		t.Fatal("expected a title search for the TMDBID-less series")
	}
	if want := "https://image.tmdb.org/t/p/w342/got.jpg"; s.PosterURL != want {
		t.Fatalf("PosterURL = %q, want %q", s.PosterURL, want)
	}

	// No search hits: fields stay "".
	none := &fakeArtworker{search: nil, art: map[string]*providers.SeriesArtwork{}}
	s2 := &dom.Series{Title: "Unknown Show"}
	enrichSeriesArtwork(context.Background(), none, s2)
	if s2.PosterURL != "" || s2.BackdropURL != "" {
		t.Fatalf("no-hit artwork = %q/%q, want empty/empty", s2.PosterURL, s2.BackdropURL)
	}
}

func TestGetSeries_EnrichesAndPersistsArtwork(t *testing.T) {
	// The detail read enriches and persists the artwork on the row; the
	// LIST read (no enrichment) must then serve PosterURL from persisted
	// data — no second provider call.
	art := &fakeArtworker{art: map[string]*providers.SeriesArtwork{
		"1399": {PosterPath: "/poster.jpg", BackdropPath: "/backdrop.jpg"},
	}}
	repo := newMemRepo()
	svc := New(Deps{Repo: repo, Artwork: art})

	id, err := repo.CreateSeries(context.Background(), dom.Series{TMDBID: 1399, Title: "Game of Thrones", Year: 2011})
	if err != nil {
		t.Fatalf("CreateSeries: %v", err)
	}
	detail, err := svc.GetSeries(context.Background(), id)
	if err != nil {
		t.Fatalf("GetSeries: %v", err)
	}
	if want := "https://image.tmdb.org/t/p/w342/poster.jpg"; detail.PosterURL != want {
		t.Fatalf("detail PosterURL = %q, want %q", detail.PosterURL, want)
	}
	if want := "https://image.tmdb.org/t/p/w1280/backdrop.jpg"; detail.BackdropURL != want {
		t.Fatalf("detail BackdropURL = %q, want %q", detail.BackdropURL, want)
	}

	list, err := svc.ListSeries(context.Background())
	if err != nil {
		t.Fatalf("ListSeries: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("list len = %d, want 1", len(list))
	}
	if want := "https://image.tmdb.org/t/p/w342/poster.jpg"; list[0].PosterURL != want {
		t.Fatalf("list PosterURL = %q, want %q (persisted)", list[0].PosterURL, want)
	}
	if art.artCalls != 1 {
		t.Fatalf("provider artwork calls = %d, want 1 (list must not enrich)", art.artCalls)
	}
}

func TestGetSeries_KeylessArtworkEmpty(t *testing.T) {
	repo := newMemRepo()
	svc := New(Deps{Repo: repo}) // no Artwork dep: keyless
	id, err := repo.CreateSeries(context.Background(), dom.Series{Title: "Old Show", Year: 1999})
	if err != nil {
		t.Fatalf("CreateSeries: %v", err)
	}
	detail, err := svc.GetSeries(context.Background(), id)
	if err != nil {
		t.Fatalf("GetSeries: %v", err)
	}
	if detail.PosterURL != "" || detail.BackdropURL != "" {
		t.Fatalf("keyless artwork = %q/%q, want empty/empty", detail.PosterURL, detail.BackdropURL)
	}
}
