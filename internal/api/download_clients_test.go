package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/brandenk514/mediarr/internal/auth"
	"github.com/brandenk514/mediarr/internal/downloadclients"
	"github.com/brandenk514/mediarr/internal/health"
)

// fakeDCRepo is an in-memory downloadclients.Repo for the PR5 plural
// download-client endpoint tests. List returns display order (position
// ascending, then id); unknown ids come back as sql.ErrNoRows, matching the
// Postgres repository contract the handlers map to 404.
type fakeDCRepo struct {
	seq   int64
	byID  map[int64]downloadclients.Record
	order []int64
}

func newFakeDCRepo() *fakeDCRepo {
	return &fakeDCRepo{byID: map[int64]downloadclients.Record{}}
}

func (f *fakeDCRepo) Create(_ context.Context, r downloadclients.Record) (int64, time.Time, error) {
	f.seq++
	r.ID = f.seq
	now := time.Now().UTC()
	r.CreatedAt, r.UpdatedAt = now, now
	f.byID[r.ID] = r
	f.order = append(f.order, r.ID)
	return r.ID, r.CreatedAt, nil
}

func (f *fakeDCRepo) Get(_ context.Context, id int64) (*downloadclients.Record, error) {
	r, ok := f.byID[id]
	if !ok {
		return nil, sql.ErrNoRows
	}
	return &r, nil
}

func (f *fakeDCRepo) List(_ context.Context) ([]downloadclients.Record, error) {
	out := make([]downloadclients.Record, 0, len(f.order))
	for _, id := range f.order {
		out = append(out, f.byID[id])
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Position != out[j].Position {
			return out[i].Position < out[j].Position
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (f *fakeDCRepo) GetByName(_ context.Context, name string) (*downloadclients.Record, error) {
	for _, id := range f.order {
		if f.byID[id].Name == name {
			r := f.byID[id]
			return &r, nil
		}
	}
	return nil, sql.ErrNoRows
}

func (f *fakeDCRepo) Update(_ context.Context, r downloadclients.Record) error {
	if _, ok := f.byID[r.ID]; !ok {
		return sql.ErrNoRows
	}
	r.UpdatedAt = time.Now().UTC()
	f.byID[r.ID] = r
	return nil
}

func (f *fakeDCRepo) Delete(_ context.Context, id int64) error {
	if _, ok := f.byID[id]; !ok {
		return sql.ErrNoRows
	}
	delete(f.byID, id)
	for i, v := range f.order {
		if v == id {
			f.order = append(f.order[:i], f.order[i+1:]...)
			break
		}
	}
	return nil
}

// newDownloadClientsTestServer builds an authenticated server wired with the
// PR5 plural download-client endpoints over an in-memory repo.
func newDownloadClientsTestServer(t *testing.T, repo *fakeDCRepo) (*Server, string) {
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
	s.SetDownloadClients(&DownloadClientsDeps{Repo: repo})
	return s, pair.Raw
}

func dcDo(t *testing.T, s *Server, token, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

// TestDownloadClients_RequiresAuth pins the auth wall on every PR5 route.
func TestDownloadClients_RequiresAuth(t *testing.T) {
	s, _ := newDownloadClientsTestServer(t, newFakeDCRepo())
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/settings/download-clients"},
		{http.MethodPost, "/api/v1/settings/download-clients"},
		{http.MethodPut, "/api/v1/settings/download-clients/1"},
		{http.MethodDelete, "/api/v1/settings/download-clients/1"},
		{http.MethodPost, "/api/v1/settings/download-clients/1/test"},
		{http.MethodPost, "/api/v1/settings/download-clients/test-all"},
	} {
		rec := dcDo(t, s, "", tc.method, tc.path, "")
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: got %d, want 401", tc.method, tc.path, rec.Code)
		}
	}
}

// TestDownloadClients_NotWired pins the 501 when the deps are absent.
func TestDownloadClients_NotWired(t *testing.T) {
	store := newFakeStore()
	u, _ := store.CreateUser(context.Background(), "admin", "", "admin", "hash")
	pair, _ := auth.NewToken("ma_live_")
	_, _ = store.CreateToken(context.Background(), u, "test", pair, nil)

	s := NewServer(health.NewChecker(nil, nil))
	s.SetAuth(&AuthDeps{Repo: store})
	s.SetDownloadClients(nil) // wired but nil deps -> 501

	rec := dcDo(t, s, pair.Raw, http.MethodGet, "/api/v1/settings/download-clients", "")
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("unwired GET: got %d, want 501", rec.Code)
	}
}

// TestDownloadClients_ListMasksKeys pins the PR5 masked read: api_key is
// always empty and api_key_set reports presence — the raw key never appears.
func TestDownloadClients_ListMasksKeys(t *testing.T) {
	repo := newFakeDCRepo()
	_, _, _ = repo.Create(context.Background(), downloadclients.Record{
		Name: "qBittorrent", Kind: downloadclients.KindQB, Enabled: true,
		Position: 1, BaseURL: "http://qb:8080", Username: "[REDACTED]",
		APIKey: "***",
	})
	_, _, _ = repo.Create(context.Background(), downloadclients.Record{
		Name: "SABnzbd", Kind: downloadclients.KindSAB, Enabled: true,
		Position: 2, BaseURL: "http://sab:1234",
	})
	s, token := newDownloadClientsTestServer(t, repo)

	rec := dcDo(t, s, token, http.MethodGet, "/api/v1/settings/download-clients", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET list: got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "top-secret-key") {
		t.Fatalf("raw API key leaked in the list response: %s", rec.Body.String())
	}
	var out []downloadClientJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("list length %d, want 2", len(out))
	}
	if out[0].APIKey != "" {
		t.Errorf("qb api_key must be masked, got %q", out[0].APIKey)
	}
	if !out[0].APIKeySet {
		t.Error("qb api_key_set should be true")
	}
	if out[1].APIKeySet {
		t.Error("sab api_key_set should be false (no key stored)")
	}
	if out[0].BaseURL != "http://qb:8080" || out[0].Kind != "qbittorrent" {
		t.Errorf("qb fields not carried: %+v", out[0])
	}
}

