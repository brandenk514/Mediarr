package api

import (
	"errors"
	"net/http"

	booksdom "github.com/brandenk514/mediarr/internal/domains/books"
	bookssvc "github.com/brandenk514/mediarr/internal/services/books"
)

// BooksDeps bundles what the books endpoints need.
type BooksDeps struct {
	Svc *bookssvc.Service
}

// SetBooks wires the books endpoints. Called after construction, mirroring
// SetMusic, so NewServer stays usable for health-only boot and tests.
//
// Routes are prefixed by collection (authors, titles, editions) so that the
// by-id routes (GET /api/v1/books/titles/{id}) and the collection routes
// (GET /api/v1/books/titles/{id}/editions) never share a concrete path — the
// ambiguity that makes Go 1.22+'s ServeMux panic at registration. The
// route-guard test (TestBooksRoutesRegister) pins this.
func (s *Server) SetBooks(deps *BooksDeps) {
	s.booksDeps = deps
	s.mux.HandleFunc("POST /api/v1/books/authors", s.requireAuth(s.AddAuthor))
	s.mux.HandleFunc("GET /api/v1/books/authors", s.requireAuth(s.ListAuthors))
	s.mux.HandleFunc("GET /api/v1/books/authors/{id}", s.requireAuth(s.GetAuthor))
	s.mux.HandleFunc("PUT /api/v1/books/authors/{id}", s.requireAuth(s.SetAuthorMonitored))
	s.mux.HandleFunc("POST /api/v1/books/authors/{id}/titles", s.requireAuth(s.AddTitle))
	s.mux.HandleFunc("GET /api/v1/books/authors/{id}/titles", s.requireAuth(s.ListTitles))
	s.mux.HandleFunc("GET /api/v1/books/titles/{id}", s.requireAuth(s.GetTitle))
	s.mux.HandleFunc("PUT /api/v1/books/titles/{id}", s.requireAuth(s.SetTitleMonitored))
	s.mux.HandleFunc("POST /api/v1/books/titles/{id}/editions", s.requireAuth(s.AddEdition))
	s.mux.HandleFunc("GET /api/v1/books/titles/{id}/editions", s.requireAuth(s.ListEditions))
	s.mux.HandleFunc("GET /api/v1/books/editions/{id}", s.requireAuth(s.GetEdition))
	s.mux.HandleFunc("PUT /api/v1/books/editions/{id}", s.requireAuth(s.SetEditionMonitored))
	s.mux.HandleFunc("GET /api/v1/books/titles/{id}/wanted", s.requireAuth(s.ListTitleWanted))
	s.mux.HandleFunc("POST /api/v1/books/authors/{id}/pipeline", s.requireAuth(s.RunBooksPipeline))
}

// addAuthorRequest is the body for POST /api/v1/books/authors. Monitored is a
// pointer so "omitted" (defaults to monitored) is distinguishable from an
// explicit false.
type addAuthorRequest struct {
	Name      string `json:"name"`
	Monitored *bool  `json:"monitored"`
}

