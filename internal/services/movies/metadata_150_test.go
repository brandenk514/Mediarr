package movies

import (
	"context"
	"testing"

	"github.com/brandenk514/mediarr/internal/providers"
)

// Issue #150 (movies): the TMDB detail fetch already fills genres,
// runtime, original language, certification and the TMDB score on the
// detail document; this issue persists them on the row so the list
// endpoint can filter and sort on them. Keyless degrades to zero
// values, and the list read must not make a provider call.

func TestGetMovie_PersistsMetadataForList(t *testing.T) {
	// The detail read enriches and persists the provider metadata on the
	// row; the LIST read (no enrichment) must then serve it from the
	// persisted data — no second provider call.
	stub := &countingDetailer{
		searchResults: []providers.Result{{ID: "157336", Title: "Inception", Year: 2010}},
		detail: &providers.MovieDetail{
			TmdbID:           "157336",
			Genres:           []string{"Action", "Science Fiction"},
			Runtime:          148,
			OriginalLanguage: "en",
			VoteAverage:      8.4,
			Certification:    "PG-13",
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
	// The detail document carries the enrichment (existing behavior).
	if len(detail.Genres) != 2 || detail.Genres[0] != "Action" {
		t.Fatalf("detail Genres = %v, want [Action, Science Fiction]", detail.Genres)
	}
	if detail.Runtime == nil || *detail.Runtime != 148 {
		t.Fatalf("detail Runtime = %v, want 148", detail.Runtime)
	}
	if detail.Scores == nil || detail.Scores.TmdbPct == nil || *detail.Scores.TmdbPct != 84 {
		t.Fatalf("detail TmdbPct = %v, want 84", detail.Scores)
	}

	// The list read serves the same metadata persisted on the row.
	list, err := svc.ListMovies(context.Background())
	if err != nil {
		t.Fatalf("ListMovies: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("list len = %d, want 1", len(list))
	}
	got := list[0]
	if len(got.Genres) != 2 || got.Genres[0] != "Action" {
		t.Fatalf("list Genres = %v, want persisted [Action, Science Fiction]", got.Genres)
	}
	if got.Runtime == nil || *got.Runtime != 148 {
		t.Fatalf("list Runtime = %v, want persisted 148", got.Runtime)
	}
	if got.OriginalLanguage == nil || *got.OriginalLanguage != "en" {
		t.Fatalf("list OriginalLanguage = %v, want persisted en", got.OriginalLanguage)
	}
	if got.Certification == nil || *got.Certification != "PG-13" {
		t.Fatalf("list Certification = %v, want persisted PG-13", got.Certification)
	}
	if got.Scores == nil || got.Scores.TmdbPct == nil || *got.Scores.TmdbPct != 84 {
		t.Fatalf("list TmdbPct = %v, want persisted 84", got.Scores)
	}
	if stub.detailCalls != 1 {
		t.Fatalf("provider detail calls = %d, want 1 (list must not enrich)", stub.detailCalls)
	}
}

func TestGetMovie_MetadataPersistedOnlyWhenChanged(t *testing.T) {
	// The persist call is best-effort and only fires when the provider
	// reported something the row does not already carry (the #149
	// artwork pattern) — a second detail read with no changes must not
	// re-write the metadata.
	stub := &countingDetailer{
		searchResults: []providers.Result{{ID: "157336", Title: "Inception", Year: 2010}},
		detail: &providers.MovieDetail{
			TmdbID:      "157336",
			Genres:      []string{"Action"},
			VoteAverage: 8.0,
		},
	}
	repo := newMemRepo()
	svc := New(Deps{Repo: repo, Enricher: &TMDBEnricher{Client: stub}})

	id, err := svc.AddMovie(context.Background(), AddMovieRequest{Title: "Inception", Year: "2010"})
	if err != nil {
		t.Fatalf("AddMovie: %v", err)
	}
	for i := 0; i < 2; i++ {
		if _, err := svc.GetMovie(context.Background(), id); err != nil {
			t.Fatalf("GetMovie %d: %v", i, err)
		}
	}
	if repo.metadataWrites[id] != 1 {
		t.Fatalf("metadata writes = %d, want 1 (unchanged second read)", repo.metadataWrites[id])
	}
}
