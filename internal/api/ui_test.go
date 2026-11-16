package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brandenk514/mediarr/internal/health"
	"github.com/brandenk514/mediarr/internal/metrics"
)

// This suite is the failing-first (TDD) contract for issue #87: same-origin
// UI serving with SPA fallback, isolated from API/health/metrics/events.
// It runs against a synthetic Vite dist built in a temp dir (fixture), not
// against the real ui/dist, so it is deterministic and independent of the
// #88 image work.
//
// Acceptance coverage: AC1-10 of #87 plus the review lessons (mux-owned
// canonicalisation, os.OpenInRoot containment, segment-boundary reserved
// prefixes, trailing-slash empty segments, encoded-path/query separation,
// GET/HEAD-only fallback, no directory listing).

// uiIndexHTML is the fixture SPA entry point; every fallback response must
// return it byte-for-byte.
const uiIndexHTML = `<html><head><script src="/assets/app-abc123.js"></script><link rel="stylesheet" href="/assets/style-def456.css"></head><body><div id="root"></div></body></html>`

// uiSentinel is the content of a file OUTSIDE the fixture asset root. No
// request — however it is encoded — may ever return it.
const uiSentinel = "TOP-SECRET-SENTINEL-CONTENT"

// uiFixture is a synthetic Vite dist plus an out-of-root sentinel file.
type uiFixture struct {
	dir string // the asset root (what UIDeps.Dir points at)
}

// newUIFixture builds:
//
//	<base>/secret.txt                    <- sentinel, OUTSIDE the asset root
//	<base>/ui/index.html                 <- SPA entry point
//	<base>/ui/assets/app-abc123.js       <- hashed JS asset
//	<base>/ui/assets/style-def456.css    <- hashed CSS asset
//	<base>/ui/nested/deep/chunk-xyz.js   <- asset in a subdirectory
func newUIFixture(t *testing.T) *uiFixture {
	t.Helper()
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "secret.txt"), []byte(uiSentinel), 0o600); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}
	dir := filepath.Join(base, "ui")
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", rel, err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	write("index.html", uiIndexHTML)
	write("assets/app-abc123.js", "console.log('app');\n")
	write("assets/style-def456.css", "body { color: red; }\n")
	write("nested/deep/chunk-xyz.js", "export const x = 1;\n")
	return &uiFixture{dir: dir}
}

// newUIServer wires the UI catch-all onto a fresh server (the #87 layering:
// API/health routes registered first, UI catch-all last).
func newUIServer(t *testing.T, dir string) *Server {
	t.Helper()
	s := NewServer(health.NewChecker(nil, nil))
	s.SetUI(&UIDeps{Dir: dir})
	return s
}

// doUIRequest runs one request through the full middleware chain and returns
// the recorder.
func doUIRequest(t *testing.T, s *Server, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

// literalPathRequest builds a request whose URL.Path is exactly raw,
// bypassing url.Parse (which may normalise backslashes). A raw "\\" and a
// "%5C" decode to the same path, so direct construction covers both.
func literalPathRequest(method, raw string) *http.Request {
	return &http.Request{
		Method: method,
		URL:    &url.URL{Path: raw},
		Host:   "127.0.0.1",
	}
}

func serveLiteral(t *testing.T, s *Server, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

// ---- AC1: GET / returns the SPA entry point -------------------------------

func TestUI_RootServesIndexHTML(t *testing.T) {
	fx := newUIFixture(t)
	s := newUIServer(t, fx.dir)
	rec := doUIRequest(t, s, http.MethodGet, "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / status = %d, want 200 (body: %q)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	if rec.Body.String() != uiIndexHTML {
		t.Errorf("body = %q, want the fixture index.html (the app's HTML root element)", rec.Body.String())
	}
}

// ---- AC2: client-routing deep links fall back to index.html ---------------

func TestUI_DeepLinksServeIndexHTML(t *testing.T) {
	fx := newUIFixture(t)
	s := newUIServer(t, fx.dir)
	for _, p := range []string{"/movies", "/movies/add", "/login", "/settings/billing/plan-42"} {
		rec := doUIRequest(t, s, http.MethodGet, p)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s status = %d, want 200 (body: %q)", p, rec.Code, rec.Body.String())
			continue
		}
		if rec.Body.String() != uiIndexHTML {
			t.Errorf("GET %s served %q, want index.html", p, rec.Body.String())
		}
	}
}

// ---- AC3: existing assets 200 with content type; missing assets 404 -------

func TestUI_ExistingAssetServedWithContentType(t *testing.T) {
	fx := newUIFixture(t)
	s := newUIServer(t, fx.dir)
	cases := []struct {
		path   string
		wantCT string
	}{
		{"/assets/app-abc123.js", "text/javascript; charset=utf-8"},
		{"/assets/style-def456.css", "text/css; charset=utf-8"},
		{"/nested/deep/chunk-xyz.js", "text/javascript; charset=utf-8"},
	}
	for _, c := range cases {
		rec := doUIRequest(t, s, http.MethodGet, c.path)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s status = %d, want 200 (body: %q)", c.path, rec.Code, rec.Body.String())
			continue
		}
		if got := rec.Header().Get("Content-Type"); got != c.wantCT {
			t.Errorf("GET %s Content-Type = %q, want %q", c.path, got, c.wantCT)
		}
	}
}

