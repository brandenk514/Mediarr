package api

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	moviesdom "github.com/brandenk514/mediarr/internal/domains/movies"
	"github.com/brandenk514/mediarr/internal/paging"
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
	s.register("POST /api/v1/movies", s.requireAuth(s.AddMovie))
	s.register("GET /api/v1/movies", s.requireAuth(s.ListMovies))
	s.register("GET /api/v1/movies/stats", s.requireAuth(s.GetMovieStats))
	s.register("GET /api/v1/movies/collections", s.requireAuth(s.ListMovieCollections))
	s.register("GET /api/v1/movies/{id}", s.requireAuth(s.GetMovie))
	s.register("PUT /api/v1/movies/{id}", s.requireAuth(s.SetMovieMonitored))
	s.register("PUT /api/v1/movies/{id}/editions/{editionId}", s.requireAuth(s.SetMovieEditionMonitored))
	s.register("GET /api/v1/movies/{id}/wanted", s.requireAuth(s.GetMovieWanted))
	s.register("POST /api/v1/movies/{id}/pipeline", s.requireAuth(s.RunMoviePipeline))
}

// addMovieRequest is the body of POST /api/v1/movies (Radarr R2 two-pane
// add). The R2 fields are optional: omitted ones keep pre-R2 behavior
// (monitor=all, no tags, server default root folder, no search).
type addMovieRequest struct {
	Title               string   `json:"title"`
	Year                string   `json:"year"`
	QualityProfile      string   `json:"quality_profile"`
	Monitor             string   `json:"monitor"`
	MinimumAvailability string   `json:"minimum_availability"`
	Tags                []string `json:"tags"`
	RootFolder          string   `json:"root_folder"`
	StartSearch         bool     `json:"start_search"`
}

