package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/brandenk514/mediarr/internal/auth"
	"github.com/brandenk514/mediarr/internal/health"
	"github.com/brandenk514/mediarr/internal/settings"
)

// fakeTagUsage is an in-memory TagUsageer for the tag-usage endpoint tests.
type fakeTagUsage struct {
	series   int
	episodes int
	calls    int
	lastTag  string
	// Prowlarr PR4: indexer usage for IndexersByTag (count + a-to-z names).
	indexerNames   []string
	indexerCalls   int
	lastIndexerTag string
}

func (f *fakeTagUsage) CountByTag(_ context.Context, label string) (int, int, error) {
	f.calls++
	f.lastTag = label
	return f.series, f.episodes, nil
}

func (f *fakeTagUsage) IndexersByTag(_ context.Context, label string) (int, []string, error) {
	f.indexerCalls++
	f.lastIndexerTag = label
	if f.indexerNames == nil {
		return 0, []string{}, nil
	}
	return len(f.indexerNames), f.indexerNames, nil
}

// newSettingsSectionTestServer builds an authenticated server wired with the
// P5 generic section endpoints over a fresh in-memory store + tag usage.
func newSettingsSectionTestServer(t *testing.T) (*Server, *fakeSettingsStore, *fakeTagUsage, string) {
	t.Helper()
	store := newFakeStore()
	u, _ := store.CreateUser(context.Background(), "admin", "", "admin", "hash")
	pair, err := auth.NewToken("ma_live_")
	if err != nil {
		t.Fatalf("new token: %v", err)
	}
	_, _ = store.CreateToken(context.Background(), u, "test", pair, nil)

	settingsStore := newFakeSettingsStore()
	tagUsage := &fakeTagUsage{series: 3, episodes: 12}
	s := NewServer(health.NewChecker(nil, nil))
	s.SetAuth(&AuthDeps{Repo: store})
	s.SetSettingsSections(&SettingsSectionDeps{Store: settingsStore, TagUsage: tagUsage})
	return s, settingsStore, tagUsage, pair.Raw
}

// TestSettingsSections_RequiresAuth is the P5 auth contract: every generic
// section route is 401 without a token.
func TestSettingsSections_RequiresAuth(t *testing.T) {
	s, _, _, _ := newSettingsSectionTestServer(t)
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/settings/ui"},
		{http.MethodPut, "/api/v1/settings/general"},
		{http.MethodGet, "/api/v1/settings/tags/1/usage"},
	} {
		rec := doReq(t, s.Handler(), tc.method, tc.path, "")
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without token: got %d, want 401", tc.method, tc.path, rec.Code)
		}
	}
}

