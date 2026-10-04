package api

import (
	"net/http"

	tvsvc "github.com/brandenk514/mediarr/internal/services/tv"
)

// TVDeps bundles what the TV endpoints need.
type TVDeps struct {
	Svc *tvsvc.Service
}

// SetTV wires the TV endpoints. Called after construction, mirroring
// SetMovies, so NewServer stays usable for health-only boot and tests.
func (s *Server) SetTV(deps *TVDeps) {
	s.tvDeps = deps
	s.mux.HandleFunc("POST /api/v1/tv", s.requireAuth(s.AddSeries))
	s.mux.HandleFunc("GET /api/v1/tv", s.requireAuth(s.ListSeries))
	s.mux.HandleFunc("GET /api/v1/tv/{id}", s.requireAuth(s.GetSeries))
	s.mux.HandleFunc("POST /api/v1/tv/{id}/episodes", s.requireAuth(s.AddEpisode))
	s.mux.HandleFunc("GET /api/v1/tv/{id}/episodes", s.requireAuth(s.ListEpisodes))
	s.mux.HandleFunc("GET /api/v1/tv/{id}/wanted", s.requireAuth(s.ListWanted))
	s.mux.HandleFunc("POST /api/v1/tv/{id}/pipeline", s.requireAuth(s.RunTVPipeline))
}

// addSeriesRequest is the body for POST /api/v1/tv.
type addSeriesRequest struct {
	Title          string `json:"title"`
	Year           string `json:"year"`
	QualityProfile string `json:"quality_profile"`
}

// AddSeries handles POST /api/v1/tv.
func (s *Server) AddSeries(w http.ResponseWriter, r *http.Request) {
	var req addSeriesRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	id, err := s.tvDeps.Svc.AddSeries(r.Context(), req.Title, req.Year, req.QualityProfile)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

// ListSeries handles GET /api/v1/tv.
func (s *Server) ListSeries(w http.ResponseWriter, r *http.Request) {
	series, err := s.tvDeps.Svc.ListSeries(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, series)
}

// GetSeries handles GET /api/v1/tv/{id}.
func (s *Server) GetSeries(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	series, err := s.tvDeps.Svc.GetSeries(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, series)
}

// addEpisodeRequest is the body for POST /api/v1/tv/{id}/episodes. Monitored
// is a pointer so "omitted" (defaults to monitored) is distinguishable from an
// explicit false.
type addEpisodeRequest struct {
	Season    int    `json:"season"`
	Episode   int    `json:"episode"`
	Title     string `json:"title"`
	Monitored *bool  `json:"monitored"`
}

// AddEpisode handles POST /api/v1/tv/{id}/episodes.
func (s *Server) AddEpisode(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req addEpisodeRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Season <= 0 || req.Episode <= 0 {
		writeError(w, http.StatusBadRequest, "season and episode must be positive")
		return
	}
	// A newly added episode is monitored by default; an explicit
	// "monitored": false opts out.
	monitored := true
	if req.Monitored != nil {
		monitored = *req.Monitored
	}
	_, err := s.tvDeps.Svc.AddEpisode(r.Context(), id, req.Season, req.Episode, req.Title, monitored)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{})
}

// ListEpisodes handles GET /api/v1/tv/{id}/episodes.
func (s *Server) ListEpisodes(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	eps, err := s.tvDeps.Svc.ListEpisodes(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, eps)
}

// ListWanted handles GET /api/v1/tv/{id}/wanted.
func (s *Server) ListWanted(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	wants, err := s.tvDeps.Svc.ListWanted(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, wants)
}

// RunTVPipeline handles POST /api/v1/tv/{id}/pipeline.
func (s *Server) RunTVPipeline(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	imported, err := s.tvDeps.Svc.RunPipeline(r.Context(), id)
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case err == tvsvc.ErrNoMatch:
			status = http.StatusUnprocessableEntity
		case err == tvsvc.ErrNoWanted:
			status = http.StatusNoContent
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"imported": imported})
}
