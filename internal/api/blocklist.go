package api

import (
	"net/http"
	"strconv"

	blkdom "github.com/brandenk514/mediarr/internal/domains/blocklist"
	blkservice "github.com/brandenk514/mediarr/internal/services/blocklist"
)

// BlocklistDeps bundles what the blocklist endpoints (P3 Activity >
// Blocklist) need: the blocklist service.
type BlocklistDeps struct {
	Svc *blkservice.Service
}

// SetBlocklist wires the blocklist endpoints:
//
//	GET    /api/v1/blocklist      — the blocked releases (?kind, ?parent_id)
//	POST   /api/v1/blocklist      — block a release
//	DELETE /api/v1/blocklist/{id} — unblock a release
//
// Called after construction, mirroring SetQueue.
func (s *Server) SetBlocklist(deps *BlocklistDeps) {
	s.blocklistDeps = deps
	s.register("GET /api/v1/blocklist", s.requireAuth(s.ListBlocklist))
	s.register("POST /api/v1/blocklist", s.requireAuth(s.AddBlocklist))
	s.register("DELETE /api/v1/blocklist/{id}", s.requireAuth(s.UnblockRelease))
}

// blocklistKinds is the accepted set for the ?kind= filter and the request
// body's kind.
var blocklistKinds = map[blkdom.Kind]bool{
	blkdom.KindMovie: true,
	blkdom.KindTV:    true,
	blkdom.KindMusic: true,
	blkdom.KindBook:  true,
}

// ListBlocklist handles GET /api/v1/blocklist: the blocked releases, newest
// first, narrowed by ?kind and/or ?parent_id. An empty blocklist is a 200
// with [].
func (s *Server) ListBlocklist(w http.ResponseWriter, r *http.Request) {
	var f blkdom.Filter
	if v := r.URL.Query().Get("kind"); v != "" {
		f.Kind = blkdom.Kind(v)
		if !blocklistKinds[f.Kind] {
			writeError(w, http.StatusBadRequest, "unknown kind (want movie, tv, music, or book)")
			return
		}
	}
	if v := r.URL.Query().Get("parent_id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil || id <= 0 {
			writeError(w, http.StatusBadRequest, "invalid parent_id")
			return
		}
		f.ParentID = id
	}

	items, err := s.blocklistDeps.Svc.List(r.Context(), f)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load blocklist")
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// blocklistRequest is the body for POST /api/v1/blocklist.
type blocklistRequest struct {
	Kind         string `json:"kind"`
	ParentID     int64  `json:"parent_id"`
	ReleaseTitle string `json:"release_title"`
	Indexer      string `json:"indexer"`
	Reason       string `json:"reason"`
	// L4 music display columns (Lidarr blocklist row: Album Title / Quality /
	// Formats). Optional; carried through for music rows, ignored otherwise.
	AlbumTitle string `json:"album_title"`
	Quality    string `json:"quality"`
	Formats    string `json:"formats"`
}

// AddBlocklist handles POST /api/v1/blocklist: records a rejected release.
// The kind must be canonical and the release title non-empty (400
// otherwise); a successful add is a 201 with the new id.
func (s *Server) AddBlocklist(w http.ResponseWriter, r *http.Request) {
	var req blocklistRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	e := blkdom.Entry{
		Kind:         blkdom.Kind(req.Kind),
		ParentID:     req.ParentID,
		ReleaseTitle: req.ReleaseTitle,
		Indexer:      req.Indexer,
		Reason:       req.Reason,
		AlbumTitle:   req.AlbumTitle,
		Quality:      req.Quality,
		Formats:      req.Formats,
	}
	id, err := s.blocklistDeps.Svc.Add(r.Context(), e)
	if err != nil {
		switch err {
		case blkdom.ErrInvalidKind:
			writeError(w, http.StatusBadRequest, "unknown kind (want movie, tv, music, or book)")
		case blkdom.ErrEmptyTitle:
			writeError(w, http.StatusBadRequest, "release_title is required")
		default:
			writeError(w, http.StatusInternalServerError, "failed to block release")
		}
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

// UnblockRelease handles DELETE /api/v1/blocklist/{id}: unblocks the
// release (204). An unknown id is a 404.
func (s *Server) UnblockRelease(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	deleted, err := s.blocklistDeps.Svc.Unblock(r.Context(), id)
	if err != nil {
		switch err {
		case blkdom.ErrInvalidID:
			writeError(w, http.StatusBadRequest, "invalid id")
		default:
			writeError(w, http.StatusInternalServerError, "failed to unblock release")
		}
		return
	}
	if !deleted {
		writeError(w, http.StatusNotFound, "blocklist entry not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
