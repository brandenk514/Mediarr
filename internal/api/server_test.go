package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/brandenk514/mediarr/internal/health"
)

// newTestServer builds a Server with a healthy checker.
func newTestServer() *Server {
	return NewServer(health.NewChecker(nil, nil))
}

func TestRoutesRegistered(t *testing.T) {
	s := newTestServer()
	for _, path := range []string{"/healthz", "/readyz", "/version"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		// healthz/readyz/version should all return 200 with a nil store/cache.
		// (With nil deps, readiness will be degraded but the route must exist.)
		if rec.Code == http.StatusNotFound {
			t.Errorf("route %s not registered (404)", path)
		}
	}
}

func TestHealthz_200(t *testing.T) {
	s := newTestServer()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if _, ok := body["version"]; !ok {
		t.Error("version missing from body")
	}
}

func TestVersion_200(t *testing.T) {
	s := newTestServer()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/version", nil)
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestRequestID_HeaderSet(t *testing.T) {
	s := newTestServer()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	s.Handler().ServeHTTP(rec, req)
	if rec.Header().Get("X-Request-Id") == "" {
		t.Error("X-Request-Id header should be set")
	}
}

func TestRequestID_EchoedIfInbound(t *testing.T) {
	s := newTestServer()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("X-Request-Id", "trace-abc123")
	s.Handler().ServeHTTP(rec, req)
	if got := rec.Header().Get("X-Request-Id"); got != "trace-abc123" {
		t.Errorf("X-Request-Id = %q, want echoed trace-abc123", got)
	}
}

func TestRequestID_RejectsGarbage(t *testing.T) {
	s := newTestServer()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("X-Request-Id", "bad;id with spaces&!@#")
	s.Handler().ServeHTTP(rec, req)
	got := rec.Header().Get("X-Request-Id")
	if got == "bad;id with spaces&!@#" {
		t.Error("garbage request ID should be replaced")
	}
}

func TestRecoverPanic_Returns500(t *testing.T) {
	h := recoverPanic(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestNewRequestID_Format(t *testing.T) {
	id := newRequestID()
	if len(id) != 16 {
		t.Errorf("request ID length = %d, want 16", len(id))
	}
	for _, c := range id {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'z')) {
			t.Errorf("unexpected char %q in request ID %q", c, id)
		}
	}
}

func TestIsValidID(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"trace-abc123", true},
		{"abc", false}, // too short
		{"a", false},
		{"", false},
		{"bad;id", false},
		{"good_id.value", true},
		{string(make([]byte, 200)), false}, // too long
	}
	for _, c := range cases {
		if got := isValidID(c.in); got != c.want {
			t.Errorf("isValidID(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// Ensure the server can be built and the handler chain works end-to-end.
func TestServe_Smoke(t *testing.T) {
	s := newTestServer()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- s.Serve(ctx, "127.0.0.1:0", 1*time.Second, 1*time.Second, time.Second)
	}()

	// Wait for server to be ready or fail.
	select {
	case err := <-done:
		t.Fatalf("server exited early: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	cancel()
	// Drain shutdown.
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("server did not shut down in time")
	}
}
