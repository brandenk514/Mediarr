package health

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// fakeStore is a stub store.Store for tests.
type fakeStore struct {
	pingErr error
}

func (f fakeStore) Migrate(ctx context.Context) error { return nil }
func (f fakeStore) Ping(ctx context.Context) error    { return f.pingErr }
func (f fakeStore) Version(ctx context.Context) (string, error) {
	return "0001", nil
}

// fakeCache is a stub store.Cache for tests.
type fakeCache struct{ pingErr error }

func (f fakeCache) Get(ctx context.Context, key string) (string, bool) { return "", false }
func (f fakeCache) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	return nil
}
func (f fakeCache) Delete(ctx context.Context, key string) error { return nil }
func (f fakeCache) Ping(ctx context.Context) error               { return f.pingErr }

func TestLiveness_Returns200WithVersion(t *testing.T) {
	h := NewHandlers(NewChecker(nil, nil))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	h.Liveness(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := body["version"]; !ok {
		t.Error("expected 'version' in liveness body")
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Errorf("content-type = %q", rec.Header().Get("Content-Type"))
	}
}

func TestReadiness_AllHealthy(t *testing.T) {
	h := NewHandlers(NewChecker(fakeStore{}, fakeCache{}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	h.Readiness(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var rep Report
	if err := json.NewDecoder(rec.Body).Decode(&rep); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if rep.Status != "ok" {
		t.Errorf("status = %q, want ok", rep.Status)
	}
	if len(rep.Checks) != 2 {
		t.Errorf("checks = %d, want 2", len(rep.Checks))
	}
	for _, c := range rep.Checks {
		if !c.OK {
			t.Errorf("check %q should be OK", c.Name)
		}
	}
}

func TestReadiness_PostgresDown(t *testing.T) {
	h := NewHandlers(NewChecker(
		fakeStore{pingErr: errTest("connection refused")},
		fakeCache{},
	))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	h.Readiness(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	var rep Report
	_ = json.NewDecoder(rec.Body).Decode(&rep)
	if rep.Status != "degraded" {
		t.Errorf("status = %q, want degraded", rep.Status)
	}
}

func TestReadiness_RedisDown(t *testing.T) {
	h := NewHandlers(NewChecker(
		fakeStore{},
		fakeCache{pingErr: errTest("timeout")},
	))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	h.Readiness(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestReadiness_BothDown(t *testing.T) {
	h := NewHandlers(NewChecker(
		fakeStore{pingErr: errTest("no")},
		fakeCache{pingErr: errTest("no")},
	))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	h.Readiness(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	var rep Report
	_ = json.NewDecoder(rec.Body).Decode(&rep)
	for _, c := range rep.Checks {
		if c.OK {
			t.Errorf("check %q should be failing", c.Name)
		}
	}
}

func TestInfoFunc_Defaults(t *testing.T) {
	info := InfoFunc()
	if info.Version == "" {
		t.Error("version should be non-empty")
	}
	if info.GoVersion == "" {
		t.Error("go version should be non-empty")
	}
}

func TestSetVersion_Overrides(t *testing.T) {
	SetVersion("1.2.3", "abc1234", "2026-01-01T00:00:00Z")
	info := InfoFunc()
	if info.Version != "1.2.3" {
		t.Errorf("version = %q, want 1.2.3", info.Version)
	}
	if info.Commit != "abc1234" {
		t.Errorf("commit = %q, want abc1234", info.Commit)
	}
}

type testError string

func (e testError) Error() string { return string(e) }

func errTest(s string) error { return testError(s) }