// AddAuthor handles POST /api/v1/books/authors. An author is monitored by
// default; an explicit "monitored": false opts out (PLAN §4 author-level
// monitor).
func (s *Server) AddAuthor(w http.ResponseWriter, r *http.Request) {
	var req addAuthorRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	monitored := true
	if req.Monitored != nil {
		monitored = *req.Monitored
	}
	id, err := s.booksDeps.Svc.AddAuthor(r.Context(), req.Name, monitored)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

// ListAuthors handles GET /api/v1/books/authors.
func (s *Server) ListAuthors(w http.ResponseWriter, r *http.Request) {
	authors, err := s.booksDeps.Svc.ListAuthors(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, authors)
}

// GetAuthor handles GET /api/v1/books/authors/{id}.
func (s *Server) GetAuthor(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	author, err := s.booksDeps.Svc.GetAuthor(r.Context(), id)
	if err != nil {
		writeBooksNotFound(w, err)
		return
	}
	writeJSON(w, http.StatusOK, author)
}

// addTitleRequest is the body for POST /api/v1/books/authors/{id}/titles.
type addTitleRequest struct {
	Name      string `json:"name"`
	Monitored *bool  `json:"monitored"`
}

// AddTitle handles POST /api/v1/books/authors/{id}/titles. A newly added title
// is monitored by default (PLAN §4); an explicit "monitored": false opts out.
func (s *Server) AddTitle(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req addTitleRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	monitored := true
	if req.Monitored != nil {
		monitored = *req.Monitored
	}
	titleID, err := s.booksDeps.Svc.AddTitle(r.Context(), id, req.Name, monitored)
	if err != nil {
		writeBooksNotFound(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": titleID})
}

// ListTitles handles GET /api/v1/books/authors/{id}/titles.
func (s *Server) ListTitles(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	titles, err := s.booksDeps.Svc.ListTitles(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, titles)
}

// GetTitle handles GET /api/v1/books/titles/{id}.
func (s *Server) GetTitle(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	title, err := s.booksDeps.Svc.GetTitle(r.Context(), id)
	if err != nil {
		writeBooksNotFound(w, err)
		return
	}
	writeJSON(w, http.StatusOK, title)
}

// addEditionRequest is the body for POST /api/v1/books/titles/{id}/editions.
// Monitored is a pointer so "omitted" (defaults to monitored) is distinguishable
// from an explicit false.
type addEditionRequest struct {
	Format    string `json:"format"`
	ISBN      string `json:"isbn"`
	Publisher string `json:"publisher"`
	Year      int    `json:"year"`
	Pages     int    `json:"pages"`
	Monitored *bool  `json:"monitored"`
}

// AddEdition handles POST /api/v1/books/titles/{id}/editions. A newly added
// edition is monitored by default (PLAN §4 edition-level monitor); an explicit
// "monitored": false opts out. The format must be a supported e-book format
// (epub/mobi/azw3) — unsupported formats are rejected with 400.
func (s *Server) AddEdition(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req addEditionRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if !booksdom.IsSupportedFormat(req.Format) {
		writeError(w, http.StatusBadRequest, "format must be one of epub, mobi, azw3")
		return
	}
	monitored := true
	if req.Monitored != nil {
		monitored = *req.Monitored
	}
	editionID, err := s.booksDeps.Svc.AddEdition(r.Context(), id, req.Format, req.ISBN, req.Publisher, req.Year, req.Pages, monitored)
	if err != nil {
		writeBooksNotFound(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": editionID})
}

// ListEditions handles GET /api/v1/books/titles/{id}/editions.
func (s *Server) ListEditions(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	editions, err := s.booksDeps.Svc.ListEditions(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, editions)
}

// GetEdition handles GET /api/v1/books/editions/{id}.
func (s *Server) GetEdition(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	edition, err := s.booksDeps.Svc.GetEdition(r.Context(), id)
	if err != nil {
		writeBooksNotFound(w, err)
		return
	}
	writeJSON(w, http.StatusOK, edition)
}

// ListTitleWanted handles GET /api/v1/books/titles/{id}/wanted (title-level and
// edition-level wanted entries with their status).
func (s *Server) ListTitleWanted(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	wants, err := s.booksDeps.Svc.ListWanted(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, wants)
}

// RunBooksPipeline handles POST /api/v1/books/authors/{id}/pipeline.
func (s *Server) RunBooksPipeline(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	imported, err := s.booksDeps.Svc.RunPipeline(r.Context(), id)
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case err == bookssvc.ErrNoMatch:
			status = http.StatusUnprocessableEntity
		case err == bookssvc.ErrNoWanted:
			status = http.StatusNoContent
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"imported": imported})
}

// writeBooksMonitoredError maps a toggle error to the right status: an unknown
// author, title, or edition is 404, otherwise 500.
func writeBooksMonitoredError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	if errors.Is(err, bookssvc.ErrAuthorNotFound) ||
		errors.Is(err, bookssvc.ErrTitleNotFound) ||
		errors.Is(err, bookssvc.ErrEditionNotFound) {
		status = http.StatusNotFound
	}
	writeError(w, status, err.Error())
}

// writeBooksNotFound maps a lookup error to 404 for unknown entities and 500
// otherwise.
func writeBooksNotFound(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	if errors.Is(err, bookssvc.ErrAuthorNotFound) ||
		errors.Is(err, bookssvc.ErrTitleNotFound) ||
		errors.Is(err, bookssvc.ErrEditionNotFound) {
		status = http.StatusNotFound
	}
	writeError(w, status, err.Error())
}

// SetAuthorMonitored handles PUT /api/v1/books/authors/{id}. It toggles the
// author-level monitor, which fans out to the author's known titles (PLAN §4).
func (s *Server) SetAuthorMonitored(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req monitoredRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := s.booksDeps.Svc.SetAuthorMonitored(r.Context(), id, req.Monitored); err != nil {
		writeBooksMonitoredError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "monitored": req.Monitored})
}

// SetTitleMonitored handles PUT /api/v1/books/titles/{id}. It toggles a single
// title's monitor.
func (s *Server) SetTitleMonitored(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req monitoredRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := s.booksDeps.Svc.SetTitleMonitored(r.Context(), id, req.Monitored); err != nil {
		writeBooksMonitoredError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "monitored": req.Monitored})
}

// SetEditionMonitored handles PUT /api/v1/books/editions/{id}. It toggles a
// single edition's monitor.
func (s *Server) SetEditionMonitored(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req monitoredRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	edition, err := s.booksDeps.Svc.GetEdition(r.Context(), id)
	if err != nil {
		writeBooksMonitoredError(w, err)
		return
	}
	if err := s.booksDeps.Svc.SetEditionMonitored(r.Context(), edition.TitleID, id, req.Monitored); err != nil {
		writeBooksMonitoredError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "monitored": req.Monitored})
}
