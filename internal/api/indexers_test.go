package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/brandenk514/mediarr/internal/auth"
	"github.com/brandenk514/mediarr/internal/health"
	"github.com/brandenk514/mediarr/internal/indexers"
)

// fakeIdxRepo is an in-memory indexers.IndexerRepo for API contract tests.
type fakeIdxRepo struct {
	mu    sync.Mutex
	defs  []indexers.Definition
	errOn map[string]error
}

func newFakeIdxRepo() *fakeIdxRepo { return &fakeIdxRepo{errOn: map[string]error{}} }

func (r *fakeIdxRepo) Create(ctx context.Context, def indexers.Definition) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	def.ID = int64(len(r.defs) + 1)
	r.defs = append(r.defs, def)
	return def.ID, nil
}
func (r *fakeIdxRepo) Get(ctx context.Context, id int64) (*indexers.Definition, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, d := range r.defs {
		if d.ID == id {
			cp := d
			return &cp, nil
		}
	}
	return nil, sql.ErrNoRows
}
func (r *fakeIdxRepo) GetByName(ctx context.Context, name string) (*indexers.Definition, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, d := range r.defs {
		if d.Name == name {
			cp := d
			return &cp, nil
		}
	}
	return nil, sql.ErrNoRows
}
func (r *fakeIdxRepo) List(ctx context.Context) ([]indexers.Definition, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]indexers.Definition(nil), r.defs...), nil
}
func (r *fakeIdxRepo) ListEnabled(ctx context.Context) ([]indexers.Definition, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []indexers.Definition
	for _, d := range r.defs {
		if d.Enabled {
			out = append(out, d)
		}
	}
	return out, nil
}
func (r *fakeIdxRepo) Update(ctx context.Context, def indexers.Definition) error { return nil }
func (r *fakeIdxRepo) Delete(ctx context.Context, id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, d := range r.defs {
		if d.ID == id {
			r.defs = append(r.defs[:i], r.defs[i+1:]...)
			return nil
		}
	}
	return sql.ErrNoRows
}
func (r *fakeIdxRepo) SetEnabled(ctx context.Context, id int64, enabled bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, d := range r.defs {
		if d.ID == id {
			r.defs[i].Enabled = enabled
			return nil
		}
	}
	return sql.ErrNoRows
}
func (r *fakeIdxRepo) SetLastTest(ctx context.Context, id int64, at *time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, d := range r.defs {
		if d.ID == id {
			r.defs[i].LastTest = at
			return nil
		}
	}
	return sql.ErrNoRows
}

// newIndexersTestServer builds an authenticated server wired with the indexer
// endpoints: a fake user store with one live token, a fake indexer repo with
// two definitions (one enabled with a key, one disabled without), the shared
// tracker, and a worker over the fake registry (kind "fake" always healthy).
func newIndexersTestServer(t *testing.T) (*Server, *fakeIdxRepo, string, indexers.HealthTracker) {
	t.Helper()
	repo := newFakeIdxRepo()
	_, _ = repo.Create(context.Background(), indexers.Definition{
		Name: "nyaa", Kind: "fake", BaseURL: "https://nyaa.example/api", APIKey: "redacted-placeholder", Enabled: true,
	})
	_, _ = repo.Create(context.Background(), indexers.Definition{
		Name: "disabled-one", Kind: "fake", BaseURL: "https://off.example/api", Enabled: false,
	})

	tracker := indexers.NewHealthTracker()
	deps := indexers.AdapterDeps{Health: tracker}
	worker := indexers.NewHealthWorker(repo, indexers.NewDefaultRegistry(), deps, 0, nil)

	store := newFakeStore()
	u, _ := store.CreateUser(context.Background(), "admin", "", "admin", "hash")
	pair, err := auth.NewToken("ma_live_")
	if err != nil {
		t.Fatalf("new token: %v", err)
	}
	_, _ = store.CreateToken(context.Background(), u, "test", pair, nil)

	s := NewServer(health.NewChecker(nil, nil))
	s.SetAuth(&AuthDeps{Repo: store})
	s.SetIndexers(&IndexersDeps{Repo: repo, Worker: worker, Tracker: tracker})
	return s, repo, pair.Raw, tracker
}

