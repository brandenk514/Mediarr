package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/brandenk514/mediarr/internal/auth"
	"github.com/brandenk514/mediarr/internal/downloads"
	"github.com/brandenk514/mediarr/internal/health"
	"github.com/brandenk514/mediarr/internal/indexers"
)

// fakeActivityRepo is an in-memory indexers.SearchActivityRepo for endpoint
// contract tests. It faithfully implements the filters and pagination the
// endpoints rely on.
type fakeActivityRepo struct {
	mu      sync.Mutex
	queries []indexers.QueryLog
	grabs   []indexers.GrabLog
	nextQ   int64
	nextG   int64
}

func (r *fakeActivityRepo) RecordQuery(_ context.Context, q indexers.QueryLog) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextQ++
	q.ID = r.nextQ
	r.queries = append(r.queries, q)
	return nil
}

func (r *fakeActivityRepo) RecordGrab(_ context.Context, g indexers.GrabLog) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextG++
	r.grabs = append(r.grabs, g)
	return nil
}

func (r *fakeActivityRepo) ListQueries(_ context.Context, f indexers.QueryFilter) ([]indexers.QueryLog, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var matched []indexers.QueryLog
	for _, q := range r.queries {
		if f.Indexer != "" && q.Indexer != f.Indexer {
			continue
		}
		if f.Term != "" && !strings.Contains(strings.ToLower(q.Term), strings.ToLower(f.Term)) {
			continue
		}
		matched = append(matched, q)
	}
	// Newest first (stable by id on ties).
	sort.Slice(matched, func(i, j int) bool {
		if matched[i].At.Equal(matched[j].At) {
			return matched[i].ID > matched[j].ID
		}
		return matched[i].At.After(matched[j].At)
	})
	total := len(matched)
	page, size := f.Page, f.PageSize
	if page < 1 {
		page = 1
	}
	if size <= 0 {
		size = 25
	}
	start := (page - 1) * size
	if start >= total {
		return []indexers.QueryLog{}, total, nil
	}
	end := start + size
	if end > total {
		end = total
	}
	return matched[start:end], total, nil
}

func (r *fakeActivityRepo) DeleteAllQueries(_ context.Context) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := len(r.queries)
	r.queries = nil
	return n, nil
}

func (r *fakeActivityRepo) Totals(_ context.Context) (int, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.queries), len(r.grabs), nil
}

func (r *fakeActivityRepo) DailyCounts(_ context.Context, since time.Time) ([]indexers.DailyCount, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	type k struct{ indexer, kind string }
	counts := map[k]map[string]int{}
	bump := func(indexer, kind string, at time.Time) {
		if at.Before(since) {
			return
		}
		m := counts[k{indexer, kind}]
		if m == nil {
			m = map[string]int{}
			counts[k{indexer, kind}] = m
		}
		m[at.UTC().Format("2006-01-02")]++
	}
	for _, q := range r.queries {
		bump(q.Indexer, "query", q.At)
	}
	for _, g := range r.grabs {
		bump(g.Indexer, "grab", g.At)
	}
	var out []indexers.DailyCount
	for kk, days := range counts {
		for day, c := range days {
			at, _ := time.ParseInLocation("2006-01-02", day, time.UTC)
			out = append(out, indexers.DailyCount{Indexer: kk.indexer, Kind: kk.kind, Day: at, Count: c})
		}
	}
	return out, nil
}

// testSearchRegistry builds a registry with a "good" scripted indexer (returns
// one known torrent release) and a "dead" kind that fails to build. The scripted
// kind lets the happy-path test assert the full result projection (the
// default-registry "fake" kind would build an empty pool).
func testSearchRegistry(t *testing.T) *indexers.Registry {
	t.Helper()
	reg := indexers.NewRegistry()
	reg.Register("good", func(def indexers.Definition) (indexers.Provider, error) {
		f := indexers.NewFakeIndexer(def.Name)
		f.SearchFn = func(_ indexers.SearchQuery) []indexers.SearchResult {
			return []indexers.SearchResult{{
				Title:     "Inception.2010.1080p.WEB.x264",
				Indexer:   def.Name,
				SizeBytes: 4_300_000_000,
				Info: indexers.ReleaseInfo{
					Protocol: indexers.ProtocolTorrent,
					URL:      "magnet:?xt=urn:btih:inception",
					Seeders:  42,
				},
			}}
		}
		return f, nil
	})
	reg.Register("dead", func(def indexers.Definition) (indexers.Provider, error) {
		return nil, errors.New("adapter boom")
	})
	return reg
}