func TestUI_MissingAssetIs404NotIndex(t *testing.T) {
	fx := newUIFixture(t)
	s := newUIServer(t, fx.dir)
	rec := doUIRequest(t, s, http.MethodGet, "/assets/does-not-exist.js")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if rec.Body.String() == uiIndexHTML || strings.Contains(rec.Body.String(), `<div id="root">`) {
		t.Errorf("missing asset served the SPA: %q", rec.Body.String())
	}
}

// The #87 asset contract: a missing path with a file extension is an asset
// reference (404); a missing path without one is a client-routing deep link
// (index.html). This test pins the heuristic so a "serve everything 404" or
// "fallback everything" regression fails.
func TestUI_MissingExtensionlessPathFallsBackToIndex(t *testing.T) {
	fx := newUIFixture(t)
	s := newUIServer(t, fx.dir)
	rec := doUIRequest(t, s, http.MethodGet, "/assets/does-not-exist")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (extensionless missing path is a deep link)", rec.Code)
	}
	if rec.Body.String() != uiIndexHTML {
		t.Errorf("body = %q, want index.html", rec.Body.String())
	}
}

// ---- AC4: reserved routes behave exactly as before ------------------------

func TestUI_ReservedRoutesUnchanged(t *testing.T) {
	fx := newUIFixture(t)
	s := newUIServer(t, fx.dir)
	s.SetAuth(&AuthDeps{Repo: newFakeStore()})
	s.SetMetrics(&MetricsDeps{M: metrics.New(metrics.Options{})})

	rec := doUIRequest(t, s, http.MethodGet, "/healthz")
	if rec.Code != http.StatusOK {
		t.Errorf("GET /healthz = %d, want 200", rec.Code)
	}
	// Nil store/cache => degraded => 503: the pre-#87 readiness behaviour.
	rec = doUIRequest(t, s, http.MethodGet, "/readyz")
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("GET /readyz = %d, want 503 (degraded with nil deps)", rec.Code)
	}
	rec = doUIRequest(t, s, http.MethodGet, "/version")
	if rec.Code != http.StatusOK {
		t.Errorf("GET /version = %d, want 200", rec.Code)
	}
	// /metrics stays auth-gated (single-user v1) — no token, no scrape.
	rec = doUIRequest(t, s, http.MethodGet, "/metrics")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("GET /metrics = %d, want 401 (auth-gated, not the SPA)", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("GET /metrics Content-Type = %q, want application/json", ct)
	}
}

// AC4 (login leg): POST /api/v1/auth/login valid (first-boot create) and
// invalid (wrong password) keep their exact API behaviour with the UI
// catch-all in place — a JSON 200 with the token, then a JSON 401, never an
// HTML fallback.
func TestUI_LoginStillAnAPIRoute(t *testing.T) {
	fx := newUIFixture(t)
	s := newUIServer(t, fx.dir)
	s.SetAuth(&AuthDeps{Repo: newFakeStore()})

	body := `{"username":"admin123","password":"password-1234"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("first-boot login status = %d, want 200 (body: %q)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("login Content-Type = %q, want application/json", ct)
	}
	if strings.Contains(rec.Body.String(), `<div id="root">`) {
		t.Error("login response is HTML: the SPA leaked into /api")
	}

	body = `{"username":"admin123","password":"wrong-password"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("invalid login status = %d, want 401 (body: %q)", rec.Code, rec.Body.String())
	}
}

