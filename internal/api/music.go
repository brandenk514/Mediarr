package api

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	musicdom "github.com/brandenk514/mediarr/internal/domains/music"
	"github.com/brandenk514/mediarr/internal/paging"
	musicsvc "github.com/brandenk514/mediarr/internal/services/music"
	rfsvc "github.com/brandenk514/mediarr/internal/services/rootfolders"
	"github.com/brandenk514/mediarr/internal/settings"
)

// MusicDeps bundles what the music endpoints need.
type MusicDeps struct {
	Svc *musicsvc.Service
	// RootFolders, when non-nil, backs POST /api/v1/music/import's
	// root_folder_id resolution (L4). Nil-safe: import by path still works
	// without it.
	RootFolders *rfsvc.Service
}

// SetMusic wires the music endpoints. Called after construction, mirroring
// SetTV, so NewServer stays usable for health-only boot and tests.
//
// Artist routes are prefixed /api/v1/music/artists, not the bare
// /api/v1/music/{id}. The alternative would register the artist's album
// collection (GET /api/v1/music/{id}/albums) alongside the album's by-id
// route (GET /api/v1/music/albums/{id}); those two patterns are ambiguous
// for /api/v1/music/albums/albums, and Go 1.22+'s ServeMux panics on the
// registration — which kills the process at startup before the HTTP server
// ever runs. The prefixed artist routes are disjoint from the prefixed
// album routes, so the whole table registers cleanly (guarded by
// TestMusicRoutesRegister).
func (s *Server) SetMusic(deps *MusicDeps) {
	s.musicDeps = deps
	s.register("POST /api/v1/music/artists", s.requireAuth(s.AddArtist))
	s.register("GET /api/v1/music/artists", s.requireAuth(s.ListArtists))
	s.register("GET /api/v1/music/artists/{id}", s.requireAuth(s.GetArtist))
	s.register("PUT /api/v1/music/artists/{id}", s.requireAuth(s.SetArtistMonitored))
	s.register("GET /api/v1/music/artists/{id}/history", s.requireAuth(s.GetArtistHistory))
	s.register("GET /api/v1/music/library/summary", s.requireAuth(s.MusicLibrarySummary))
	s.register("POST /api/v1/music/artists/{id}/albums", s.requireAuth(s.AddAlbum))
	s.register("GET /api/v1/music/artists/{id}/albums", s.requireAuth(s.ListAlbums))
	s.register("GET /api/v1/music/albums/{id}", s.requireAuth(s.GetAlbum))
	s.register("PUT /api/v1/music/albums/{id}", s.requireAuth(s.SetAlbumMonitored))
	s.register("POST /api/v1/music/albums/{id}/tracks", s.requireAuth(s.AddTrack))
	s.register("GET /api/v1/music/albums/{id}/tracks", s.requireAuth(s.ListTracks))
	s.register("PUT /api/v1/music/albums/{id}/tracks/{disc}/{number}", s.requireAuth(s.SetTrackMonitored))
	s.register("GET /api/v1/music/albums/{id}/wanted", s.requireAuth(s.ListAlbumWanted))
	s.register("POST /api/v1/music/artists/{id}/pipeline", s.requireAuth(s.RunMusicPipeline))
	s.register("POST /api/v1/music/artists/{id}/refresh", s.requireAuth(s.RefreshArtist))
	s.register("POST /api/v1/music/import", s.requireAuth(s.ImportMusicLibrary))
}

// addArtistRequest is the body for POST /api/v1/music. Monitored is a pointer
// so "omitted" (defaults to monitored) is distinguishable from an explicit
// false. MBID is the MusicBrainz id of the selected result (M6 #42); "" = unset.
// QualityProfile and Path are the optional L1/L2 modal fields: "" = none /
// same-named subfolder under the root folder. MonitorPolicy and
// MonitorNewAlbums are the L2 monitoring-model fields ("" = all); Tags are
// the L2 free-form tags (nil/absent = none).
type addArtistRequest struct {
	Name             string   `json:"name"`
	MBID             string   `json:"mbid"`
	Monitored        *bool    `json:"monitored"`
	QualityProfile   string   `json:"quality_profile"`
	Path             string   `json:"path"`
	MonitorPolicy    string   `json:"monitor_policy"`
	MonitorNewAlbums string   `json:"monitor_new_albums"`
	Tags             []string `json:"tags"`
}

