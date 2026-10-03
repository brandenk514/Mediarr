package api

import (
	"net/http"
	"strconv"

	moviesvc "github.com/brandenk514/mediarr/internal/services/movies"
)

// MoviesDeps bundles what the movie endpoints need.
type MoviesDeps struct {
	Svc *moviesvc.Service
}

// SetMovies wires the movie endpoints. Called after construction, mirroring
// SetAuth, so NewServer stays usable for health-only boot and tests.
func (s *Server) SetMovies(deps *MoviesDeps) {
	s.moviesDeps = deps
	s.mux.HandleFunc("POST /api/v1/movies", s.requireAuth(s.AddMovie))
	s.mux.HandleFunc("GET /api/v1/movies", s.requireAuth(s.ListMovies))
	s.mux.HandleFunc("GET /api/v1/movies/{id}", s.requireAuth(s.GetMovie))
	s.mux.HandleFunc("POST /api/v1/movies/{id}/pipeline", s.requireAuth(s.RunMoviePipeline))
}

// addMovieRequest is the body for POST /api/v1/movies.
type addMovieRequest struct {
	Title          string `json:"title"`
	Year           string `json:"year"`
	QualityProfile string `json:"quality_profile"`
}

// AddMovie handles POST /api/v1/movies.
func (s *Server) AddMovie(w http.ResponseWriter, r *http.Request) {
	var req addMovieRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	id, err := s.moviesDeps.Svc.AddMovie(r.Context(), req.Title, req.Year, req.QualityProfile)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

// ListMovies handles GET /api/v1/movies.
func (s *Server) ListMovies(w http.ResponseWriter, r *http.Request) {
	movies, err := s.moviesDeps.Svc.ListMovies(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, movies)
}

// GetMovie handles GET /api/v1/movies/{id}.
func (s *Server) GetMovie(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	m, err := s.moviesDeps.Svc.GetMovie(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, m)
}

// RunMoviePipeline handles POST /api/v1/movies/{id}/pipeline.
func (s *Server) RunMoviePipeline(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	imported, err := s.moviesDeps.Svc.RunPipeline(r.Context(), id)
	if err != nil {
		status := http.StatusInternalServerError
		if err == moviesvc.ErrNoMatch {
			status = http.StatusUnprocessableEntity
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"imported": imported})
}

// pathID extracts the {id} path segment as an int64, writing an error on
// failure. Returns ok=false when an error was written.
func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := r.PathValue("id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid movie id")
		return 0, false
	}
	return id, true
}