// TestSettingsSections_GetDefaults is the "fresh install reads complete
// documents" contract: an unsaved section returns its defaults.
func TestSettingsSections_GetDefaults(t *testing.T) {
	s, _, _, token := newSettingsSectionTestServer(t)
	rec := doReq(t, s.Handler(), http.MethodGet, "/api/v1/settings/general", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var gen map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &gen); err != nil {
		t.Fatal(err)
	}
	if gen["port"].(float64) != 8080 || gen["host"] != "0.0.0.0" {
		t.Errorf("general defaults wrong: %v", gen)
	}

	rec = doReq(t, s.Handler(), http.MethodGet, "/api/v1/settings/metadata-source", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var doc struct {
		Sources map[string][]string `json:"sources"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Sources["tv"]) != 3 || doc.Sources["tv"][0] != "tvdb" {
		t.Errorf("tv source list wrong: %v", doc.Sources["tv"])
	}
}

// TestSettingsSections_PutRoundTrip is the per-section get/update round trip:
// save a section and read it back.
func TestSettingsSections_PutRoundTrip(t *testing.T) {
	s, store, _, token := newSettingsSectionTestServer(t)
	rec := doJSONReq(t, s.Handler(), http.MethodPut, "/api/v1/settings/ui", token,
		`{"default_view":"calendar","date_format":"DD/MM/YYYY","time_format":"12h","items_per_page":25}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["default_view"] != "calendar" || out["items_per_page"].(float64) != 25 {
		t.Errorf("put response wrong: %v", out)
	}

	// And the store holds the document.
	raw, _ := store.GetSectionDocument(context.Background(), settings.SectionUI)
	if raw == nil {
		t.Fatal("expected the ui document to be stored")
	}
	var stored map[string]any
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatal(err)
	}
	if stored["time_format"] != "12h" {
		t.Errorf("stored time_format wrong: %v", stored)
	}

	// And GET now returns the saved values.
	rec = doReq(t, s.Handler(), http.MethodGet, "/api/v1/settings/ui", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rec.Code)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["default_view"] != "calendar" {
		t.Errorf("get after put wrong: %v", out)
	}
}

// TestSettingsSections_PutUnknownSection is the slug contract: slugs outside
// the eleven generic sections (including the dedicated indexers /
// download-clients names) are 400.
func TestSettingsSections_PutUnknownSection(t *testing.T) {
	s, _, _, token := newSettingsSectionTestServer(t)
	for _, path := range []string{
		"/api/v1/settings/bogus",
		"/api/v1/settings/indexers",
		"/api/v1/settings/download-clients",
	} {
		rec := doJSONReq(t, s.Handler(), http.MethodPut, path, token, `{}`)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("PUT %s: got %d, want 400", path, rec.Code)
		}
		rec = doReq(t, s.Handler(), http.MethodGet, path, token)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("GET %s: got %d, want 400", path, rec.Code)
		}
	}
}