// AddArtist handles POST /api/v1/music/artists. An artist is monitored by
// default; an explicit "monitored": false opts out (PLAN §4 artist-level
// monitor). Omitted quality_profile/path keep the pre-L1 behavior; omitted
// monitor_policy/monitor_new_albums/tags store the empty (= all / none)
// defaults.
func (s *Server) AddArtist(w http.ResponseWriter, r *http.Request) {
	var req addArtistRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	// L3: an explicit quality_profile must be one of the seeded Lidarr music
	// profile names (the /profiles?kind=music set). Empty keeps the pre-L3
	// behavior (the app default applies on import).
	if req.QualityProfile != "" && !settings.MusicProfileNames()[req.QualityProfile] {
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf("unknown quality profile %q; use one of: Lossless Best Effort, High Quality Lossy, Lossless, WAV",
				req.QualityProfile))
		return
	}
	monitored := true
	if req.Monitored != nil {
		monitored = *req.Monitored
	}
	id, err := s.musicDeps.Svc.AddArtist(r.Context(), musicsvc.AddArtistInput{
		Name:             req.Name,
		MBID:             req.MBID,
		Monitored:        monitored,
		QualityProfile:   req.QualityProfile,
		Path:             req.Path,
		MonitorPolicy:    req.MonitorPolicy,
		MonitorNewAlbums: req.MonitorNewAlbums,
		Tags:             req.Tags,
	})
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

// ListArtists handles GET /api/v1/music/artists. It returns a server-side
// filtered, paginated envelope (M6 #41): ?q=<search>&monitored=<bool>&page=&page_size=.
func (s *Server) ListArtists(w http.ResponseWriter, r *http.Request) {
	q := paging.QueryFromURLValues(r.URL.Query())
	page, err := s.musicDeps.Svc.ListArtistsQuery(r.Context(), q)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, page)
}

// GetArtist handles GET /api/v1/music/artists/{id}.
func (s *Server) GetArtist(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	artist, err := s.musicDeps.Svc.GetArtist(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, artist)
}

// GetArtistHistory handles GET /api/v1/music/artists/{id}/history (Lidarr L1
// detail toolbar). ?limit clamps to 1..1000, defaulting to 200. A missing
// artist is 404.
func (s *Server) GetArtistHistory(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	limit := 200
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 1000 {
		limit = 1000
	}
	h, err := s.musicDeps.Svc.GetArtistHistory(r.Context(), id, limit)
	if err != nil {
		writeMusicMonitoredError(w, err)
		return
	}
	if h == nil {
		h = []musicdom.HistoryEntry{}
	}
	writeJSON(w, http.StatusOK, h)
}

