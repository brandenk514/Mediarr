package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/brandenk514/mediarr/internal/auth"
	blkdom "github.com/brandenk514/mediarr/internal/domains/blocklist"
	"github.com/brandenk514/mediarr/internal/health"
	blkservice "github.com/brandenk514/mediarr/internal/services/blocklist"
)

type memBlocklistRepo struct {
	entries []blkdom.Entry
	next    int64
}

func (m *memBlocklistRepo) List(_ context.Context, f blkdom.Filter) ([]blkdom.Entry, error) {
	return f.Apply(m.entries), nil
}
func (m *memBlocklistRepo) Add(_ context.Context, e blkdom.Entry) (int64, error) {
	m.next++
	e.ID = m.next
	m.entries = append(m.entries, e)
	return e.ID, nil
}
func (m *memBlocklistRepo) Unblock(_ context.Context, id int64) (bool, error) {
	for i, e := range m.entries {
		if e.ID == id {
			m.entries = append(m.entries[:i], m.entries[i+1:]...)
			return true, nil
		}
	}
	return false, nil
}

func newBlocklistTestServer(t *testing.T) (*Server, *memBlocklistRepo, string) {
	t.Helper()
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	repo := &memBlocklistRepo{
		next: 10,
		entries: []blkdom.Entry{
			{ID: 1, Kind: blkdom.KindTV, ParentID: 201, ParentName: "Severance", ReleaseTitle: "Severance.S01E01.720p", Indexer: "nyaa", Reason: "low quality", CreatedAt: now},
			{ID: 2, Kind: blkdom.KindMovie, ParentID: 101, ParentName: "Dune", ReleaseTitle: "Dune.2160p.WEB", Indexer: "torrentle", Reason: "", CreatedAt: now},
		},
	}
	store := newFakeStore()
	u, _ := store.CreateUser(context.Background(), "admin", "", "admin", "hash")
	pair, err := auth.NewToken("ma_live_")
	if err != nil {
		t.Fatalf("new token: %v", err)
	}
	_, _ = store.CreateToken(context.Background(), u, "test", pair, nil)

	s := NewServer(health.NewChecker(nil, nil))
	s.SetAuth(&AuthDeps{Repo: store})
	s.SetBlocklist(&BlocklistDeps{Svc: blkservice.New(repo)})
	return s, repo, pair.Raw
}

func TestBlocklist_RequiresAuth(t *testing.T) {
	s, _, _ := newBlocklistTestServer(t)
	rec := doReq(t, s.Handler(), http.MethodGet, "/api/v1/blocklist", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token: got %d, want 401", rec.Code)
	}
}

func TestBlocklist_List(t *testing.T) {
	s, _, token := newBlocklistTestServer(t)
	rec := doReq(t, s.Handler(), http.MethodGet, "/api/v1/blocklist", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d", rec.Code)
	}
	var items []blkdom.Entry
	if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
	// Filter by kind.
	rec = doReq(t, s.Handler(), http.MethodGet, "/api/v1/blocklist?kind=movie", token)
	items = nil
	_ = json.Unmarshal(rec.Body.Bytes(), &items)
	if len(items) != 1 || items[0].Kind != blkdom.KindMovie {
		t.Fatalf("kind filter: got %v", items)
	}
	// Filter by parent.
	rec = doReq(t, s.Handler(), http.MethodGet, "/api/v1/blocklist?kind=tv&parent_id=201", token)
	items = nil
	_ = json.Unmarshal(rec.Body.Bytes(), &items)
	if len(items) != 1 || items[0].ReleaseTitle != "Severance.S01E01.720p" {
		t.Fatalf("parent filter: got %v", items)
	}
	// Unknown kind is a 400.
	rec = doReq(t, s.Handler(), http.MethodGet, "/api/v1/blocklist?kind=cartoon", token)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad kind: got %d, want 400", rec.Code)
	}
}

func TestBlocklist_AddAndUnblock(t *testing.T) {
	s, repo, token := newBlocklistTestServer(t)

	// Add a new entry.
	body := `{"kind":"tv","parent_id":301,"release_title":"The Wire S01E01","indexer":"zlib","reason":"repack"}`
	rec := doJSONReq(t, s.Handler(), http.MethodPost, "/api/v1/blocklist", token, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("add: got %d; body %s", rec.Code, rec.Body.String())
	}
	if len(repo.entries) != 3 {
		t.Fatalf("entries = %d, want 3", len(repo.entries))
	}
	newID := repo.entries[len(repo.entries)-1].ID

	// Add with a bad kind is a 400.
	rec = doJSONReq(t, s.Handler(), http.MethodPost, "/api/v1/blocklist", token, `{"kind":"nope","parent_id":1,"release_title":"x"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad kind add: got %d, want 400", rec.Code)
	}
	// Add with an empty title is a 400.
	rec = doJSONReq(t, s.Handler(), http.MethodPost, "/api/v1/blocklist", token, `{"kind":"tv","parent_id":1,"release_title":"  "}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty title add: got %d, want 400", rec.Code)
	}

	// Unblock the new entry.
	rec = doReq(t, s.Handler(), http.MethodDelete, "/api/v1/blocklist/"+strconv.FormatInt(newID, 10), token)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("unblock: got %d; body %s", rec.Code, rec.Body.String())
	}
	if len(repo.entries) != 2 {
		t.Fatalf("after unblock: entries = %d, want 2", len(repo.entries))
	}
	// Unblock a missing id is a 404.
	rec = doReq(t, s.Handler(), http.MethodDelete, "/api/v1/blocklist/9999", token)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unblock missing: got %d, want 404", rec.Code)
	}
}
