package api

import (
	"errors"
	"net/http"

	musicsvc "github.com/brandenk514/mediarr/internal/services/music"
)

// MusicDeps bundles what the music endpoints need.
type MusicDeps struct {
	Svc *musicsvc.Service
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
	s.mux.HandleFunc("POST /api/v1/music/artists", s.requireAuth(s.AddArtist))
	s.mux.HandleFunc("GET /api/v1/music/artists", s.requireAuth(s.ListArtists))
	s.mux.HandleFunc("GET /api/v1/music/artists/{id}", s.requireAuth(s.GetArtist))
	s.mux.HandleFunc("PUT /api/v1/music/artists/{id}", s.requireAuth(s.SetArtistMonitored))
	s.mux.HandleFunc("POST /api/v1/music/artists/{id}/albums", s.requireAuth(s.AddAlbum))
	s.mux.HandleFunc("GET /api/v1/music/artists/{id}/albums", s.requireAuth(s.ListAlbums))
	s.mux.HandleFunc("GET /api/v1/music/albums/{id}", s.requireAuth(s.GetAlbum))
	s.mux.HandleFunc("PUT /api/v1/music/albums/{id}", s.requireAuth(s.SetAlbumMonitored))
	s.mux.HandleFunc("POST /api/v1/music/albums/{id}/tracks", s.requireAuth(s.AddTrack))
	s.mux.HandleFunc("GET /api/v1/music/albums/{id}/tracks", s.requireAuth(s.ListTracks))
	s.mux.HandleFunc("PUT /api/v1/music/albums/{id}/tracks/{disc}/{number}", s.requireAuth(s.SetTrackMonitored))
	s.mux.HandleFunc("GET /api/v1/music/albums/{id}/wanted", s.requireAuth(s.ListAlbumWanted))
	s.mux.HandleFunc("POST /api/v1/music/artists/{id}/pipeline", s.requireAuth(s.RunMusicPipeline))
}

// addArtistRequest is the body for POST /api/v1/music. Monitored is a pointer
// so "omitted" (defaults to monitored) is distinguishable from an explicit
// false.
type addArtistRequest struct {
	Name      string `json:"name"`
	Monitored *bool  `json:"monitored"`
}

// AddArtist handles POST /api/v1/music/artists. An artist is monitored by default; an
// explicit "monitored": false opts out (PLAN §4 artist-level monitor).
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
	monitored := true
	if req.Monitored != nil {
		monitored = *req.Monitored
	}
	id, err := s.musicDeps.Svc.AddArtist(r.Context(), req.Name, monitored)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

// ListArtists handles GET /api/v1/music/artists.
func (s *Server) ListArtists(w http.ResponseWriter, r *http.Request) {
	artists, err := s.musicDeps.Svc.ListArtists(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, artists)
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

// addAlbumRequest is the body for POST /api/v1/music/{id}/albums. Monitored is
// a pointer so "omitted" (defaults to monitored) is distinguishable from an
// explicit false.
type addAlbumRequest struct {
	Name           string `json:"name"`
	Year           int    `json:"year"`
	QualityProfile string `json:"quality_profile"`
	Monitored      *bool  `json:"monitored"`
}

// AddAlbum handles POST /api/v1/music/{id}/albums. A newly added album is
// monitored by default (PLAN §4); an explicit "monitored": false opts out.
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
	monitored := true
	if req.Monitored != nil {
		monitored = *req.Monitored
	}
	albumID, err := s.musicDeps.Svc.AddAlbum(r.Context(), id, req.Name, req.Year, req.QualityProfile, monitored)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": albumID})
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
		switch {
		case err == musicsvc.ErrNoMatch:
			status = http.StatusUnprocessableEntity
		case err == musicsvc.ErrNoWanted:
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

// SetArtistMonitored handles PUT /api/v1/music/artists/{id}. It toggles the
// artist-level monitor, which fans out to the artist's known albums (PLAN §4).
func (s *Server) SetArtistMonitored(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req monitoredRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := s.musicDeps.Svc.SetArtistMonitored(r.Context(), id, req.Monitored); err != nil {
		writeMusicMonitoredError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "monitored": req.Monitored})
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
