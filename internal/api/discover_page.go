package api

import (
	"net/http"

	discoversvc "github.com/brandenk514/mediarr/internal/services/discover"
)

// DiscoverPageDeps carries the unified Discover service (issue #118 A1)
// the /api/v1/discover route uses.
type DiscoverPageDeps struct {
	Svc *discoversvc.Service
}

// SetDiscoverPage wires the unified Discover route. Called after
// construction, mirroring SetDiscover (the Radarr R3 movies route), so
// NewServer stays usable for health-only boot and tests.
func (s *Server) SetDiscoverPage(deps *DiscoverPageDeps) {
	if deps == nil {
		return
	}
	s.discoverPageDeps = deps
	s.register("GET /api/v1/discover", s.requireAuth(s.DiscoverPage))
}

// DiscoverPage handles GET /api/v1/discover (the unified Discover page,
// issue #118 A1+A2): trending / upcoming / newReleases cells for
// movies, TV, music, and books. Per-cell failures degrade to 200 +
// `unavailable` entries; only a total failure is a 5xx.
func (s *Server) DiscoverPage(w http.ResponseWriter, r *http.Request) {
	res, err := s.discoverPageDeps.Svc.Discover(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}
