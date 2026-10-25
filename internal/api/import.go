package api

import (
	"errors"
	"net/http"

	wdom "github.com/brandenk514/mediarr/internal/domains/wanted"
	wsvc "github.com/brandenk514/mediarr/internal/services/wanted"
)

// ImportDeps bundles what the manual-import endpoint (P3 Wanted > Manual
// Import) needs: the wanted service (which owns the import flow and
// records the global history event).
type ImportDeps struct {
	Wanted *wsvc.Service
}

// SetImport wires the manual-import endpoint:
//
//	POST /api/v1/import — manually import a wanted episode from a local file
//
// Called after construction, mirroring SetQueue.
func (s *Server) SetImport(deps *ImportDeps) {
	s.importDeps = deps
	s.register("POST /api/v1/import", s.requireAuth(s.ManualImport))
}

// manualImportRequest is the body for POST /api/v1/import.
type manualImportRequest struct {
	Kind         string `json:"kind"`
	SeriesID     int64  `json:"series_id"`
	Season       int    `json:"season"`
	Episode      int    `json:"episode"`
	FilePath     string `json:"file_path"`
	ReleaseTitle string `json:"release_title"`
}

// ManualImport handles POST /api/v1/import: records a manual import of a
// wanted episode from a local file (phase 1, TV-only). 200 with the
// report {imported, message}; 400 for a non-TV kind, non-positive
// season/episode, empty file path, or missing file; 404 for an unknown
// series or episode.
func (s *Server) ManualImport(w http.ResponseWriter, r *http.Request) {
	var req manualImportRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	rep, err := s.importDeps.Wanted.ManualImport(r.Context(), wdom.ManualImport{
		Kind:         req.Kind,
		SeriesID:     req.SeriesID,
		Season:       req.Season,
		Episode:      req.Episode,
		FilePath:     req.FilePath,
		ReleaseTitle: req.ReleaseTitle,
	})
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, rep)
	case errors.Is(err, wdom.ErrUnsupportedKind):
		writeError(w, http.StatusBadRequest, "kind must be tv for manual import (P3)")
	case errors.Is(err, wdom.ErrInvalidEpisode):
		writeError(w, http.StatusBadRequest, "season and episode must be positive")
	case errors.Is(err, wdom.ErrEmptyPath):
		writeError(w, http.StatusBadRequest, "file_path is required")
	case errors.Is(err, wdom.ErrFileNotFound):
		writeError(w, http.StatusBadRequest, "file not found on the server")
	case errors.Is(err, wdom.ErrSeriesNotFound):
		writeError(w, http.StatusNotFound, "series not found")
	case errors.Is(err, wdom.ErrEpisodeNotFound):
		writeError(w, http.StatusNotFound, "episode not found")
	default:
		writeError(w, http.StatusInternalServerError, "failed to import episode")
	}
}