// ---- AC5: the events route stays reserved and auth-gated ------------------
// (the full WebSocket upgrade is covered by the pre-existing TestEvents_*
// suite, which must stay green; this pins the non-upgrade boundary.)

func TestUI_EventsRouteStaysAuthGated(t *testing.T) {
	fx := newUIFixture(t)
	s, _, _ := newEventsTestServer(t)
	s.SetUI(&UIDeps{Dir: fx.dir})

	rec := doUIRequest(t, s, http.MethodGet, "/api/v1/events")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET /api/v1/events status = %d, want 401 (body: %q)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json (not the SPA)", ct)
	}
}

// ---- AC6: unknown /api paths are API errors, never the SPA ----------------

func TestUI_UnknownAPIPathIsNonHTML404(t *testing.T) {
	fx := newUIFixture(t)
	s := newUIServer(t, fx.dir)

	rec := doUIRequest(t, s, http.MethodGet, "/api/v1/does-not-exist")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /api/v1/does-not-exist status = %d, want 404 (body: %q)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json (non-HTML)", ct)
	}
	if strings.Contains(rec.Body.String(), "<html") || strings.Contains(rec.Body.String(), `<div id="root">`) {
		t.Errorf("unknown /api path served HTML: %q", rec.Body.String())
	}
	// The reservation is method-independent: POST to an unknown API path is
	// an API 404 too, not a 405/200 of any other shape.
	rec = doUIRequest(t, s, http.MethodPost, "/api/v1/does-not-exist")
	if rec.Code != http.StatusNotFound {
		t.Errorf("POST /api/v1/does-not-exist status = %d, want 404", rec.Code)
	}
}

// ---- Review lesson 3: segment-boundary reservation ------------------------
// "/apiary" is a UI path, NOT under /api: it gets the SPA, not an API 404.
// (The inverse — an /api/* path getting the SPA — is covered above.)

func TestUI_ApiaryIsNotReserved(t *testing.T) {
	fx := newUIFixture(t)
	s := newUIServer(t, fx.dir)

	rec := doUIRequest(t, s, http.MethodGet, "/apiary")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /apiary status = %d, want 200 (it is a UI route, not /api; body: %q)", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != uiIndexHTML {
		t.Errorf("body = %q, want index.html", rec.Body.String())
	}

	// The bare reserved prefix is still an API 404 (no route lives there).
	rec = doUIRequest(t, s, http.MethodGet, "/api")
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /api status = %d, want 404", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("GET /api Content-Type = %q, want application/json", ct)
	}
}

// ---- AC7 / lesson 6: non-GET/HEAD methods never get the SPA ---------------

func TestUI_NonGETMethodsGetNoSPA(t *testing.T) {
	fx := newUIFixture(t)
	s := newUIServer(t, fx.dir)
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		// Both a deep link AND an existing asset path: a 200 file or a 200
		// index here would be the SPA/file leaking to a write method.
		for _, p := range []string{"/movies", "/assets/app-abc123.js"} {
			rec := doUIRequest(t, s, m, p)
			if rec.Code == http.StatusOK {
				t.Errorf("%s %s status = 200 (SPA/file); must be 4xx", m, p)
				continue
			}
			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s status = %d, want 405", m, p, rec.Code)
			}
			if rec.Body.String() == uiIndexHTML {
				t.Errorf("%s %s served index.html: %q", m, p, rec.Body.String())
			}
		}
	}
	rec := doUIRequest(t, s, http.MethodPost, "/movies")
	if got := rec.Header().Get("Allow"); !strings.Contains(got, "GET") || !strings.Contains(got, "HEAD") {
		t.Errorf("Allow = %q, want GET and HEAD", got)
	}
}

// ---- AC8 / lesson 6: HEAD mirrors GET (status + headers, no body) ---------