// MusicLibrarySummary handles GET /api/v1/music/library/summary: the
// library-wide footer status bar counts (Lidarr-specific, no Sonarr
// equivalent).
func (s *Server) MusicLibrarySummary(w http.ResponseWriter, r *http.Request) {
	summary, err := s.musicDeps.Svc.LibrarySummary(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

// addAlbumRequest is the body for POST /api/v1/music/{id}/albums. Monitored
// is a pointer: "omitted" derives the value from the artist's L2 monitor
// policy, an explicit false opts out, an explicit true forces on.
// ReleaseGroupMBID (L4) is an optional MusicBrainz release-group (or bare
// release) id: when present, the album and its tracks are enriched from the
// keyless metadata adapter, and adapter failure is surfaced in the 201
// warnings array (the add itself still succeeds).
type addAlbumRequest struct {
	Name             string `json:"name"`
	Year             int    `json:"year"`
	QualityProfile   string `json:"quality_profile"`
	Monitored        *bool  `json:"monitored"`
	ReleaseGroupMBID string `json:"release_group_mb_id"`
}

// AddAlbum handles POST /api/v1/music/{id}/albums. A newly added album
// follows the artist's monitor policy (L2) unless the request sets
// "monitored" explicitly (PLAN §4; Lidarr L2 policy application). L4: an
// optional release_group_mb_id enriches the album + tracks from
// MusicBrainz; the 201 body is {id, warnings} with warnings always present
// (empty when enrichment succeeded or was not requested).
func (s *Server) AddAlbum(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req addAlbumRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	albumID, warnings, err := s.musicDeps.Svc.AddAlbum(r.Context(), id, req.Name, req.Year, req.QualityProfile, req.Monitored, req.ReleaseGroupMBID)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": albumID, "warnings": warnings})
}

// RefreshArtist handles POST /api/v1/music/artists/{id}/refresh (L4): the
// keyless adapter re-fetches the artist's metadata (status, genres, links,
// cover) and refreshes every album with a release MBID (metadata + track
// replacement, surviving tracks keep their monitored flag). Response: the
// refreshed artist plus the {albums_refreshed, albums_failed} report.
func (s *Server) RefreshArtist(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	artist, report, err := s.musicDeps.Svc.RefreshArtist(r.Context(), id)
	if err != nil {
		writeMusicMonitoredError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"artist": artist, "report": report})
}

// musicImportRequest is the body for POST /api/v1/music/import (L4).
// Exactly one of root_folder_id (a registered root folder) or path (an
// ad-hoc path, the "unmapped" case) is required — same shape as the TV
// import.
type musicImportRequest struct {
	RootFolderID int64  `json:"root_folder_id"`
	Path         string `json:"path"`
}

// ImportMusicLibrary handles POST /api/v1/music/import (L4, Lidarr
// import phase 1, mirroring the TV wizard): it scans a root folder,
// matches the candidate artist folders against the tracked artists, and
// reports them for confirmation. Phase 1 ends at the report — no file
// moves and no artist creation from this endpoint (the UI adds selected
// new artists through the existing add-artist flow).
func (s *Server) ImportMusicLibrary(w http.ResponseWriter, r *http.Request) {
	var req musicImportRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	var rootPath string
	if req.RootFolderID > 0 {
		if s.musicDeps.RootFolders == nil {
			writeError(w, http.StatusBadRequest, "root folder registry is not configured")
			return
		}
		rf, err := s.musicDeps.RootFolders.Get(req.RootFolderID)
		if err != nil {
			writeError(w, http.StatusNotFound, "root folder not found")
			return
		}
		rootPath = rf.Path
	} else {
		rootPath = req.Path
	}
	if rootPath == "" {
		writeError(w, http.StatusBadRequest, "root_folder_id or path is required")
		return
	}
	// Map filesystem problems to a clean 404/400 before the scan.
	if fi, err := os.Stat(filepath.Clean(rootPath)); err != nil {
		writeError(w, http.StatusNotFound, "root folder not found: "+rootPath)
		return
	} else if !fi.IsDir() {
		writeError(w, http.StatusBadRequest, "root folder is not a directory: "+rootPath)
		return
	}
	report, err := s.musicDeps.Svc.ScanMusicLibrary(r.Context(), rootPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// ListAlbums handles GET /api/v1/music/{id}/albums.
func (s *Server) ListAlbums(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	albums, err := s.musicDeps.Svc.ListAlbums(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, albums)
}

// GetAlbum handles GET /api/v1/music/albums/{id}.
func (s *Server) GetAlbum(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	album, err := s.musicDeps.Svc.GetAlbum(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, album)
}

// addTrackRequest is the body for POST /api/v1/music/albums/{id}/tracks.
// Monitored is a pointer so "omitted" (defaults to monitored) is distinguishable
// from an explicit false.
type addTrackRequest struct {
	Disc      int    `json:"disc"`
	Number    int    `json:"number"`
	Title     string `json:"title"`
	Monitored *bool  `json:"monitored"`
}

// AddTrack handles POST /api/v1/music/albums/{id}/tracks. A newly added track
// is monitored by default (PLAN §4 track-level monitor); an explicit
// "monitored": false opts out.
func (s *Server) AddTrack(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req addTrackRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Disc <= 0 || req.Number <= 0 {
		writeError(w, http.StatusBadRequest, "disc and number must be positive")
		return
	}
	monitored := true
	if req.Monitored != nil {
		monitored = *req.Monitored
	}
	_, err := s.musicDeps.Svc.AddTrack(r.Context(), id, req.Disc, req.Number, req.Title, monitored)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{})
}

// ListTracks handles GET /api/v1/music/albums/{id}/tracks.
func (s *Server) ListTracks(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	tracks, err := s.musicDeps.Svc.ListTracks(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, tracks)
}

// ListAlbumWanted handles GET /api/v1/music/albums/{id}/wanted (album-level and
// track-level wanted entries with their status).
func (s *Server) ListAlbumWanted(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	wants, err := s.musicDeps.Svc.ListWanted(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, wants)
}

// RunMusicPipeline handles POST /api/v1/music/artists/{id}/pipeline.
func (s *Server) RunMusicPipeline(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	imported, err := s.musicDeps.Svc.RunPipeline(r.Context(), id)
	if err != nil {
		status := http.StatusInternalServerError
		switch err {
		case musicsvc.ErrNoMatch:
			status = http.StatusUnprocessableEntity
		case musicsvc.ErrNoWanted:
			status = http.StatusNoContent
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"imported": imported})
}

// writeMusicMonitoredError maps a toggle error to the right status: an unknown
// artist, album, or track is 404, otherwise 500.
func writeMusicMonitoredError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	if errors.Is(err, musicsvc.ErrArtistNotFound) ||
		errors.Is(err, musicsvc.ErrAlbumNotFound) ||
		errors.Is(err, musicsvc.ErrTrackNotFound) {
		status = http.StatusNotFound
	}
	writeError(w, status, err.Error())
}

// updateArtistRequest is the body for PUT /api/v1/music/artists/{id} (L2 Edit
// Artist modal). Every field is a pointer: omitted = keep the current value
// (the L1 toggle sends only {"monitored": bool}), provided = apply — an
// explicit empty string for policy/profile/path is a real value. When
// monitored is provided and differs from the current state, the change fans
// out to the artist's known albums.
type updateArtistRequest struct {
	Monitored        *bool     `json:"monitored"`
	MonitorPolicy    *string   `json:"monitor_policy"`
	MonitorNewAlbums *string   `json:"monitor_new_albums"`
	QualityProfile   *string   `json:"quality_profile"`
	Path             *string   `json:"path"`
	Tags             *[]string `json:"tags"`
}

// SetArtistMonitored handles PUT /api/v1/music/artists/{id}. L1: the
// monitored toggle ({"monitored": bool} — the response's monitored state is
// what the L1 UI checkbox binds to). L2: the full Edit Artist modal
// (Monitored + Monitor Policy + Monitor New Albums + Quality Profile + Path
// + Tags) persists through the same endpoint.
func (s *Server) SetArtistMonitored(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req updateArtistRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Monitored == nil && req.MonitorPolicy == nil && req.MonitorNewAlbums == nil &&
		req.QualityProfile == nil && req.Path == nil && req.Tags == nil {
		writeError(w, http.StatusBadRequest, "monitored is required")
		return
	}
	a, err := s.musicDeps.Svc.UpdateArtist(r.Context(), id, musicsvc.UpdateArtistInput{
		Monitored:        req.Monitored,
		MonitorPolicy:    req.MonitorPolicy,
		MonitorNewAlbums: req.MonitorNewAlbums,
		QualityProfile:   req.QualityProfile,
		Path:             req.Path,
		Tags:             req.Tags,
	})
	if err != nil {
		writeMusicMonitoredError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "monitored": a.Monitored})
}

// SetAlbumMonitored handles PUT /api/v1/music/albums/{id}. It toggles a single
// album's monitor.
func (s *Server) SetAlbumMonitored(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req monitoredRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := s.musicDeps.Svc.SetAlbumMonitored(r.Context(), id, req.Monitored); err != nil {
		writeMusicMonitoredError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "monitored": req.Monitored})
}

// SetTrackMonitored handles PUT /api/v1/music/albums/{id}/tracks/{disc}/{number}.
func (s *Server) SetTrackMonitored(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	disc, ok := pathInt(w, r.PathValue("disc"), "disc")
	if !ok {
		return
	}
	number, ok := pathInt(w, r.PathValue("number"), "number")
	if !ok {
		return
	}
	var req monitoredRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := s.musicDeps.Svc.SetTrackMonitored(r.Context(), id, disc, number, req.Monitored); err != nil {
		writeMusicMonitoredError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"disc": disc, "number": number, "monitored": req.Monitored})
}
