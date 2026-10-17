package api

import (
	"errors"
	"net/http"
	"strings"

	booksdom "github.com/brandenk514/mediarr/internal/domains/books"
	moviesdom "github.com/brandenk514/mediarr/internal/domains/movies"
	musicdom "github.com/brandenk514/mediarr/internal/domains/music"
	tvdom "github.com/brandenk514/mediarr/internal/domains/tv"
	"github.com/brandenk514/mediarr/internal/providers"
)

// SearchDeps bundles the metadata-provider adapters the add-flow search
// endpoints need (M6 #42). Each adapter is an outer boundary: this layer is
// the only one that talks to providers. Adapters may be nil when their
// provider is not configured (e.g. no TMDB key) — the corresponding search
// endpoint answers 503, which the UI surfaces inline.
type SearchDeps struct {
	TMDB        *providers.TMDB
	TVDB        *providers.TVDB
	MusicBrainz *providers.MusicBrainz
	OpenLibrary *providers.OpenLibrary
}

// SetSearch wires the add-flow search endpoints (M6 #42). Called after
// construction, mirroring SetMovies, so NewServer stays usable for
// health-only boot and tests.
//
// Routes:
//
//	GET /api/v1/search/movies?query=      -> TMDB movies
//	GET /api/v1/search/tv?query=          -> TVDB series (tmdb_id not known)
//	GET /api/v1/search/tv-tmdb?query=     -> TMDB series (provides tmdb_id)
//	GET /api/v1/search/artists?query=     -> MusicBrainz artists
//	GET /api/v1/search/books/authors?query=   -> Open Library authors
//	GET /api/v1/search/books/works?query=&author= -> Open Library works
//	GET /api/v1/profiles?kind=movies      -> built-in quality profiles + default
func (s *Server) SetSearch(deps *SearchDeps) {
	s.searchDeps = deps
	s.register("GET /api/v1/search/movies", s.requireAuth(s.searchMovies))
	s.register("GET /api/v1/search/tv", s.requireAuth(s.searchTV))
	s.register("GET /api/v1/search/tv-tmdb", s.requireAuth(s.searchTVMDB))
	s.register("GET /api/v1/search/artists", s.requireAuth(s.searchArtists))
	s.register("GET /api/v1/search/books/authors", s.requireAuth(s.searchBookAuthors))
	s.register("GET /api/v1/search/books/works", s.requireAuth(s.searchBookWorks))
	s.register("GET /api/v1/profiles", s.requireAuth(s.listProfiles))
}

// searchResult is the wire shape of one provider search hit. Fields mirror
// providers.Result; the API adds nothing — display-only detail (overview,
// rating, country, author) travels in extra.
type searchResult struct {
	ID    string            `json:"id"`
	Title string            `json:"title"`
	Year  int               `json:"year"`
	Extra map[string]string `json:"extra"`
}

func toWire(results []providers.Result) []searchResult {
	out := make([]searchResult, 0, len(results))
	for _, r := range results {
		out = append(out, searchResult{ID: r.ID, Title: r.Title, Year: r.Year, Extra: r.Extra})
	}
	return out
}

