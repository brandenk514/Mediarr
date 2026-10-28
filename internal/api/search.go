package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	booksdom "github.com/brandenk514/mediarr/internal/domains/books"
	moviesdom "github.com/brandenk514/mediarr/internal/domains/movies"
	tvdom "github.com/brandenk514/mediarr/internal/domains/tv"
	"github.com/brandenk514/mediarr/internal/providers"
	"github.com/brandenk514/mediarr/internal/settings"
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
	q := strings.TrimSpace(r.URL.Query().Get("query"))
	if strings.HasPrefix(q, "tmdb:") {
		// Direct lookup by TMDB id (Radarr R2): one hit from /movie/{id}.
		id := strings.TrimSpace(strings.TrimPrefix(q, "tmdb:"))
		if id == "" {
			writeError(w, http.StatusBadRequest, "invalid tmdb:<id> query")
			return
		}
		h, err := s.searchDeps.TMDB.FindMovieHit(r.Context(), id)
		if err != nil {
			writeSearchError(w, "tmdb", err)
			return
		}
		writeJSON(w, http.StatusOK, []movieSearchHit{toMovieHit(*h)})
		return
	}
	hits, err := s.searchDeps.TMDB.SearchMovieHits(r.Context(), q)
	if err != nil {
		writeSearchError(w, "tmdb", err)
		return
	}
	out := make([]movieSearchHit, 0, len(hits))
	for _, h := range hits {
		out = append(out, toMovieHit(h))
	}
	writeJSON(w, http.StatusOK, out)
}

// movieSearchHit is the wire shape of /api/v1/search/movies (Radarr R2).
// tmdb_pct is null when TMDB has no vote for the movie.
type movieSearchHit struct {
	ID               string        `json:"id"`
	Title            string        `json:"title"`
	Year             int           `json:"year"`
	Poster           string        `json:"poster"`
	Overview         string        `json:"overview"`
	Certification    string        `json:"certification"`
	Runtime          int           `json:"runtime"`
	OriginalLanguage string        `json:"original_language"`
	Studio           string        `json:"studio"`
	Genres           []string      `json:"genres"`
	TmdbPct          *float64      `json:"tmdb_pct"`
	Imdb             string        `json:"imdb"`
	Links            movieHitLinks `json:"links"`
}

type movieHitLinks struct {
	TMDB string `json:"tmdb"`
}

