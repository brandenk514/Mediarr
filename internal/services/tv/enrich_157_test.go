package tv

import (
	"context"
	"errors"
	"testing"

	dom "github.com/brandenk514/mediarr/internal/domains/tv"
	"github.com/brandenk514/mediarr/internal/providers"
)

// Issue #157: TV detail metadata enrichment. The TMDB series detail
// payload is fetched on the detail read, mapped onto the enrichment wire
// fields (overview, genres, keywords, scores, crew, cast, network, first
// air, status, language, production countries), and persisted on the row
// so list reads serve it without a per-row provider call (the #118/#149
// house pattern). Keyless (no Detailer) degrades to empty/nil fields —
// never an error, never "not configured" text.

// fakeDetailer is an in-memory SeriesDetailer for the #157 tests.
type fakeDetailer struct {
	search     []providers.Result
	detail     map[string]*providers.SeriesDetail // tmdbID -> detail
	searchErr  error
	detailErr  error
	searchCall bool
	detailCall int
}

func (f *fakeDetailer) SearchTV(ctx context.Context, q string) ([]providers.Result, error) {
	f.searchCall = true
	return f.search, f.searchErr
}

func (f *fakeDetailer) GetSeriesDetail(ctx context.Context, tmdbID string) (*providers.SeriesDetail, error) {
	f.detailCall++
	if f.detailErr != nil {
		return nil, f.detailErr
	}
	return f.detail[tmdbID], nil
}

// sampleDetail builds a full payload for the mapping tests.
func sampleDetail() *providers.SeriesDetail {
	return &providers.SeriesDetail{
		TmdbID:              "1396",
		Overview:            "A drug kingpin becomes a chemistry teacher.",
		OriginalLanguage:    "en",
		VoteAverage:         8.9,
		VoteCount:           21543,
		FirstAirDate:        "2008-01-20",
		Status:              "Ended",
		Network:             "AMC",
		Genres:              []string{"Drama", "Crime"},
		Keywords:            []string{"drug", "mexico"},
		ProductionCountries: []string{"United States of America"},
		ImdbID:              "tt0903747",
		TraktID:             "1541",
		TrailerURL:          "https://www.youtube.com/watch?v=bb",
		PosterPath:          "/bb-poster.jpg",
		BackdropPath:        "/bb-backdrop.jpg",
		Cast:                []providers.CastCredit{{Name: "Bryan Cranston", Role: "Walter White"}},
		Crew:                []providers.CrewCredit{{Name: "Vince Gilligan", Job: "Creator"}},
	}
}

func TestEnrichSeriesDetail_MapsPayload(t *testing.T) {
	d := &fakeDetailer{detail: map[string]*providers.SeriesDetail{"1396": sampleDetail()}}
	s := &dom.Series{TMDBID: 1396}
	enrichSeriesDetail(context.Background(), d, s)

	if s.Overview == nil || *s.Overview != "A drug kingpin becomes a chemistry teacher." {
		t.Fatalf("Overview = %v", s.Overview)
	}
	if len(s.Genres) != 2 || s.Genres[0] != "Drama" {
		t.Fatalf("Genres: %v", s.Genres)
	}
	if len(s.Keywords) != 2 || s.Keywords[0] != "drug" {
		t.Fatalf("Keywords: %v", s.Keywords)
	}
	if s.Status == nil || *s.Status != "Ended" {
		t.Fatalf("Status = %v", s.Status)
	}
	if s.OriginalLanguage == nil || *s.OriginalLanguage != "en" {
		t.Fatalf("OriginalLanguage = %v", s.OriginalLanguage)
	}
	if s.Network == nil || *s.Network != "AMC" {
		t.Fatalf("Network = %v", s.Network)
	}
	if s.FirstAirDate != "2008-01-20" {
		t.Fatalf("FirstAirDate = %q", s.FirstAirDate)
	}
	if len(s.ProductionCountries) != 1 || s.ProductionCountries[0] != "United States of America" {
		t.Fatalf("ProductionCountries: %v", s.ProductionCountries)
	}
	// Score scaling: TMDB 0-10 vote_average -> 0-100 TmdbPct (movie
	// convention).
	if s.Scores == nil || s.Scores.TmdbPct == nil || *s.Scores.TmdbPct != 89 {
		t.Fatalf("Scores = %+v (want TmdbPct 89 = 8.9 * 10)", s.Scores)
	}
	// External id mappings (ids, not ratings).
	if s.ImdbID != "tt0903747" || s.TraktID != "1541" {
		t.Fatalf("external ids: %q/%q", s.ImdbID, s.TraktID)
	}
	if s.TrailerURL != "https://www.youtube.com/watch?v=bb" {
		t.Fatalf("TrailerURL = %q", s.TrailerURL)
	}
	// Artwork: upstream TMDB URLs at the house sizes (w342 poster,
	// w1280 backdrop), never proxy URLs.
	if want := "https://image.tmdb.org/t/p/w342/bb-poster.jpg"; s.PosterURL != want {
		t.Fatalf("PosterURL = %q, want %q", s.PosterURL, want)
	}
	if want := "https://image.tmdb.org/t/p/w1280/bb-backdrop.jpg"; s.BackdropURL != want {
		t.Fatalf("BackdropURL = %q, want %q", s.BackdropURL, want)
	}
	if len(s.Cast) != 1 || s.Cast[0].Name != "Bryan Cranston" || s.Cast[0].Role != "Walter White" {
		t.Fatalf("Cast: %+v", s.Cast)
	}
	if len(s.Crew) != 1 || s.Crew[0].Name != "Vince Gilligan" || s.Crew[0].Job != "Creator" {
		t.Fatalf("Crew: %+v", s.Crew)
	}
	if d.detailCall != 1 {
		t.Fatalf("detail calls = %d, want 1", d.detailCall)
	}
}

