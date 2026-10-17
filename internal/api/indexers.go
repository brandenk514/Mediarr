package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/brandenk514/mediarr/internal/indexers"
)

// IndexersDeps bundles what the indexer endpoints need. Worker is the health
// worker (#34) that probes indexers; Tracker is the shared per-indexer rollup
// (#34) that the stats endpoint reads; Repo loads the definitions the list
// endpoint reports. Admin is the provider registry the write endpoints (#44)
// validate a definition's kind against, so a definition can only be saved with
// a protocol this build can actually materialize. All shared instances are the
// same ones the live search path uses, so stats reflect real traffic, not just
// on-demand probes.
type IndexersDeps struct {
	Repo    indexers.IndexerRepo
	Worker  *indexers.HealthWorker
	Tracker indexers.HealthTracker
	Admin   *indexers.Registry
}

// SetIndexers wires the indexer endpoints (#35 read/health, #44 write). Called
// after construction, mirroring SetAuth/SetMovies, so NewServer stays usable
// for health-only boot and tests. The write routes are only registered when
// Admin is non-nil, so an existing caller that wires only the read deps keeps
// working (and the write surface is deliberately gated behind a registry).
func (s *Server) SetIndexers(deps *IndexersDeps) {
	s.indexersDeps = deps
	s.register("GET /api/v1/indexers", s.requireAuth(s.ListIndexers))
	s.register("POST /api/v1/indexers/{id}/test", s.requireAuth(s.TestIndexer))
	s.register("GET /api/v1/indexers/{id}/stats", s.requireAuth(s.IndexerStats))
	if deps.Admin != nil {
		s.register("POST /api/v1/indexers", s.requireAuth(s.CreateIndexer))
		s.register("PUT /api/v1/indexers/{id}", s.requireAuth(s.UpdateIndexer))
		s.register("PATCH /api/v1/indexers/{id}/enabled", s.requireAuth(s.SetIndexerEnabled))
		s.register("DELETE /api/v1/indexers/{id}", s.requireAuth(s.DeleteIndexer))
	}
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

// ---- write endpoints (#44) ----

// indexerWriteRequest is the body for POST /api/v1/indexers (create). APIKey is
// plaintext in the request but is never echoed back in the response (defToJSON
// only reports has_api_key). An empty APIKey is legal (keyless indexers).
type indexerWriteRequest struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	BaseURL  string `json:"base_url"`
	APIKey   string `json:"api_key"`
	Settings string `json:"settings_json,omitempty"`
	Enabled  *bool  `json:"enabled,omitempty"`
}

// indexerUpdateRequest is the body for PUT /api/v1/indexers/{id}. An empty
// APIKey means "keep the existing key" (the masked-edit round-trip, so the UI
// can change name/kind/base without re-sending a stored credential).
type indexerUpdateRequest struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	BaseURL  string `json:"base_url"`
	APIKey   string `json:"api_key"`
	Settings string `json:"settings_json,omitempty"`
}

// indexerEnabledRequest is the body for PATCH /api/v1/indexers/{id}/enabled.
type indexerEnabledRequest struct {
	Enabled bool `json:"enabled"`
}

// validateIndexerDef returns a human-readable error if the name/kind are
// invalid for the write endpoints, else "". The kind must be one the build can
// actually materialize (a registry Lookup), so a definition can never be saved
// in a state the search fan-out cannot use.
func validateIndexerDef(name, kind string, reg *indexers.Registry) string {
	if strings.TrimSpace(name) == "" {
		return "indexer name is required"
	}
	if reg != nil {
		if kind == "" {
			return "indexer kind is required"
		}
		if !reg.Lookup(kind) {
			return "unknown indexer kind " + strconv.Quote(kind)
		}
	}
	return ""
}

