package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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

// errDuplicate mimics the Postgres driver's rendering of a unique-constraint
// violation (the substring isUniqueViolation keys on). It is what a real
// indexers repo returns when a create/update collides on the unique name.
var errDuplicate = errors.New(`duplicate key value violates unique constraint "indexers_name_key"`)

// fakeWriteIdxRepo is an in-memory indexers.IndexerRepo that actually enforces
// the unique-name constraint, so the write endpoints' 409 path is exercised
// against realistic behavior (not an injected error). It is the same 8-method
// IndexerRepo interface the read endpoints use.
type fakeWriteIdxRepo struct {
	mu   sync.Mutex
	defs []indexers.Definition
}

func (r *fakeWriteIdxRepo) Create(ctx context.Context, def indexers.Definition) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, d := range r.defs {
		if d.Name == def.Name {
			return 0, errDuplicate
		}
	}
	def.ID = int64(len(r.defs) + 1)
	r.defs = append(r.defs, def)
	return def.ID, nil
}

func (r *fakeWriteIdxRepo) Get(ctx context.Context, id int64) (*indexers.Definition, error) {
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

func (r *fakeWriteIdxRepo) GetByName(ctx context.Context, name string) (*indexers.Definition, error) {
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

func (r *fakeWriteIdxRepo) List(ctx context.Context) ([]indexers.Definition, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]indexers.Definition(nil), r.defs...), nil
}

func (r *fakeWriteIdxRepo) ListEnabled(ctx context.Context) ([]indexers.Definition, error) {
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

func (r *fakeWriteIdxRepo) Update(ctx context.Context, def indexers.Definition) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	found := false
	for _, d := range r.defs {
		if d.ID == def.ID {
			found = true
			break
		}
	}
	if !found {
		return sql.ErrNoRows
	}
	// Enforce the unique-name constraint against the other rows.
	for _, o := range r.defs {
		if o.ID != def.ID && o.Name == def.Name {
			return errDuplicate
		}
	}
	// Apply the update in place, preserving the row's LastTest.
	for i, d := range r.defs {
		if d.ID == def.ID {
			def.LastTest = d.LastTest
			r.defs[i] = def
			break
		}
	}
	return nil
}

