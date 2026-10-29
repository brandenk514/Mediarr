package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/brandenk514/mediarr/internal/indexers"
	indexerdefinitions "github.com/brandenk514/mediarr/internal/indexers/definitions"
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
	// Definitions is the definition-library store (Prowlarr PR2) behind
	// GET /api/v1/indexer/definitions. The route is only registered when it
	// is wired, so existing callers that wire only the read/write deps keep
	// working unchanged.
	Definitions indexerdefinitions.Store
}

// SetIndexers wires the indexer endpoints (#35 read/health, #44 write). Called
// after construction, mirroring SetAuth/SetMovies, so NewServer stays usable
// for health-only boot and tests. The write routes are only registered when
// Admin is non-nil, so an existing caller that wires only the read deps keeps
// working (and the write surface is deliberately gated behind a registry).
func (s *Server) SetIndexers(deps *IndexersDeps) {
	s.indexersDeps = deps
	s.register("GET /api/v1/indexers", s.requireAuth(s.ListIndexers))
	s.register("POST /api/v1/indexers/test-all", s.requireAuth(s.TestAllIndexers))
	s.register("POST /api/v1/indexers/{id}/test", s.requireAuth(s.TestIndexer))
	s.register("GET /api/v1/indexers/{id}/stats", s.requireAuth(s.IndexerStats))
	if deps.Admin != nil {
		s.register("POST /api/v1/indexers", s.requireAuth(s.CreateIndexer))
		s.register("PUT /api/v1/indexers/{id}", s.requireAuth(s.UpdateIndexer))
		s.register("PATCH /api/v1/indexers/{id}/enabled", s.requireAuth(s.SetIndexerEnabled))
		s.register("DELETE /api/v1/indexers/{id}", s.requireAuth(s.DeleteIndexer))
	}
	if deps.Definitions != nil {
		s.register("GET /api/v1/indexer/definitions", s.requireAuth(s.ListIndexerDefinitions))
	}
}

// indexerJSON is the wire shape for a single indexer definition. APIKey is
// deliberately absent (#35: "no secrets in responses — keys never returned,
// only masked presence").
type indexerJSON struct {
	ID                int64      `json:"id"`
	Name              string     `json:"name"`
	Kind              string     `json:"kind"`
	BaseURL           string     `json:"base_url"`
	Enabled           bool       `json:"enabled"`
	HasAPIKey         bool       `json:"has_api_key"`
	LastTest          *time.Time `json:"last_test"`
	RSS               bool       `json:"rss"`
	AutoSearch        bool       `json:"auto_search"`
	InteractiveSearch bool       `json:"interactive_search"`
	// Prowlarr PR1 indexer-hub list columns.
	Privacy     string    `json:"privacy"`
	Priority    int       `json:"priority"`
	SyncProfile string    `json:"sync_profile"`
	Added       time.Time `json:"added"`
	Categories  []string  `json:"categories"`
	// Prowlarr PR2 definition-library fields. ConfigFields carries the
	// per-indexer contract values with password fields masked to "" (secrets
	// are never echoed); DefinitionName names the library definition the
	// indexer was added from (null on the legacy path).
	ConfigFields   map[string]any `json:"config_fields"`
	DefinitionName *string        `json:"definition_name"`
}

func defToJSON(d indexers.Definition) indexerJSON {
	t := indexers.ParseIndexerSettings(d.SettingsJSON)
	privacy := d.Privacy
	if privacy == "" {
		privacy = "public"
	}
	syncProfile := d.SyncProfile
	if syncProfile == "" {
		syncProfile = "standard"
	}
	priority := d.Priority
	if priority == 0 {
		priority = 25
	}
	return indexerJSON{
		ID:                d.ID,
		Name:              d.Name,
		Kind:              d.Kind,
		BaseURL:           d.BaseURL,
		Enabled:           d.Enabled,
		HasAPIKey:         d.APIKey != "",
		LastTest:          d.LastTest,
		RSS:               t.RSS,
		AutoSearch:        t.AutoSearch,
		InteractiveSearch: t.InteractiveSearch,
		Privacy:           privacy,
		Priority:          priority,
		SyncProfile:       syncProfile,
		Added:             d.Added,
		Categories:        parseCategories(d.SettingsJSON),
		ConfigFields:      maskConfigFields(&d),
		DefinitionName:    d.DefinitionNamePtr(),
	}
}

// maskConfigFields returns the response form of an indexer's contract values:
// an empty object on the legacy path, or a copy with every password field
// masked to "" (matching the has_api_key convention: secrets are never
// echoed).
func maskConfigFields(d *indexers.Definition) map[string]any {
	out := map[string]any{}
	if len(d.ConfigFields) == 0 {
		return out
	}
	contract := (*indexerdefinitions.Definition)(nil)
	if d.DefinitionName != "" {
		contract = indexerdefinitions.Default().ResolveDefinition(d.DefinitionName)
	}
	return indexerdefinitions.MaskSecrets(contract, d.ConfigFields)
}

