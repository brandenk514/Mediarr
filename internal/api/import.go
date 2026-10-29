package api

import (
	"errors"
	"net/http"
	"strings"

	wdom "github.com/brandenk514/mediarr/internal/domains/wanted"
	msvc "github.com/brandenk514/mediarr/internal/services/music"
	wsvc "github.com/brandenk514/mediarr/internal/services/wanted"
)

// ImportDeps bundles what the manual-import endpoint (P3 Wanted > Manual
// Import, L4 + music) needs: the wanted service (which owns the TV import
// flow and records the global history event) and, for kind=music, the music
// service (which owns the music import path).
type ImportDeps struct {
	Wanted *wsvc.Service
	Music  *msvc.Service
}

// SetImport wires the manual-import endpoint:
//
//	POST /api/v1/import — manually import a wanted episode (kind=tv) or
//	                     album/track (kind=music) from a local file
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
	// L4 (kind=music): the album the file belongs to.
	AlbumID int64 `json:"album_id"`
}

// ManualImport handles POST /api/v1/import: records a manual import of a
// wanted episode from a local file (kind=tv, P3) or of a music album/track
// (kind=music, L4). 200 with the report {imported, message}; 400 for an
// unknown kind, non-positive season/episode, empty file path, a missing
// file, or a music request without an album id; 404 for an unknown series,
// episode, or album.
func (s *Server) ManualImport(w http.ResponseWriter, r *http.Request) {
	var req manualImportRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	kind := strings.ToLower(strings.TrimSpace(req.Kind))
	switch kind {
	case "tv", "":
		// TV stays the default; the service enforces kind "tv".
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
			writeError(w, http.StatusBadRequest, "kind must be tv or music for manual import")
		case errors.Is(err, wdom.ErrInvalidEpisode):
			writeError(w, http.StatusBadRequest, "season and episode must be positive")
		case errors.Is(err, wdom.ErrEmptyPath):
			writeError(w, http.StatusBadRequest, "file_path is required")
		case errors.Is(err, wdom.ErrFileNotFound):
			writeError(w, http.StatusBadRequest, "file not found on the server")
		case errors.Is(err, wdom.ErrPathOutsideRoot):
			writeError(w, http.StatusBadRequest, "file_path must be under the media root")
		case errors.Is(err, wdom.ErrSeriesNotFound):
			writeError(w, http.StatusNotFound, "series not found")
		case errors.Is(err, wdom.ErrEpisodeNotFound):
			writeError(w, http.StatusNotFound, "episode not found")
		default:
			writeError(w, http.StatusInternalServerError, "failed to import episode")
		}
		return
	case "music":
		if req.AlbumID <= 0 {
			writeError(w, http.StatusBadRequest, "album_id is required for kind=music")
			return
		}
		if s.importDeps.Music == nil {
			writeError(w, http.StatusInternalServerError, "music manual import is not available")
			return
		}
		rep, err := s.importDeps.Music.ManualImport(r.Context(), wdom.ManualImport{
			Kind:         "music",
			AlbumID:      req.AlbumID,
			FilePath:     req.FilePath,
			ReleaseTitle: req.ReleaseTitle,
		})
		switch {
		case err == nil:
			writeJSON(w, http.StatusOK, rep)
		case errors.Is(err, wdom.ErrEmptyPath):
			writeError(w, http.StatusBadRequest, "file_path is required")
		case errors.Is(err, wdom.ErrFileNotFound):
			writeError(w, http.StatusBadRequest, "file not found on the server")
		case errors.Is(err, wdom.ErrPathOutsideRoot):
			writeError(w, http.StatusBadRequest, "file_path must be under the media root")
		case errors.Is(err, msvc.ErrUnsupportedFile):
			writeError(w, http.StatusBadRequest, "unsupported file type (phase 1: a single audio file or a directory of track files)")
		case errors.Is(err, msvc.ErrAlbumNotFound):
			writeError(w, http.StatusNotFound, "album not found")
		default:
			writeError(w, http.StatusInternalServerError, "failed to import album")
		}
		return
	}
	writeError(w, http.StatusBadRequest, "kind must be tv or music for manual import")
}
