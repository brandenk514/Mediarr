package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/brandenk514/mediarr/internal/auth"
	"github.com/brandenk514/mediarr/internal/health"
	"github.com/brandenk514/mediarr/internal/system"
)

// newSystemTestServer builds a fresh authenticated server (one per endpoint
// test, since mux double-registration panics) and a bearer token.
func newSystemTestServer(t *testing.T) (*Server, string) {
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
	return s, pair.Raw
}

func TestSystemHealth(t *testing.T) {
	s, token := newSystemTestServer(t)
	h := system.NewHealth([]system.Probe{
		{Name: "postgres", Ping: func(context.Context) error { return nil }},
		{Name: "redis", Ping: func(context.Context) error { return errors.New("nope") }},
	})
	s.SetSystem(&SystemDeps{Health: h})

	rec := doReq(t, s.Handler(), http.MethodGet, "/api/v1/system/health", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out system.SystemHealth
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Checks) != 2 || out.OK != 1 || out.Error != 1 {
		t.Fatalf("unexpected report: %+v", out)
	}
	// Unauthenticated is rejected.
	if rec2 := doReq(t, s.Handler(), http.MethodGet, "/api/v1/system/health", ""); rec2.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 unauth, got %d", rec2.Code)
	}
}

func TestSystemTasks(t *testing.T) {
	s, token := newSystemTestServer(t)
	reg := system.NewTaskRegistry()
	reg.Register(system.TaskSpec{Name: "indexer_health", Kind: system.TaskScheduled, Interval: time.Minute, Enabled: true, Run: func(context.Context) error { return nil }})
	reg.Register(system.TaskSpec{Name: "import_feed", Kind: system.TaskPlanned})
	s.SetSystem(&SystemDeps{Tasks: reg})

	rec := doReq(t, s.Handler(), http.MethodGet, "/api/v1/system/tasks", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out system.SystemTasks
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(out.Tasks))
	}
	if out.Scheduled != 1 {
		t.Fatalf("Scheduled=%d want 1", out.Scheduled)
	}
}

func TestSystemDisk(t *testing.T) {
	s, token := newSystemTestServer(t)
	media := t.TempDir()
	s.SetSystem(&SystemDeps{
		MediaRoot:    media,
		DownloadsDir: "",
		RootFolderPaths: func() []string {
			return []string{media}
		},
	})
	rec := doReq(t, s.Handler(), http.MethodGet, "/api/v1/system/disk", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out system.SystemDisk
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Roots) != 1 {
		t.Fatalf("expected 1 deduped root, got %d: %+v", len(out.Roots), out.Roots)
	}
	if !out.Roots[0].Exists || out.Roots[0].TotalBytes <= 0 {
		t.Fatalf("root should exist with usage: %+v", out.Roots[0])
	}
	if out.MediaRoot != media {
		t.Fatalf("media_root=%q", out.MediaRoot)
	}
}

func TestSystemAbout(t *testing.T) {
	s, token := newSystemTestServer(t)
	start := time.Now().Add(-2 * time.Hour)
	s.SetSystem(&SystemDeps{About: system.AboutDeps{
		Version: "v0.6.0", Commit: "deadbeef", BuildTime: "2026-10-25T00:00:00Z",
		Mode: "development", StartedAt: start,
	}})
	rec := doReq(t, s.Handler(), http.MethodGet, "/api/v1/system/about", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out system.About
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Version != "v0.6.0" || out.Commit != "deadbeef" || out.Mode != "development" {
		t.Fatalf("about wrong: %+v", out)
	}
	if out.UptimeSeconds < 7100 || out.UptimeSeconds > 7300 {
		t.Fatalf("uptime_seconds=%d (want ~7200)", out.UptimeSeconds)
	}
}

func TestSystemNotWiredIs501(t *testing.T) {
	// A server with no SetSystem -> 501 on every system route is not
	// registered, so this asserts the 404 from the mux (routes only exist
	// once SetSystem is called). We instead assert the per-field nil guards by
	// wiring SetSystem with empty deps.
	s, token := newSystemTestServer(t)
	s.SetSystem(&SystemDeps{}) // wired but every field nil
	for _, path := range []string{
		"/api/v1/system/health",
		"/api/v1/system/tasks",
	} {
		rec := doReq(t, s.Handler(), http.MethodGet, path, token)
		if rec.Code != http.StatusNotImplemented {
			t.Fatalf("%s: expected 501, got %d", path, rec.Code)
		}
	}
	// disk/about still work with empty deps (pure functions).
	if rec := doReq(t, s.Handler(), http.MethodGet, "/api/v1/system/disk", token); rec.Code != http.StatusOK {
		t.Fatalf("disk expected 200, got %d", rec.Code)
	}
	if rec := doReq(t, s.Handler(), http.MethodGet, "/api/v1/system/about", token); rec.Code != http.StatusOK {
		t.Fatalf("about expected 200, got %d", rec.Code)
	}
}
