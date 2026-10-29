package api

import (
	"net/http"
	"strconv"
	"strings"

	wdom "github.com/brandenk514/mediarr/internal/domains/wanted"
	"github.com/brandenk514/mediarr/internal/paging"
	wsvc "github.com/brandenk514/mediarr/internal/services/wanted"
)

// WantedDeps bundles what the global Wanted endpoints (P3 Wanted) need:
// the wanted service.
type WantedDeps struct {
	Svc *wsvc.Service
}

// SetWanted wires the global wanted endpoints:
//
//	GET  /api/v1/wanted               — tab page (?tab=missing|cutoff&search&page&pageSize)
//	POST /api/v1/wanted/search        — Search All for the tab's episodes
//	POST /api/v1/wanted/unmonitor     — batch-unmonitor selected episodes
//
// Called after construction, mirroring SetQueue.
func (s *Server) SetWanted(deps *WantedDeps) {
	s.wantedDeps = deps
	s.register("GET /api/v1/wanted", s.requireAuth(s.ListGlobalWanted))
	s.register("POST /api/v1/wanted/search", s.requireAuth(s.SearchWanted))
	s.register("POST /api/v1/wanted/unmonitor", s.requireAuth(s.UnmonitorWanted))
}

// ListGlobalWanted handles GET /api/v1/wanted: one server-side page of the
// tab's wanted episodes. ?kind=tv (default) | music (L4); ?tab=missing
// (default) | cutoff; ?search filters case-insensitively (TV: series
// titles; music: album or artist titles); ?page (default 1) and ?pageSize
// (default 10, max 500) page the result. The body is a PageWantedItem:
// {items,total,page,pageSize}. An unknown kind or tab is a 400. (The
// per-series endpoint is ListWanted in tv.go.)
func (s *Server) ListGlobalWanted(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	kind, ok := wantedKind(q.Get("kind"))
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown kind (want tv or music)")
		return
	}

	var tab wdom.Tab
	if v := q.Get("tab"); v != "" {
		tab = wdom.Tab(v)
	}
	if !tab.Normalize().Valid() {
		writeError(w, http.StatusBadRequest, "unknown tab (want missing or cutoff)")
		return
	}
	search := q.Get("search")

	page, _ := strconv.Atoi(q.Get("page"))
	pageSize, _ := strconv.Atoi(q.Get("pageSize"))

	var (
		p   paging.Page[wdom.Item]
		err error
	)
	if kind == "music" {
		p, err = s.wantedDeps.Svc.ListMusic(r.Context(), tab, search, page, pageSize)
	} else {
		p, err = s.wantedDeps.Svc.List(r.Context(), tab, search, page, pageSize)
	}
	if err != nil {
		switch err {
		case wdom.ErrUnknownTab:
			writeError(w, http.StatusBadRequest, "unknown tab (want missing or cutoff)")
		default:
			writeError(w, http.StatusInternalServerError, "failed to load wanted items")
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":    p.Items,
		"total":    p.Total,
		"page":     p.Page,
		"pageSize": p.PageSize,
	})
}

// wantedKind normalizes the kind parameter on the wanted endpoints: "" and
// "tv" keep the P3 TV behavior, "music" is the L4 music branch; anything
// else is unknown (400).
func wantedKind(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "tv":
		return "tv", true
	case "music":
		return "music", true
	}
	return "", false
}

// wantedSearchRequest is the body for POST /api/v1/wanted/search.
type wantedSearchRequest struct {
	Tab  string `json:"tab"`
	Kind string `json:"kind"`
}

// SearchWanted handles POST /api/v1/wanted/search: fans out a search for
// the tab's wanted episodes (kind=tv, default) or albums (kind=music) —
// 200 {searched: n}. An invalid tab or unknown kind is a 400.
func (s *Server) SearchWanted(w http.ResponseWriter, r *http.Request) {
	var req wantedSearchRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	kind, ok := wantedKind(req.Kind)
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown kind (want tv or music)")
		return
	}
	tab := wdom.Tab(req.Tab)
	if !tab.Normalize().Valid() {
		writeError(w, http.StatusBadRequest, "unknown tab (want missing or cutoff)")
		return
	}
	var (
		searched int
		err      error
	)
	if kind == "music" {
		searched, err = s.wantedDeps.Svc.SearchAllMusic(r.Context(), tab)
	} else {
		searched, err = s.wantedDeps.Svc.SearchAll(r.Context(), tab)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to search wanted items")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"searched": searched})
}

// unmonitorRequest is the body for POST /api/v1/wanted/unmonitor: a batch
// of episode refs (kind=tv, default) or music refs (kind=music).
type unmonitorRequest struct {
	Kind     string            `json:"kind"`
	Episodes []wdom.EpisodeRef `json:"episodes"`
	Music    []wdom.MusicRef   `json:"music"`
}

// UnmonitorWanted handles POST /api/v1/wanted/unmonitor: turns monitoring
// off for each episode (kind=tv, default) or album/track (kind=music) in
// the body and reports how many (200 {unmonitored: n}). An unknown kind is
// a 400.
func (s *Server) UnmonitorWanted(w http.ResponseWriter, r *http.Request) {
	var req unmonitorRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	kind, ok := wantedKind(req.Kind)
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown kind (want tv or music)")
		return
	}
	var (
		n   int
		err error
	)
	if kind == "music" {
		n, err = s.wantedDeps.Svc.UnmonitorMusic(r.Context(), req.Music)
	} else {
		n, err = s.wantedDeps.Svc.Unmonitor(r.Context(), req.Episodes)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to unmonitor wanted items")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"unmonitored": n})
}