// doJSON runs a request through the server handler with a JSON body (nil body
// → empty) and the given bearer token ("\" for no Authorization header).
func doJSON(t *testing.T, h http.Handler, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		rd = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// newAuthServer builds an authenticated server with the auth endpoints and one
// live token, ready for a test to wire its deps onto.
func newAuthServer(t *testing.T) (*Server, string) {
	t.Helper()
	store := newFakeStore()
	ctx := context.Background()
	u, _ := store.CreateUser(ctx, "admin", "", "admin", "hash")
	pair, err := auth.NewToken("ma_live_")
	if err != nil {
		t.Fatalf("new token: %v", err)
	}
	_, _ = store.CreateToken(ctx, u, "test", pair, nil)
	s := NewServer(health.NewChecker(nil, nil))
	s.SetAuth(&AuthDeps{Repo: store})
	return s, pair.Raw
}

// newIndexerSearchTestServer builds a server wired with the manual-search
// endpoints: two enabled indexers (one "good", one "dead") plus one disabled,
// the scripted registry, an in-memory activity store, and a mock download
// client (kind mock → accepts any protocol).
func newIndexerSearchTestServer(t *testing.T) (*Server, *fakeIdxRepo, *fakeActivityRepo, string) {
	t.Helper()
	s, token := newAuthServer(t)
	repo := newFakeIdxRepo()
	ctx := context.Background()
	_, _ = repo.Create(ctx, indexers.Definition{Name: "good-ix", Kind: "good", Enabled: true})
	_, _ = repo.Create(ctx, indexers.Definition{Name: "dead-ix", Kind: "dead", Enabled: true})
	_, _ = repo.Create(ctx, indexers.Definition{Name: "off-ix", Kind: "good", Enabled: false})
	mock, err := downloads.NewMockClient("mock", t.TempDir())
	if err != nil {
		t.Fatalf("mock client: %v", err)
	}
	activity := &fakeActivityRepo{}
	s.SetIndexerSearch(&IndexerSearchDeps{
		Indexers:   repo,
		Registry:   testSearchRegistry(t),
		Activity:   activity,
		Client:     mock,
		ClientKind: downloads.KindMock,
	})
	return s, repo, activity, token
}

// TestIndexerSearch_RequiresAuth: unauthenticated calls to all five routes 401.
func TestIndexerSearch_RequiresAuth(t *testing.T) {
	s, _, _, _ := newIndexerSearchTestServer(t)
	for _, tc := range []struct {
		method, path string
		body         any
	}{
		{http.MethodPost, "/api/v1/search", map[string]any{"query": "x"}},
		{http.MethodPost, "/api/v1/search/grab", map[string]any{"title": "t", "protocol": "torrent"}},
		{http.MethodGet, "/api/v1/search/history", nil},
		{http.MethodDelete, "/api/v1/search/history", nil},
		{http.MethodGet, "/api/v1/indexers/stats/summary", nil},
	} {
		rec := doJSON(t, s.Handler(), tc.method, tc.path, "", tc.body)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without token: got %d, want 401", tc.method, tc.path, rec.Code)
		}
	}
}

// TestManualSearch_EmptyQuery400: an empty/blank query is a 400.
func TestManualSearch_EmptyQuery400(t *testing.T) {
	s, _, _, token := newIndexerSearchTestServer(t)
	for _, q := range []string{"", "   "} {
		rec := doJSON(t, s.Handler(), http.MethodPost, "/api/v1/search", token, map[string]any{"query": q})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("query %q: got %d, want 400: %s", q, rec.Code, rec.Body.String())
		}
	}
}

