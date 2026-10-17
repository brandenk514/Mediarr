package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/brandenk514/mediarr/internal/paging"
	"github.com/brandenk514/mediarr/internal/queue"
)

// QueueDeps bundles what the queue-view endpoints (#43) need: the cross-kind
// queue repository. It is the same shared instance the Postgres adapter is
// built from, so the queue view reflects real state.
type QueueDeps struct {
	Repo queue.Repo
}

// SetQueue wires the queue-view endpoints (#43):
//
//	GET /api/v1/queue                          — the full queue, filtered + paginated
//	GET /api/v1/queue/{kind}/{parent}/history  — one parent's lifecycle history
//
// Called after construction, mirroring SetDashboard, so NewServer stays usable
// for health-only boot and tests.
func (s *Server) SetQueue(deps *QueueDeps) {
	s.queueDeps = deps
	s.register("GET /api/v1/queue", s.requireAuth(s.ListQueue))
	s.register("GET /api/v1/queue/{kind}/{parent}/history", s.requireAuth(s.QueueHistory))
}

// queueKinds is the accepted set for the {kind} path segment and the ?kind=
// filter. They are the canonical singular names (matching dashboard.Kind).
var queueKinds = map[queue.Kind]bool{
	queue.KindMovie: true,
	queue.KindTV:    true,
	queue.KindMusic: true,
	queue.KindBook:  true,
}

// ListQueue handles GET /api/v1/queue: the *full* download/import queue across
// all four kinds, filtered (?kind, ?state, ?q) and paginated (?page,
// ?page_size) server-side, returned in the standard Page envelope. This is the
// dedicated queue view's data source — unlike the dashboard's bounded
// "recent" tail, it exposes the whole set with filters.
func (s *Server) ListQueue(w http.ResponseWriter, r *http.Request) {
	q := paging.QueryFromURLValues(r.URL.Query())
	f := queue.Filter{
		Kind:     strings.ToLower(strings.TrimSpace(r.URL.Query().Get("kind"))),
		State:    strings.ToLower(strings.TrimSpace(r.URL.Query().Get("state"))),
		Q:        q.Q,
		Page:     q.Page,
		PageSize: q.PageSize,
	}
	f.Normalize()

	items, err := s.queueDeps.Repo.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load queue")
		return
	}

	page, total := queue.Apply(items, f)
	writeJSON(w, http.StatusOK, paging.Page[queue.Item]{
		Items:    page,
		Total:    total,
		Page:     f.Page,
		PageSize: f.PageSize,
	})
}

// QueueHistory handles GET /api/v1/queue/{kind}/{parent}/history: the
// lifecycle events for one parent media item — the content of a queue row's
// "history" drill-down. An unknown kind or malformed parent id is a 400; a
// valid-but-empty history is a 200 with [].
func (s *Server) QueueHistory(w http.ResponseWriter, r *http.Request) {
	kind := queue.Kind(strings.ToLower(r.PathValue("kind")))
	if !queueKinds[kind] {
		writeError(w, http.StatusBadRequest, "unknown kind (want movie, tv, music, or book)")
		return
	}
	raw := r.PathValue("parent")
	parentID, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || parentID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid parent id")
		return
	}

	limit := paging.DefaultPageSize // sensible default; the UI asks for a bounded page
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > paging.MaxPageSize {
		limit = paging.MaxPageSize
	}

	events, err := s.queueDeps.Repo.History(r.Context(), kind, parentID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load history")
		return
	}
	writeJSON(w, http.StatusOK, events)
}