// TestDownloadClients_Create_Validation pins the 400 contract: a name is
// required and the kind must be one of the three supported kinds.
func TestDownloadClients_Create_Validation(t *testing.T) {
	repo := newFakeDCRepo()
	s, token := newDownloadClientsTestServer(t, repo)

	rec := dcDo(t, s, token, http.MethodPost, "/api/v1/settings/download-clients",
		`{"name":"","kind":"qbittorrent"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("empty name: got %d, want 400", rec.Code)
	}
	rec = dcDo(t, s, token, http.MethodPost, "/api/v1/settings/download-clients",
		`{"name":"aria","kind":"aria2"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("bad kind: got %d, want 400", rec.Code)
	}
	rec = dcDo(t, s, token, http.MethodPost, "/api/v1/settings/download-clients",
		`{"name":"x","kind":"QBITTORRENT"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("uppercase kind: got %d, want 400", rec.Code)
	}
}

// TestDownloadClients_Create_Defaults pins the create defaults: enabled
// defaults to true and an omitted position appends after the last client.
func TestDownloadClients_Create_Defaults(t *testing.T) {
	repo := newFakeDCRepo()
	s, token := newDownloadClientsTestServer(t, repo)

	rec := dcDo(t, s, token, http.MethodPost, "/api/v1/settings/download-clients",
		`{"name":"qBittorrent","kind":"qbittorrent","base_url":"http://qb:8080"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: got %d, want 201 (%s)", rec.Code, rec.Body.String())
	}
	var first downloadClientJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &first); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if !first.Enabled {
		t.Error("enabled should default to true")
	}
	if first.Position != 1 {
		t.Errorf("first position %d, want 1", first.Position)
	}

	rec = dcDo(t, s, token, http.MethodPost, "/api/v1/settings/download-clients",
		`{"name":"SABnzbd","kind":"sabnzbd","base_url":"http://sab:1234"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create 2: got %d, want 201", rec.Code)
	}
	var second downloadClientJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &second); err != nil {
		t.Fatalf("decode create 2: %v", err)
	}
	if second.Position != 2 {
		t.Errorf("second position %d, want 2 (append)", second.Position)
	}
}

// TestDownloadClients_Update_KeepsStoredKey pins the masked-edit round-trip:
// a PUT with api_key "" keeps the stored key; a non-empty api_key replaces
// it; a bad kind is a 400.
func TestDownloadClients_Update_KeepsStoredKey(t *testing.T) {
	repo := newFakeDCRepo()
	id, _, _ := repo.Create(context.Background(), downloadclients.Record{
		Name: "qBittorrent", Kind: downloadclients.KindQB, Enabled: true,
		Position: 1, BaseURL: "http://qb:8080", APIKey: "***",
	})
	origKey := ""
	if cur, _ := repo.Get(context.Background(), id); cur != nil {
		origKey = cur.APIKey
	}
	s, token := newDownloadClientsTestServer(t, repo)

	// Empty api_key keeps the stored key.
	rec := dcDo(t, s, token, http.MethodPut, "/api/v1/settings/download-clients/1",
		`{"api_key":"","name":"qBittorrent"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update: got %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	var out downloadClientJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode update: %v", err)
	}
	if out.APIKey != "" || !out.APIKeySet {
		t.Errorf("masked round-trip failed: api_key=%q api_key_set=%v", out.APIKey, out.APIKeySet)
	}
	if got, _ := repo.Get(context.Background(), id); got.APIKey != origKey {
		t.Errorf("stored key changed to %q (want %q)", got.APIKey, origKey)
	}

	// A new key replaces it.
	rec = dcDo(t, s, token, http.MethodPut, "/api/v1/settings/download-clients/1",
		`{"api_key":"new-key"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update key: got %d", rec.Code)
	}
	if got, _ := repo.Get(context.Background(), id); got.APIKey != "new-key" {
		t.Errorf("stored key not replaced, got %q", got.APIKey)
	}

	// Bad kind is a 400; empty name is a 400.
	rec = dcDo(t, s, token, http.MethodPut, "/api/v1/settings/download-clients/1", `{"kind":"aria2"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("bad kind update: got %d, want 400", rec.Code)
	}
	rec = dcDo(t, s, token, http.MethodPut, "/api/v1/settings/download-clients/1", `{"name":""}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("empty name update: got %d, want 400", rec.Code)
	}

	// Unknown id is a 404.
	rec = dcDo(t, s, token, http.MethodPut, "/api/v1/settings/download-clients/99", `{}`)
	if rec.Code != http.StatusNotFound {
		t.Errorf("update unknown: got %d, want 404", rec.Code)
	}
}

// TestDownloadClients_Delete pins the 204 delete and the 404 afterwards.
func TestDownloadClients_Delete(t *testing.T) {
	repo := newFakeDCRepo()
	id, _, _ := repo.Create(context.Background(), downloadclients.Record{
		Name: "qBittorrent", Kind: downloadclients.KindQB, Enabled: true,
		Position: 1, BaseURL: "http://qb:8080",
	})
	s, token := newDownloadClientsTestServer(t, repo)

	rec := dcDo(t, s, token, http.MethodDelete,
		"/api/v1/settings/download-clients/"+itoa(int(id)), "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete: got %d, want 204", rec.Code)
	}
	rec = dcDo(t, s, token, http.MethodGet, "/api/v1/settings/download-clients", "")
	var out []downloadClientJSON
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out) != 0 {
		t.Errorf("list after delete has %d clients, want 0", len(out))
	}
	// Subsequent per-id calls are 404.
	for _, tc := range []struct{ method, path string }{
		{http.MethodPut, "/api/v1/settings/download-clients/1"},
		{http.MethodDelete, "/api/v1/settings/download-clients/1"},
		{http.MethodPost, "/api/v1/settings/download-clients/1/test"},
	} {
		rec = dcDo(t, s, token, tc.method, tc.path, `{}`)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s %s after delete: got %d, want 404", tc.method, tc.path, rec.Code)
		}
	}
}

// newFakeClientServer stands up a per-kind stub for the /test probes and
// counts hits. qb expects GET /api/v2/app/version; sab expects GET /api with
// the apikey query param; transmission expects POST /transmission/rpc.
func newFakeClientServer(t *testing.T, kind downloadclients.Kind, fail bool) (*httptest.Server, *int, *string) {
	t.Helper()
	var hits int
	var cred string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		switch kind {
		case downloadclients.KindQB:
			if r.URL.Path != "/api/v2/app/version" {
				t.Errorf("qb probe path %q", r.URL.Path)
			}
			cred = r.Header.Get("X-Api-Key")
			if fail {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			_, _ = w.Write([]byte(`"R1.5.0"`))
		case downloadclients.KindSAB:
			if r.URL.Path != "/api" || r.URL.Query().Get("mode") != "version" {
				t.Errorf("sab probe %q?%q", r.URL.Path, r.URL.RawQuery)
			}
			cred = r.URL.Query().Get("apikey")
			if fail {
				_, _ = w.Write([]byte(`{"error":"invalid API key"}`))
				return
			}
			_, _ = w.Write([]byte(`{"version":"4.7.0"}`))
		case downloadclients.KindTransmission:
			if r.URL.Path != "/transmission/rpc" {
				t.Errorf("transmission probe path %q", r.URL.Path)
			}
			if fail {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`{"result":{"session-id":"stub-sid"}}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &hits, &cred
}

// TestDownloadClients_Test_PerID pins the per-kind probes: ok true on a
// 200, ok false (still 200) on a config gap / unreachable / rejected
// endpoint, and 404 for an unknown id.
func TestDownloadClients_Test_PerID(t *testing.T) {
	repo := newFakeDCRepo()
	qbSrv, _, qbCred := newFakeClientServer(t, downloadclients.KindQB, false)
	rec, _, _ := repo.Create(context.Background(), downloadclients.Record{
		Name: "qBittorrent", Kind: downloadclients.KindQB, Enabled: true,
		Position: 1, BaseURL: qbSrv.URL, APIKey: "***",
	})
	s, token := newDownloadClientsTestServer(t, repo)

	out := func(r *httptest.ResponseRecorder) *downloadClientTestResult {
		t.Helper()
		var res downloadClientTestResult
		if err := json.Unmarshal(r.Body.Bytes(), &res); err != nil {
			t.Fatalf("decode test result: %v (%s)", err, r.Body.String())
		}
		return &res
	}

	// ok: qBittorrent version endpoint answers.
	r := dcDo(t, s, token, http.MethodPost,
		"/api/v1/settings/download-clients/"+itoa(int(rec))+"/test", "")
	if r.Code != http.StatusOK {
		t.Fatalf("qb test: got %d, want 200", r.Code)
	}
	if res := out(r); !res.OK {
		t.Errorf("qb test should be ok: %+v", res)
	}
	// The probe must send the stored key verbatim.
	stored, _ := repo.Get(context.Background(), rec)
	if stored == nil || *qbCred != stored.APIKey {
		t.Errorf("probe did not send the stored key: sent %q stored %q", *qbCred, stored.APIKey)
	}

	// ok=false: a qBittorrent without a key never even probes.
	id2, _, _ := repo.Create(context.Background(), downloadclients.Record{
		Name: "nokey", Kind: downloadclients.KindQB, Enabled: true,
		Position: 2, BaseURL: qbSrv.URL,
	})
	r = dcDo(t, s, token, http.MethodPost,
		"/api/v1/settings/download-clients/"+itoa(int(id2))+"/test", "")
	if r.Code != http.StatusOK {
		t.Fatalf("no-key test: got %d, want 200", r.Code)
	}
	if res := out(r); res.OK || !strings.Contains(res.Message, "API key") {
		t.Errorf("no-key qb test: %+v", res)
	}

	// ok=false: the endpoint rejects the credentials.
	reject, _, _ := newFakeClientServer(t, downloadclients.KindQB, true)
	id3, _, _ := repo.Create(context.Background(), downloadclients.Record{
		Name: "rejected", Kind: downloadclients.KindQB, Enabled: true,
		Position: 3, BaseURL: reject.URL, APIKey: "***",
	})
	r = dcDo(t, s, token, http.MethodPost,
		"/api/v1/settings/download-clients/"+itoa(int(id3))+"/test", "")
	if res := out(r); res.OK || !strings.Contains(res.Message, "HTTP 403") {
		t.Errorf("rejected qb test: %+v", res)
	}

	// ok=false: an unreachable endpoint.
	id4, _, _ := repo.Create(context.Background(), downloadclients.Record{
		Name: "down", Kind: downloadclients.KindQB, Enabled: true,
		Position: 4, BaseURL: "http://127.0.0.1:1", APIKey: "***",
	})
	r = dcDo(t, s, token, http.MethodPost,
		"/api/v1/settings/download-clients/"+itoa(int(id4))+"/test", "")
	if res := out(r); res.OK || !strings.Contains(res.Message, "unreachable") {
		t.Errorf("unreachable qb test: %+v", res)
	}

	// 404: unknown id.
	r = dcDo(t, s, token, http.MethodPost,
		"/api/v1/settings/download-clients/99/test", "")
	if r.Code != http.StatusNotFound {
		t.Errorf("unknown id test: got %d, want 404", r.Code)
	}
}

// TestDownloadClients_Test_SAB pins the SABnzbd probe: a version answer with
// the stored key is ok; an "error" body is ok=false.
func TestDownloadClients_Test_SAB(t *testing.T) {
	repo := newFakeDCRepo()
	srv, _, _ := newFakeClientServer(t, downloadclients.KindSAB, false)
	id, _, _ := repo.Create(context.Background(), downloadclients.Record{
		Name: "SABnzbd", Kind: downloadclients.KindSAB, Enabled: true,
		Position: 1, BaseURL: srv.URL, APIKey: "sab-key",
	})
	s, token := newDownloadClientsTestServer(t, repo)

	rec := dcDo(t, s, token, http.MethodPost,
		"/api/v1/settings/download-clients/"+itoa(int(id))+"/test", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("sab test: got %d", rec.Code)
	}
	var res downloadClientTestResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !res.OK {
		t.Errorf("sab test should be ok: %+v", res)
	}
}

// TestDownloadClients_Test_Transmission pins the transmission RPC probe.
func TestDownloadClients_Test_Transmission(t *testing.T) {
	repo := newFakeDCRepo()
	srv, _, _ := newFakeClientServer(t, downloadclients.KindTransmission, false)
	id, _, _ := repo.Create(context.Background(), downloadclients.Record{
		Name: "transmission", Kind: downloadclients.KindTransmission, Enabled: true,
		Position: 1, BaseURL: srv.URL,
	})
	s, token := newDownloadClientsTestServer(t, repo)

	rec := dcDo(t, s, token, http.MethodPost,
		"/api/v1/settings/download-clients/"+itoa(int(id))+"/test", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("transmission test: got %d", rec.Code)
	}
	var res downloadClientTestResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !res.OK {
		t.Errorf("transmission test should be ok: %+v", res)
	}
}

// TestDownloadClients_TestAll pins test-all: one entry per saved client, in
// list order, with per-client outcomes.
func TestDownloadClients_TestAll(t *testing.T) {
	repo := newFakeDCRepo()
	qbSrv, _, _ := newFakeClientServer(t, downloadclients.KindQB, false)
	idOK, _, _ := repo.Create(context.Background(), downloadclients.Record{
		Name: "qBittorrent", Kind: downloadclients.KindQB, Enabled: true,
		Position: 1, BaseURL: qbSrv.URL, APIKey: "***",
	})
	idDown, _, _ := repo.Create(context.Background(), downloadclients.Record{
		Name: "down", Kind: downloadclients.KindSAB, Enabled: true,
		Position: 2, BaseURL: "http://127.0.0.1:1", APIKey: "sab-key",
	})
	s, token := newDownloadClientsTestServer(t, repo)

	rec := dcDo(t, s, token, http.MethodPost, "/api/v1/settings/download-clients/test-all", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("test-all: got %d, want 200", rec.Code)
	}
	var out []struct {
		ID      int64  `json:"id"`
		Name    string `json:"name"`
		OK      bool   `json:"ok"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode test-all: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("test-all results %d, want 2", len(out))
	}
	if out[0].ID != idOK || !out[0].OK {
		t.Errorf("first result: %+v", out[0])
	}
	if out[1].ID != idDown || out[1].OK {
		t.Errorf("second result should be the unreachable client: %+v", out[1])
	}
	if !strings.Contains(out[1].Message, "unreachable") {
		t.Errorf("second message: %q", out[1].Message)
	}
}
