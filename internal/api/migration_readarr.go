package api

import (
	"encoding/json"
	"net/http"

	"github.com/brandenk514/mediarr/internal/system"
)

// ---- Readarr B6: dedicated Readarr migration endpoints ----
//
// The generic POST /api/v1/system/migration keeps its cross-source
// semantics: source "readarr" (with dry_run / resume / include) is routed
// through MigrationService exactly like the other four sources, and
// GET /api/v1/system/migration/status reflects the most recently run
// source. These endpoints are the source-scoped shape the UI migration
// page drives: a fixed dry-run (read-only, safe to repeat), an explicit
// execute, and a source-scoped status that reports the Readarr run even
// when another source migrated most recently.

// readarrMigrationRequest is the POST body (OpenAPI ReadarrMigrationRequest).
// base_url is the Readarr instance (the client appends /api/v1); api_key is
// sent as X-Api-Key. resume (execute only) continues a partial run.
type readarrMigrationRequest struct {
	BaseURL string                   `json:"base_url"`
	APIKey  string                   `json:"api_key"`
	Resume  bool                     `json:"resume"`
	Include *system.MigrationInclude `json:"include,omitempty"`
}

// toMigrationRequest adapts the dedicated body to the shared migration
// request with the source pinned to readarr.
func (r readarrMigrationRequest) toMigrationRequest(dryRun, resume bool) system.MigrationRequest {
	return system.MigrationRequest{
		SourceURL: r.BaseURL,
		Source:    system.SourceReadarr,
		APIKey:    r.APIKey,
		DryRun:    dryRun,
		Resume:    resume,
		Include:   r.Include,
	}
}

// readarrDepsOr400 wires the migration service from the system deps and
// validates the dedicated request body; it writes the error response and
// returns ok=false when it can continue no further.
func (s *Server) readarrDepsOr400(w http.ResponseWriter, r *http.Request) (*system.MigrationService, readarrMigrationRequest, bool) {
	d, ok := s.systemDepsOr501(w)
	if !ok {
		return nil, readarrMigrationRequest{}, false
	}
	if d.Migration == nil {
		writeError(w, http.StatusNotImplemented, "migration is not wired")
		return nil, readarrMigrationRequest{}, false
	}
	var req readarrMigrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return nil, readarrMigrationRequest{}, false
	}
	if req.BaseURL == "" {
		writeError(w, http.StatusBadRequest, "base_url is required")
		return nil, readarrMigrationRequest{}, false
	}
	return d.Migration, req, true
}

// writeReadarrMigrationResult runs the readarr migration through the shared
// service and maps the outcome onto the same status codes as the generic
// endpoint: 200 (full or partial mapping report), 400 (request validation),
// 502 (the Readarr instance cannot be reached).
func (s *Server) writeReadarrMigrationResult(w http.ResponseWriter, r *http.Request, svc *system.MigrationService, req system.MigrationRequest) {
	result, err := svc.Run(r.Context(), req)
	if err != nil {
		// A partial run (result non-nil) is the resumable contract: 200
		// with the partial mapping report.
		if result != nil {
			writeJSON(w, http.StatusOK, result)
			return
		}
		if isMigrationValidationError(err) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusBadGateway, "failed to reach the Readarr instance: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// RunReadarrMigration handles POST /api/v1/system/migration/readarr: the
// Readarr drop-in dry-run. It pulls the live Readarr v0.4.18.2805 instance
// and reports the mapping WITHOUT writing anything — safe to repeat
// (nothing is persisted, so every run starts from a clean slate).
func (s *Server) RunReadarrMigration(w http.ResponseWriter, r *http.Request) {
	svc, req, ok := s.readarrDepsOr400(w, r)
	if !ok {
		return
	}
	// Dry-run ignores resume: nothing was (or is) written to resume from.
	s.writeReadarrMigrationResult(w, r, svc, req.toMigrationRequest(true, false))
}

// ExecuteReadarrMigration handles POST /api/v1/system/migration/readarr/
// execute: the Readarr drop-in import. It pulls the live Readarr instance
// and imports it into Mediarr's books domain over Postgres. Idempotent
// (re-running reports the existing entities) and resumable: a partial run
// records the migrated book keys, and a follow-up with resume true skips
// them and continues.
func (s *Server) ExecuteReadarrMigration(w http.ResponseWriter, r *http.Request) {
	svc, req, ok := s.readarrDepsOr400(w, r)
	if !ok {
		return
	}
	s.writeReadarrMigrationResult(w, r, svc, req.toMigrationRequest(false, req.Resume))
}

// GetReadarrMigrationStatus handles GET /api/v1/system/migration/readarr/
// status: the Readarr run's progress (idle / running / completed / partial
// / failed) and last mapping report — source-scoped, so it reports the
// Readarr run even when another source migrated most recently.
func (s *Server) GetReadarrMigrationStatus(w http.ResponseWriter, r *http.Request) {
	d, ok := s.systemDepsOr501(w)
	if !ok {
		return
	}
	if d.Migration == nil {
		writeError(w, http.StatusNotImplemented, "migration is not wired")
		return
	}
	writeJSON(w, http.StatusOK, d.Migration.StatusFor(system.SourceReadarr))
}
