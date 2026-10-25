package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/brandenk514/mediarr/internal/auth"
	hfdom "github.com/brandenk514/mediarr/internal/domains/historyfeed"
	"github.com/brandenk514/mediarr/internal/health"
	hfservice "github.com/brandenk514/mediarr/internal/services/historyfeed"
)

// memHistoryRepo is an in-memory historyfeed.Repo for the endpoint contract
// tests.
type memHistoryRepo struct {
	items []hfdom.Item
	got   hfdom.Filter
}

func (m *memHistoryRepo) List(_ context.Context, f hfdom.Filter) ([]hfdom.Item, error) {
	m.got = f
	return f.Apply(m.items), nil
}

func newActivityTestServer(t *testing.T) (*Server, *memHistoryRepo, string) {
	t.Helper()
	repo := &memHistoryRepo{items: []hfdom.Item{
		{Kind: "movie", ParentID: 101, Name: "Dune", Event: "imported", Detail: "Dune.2160p.mkv", At: time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)},
		{Kind: "tv", ParentID: 201, Name: "Severance", Event: "search-requested", Detail: "S01E01", At: time.Date(2026, 10, 11, 11, 0, 0, 0, time.UTC)},
		{Kind: "tv", ParentID: 201, Name: "Severance", Event: "added", Detail: "", At: time.Date(2026, 10, 10, 11, 0, 0, 0, time.UTC)},
	}}
	store := newFakeStore()
	u, _ := store.CreateUser(context.Background(), "admin", "", "admin", "hash")
	pair, err := auth.NewToken("ma_live_")
	if err != nil {
		t.Fatalf("new token: %v", err)
	}
	_, _ = store.CreateToken(context.Background(), u, "test", pair, nil)

	s := NewServer(health.NewChecker(nil, nil))
	s.SetAuth(&AuthDeps{Repo: store})
	s.SetHistory(&HistoryDeps{Svc: hfservice.New(repo)})
	return s, repo, pair.Raw
}

func TestHistory_RequiresAuth(t *testing.T) {
	s, _, _ := newActivityTestServer(t)
	rec := doReq(t, s.Handler(), http.MethodGet, "/api/v1/history", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token: got %d, want 401", rec.Code)
	}
}

func TestHistory_List(t *testing.T) {
	s, _, token := newActivityTestServer(t)
	rec := doReq(t, s.Handler(), http.MethodGet, "/api/v1/history", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d; body %s", rec.Code, rec.Body.String())
	}
	var items []hfdom.Item
	if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("items = %d, want 3", len(items))
	}
	if items[0].Kind != "movie" || items[0].Name != "Dune" {
		t.Fatalf("first = %+v, want newest (movie/Dune)", items[0])
	}
}

func TestHistory_Filters(t *testing.T) {
	s, repo, token := newActivityTestServer(t)

	// Event filter is passed through (case-insensitive by the domain).
	rec := doReq(t, s.Handler(), http.MethodGet, "/api/v1/history?event=ADDED", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	var items []hfdom.Item
	_ = json.Unmarshal(rec.Body.Bytes(), &items)
	if len(items) != 1 || items[0].Event != "added" {
		t.Fatalf("event filter: got %v", items)
	}
	if repo.got.Event != "ADDED" {
		t.Fatalf("repo got event %q, want raw value passthrough", repo.got.Event)
	}

	// Since filter parses RFC 3339.
	rec = doReq(t, s.Handler(), http.MethodGet, "/api/v1/history?since=2026-10-11T00:00:00Z", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("since status: %d", rec.Code)
	}
	if repo.got.Since == nil {
		t.Fatal("since not parsed")
	}
	items = nil
	_ = json.Unmarshal(rec.Body.Bytes(), &items)
	if len(items) != 2 {
		t.Fatalf("since: got %d items, want 2", len(items))
	}

	// Bad since is a 400.
	rec = doReq(t, s.Handler(), http.MethodGet, "/api/v1/history?since=yesterday", token)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad since: got %d, want 400", rec.Code)
	}
	// Bad limit is a 400.
	rec = doReq(t, s.Handler(), http.MethodGet, "/api/v1/history?limit=0", token)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad limit: got %d, want 400", rec.Code)
	}
}