// TestManualSearch_HappyPath: a search fans out and returns the release with
// the full projection; one history row is recorded per attempted indexer.
func TestManualSearch_HappyPath(t *testing.T) {
	s, _, activity, token := newIndexerSearchTestServer(t)
	// All enabled = good-ix (1 release) + dead-ix (build failure, 0 releases).
	rec := doJSON(t, s.Handler(), http.MethodPost, "/api/v1/search", token, map[string]any{"query": "Inception"})
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var out []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("body not a JSON array: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 release, got %d: %s", len(out), rec.Body.String())
	}
	r := out[0]
	if r["title"] != "Inception.2010.1080p.WEB.x264" {
		t.Errorf("title = %v", r["title"])
	}
	if r["indexer"] != "good-ix" {
		t.Errorf("indexer = %v", r["indexer"])
	}
	if r["size_bytes"] != float64(4_300_000_000) {
		t.Errorf("size_bytes = %v", r["size_bytes"])
	}
	if r["seeders"] != float64(42) {
		t.Errorf("seeders = %v", r["seeders"])
	}
	if r["protocol"] != indexers.ProtocolTorrent {
		t.Errorf("protocol = %v", r["protocol"])
	}
	if r["url"] != "magnet:?xt=urn:btih:inception" {
		t.Errorf("url = %v", r["url"])
	}
	if r["category"] != "" {
		t.Errorf("category = %v (fake reports none)", r["category"])
	}
	// One history row per attempted indexer (good + dead = 2).
	if n, _, _ := activity.ListQueries(context.Background(), indexers.QueryFilter{Page: 1, PageSize: 10}); len(n) != 2 {
		t.Fatalf("expected 2 history rows, got %d", len(n))
	}
}

// TestManualSearch_RecordsHistoryIncludingFailures: the failing indexer
// contributes a history row (ok=false) but no results.
func TestManualSearch_RecordsHistoryIncludingFailures(t *testing.T) {
	s, _, activity, token := newIndexerSearchTestServer(t)
	rec := doJSON(t, s.Handler(), http.MethodPost, "/api/v1/search", token, map[string]any{"query": "Inception"})
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", rec.Code, rec.Body.String())
	}
	rows, total, _ := activity.ListQueries(context.Background(), indexers.QueryFilter{Page: 1, PageSize: 10})
	if total != 2 {
		t.Fatalf("expected 2 history rows, got %d", total)
	}
	byName := map[string]indexers.QueryLog{}
	for _, row := range rows {
		byName[row.Indexer] = row
	}
	good := byName["good-ix"]
	if !good.OK {
		t.Errorf("good-ix history row should be ok=true")
	}
	dead := byName["dead-ix"]
	if dead.OK {
		t.Errorf("dead-ix history row should be ok=false (build failure)")
	}
	if dead.ElapsedMS < 0 {
		t.Errorf("dead-ix elapsed_ms should be >= 0, got %d", dead.ElapsedMS)
	}
	// The good row carries the term.
	if good.Term != "Inception" {
		t.Errorf("good-ix term = %q", good.Term)
	}
}

// TestManualSearch_UnknownIDFiltered: a requested id that is not an enabled
// indexer is filtered out (no error); the enabled ones still search.
func TestManualSearch_UnknownIDFiltered(t *testing.T) {
	s, _, activity, token := newIndexerSearchTestServer(t)
	// good-ix is id 1; 999 is unknown; off-ix (id 3) is disabled.
	rec := doJSON(t, s.Handler(), http.MethodPost, "/api/v1/search", token, map[string]any{
		"query":       "Inception",
		"indexer_ids": []int64{1, 999, 3},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", rec.Code, rec.Body.String())
	}
	// Only good-ix (id 1) is searched → 1 release, 1 history row.
	var out []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("body not an array: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 release, got %d", len(out))
	}
	if _, total, _ := activity.ListQueries(context.Background(), indexers.QueryFilter{Page: 1, PageSize: 10}); total != 1 {
		t.Fatalf("expected 1 history row (only the enabled, requested indexer), got %d", total)
	}
}

// TestManualSearch_NoEnabledIndexers: with nothing to search, a 200 empty array.
func TestManualSearch_NoEnabledIndexers(t *testing.T) {
	s, token := newAuthServer(t)
	repo := newFakeIdxRepo()
	// No enabled indexers at all.
	mock, _ := downloads.NewMockClient("mock", t.TempDir())
	s.SetIndexerSearch(&IndexerSearchDeps{
		Indexers: repo, Registry: testSearchRegistry(t), Activity: &fakeActivityRepo{},
		Client: mock, ClientKind: downloads.KindMock,
	})
	rec := doJSON(t, s.Handler(), http.MethodPost, "/api/v1/search", token, map[string]any{"query": "Inception"})
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var out []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("body not an array: %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("expected empty array, got %v", out)
	}
}

