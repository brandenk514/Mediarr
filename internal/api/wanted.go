package api

import (
	"net/http"
	"strconv"

	wdom "github.com/brandenk514/mediarr/internal/domains/wanted"
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
// tab's wanted episodes. ?tab=missing (default) | cutoff; ?search filters
// case-insensitively against series/episode/episode titles; ?page (default
// 1) and ?pageSize (default 10, max 500) page the result. The body is a
// PageWantedItem: {items,total,page,pageSize}. (The per-series endpoint is
// ListWanted in tv.go.)
func (s *Server) ListGlobalWanted(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

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

	p, err := s.wantedDeps.Svc.List(r.Context(), tab, search, page, pageSize)
	if err != nil {
		switch err {
		case wdom.ErrUnknownTab:
			writeError(w, http.StatusBadRequest, "unknown tab (want missing or cutoff)")
		default:
			writeError(w, http.StatusInternalServerError, "failed to load wanted episodes")
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

// wantedSearchRequest is the body for POST /api/v1/wanted/search.
type wantedSearchRequest struct {
	Tab string `json:"tab"`
}

// SearchWanted handles POST /api/v1/wanted/search: fans out a search for
// the tab's wanted episodes (200 {searched: n}). An invalid tab is a 400.
func (s *Server) SearchWanted(w http.ResponseWriter, r *http.Request) {
	var req wantedSearchRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	tab := wdom.Tab(req.Tab)
	if !tab.Normalize().Valid() {
		writeError(w, http.StatusBadRequest, "unknown tab (want missing or cutoff)")
		return
	}
	searched, err := s.wantedDeps.Svc.SearchAll(r.Context(), tab)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to search wanted episodes")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"searched": searched})
}

// unmonitorRequest is the body for POST /api/v1/wanted/unmonitor: a batch
// of episode refs.
type unmonitorRequest struct {
	Episodes []wdom.EpisodeRef `json:"episodes"`
}

// UnmonitorWanted handles POST /api/v1/wanted/unmonitor: turns monitoring
// off for each episode in the body and reports how many (200
// {unmonitored: n}).
func (s *Server) UnmonitorWanted(w http.ResponseWriter, r *http.Request) {
	var req unmonitorRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	n, err := s.wantedDeps.Svc.Unmonitor(r.Context(), req.Episodes)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to unmonitor episodes")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"unmonitored": n})
}