// parseCategories pulls the indexer's category badges out of its settings_json
// bag (PR1 list parity: "category badges"). The bag may carry categories as a
// JSON array of strings or a comma-separated string; a missing/empty value
// yields an empty slice (the UI renders a dash).
func parseCategories(raw string) []string {
	if raw == "" {
		return []string{}
	}
	var bag map[string]any
	if err := json.Unmarshal([]byte(raw), &bag); err != nil {
		return []string{}
	}
	v, ok := bag["categories"]
	if !ok {
		return []string{}
	}
	switch c := v.(type) {
	case string:
		out := []string{}
		for _, p := range strings.Split(c, ",") {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
		return out
	case []any:
		out := []string{}
		for _, e := range c {
			if s, ok := e.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return []string{}
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

// TestAllIndexers handles POST /api/v1/indexers/test-all: the "Test All
// Indexers" toolbar action (Prowlarr PR1). It fans out a health probe over
// every enabled indexer through the shared worker and returns the per-indexer
// outcomes. A per-indexer failure is a 200 with ok=false for that indexer; only
// a hard failure to load the definition list is an HTTP error.
func (s *Server) TestAllIndexers(w http.ResponseWriter, r *http.Request) {
	results, err := s.indexersDeps.Worker.TestAllResults(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, results)
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
	// Prowlarr PR1 hub fields (zero/empty = wire default).
	Privacy     string `json:"privacy"`
	Priority    int    `json:"priority"`
	SyncProfile string `json:"sync_profile"`
	// Radarr R5a per-indexer toggles; nil = default true.
	RSS               *bool `json:"rss,omitempty"`
	AutoSearch        *bool `json:"auto_search,omitempty"`
	InteractiveSearch *bool `json:"interactive_search,omitempty"`
	// Prowlarr PR2 config-contract fields. When ConfigFields is present it
	// must validate against the named definition's contract (DefinitionName
	// identifies the library definition by id or display name). The form's
	// base_url is authoritative for the stored base_url column and is
	// mirrored into the contract's baseurl value for adapter parity.
	ConfigFields   *map[string]any `json:"config_fields,omitempty"`
	DefinitionName string          `json:"definition_name,omitempty"`
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
	// Prowlarr PR1 hub fields; nil = keep the stored value.
	Privacy     *string `json:"privacy,omitempty"`
	Priority    *int    `json:"priority,omitempty"`
	SyncProfile *string `json:"sync_profile,omitempty"`
	// Radarr R5a per-indexer toggles; nil = keep the stored value.
	RSS               *bool `json:"rss,omitempty"`
	AutoSearch        *bool `json:"auto_search,omitempty"`
	InteractiveSearch *bool `json:"interactive_search,omitempty"`
	// Prowlarr PR2 config-contract fields (same semantics as on create). A
	// nil ConfigFields keeps the stored contract untouched; an empty/absent
	// password value keeps the stored value (masked-edit round-trip).
	// DefinitionName omitted = the stored name.
	ConfigFields   *map[string]any `json:"config_fields,omitempty"`
	DefinitionName string          `json:"definition_name,omitempty"`
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

// resolveConfigFields implements the Prowlarr PR2 config-contract write path
// shared by create and update:
//   - nil reqFields is the legacy path: stored values (if any) pass through
//     untouched, else nothing is stored;
//   - present reqFields must name a known library definition (by id or
//     display name; on update an omitted name falls back to the stored one),
//     and validate against its contract (definitions.Resolve);
//   - the form's base_url is authoritative for the stored base_url column and
//     is mirrored into the contract's baseurl value so config_fields is
//     self-contained for future adapters (PR6);
//   - on update an empty/absent password value keeps the stored value
//     (masked-edit round-trip, same convention as the legacy api_key).
//
// All returned errors are 400-level contract violations (or a missing
// definition name); the caller maps them straight to a 400.
func (s *Server) resolveConfigFields(reqFields *map[string]any, reqDefName, reqBaseURL string, existing *indexers.Definition) (map[string]any, string, error) {
	if reqFields == nil {
		if existing != nil {
			return existing.ConfigFields, existing.DefinitionName, nil
		}
		return nil, "", nil
	}
	name := reqDefName
	if name == "" && existing != nil {
		name = existing.DefinitionName
	}
	if name == "" {
		return nil, "", errors.New("definition_name is required when config_fields is provided")
	}
	contract := indexerdefinitions.Default().ResolveDefinition(name)
	if contract == nil {
		return nil, "", fmt.Errorf("unknown definition %q", name)
	}
	values := make(map[string]any, len(*reqFields))
	for k, v := range *reqFields {
		values[k] = v
	}
	if contract.Field("baseurl") != nil {
		effective := reqBaseURL
		if effective == "" && existing != nil {
			if stored, ok := existing.ConfigFields["baseurl"].(string); ok {
				effective = stored
			}
			if effective == "" {
				effective = existing.BaseURL
			}
		}
		values["baseurl"] = effective
	}
	if existing != nil {
		for _, secret := range contract.SecretFieldNames() {
			keep := false
			if v, ok := values[secret]; !ok {
				keep = true
			} else if str, isStr := v.(string); isStr && str == "" {
				keep = true
			}
			if keep {
				if stored, ok := existing.ConfigFields[secret]; ok {
					values[secret] = stored
				}
			}
		}
	}
	resolved, err := indexerdefinitions.Resolve(contract, values)
	if err != nil {
		return nil, "", err
	}
	// Store the canonical Cardigann id regardless of whether the client sent
	// an id or a display name.
	return resolved, contract.ID, nil
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
	// Radarr R5a: resolve the per-indexer toggles (omitted = factory default
	// true) and store them in the settings bag, preserving any other keys.
	toggles := indexers.DefaultIndexerSettings()
	if req.RSS != nil {
		toggles.RSS = *req.RSS
	}
	if req.AutoSearch != nil {
		toggles.AutoSearch = *req.AutoSearch
	}
	if req.InteractiveSearch != nil {
		toggles.InteractiveSearch = *req.InteractiveSearch
	}
	settingsJSON := indexers.WithIndexerToggles(req.Settings, toggles)
	name := strings.TrimSpace(req.Name)
	// Prowlarr PR2: resolve the config-contract values (nil on the legacy
	// path) against the named definition's contract before anything is
	// stored.
	configFields, defName, err := s.resolveConfigFields(req.ConfigFields, req.DefinitionName, req.BaseURL, nil)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Prowlarr PR1 hub fields (zero/empty = wire default).
	privacy := req.Privacy
	if privacy == "" {
		privacy = "public"
	}
	syncProfile := req.SyncProfile
	if syncProfile == "" {
		syncProfile = "standard"
	}
	priority := req.Priority
	if priority == 0 {
		priority = 25
	}
	id, err := s.indexersDeps.Repo.Create(r.Context(), indexers.Definition{
		Name:           name,
		Kind:           req.Kind,
		BaseURL:        req.BaseURL,
		APIKey:         req.APIKey,
		SettingsJSON:   settingsJSON,
		Enabled:        enabled,
		Privacy:        privacy,
		Priority:       priority,
		SyncProfile:    syncProfile,
		DefinitionName: defName,
		ConfigFields:   configFields,
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
		ID:             id,
		Name:           name,
		Kind:           req.Kind,
		BaseURL:        req.BaseURL,
		APIKey:         req.APIKey,
		SettingsJSON:   settingsJSON,
		Enabled:        enabled,
		Privacy:        privacy,
		Priority:       priority,
		SyncProfile:    syncProfile,
		Added:          time.Now().UTC(),
		DefinitionName: defName,
		ConfigFields:   configFields,
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
	// Radarr R5a: toggles update over the stored bag (an explicit
	// settings_json in the body becomes the base), omitted toggles keep the
	// stored value.
	baseSettings := req.Settings
	if baseSettings == "" {
		baseSettings = existing.SettingsJSON
	}
	toggles := indexers.ParseIndexerSettings(baseSettings)
	if req.RSS != nil {
		toggles.RSS = *req.RSS
	}
	if req.AutoSearch != nil {
		toggles.AutoSearch = *req.AutoSearch
	}
	if req.InteractiveSearch != nil {
		toggles.InteractiveSearch = *req.InteractiveSearch
	}
	settingsJSON := indexers.WithIndexerToggles(baseSettings, toggles)
	// Prowlarr PR1 hub fields: omitted (nil) keeps the stored value.
	privacy := existing.Privacy
	if req.Privacy != nil {
		privacy = *req.Privacy
	}
	syncProfile := existing.SyncProfile
	if req.SyncProfile != nil {
		syncProfile = *req.SyncProfile
	}
	priority := existing.Priority
	if req.Priority != nil {
		priority = *req.Priority
	}
	// Prowlarr PR2: resolve the config-contract values against the named
	// definition's contract (a nil req.ConfigFields keeps the stored values).
	configFields, defName, err := s.resolveConfigFields(req.ConfigFields, req.DefinitionName, req.BaseURL, existing)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	def := indexers.Definition{
		ID:             id,
		Name:           strings.TrimSpace(req.Name),
		Kind:           req.Kind,
		BaseURL:        req.BaseURL,
		APIKey:         apiKey,
		SettingsJSON:   settingsJSON,
		Enabled:        existing.Enabled,
		LastTest:       existing.LastTest,
		Privacy:        privacy,
		Priority:       priority,
		SyncProfile:    syncProfile,
		Added:          existing.Added,
		DefinitionName: defName,
		ConfigFields:   configFields,
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