// TestGrab_MockHappyPath: a mock client accepts a torrent grab (200) and the
// grab is recorded.
func TestGrab_MockHappyPath(t *testing.T) {
	s, _, activity, token := newIndexerSearchTestServer(t)
	rec := doJSON(t, s.Handler(), http.MethodPost, "/api/v1/search/grab", token, map[string]any{
		"title": "Inception.2010.1080p.WEB.x264", "indexer": "good-ix",
		"protocol": indexers.ProtocolTorrent, "url": "magnet:?xt=urn:btih:inception",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var res map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("body not an object: %v", err)
	}
	if res["ok"] != true || res["indexer"] != "good-ix" || res["protocol"] != indexers.ProtocolTorrent {
		t.Errorf("unexpected grab result: %v", res)
	}
	if _, g, _ := activity.Totals(context.Background()); g != 1 {
		t.Fatalf("expected 1 recorded grab, got %d", g)
	}
}

// TestGrab_WrongProtocol409: a qBittorrent-configured client cannot handle an
// nzb release (409). A mock client is used with kind=qb so the routing logic
// is exercised without a real qBittorrent.
func TestGrab_WrongProtocol409(t *testing.T) {
	s, token := newAuthServer(t)
	repo := newFakeIdxRepo()
	mock, _ := downloads.NewMockClient("mock", t.TempDir())
	s.SetIndexerSearch(&IndexerSearchDeps{
		Indexers: repo, Registry: testSearchRegistry(t), Activity: &fakeActivityRepo{},
		Client: mock, ClientKind: downloads.KindQB,
	})
	rec := doJSON(t, s.Handler(), http.MethodPost, "/api/v1/search/grab", token, map[string]any{
		"title": "Some.NZB.Release", "indexer": "ix", "protocol": indexers.ProtocolNZB, "url": "https://x/nzb",
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("got %d, want 409: %s", rec.Code, rec.Body.String())
	}
}

// TestGrab_NoClient503: with no download client configured, a grab is a 503.
func TestGrab_NoClient503(t *testing.T) {
	s, token := newAuthServer(t)
	repo := newFakeIdxRepo()
	s.SetIndexerSearch(&IndexerSearchDeps{
		Indexers: repo, Registry: testSearchRegistry(t), Activity: &fakeActivityRepo{},
		Client: nil, ClientKind: downloads.KindMock,
	})
	rec := doJSON(t, s.Handler(), http.MethodPost, "/api/v1/search/grab", token, map[string]any{
		"title": "t", "indexer": "ix", "protocol": indexers.ProtocolTorrent, "url": "magnet:?x",
	})
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want 503: %s", rec.Code, rec.Body.String())
	}
}

// TestSearchHistory_List: a search records rows the history endpoint returns,
// newest first, with the indexer filter working.
func TestSearchHistory_List(t *testing.T) {
	s, _, _, token := newIndexerSearchTestServer(t)
	// Two searches so the history has multiple rows.
	for _, q := range []string{"Inception", "Dune"} {
		rec := doJSON(t, s.Handler(), http.MethodPost, "/api/v1/search", token, map[string]any{"query": q})
		if rec.Code != http.StatusOK {
			t.Fatalf("search %q: got %d", q, rec.Code)
		}
	}
	rec := doJSON(t, s.Handler(), http.MethodGet, "/api/v1/search/history", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var page struct {
		Items []map[string]any `json:"items"`
		Total int              `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("body not an object: %v", err)
	}
	// 2 searches × 2 attempted indexers = 4 rows.
	if page.Total != 4 {
		t.Fatalf("expected total 4, got %d", page.Total)
	}
	if len(page.Items) != 4 {
		t.Fatalf("expected 4 items, got %d", len(page.Items))
	}

	// The indexer filter narrows to one indexer's rows.
	rec2 := doJSON(t, s.Handler(), http.MethodGet, "/api/v1/search/history?indexer=good-ix", token, nil)
	var page2 struct {
		Items []map[string]any `json:"items"`
		Total int              `json:"total"`
	}
	if err := json.Unmarshal(rec2.Body.Bytes(), &page2); err != nil {
		t.Fatalf("filtered body: %v", err)
	}
	if page2.Total != 2 {
		t.Fatalf("indexer=good-ix: expected total 2, got %d", page2.Total)
	}
	for _, it := range page2.Items {
		if it["indexer"] != "good-ix" {
			t.Errorf("filter leaked indexer %v", it["indexer"])
		}
	}
}

// TestSearchHistory_Clear: DELETE clears all rows and reports the count.
func TestSearchHistory_Clear(t *testing.T) {
	s, _, activity, token := newIndexerSearchTestServer(t)
	rec := doJSON(t, s.Handler(), http.MethodPost, "/api/v1/search", token, map[string]any{"query": "Inception"})
	if rec.Code != http.StatusOK {
		t.Fatalf("search: got %d", rec.Code)
	}
	deletedRec := doJSON(t, s.Handler(), http.MethodDelete, "/api/v1/search/history", token, nil)
	if deletedRec.Code != http.StatusOK {
		t.Fatalf("delete: got %d: %s", deletedRec.Code, deletedRec.Body.String())
	}
	var cleared map[string]int
	if err := json.Unmarshal(deletedRec.Body.Bytes(), &cleared); err != nil {
		t.Fatalf("cleared body: %v", err)
	}
	if cleared["deleted"] != 2 {
		t.Fatalf("expected deleted 2, got %d", cleared["deleted"])
	}
	if _, g, _ := activity.Totals(context.Background()); g != 0 {
		t.Fatalf("queries should be cleared, got %d", g)
	}
}

// TestStatsSummary: the stats endpoint aggregates the totals and the 30-day
// series (zero-filled) with a per-indexer split.
func TestStatsSummary(t *testing.T) {
	s, _, activity, token := newIndexerSearchTestServer(t)
	now := time.Now().UTC()
	// Record 2 queries + 1 grab for good-ix in the current window.
	for i := 0; i < 2; i++ {
		_ = activity.RecordQuery(context.Background(), indexers.QueryLog{Indexer: "good-ix", Term: "x", OK: true, At: now})
	}
	_ = activity.RecordGrab(context.Background(), indexers.GrabLog{Indexer: "good-ix", Title: "x", Protocol: indexers.ProtocolTorrent, At: now})

	rec := doJSON(t, s.Handler(), http.MethodGet, "/api/v1/indexers/stats/summary", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var st struct {
		ActiveIndexers int `json:"active_indexers"`
		ActiveApps     int `json:"active_apps"`
		TotalQueries   int `json:"total_queries"`
		TotalGrabs     int `json:"total_grabs"`
		Series         struct {
			Queries   []dayBucketJSON `json:"queries"`
			Grabs     []dayBucketJSON `json:"grabs"`
			ByIndexer []struct {
				Indexer string `json:"indexer"`
				Queries []struct {
					Date  string `json:"date"`
					Count int    `json:"count"`
				} `json:"queries"`
				Grabs []struct {
					Date  string `json:"date"`
					Count int    `json:"count"`
				} `json:"grabs"`
			} `json:"by_indexer"`
		} `json:"series"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatalf("body not an object: %v", err)
	}
	if st.ActiveIndexers != 2 { // good-ix + dead-ix are enabled
		t.Errorf("active_indexers = %d, want 2", st.ActiveIndexers)
	}
	if st.ActiveApps != 0 {
		t.Errorf("active_apps = %d, want 0", st.ActiveApps)
	}
	if st.TotalQueries != 2 || st.TotalGrabs != 1 {
		t.Errorf("totals = (%d,%d), want (2,1)", st.TotalQueries, st.TotalGrabs)
	}
	if len(st.Series.Queries) != StatsWindowDays || len(st.Series.Grabs) != StatsWindowDays {
		t.Fatalf("series length: queries=%d grabs=%d, want %d each",
			len(st.Series.Queries), len(st.Series.Grabs), StatsWindowDays)
	}
	// Sum the buckets (rollover-proof): all queries land in one day, all grabs
	// in one day.
	sumQ := 0
	for _, b := range st.Series.Queries {
		sumQ += b.Count
	}
	sumG := 0
	for _, b := range st.Series.Grabs {
		sumG += b.Count
	}
	if sumQ != 2 || sumG != 1 {
		t.Errorf("series sums = (q:%d,g:%d), want (2,1)", sumQ, sumG)
	}
	// by_indexer has exactly good-ix (it has events; dead-ix has none).
	if len(st.Series.ByIndexer) != 1 {
		t.Fatalf("by_indexer length = %d, want 1: %s", len(st.Series.ByIndexer), rec.Body.String())
	}
	if st.Series.ByIndexer[0].Indexer != "good-ix" {
		t.Errorf("by_indexer[0].indexer = %q", st.Series.ByIndexer[0].Indexer)
	}
	bq, bg := 0, 0
	for _, b := range st.Series.ByIndexer[0].Queries {
		bq += b.Count
	}
	for _, b := range st.Series.ByIndexer[0].Grabs {
		bg += b.Count
	}
	if bq != 2 || bg != 1 {
		t.Errorf("by_indexer good-ix sums = (q:%d,g:%d), want (2,1)", bq, bg)
	}
}

// TestBuildStatsSeries is the pure unit test for the 30-day UTC bucketing math:
// zero-fills missing days, sums the aggregate across per-indexer rows, splits
// per indexer (sorted a→b), orders chronologically (oldest first, today last),
// and drops rows outside the window.
func TestBuildStatsSeries(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	ago := func(days int) time.Time { return now.AddDate(0, 0, -days) }
	counts := []indexers.DailyCount{
		{Indexer: "a", Kind: "query", Day: ago(0), Count: 1},
		{Indexer: "a", Kind: "query", Day: ago(2), Count: 2},
		{Indexer: "a", Kind: "grab", Day: ago(0), Count: 1},
		{Indexer: "b", Kind: "grab", Day: ago(5), Count: 3},
		{Indexer: "b", Kind: "query", Day: ago(5), Count: 1},
		{Indexer: "c", Kind: "query", Day: ago(40), Count: 7}, // outside window → dropped
	}
	got := buildStatsSeries(now, counts)

	if len(got.Queries) != StatsWindowDays || len(got.Grabs) != StatsWindowDays {
		t.Fatalf("bucket length: q=%d g=%d, want %d", len(got.Queries), len(got.Grabs), StatsWindowDays)
	}
	if got.Queries[0].Date != "2026-09-30" {
		t.Errorf("first bucket = %q, want 2026-09-30", got.Queries[0].Date)
	}
	if got.Queries[StatsWindowDays-1].Date != "2026-10-29" {
		t.Errorf("last bucket = %q, want today 2026-10-29", got.Queries[StatsWindowDays-1].Date)
	}

	sum := func(buckets []dayBucketJSON) int {
		s := 0
		for _, b := range buckets {
			s += b.Count
		}
		return s
	}
	byDate := func(buckets []dayBucketJSON, date string) int {
		for _, b := range buckets {
			if b.Date == date {
				return b.Count
			}
		}
		return 0
	}
	if q := sum(got.Queries); q != 4 {
		t.Errorf("aggregate queries = %d, want 4 (a=3 + b=1; 40-day-old row dropped)", q)
	}
	if g := sum(got.Grabs); g != 4 {
		t.Errorf("aggregate grabs = %d, want 4", g)
	}
	if q := byDate(got.Queries, "2026-10-29"); q != 1 {
		t.Errorf("queries today = %d, want 1", q)
	}
	if q := byDate(got.Queries, "2026-10-27"); q != 2 {
		t.Errorf("queries 2-days-ago = %d, want 2", q)
	}

	if len(got.ByIndexer) != 2 {
		t.Fatalf("by_indexer length = %d, want 2 (a, b; c is outside the window)", len(got.ByIndexer))
	}
	if got.ByIndexer[0].Indexer != "a" || got.ByIndexer[1].Indexer != "b" {
		t.Fatalf("by_indexer order = %q,%q; want a,b", got.ByIndexer[0].Indexer, got.ByIndexer[1].Indexer)
	}
	if q := sum(got.ByIndexer[0].Queries); q != 3 {
		t.Errorf("indexer a queries = %d, want 3", q)
	}
	if g := sum(got.ByIndexer[0].Grabs); g != 1 {
		t.Errorf("indexer a grabs = %d, want 1", g)
	}
	if q := sum(got.ByIndexer[1].Queries); q != 1 {
		t.Errorf("indexer b queries = %d, want 1", q)
	}
	if g := sum(got.ByIndexer[1].Grabs); g != 3 {
		t.Errorf("indexer b grabs = %d, want 3", g)
	}
}
