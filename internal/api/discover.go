package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/brandenk514/mediarr/internal/providers"
	moviesvc "github.com/brandenk514/mediarr/internal/services/movies"
)

// DiscoverDeps carries the movie service the /api/v1/discover/movies route
// uses (Radarr R3).
type DiscoverDeps struct {
	Svc *moviesvc.Service
}

// SetDiscover wires the Radarr R3 Discover route. Called after
// construction, mirroring SetSearch, so NewServer stays usable for
// health-only boot and tests.
func (s *Server) SetDiscover(deps *DiscoverDeps) {
	if deps == nil {
		return
	}
	s.discoverDeps = deps
	s.register("GET /api/v1/discover/movies", s.requireAuth(s.DiscoverMovies))
}

// discoverMoviesResponse is the wire shape of /api/v1/discover/movies
// (Radarr R3): one page of the TMDB trending feed with the total for the
// active filter.
type discoverMoviesResponse struct {
	Results []movieSearchHit `json:"results"`
	Page    int              `json:"page"`
	Total   int              `json:"total"`
}

// DiscoverMovies handles GET /api/v1/discover/movies (Radarr R3 Discover
// page): a page of the TMDB trending-movies feed, optionally filtered by
// genre name and year. Keyless dev → 503 with the same reason shape as
// /search/movies.
func (s *Server) DiscoverMovies(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}
	year, _ := strconv.Atoi(q.Get("year"))

	hits, total, err := s.discoverDeps.Svc.Discover(r.Context(), page, q.Get("genre"), year)
	if errors.Is(err, moviesvc.ErrTMDBUnavailable) {
		writeError(w, http.StatusServiceUnavailable, "tmdb is not configured (missing API key)")
		return
	}
	if errors.Is(err, providers.ErrUnknownGenre) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		writeSearchError(w, "tmdb", err)
		return
	}

	out := make([]movieSearchHit, 0, len(hits))
	for _, h := range hits {
		out = append(out, toMovieHit(h))
	}
	writeJSON(w, http.StatusOK, discoverMoviesResponse{Results: out, Page: page, Total: total})
}
