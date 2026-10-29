package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/brandenk514/mediarr/internal/system"
)

// ---- P6 Wave 3 / Radarr R6: drop-in migration endpoints ----

// RunMigration handles POST /api/v1/system/migration: the drop-in moment. It
// pulls a live Sonarr or Radarr instance (selected by the request's source)
// and imports it into Mediarr, returning the mapping report. dry_run
// reports without writing; the run is idempotent and resumable. A bad
// source_url (or unknown source) is a 400; a source that cannot be reached
// (the source client errors) is a 502; a partial run still returns 200 with
// the partial mapping report (and resumable=true).
func (s *Server) RunMigration(w http.ResponseWriter, r *http.Request) {
	d, ok := s.systemDepsOr501(w)
	if !ok {
		return
	}
	if d.Migration == nil {
		writeError(w, http.StatusNotImplemented, "migration is not wired")
		return
	}
	var req system.MigrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	result, err := d.Migration.Run(r.Context(), req)
	if err != nil {
		// A source_url validation error is a 400. A partial run (runErr
		// non-nil but result non-nil) is reported as a 200 with the partial
		// mapping report — that is the resumable contract.
		if result != nil {
			writeJSON(w, http.StatusOK, result)
			return
		}
		if isMigrationValidationError(err) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		source := req.Source
		if source == "" {
			source = system.SourceSonarr
		}
		name := "Sonarr"
		switch source {
		case system.SourceRadarr:
			name = "Radarr"
		case system.SourceLidarr:
			name = "Lidarr"
		}
		writeError(w, http.StatusBadGateway, "failed to reach the "+name+" instance: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// GetMigrationStatus handles GET /api/v1/system/migration/status: the current
// migration progress and last mapping report.
func (s *Server) GetMigrationStatus(w http.ResponseWriter, r *http.Request) {
	d, ok := s.systemDepsOr501(w)
	if !ok {
		return
	}
	if d.Migration == nil {
		writeError(w, http.StatusNotImplemented, "migration is not wired")
		return
	}
	writeJSON(w, http.StatusOK, d.Migration.Status())
}

// isMigrationValidationError reports whether the error is a request
// validation error (a 400) rather than a transport/source failure (a 502).
// The Migrator prefixes validation errors with "migration:".
func isMigrationValidationError(err error) bool {
	return err != nil && strings.HasPrefix(err.Error(), "migration:")
}
