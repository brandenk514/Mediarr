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
	"github.com/brandenk514/mediarr/internal/notify"
)

// fakeSinkRepo is an in-memory notify.SinkRepo for the notification-sink
// endpoint tests. Get returns sql.ErrNoRows for unknown ids, the same
// convention as the Postgres repo.
type fakeSinkRepo struct {
	seq   int64
	byID  map[int64]notify.SinkRecord
	order []int64
}

func newFakeSinkRepo() *fakeSinkRepo {
	return &fakeSinkRepo{byID: map[int64]notify.SinkRecord{}}
}

func (f *fakeSinkRepo) Create(_ context.Context, rec notify.SinkRecord) (int64, time.Time, error) {
	f.seq++
	rec.ID = f.seq
	rec.CreatedAt = time.Now().UTC()
	f.byID[rec.ID] = rec
	f.order = append(f.order, rec.ID)
	return rec.ID, rec.CreatedAt, nil
}

func (f *fakeSinkRepo) Get(_ context.Context, id int64) (*notify.SinkRecord, error) {
	rec, ok := f.byID[id]
	if !ok {
		return nil, sql.ErrNoRows
	}
	out := rec
	return &out, nil
}

func (f *fakeSinkRepo) List(_ context.Context) ([]notify.SinkRecord, error) {
	out := make([]notify.SinkRecord, 0, len(f.order))
	for _, id := range f.order {
		out = append(out, f.byID[id])
	}
	return out, nil
}

func (f *fakeSinkRepo) Update(_ context.Context, rec notify.SinkRecord) error {
	if _, ok := f.byID[rec.ID]; !ok {
		return sql.ErrNoRows
	}
	f.byID[rec.ID] = rec
	return nil
}

func (f *fakeSinkRepo) Delete(_ context.Context, id int64) error {
	delete(f.byID, id)
	for i, v := range f.order {
		if v == id {
			f.order = append(f.order[:i], f.order[i+1:]...)
			break
		}
	}
	return nil
}

// newNotificationsTestServer builds an authenticated server wired with the
// PR4 notification-sink endpoints over an in-memory sink repo.
func newNotificationsTestServer(t *testing.T, repo *fakeSinkRepo) (*Server, string) {
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
	s.SetNotifications(&NotificationsDeps{Repo: repo})
	return s, pair.Raw
}