func toMovieHit(h providers.MovieSearchHit) movieSearchHit {
	genres := h.Genres
	if genres == nil {
		genres = []string{}
	}
	var pct *float64
	if h.VoteAverage > 0 {
		v := h.VoteAverage * 100
		pct = &v
	}
	return movieSearchHit{
		ID:               h.ID,
		Title:            h.Title,
		Year:             h.Year,
		Poster:           h.Poster,
		Overview:         h.Overview,
		Certification:    h.Certification,
		Runtime:          h.Runtime,
		OriginalLanguage: h.OriginalLanguage,
		Studio:           h.Studio,
		Genres:           genres,
		TmdbPct:          pct,
		Imdb:             h.ImdbURL,
		Links:            movieHitLinks{TMDB: "https://www.themoviedb.org/movie/" + h.ID},
	}
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

// searchArtists handles GET /api/v1/search/artists (L2 add-artist page). The
// response is {query, artists: [ArtistSearchHit], release_groups:
// [ReleaseGroupHit]} — L1 returned a flat [SearchResult]; extended in place
// because the music add flow is the only consumer (spec'd in the endpoint
// description). already_added is computed server-side against the music
// library (by mbid, then by exact name). A bare MusicBrainz id (or
// `lidarr:<id>`) in the query resolves to its artist directly.
func (s *Server) searchArtists(w http.ResponseWriter, r *http.Request) {
	if s.searchDeps.MusicBrainz == nil {
		writeError(w, http.StatusServiceUnavailable, "musicbrainz is not configured")
		return
	}
	q := r.URL.Query().Get("query")
	artistHits, err := s.searchDeps.MusicBrainz.SearchArtistHits(r.Context(), q)
	if err != nil {
		writeSearchError(w, "musicbrainz", err)
		return
	}
	groupHits, err := s.searchDeps.MusicBrainz.SearchReleaseGroupHits(r.Context(), q)
	if err != nil {
		writeSearchError(w, "musicbrainz", err)
		return
	}
	idByMbid, idByName := s.musicLibraryArtistIDs(r.Context())
	artists := make([]artistSearchHit, 0, len(artistHits))
	for _, h := range artistHits {
		wh := toArtistSearchHit(h)
		if id, ok := idByMbid[strings.ToLower(h.ID)]; ok {
			wh.AlreadyAdded = true
			wh.LibraryID = id
		} else if id, ok := idByName[strings.ToLower(strings.TrimSpace(h.Title))]; ok {
			wh.AlreadyAdded = true
			wh.LibraryID = id
		}
		artists = append(artists, wh)
	}
	releaseGroups := make([]releaseGroupHit, 0, len(groupHits))
	for _, g := range groupHits {
		releaseGroups = append(releaseGroups, toReleaseGroupHit(g))
	}
	writeJSON(w, http.StatusOK, artistSearchResult{Query: q, Artists: artists, ReleaseGroups: releaseGroups})
}

// artistSearchHit is the wire shape of one enriched artist hit (L2). Score
// stays 0 (MusicBrainz has no popularity number — the UI hides the heart).
type artistSearchHit struct {
	ID             string   `json:"id"`
	Title          string   `json:"title"`
	Type           string   `json:"type"`
	Score          float64  `json:"score"`
	Country        string   `json:"country"`
	Disambiguation string   `json:"disambiguation"`
	Genres         []string `json:"genres"`
	Links          []string `json:"links"`
	AlreadyAdded   bool     `json:"already_added"`
	// LibraryID is the music library artist row id when the hit is already
	// in the library (the add page jumps to its detail page); 0 otherwise.
	LibraryID int64 `json:"library_id"`
}

// releaseGroupHit is the wire shape of one release-group hit (L2).
type releaseGroupHit struct {
	ID           string  `json:"id"`
	Title        string  `json:"title"`
	Year         int     `json:"year"`
	ReleaseCount int     `json:"release_count"`
	Type         string  `json:"type"`
	Score        float64 `json:"score"`
	Artist       string  `json:"artist"`
}

// artistSearchResult is the wire shape of GET /api/v1/search/artists (L2).
type artistSearchResult struct {
	Query         string            `json:"query"`
	Artists       []artistSearchHit `json:"artists"`
	ReleaseGroups []releaseGroupHit `json:"release_groups"`
}

func toArtistSearchHit(h providers.ArtistSearchHit) artistSearchHit {
	genres := h.Genres
	if genres == nil {
		genres = []string{}
	}
	links := h.Links
	if links == nil {
		links = []string{}
	}
	return artistSearchHit{
		ID:             h.ID,
		Title:          h.Title,
		Type:           h.Type,
		Score:          h.Score,
		Country:        h.Country,
		Disambiguation: h.Disambiguation,
		Genres:         genres,
		Links:          links,
		AlreadyAdded:   h.AlreadyAdded,
	}
}

func toReleaseGroupHit(g providers.ReleaseGroupHit) releaseGroupHit {
	return releaseGroupHit{
		ID:           g.ID,
		Title:        g.Title,
		Year:         g.Year,
		ReleaseCount: g.ReleaseCount,
		Type:         g.Type,
		Score:        g.Score,
		Artist:       g.Artist,
	}
}

// musicLibraryArtistIDs returns lowercased mbid→id and name→id maps of the
// artists in the music library (the add page's already_added badge + the
// jump to the already-added artist's detail page). A server without the
// music collection wired (search-only) yields empty maps — nothing is
// already added.
func (s *Server) musicLibraryArtistIDs(ctx context.Context) (map[string]int64, map[string]int64) {
	byMbid := map[string]int64{}
	byName := map[string]int64{}
	if s.musicDeps == nil || s.musicDeps.Svc == nil {
		return byMbid, byName
	}
	artists, err := s.musicDeps.Svc.ListArtists(ctx)
	if err != nil {
		return byMbid, byName
	}
	for _, a := range artists {
		if a.MBID != "" {
			byMbid[strings.ToLower(a.MBID)] = a.ID
		}
		if n := strings.ToLower(strings.TrimSpace(a.Name)); n != "" {
			byName[n] = a.ID
		}
	}
	return byMbid, byName
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
		// L3: the music picker lists the SETTINGS-seeded Lidarr profiles
		// (the 4 factory defaults), not the release-matching domain
		// built-ins (the matching engine is a separate layer).
		return namesOfMusicProfiles(settings.DefaultMusicProfiles()), settings.DefaultMusicQualityProfile
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

func namesOfMusicProfiles(ps []settings.QualityProfile) []string {
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
