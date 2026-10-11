package api

import (
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/brandenk514/mediarr/internal/indexers"
)

// IndexersDeps bundles what the indexer endpoints need. Worker is the health
// worker (#34) that probes indexers; Tracker is the shared per-indexer rollup
// (#34) that the stats endpoint reads; Repo loads the definitions the list
// endpoint reports. All three are the same shared instances the live search
// path uses, so stats reflect real traffic, not just on-demand probes.
type IndexersDeps struct {
	Repo    indexers.IndexerRepo
	Worker  *indexers.HealthWorker
	Tracker indexers.HealthTracker
}

// SetIndexers wires the indexer health endpoints (#35). Called after
// construction, mirroring SetAuth/SetMovies, so NewServer stays usable for
// health-only boot and tests.
func (s *Server) SetIndexers(deps *IndexersDeps) {
	s.indexersDeps = deps
	s.mux.HandleFunc("GET /api/v1/indexers", s.requireAuth(s.ListIndexers))
	s.mux.HandleFunc("POST /api/v1/indexers/{id}/test", s.requireAuth(s.TestIndexer))
	s.mux.HandleFunc("GET /api/v1/indexers/{id}/stats", s.requireAuth(s.IndexerStats))
}

// indexerJSON is the wire shape for a single indexer definition. APIKey is
// deliberately absent (#35: "no secrets in responses — keys never returned,
// only masked presence").
type indexerJSON struct {
	ID        int64      `json:"id"`
	Name      string     `json:"name"`
	Kind      string     `json:"kind"`
	BaseURL   string     `json:"base_url"`
	Enabled   bool       `json:"enabled"`
	HasAPIKey bool       `json:"has_api_key"`
	LastTest  *time.Time `json:"last_test"`
}

func defToJSON(d indexers.Definition) indexerJSON {
	return indexerJSON{
		ID:        d.ID,
		Name:      d.Name,
		Kind:      d.Kind,
		BaseURL:   d.BaseURL,
		Enabled:   d.Enabled,
		HasAPIKey: d.APIKey != "",
		LastTest:  d.LastTest,
	}
}

// ListIndexers handles GET /api/v1/indexers: every configured indexer.
func (s *Server) ListIndexers(w http.ResponseWriter, r *http.Request) {
	defs, err := s.indexersDeps.Repo.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]indexerJSON, 0, len(defs))
	for _, d := range defs {
		out = append(out, defToJSON(d))
	}
	writeJSON(w, http.StatusOK, out)
}

// TestIndexer handles POST /api/v1/indexers/{id}/test: an on-demand health
// probe. A failed probe is a *successful* endpoint call (the indexer is
// reachable-but-broken, and that is the useful answer) — 200 with ok=false,
// not an HTTP error. Only an unknown indexer is a 404.
func (s *Server) TestIndexer(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	res, err := s.indexersDeps.Worker.TestOne(r.Context(), id)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// statsJSON is the wire shape of a per-indexer health rollup.
type statsJSON struct {
	Name         string     `json:"name"`
	Calls        int64      `json:"calls"`
	Successes    int64      `json:"successes"`
	Failures     int64      `json:"failures"`
	LastSuccess  *time.Time `json:"last_success"`
	LastError    string     `json:"last_error"`
	LastLatency  int64      `json:"last_latency_ns"`
	AvgLatency   int64      `json:"avg_latency_ns"`
	TotalLatency int64      `json:"total_latency_ns"`
}

func statsToJSON(s indexers.IndexerStats) statsJSON {
	return statsJSON{
		Name:         s.Name,
		Calls:        s.Calls,
		Successes:    s.Successes,
		Failures:     s.Failures,
		LastSuccess:  s.LastSuccess,
		LastError:    s.LastError,
		LastLatency:  int64(s.LastLatency),
		AvgLatency:   int64(s.AvgLatency),
		TotalLatency: s.TotalLatencyNs,
	}
}

// IndexerStats handles GET /api/v1/indexers/{id}/stats: the per-indexer health
// rollup. An unknown indexer yields a zero-valued stats document (200), not a
// 404: the shape is stable for the UI, and "configured but never called" is a
// legitimate state.
func (s *Server) IndexerStats(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	def, err := s.indexersDeps.Repo.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "unknown indexer")
		return
	}
	writeJSON(w, http.StatusOK, statsToJSON(s.indexersDeps.Tracker.Stats(def.Name)))
}