// CreateIndexer handles POST /api/v1/indexers. A duplicate name is a 409
// (the name column is unique); validation failures are 400.
func (s *Server) CreateIndexer(w http.ResponseWriter, r *http.Request) {
	var req indexerWriteRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if msg := validateIndexerDef(req.Name, req.Kind, s.indexersDeps.Admin); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	settingsJSON := req.Settings
	if settingsJSON == "" {
		settingsJSON = "{}"
	}
	name := strings.TrimSpace(req.Name)
	id, err := s.indexersDeps.Repo.Create(r.Context(), indexers.Definition{
		Name:         name,
		Kind:         req.Kind,
		BaseURL:      req.BaseURL,
		APIKey:       req.APIKey,
		SettingsJSON: settingsJSON,
		Enabled:      enabled,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "an indexer named "+strconv.Quote(name)+" already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// No read-back: we know every field. A fresh create has no last_test.
	writeJSON(w, http.StatusCreated, defToJSON(indexers.Definition{
		ID: id, Name: name, Kind: req.Kind, BaseURL: req.BaseURL,
		APIKey: req.APIKey, Enabled: enabled,
	}))
}

// UpdateIndexer handles PUT /api/v1/indexers/{id}. An empty APIKey preserves the
// stored key. A name that collides with another indexer is a 409; an unknown
// id is a 404.
func (s *Server) UpdateIndexer(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req indexerUpdateRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if msg := validateIndexerDef(req.Name, req.Kind, s.indexersDeps.Admin); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	existing, err := s.indexersDeps.Repo.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "unknown indexer")
		return
	}
	apiKey := req.APIKey
	if apiKey == "" {
		apiKey = existing.APIKey // masked-edit: empty means "keep"
	}
	settingsJSON := req.Settings
	if settingsJSON == "" {
		settingsJSON = existing.SettingsJSON
	}
	def := indexers.Definition{
		ID:           id,
		Name:         strings.TrimSpace(req.Name),
		Kind:         req.Kind,
		BaseURL:      req.BaseURL,
		APIKey:       apiKey,
		SettingsJSON: settingsJSON,
		Enabled:      existing.Enabled,
		LastTest:     existing.LastTest,
	}
	if err := s.indexersDeps.Repo.Update(r.Context(), def); err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "an indexer named "+strconv.Quote(req.Name)+" already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// No read-back: every field is known (updated value + carried-over
	// enabled/last_test from the pre-image).
	writeJSON(w, http.StatusOK, defToJSON(def))
}

// SetIndexerEnabled handles PATCH /api/v1/indexers/{id}/enabled. A Get-first
// existence check yields the 404 for an unknown id: it is the one repo method
// that returns sql.ErrNoRows consistently for both the Postgres repo and the
// in-memory test fake (the toggle/delete methods surface a missing row as a
// plain error), so the API never returns 500 for a deleted indexer.
func (s *Server) SetIndexerEnabled(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req indexerEnabledRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	existing, err := s.indexersDeps.Repo.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "unknown indexer")
		return
	}
	if err := s.indexersDeps.Repo.SetEnabled(r.Context(), id, req.Enabled); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	existing.Enabled = req.Enabled
	writeJSON(w, http.StatusOK, defToJSON(*existing))
}

// DeleteIndexer handles DELETE /api/v1/indexers/{id}. A Get-first existence
// check yields the 404 for an unknown id (see SetIndexerEnabled for why).
func (s *Server) DeleteIndexer(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if _, err := s.indexersDeps.Repo.Get(r.Context(), id); err != nil {
		writeError(w, http.StatusNotFound, "unknown indexer")
		return
	}
	if err := s.indexersDeps.Repo.Delete(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// 204 No Content: the resource is gone, no body.
	w.WriteHeader(http.StatusNoContent)
}

// isUniqueViolation reports whether a repo error is a Postgres unique-constraint
// violation. It matches the same substrings the auth repo uses (the pgx driver
// renders the message as "duplicate key value violates unique constraint"),
// avoiding a direct dependency on the pgconn error type.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, sub := range []string{"duplicate key", "unique constraint"} {
		if strings.Contains(msg, sub) {
			return true
		}
	}
	return false
}
