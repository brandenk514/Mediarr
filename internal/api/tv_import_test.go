package api

// #107: the TV scan/import-by-path endpoint accepts a raw filesystem path
// (req.Path, the "unmapped" import case). These tests pin the confinement:
// the ad-hoc path must resolve strictly inside the TV media root — absolute
// paths outside the root, ".." traversal, and symlink escapes are 400s —
// while in-root scans (absolute and relative) keep working.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brandenk514/mediarr/internal/auth"
	"github.com/brandenk514/mediarr/internal/health"
	tvsvc "github.com/brandenk514/mediarr/internal/services/tv"
)

// tvImportServer is a fully-wired, authenticated server with a TV service
// confined to the given media root, for the import-by-path endpoint tests.
type tvImportServer struct {
	s     *Server
	token string
}

func newTVImportServer(t *testing.T, mediaRoot string) *tvImportServer {
	t.Helper()
	store := newFakeStore()
	s := NewServer(health.NewChecker(nil, nil))
	s.SetAuth(&AuthDeps{Repo: store})
	hash, err := auth.HashPassword("a-very-long-password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateUser(context.Background(), "admin", "", "admin", hash); err != nil {
		t.Fatal(err)
	}
	rec := postJSON(t, s.Handler(), "/api/v1/auth/login", loginRequest{
		Username: "admin", Password: "a-very-long-password",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d", rec.Code)
	}
	var lr loginResponse
	if err := json.NewDecoder(rec.Body).Decode(&lr); err != nil {
		t.Fatal(err)
	}
	tvSvc := tvsvc.New(tvsvc.Deps{Repo: newMemTVRepo(), MediaRoot: mediaRoot, DefaultProfile: "HD-1080p"})
	s.SetTV(&TVDeps{Svc: tvSvc})
	return &tvImportServer{s: s, token: lr.Token}
}

func (f *tvImportServer) post(t *testing.T, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+f.token)
	rec := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(rec, req)
	return rec
}

// writeTVImportLibrary lays out a one-series TV library under root.
func writeTVImportLibrary(t *testing.T, root string) {
	t.Helper()
	season := filepath.Join(root, "Alpha Show (2020)", "Season 01")
	if err := os.MkdirAll(season, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(season, "a.mkv"), []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestTVImportLibrary_ByPath_InRoot is the happy path: an ad-hoc path inside
// the TV media root scans (and matches a seeded series) exactly as before
// the confinement.
func TestTVImportLibrary_ByPath_InRoot(t *testing.T) {
	mediaRoot := t.TempDir()
	writeTVImportLibrary(t, mediaRoot)
	f := newTVImportServer(t, mediaRoot)

	rec := f.post(t, "/api/v1/tv", map[string]any{"title": "Alpha Show", "year": "2020"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("add series status = %d body=%s", rec.Code, rec.Body.String())
	}

	rec = f.post(t, "/api/v1/tv/import", map[string]any{"path": mediaRoot})
	if rec.Code != http.StatusOK {
		t.Fatalf("import status = %d body=%s", rec.Code, rec.Body.String())
	}
	var report struct {
		Root       string `json:"Root"`
		Scanned    int    `json:"Scanned"`
		Candidates []struct {
			Path    string `json:"Path"`
			Title   string `json:"Title"`
			Year    int    `json:"Year"`
			Matched bool   `json:"Matched"`
			Files   int    `json:"Files"`
		} `json:"Candidates"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&report); err != nil {
		t.Fatalf("decode report: %v body=%s", err, rec.Body.String())
	}
	if report.Root != mediaRoot || report.Scanned != 1 || len(report.Candidates) != 1 {
		t.Fatalf("report = %+v, want Root=%q Scanned=1 (the series folder)", report, mediaRoot)
	}
	c := report.Candidates[0]
	if c.Title != "Alpha Show" || c.Year != 2020 || !c.Matched || c.Files != 1 {
		t.Errorf("candidate = %+v, want matched Alpha Show (2020) with 1 file", c)
	}
}

// TestTVImportLibrary_ByPath_OutOfRoot (#107): ad-hoc paths that escape the
// TV media root are rejected with a 400 before the filesystem is touched:
// an absolute path outside the root, ".." traversal, a symlink pointing out
// of the root, and a nonexistent absolute path (400, not the pre-fix 404).
func TestTVImportLibrary_ByPath_OutOfRoot(t *testing.T) {
	base := t.TempDir()
	mediaRoot := filepath.Join(base, "media")
	if err := os.MkdirAll(mediaRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	// An out-of-root decoy with content: pre-fix, scanning it succeeded.
	secret := filepath.Join(base, "secret")
	if err := os.MkdirAll(secret, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secret, "leak.mkv"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(mediaRoot, "escape")); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	f := newTVImportServer(t, mediaRoot)

	cases := map[string]string{
		"absolute path outside root":   secret,
		"..-traversal out of root":     filepath.Join(mediaRoot, "..", "secret"),
		"symlink pointing out of root": filepath.Join(mediaRoot, "escape"),
		"nonexistent absolute path":    filepath.Join(base, "no-such-dir"),
	}
	for name, p := range cases {
		rec := f.post(t, "/api/v1/tv/import", map[string]any{"path": p})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400 body=%s", name, rec.Code, rec.Body.String())
			continue
		}
		if !strings.Contains(rec.Body.String(), "media root") {
			t.Errorf("%s: 400 body %q, want a message naming the media root", name, rec.Body.String())
		}
	}
}

// TestTVImportLibrary_ByPath_MissingContract pins the endpoint contract
// around the confinement: neither field → 400, and an unconfigured media
// root fails closed with a 400 (import by path needs a configured location
// to be confined to).
func TestTVImportLibrary_ByPath_MissingContract(t *testing.T) {
	mediaRoot := t.TempDir()
	f := newTVImportServer(t, mediaRoot)

	rec := f.post(t, "/api/v1/tv/import", map[string]any{})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty import body status = %d, want 400 body=%s", rec.Code, rec.Body.String())
	}

	unconfigured := newTVImportServer(t, "")
	rec = unconfigured.post(t, "/api/v1/tv/import", map[string]any{"path": mediaRoot})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unconfigured media root status = %d, want 400 body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "not configured") {
		t.Errorf("unconfigured media root body %q, want a 'not configured' message", rec.Body.String())
	}
}