// writeSearchError maps provider errors to statuses: not-configured is 503
// (the UI greys out the search box with the reason), a bad upstream answer
// is 502, anything else 500.
func writeSearchError(w http.ResponseWriter, provider string, err error) {
	switch {
	case errors.Is(err, providers.ErrNotConfigured):
		writeError(w, http.StatusServiceUnavailable, provider+" is not configured (missing API key)")
	case errors.Is(err, providers.ErrBadResponse):
		writeError(w, http.StatusBadGateway, provider+" search failed: "+err.Error())
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

func (s *Server) searchMovies(w http.ResponseWriter, r *http.Request) {
	if s.searchDeps.TMDB == nil {
		writeError(w, http.StatusServiceUnavailable, "tmdb is not configured (missing API key)")
		return
	}
	results, err := s.searchDeps.TMDB.SearchMovies(r.Context(), r.URL.Query().Get("query"))
	if err != nil {
		writeSearchError(w, "tmdb", err)
		return
	}
	writeJSON(w, http.StatusOK, toWire(results))
}

// searchTV searches TVDB series. The response carries tvdb_id; tmdb_id is
// not in TVDB's search payload, so the TV add flow offers TVDB as the source
// of truth (tvdb_id) and leaves tmdb_id empty.
func (s *Server) searchTV(w http.ResponseWriter, r *http.Request) {
	if s.searchDeps.TVDB == nil {
		writeError(w, http.StatusServiceUnavailable, "tvdb is not configured (missing API key)")
		return
	}
	results, err := s.searchDeps.TVDB.Search(r.Context(), r.URL.Query().Get("query"))
	if err != nil {
		writeSearchError(w, "tvdb", err)
		return
	}
	writeJSON(w, http.StatusOK, toWire(results))
}

// searchTVMDB searches TMDB series. The response carries tmdb_id; tvdb_id is
// not in TMDB's search payload, so a series added from this source has
// tvdb_id 0 (the domain treats 0 as "unknown", matching TVDB's own behavior
// for series that predate it).
func (s *Server) searchTVMDB(w http.ResponseWriter, r *http.Request) {
	if s.searchDeps.TMDB == nil {
		writeError(w, http.StatusServiceUnavailable, "tmdb is not configured (missing API key)")
		return
	}
	results, err := s.searchDeps.TMDB.SearchTV(r.Context(), r.URL.Query().Get("query"))
	if err != nil {
		writeSearchError(w, "tmdb", err)
		return
	}
	writeJSON(w, http.StatusOK, toWire(results))
}

func (s *Server) searchArtists(w http.ResponseWriter, r *http.Request) {
	if s.searchDeps.MusicBrainz == nil {
		writeError(w, http.StatusServiceUnavailable, "musicbrainz is not configured")
		return
	}
	results, err := s.searchDeps.MusicBrainz.Search(r.Context(), r.URL.Query().Get("query"))
	if err != nil {
		writeSearchError(w, "musicbrainz", err)
		return
	}
	writeJSON(w, http.StatusOK, toWire(results))
}

func (s *Server) searchBookAuthors(w http.ResponseWriter, r *http.Request) {
	if s.searchDeps.OpenLibrary == nil {
		writeError(w, http.StatusServiceUnavailable, "openlibrary is not configured")
		return
	}
	results, err := s.searchDeps.OpenLibrary.SearchAuthors(r.Context(), r.URL.Query().Get("query"))
	if err != nil {
		writeSearchError(w, "openlibrary", err)
		return
	}
	writeJSON(w, http.StatusOK, toWire(results))
}

func (s *Server) searchBookWorks(w http.ResponseWriter, r *http.Request) {
	if s.searchDeps.OpenLibrary == nil {
		writeError(w, http.StatusServiceUnavailable, "openlibrary is not configured")
		return
	}
	q := r.URL.Query()
	results, err := s.searchDeps.OpenLibrary.SearchWorks(r.Context(), q.Get("author"), q.Get("query"))
	if err != nil {
		writeSearchError(w, "openlibrary", err)
		return
	}
	writeJSON(w, http.StatusOK, toWire(results))
}

// profileResponse is the wire shape of GET /api/v1/profiles: the built-in
// profiles of one kind (names only — the UI picker needs no item detail) plus
// the kind's default, so the add flow can preselect it.
type profileResponse struct {
	Profiles []string `json:"profiles"`
	Default  string   `json:"default"`
}

func (s *Server) listProfiles(w http.ResponseWriter, r *http.Request) {
	kind := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("kind")))
	names, def := profilesForKind(kind)
	if len(names) == 0 {
		writeError(w, http.StatusBadRequest, "kind must be one of movies, tv, music, books")
		return
	}
	writeJSON(w, http.StatusOK, profileResponse{Profiles: names, Default: def})
}

func profilesForKind(kind string) ([]string, string) {
	switch kind {
	case "movies":
		return namesOfMovieProfiles(moviesdom.BuiltInProfiles()), "HD-1080p"
	case "tv":
		return namesOfTVProfiles(tvdom.BuiltInProfiles()), "HD-1080p"
	case "music":
		return namesOfMusicProfiles(musicdom.BuiltInProfiles()), "Lossless"
	case "books":
		return namesOfBookProfiles(booksdom.BuiltInProfiles()), "Best"
	default:
		return nil, ""
	}
}

func namesOfMovieProfiles(ps []moviesdom.Profile) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.Name)
	}
	return out
}

func namesOfTVProfiles(ps []tvdom.Profile) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.Name)
	}
	return out
}

func namesOfMusicProfiles(ps []musicdom.Profile) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.Name)
	}
	return out
}

func namesOfBookProfiles(ps []booksdom.Profile) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.Name)
	}
	return out
}
