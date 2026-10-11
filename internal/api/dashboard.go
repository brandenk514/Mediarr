package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/brandenk514/mediarr/internal/dashboard"
	"github.com/brandenk514/mediarr/internal/indexers"
)

// DashboardDeps bundles what the dashboard endpoint (#40) needs: the cross-
// library snapshot repository and the indexer health rollups the "indexer
// health at a glance" card reads. Both are the same shared instances the rest
// of the app uses, so the dashboard reflects real state, not a copy.
type DashboardDeps struct {
	Repo    dashboard.Repo
	Tracker indexers.HealthTracker
	// DefaultRecent caps the recent-history / recent-queue feeds when the
	// caller does not pass ?recent=N. A non-positive value defers to the
	// repository default.
	DefaultRecent int
}

// SetDashboard wires GET /api/v1/dashboard (#40). Called after construction,
// mirroring SetAuth/SetIndexers, so NewServer stays usable for health-only
// boot and tests.
func (s *Server) SetDashboard(deps *DashboardDeps) {
	s.dashboardDeps = deps
	s.mux.HandleFunc("GET /api/v1/dashboard", s.requireAuth(s.Dashboard))
}

// recentClamp bounds ?recent=N so a hostile or buggy client can't turn a
// dashboard load into an unbounded table scan.
const (
	recentMin = 1
	recentMax = 100
)

// indexHealthJSON is the per-indexer entry of the dashboard's "indexer health
// at a glance" card. Status is a coarse tri-state the UI can style directly:
// ok has a recent success, error has a failure and no success, idle has never
// been observed.
type indexHealthJSON struct {
	Name        string     `json:"name"`
	Status      string     `json:"status"`
	Successes   int64      `json:"successes"`
	Failures    int64      `json:"failures"`
	LastError   string     `json:"last_error"`
	LastSuccess *time.Time `json:"last_success"`
	AvgLatency  int64      `json:"avg_latency_ns"`
}

func (s *Server) indexHealth() []indexHealthJSON {
	stats := s.dashboardDeps.Tracker.All()
	out := make([]indexHealthJSON, 0, len(stats))
	for _, st := range stats {
		status := "idle"
		switch {
		case st.Calls == 0:
			status = "idle"
		case st.LastError != "" && st.LastSuccess == nil:
			status = "error"
		case st.Failures > st.Successes:
			// More recent failures than successes: surface as degraded.
			status = "error"
		case st.Successes > 0:
			status = "ok"
		}
		out = append(out, indexHealthJSON{
			Name:        st.Name,
			Status:      status,
			Successes:   st.Successes,
			Failures:    st.Failures,
			LastError:   st.LastError,
			LastSuccess: st.LastSuccess,
			AvgLatency:  int64(st.AvgLatency),
		})
	}
	return out
}

// dashboardResponse is the wire shape for GET /api/v1/dashboard. It mirrors
// dashboard.Snapshot with the indexer health card appended.
type dashboardResponse struct {
	Libraries     []dashboard.Library     `json:"libraries"`
	Totals        dashboard.Totals        `json:"totals"`
	RecentHistory []dashboard.HistoryItem `json:"recent_history"`
	RecentQueue   []dashboard.QueueItem   `json:"recent_queue"`
	Indexers      []indexHealthJSON       `json:"indexers"`
	GeneratedAt   time.Time               `json:"generated_at"`
}

// Dashboard handles GET /api/v1/dashboard: the M6 overview — library counts
// per kind, active wants, queue depth, recent history, and indexer health at a
// glance. The ?recent=N query parameter bounds the two "recent" feeds.
func (s *Server) Dashboard(w http.ResponseWriter, r *http.Request) {
	recent := s.dashboardDeps.DefaultRecent
	if q := r.URL.Query().Get("recent"); q != "" {
		if n, err := strconv.Atoi(q); err == nil && n > 0 {
			recent = n
		}
	}
	if recent < recentMin {
		recent = recentMin
	}
	if recent > recentMax {
		recent = recentMax
	}

	snap, err := s.dashboardDeps.Repo.Snapshot(r.Context(), recent)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load dashboard")
		return
	}

	resp := dashboardResponse{
		Libraries:     snap.Libraries,
		Totals:        snap.Totals,
		RecentHistory: snap.RecentHistory,
		RecentQueue:   snap.RecentQueue,
		Indexers:      s.indexHealth(),
		GeneratedAt:   snap.GeneratedAt,
	}
	writeJSON(w, http.StatusOK, resp)
}