func TestUI_HeadMirrorsGet(t *testing.T) {
	fx := newUIFixture(t)
	s := newUIServer(t, fx.dir)
	for _, p := range []string{"/", "/movies", "/assets/app-abc123.js"} {
		g := doUIRequest(t, s, http.MethodGet, p)
		h := doUIRequest(t, s, http.MethodHead, p)
		if h.Code != g.Code {
			t.Errorf("%s: HEAD status %d vs GET status %d", p, h.Code, g.Code)
		}
		if h.Code != http.StatusOK {
			t.Fatalf("%s: HEAD status = %d, want 200 (body: %q)", p, h.Code, h.Body.String())
		}
		if h.Header().Get("Content-Type") != g.Header().Get("Content-Type") {
			t.Errorf("%s Content-Type: HEAD %q vs GET %q", p, h.Header().Get("Content-Type"), g.Header().Get("Content-Type"))
		}
		if h.Header().Get("Content-Length") != g.Header().Get("Content-Length") {
			t.Errorf("%s Content-Length: HEAD %q vs GET %q", p, h.Header().Get("Content-Length"), g.Header().Get("Content-Length"))
		}
		if h.Body.Len() != 0 {
			t.Errorf("HEAD %s body = %q, want empty", p, h.Body.String())
		}
	}
}

// ---- AC9 / lesson 7: no directory listing, no out-of-root exposure --------

func TestUI_NoDirectoryListing(t *testing.T) {
	fx := newUIFixture(t)
	s := newUIServer(t, fx.dir)
	for _, p := range []string{"/assets/", "/nested/"} {
		rec := doUIRequest(t, s, http.MethodGet, p)
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s status = %d, want 404 (no directory listing)", p, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "app-abc123.js") || strings.Contains(rec.Body.String(), "chunk-xyz.js") {
			t.Errorf("GET %s leaked a directory listing: %q", p, rec.Body.String())
		}
	}
}

// AC9 / lesson 5: traversal in raw and encoded form never serves the
// out-of-root sentinel, whatever the final status (404 from the handler's
// dot-segment validation, or the mux's canonicalisation redirect).
func TestUI_TraversalNeverLeavesRoot(t *testing.T) {
	fx := newUIFixture(t)
	s := newUIServer(t, fx.dir)
	targets := []string{
		"/assets/../../secret.txt",         // raw ..: mux canonicalises
		"/%2e%2e/%2e%2e/secret.txt",        // encoded dot-dot
		"/assets/%2e%2e/%2e%2e/secret.txt", // encoded dot-dot inside a dir
		"/..%2f..%2fsecret.txt",            // encoded slash
		"/assets/..%2f..%2fsecret.txt",     // encoded slash inside a dir
	}
	for _, target := range targets {
		rec := doUIRequest(t, s, http.MethodGet, target)
		if strings.Contains(rec.Body.String(), uiSentinel) {
			t.Errorf("GET %s leaked the out-of-root sentinel (status %d)", target, rec.Code)
		}
		if rec.Code == http.StatusOK {
			t.Errorf("GET %s status = 200: something was served (body: %q)", target, rec.Body.String())
		}
	}
}

// AC9 (symlink leg): a symlink inside the asset root pointing outside it must
// not serve the target. os.OpenInRoot refuses to resolve out of the root.
func TestUI_SymlinkEscapeBlocked(t *testing.T) {
	fx := newUIFixture(t)
	base := filepath.Dir(fx.dir)
	link := filepath.Join(fx.dir, "escape-link.txt")
	if err := os.Symlink(filepath.Join(base, "secret.txt"), link); err != nil {
		t.Skipf("symlinks unavailable on this platform: %v", err)
	}
	defer os.Remove(link)

	s := newUIServer(t, fx.dir)
	rec := doUIRequest(t, s, http.MethodGet, "/escape-link.txt")
	if strings.Contains(rec.Body.String(), uiSentinel) {
		t.Fatal("served the out-of-root file through the symlink")
	}
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 (body: %q)", rec.Code, rec.Body.String())
	}
}

// ---- Review lesson 5: encoded-path vs query separation; backslashes -------

func TestUI_BackslashPathRejected(t *testing.T) {
	fx := newUIFixture(t)
	s := newUIServer(t, fx.dir)
	// A raw "\" and a "%5C" decode to the same path; browsers normalise
	// "\" to "/", raw clients don't. None of these may bypass the reserved
	// prefix isolation, escape the root, or fall through to the SPA.
	targets := []string{
		"/assets\\app-abc123.js", // %5C variant
		"/api\\v1\\auth\\login",  // backslash right after the /api boundary
		"/\\..\\..\\secret.txt",  // backslash "traversal"
	}
	for _, raw := range targets {
		rec := serveLiteral(t, s, literalPathRequest(http.MethodGet, raw))
		t.Logf("path %q -> status %d", raw, rec.Code)
		if strings.Contains(rec.Body.String(), uiSentinel) {
			t.Errorf("path %q leaked the out-of-root sentinel", raw)
		}
		if rec.Body.String() == uiIndexHTML {
			t.Errorf("path %q fell through to the SPA", raw)
		}
		if rec.Code == http.StatusOK {
			t.Errorf("path %q status = 200; must be rejected", raw)
		}
		if rec.Code != http.StatusBadRequest && rec.Code != http.StatusNotFound {
			t.Errorf("path %q status = %d, want 400 (or a 404-class rejection)", raw, rec.Code)
		}
	}
}

