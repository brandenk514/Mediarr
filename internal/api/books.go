package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	booksdom "github.com/brandenk514/mediarr/internal/domains/books"
	"github.com/brandenk514/mediarr/internal/paging"
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
	s.register("POST /api/v1/books/authors", s.requireAuth(s.AddAuthor))
	s.register("GET /api/v1/books/authors", s.requireAuth(s.ListAuthors))
	s.register("GET /api/v1/books/authors/{id}", s.requireAuth(s.GetAuthor))
	s.register("PUT /api/v1/books/authors/{id}", s.requireAuth(s.SetAuthorMonitored))
	s.register("POST /api/v1/books/authors/{id}/titles", s.requireAuth(s.AddTitle))
	s.register("GET /api/v1/books/authors/{id}/titles", s.requireAuth(s.ListTitles))
	s.register("GET /api/v1/books/authors/{id}/history", s.requireAuth(s.ListAuthorHistory))
	s.register("GET /api/v1/books/authors/{id}/series", s.requireAuth(s.ListAuthorSeries))
	s.register("GET /api/v1/books/titles/{id}", s.requireAuth(s.GetTitle))
	s.register("PUT /api/v1/books/titles/{id}", s.requireAuth(s.SetTitleMonitored))
	s.register("POST /api/v1/books/titles/{id}/editions", s.requireAuth(s.AddEdition))
	s.register("GET /api/v1/books/titles/{id}/editions", s.requireAuth(s.ListEditions))
	s.register("GET /api/v1/books/editions/{id}", s.requireAuth(s.GetEdition))
	s.register("PUT /api/v1/books/editions/{id}", s.requireAuth(s.SetEditionMonitored))
	s.register("GET /api/v1/books/titles/{id}/wanted", s.requireAuth(s.ListTitleWanted))
	s.register("GET /api/v1/books/titles/{id}/files", s.requireAuth(s.ListBookFiles))
	s.register("GET /api/v1/books/titles/{id}/series", s.requireAuth(s.ListTitleSeries))
	s.register("POST /api/v1/books/titles/{id}/series", s.requireAuth(s.LinkTitleToSeries))
	// Flat Books index (every book across all authors) + series (Readarr B1).
	s.register("GET /api/v1/books", s.requireAuth(s.ListBooks))
	s.register("GET /api/v1/books/series", s.requireAuth(s.ListBookSeries))
	s.register("POST /api/v1/books/series", s.requireAuth(s.AddBookSeries))
	s.register("GET /api/v1/books/series/{id}", s.requireAuth(s.GetBookSeries))
	s.register("GET /api/v1/books/series/{id}/books", s.requireAuth(s.ListSeriesBooks))
	s.register("POST /api/v1/books/authors/{id}/pipeline", s.requireAuth(s.RunBooksPipeline))
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