func TestEnrichSeriesDetail_NilAndFailureSafe(t *testing.T) {
	// nil Detailer (keyless): no panic, fields stay empty/nil.
	s := &dom.Series{TMDBID: 1396}
	enrichSeriesDetail(context.Background(), nil, s)
	if s.Overview != nil || s.Genres != nil || s.Scores != nil || s.PosterURL != "" || s.BackdropURL != "" {
		t.Fatalf("nil detailer must leave fields empty: %+v", s)
	}

	// Provider error: fields stay empty (enrichment never fails the read).
	bad := &fakeDetailer{detailErr: errors.New("tmdb down"), detail: map[string]*providers.SeriesDetail{}}
	s2 := &dom.Series{TMDBID: 1396}
	enrichSeriesDetail(context.Background(), bad, s2)
	if s2.Overview != nil || s2.PosterURL != "" {
		t.Fatalf("error detail must leave fields empty: %+v", s2)
	}

	// Payload missing for the id: fields stay empty.
	miss := &fakeDetailer{detail: map[string]*providers.SeriesDetail{}}
	s3 := &dom.Series{TMDBID: 1396}
	enrichSeriesDetail(context.Background(), miss, s3)
	if s3.Overview != nil {
		t.Fatalf("missing detail must leave fields empty: %+v", s3)
	}

	// Zero vote_average: Scores stays nil (no invented 0% score).
	zero := &fakeDetailer{detail: map[string]*providers.SeriesDetail{"1396": {TmdbID: "1396"}}}
	s4 := &dom.Series{TMDBID: 1396}
	enrichSeriesDetail(context.Background(), zero, s4)
	if s4.Scores != nil {
		t.Fatalf("zero vote must leave Scores nil: %+v", s4.Scores)
	}
}

func TestEnrichSeriesDetail_SearchFallback(t *testing.T) {
	// No stored TMDB id: resolve via title search (the #149 TVDB-only
	// fallback), then fetch the detail.
	d := &fakeDetailer{
		search: []providers.Result{{ID: "1396", Title: "Breaking Bad", Year: 2008}},
		detail: map[string]*providers.SeriesDetail{"1396": sampleDetail()},
	}
	s := &dom.Series{Title: "Breaking Bad", Year: 2008}
	enrichSeriesDetail(context.Background(), d, s)
	if !d.searchCall {
		t.Fatal("expected a title search for the TMDBID-less series")
	}
	if s.Overview == nil || *s.Overview != "A drug kingpin becomes a chemistry teacher." {
		t.Fatalf("fallback Overview = %v", s.Overview)
	}

	// A non-matching hit is skipped; the year-matched hit wins.
	yr := &fakeDetailer{
		search: []providers.Result{
			{ID: "222", Title: "Breaking Bad", Year: 2005},
			{ID: "1396", Title: "Breaking Bad", Year: 2008},
		},
		detail: map[string]*providers.SeriesDetail{"1396": sampleDetail()},
	}
	s2 := &dom.Series{Title: "Breaking Bad", Year: 2008}
	enrichSeriesDetail(context.Background(), yr, s2)
	if s2.Overview == nil || *s2.Overview != "A drug kingpin becomes a chemistry teacher." {
		t.Fatalf("year-matched fallback Overview = %v", s2.Overview)
	}

	// No search hits: fields stay empty.
	none := &fakeDetailer{search: nil, detail: map[string]*providers.SeriesDetail{}}
	s3 := &dom.Series{Title: "Unknown Show"}
	enrichSeriesDetail(context.Background(), none, s3)
	if s3.Overview != nil {
		t.Fatalf("no-hit detail must leave fields empty: %+v", s3)
	}

	// Search failure (e.g. keyless ErrNotConfigured): fields stay empty.
	sErr := &fakeDetailer{searchErr: errors.New("not configured"), detail: map[string]*providers.SeriesDetail{}}
	s4 := &dom.Series{Title: "Unknown Show"}
	enrichSeriesDetail(context.Background(), sErr, s4)
	if s4.Overview != nil {
		t.Fatalf("search failure must leave fields empty: %+v", s4)
	}
}

