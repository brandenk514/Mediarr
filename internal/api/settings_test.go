package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/brandenk514/mediarr/internal/auth"
	"github.com/brandenk514/mediarr/internal/health"
	"github.com/brandenk514/mediarr/internal/settings"
)

// fakeSettingsStore is an in-memory settings.Store for API contract tests. It
// mirrors the Postgres store's contract: GetX returns a non-nil default document
// when nothing has been saved, PutX upserts, and a zero library override clears
// the row.
type fakeSettingsStore struct {
	mu   sync.Mutex
	dc   *settings.DownloadClient
	libs map[settings.LibraryKind]*settings.LibraryOverride
}

func newFakeSettingsStore() *fakeSettingsStore {
	return &fakeSettingsStore{libs: map[settings.LibraryKind]*settings.LibraryOverride{}}
}

func (s *fakeSettingsStore) GetDownloadClient(ctx context.Context) (*settings.DownloadClient, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dc == nil {
		return &settings.DownloadClient{Kind: "mock"}, nil
	}
	cp := *s.dc
	return &cp, nil
}

func (s *fakeSettingsStore) PutDownloadClient(ctx context.Context, dc *settings.DownloadClient) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *dc
	s.dc = &cp
	return nil
}

func (s *fakeSettingsStore) GetLibrary(ctx context.Context, k settings.LibraryKind) (*settings.LibraryOverride, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if o, ok := s.libs[k]; ok {
		cp := *o
		return &cp, nil
	}
	return &settings.LibraryOverride{}, nil
}

func (s *fakeSettingsStore) PutLibrary(ctx context.Context, k settings.LibraryKind, o *settings.LibraryOverride) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if o.IsZero() {
		delete(s.libs, k)
		return nil
	}
	cp := *o
	s.libs[k] = &cp
	return nil
}

// newSettingsTestServer builds an authenticated server wired with the settings
// endpoints over a fresh in-memory store.
func newSettingsTestServer(t *testing.T) (*Server, *fakeSettingsStore, string) {
	t.Helper()
	store := newFakeStore()
	u, _ := store.CreateUser(context.Background(), "admin", "", "admin", "hash")
	pair, err := auth.NewToken("ma_live_")
	if err != nil {
		t.Fatalf("new token: %v", err)
	}
	_, _ = store.CreateToken(context.Background(), u, "test", pair, nil)

	settingsStore := newFakeSettingsStore()
	s := NewServer(health.NewChecker(nil, nil))
	s.SetAuth(&AuthDeps{Repo: store})
	s.SetSettings(&SettingsDeps{Store: settingsStore})
	return s, settingsStore, pair.Raw
}

// TestSettings_RequiresAuth is the #44 auth contract: all settings routes are
// 401 without a token.
func TestSettings_RequiresAuth(t *testing.T) {
	s, _, _ := newSettingsTestServer(t)
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/settings/download-client"},
		{http.MethodPut, "/api/v1/settings/download-client"},
		{http.MethodGet, "/api/v1/settings/library"},
		{http.MethodPut, "/api/v1/settings/library/movie"},
	} {
		rec := doReq(t, s.Handler(), tc.method, tc.path, "")
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without token: got %d, want 401", tc.method, tc.path, rec.Code)
		}
	}
}

