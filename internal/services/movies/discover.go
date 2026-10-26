package movies

import (
	"context"
	"errors"

	"github.com/brandenk514/mediarr/internal/providers"
)

// TMDBDiscoverer is the narrow provider surface the Discover page needs
// (Radarr R3). *providers.TMDB satisfies it; tests use a fake.
type TMDBDiscoverer interface {
	DiscoverMovieHits(ctx context.Context, q providers.DiscoverQuery) ([]providers.MovieSearchHit, int, error)
}

// ErrTMDBUnavailable means Discover cannot run — the TMDB client is not
// wired (no API key). The API maps this to a 503 with a reason, the same
// keyless shape /search/movies uses.
var ErrTMDBUnavailable = errors.New("movies: tmdb is not configured")

// Discover is a page of the TMDB trending-movies feed (Radarr R3 Discover
// page). genre and year are optional filters; page is 1-based (values below
// 1 are clamped upstream). With no TMDB client it returns
// ErrTMDBUnavailable; TMDB errors (including the unknown-genre
// ErrUnknownGenre) propagate so the API can map them to statuses.
func (s *Service) Discover(ctx context.Context, page int, genre string, year int) ([]providers.MovieSearchHit, int, error) {
	if s.deps.TMDB == nil {
		return nil, 0, ErrTMDBUnavailable
	}
	return s.deps.TMDB.DiscoverMovieHits(ctx, providers.DiscoverQuery{
		Page:  page,
		Genre: genre,
		Year:  year,
	})
}