// doReq runs a request through the server handler with the given bearer token
// ("" means no Authorization header).
func doReq(t *testing.T, h http.Handler, method, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestIndexers_ListRequiresAuth is the #35 "auth required" contract: unauthenticated
// requests to the indexer endpoints are 401.
func TestIndexers_ListRequiresAuth(t *testing.T) {
	s, _, _, _ := newIndexersTestServer(t)
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/indexers"},
		{http.MethodPost, "/api/v1/indexers/1/test"},
		{http.MethodGet, "/api/v1/indexers/1/stats"},
	} {
		rec := doReq(t, s.Handler(), tc.method, tc.path, "")
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without token: got %d, want 401", tc.method, tc.path, rec.Code)
		}
	}
}

// TestIndexers_List_MasksKey is the #35 "no secrets in responses" contract:
// the list includes a has_api_key boolean and never the key itself.
func TestIndexers_List_MasksKey(t *testing.T) {
	s, _, token, _ := newIndexersTestServer(t)
	rec := doReq(t, s.Handler(), http.MethodGet, "/api/v1/indexers", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var out []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("body is not a JSON array: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 indexers, got %d", len(out))
	}
	byName := map[string]map[string]any{}
	for _, m := range out {
		byName[m["name"].(string)] = m
	}
	nyaa := byName["nyaa"]
	if nyaa["has_api_key"] != true {
		t.Errorf("nyaa: expected has_api_key true, got %v", nyaa["has_api_key"])
	}
	if off := byName["disabled-one"]; off["has_api_key"] != false {
		t.Errorf("disabled-one: expected has_api_key false, got %v", off["has_api_key"])
	}
	// The key must not appear anywhere in the response, under any field name.
	for _, field := range []string{"api_key", "apikey", "key", "secret", "token"} {
		if _, present := nyaa[field]; present {
			t.Errorf("response must not contain field %q", field)
		}
	}
	if strings.Contains(rec.Body.String(), "redacted-placeholder") {
		t.Error("response body leaks the API key value")
	}
}

// TestIndexers_Test_OnDemand is the #35 on-demand test contract: a 200 with the
// probe outcome. A healthy fake indexer reports ok=true.
func TestIndexers_Test_OnDemand(t *testing.T) {
	s, _, token, tracker := newIndexersTestServer(t)
	rec := doReq(t, s.Handler(), http.MethodPost, "/api/v1/indexers/1/test", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var res map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res["ok"] != true {
		t.Errorf("expected ok=true, got %v", res["ok"])
	}
	if res["name"] != "nyaa" {
		t.Errorf("expected name nyaa, got %v", res["name"])
	}
	// The probe must be recorded in the shared tracker (live stats).
	if s := tracker.Stats("nyaa"); s.Calls != 1 {
		t.Errorf("expected the on-demand probe recorded, got %+v", s)
	}
}

// TestIndexers_Test_UnknownIs404 is the #35 error-shape contract: an unknown
// indexer id is a 404 with an error field.
func TestIndexers_Test_UnknownIs404(t *testing.T) {
	s, _, token, _ := newIndexersTestServer(t)
	rec := doReq(t, s.Handler(), http.MethodPost, "/api/v1/indexers/999/test", token)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("got %d, want 404: %s", rec.Code, rec.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["error"] == "" {
		t.Error("expected a non-empty error field")
	}
}

// TestIndexers_Stats_Shape is the #35 stats contract: the per-indexer rollup
// shape, including a configured-but-never-called indexer (zero values, 200).
func TestIndexers_Stats_Shape(t *testing.T) {
	s, _, token, tracker := newIndexersTestServer(t)
	// Seed one success so the stats are non-zero.
	tracker.Record("nyaa", true, 12*time.Millisecond, "")

	rec := doReq(t, s.Handler(), http.MethodGet, "/api/v1/indexers/1/stats", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var st map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st["name"] != "nyaa" || st["calls"] != float64(1) || st["successes"] != float64(1) {
		t.Errorf("unexpected stats: %v", st)
	}
	if st["avg_latency_ns"] != float64(int64(12*time.Millisecond)) {
		t.Errorf("expected avg_latency_ns 12000000, got %v", st["avg_latency_ns"])
	}

	// A configured-but-uncalled indexer returns zero values, not an error.
	rec2 := doReq(t, s.Handler(), http.MethodGet, "/api/v1/indexers/2/stats", token)
	if rec2.Code != http.StatusOK {
		t.Fatalf("uncalled indexer stats: got %d, want 200", rec2.Code)
	}
	var zero map[string]any
	_ = json.Unmarshal(rec2.Body.Bytes(), &zero)
	if zero["calls"] != float64(0) {
		t.Errorf("expected zero calls for uncalled indexer, got %v", zero["calls"])
	}
}