// TestNotifications_RequiresAuth is the PR4 auth contract: every
// notification-sink route is 401 without a token.
func TestNotifications_RequiresAuth(t *testing.T) {
	s, _ := newNotificationsTestServer(t, newFakeSinkRepo())
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/settings/notifications", ""},
		{http.MethodPost, "/api/v1/settings/notifications", `{"name":"n","kind":"webhook","url":"http://x"}`},
		{http.MethodPut, "/api/v1/settings/notifications/1", `{"name":"n","kind":"webhook","url":"http://x"}`},
		{http.MethodDelete, "/api/v1/settings/notifications/1", ""},
		{http.MethodPost, "/api/v1/settings/notifications/1/test", ""},
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

// TestNotifications_NotWired is the nil-deps contract: 501.
func TestNotifications_NotWired(t *testing.T) {
	store := newFakeStore()
	u, _ := store.CreateUser(context.Background(), "admin", "", "admin", "hash")
	pair, _ := auth.NewToken("ma_live_")
	_, _ = store.CreateToken(context.Background(), u, "test", pair, nil)
	s := NewServer(health.NewChecker(nil, nil))
	s.SetAuth(&AuthDeps{Repo: store})
	s.SetNotifications(nil) // wired but nil deps -> 501
	rec := doReq(t, s.Handler(), http.MethodGet, "/api/v1/settings/notifications", pair.Raw)
	if rec.Code != http.StatusNotImplemented {
		t.Errorf("got %d, want 501: %s", rec.Code, rec.Body.String())
	}
}

// TestNotifications_ListMasksSecret is the PR4 masking contract: token is
// always "" and token_set reports presence; rows are in creation order.
func TestNotifications_ListMasksSecret(t *testing.T) {
	repo := newFakeSinkRepo()
	s, token := newNotificationsTestServer(t, repo)

	rec := doJSONReq(t, s.Handler(), http.MethodPost, "/api/v1/settings/notifications", token,
		`{"name":"ntfy home","kind":"webhook","url":"http://ntfy.example:2000/mediarr","token":"s3cret"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create got %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var created notificationSinkJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Token != "" || !created.TokenSet {
		t.Errorf("create response must mask token: %+v", created)
	}
	if created.Kind != "webhook" || !created.Enabled {
		t.Errorf("create response fields wrong (enabled omitted = true): %+v", created)
	}

	// A tokenless sink lists with token_set false.
	doJSONReq(t, s.Handler(), http.MethodPost, "/api/v1/settings/notifications", token,
		`{"name":"plex","kind":"plex","url":"http://plex.example:32400"}`)

	rec = doReq(t, s.Handler(), http.MethodGet, "/api/v1/settings/notifications", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("list got %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var list []notificationSinkJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("list len = %d, want 2: %s", len(list), rec.Body.String())
	}
	if list[0].Name != "ntfy home" || list[0].Token != "" || !list[0].TokenSet {
		t.Errorf("row 0 wrong: %+v", list[0])
	}
	if list[1].Name != "plex" || list[1].TokenSet {
		t.Errorf("row 1 wrong: %+v", list[1])
	}
	if !strings.Contains(rec.Body.String(), `"token":""`) {
		t.Errorf("token must be empty string on read: %s", rec.Body.String())
	}
}

// TestNotifications_CreateValidation is the PR4 400 contract: unknown kind
// and missing url carry the sink-construction message.
func TestNotifications_CreateValidation(t *testing.T) {
	s, token := newNotificationsTestServer(t, newFakeSinkRepo())

	rec := doJSONReq(t, s.Handler(), http.MethodPost, "/api/v1/settings/notifications", token,
		`{"name":"x","kind":"carrier-pigeon","url":"http://x"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown kind got %d, want 400: %s", rec.Code, rec.Body.String())
	}
	// The BuildSink message (JSON-escaped in the body).
	if !strings.Contains(rec.Body.String(), "unknown notification kind") || !strings.Contains(rec.Body.String(), "carrier-pigeon") {
		t.Errorf("want the BuildSink message, got: %s", rec.Body.String())
	}

	rec = doJSONReq(t, s.Handler(), http.MethodPost, "/api/v1/settings/notifications", token,
		`{"name":"x","kind":"webhook"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing url got %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "webhook requires a url") {
		t.Errorf("want the BuildSink message, got: %s", rec.Body.String())
	}

	rec = doJSONReq(t, s.Handler(), http.MethodPost, "/api/v1/settings/notifications", token,
		`{"kind":"webhook","url":"http://x"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing name got %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

// TestNotifications_UpdateKeepsStoredToken is the masked-edit round trip:
// an empty token on PUT keeps the stored (decrypted) token.
func TestNotifications_UpdateKeepsStoredToken(t *testing.T) {
	repo := newFakeSinkRepo()
	s, token := newNotificationsTestServer(t, repo)

	rec := doJSONReq(t, s.Handler(), http.MethodPost, "/api/v1/settings/notifications", token,
		`{"name":"ntfy home","kind":"webhook","url":"http://ntfy.example:2000/mediarr","token":"s3cret"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create got %d, want 201: %s", rec.Code, rec.Body.String())
	}

	// Change name + url, send an empty token (what the UI sends after a
	// masked read).
	rec = doJSONReq(t, s.Handler(), http.MethodPut, "/api/v1/settings/notifications/1", token,
		`{"name":"ntfy home v2","kind":"webhook","url":"http://ntfy.example:2000/other","token":""}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update got %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var updated notificationSinkJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.Name != "ntfy home v2" || updated.URL != "http://ntfy.example:2000/other" {
		t.Errorf("update fields wrong: %+v", updated)
	}
	if updated.Token != "" || !updated.TokenSet {
		t.Errorf("update response must still mask + report token_set: %+v", updated)
	}
	if stored := repo.byID[1]; stored.Token != "s3cret" {
		t.Errorf("stored token not preserved on masked edit: %q", stored.Token)
	}

	// A non-empty token replaces the stored one.
	doJSONReq(t, s.Handler(), http.MethodPut, "/api/v1/settings/notifications/1", token,
		`{"name":"ntfy home v3","kind":"webhook","url":"http://ntfy.example:2000/other","token":"newtok"}`)
	if stored := repo.byID[1]; stored.Token != "newtok" {
		t.Errorf("stored token not replaced: %q", stored.Token)
	}
}

// TestNotifications_Update404 is the unknown-id contract on PUT.
func TestNotifications_Update404(t *testing.T) {
	s, token := newNotificationsTestServer(t, newFakeSinkRepo())
	rec := doJSONReq(t, s.Handler(), http.MethodPut, "/api/v1/settings/notifications/99", token,
		`{"name":"x","kind":"webhook","url":"http://x"}`)
	if rec.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404: %s", rec.Code, rec.Body.String())
	}
}

// TestNotifications_Delete is the 204-then-404 delete contract.
func TestNotifications_Delete(t *testing.T) {
	repo := newFakeSinkRepo()
	s, token := newNotificationsTestServer(t, repo)
	rec := doJSONReq(t, s.Handler(), http.MethodPost, "/api/v1/settings/notifications", token,
		`{"name":"x","kind":"webhook","url":"http://x"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create got %d, want 201: %s", rec.Code, rec.Body.String())
	}

	rec = doReq(t, s.Handler(), http.MethodDelete, "/api/v1/settings/notifications/1", token)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete got %d, want 204: %s", rec.Code, rec.Body.String())
	}
	if len(repo.byID) != 0 {
		t.Errorf("repo still has %d rows after delete", len(repo.byID))
	}

	rec = doReq(t, s.Handler(), http.MethodDelete, "/api/v1/settings/notifications/1", token)
	if rec.Code != http.StatusNotFound {
		t.Errorf("delete unknown got %d, want 404: %s", rec.Code, rec.Body.String())
	}
}

// TestNotifications_TestEndpointDelivers is the PR4 test-button contract:
// the stored (decrypted) config builds the sink, a test event is delivered,
// and success is 200 {ok: true}.
func TestNotifications_TestEndpointDelivers(t *testing.T) {
	repo := newFakeSinkRepo()
	s, token := newNotificationsTestServer(t, repo)

	var gotAuth, gotUA string
	var gotBody map[string]string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotUA = r.Header.Get("User-Agent")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()

	rec := doJSONReq(t, s.Handler(), http.MethodPost, "/api/v1/settings/notifications", token,
		`{"name":"ntfy","kind":"webhook","url":"`+target.URL+`","token":"s3cret","user_agent":"mediarr-test/1.0"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create got %d, want 201: %s", rec.Code, rec.Body.String())
	}

	rec = doReq(t, s.Handler(), http.MethodPost, "/api/v1/settings/notifications/1/test", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("test got %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var out map[string]bool
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !out["ok"] {
		t.Errorf("want ok:true, got %s", rec.Body.String())
	}
	if gotAuth != "Bearer s3cret" {
		t.Errorf("test event must carry the decrypted token as Bearer auth, got %q", gotAuth)
	}
	if gotUA != "mediarr-test/1.0" {
		t.Errorf("test event must carry the stored user agent, got %q", gotUA)
	}
	if gotBody["type"] != "test" {
		t.Errorf("test event type wrong: %v", gotBody)
	}
}

// TestNotifications_TestEndpointFailures covers 400 (stored config is not
// constructible), 502 (delivery failure), and 404 (unknown id).
func TestNotifications_TestEndpointFailures(t *testing.T) {
	repo := newFakeSinkRepo()
	s, token := newNotificationsTestServer(t, repo)

	// 400: the repo holds a config BuildSink rejects (corrupt/legacy row).
	repo.byID[1] = notify.SinkRecord{ID: 1, Name: "bad", Kind: notify.SinkKind("carrier-pigeon"), URL: "http://x"}
	rec := doReq(t, s.Handler(), http.MethodPost, "/api/v1/settings/notifications/1/test", token)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("unbuildable config got %d, want 400: %s", rec.Code, rec.Body.String())
	}

	// 502: unreachable target.
	repo.byID[2] = notify.SinkRecord{ID: 2, Name: "down", Kind: notify.KindWebhook, URL: "http://127.0.0.1:1"}
	rec = doReq(t, s.Handler(), http.MethodPost, "/api/v1/settings/notifications/2/test", token)
	if rec.Code != http.StatusBadGateway {
		t.Errorf("unreachable target got %d, want 502: %s", rec.Code, rec.Body.String())
	}

	// 502: non-2xx target.
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer failing.Close()
	repo.byID[3] = notify.SinkRecord{ID: 3, Name: "500er", Kind: notify.KindWebhook, URL: failing.URL}
	rec = doReq(t, s.Handler(), http.MethodPost, "/api/v1/settings/notifications/3/test", token)
	if rec.Code != http.StatusBadGateway {
		t.Errorf("non-2xx target got %d, want 502: %s", rec.Code, rec.Body.String())
	}

	// 404: unknown id.
	rec = doReq(t, s.Handler(), http.MethodPost, "/api/v1/settings/notifications/99/test", token)
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown id got %d, want 404: %s", rec.Code, rec.Body.String())
	}
}
