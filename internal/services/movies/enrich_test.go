package movies

import (
	"context"
	"errors"
	"testing"

	dom "github.com/brandenk514/mediarr/internal/domains/movies"
	"github.com/brandenk514/mediarr/internal/providers"
)

// stubDetailer is an in-process TMDBDetailer for the enricher tests.
type stubDetailer struct {
	searchResults []providers.Result
	searchErr     error
	detail        *providers.MovieDetail
	detailErr     error
	seenQuery     string
	seenID        string
}

func (s *stubDetailer) SearchMovies(ctx context.Context, q string) ([]providers.Result, error) {
	s.seenQuery = q
	if s.searchErr != nil {
		return nil, s.searchErr
	}
	return s.searchResults, nil
}

func (s *stubDetailer) GetMovieDetail(ctx context.Context, id string) (*providers.MovieDetail, error) {
	s.seenID = id
	if s.detailErr != nil {
		return nil, s.detailErr
	}
	return s.detail, nil
}

func TestEnricher_FillsDetailFields(t *testing.T) {
	stub := &stubDetailer{
		searchResults: []providers.Result{
			{ID: "other", Title: "Inception", Year: 2020},
			{ID: "157336", Title: "Inception", Year: 2010},
		},
		detail: &providers.MovieDetail{
			TmdbID:           "157336",
			Overview:         "A mind-bending heist.",
			Runtime:          148,
			OriginalLanguage: "en",
			VoteAverage:      8.4,
			ReleaseDate:      "2010-07-16",
			Certification:    "PG-13",
			Studio:           "Legendary Pictures",
			Genres:           []string{"Action", "Sci-Fi"},
			CollectionName:   "Nolan Collection",
			Cast:             []providers.CastCredit{{Name: "Leonardo DiCaprio", Role: "Cobb"}},
			Crew:             []providers.CrewCredit{{Name: "Christopher Nolan", Job: "Director"}},
		},
	}
	e := &TMDBEnricher{Client: stub}
	m := &dom.Movie{Title: "Inception", Year: 2010}

	if err := e.EnrichMovie(context.Background(), m); err != nil {
		t.Fatalf("EnrichMovie: %v", err)
	}
	if stub.seenQuery != "Inception" {
		t.Fatalf("search query = %q", stub.seenQuery)
	}
	if stub.seenID != "157336" {
		t.Fatalf("resolved id = %q, want the year-matched hit", stub.seenID)
	}
	if m.Overview == nil || *m.Overview != "A mind-bending heist." {
		t.Fatalf("overview: %v", m.Overview)
	}
	if m.Runtime == nil || *m.Runtime != 148 {
		t.Fatalf("runtime: %v", m.Runtime)
	}
	if m.Certification == nil || *m.Certification != "PG-13" {
		t.Fatalf("certification: %v", m.Certification)
	}
	if m.Studio == nil || *m.Studio != "Legendary Pictures" {
		t.Fatalf("studio: %v", m.Studio)
	}
	if m.OriginalLanguage == nil || *m.OriginalLanguage != "en" {
		t.Fatalf("language: %v", m.OriginalLanguage)
	}
	if m.CollectionName == nil || *m.CollectionName != "Nolan Collection" {
		t.Fatalf("collection: %v", m.CollectionName)
	}
	if len(m.Genres) != 2 || m.Genres[0] != "Action" {
		t.Fatalf("genres: %v", m.Genres)
	}
	if m.Scores == nil || m.Scores.TmdbPct == nil || *m.Scores.TmdbPct != 84 {
		t.Fatalf("scores: %+v (8.4 -> 84 pct)", m.Scores)
	}
	if len(m.Cast) != 1 || m.Cast[0].Name != "Leonardo DiCaprio" || m.Cast[0].Role != "Cobb" {
		t.Fatalf("cast: %+v", m.Cast)
	}
	if len(m.Crew) != 1 || m.Crew[0].Job != "Director" {
		t.Fatalf("crew: %+v", m.Crew)
	}
	if m.ReleaseDate == nil || m.ReleaseDate.Year() != 2010 || m.ReleaseDate.Month() != 7 {
		t.Fatalf("release date: %v", m.ReleaseDate)
	}
}

func TestEnricher_DegradesGracefully(t *testing.T) {
	cases := map[string]*stubDetailer{
		"search error (no key)": {searchErr: providers.ErrNotConfigured},
		"no search results":     {},
		"detail fetch error":    {searchResults: []providers.Result{{ID: "1", Title: "X", Year: 2020}}, detailErr: errors.New("boom")},
		"detail nil":            {searchResults: []providers.Result{{ID: "1", Title: "X", Year: 2020}}},
	}
	for name, stub := range cases {
		t.Run(name, func(t *testing.T) {
			e := &TMDBEnricher{Client: stub}
			m := &dom.Movie{Title: "Inception", Year: 2010}
			if err := e.EnrichMovie(context.Background(), m); err != nil {
				t.Fatalf("EnrichMovie: %v (must never fail the read)", err)
			}
			if m.Overview != nil || m.Runtime != nil || m.Certification != nil || m.Scores != nil || m.Genres != nil || m.ReleaseDate != nil {
				t.Fatalf("fields must stay empty: %+v", *m)
			}
		})
	}
}

func TestEnricher_NilClient(t *testing.T) {
	var e *TMDBEnricher
	m := &dom.Movie{Title: "Inception"}
	if err := e.EnrichMovie(context.Background(), m); err != nil {
		t.Fatalf("nil enricher must be a no-op: %v", err)
	}
}