// AddMovie handles POST /api/v1/movies (Radarr R2 two-pane add). The
// edition-aware monitor mode, tags, root folder, minimum availability and
// the best-effort start-search flag are honored by the service; invalid enum
// values map to 400 (ErrInvalidAddParams), any other service failure stays a
// 409 conflict.
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
	id, err := s.moviesDeps.Svc.AddMovie(r.Context(), moviesvc.AddMovieRequest{
		Title:               req.Title,
		Year:                req.Year,
		QualityProfile:      req.QualityProfile,
		Monitor:             req.Monitor,
		MinimumAvailability: req.MinimumAvailability,
		Tags:                req.Tags,
		RootFolder:          req.RootFolder,
		StartSearch:         req.StartSearch,
	})
	if err != nil {
		if errors.Is(err, moviesvc.ErrInvalidAddParams) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

// ListMovies handles GET /api/v1/movies. It returns a server-side filtered,
// sorted, paginated envelope (M6 #41, sort #70) so the UI's list view does
// pagination, filtering and ordering server-side:
// ?q=&monitored=&page=&page_size=&sort=<title|year|added|release_date|rating>-<asc|desc>
// plus the #150 filter-drawer facets:
// &genre=<a,b>&lang=<iso>&runtime_min=&runtime_max=&score_min=&score_max=
// &cert=<rating>&from=YYYY-MM-DD&to=YYYY-MM-DD&status=<chip>.
// #150: the sort value is validated against the closed allowlist and every
// facet param is type-checked — unknown or malformed values are a 400,
// never a silent default.
func (s *Server) ListMovies(w http.ResponseWriter, r *http.Request) {
	q := paging.QueryFromURLValues(r.URL.Query())
	f, err := movieListFilters(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	page, err := s.moviesDeps.Svc.ListMoviesQuery(r.Context(), q, f)
	if err != nil {
		if errors.Is(err, paging.ErrInvalidSort) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, page)
}

// movieListFilters parses the #150 facet params. Malformed values are
// errors (the handler maps them to 400); set-but-empty means unset.
func movieListFilters(v url.Values) (moviesvc.ListFilters, error) {
	f := moviesvc.ListFilters{}
	if g := strings.TrimSpace(v.Get("genre")); g != "" {
		for _, part := range strings.Split(g, ",") {
			if part = strings.TrimSpace(part); part != "" {
				f.Genres = append(f.Genres, part)
			}
		}
	}
	f.Language = strings.TrimSpace(v.Get("lang"))
	f.Certification = strings.TrimSpace(v.Get("cert"))
	f.Status = strings.TrimSpace(v.Get("status"))

	var err error
	if f.RuntimeMin, err = queryIntPtr(v, "runtime_min"); err != nil {
		return f, err
	}
	if f.RuntimeMax, err = queryIntPtr(v, "runtime_max"); err != nil {
		return f, err
	}
	if f.ScoreMin, err = queryFloatPtr(v, "score_min"); err != nil {
		return f, err
	}
	if f.ScoreMax, err = queryFloatPtr(v, "score_max"); err != nil {
		return f, err
	}
	if f.ReleaseFrom, err = queryDate(v, "from"); err != nil {
		return f, err
	}
	if f.ReleaseTo, err = queryDate(v, "to"); err != nil {
		return f, err
	}
	return f, nil
}

// queryIntPtr parses an optional positive integer query param; a set but
// malformed value is an error.
func queryIntPtr(v url.Values, name string) (*int, error) {
	raw := strings.TrimSpace(v.Get(name))
	if raw == "" {
		return nil, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return nil, fmt.Errorf("invalid %s %q", name, raw)
	}
	return &n, nil
}

// queryFloatPtr parses an optional non-negative float query param; a set
// but malformed value is an error.
func queryFloatPtr(v url.Values, name string) (*float64, error) {
	raw := strings.TrimSpace(v.Get(name))
	if raw == "" {
		return nil, nil
	}
	n, err := strconv.ParseFloat(raw, 64)
	if err != nil || n < 0 {
		return nil, fmt.Errorf("invalid %s %q", name, raw)
	}
	return &n, nil
}

// queryDate parses an optional "YYYY-MM-DD" query param; a set but
// malformed value is an error (never silently ignored).
func queryDate(v url.Values, name string) (string, error) {
	raw := strings.TrimSpace(v.Get(name))
	if raw == "" {
		return "", nil
	}
	if _, err := time.Parse("2006-01-02", raw); err != nil {
		return "", fmt.Errorf("invalid %s %q (want YYYY-MM-DD)", name, raw)
	}
	return raw, nil
}

// setMovieMonitoredRequest is the body for PUT /api/v1/movies/{id}.
type setMovieMonitoredRequest struct {
	Monitored bool `json:"monitored"`
}

// SetMovieMonitored handles PUT /api/v1/movies/{id}. It toggles the movie's
// monitoring; on toggle-on a pending wanted row is ensured so a re-monitored
// movie is wanted again (mirrors the tv/music/books monitored toggles).
func (s *Server) SetMovieMonitored(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req setMovieMonitoredRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := s.moviesDeps.Svc.SetMovieMonitored(r.Context(), id, req.Monitored); err != nil {
		if err == moviesvc.ErrMovieNotFound {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "monitored": req.Monitored})
}

// GetMovieStats handles GET /api/v1/movies/stats (Radarr R1 stats bar).
func (s *Server) GetMovieStats(w http.ResponseWriter, r *http.Request) {
	st, err := s.moviesDeps.Svc.GetStats(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// movieCollection is the wire shape of /api/v1/movies/collections (Radarr
// R3): the spec's lowercase MovieCollection schema (unlike the Movie schema,
// which follows Go's default PascalCase marshaling).
type movieCollection struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	MovieCount   int               `json:"movie_count"`
	MissingCount int               `json:"missing_count"`
	Movies       []moviesdom.Movie `json:"movies"`
}

// ListMovieCollections handles GET /api/v1/movies/collections (Radarr R3
// collections page): the library grouped by TMDB collection, sorted by name.
// Keyless dev works (groups by the persisted collection fields; missing
// counts stay 0).
func (s *Server) ListMovieCollections(w http.ResponseWriter, r *http.Request) {
	cols, err := s.moviesDeps.Svc.ListCollections(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]movieCollection, 0, len(cols))
	for _, c := range cols {
		members := c.Movies
		if members == nil {
			members = []moviesdom.Movie{}
		}
		out = append(out, movieCollection{
			ID:           c.ID,
			Name:         c.Name,
			MovieCount:   len(members),
			MissingCount: c.MissingCount,
			Movies:       members,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// SetMovieEditionMonitored handles PUT /api/v1/movies/{id}/editions/{editionId}
// (Radarr v6 per-edition monitor toggle, R1).
func (s *Server) SetMovieEditionMonitored(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	raw := r.PathValue("editionId")
	editionID, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || editionID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid edition id")
		return
	}
	var req setMovieMonitoredRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	e, err := s.moviesDeps.Svc.SetEditionMonitored(r.Context(), id, editionID, req.Monitored)
	if err != nil {
		if err == moviesvc.ErrMovieNotFound || err == moviesvc.ErrEditionNotFound {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": e.ID, "monitored": e.Monitored})
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

// GetMovieWanted handles GET /api/v1/movies/{id}/wanted (P7): the movie's
// wanted (release) state for the detail page. 404 when the movie does not
// exist, 204 when it exists but has no wanted row yet.
func (s *Server) GetMovieWanted(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	wd, err := s.moviesDeps.Svc.GetWanted(r.Context(), id)
	if err != nil {
		if err == moviesvc.ErrMovieNotFound {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if wd == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, wd)
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

// pathInt extracts a named positive-integer path segment ({season}/{episode}),
// writing an error on failure. Returns ok=false when an error was written.
func pathInt(w http.ResponseWriter, raw, name string) (int, bool) {
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		writeError(w, http.StatusBadRequest, "invalid "+name)
		return 0, false
	}
	return n, true
}
