package api

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/brandenk514/mediarr/internal/downloads"
	"github.com/brandenk514/mediarr/internal/indexers"
	"github.com/brandenk514/mediarr/internal/paging"
)

// StatsWindowDays is the number of UTC-day buckets the stats summary emits
// (the last 30 days, including zero days so the UI's x-axis is stable).
const StatsWindowDays = 30

// IndexerSearchDeps bundles what the Prowlarr PR3 manual-search endpoints need
// (POST /api/v1/search, /search/grab, GET/DELETE /search/history, and the
// /indexers/stats/summary rollup). It is a distinct deps bundle from the
// per-media SearchDeps (the TMDB add-flow search) and from IndexersDeps (the
// hub's read/health/write endpoints), so each concern wires independently.
type IndexerSearchDeps struct {
	// Indexers loads the enabled definitions the fan-out is built from and the
	// active_indexers count.
	Indexers indexers.IndexerRepo
	// Registry builds a provider per definition (the manual-search workhorse).
	Registry *indexers.Registry
	// Activity persists query history and grabs and feeds the stats summary.
	Activity indexers.SearchActivityRepo
	// Client is the configured download client. A nil client means no
	// download backend is available: a grab then returns 503.
	Client downloads.Client
	// ClientKind is the configured client kind, used to route a grab by
	// protocol (torrent/torrent-file to qBittorrent, nzb to SABnzbd; the mock
	// accepts any).
	ClientKind downloads.ClientKind
}

// SetIndexerSearch wires the manual-search, grab, history, and stats-summary
// endpoints. Called after construction, mirroring SetIndexers/SetSearch, so
// NewServer stays usable for health-only boot and tests.
func (s *Server) SetIndexerSearch(deps *IndexerSearchDeps) {
	s.indexerSearchDeps = deps
	s.register("POST /api/v1/search", s.requireAuth(s.ManualSearch))
	s.register("POST /api/v1/search/grab", s.requireAuth(s.GrabRelease))
	s.register("GET /api/v1/search/history", s.requireAuth(s.SearchHistory))
	s.register("DELETE /api/v1/search/history", s.requireAuth(s.ClearSearchHistory))
	s.register("GET /api/v1/indexers/stats/summary", s.requireAuth(s.IndexerStatsSummary))
}

// ---- POST /api/v1/search ----

type manualSearchRequest struct {
	Query      string   `json:"query"`
	IndexerIDs []int64  `json:"indexer_ids"`
	Categories []string `json:"categories"`
}

// searchReleaseJSON is the wire shape for one candidate release (no
// cross-indexer dedup). It is a flat projection of indexers.SearchResult.
type searchReleaseJSON struct {
	Title     string `json:"title"`
	Indexer   string `json:"indexer"`
	SizeBytes int64  `json:"size_bytes"`
	Seeders   int    `json:"seeders"`
	Protocol  string `json:"protocol"`
	URL       string `json:"url"`
	Category  string `json:"category"`
}

