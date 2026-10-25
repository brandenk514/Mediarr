package api

import (
	"net/http"
	"strconv"
	"time"

	hfdom "github.com/brandenk514/mediarr/internal/domains/historyfeed"
	hfservice "github.com/brandenk514/mediarr/internal/services/historyfeed"
)

// HistoryDeps bundles what the global history endpoint (P3 Activity >
// History) needs: the cross-kind history feed service.
type HistoryDeps struct {
	Svc *hfservice.Service
}

// SetHistory wires the global history endpoint:
//
//	GET /api/v1/history  — the cross-media event feed (since/event/limit)
//
// Called after construction, mirroring SetQueue.
func (s *Server) SetHistory(deps *HistoryDeps) {
	s.historyDeps = deps
	s.register("GET /api/v1/history", s.requireAuth(s.ListGlobalHistory))
}

// ListGlobalHistory handles GET /api/v1/history: the global event feed
// across all four kinds, newest first. ?since (RFC 3339, inclusive),
// ?event (case-insensitive exact match), and ?limit (default 50, clamped to
// 1..200) narrow the feed. An empty feed is a 200 with [].
func (s *Server) ListGlobalHistory(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	var since *time.Time
	if v := q.Get("since"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid since (want RFC 3339)")
			return
		}
		since = &t
	}
	event := q.Get("event")

	limit := hfdom.DefaultLimit
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, "invalid limit (want a positive integer)")
			return
		}
		limit = n
	}

	items, err := s.historyDeps.Svc.List(r.Context(), hfdom.Filter{
		Since: since,
		Event: event,
		Limit: limit,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load history")
		return
	}
	writeJSON(w, http.StatusOK, items)
}