func TestUI_EncodedPathVsQuerySeparation(t *testing.T) {
	fx := newUIFixture(t)
	s := newUIServer(t, fx.dir)

	// A real query string is separated by net/url before the handler sees
	// the path: the asset still serves, query ignored.
	rec := doUIRequest(t, s, http.MethodGet, "/assets/app-abc123.js?v=42")
	if rec.Code != http.StatusOK {
		t.Fatalf("query-string request status = %d, want 200 (body: %q)", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "console.log('app');\n" {
		t.Errorf("query-string request served %q, want the asset", rec.Body.String())
	}

	// An encoded %3F decodes to a literal '?' INSIDE the file name: it must
	// not be treated as a query separator (no truncation at the decoded
	// '?'). "app.js?x" does not exist and is file-like, so it 404s — it may
	// not serve index.html, and it may not serve app.js under a truncated
	// name.
	rec = doUIRequest(t, s, http.MethodGet, "/assets/app.js%3Fx")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("%%3F path status = %d, want 404 (body: %q)", rec.Code, rec.Body.String())
	}
	if rec.Body.String() == uiIndexHTML || strings.Contains(rec.Body.String(), "console.log") {
		t.Errorf("%%3F path served SPA/asset: %q", rec.Body.String())
	}
}

// ---- Review lesson 4: trailing-slash (empty-segment) asset paths 404 ------

func TestUI_TrailingSlashAssetIs404(t *testing.T) {
	fx := newUIFixture(t)
	s := newUIServer(t, fx.dir)
	for _, p := range []string{"/assets/app-abc123.js/", "/nested/deep/"} {
		rec := doUIRequest(t, s, http.MethodGet, p)
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s status = %d, want 404 (trailing slash is a directory reference)", p, rec.Code)
		}
		if rec.Body.String() == uiIndexHTML {
			t.Errorf("GET %s fell through to the SPA", p)
		}
	}
}

// ---- Review lesson 1: the mux owns canonicalisation (// double slash) -----
// The handler must not hand-roll redirects from request-derived URLs; the
// // request gets the mux's own canonical redirect to the clean path.

func TestUI_DoubleSlashCleanedByMux(t *testing.T) {
	fx := newUIFixture(t)
	s := newUIServer(t, fx.dir)
	rec := doUIRequest(t, s, http.MethodGet, "//assets/app-abc123.js")
	if rec.Code != http.StatusTemporaryRedirect && rec.Code != http.StatusMovedPermanently {
		t.Fatalf("status = %d, want the mux-owned canonical redirect (301/307)", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/assets/app-abc123.js" {
		t.Errorf("Location = %q, want /assets/app-abc123.js", loc)
	}
}

// ---- Missing asset root (dev without a UI build) --------------------------
// UI paths 404 (the fallback finds no index.html); API/health unaffected.

func TestUI_MissingAssetRoot404sUIAndSparesAPI(t *testing.T) {
	s := newUIServer(t, filepath.Join(t.TempDir(), "no-such-dist"))

	for _, p := range []string{"/", "/movies", "/assets/app-abc123.js"} {
		rec := doUIRequest(t, s, http.MethodGet, p)
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s status = %d, want 404 (no asset root)", p, rec.Code)
		}
	}
	rec := doUIRequest(t, s, http.MethodGet, "/healthz")
	if rec.Code != http.StatusOK {
		t.Errorf("GET /healthz status = %d, want 200 (API/health unaffected)", rec.Code)
	}
	rec = doUIRequest(t, s, http.MethodGet, "/api/v1/does-not-exist")
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /api/v1/does-not-exist status = %d, want 404", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("GET /api/v1/does-not-exist Content-Type = %q, want application/json", ct)
	}
}