// selectDefs returns the subset of enabled definitions to search. An empty id
// list means "all enabled"; a non-empty list keeps only enabled indexers whose
// id appears in it (unknown/disabled ids are silently filtered, not errors).
func selectDefs(enabled []indexers.Definition, ids []int64) []indexers.Definition {
	if len(ids) == 0 {
		return enabled
	}
	want := make(map[int64]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	var out []indexers.Definition
	for _, def := range enabled {
		if want[def.ID] {
			out = append(out, def)
		}
	}
	return out
}

// ManualSearch handles POST /api/v1/search: a manual release search fanned out
// over the selected (or all enabled) indexers concurrently. One query-history
// row is recorded per attempted indexer, including failures.
func (s *Server) ManualSearch(w http.ResponseWriter, r *http.Request) {
	d := s.indexerSearchDeps
	var req manualSearchRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Query) == "" {
		writeError(w, http.StatusBadRequest, "query is required")
		return
	}
	ctx := r.Context()
	enabled, err := d.Indexers.ListEnabled(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load indexers")
		return
	}
	defs := selectDefs(enabled, req.IndexerIDs)
	if len(defs) == 0 {
		// Nothing to search (no enabled indexers, or every requested id was
		// filtered out). An empty array is a valid, non-error response.
		writeJSON(w, http.StatusOK, []searchReleaseJSON{})
		return
	}

	q := indexers.SearchQuery{Term: req.Query, Categories: req.Categories}
	// timeout 0 → the layer's DefaultSearchTimeout (per-indexer 30s).
	results := indexers.SearchAll(ctx, d.Registry, defs, q, 0)

	// A cancelled/aborted request: every per-indexer attempt carries the
	// context error. Fail the whole call (per the spec) and skip history.
	if ctx.Err() != nil {
		writeError(w, http.StatusInternalServerError, "search request was cancelled")
		return
	}

	now := time.Now().UTC()
	out := make([]searchReleaseJSON, 0)
	for i, m := range results {
		def := defs[i] // SearchAll returns one result per def, in input order
		id := def.ID
		if err := d.Activity.RecordQuery(ctx, indexers.QueryLog{
			IndexerID:  &id,
			Indexer:    def.Name,
			Term:       req.Query,
			Categories: req.Categories,
			OK:         m.Err == nil,
			ElapsedMS:  m.Elapsed.Milliseconds(),
			At:         now,
		}); err != nil {
			// A history-write failure must not fail the search itself (the
			// releases are the primary payload); the row is simply skipped.
		}
		for _, res := range m.Results {
			out = append(out, searchReleaseJSON{
				Title:     res.Title,
				Indexer:   res.Indexer,
				SizeBytes: res.SizeBytes,
				Seeders:   res.Info.Seeders,
				Protocol:  res.Info.Protocol,
				URL:       res.Info.URL,
				Category:  res.Category,
			})
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// ---- POST /api/v1/search/grab ----

type grabRequest struct {
	Title     string `json:"title"`
	Indexer   string `json:"indexer"`
	Protocol  string `json:"protocol"`
	URL       string `json:"url"`
	SizeBytes int64  `json:"size_bytes"`
	Seeders   int    `json:"seeders"`
}

type grabResultJSON struct {
	OK       bool   `json:"ok"`
	Indexer  string `json:"indexer"`
	Protocol string `json:"protocol"`
}

// routeProtocol resolves a search-result protocol to the configured download
// client. The mock client (a test double) accepts any protocol; qBittorrent
// handles torrent/torrent-file; SABnzbd handles nzb. ok is false when the
// configured client cannot handle the protocol (the endpoint then 409s).
func routeProtocol(client downloads.Client, kind downloads.ClientKind, protocol string) (downloads.Client, bool) {
	switch kind {
	case downloads.KindMock, "":
		return client, true
	case downloads.KindQB:
		if protocol == indexers.ProtocolTorrent || protocol == indexers.ProtocolTorrentFile {
			return client, true
		}
		return nil, false
	case downloads.KindSAB:
		if protocol == indexers.ProtocolNZB {
			return client, true
		}
		return nil, false
	default:
		// A future client kind is unknown to the router; be permissive so a
		// new kind keeps working until the router learns its protocols.
		return client, true
	}
}

// grabFileName is a safe on-disk name for the mock client (which writes one
// file per grab). Real clients name from the release/URL; this is a fallback.
func grabFileName(title string) string {
	safe := strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			return '_'
		}
		return r
	}, title)
	if strings.TrimSpace(safe) == "" {
		return "release"
	}
	return safe
}