// ListAuthors handles GET /api/v1/books/authors. It returns a server-side
// filtered, paginated envelope (M6 #41): ?q=<search>&monitored=<bool>&page=&page_size=.
func (s *Server) ListAuthors(w http.ResponseWriter, r *http.Request) {
	q := paging.QueryFromURLValues(r.URL.Query())
	page, err := s.booksDeps.Svc.ListAuthorsQuery(r.Context(), q)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, page)
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
// The edition is the (format, language, monitored) triple (Readarr B1);
// language may be "" (unknown). Monitored is a pointer so "omitted" (defaults
// to monitored) is distinguishable from an explicit false.
type addEditionRequest struct {
	Format    string `json:"format"`
	Language  string `json:"language"`
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
	editionID, err := s.booksDeps.Svc.AddEdition(r.Context(), id, req.Format, req.Language, req.ISBN, req.Publisher, req.Year, req.Pages, monitored)
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

// ListBookFiles handles GET /api/v1/books/titles/{id}/files (Readarr B1: the
// title's imported e-book files with their media info — pages, size, format,
// language, added date).
func (s *Server) ListBookFiles(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	files, err := s.booksDeps.Svc.GetBookFiles(r.Context(), id)
	if err != nil {
		writeBooksNotFound(w, err)
		return
	}
	if files == nil {
		files = []booksdom.BookFile{}
	}
	writeJSON(w, http.StatusOK, files)
}

// ListAuthorHistory handles GET /api/v1/books/authors/{id}/history — the
// books analogue of TV's series-level history. ?limit clamps to 1..200,
// defaulting to 50.
func (s *Server) ListAuthorHistory(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 200 {
		limit = 200
	}
	h, err := s.booksDeps.Svc.GetAuthorHistory(r.Context(), id, limit)
	if err != nil {
		writeBooksNotFound(w, err)
		return
	}
	if h == nil {
		h = []booksdom.HistoryEntry{}
	}
	writeJSON(w, http.StatusOK, h)
}

// ListBooks handles GET /api/v1/books — the flat Books index: every book
// across all authors, each carrying its author name, sorted by author name
// then title name (Readarr B1).
func (s *Server) ListBooks(w http.ResponseWriter, r *http.Request) {
	books, err := s.booksDeps.Svc.ListBooks(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if books == nil {
		books = []booksdom.Book{}
	}
	writeJSON(w, http.StatusOK, books)
}

// addBookSeriesRequest is the body for POST /api/v1/books/series.
type addBookSeriesRequest struct {
	Name string `json:"name"`
}

// AddBookSeries handles POST /api/v1/books/series. Creating a series is
// idempotent by name: the existing series is returned as-is (201, the same
// convention as AddAuthor).
func (s *Server) AddBookSeries(w http.ResponseWriter, r *http.Request) {
	var req addBookSeriesRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	id, err := s.booksDeps.Svc.AddSeries(r.Context(), req.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

// GetBookSeries handles GET /api/v1/books/series/{id}.
func (s *Server) GetBookSeries(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	series, err := s.booksDeps.Svc.GetSeries(r.Context(), id)
	if err != nil {
		writeBooksNotFound(w, err)
		return
	}
	writeJSON(w, http.StatusOK, series)
}

// ListBookSeries handles GET /api/v1/books/series.
func (s *Server) ListBookSeries(w http.ResponseWriter, r *http.Request) {
	series, err := s.booksDeps.Svc.ListSeries(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if series == nil {
		series = []booksdom.Series{}
	}
	writeJSON(w, http.StatusOK, series)
}

// ListSeriesBooks handles GET /api/v1/books/series/{id}/books (the books in a
// series, with their author names).
func (s *Server) ListSeriesBooks(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	books, err := s.booksDeps.Svc.ListSeriesBooks(r.Context(), id)
	if err != nil {
		writeBooksNotFound(w, err)
		return
	}
	if books == nil {
		books = []booksdom.SeriesBook{}
	}
	writeJSON(w, http.StatusOK, books)
}

// ListTitleSeries handles GET /api/v1/books/titles/{id}/series (the series a
// book belongs to).
func (s *Server) ListTitleSeries(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	series, err := s.booksDeps.Svc.ListTitleSeries(r.Context(), id)
	if err != nil {
		writeBooksNotFound(w, err)
		return
	}
	if series == nil {
		series = []booksdom.Series{}
	}
	writeJSON(w, http.StatusOK, series)
}

// ListAuthorSeries handles GET /api/v1/books/authors/{id}/series — the series
// the author's books belong to, with the member titles by this author (the
// author detail's series links).
func (s *Server) ListAuthorSeries(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	series, err := s.booksDeps.Svc.ListAuthorSeries(r.Context(), id)
	if err != nil {
		writeBooksNotFound(w, err)
		return
	}
	if series == nil {
		series = []booksdom.AuthorSeries{}
	}
	writeJSON(w, http.StatusOK, series)
}

// linkTitleToSeriesRequest is the body for
// POST /api/v1/books/titles/{id}/series.
type linkTitleToSeriesRequest struct {
	SeriesID int64 `json:"series_id"`
}

// LinkTitleToSeries handles POST /api/v1/books/titles/{id}/series — linking a
// book to a series (idempotent; manual data entry until MT1's id-lookup).
func (s *Server) LinkTitleToSeries(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req linkTitleToSeriesRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.SeriesID <= 0 {
		writeError(w, http.StatusBadRequest, "series_id is required")
		return
	}
	if err := s.booksDeps.Svc.LinkTitleToSeries(r.Context(), id, req.SeriesID); err != nil {
		writeBooksNotFound(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"title_id": id, "series_id": req.SeriesID})
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
		switch err {
		case bookssvc.ErrNoMatch:
			status = http.StatusUnprocessableEntity
		case bookssvc.ErrNoWanted:
			status = http.StatusNoContent
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"imported": imported})
}

// writeBooksMonitoredError maps a toggle error to the right status: an unknown
// author, title, edition, or series is 404, otherwise 500.
func writeBooksMonitoredError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	if errors.Is(err, bookssvc.ErrAuthorNotFound) ||
		errors.Is(err, bookssvc.ErrTitleNotFound) ||
		errors.Is(err, bookssvc.ErrEditionNotFound) ||
		errors.Is(err, bookssvc.ErrSeriesNotFound) {
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
		errors.Is(err, bookssvc.ErrEditionNotFound) ||
		errors.Is(err, bookssvc.ErrSeriesNotFound) {
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
