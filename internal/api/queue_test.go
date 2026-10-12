package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/brandenk514/mediarr/internal/auth"
	"github.com/brandenk514/mediarr/internal/health"
	"github.com/brandenk514/mediarr/internal/paging"
	"github.com/brandenk514/mediarr/internal/queue"
)

// fakeQueueRepo is an in-memory queue.Repo for the API contract test: it
// returns a fixed set of items / history and records what the caller asked for.
type fakeQueueRepo struct {
	items     []queue.Item
	hist      []queue.HistoryEvent
	err       error
	gotKind   queue.Kind
	gotParent int64
	gotLimit  int
	sawList   bool
	sawHist   bool
}

func (f *fakeQueueRepo) List(ctx context.Context) ([]queue.Item, error) {
	f.sawList = true
	if f.err != nil {
		return nil, f.err
	}
	if f.items == nil {
		return []queue.Item{}, nil
	}
	return f.items, nil
}

func (f *fakeQueueRepo) History(ctx context.Context, kind queue.Kind, parentID int64, limit int) ([]queue.HistoryEvent, error) {
	f.sawHist = true
	f.gotKind, f.gotParent, f.gotLimit = kind, parentID, limit
	if f.err != nil {
		return nil, f.err
	}
	if f.hist == nil {
		return []queue.HistoryEvent{}, nil
	}
	return f.hist, nil
}

// newQueueTestServer builds an authenticated server with the queue endpoints
// wired to a fake repo.
func newQueueTestServer(t *testing.T) (*Server, *fakeQueueRepo, string) {
	t.Helper()

	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	items := []queue.Item{
		{Kind: queue.KindMovie, ParentID: 101, ParentName: "Dune", ReleaseTitle: "Dune.Part.Two.2160p.WEB.x265", Indexer: "nyaa", State: "downloading", Progress: 42, UpdatedAt: now},
		{Kind: queue.KindTV, ParentID: 201, ParentName: "Severance", ReleaseTitle: "Severance.S01E01.1080p", Indexer: "torrentle", State: "queued", Progress: 0, UpdatedAt: now},
		{Kind: queue.KindMusic, ParentID: 301, ParentName: "Radiohead", ReleaseTitle: "In.Rainbows.1994.flac", Indexer: "nzbkit", State: "complete", Progress: 100, UpdatedAt: now},
		{Kind: queue.KindBook, ParentID: 401, ParentName: "Tolkien", ReleaseTitle: "The.Hobbit.epub", Indexer: "zlib", State: "failed", Progress: 0, UpdatedAt: now},
		{Kind: queue.KindMovie, ParentID: 102, ParentName: "Inception", ReleaseTitle: "Inception.2010.1080p", Indexer: "nyaa", State: "queued", Progress: 0, UpdatedAt: now},
	}
	hist := []queue.HistoryEvent{
		{Event: "added_to_queue", Detail: "from nyaa", At: now.Add(-2 * time.Hour)},
		{Event: "downloading", Detail: "42%", At: now.Add(-1 * time.Hour)},
	}
	repo := &fakeQueueRepo{items: items, hist: hist}

	store := newFakeStore()
	u, _ := store.CreateUser(context.Background(), "admin", "", "admin", "hash")
	pair, err := auth.NewToken("ma_live_")
	if err != nil {
		t.Fatalf("new token: %v", err)
	}
	_, _ = store.CreateToken(context.Background(), u, "test", pair, nil)

	s := NewServer(health.NewChecker(nil, nil))
	s.SetAuth(&AuthDeps{Repo: store})
	s.SetQueue(&QueueDeps{Repo: repo})
	return s, repo, pair.Raw
}

// TestQueue_RequiresAuth is the #43 "auth required" contract: unauthenticated
// queue requests are 401.
func TestQueue_RequiresAuth(t *testing.T) {
	s, _, _ := newQueueTestServer(t)
	rec := doReq(t, s.Handler(), http.MethodGet, "/api/v1/queue", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token: got %d, want 401", rec.Code)
	}
	rec = doReq(t, s.Handler(), http.MethodGet, "/api/v1/queue/movie/1/history", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token (history): got %d, want 401", rec.Code)
	}
}

// TestQueue_ListEnvelope is the #43 "filterable + paginated full queue"
// contract: the endpoint returns the standard Page envelope {items,total,
// page,page_size} with the whole set when unfiltered.
func TestQueue_ListEnvelope(t *testing.T) {
	s, _, token := newQueueTestServer(t)
	rec := doReq(t, s.Handler(), http.MethodGet, "/api/v1/queue", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	var env paging.Page[queue.Item]
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v; body %s", err, rec.Body.String())
	}
	if env.Total != 5 || len(env.Items) != 5 {
		t.Fatalf("total/len = %d/%d, want 5/5", env.Total, len(env.Items))
	}
	if env.Page != 1 || env.PageSize != paging.DefaultPageSize {
		t.Fatalf("page/size = %d/%d, want 1/%d", env.Page, env.PageSize, paging.DefaultPageSize)
	}
}