// GrabRelease handles POST /api/v1/search/grab: it routes a search result to
// the configured download client by protocol and records the grab on success.
func (s *Server) GrabRelease(w http.ResponseWriter, r *http.Request) {
	d := s.indexerSearchDeps
	var req grabRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	ctx := r.Context()
	if d.Client == nil {
		writeError(w, http.StatusServiceUnavailable, "no download client is configured")
		return
	}
	client, ok := routeProtocol(d.Client, d.ClientKind, req.Protocol)
	if !ok {
		writeError(w, http.StatusConflict,
			fmt.Sprintf("the configured %s client cannot handle %q releases", d.ClientKind, req.Protocol))
		return
	}
	release := downloads.Release{
		Title:    req.Title,
		FileName: grabFileName(req.Title),
		URL:      req.URL,
	}
	if err := client.Add(ctx, release); err != nil {
		writeError(w, http.StatusInternalServerError,
			fmt.Sprintf("failed to send release to download client: %v", err))
		return
	}
	if err := d.Activity.RecordGrab(ctx, indexers.GrabLog{
		Indexer:  req.Indexer,
		Title:    req.Title,
		Protocol: req.Protocol,
		At:       time.Now().UTC(),
	}); err != nil {
		// The release was handed to the client; a history-write failure is
		// non-fatal (stats will undercount, but the download proceeded).
	}
	writeJSON(w, http.StatusOK, grabResultJSON{OK: true, Indexer: req.Indexer, Protocol: req.Protocol})
}

// ---- GET /api/v1/search/history ----

type searchHistoryItemJSON struct {
	ID         int64     `json:"id"`
	Indexer    string    `json:"indexer"`
	Term       string    `json:"term"`
	Categories []string  `json:"categories"`
	OK         bool      `json:"ok"`
	ElapsedMS  int64     `json:"elapsed_ms"`
	At         time.Time `json:"at"`
}

type searchHistoryJSON struct {
	Items []searchHistoryItemJSON `json:"items"`
	Total int                     `json:"total"`
}

// SearchHistory handles GET /api/v1/search/history: one page of query history
// (newest first) with the `indexer` (exact) and `q` (case-insensitive
// substring) filters.
func (s *Server) SearchHistory(w http.ResponseWriter, r *http.Request) {
	d := s.indexerSearchDeps
	pq := paging.QueryFromURLValues(r.URL.Query())
	pq.Normalize()
	filter := indexers.QueryFilter{
		Indexer:  strings.TrimSpace(r.URL.Query().Get("indexer")),
		Term:     pq.Q,
		Page:     pq.Page,
		PageSize: pq.PageSize,
	}
	items, total, err := d.Activity.ListQueries(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load search history")
		return
	}
	out := make([]searchHistoryItemJSON, 0, len(items))
	for _, it := range items {
		cats := it.Categories
		if cats == nil {
			cats = []string{}
		}
		out = append(out, searchHistoryItemJSON{
			ID:         it.ID,
			Indexer:    it.Indexer,
			Term:       it.Term,
			Categories: cats,
			OK:         it.OK,
			ElapsedMS:  it.ElapsedMS,
			At:         it.At,
		})
	}
	writeJSON(w, http.StatusOK, searchHistoryJSON{Items: out, Total: total})
}

// ---- DELETE /api/v1/search/history ----

type historyClearedJSON struct {
	Deleted int `json:"deleted"`
}

// ClearSearchHistory handles DELETE /api/v1/search/history (the Prowlarr
// "Clear" action): it deletes every query-history row.
func (s *Server) ClearSearchHistory(w http.ResponseWriter, r *http.Request) {
	d := s.indexerSearchDeps
	n, err := d.Activity.DeleteAllQueries(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clear search history")
		return
	}
	writeJSON(w, http.StatusOK, historyClearedJSON{Deleted: n})
}

// ---- GET /api/v1/indexers/stats/summary ----

type dayBucketJSON struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

type indexerSeriesJSON struct {
	Indexer string          `json:"indexer"`
	Queries []dayBucketJSON `json:"queries"`
	Grabs   []dayBucketJSON `json:"grabs"`
}

type statsSeriesJSON struct {
	Queries   []dayBucketJSON     `json:"queries"`
	Grabs     []dayBucketJSON     `json:"grabs"`
	ByIndexer []indexerSeriesJSON `json:"by_indexer"`
}

type statsSummaryJSON struct {
	ActiveIndexers int             `json:"active_indexers"`
	ActiveApps     int             `json:"active_apps"`
	TotalQueries   int             `json:"total_queries"`
	TotalGrabs     int             `json:"total_grabs"`
	Series         statsSeriesJSON `json:"series"`
}

