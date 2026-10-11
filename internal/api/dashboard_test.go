package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/brandenk514/mediarr/internal/auth"
	"github.com/brandenk514/mediarr/internal/dashboard"
	"github.com/brandenk514/mediarr/internal/health"
	"github.com/brandenk514/mediarr/internal/indexers"
)

// fakeDashRepo is an in-memory dashboard.Repo for the API contract test: it
// returns a fixed snapshot and records the maxRecent the caller requested.
type fakeDashRepo struct {
	snap   *dashboard.Snapshot
	err    error
	gotN   int
	sawAny bool
}

func (f *fakeDashRepo) Snapshot(ctx context.Context, maxRecent int) (*dashboard.Snapshot, error) {
	f.sawAny = true
	f.gotN = maxRecent
	if f.err != nil {
		return nil, f.err
	}
	if f.snap == nil {
		return &dashboard.Snapshot{GeneratedAt: time.Now().UTC()}, nil
	}
	return f.snap, nil
}

// newDashboardTestServer builds an authenticated server with the dashboard
// endpoint wired to a fixed snapshot and a health tracker with two indexers:
// one healthy (ok) and one broken (error), plus one never-observed (idle).
func newDashboardTestServer(t *testing.T) (*Server, *fakeDashRepo, string) {
	t.Helper()

	tracker := indexers.NewHealthTracker()
	// "nyaa": a success -> ok.
	tracker.Record("nyaa", true, 12*time.Millisecond, "")
	// "broken": a failure and no success -> error.
	tracker.Record("broken", false, 300*time.Millisecond, "connection refused")
	// "idle": never observed.

	snap := &dashboard.Snapshot{
		Libraries: []dashboard.Library{
			{Kind: dashboard.KindMovie, Label: "Movies", Count: 2, Wants: 1, Queue: 1},
			{Kind: dashboard.KindTV, Label: "TV", Count: 1, Wants: 2, Queue: 1},
			{Kind: dashboard.KindMusic, Label: "Music", Count: 1, Wants: 1, Queue: 0},
			{Kind: dashboard.KindBook, Label: "Books", Count: 1, Wants: 1, Queue: 0},
		},
		Totals: dashboard.Totals{Libraries: 5, Wants: 5, Queue: 2},
		RecentHistory: []dashboard.HistoryItem{
			{Kind: dashboard.KindMovie, Name: "Inception", Event: "imported", Detail: "to /media/movies/Inception (2010)/Inception.mkv", At: time.Now().UTC()},
		},
		RecentQueue: []dashboard.QueueItem{
			{Kind: dashboard.KindMovie, ReleaseTitle: "Inception.2010.1080p.WEB.x264", Indexer: "nyaa", State: "downloading", Progress: 42, UpdatedAt: time.Now().UTC()},
		},
		GeneratedAt: time.Now().UTC(),
	}
	repo := &fakeDashRepo{snap: snap}

	store := newFakeStore()
	u, _ := store.CreateUser(context.Background(), "admin", "", "admin", "hash")
	pair, err := auth.NewToken("ma_live_")
	if err != nil {
		t.Fatalf("new token: %v", err)
	}
	_, _ = store.CreateToken(context.Background(), u, "test", pair, nil)

	s := NewServer(health.NewChecker(nil, nil))
	s.SetAuth(&AuthDeps{Repo: store})
	s.SetDashboard(&DashboardDeps{Repo: repo, Tracker: tracker, DefaultRecent: 10})
	return s, repo, pair.Raw
}

// TestDashboard_RequiresAuth is the #40 "auth required" contract: an
// unauthenticated dashboard request is 401.
func TestDashboard_RequiresAuth(t *testing.T) {
	s, _, _ := newDashboardTestServer(t)
	rec := doReq(t, s.Handler(), http.MethodGet, "/api/v1/dashboard", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token: got %d, want 401", rec.Code)
	}
}

