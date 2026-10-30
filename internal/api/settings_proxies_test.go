package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/brandenk514/mediarr/internal/auth"
	"github.com/brandenk514/mediarr/internal/health"
	"github.com/brandenk514/mediarr/internal/indexers"
)

// fakeProxyRepo is an in-memory indexers.ProxyRepo for the indexer-proxy
// endpoint tests. Get returns sql.ErrNoRows for unknown ids, the same
// convention as the Postgres repo.
type fakeProxyRepo struct {
	seq   int64
	byID  map[int64]indexers.Proxy
	order []int64
}

func newFakeProxyRepo() *fakeProxyRepo {
	return &fakeProxyRepo{byID: map[int64]indexers.Proxy{}}
}

func (f *fakeProxyRepo) Create(_ context.Context, p indexers.Proxy) (int64, time.Time, error) {
	f.seq++
	p.ID = f.seq
	p.CreatedAt = time.Now().UTC()
	f.byID[p.ID] = p
	f.order = append(f.order, p.ID)
	return p.ID, p.CreatedAt, nil
}

func (f *fakeProxyRepo) Get(_ context.Context, id int64) (*indexers.Proxy, error) {
	p, ok := f.byID[id]
	if !ok {
		return nil, sql.ErrNoRows
	}
	out := p
	return &out, nil
}

func (f *fakeProxyRepo) List(_ context.Context) ([]indexers.Proxy, error) {
	out := make([]indexers.Proxy, 0, len(f.order))
	for _, id := range f.order {
		out = append(out, f.byID[id])
	}
	return out, nil
}

func (f *fakeProxyRepo) Update(_ context.Context, p indexers.Proxy) error {
	if _, ok := f.byID[p.ID]; !ok {
		return sql.ErrNoRows
	}
	f.byID[p.ID] = p
	return nil
}

func (f *fakeProxyRepo) Delete(_ context.Context, id int64) error {
	delete(f.byID, id)
	for i, v := range f.order {
		if v == id {
			f.order = append(f.order[:i], f.order[i+1:]...)
			break
		}
	}
	return nil
}

// newIndexerProxiesTestServer builds an authenticated server wired with the
// PR4 indexer-proxy endpoints over an in-memory proxy repo.
func newIndexerProxiesTestServer(t *testing.T, repo *fakeProxyRepo) (*Server, string) {
	t.Helper()
	store := newFakeStore()
	u, _ := store.CreateUser(context.Background(), "admin", "", "admin", "hash")
	pair, err := auth.NewToken("ma_live_")
	if err != nil {
		t.Fatalf("new token: %v", err)
	}
	_, _ = store.CreateToken(context.Background(), u, "test", pair, nil)

	s := NewServer(health.NewChecker(nil, nil))
	s.SetAuth(&AuthDeps{Repo: store})
	s.SetIndexerProxies(&IndexerProxiesDeps{Repo: repo})
	return s, pair.Raw
}

// TestIndexerProxies_RequiresAuth is the PR4 auth contract: every
// indexer-proxy route is 401 without a token.
func TestIndexerProxies_RequiresAuth(t *testing.T) {
	s, _ := newIndexerProxiesTestServer(t, newFakeProxyRepo())
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/settings/proxies", ""},
		{http.MethodPost, "/api/v1/settings/proxies", `{"name":"fs","url":"http://fs:8191"}`},
		{http.MethodPut, "/api/v1/settings/proxies/1", `{"name":"fs","url":"http://fs:8191"}`},
		{http.MethodDelete, "/api/v1/settings/proxies/1", ""},
		{http.MethodPost, "/api/v1/settings/proxies/1/test", ""},
	} {
		var rec *httptest.ResponseRecorder
		if tc.body == "" {
			rec = doReq(t, s.Handler(), tc.method, tc.path, "")
		} else {
			rec = doJSONReq(t, s.Handler(), tc.method, tc.path, "", tc.body)
		}
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without token: got %d, want 401", tc.method, tc.path, rec.Code)
		}
	}
}

// TestIndexerProxies_NotWired is the nil-deps contract: 501.
func TestIndexerProxies_NotWired(t *testing.T) {
	store := newFakeStore()
	u, _ := store.CreateUser(context.Background(), "admin", "", "admin", "hash")
	pair, _ := auth.NewToken("ma_live_")
	_, _ = store.CreateToken(context.Background(), u, "test", pair, nil)
	s := NewServer(health.NewChecker(nil, nil))
	s.SetAuth(&AuthDeps{Repo: store})
	s.SetIndexerProxies(nil) // wired but nil deps -> 501
	rec := doReq(t, s.Handler(), http.MethodGet, "/api/v1/settings/proxies", pair.Raw)
	if rec.Code != http.StatusNotImplemented {
		t.Errorf("got %d, want 501: %s", rec.Code, rec.Body.String())
	}
}