func TestGetSeries_EnrichesAndPersistsDetail(t *testing.T) {
	// The detail read enriches from one provider call and persists the
	// metadata + artwork on the row; the LIST read must then serve the
	// enriched values from persisted data — no second provider call (the
	// #118/#149 house pattern: enrich on the detail read, never on list).
	d := &fakeDetailer{detail: map[string]*providers.SeriesDetail{"1396": sampleDetail()}}
	repo := newMemRepo()
	svc := New(Deps{Repo: repo, Detailer: d})

	id, err := repo.CreateSeries(context.Background(), dom.Series{TMDBID: 1396, Title: "Breaking Bad", Year: 2008})
	if err != nil {
		t.Fatalf("CreateSeries: %v", err)
	}
	detail, err := svc.GetSeries(context.Background(), id)
	if err != nil {
		t.Fatalf("GetSeries: %v", err)
	}
	if detail.Overview == nil || *detail.Overview != "A drug kingpin becomes a chemistry teacher." {
		t.Fatalf("detail Overview = %v", detail.Overview)
	}
	if detail.Scores == nil || detail.Scores.TmdbPct == nil || *detail.Scores.TmdbPct != 89 {
		t.Fatalf("detail Scores = %+v", detail.Scores)
	}
	// Arrays serialize as [] (never null) per the OpenAPI contract.
	if detail.Genres == nil || detail.Keywords == nil || detail.ProductionCountries == nil || detail.Cast == nil || detail.Crew == nil {
		t.Fatalf("detail arrays must be non-nil: %+v", detail)
	}

	list, err := svc.ListSeries(context.Background())
	if err != nil {
		t.Fatalf("ListSeries: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("list len = %d, want 1", len(list))
	}
	// The list serves the PERSISTED metadata (the flat facets, the #150
	// MovieMetadata pattern) — not the detail-only fields (Overview,
	// cast/crew, trailer, external ids), which are fetched on the detail
	// read and intentionally not persisted.
	if got := list[0].Overview; got != nil {
		t.Fatalf("list Overview = %v, want nil (detail-only, not persisted)", got)
	}
	if got := list[0].Genres; len(got) != 2 || got[0] != "Drama" {
		t.Fatalf("list Genres = %v, want persisted value", got)
	}
	if got := list[0].Status; got == nil || *got != "Ended" {
		t.Fatalf("list Status = %v, want persisted value", got)
	}
	if got := list[0].FirstAirDate; got != "2008-01-20" {
		t.Fatalf("list FirstAirDate = %q, want persisted value", got)
	}
	if got := list[0].Scores; got == nil || got.TmdbPct == nil || *got.TmdbPct != 89 {
		t.Fatalf("list Scores = %v, want persisted value", got)
	}
	if want := "https://image.tmdb.org/t/p/w342/bb-poster.jpg"; list[0].PosterURL != want {
		t.Fatalf("list PosterURL = %q, want %q (persisted)", list[0].PosterURL, want)
	}
	if len(list[0].Cast) != 0 || len(list[0].Crew) != 0 {
		t.Fatalf("list cast/crew must be [] (detail-only, not persisted): %+v / %+v", list[0].Cast, list[0].Crew)
	}
	if d.detailCall != 1 {
		t.Fatalf("provider detail calls = %d, want 1 (list must not enrich)", d.detailCall)
	}
}

func TestGetSeries_KeylessDetailEmpty(t *testing.T) {
	// No Detailer dep (keyless dev stack): the detail read succeeds with
	// every enrichment field empty/nil — never an error, never raw
	// "not configured" text (R3 convention).
	repo := newMemRepo()
	svc := New(Deps{Repo: repo})
	id, err := repo.CreateSeries(context.Background(), dom.Series{Title: "Old Show", Year: 1999})
	if err != nil {
		t.Fatalf("CreateSeries: %v", err)
	}
	detail, err := svc.GetSeries(context.Background(), id)
	if err != nil {
		t.Fatalf("GetSeries: %v", err)
	}
	if detail.Overview != nil || detail.Status != nil || detail.Network != nil || detail.OriginalLanguage != nil || detail.Scores != nil {
		t.Fatalf("keyless detail must have nil enrichment pointers: %+v", detail)
	}
	if detail.FirstAirDate != "" || detail.ImdbID != "" || detail.TraktID != "" || detail.TrailerURL != "" {
		t.Fatalf("keyless detail must have empty string fields: %+v", detail)
	}
	if detail.Genres == nil || detail.Keywords == nil || detail.ProductionCountries == nil || detail.Cast == nil || detail.Crew == nil {
		t.Fatalf("keyless detail arrays must serialize as []: %+v", detail)
	}
}