// TestSettingsSections_PutValidation is the per-section validation contract:
// an invalid document body is a 400 and nothing is stored.
func TestSettingsSections_PutValidation(t *testing.T) {
	s, store, _, token := newSettingsSectionTestServer(t)
	rec := doJSONReq(t, s.Handler(), http.MethodPut, "/api/v1/settings/ui", token,
		`{"default_view":"nope","date_format":"YYYY-MM-DD","time_format":"24h","items_per_page":50}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if raw, _ := store.GetSectionDocument(context.Background(), settings.SectionUI); raw != nil {
		t.Error("an invalid document must not be stored")
	}

	rec = doJSONReq(t, s.Handler(), http.MethodPut, "/api/v1/settings/general", token, `{not json`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("corrupt body: got %d, want 400", rec.Code)
	}
}

// TestSettingsSections_ConnectSecretMasking is the masked-secret contract for
// the connect document: the token is never returned, only token_set; an empty
// token on write keeps the stored value.
func TestSettingsSections_ConnectSecretMasking(t *testing.T) {
	s, store, _, token := newSettingsSectionTestServer(t)
	body := `{"notifications":[{"id":1,"name":"hook","kind":"webhook","enabled":true,"options":{"url":"https://hooks.example/hook","token":"top-secret","token_set":false}}]}`
	rec := doJSONReq(t, s.Handler(), http.MethodPut, "/api/v1/settings/connect", token, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); strings.Contains(got, "top-secret") {
		t.Error("response must not leak the token")
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	notifs := out["notifications"].([]any)
	opts := notifs[0].(map[string]any)["options"].(map[string]any)
	if opts["token"] != "" || opts["token_set"] != true {
		t.Errorf("masked shape wrong: %v", opts)
	}

	// The stored document carries the plaintext token (the fake store is
	// unencrypted, unlike the vault-encrypted Postgres store).
	raw, _ := store.GetSectionDocument(context.Background(), settings.SectionConnect)
	if raw == nil || !strings.Contains(string(raw), "top-secret") {
		t.Fatal("expected the token to be stored")
	}

	// Now edit with a blank token: the stored value must survive.
	rec = doJSONReq(t, s.Handler(), http.MethodPut, "/api/v1/settings/connect", token,
		`{"notifications":[{"id":1,"name":"hook","kind":"webhook","enabled":false,"options":{"url":"https://hooks.example/hook2","token":"","token_set":true}}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", rec.Code, rec.Body.String())
	}
	raw, _ = store.GetSectionDocument(context.Background(), settings.SectionConnect)
	if raw == nil || !strings.Contains(string(raw), "top-secret") {
		t.Error("expected the stored token to be preserved (empty = keep)")
	}
	// And the URL did change.
	if raw == nil || !strings.Contains(string(raw), "hook2") {
		t.Error("expected the URL to be updated")
	}
}

// TestSettingsSections_ImportListSecretMasking is the same contract for the
// import-lists document (the Sonarr bridge API key).
func TestSettingsSections_ImportListSecretMasking(t *testing.T) {
	s, _, _, token := newSettingsSectionTestServer(t)
	rec := doJSONReq(t, s.Handler(), http.MethodPut, "/api/v1/settings/import-lists", token,
		`{"lists":[{"id":1,"name":"bridge","kind":"sonarr","url":"https://sonarr.example","enabled":true,"api_key":"sk-super-secret","api_key_set":false}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); strings.Contains(got, "sk-super-secret") {
		t.Error("response must not leak the API key")
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	lists := out["lists"].([]any)
	l0 := lists[0].(map[string]any)
	if l0["api_key"] != "" || l0["api_key_set"] != true {
		t.Errorf("masked shape wrong: %v", l0)
	}
}

// TestSettingsSections_TagUsage is the tags usage-view contract: known tags
// return their label + counts; unknown tag ids are 404.
func TestSettingsSections_TagUsage(t *testing.T) {
	s, _, tagUsage, token := newSettingsSectionTestServer(t)
	// First, save a tags document with two tags.
	rec := doJSONReq(t, s.Handler(), http.MethodPut, "/api/v1/settings/tags", token,
		`{"tags":[{"id":1,"label":"family","description":""},{"id":2,"label":"anime","description":"long-running"}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("put tags: got %d, want 200: %s", rec.Code, rec.Body.String())
	}

	// Prowlarr PR4: seed the fake's indexer usage so the response sub-object
	// has content to assert against.
	tagUsage.indexerNames = []string{"Crazycrawler", "Nyaa"}
	rec = doReq(t, s.Handler(), http.MethodGet, "/api/v1/settings/tags/2/usage", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var usage TagUsage
	if err := json.Unmarshal(rec.Body.Bytes(), &usage); err != nil {
		t.Fatalf("unmarshal usage: %v", err)
	}
	if usage.ID != 2 || usage.Label != "anime" || usage.Series != 3 || usage.Episodes != 12 {
		t.Errorf("usage wrong: %+v", usage)
	}
	if usage.Indexers.Count != 2 || len(usage.Indexers.Names) != 2 ||
		usage.Indexers.Names[0] != "Crazycrawler" || usage.Indexers.Names[1] != "Nyaa" {
		t.Errorf("usage indexers wrong: %+v", usage.Indexers)
	}
	if tagUsage.lastTag != "anime" {
		t.Errorf("expected the repo to be called with the label, got %q", tagUsage.lastTag)
	}
	if tagUsage.lastIndexerTag != "anime" {
		t.Errorf("expected IndexersByTag to be called with the label, got %q", tagUsage.lastIndexerTag)
	}

	rec = doReq(t, s.Handler(), http.MethodGet, "/api/v1/settings/tags/99/usage", token)
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown tag: got %d, want 404", rec.Code)
	}
	rec = doReq(t, s.Handler(), http.MethodGet, "/api/v1/settings/tags/abc/usage", token)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("non-numeric tag id: got %d, want 400", rec.Code)
	}
}

// TestSettingsSections_AllSectionsReadable is the shell contract: every one
// of the eleven generic slugs is GET-able on a fresh install.
func TestSettingsSections_AllSectionsReadable(t *testing.T) {
	s, _, _, token := newSettingsSectionTestServer(t)
	for _, sec := range settings.AllSections {
		rec := doReq(t, s.Handler(), http.MethodGet, "/api/v1/settings/"+string(sec), token)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s: got %d, want 200: %s", sec, rec.Code, rec.Body.String())
		}
	}
}