// TestDashboard_Snapshot is the #40 "library overview + recent history +
// indexer health at a glance" contract: the endpoint returns the rollup, the
// totals, the bounded feeds, and the tri-state indexer card, with no indexer
// secrets leaking.
func TestDashboard_Snapshot(t *testing.T) {
	s, repo, token := newDashboardTestServer(t)

	rec := doReq(t, s.Handler(), http.MethodGet, "/api/v1/dashboard?recent=7", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	// The default-recent path forwarded the clamped ?recent to the repo.
	if !repo.sawAny {
		t.Fatal("repo.Snapshot was not called")
	}
	if repo.gotN != 7 {
		t.Errorf("maxRecent = %d, want 7 (from ?recent=7)", repo.gotN)
	}

	var resp dashboardResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v (body %s)", err, rec.Body.String())
	}

	if len(resp.Libraries) != 4 {
		t.Errorf("libraries = %d, want 4", len(resp.Libraries))
	}
	if resp.Totals.Libraries != 5 || resp.Totals.Wants != 5 || resp.Totals.Queue != 2 {
		t.Errorf("totals = %+v, want {5 5 2}", resp.Totals)
	}
	if len(resp.RecentHistory) != 1 || resp.RecentHistory[0].Name != "Inception" {
		t.Errorf("recent_history = %+v", resp.RecentHistory)
	}
	if len(resp.RecentQueue) != 1 || resp.RecentQueue[0].State != "downloading" {
		t.Errorf("recent_queue = %+v", resp.RecentQueue)
	}
	if resp.GeneratedAt.IsZero() {
		t.Error("generated_at is zero")
	}

	// Indexer health card: tri-state present for all three seeded indexers.
	statuses := map[string]string{}
	for _, ix := range resp.Indexers {
		statuses[ix.Name] = ix.Status
	}
	if statuses["nyaa"] != "ok" {
		t.Errorf("nyaa status = %q, want ok", statuses["nyaa"])
	}
	if statuses["broken"] != "error" {
		t.Errorf("broken status = %q, want error", statuses["broken"])
	}
	// The broken indexer carries its credential-free error string.
	for _, ix := range resp.Indexers {
		if ix.Name == "broken" && ix.LastError != "connection refused" {
			t.Errorf("broken last_error = %q, want %q", ix.LastError, "connection refused")
		}
	}
}

// TestDashboard_RecentClamp is the #40 "bounded recent feed" contract at the
// HTTP layer: ?recent is clamped into [1,100]; non-numeric and out-of-range
// values fall back to the default rather than erroring.
func TestDashboard_RecentClamp(t *testing.T) {
	cases := []struct {
		query string
		wantN int
	}{
		{"", 10},              // no param -> default
		{"?recent=1", 1},      // lower bound
		{"?recent=100", 100},  // upper bound
		{"?recent=0", 10},     // <= 0 -> default
		{"?recent=5000", 100}, // > max -> clamp to max
		{"?recent=abc", 10},   // non-numeric -> default
	}
	for _, tc := range cases {
		s, repo, token := newDashboardTestServer(t)
		rec := doReq(t, s.Handler(), http.MethodGet, "/api/v1/dashboard"+tc.query, token)
		if rec.Code != http.StatusOK {
			t.Fatalf("query %q: status %d, want 200", tc.query, rec.Code)
		}
		if repo.gotN != tc.wantN {
			t.Errorf("query %q: maxRecent = %d, want %d", tc.query, repo.gotN, tc.wantN)
		}
	}
}

// TestDashboard_RepoError is the #40 "error state" contract: a repository
// failure surfaces as a 500 with an opaque error (no internal detail leaked).
func TestDashboard_RepoError(t *testing.T) {
	s, repo, token := newDashboardTestServer(t)
	repo.err = context.DeadlineExceeded
	rec := doReq(t, s.Handler(), http.MethodGet, "/api/v1/dashboard", token)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("repo error: status %d, want 500 (body %s)", rec.Code, rec.Body.String())
	}
	var errResp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if errResp["error"] == "" {
		t.Error("error body has no 'error' field")
	}
}