// TestQueue_ListFilterAndPaging is the #43 "filtered server-side" contract:
// ?kind, ?state, ?q narrow the set, and ?page/?page_size page it.
func TestQueue_ListFilterAndPaging(t *testing.T) {
	s, _, token := newQueueTestServer(t)

	// Kind filter (case-insensitive) -> the two movies.
	rec := doReq(t, s.Handler(), http.MethodGet, "/api/v1/queue?kind=MOVIE", token)
	var env paging.Page[queue.Item]
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Total != 2 || len(env.Items) != 2 {
		t.Fatalf("kind=MOVIE total/len = %d/%d, want 2/2", env.Total, len(env.Items))
	}
	for _, it := range env.Items {
		if it.Kind != queue.KindMovie {
			t.Fatalf("kind filter leaked %s", it.Kind)
		}
	}

	// Kind + state -> one movie that is downloading.
	rec = doReq(t, s.Handler(), http.MethodGet, "/api/v1/queue?kind=movie&state=downloading", token)
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Total != 1 || env.Items[0].ReleaseTitle != "Dune.Part.Two.2160p.WEB.x265" {
		t.Fatalf("movie+downloading: total=%d first=%q", env.Total, env.Items[0].ReleaseTitle)
	}

	// Q matches a release substring.
	rec = doReq(t, s.Handler(), http.MethodGet, "/api/v1/queue?q=hobbit", token)
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Total != 1 || env.Items[0].ParentName != "Tolkien" {
		t.Fatalf("q=hobbit: total=%d first=%q", env.Total, env.Items[0].ParentName)
	}

	// Q matches a parent name.
	rec = doReq(t, s.Handler(), http.MethodGet, "/api/v1/queue?q=radiohead", token)
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Total != 1 || env.Items[0].Kind != queue.KindMusic {
		t.Fatalf("q=radiohead: total=%d first=%q", env.Total, env.Items[0].Kind)
	}

	// Pagination: page 2 of size 2 -> the 3rd item only.
	rec = doReq(t, s.Handler(), http.MethodGet, "/api/v1/queue?page=2&page_size=2", token)
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Total != 5 || len(env.Items) != 2 || env.Page != 2 || env.PageSize != 2 {
		t.Fatalf("page2 size2: total=%d len=%d page=%d size=%d", env.Total, len(env.Items), env.Page, env.PageSize)
	}

	// page_size clamps to MaxPageSize.
	rec = doReq(t, s.Handler(), http.MethodGet, "/api/v1/queue?page_size=9999", token)
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.PageSize != paging.MaxPageSize {
		t.Fatalf("page_size = %d, want clamped to %d", env.PageSize, paging.MaxPageSize)
	}
}

// TestQueue_History is the #43 "per-parent history drill-down" contract:
// the endpoint returns that parent's lifecycle events and passes the
// kind/parent through to the repo.
func TestQueue_History(t *testing.T) {
	s, repo, token := newQueueTestServer(t)
	rec := doReq(t, s.Handler(), http.MethodGet, "/api/v1/queue/movie/101/history", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	var events []queue.HistoryEvent
	if err := json.Unmarshal(rec.Body.Bytes(), &events); err != nil {
		t.Fatalf("unmarshal: %v; body %s", err, rec.Body.String())
	}
	if len(events) != 2 {
		t.Fatalf("events len = %d, want 2", len(events))
	}
	if !repo.sawHist {
		t.Fatal("repo.History was not called")
	}
	if repo.gotKind != queue.KindMovie || repo.gotParent != 101 {
		t.Fatalf("repo got kind=%s parent=%d, want movie/101", repo.gotKind, repo.gotParent)
	}
	if repo.gotLimit <= 0 {
		t.Fatalf("repo got limit %d, want >0", repo.gotLimit)
	}
}

// TestQueue_HistoryValidation is the #43 validation contract: an unknown kind
// or malformed/absent parent id is 400, not 500.
func TestQueue_HistoryValidation(t *testing.T) {
	s, _, token := newQueueTestServer(t)
	for _, path := range []string{
		"/api/v1/queue/anime/1/history",   // unknown kind
		"/api/v1/queue/movie/0/history",   // parent <= 0
		"/api/v1/queue/movie/abc/history", // non-numeric
	} {
		rec := doReq(t, s.Handler(), http.MethodGet, path, token)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: got %d, want 400; body %s", path, rec.Code, rec.Body.String())
		}
	}
}

// TestQueue_ListEmpty is the #43 "empty queue" contract: a queue with no items
// encodes as [] (not null) with total 0, so the UI renders an empty state.
func TestQueue_ListEmpty(t *testing.T) {
	store := newFakeStore()
	u, _ := store.CreateUser(context.Background(), "admin", "", "admin", "hash")
	pair, err := auth.NewToken("ma_live_")
	if err != nil {
		t.Fatalf("new token: %v", err)
	}
	_, _ = store.CreateToken(context.Background(), u, "test", pair, nil)

	s := NewServer(health.NewChecker(nil, nil))
	s.SetAuth(&AuthDeps{Repo: store})
	s.SetQueue(&QueueDeps{Repo: &fakeQueueRepo{}})

	rec := doReq(t, s.Handler(), http.MethodGet, "/api/v1/queue", pair.Raw)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", rec.Code)
	}
	var env paging.Page[queue.Item]
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Total != 0 || env.Items == nil {
		t.Fatalf("empty: total=%d items=%v, want 0/[]", env.Total, env.Items)
	}
}