// buildStatsSeries produces the StatsWindowDays-day UTC stats series ending on
// now's UTC day, zero-filling every missing day and splitting per indexer.
// It is a pure function (no I/O) so the day-bucketing math is unit-testable
// without a database. Buckets are chronological (oldest day first, today last)
// so the UI's x-axis reads left-to-right.
func buildStatsSeries(now time.Time, counts []indexers.DailyCount) statsSeriesJSON {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	const n = StatsWindowDays

	// Label each of the n days and map its date string to a bucket index.
	label := make([]string, n)
	idx := make(map[string]int, n)
	for i := 0; i < n; i++ {
		d := today.AddDate(0, 0, i-n+1) // i=0 → today-(n-1); i=n-1 → today
		s := d.Format("2006-01-02")
		label[i] = s
		idx[s] = i
	}

	aggQ := make([]int, n)
	aggG := make([]int, n)
	qByName := map[string]map[string]int{}
	gByName := map[string]map[string]int{}
	for _, c := range counts {
		ds := c.Day.UTC().Format("2006-01-02")
		i, ok := idx[ds]
		if !ok {
			continue // outside the 30-day window (DailyCounts bounds this, be safe)
		}
		switch c.Kind {
		case "query":
			aggQ[i] += c.Count
			if c.Indexer != "" {
				m := qByName[c.Indexer]
				if m == nil {
					m = map[string]int{}
					qByName[c.Indexer] = m
				}
				m[ds] += c.Count
			}
		case "grab":
			aggG[i] += c.Count
			if c.Indexer != "" {
				m := gByName[c.Indexer]
				if m == nil {
					m = map[string]int{}
					gByName[c.Indexer] = m
				}
				m[ds] += c.Count
			}
		}
	}

	perName := func(m map[string]int) []dayBucketJSON {
		out := make([]dayBucketJSON, n)
		for i := 0; i < n; i++ {
			out[i] = dayBucketJSON{Date: label[i], Count: m[label[i]]}
		}
		return out
	}
	aggBuckets := func(agg []int) []dayBucketJSON {
		out := make([]dayBucketJSON, n)
		for i := 0; i < n; i++ {
			out[i] = dayBucketJSON{Date: label[i], Count: agg[i]}
		}
		return out
	}

	// by_indexer covers only named indexers with at least one query or grab
	// in the window (an empty indexer name — a failed build — is excluded).
	names := make(map[string]bool)
	for name := range qByName {
		names[name] = true
	}
	for name := range gByName {
		names[name] = true
	}
	byIndexer := make([]indexerSeriesJSON, 0, len(names))
	for name := range names {
		byIndexer = append(byIndexer, indexerSeriesJSON{
			Indexer: name,
			Queries: perName(qByName[name]),
			Grabs:   perName(gByName[name]),
		})
	}
	sort.Slice(byIndexer, func(i, j int) bool {
		return byIndexer[i].Indexer < byIndexer[j].Indexer
	})

	return statsSeriesJSON{
		Queries:   aggBuckets(aggQ),
		Grabs:     aggBuckets(aggG),
		ByIndexer: byIndexer,
	}
}

// IndexerStatsSummary handles GET /api/v1/indexers/stats/summary: the
// cross-indexer activity rollup (all-time totals plus the 30-day UTC series).
func (s *Server) IndexerStatsSummary(w http.ResponseWriter, r *http.Request) {
	d := s.indexerSearchDeps
	ctx := r.Context()
	enabled, err := d.Indexers.ListEnabled(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load indexers")
		return
	}
	totalQueries, totalGrabs, err := d.Activity.Totals(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load stats")
		return
	}
	now := time.Now().UTC()
	since := now.AddDate(0, 0, -(StatsWindowDays - 1)) // last 30 days incl. today
	counts, err := d.Activity.DailyCounts(ctx, since)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load stats")
		return
	}
	writeJSON(w, http.StatusOK, statsSummaryJSON{
		ActiveIndexers: len(enabled),
		ActiveApps:     0, // a unified-service Mediarr has no connected apps
		TotalQueries:   totalQueries,
		TotalGrabs:     totalGrabs,
		Series:         buildStatsSeries(now, counts),
	})
}