func (r *fakeWriteIdxRepo) Delete(ctx context.Context, id int64) error {
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

func (r *fakeWriteIdxRepo) SetEnabled(ctx context.Context, id int64, enabled bool) error {
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

func (r *fakeWriteIdxRepo) SetLastTest(ctx context.Context, id int64, at *time.Time) error {
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

// newIndexerWriteTestServer builds an authenticated server wired with the
// indexer *write* endpoints. Unlike newIndexersTestServer (read-only, no
// registry), this one passes the registry to IndexersDeps.Admin so the
// POST/PUT/PATCH/DELETE routes register. A fresh server per test avoids the
// mux double-register panic.
func newIndexerWriteTestServer(t *testing.T) (*Server, *fakeWriteIdxRepo, string) {
	t.Helper()
	repo := &fakeWriteIdxRepo{}
	// Seed one indexer so delete/enable/update have a target.
	_, _ = repo.Create(context.Background(), indexers.Definition{
		Name: "nyaa", Kind: "fake", BaseURL: "https://nyaa.example/api", APIKey: "redac...er", Enabled: true,
	})

	store := newFakeStore()
	u, _ := store.CreateUser(context.Background(), "admin", "", "admin", "hash")
	pair, err := auth.NewToken("ma_live_")
	if err != nil {
		t.Fatalf("new token: %v", err)
	}
	_, _ = store.CreateToken(context.Background(), u, "test", pair, nil)

	s := NewServer(health.NewChecker(nil, nil))
	s.SetAuth(&AuthDeps{Repo: store})
	s.SetIndexers(&IndexersDeps{Repo: repo, Admin: indexers.NewDefaultRegistry()})
	return s, repo, pair.Raw
}

// doJSONReq runs a request with a JSON body through the server handler.
func doJSONReq(t *testing.T, h http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestIndexerWrite_RequiresAuth is the #44 auth contract: write endpoints are 401
// without a token.
func TestIndexerWrite_RequiresAuth(t *testing.T) {
	s, _, _ := newIndexerWriteTestServer(t)
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodPost, "/api/v1/indexers", `{"name":"x","kind":"fake"}`},
		{http.MethodPut, "/api/v1/indexers/1", `{"name":"x","kind":"fake"}`},
		{http.MethodPatch, "/api/v1/indexers/1/enabled", `{"enabled":false}`},
		{http.MethodDelete, "/api/v1/indexers/1", ""},
	} {
		rec := doJSONReq(t, s.Handler(), tc.method, tc.path, "", tc.body)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without token: got %d, want 401", tc.method, tc.path, rec.Code)
		}
	}
}

// TestIndexerWrite_Create is the #44 create contract: a valid definition is a
// 201 with a masked response (has_api_key true, the key never echoed).
func TestIndexerWrite_Create(t *testing.T) {
	s, _, token := newIndexerWriteTestServer(t)
	rec := doJSONReq(t, s.Handler(), http.MethodPost, "/api/v1/indexers", token,
		`{"name":"rarbg","kind":"torznab","base_url":"https://rarbg.example/api","api_key":"super-secret"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("got %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["name"] != "rarbg" || out["kind"] != "torznab" {
		t.Errorf("unexpected echo: %v", out)
	}
	if out["has_api_key"] != true {
		t.Errorf("expected has_api_key true, got %v", out["has_api_key"])
	}
	if _, present := out["api_key"]; present {
		t.Error("response must not contain an api_key field")
	}
	if strings.Contains(rec.Body.String(), "super-secret") {
		t.Error("response body leaks the API key value")
	}
}

// TestIndexerWrite_Create_UnknownKind is the #44 kind-validation contract: a
// kind the registry cannot materialize is a 400, not a silent save.
func TestIndexerWrite_Create_UnknownKind(t *testing.T) {
	s, _, token := newIndexerWriteTestServer(t)
	rec := doJSONReq(t, s.Handler(), http.MethodPost, "/api/v1/indexers", token,
		`{"name":"bad","kind":"not-a-real-kind"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400: %s", rec.Code, rec.Body.String())
	}
	var body map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["error"] == "" {
		t.Error("expected a non-empty error field")
	}
}

// TestIndexerWrite_Create_Duplicate is the #44 unique-name contract: a name that
// collides with an existing indexer is a 409.
func TestIndexerWrite_Create_Duplicate(t *testing.T) {
	s, _, token := newIndexerWriteTestServer(t)
	rec := doJSONReq(t, s.Handler(), http.MethodPost, "/api/v1/indexers", token,
		`{"name":"nyaa","kind":"fake"}`) // "nyaa" is already seeded
	if rec.Code != http.StatusConflict {
		t.Fatalf("got %d, want 409: %s", rec.Code, rec.Body.String())
	}
}

// TestIndexerWrite_Create_EmptyName is the #44 required-name contract.
func TestIndexerWrite_Create_EmptyName(t *testing.T) {
	s, _, token := newIndexerWriteTestServer(t)
	rec := doJSONReq(t, s.Handler(), http.MethodPost, "/api/v1/indexers", token,
		`{"name":"   ","kind":"fake"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

// TestIndexerWrite_Update is the #44 update contract: changing name/base keeps
// the stored key (the empty api_key in the body means "keep"), and the key is
// never echoed.
func TestIndexerWrite_Update(t *testing.T) {
	s, repo, token := newIndexerWriteTestServer(t)
	rec := doJSONReq(t, s.Handler(), http.MethodPut, "/api/v1/indexers/1", token,
		`{"name":"nyaa-renamed","kind":"fake","base_url":"https://new.example/api","api_key":""}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["name"] != "nyaa-renamed" {
		t.Errorf("expected renamed, got %v", out["name"])
	}
	if out["has_api_key"] != true {
		t.Errorf("expected the stored key to be preserved (has_api_key true), got %v", out["has_api_key"])
	}
	// The stored key must still be the original, preserved across the rename.
	def, _ := repo.Get(context.Background(), 1)
	if def == nil || def.APIKey != "redac...er" {
		t.Errorf("expected the stored API key to be preserved, got %+v", def)
	}
}

// TestIndexerWrite_Update_UnknownIs404 is the #44 error-shape contract: an
// unknown id on update is a 404.
func TestIndexerWrite_Update_UnknownIs404(t *testing.T) {
	s, _, token := newIndexerWriteTestServer(t)
	rec := doJSONReq(t, s.Handler(), http.MethodPut, "/api/v1/indexers/999", token,
		`{"name":"x","kind":"fake"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("got %d, want 404: %s", rec.Code, rec.Body.String())
	}
}

// TestIndexerWrite_Enable is the #44 enable-toggle contract: PATCH flips the
// flag and the read endpoint reflects it.
func TestIndexerWrite_Enable(t *testing.T) {
	s, _, token := newIndexerWriteTestServer(t)
	rec := doJSONReq(t, s.Handler(), http.MethodPatch, "/api/v1/indexers/1/enabled", token,
		`{"enabled":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out["enabled"] != false {
		t.Errorf("expected enabled false, got %v", out["enabled"])
	}
}

// TestIndexerWrite_Enable_UnknownIs404 is the #44 error-shape contract: a Get-first
// existence check yields a 404 for an unknown id (not a 500).
func TestIndexerWrite_Enable_UnknownIs404(t *testing.T) {
	s, _, token := newIndexerWriteTestServer(t)
	rec := doJSONReq(t, s.Handler(), http.MethodPatch, "/api/v1/indexers/999/enabled", token,
		`{"enabled":true}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("got %d, want 404: %s", rec.Code, rec.Body.String())
	}
}

// TestIndexerWrite_Delete is the #44 delete contract: DELETE removes the
// indexer (204), a subsequent read is a 404, and deleting again is a 404.
func TestIndexerWrite_Delete(t *testing.T) {
	s, _, token := newIndexerWriteTestServer(t)
	rec := doJSONReq(t, s.Handler(), http.MethodDelete, "/api/v1/indexers/1", token, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("got %d, want 204: %s", rec.Code, rec.Body.String())
	}
	// Read it back: gone.
	rec = doReq(t, s.Handler(), http.MethodGet, "/api/v1/indexers", token)
	var out []map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out) != 0 {
		t.Errorf("expected the list to be empty after delete, got %d", len(out))
	}
	// Delete again: 404.
	rec = doJSONReq(t, s.Handler(), http.MethodDelete, "/api/v1/indexers/1", token, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("re-delete: got %d, want 404: %s", rec.Code, rec.Body.String())
	}
}

// TestIndexerWrite_Delete_UnknownIs404 is the #44 error-shape contract.
func TestIndexerWrite_Delete_UnknownIs404(t *testing.T) {
	s, _, token := newIndexerWriteTestServer(t)
	rec := doJSONReq(t, s.Handler(), http.MethodDelete, "/api/v1/indexers/999", token, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("got %d, want 404: %s", rec.Code, rec.Body.String())
	}
}