// TestSettings_GetDownloadClient_Default is the #44 "always renderable" contract:
// an empty store yields a default document (kind mock) with no secrets present.
func TestSettings_GetDownloadClient_Default(t *testing.T) {
	s, _, token := newSettingsTestServer(t)
	rec := doReq(t, s.Handler(), http.MethodGet, "/api/v1/settings/download-client", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["kind"] != "mock" {
		t.Errorf("expected default kind mock, got %v", out["kind"])
	}
	if out["qb_api_key_set"] != false || out["sab_api_key_set"] != false {
		t.Errorf("expected no secrets present by default, got %v", out)
	}
}

// TestSettings_PutDownloadClient is the #44 masked-write contract: a valid
// qbittorrent config is saved (200) and echoed back with the key masked to a
// presence boolean — never the key itself.
func TestSettings_PutDownloadClient(t *testing.T) {
	s, store, token := newSettingsTestServer(t)
	rec := doJSONReq(t, s.Handler(), http.MethodPut, "/api/v1/settings/download-client", token,
		`{"kind":"qbittorrent","qb_base":"https://qbit.example","qb_api_key":"top-secret-key"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["kind"] != "qbittorrent" {
		t.Errorf("expected kind qbittorrent, got %v", out["kind"])
	}
	if out["qb_api_key_set"] != true {
		t.Errorf("expected qb_api_key_set true, got %v", out["qb_api_key_set"])
	}
	if _, present := out["qb_api_key"]; present {
		t.Error("response must not contain a qb_api_key field")
	}
	if strings.Contains(rec.Body.String(), "top-secret-key") {
		t.Error("response body leaks the API key value")
	}
	// The key must be persisted (the store decrypts to plaintext on read).
	got, _ := store.GetDownloadClient(context.Background())
	if got == nil || got.QBAPIKey != "top-secret-key" {
		t.Errorf("expected the API key to be stored, got %+v", got)
	}
}

// TestSettings_PutDownloadClient_UnknownKind is the #44 kind-validation
// contract: a kind outside {mock, qbittorrent, sabnzbd} is a 400.
func TestSettings_PutDownloadClient_UnknownKind(t *testing.T) {
	s, _, token := newSettingsTestServer(t)
	rec := doJSONReq(t, s.Handler(), http.MethodPut, "/api/v1/settings/download-client", token,
		`{"kind":"aria2"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

// TestSettings_PutDownloadClient_Validation is the #44 factory-mirror contract:
// a qbittorrent config with no base URL is a 400 (it would not build).
func TestSettings_PutDownloadClient_Validation(t *testing.T) {
	s, _, token := newSettingsTestServer(t)
	rec := doJSONReq(t, s.Handler(), http.MethodPut, "/api/v1/settings/download-client", token,
		`{"kind":"qbittorrent","qb_api_key":"k"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("qb no base: got %d, want 400: %s", rec.Code, rec.Body.String())
	}
	// sabnzbd without an API key is also a 400.
	rec = doJSONReq(t, s.Handler(), http.MethodPut, "/api/v1/settings/download-client", token,
		`{"kind":"sabnzbd","sab_base":"https://sab.example"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("sab no key: got %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

// TestSettings_PutDownloadClient_KeepSecret is the #44 masked-edit contract:
// an empty secret in the body preserves the stored value (the UI can edit the
// non-secret fields without re-sending — or clobbering — a credential).
func TestSettings_PutDownloadClient_KeepSecret(t *testing.T) {
	s, store, token := newSettingsTestServer(t)
	// First, store a credential.
	_ = store.PutDownloadClient(context.Background(), &settings.DownloadClient{
		Kind: "qbittorrent", QBBase: "https://qbit.example", QBAPIKey: "original-key",
	})
	// Now edit with an empty api_key (masked-edit "keep") and a new base.
	rec := doJSONReq(t, s.Handler(), http.MethodPut, "/api/v1/settings/download-client", token,
		`{"kind":"qbittorrent","qb_base":"https://new.example","qb_api_key":""}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", rec.Code, rec.Body.String())
	}
	got, _ := store.GetDownloadClient(context.Background())
	if got == nil {
		t.Fatal("expected a stored client")
	}
	if got.QBBase != "https://new.example" {
		t.Errorf("expected the base to be updated, got %q", got.QBBase)
	}
	if got.QBAPIKey != "original-key" {
		t.Errorf("expected the stored key to be preserved (empty = keep), got %q", got.QBAPIKey)
	}
}

// TestSettings_GetLibrary is the #44 "one document for all four libraries"
// contract: GET returns a map keyed by all four kinds, each a valid override
// shape (defaults when unsaved).
func TestSettings_GetLibrary(t *testing.T) {
	s, store, token := newSettingsTestServer(t)
	// Seed one library so the response is non-trivial.
	off := false
	_ = store.PutLibrary(context.Background(), settings.LibraryMusic, &settings.LibraryOverride{
		Path: "/music", ChecksumVerify: &off,
	})
	rec := doReq(t, s.Handler(), http.MethodGet, "/api/v1/settings/library", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var out map[string]map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("body is not a JSON object: %v", err)
	}
	if len(out) != 4 {
		t.Fatalf("expected 4 libraries, got %d: %v", len(out), out)
	}
	music := out["music"]
	if music == nil || music["path"] != "/music" {
		t.Errorf("expected the music override path /music, got %v", music)
	}
	// An unsaved library is present with default (empty) values.
	if movie, ok := out["movie"]; !ok || movie["path"] != "" {
		t.Errorf("expected movie to be present with an empty path, got %v", movie)
	}
}

// TestSettings_PutLibrary is the #44 per-library write contract: save one
// library's override and read it back.
func TestSettings_PutLibrary(t *testing.T) {
	s, store, token := newSettingsTestServer(t)
	rec := doJSONReq(t, s.Handler(), http.MethodPut, "/api/v1/settings/library/movie", token,
		`{"path":"/media/movies","quality_profile":"High Quality 1080p"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", rec.Code, rec.Body.String())
	}
	got, _ := store.GetLibrary(context.Background(), settings.LibraryMovie)
	if got == nil || got.Path != "/media/movies" || got.QualityProfile != "High Quality 1080p" {
		t.Errorf("expected the movie override to be stored, got %+v", got)
	}
}

// TestSettings_PutLibrary_UnknownKind is the #44 kind-validation contract: a
// library kind outside {movie, tv, music, book} is a 400.
func TestSettings_PutLibrary_UnknownKind(t *testing.T) {
	s, _, token := newSettingsTestServer(t)
	rec := doJSONReq(t, s.Handler(), http.MethodPut, "/api/v1/settings/library/anime", token,
		`{"path":"/anime"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400: %s", rec.Code, rec.Body.String())
	}
}
