package movies

import (
	"context"

	dom "github.com/brandenk514/mediarr/internal/domains/movies"
	"github.com/brandenk514/mediarr/internal/providers"
)

// TMDBDetailer is the narrow provider surface the enricher needs. *providers.TMDB
// satisfies it; tests use an in-process stub.
type TMDBDetailer interface {
	SearchMovies(ctx context.Context, q string) ([]providers.Result, error)
	GetMovieDetail(ctx context.Context, tmdbID string) (*providers.MovieDetail, error)
}

// TMDBEnricher implements dom.MovieEnricher on top of a keyed TMDB adapter.
// Movies are identified by title+year (the v1 identity), so enrichment
// resolves the TMDB id with a title search (year-matched when possible) and
// then fetches the detail payload.
//
// Graceful degradation (Radarr R1): EVERY failure — no API key, network
// error, empty search — returns nil and leaves the movie's enrichment fields
// untouched. A detail read must never fail because of a metadata provider.
type TMDBEnricher struct {
	Client TMDBDetailer
}

// EnrichMovie implements dom.MovieEnricher.
func (e *TMDBEnricher) EnrichMovie(ctx context.Context, m *dom.Movie) error {
	if e == nil || e.Client == nil || m == nil {
		return nil
	}
	id := e.resolveTMDBID(ctx, m)
	if id == "" {
		return nil
	}
	d, err := e.Client.GetMovieDetail(ctx, id)
	if err != nil || d == nil {
		return nil
	}
	e.apply(m, d)
	return nil
}

// resolveTMDBID finds the TMDB movie id for a title+year identity: a
// year-matched search hit wins; otherwise the first hit; on any failure "".
func (e *TMDBEnricher) resolveTMDBID(ctx context.Context, m *dom.Movie) string {
	results, err := e.Client.SearchMovies(ctx, m.Title)
	if err != nil || len(results) == 0 {
		return ""
	}
	var first string
	for _, r := range results {
		if r.ID == "" {
			continue
		}
		if first == "" {
			first = r.ID
		}
		if m.Year != 0 && r.Year == m.Year {
			return r.ID
		}
	}
	return first
}

// apply maps the provider payload onto the movie's display fields.
func (e *TMDBEnricher) apply(m *dom.Movie, d *providers.MovieDetail) {
	if d.Overview != "" {
		m.Overview = &d.Overview
	}
	if d.Runtime > 0 {
		m.Runtime = &d.Runtime
	}
	if d.Studio != "" {
		m.Studio = &d.Studio
	}
	if d.Genres != nil {
		m.Genres = d.Genres
	}
	if d.OriginalLanguage != "" {
		m.OriginalLanguage = &d.OriginalLanguage
	}
	if d.CollectionID != "" {
		m.CollectionID = d.CollectionID
	}
	if d.CollectionName != "" {
		m.CollectionName = &d.CollectionName
	}
	if d.Certification != "" {
		m.Certification = &d.Certification
	}
	if d.VoteAverage > 0 {
		pct := d.VoteAverage * 10 // TMDB 0-10 -> 0-100, Radarr-style
		m.Scores = &dom.MovieScores{TmdbPct: &pct}
	}
	if d.Cast != nil {
		m.Cast = make([]dom.MovieCast, 0, len(d.Cast))
		for _, c := range d.Cast {
			m.Cast = append(m.Cast, dom.MovieCast{Name: c.Name, Role: c.Role})
		}
	}
	if d.Crew != nil {
		m.Crew = make([]dom.MovieCrew, 0, len(d.Crew))
		for _, c := range d.Crew {
			m.Crew = append(m.Crew, dom.MovieCrew{Name: c.Name, Job: c.Job})
		}
	}
	// R4: persist the provider release date on the movie ("YYYY-MM-DD") so
	// the movie calendar works without a live TMDB call; a partial/malformed
	// date normalizes to '' and is left unknown.
	m.ReleaseDate = dom.NormalizeReleaseDate(d.ReleaseDate)
}