// TestIndexerProxies_ListMasksSecret is the PR4 masking contract: password
// is always "" and password_set reports presence; tags is never null.
func TestIndexerProxies_ListMasksSecret(t *testing.T) {
	repo := newFakeProxyRepo()
	s, token := newIndexerProxiesTestServer(t, repo)

	rec := doJSONReq(t, s.Handler(), http.MethodPost, "/api/v1/settings/proxies", token,
		`{"name":"flare-1","url":"http://fs1:8191","username":"u","password":"pw123","tags":["private","usenet"]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create got %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var created indexerProxyJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Password != "" || !created.PasswordSet {
		t.Errorf("create response must mask password: %+v", created)
	}
	if len(created.Tags) != 2 || created.Tags[0] != "private" {
		t.Errorf("create response tags wrong: %+v", created)
	}

	// A passwordless proxy with omitted tags lists with empty tags.
	doJSONReq(t, s.Handler(), http.MethodPost, "/api/v1/settings/proxies", token,
		`{"name":"flare-open","url":"http://fs2:8191"}`)

	rec = doReq(t, s.Handler(), http.MethodGet, "/api/v1/settings/proxies", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("list got %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var list []indexerProxyJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("list len = %d, want 2: %s", len(list), rec.Body.String())
	}
	if list[0].Name != "flare-1" || list[0].Password != "" || !list[0].PasswordSet {
		t.Errorf("row 0 wrong: %+v", list[0])
	}
	if list[1].Name != "flare-open" || list[1].PasswordSet {
		t.Errorf("row 1 wrong: %+v", list[1])
	}
	if list[1].Tags == nil || len(list[1].Tags) != 0 {
		t.Errorf("omitted tags must be an empty list, not null: %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "pw123") {
		t.Errorf("password must never be echoed: %s", rec.Body.String())
	}
}

// TestIndexerProxies_CreateValidation is the PR4 400 contract: name and url
// are required.
func TestIndexerProxies_CreateValidation(t *testing.T) {
	s, token := newIndexerProxiesTestServer(t, newFakeProxyRepo())

	rec := doJSONReq(t, s.Handler(), http.MethodPost, "/api/v1/settings/proxies", token,
		`{"url":"http://fs:8191"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing name got %d, want 400: %s", rec.Code, rec.Body.String())
	}

	rec = doJSONReq(t, s.Handler(), http.MethodPost, "/api/v1/settings/proxies", token,
		`{"name":"fs"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing url got %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

// TestIndexerProxies_UpdateKeepsStoredPassword is the masked-edit round
// trip: an empty password on PUT keeps the stored password; an omitted tags
// array keeps the stored tags.
func TestIndexerProxies_UpdateKeepsStoredPassword(t *testing.T) {
	repo := newFakeProxyRepo()
	s, token := newIndexerProxiesTestServer(t, repo)

	rec := doJSONReq(t, s.Handler(), http.MethodPost, "/api/v1/settings/proxies", token,
		`{"name":"flare-1","url":"http://fs1:8191","username":"u","password":"pw123","tags":["private"]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create got %d, want 201: %s", rec.Code, rec.Body.String())
	}

	// Change url + username, send an empty password and omit tags (what the
	// UI sends after a masked read).
	rec = doJSONReq(t, s.Handler(), http.MethodPut, "/api/v1/settings/proxies/1", token,
		`{"name":"flare-1","url":"http://fs1:9000","username":"u2","password":""}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update got %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var updated indexerProxyJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.URL != "http://fs1:9000" || updated.Username != "u2" {
		t.Errorf("update fields wrong: %+v", updated)
	}
	if updated.Password != "" || !updated.PasswordSet {
		t.Errorf("update response must still mask + report password_set: %+v", updated)
	}
	if len(updated.Tags) != 1 || updated.Tags[0] != "private" {
		t.Errorf("omitted tags must keep the stored tags: %+v", updated)
	}
	if stored := repo.byID[1]; stored.Password != "pw123" {
		t.Errorf("stored password not preserved on masked edit: %q", stored.Password)
	}

	// An explicit empty tags array clears the stored tags.
	doJSONReq(t, s.Handler(), http.MethodPut, "/api/v1/settings/proxies/1", token,
		`{"name":"flare-1","url":"http://fs1:9000","username":"u2","password":"","tags":[]}`)
	if stored := repo.byID[1]; len(stored.Tags) != 0 {
		t.Errorf("explicit empty tags must clear the stored tags: %v", stored.Tags)
	}

	// A non-empty password replaces the stored one.
	doJSONReq(t, s.Handler(), http.MethodPut, "/api/v1/settings/proxies/1", token,
		`{"name":"flare-1","url":"http://fs1:9000","username":"u2","password":"newpw","tags":["x"]}`)
	if stored := repo.byID[1]; stored.Password != "newpw" {
		t.Errorf("stored password not replaced: %q", stored.Password)
	}
}

// TestIndexerProxies_Update404 is the unknown-id contract on PUT/DELETE.
func TestIndexerProxies_Update404(t *testing.T) {
	s, token := newIndexerProxiesTestServer(t, newFakeProxyRepo())
	rec := doJSONReq(t, s.Handler(), http.MethodPut, "/api/v1/settings/proxies/99", token,
		`{"name":"x","url":"http://x"}`)
	if rec.Code != http.StatusNotFound {
		t.Errorf("update got %d, want 404: %s", rec.Code, rec.Body.String())
	}
	rec = doReq(t, s.Handler(), http.MethodDelete, "/api/v1/settings/proxies/99", token)
	if rec.Code != http.StatusNotFound {
		t.Errorf("delete got %d, want 404: %s", rec.Code, rec.Body.String())
	}
}

// TestIndexerProxies_Delete is the 204-then-404 delete contract.
func TestIndexerProxies_Delete(t *testing.T) {
	repo := newFakeProxyRepo()
	s, token := newIndexerProxiesTestServer(t, repo)
	rec := doJSONReq(t, s.Handler(), http.MethodPost, "/api/v1/settings/proxies", token,
		`{"name":"fs","url":"http://fs:8191"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create got %d, want 201: %s", rec.Code, rec.Body.String())
	}

	rec = doReq(t, s.Handler(), http.MethodDelete, "/api/v1/settings/proxies/1", token)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete got %d, want 204: %s", rec.Code, rec.Body.String())
	}
	if len(repo.byID) != 0 {
		t.Errorf("repo still has %d rows after delete", len(repo.byID))
	}

	rec = doReq(t, s.Handler(), http.MethodDelete, "/api/v1/settings/proxies/1", token)
	if rec.Code != http.StatusNotFound {
		t.Errorf("delete unknown got %d, want 404: %s", rec.Code, rec.Body.String())
	}
}

// TestIndexerProxies_TestEndpointReachable is the PR4 probe contract: a
// reachable FlareSolverr root (JSON with a version field) is 200 with
// {ok: true, version}, and basic auth is applied when stored.
func TestIndexerProxies_TestEndpointReachable(t *testing.T) {
	repo := newFakeProxyRepo()
	s, token := newIndexerProxiesTestServer(t, repo)

	var gotAuth string
	instance := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]string{"version": "3.0.81", "state": "UP"})
	}))
	defer instance.Close()

	rec := doJSONReq(t, s.Handler(), http.MethodPost, "/api/v1/settings/proxies", token,
		`{"name":"flare-1","url":"`+instance.URL+`","username":"u","password":"pw"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create got %d, want 201: %s", rec.Code, rec.Body.String())
	}

	rec = doReq(t, s.Handler(), http.MethodPost, "/api/v1/settings/proxies/1/test", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("test got %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["ok"] != true || out["version"] != "3.0.81" {
		t.Errorf("want {ok:true,version:3.0.81}, got %s", rec.Body.String())
	}
	if gotAuth == "" {
		t.Error("stored basic-auth credentials must be applied to the probe")
	}
}

// TestIndexerProxies_TestEndpointFailures covers 502 (unreachable or
// non-2xx instance) and 404 (unknown id).
func TestIndexerProxies_TestEndpointFailures(t *testing.T) {
	repo := newFakeProxyRepo()
	s, token := newIndexerProxiesTestServer(t, repo)

	// 502: unreachable instance.
	repo.byID[1] = indexers.Proxy{ID: 1, Name: "down", URL: "http://127.0.0.1:1"}
	rec := doReq(t, s.Handler(), http.MethodPost, "/api/v1/settings/proxies/1/test", token)
	if rec.Code != http.StatusBadGateway {
		t.Errorf("unreachable instance got %d, want 502: %s", rec.Code, rec.Body.String())
	}

	// 502: non-2xx instance.
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer failing.Close()
	repo.byID[2] = indexers.Proxy{ID: 2, Name: "500er", URL: failing.URL}
	rec = doReq(t, s.Handler(), http.MethodPost, "/api/v1/settings/proxies/2/test", token)
	if rec.Code != http.StatusBadGateway {
		t.Errorf("non-2xx instance got %d, want 502: %s", rec.Code, rec.Body.String())
	}

	// 404: unknown id.
	rec = doReq(t, s.Handler(), http.MethodPost, "/api/v1/settings/proxies/99/test", token)
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown id got %d, want 404: %s", rec.Code, rec.Body.String())
	}
}
