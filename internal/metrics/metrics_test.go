package metrics

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/brandenk514/mediarr/internal/health"
	"github.com/brandenk514/mediarr/internal/indexers"
)

// ---- fakes ---------------------------------------------------------------

type fakeStore struct{ pingErr error }

func (f *fakeStore) Migrate(ctx context.Context) error { return nil }
func (f *fakeStore) Ping(ctx context.Context) error    { return f.pingErr }
func (f *fakeStore) Version(ctx context.Context) (string, error) {
	return "0001", nil
}

type fakeCache struct{ pingErr error }

func (f *fakeCache) Get(ctx context.Context, key string) (string, bool) {
	return "", false
}
func (f *fakeCache) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	return nil
}
func (f *fakeCache) Delete(ctx context.Context, key string) error { return nil }
func (f *fakeCache) Ping(ctx context.Context) error               { return f.pingErr }

// gatherOne scrapes the metrics handler and returns the body.
func gatherBody(t *testing.T, m *Metrics) string {
	t.Helper()
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("scrape status = %d, want 200", rec.Code)
	}
	return rec.Body.String()
}

// ---- tests ---------------------------------------------------------------

func TestObserveHTTPRequest(t *testing.T) {
	m := New(Options{Registry: prometheus.NewRegistry()})
	m.ObserveHTTPRequest("GET", "GET /api/v1/movies", 200, 5*time.Millisecond)
	m.ObserveHTTPRequest("POST", "POST /api/v1/movies", 201, 50*time.Millisecond)
	m.ObserveHTTPRequest("GET", "GET /api/v1/movies", 404, 1*time.Millisecond)

	if got := testutil.ToFloat64(m.httpRequests.WithLabelValues("GET", "200")); got != 1 {
		t.Errorf("GET 200 count = %v, want 1", got)
	}
	if got := testutil.ToFloat64(m.httpRequests.WithLabelValues("POST", "201")); got != 1 {
		t.Errorf("POST 201 count = %v, want 1", got)
	}
	if got := testutil.ToFloat64(m.httpRequests.WithLabelValues("GET", "404")); got != 1 {
		t.Errorf("GET 404 count = %v, want 1", got)
	}
	body := gatherBody(t, m)
	if !strings.Contains(body, `mediarr_http_request_duration_seconds_count{method="GET",route="GET /api/v1/movies"} 2`) {
		t.Errorf("GET /api/v1/movies duration observations missing:\n%s", body)
	}
}

func TestObserveHTTPRequestUnmatchedRoute(t *testing.T) {
	m := New(Options{Registry: prometheus.NewRegistry()})
	m.ObserveHTTPRequest("GET", "", 404, time.Millisecond)
	body := gatherBody(t, m)
	if !strings.Contains(body, `mediarr_http_request_duration_seconds_count{method="GET",route="unmatched"} 1`) {
		t.Errorf("unmatched-route observations missing:\n%s", body)
	}
}

func TestRecordPipeline(t *testing.T) {
	m := New(Options{Registry: prometheus.NewRegistry()})
	m.RecordPipeline("movie", ResultOK, time.Second, 1)
	m.RecordPipeline("tv", ResultOK, 2*time.Second, 3)
	m.RecordPipeline("movie", ResultError, 300*time.Millisecond, 0)
	m.RecordPipeline("music", ResultSkip, 10*time.Millisecond, 0)

	if got := testutil.ToFloat64(m.pipelineRuns.WithLabelValues("movie", ResultOK)); got != 1 {
		t.Errorf("movie ok runs = %v, want 1", got)
	}
	if got := testutil.ToFloat64(m.pipelineRuns.WithLabelValues("tv", ResultOK)); got != 1 {
		t.Errorf("tv ok runs = %v, want 1", got)
	}
	if got := testutil.ToFloat64(m.pipelineRuns.WithLabelValues("movie", ResultError)); got != 1 {
		t.Errorf("movie error runs = %v, want 1", got)
	}
	if got := testutil.ToFloat64(m.pipelineRuns.WithLabelValues("music", ResultSkip)); got != 1 {
		t.Errorf("music skip runs = %v, want 1", got)
	}
	// imports: only ok runs with a positive count.
	if got := testutil.ToFloat64(m.imports.WithLabelValues("movie")); got != 1 {
		t.Errorf("movie imports = %v, want 1", got)
	}
	if got := testutil.ToFloat64(m.imports.WithLabelValues("tv")); got != 3 {
		t.Errorf("tv imports = %v, want 3", got)
	}
	if got := testutil.ToFloat64(m.imports.WithLabelValues("music")); got != 0 {
		t.Errorf("music imports = %v, want 0", got)
	}
	body := gatherBody(t, m)
	if !strings.Contains(body, `mediarr_pipeline_run_duration_seconds_count{kind="tv"} 1`) {
		t.Errorf("tv duration observations missing:\n%s", body)
	}
}

func TestRecordIndexerCallTogglesHealthy(t *testing.T) {
	m := New(Options{Registry: prometheus.NewRegistry()})
	m.RecordIndexerCall("fake", ResultSuccess, 12*time.Millisecond)
	if got := testutil.ToFloat64(m.indexerHealthy.WithLabelValues("fake")); got != 1 {
		t.Fatalf("healthy after success = %v, want 1", got)
	}
	m.RecordIndexerCall("fake", ResultFailure, 40*time.Millisecond)
	if got := testutil.ToFloat64(m.indexerHealthy.WithLabelValues("fake")); got != 0 {
		t.Fatalf("healthy after failure = %v, want 0", got)
	}
	if got := testutil.ToFloat64(m.indexerCalls.WithLabelValues("fake", ResultSuccess)); got != 1 {
		t.Errorf("success calls = %v, want 1", got)
	}
	if got := testutil.ToFloat64(m.indexerCalls.WithLabelValues("fake", ResultFailure)); got != 1 {
		t.Errorf("failure calls = %v, want 1", got)
	}
}

func TestIndexerRecorderMirrorsAndForwards(t *testing.T) {
	inner := indexers.NewHealthTracker()
	m := New(Options{Registry: prometheus.NewRegistry()})
	rec := NewIndexerRecorder(inner, m)

	rec.Record("fake", true, 10*time.Millisecond, "")
	rec.Record("fake", false, 90*time.Millisecond, "boom")

	// Inner rollup is intact (the status API reads it).
	st := rec.Stats("fake")
	if st.Calls != 2 || st.Successes != 1 || st.Failures != 1 {
		t.Errorf("stats = %+v, want Calls=2 Successes=1 Failures=1", st)
	}
	if st.LastError != "boom" {
		t.Errorf("LastError = %q, want %q", st.LastError, "boom")
	}
	if all := rec.All(); len(all) != 1 || all[0].Name != "fake" {
		t.Errorf("All() = %+v, want one entry named fake", all)
	}
	rec.Reset("fake")
	if st := rec.Stats("fake"); st.Calls != 0 {
		t.Errorf("stats after reset = %+v, want zero", st)
	}

	// Prometheus mirror saw both calls.
	if got := testutil.ToFloat64(m.indexerCalls.WithLabelValues("fake", ResultSuccess)); got != 1 {
		t.Errorf("mirrored success calls = %v, want 1", got)
	}
	if got := testutil.ToFloat64(m.indexerCalls.WithLabelValues("fake", ResultFailure)); got != 1 {
		t.Errorf("mirrored failure calls = %v, want 1", got)
	}
	// Last call was a failure, so the healthy gauge must be 0.
	if got := testutil.ToFloat64(m.indexerHealthy.WithLabelValues("fake")); got != 0 {
		t.Errorf("mirrored healthy = %v, want 0", got)
	}
}

func TestNilSafety(t *testing.T) {
	var m *Metrics
	m.ObserveHTTPRequest("GET", "r", 200, time.Millisecond)
	m.RecordPipeline("movie", ResultOK, time.Millisecond, 1)
	m.RecordIndexerCall("x", ResultSuccess, time.Millisecond)

	var r *IndexerRecorder
	r.Record("x", true, time.Millisecond, "")
	if _, _, _, ok := r.Stats("x"), r.All(), 0, true; !ok {
		t.Fatal("nil recorder methods panicked")
	}
	r.Reset("x")

	// A recorder wrapping a nil tracker must forward nothing without panicking.
	rec := NewIndexerRecorder(nil, m)
	rec.Record("x", true, time.Millisecond, "")
}

func TestHandlerServesBuildInfo(t *testing.T) {
	m := New(Options{
		Registry: prometheus.NewRegistry(),
		Build:    BuildInfo{Version: "1.2.3", Commit: "abc123", BuildTime: "2026-10-16T00:00:00Z"},
	})
	body := gatherBody(t, m)
	if !strings.Contains(body, "mediarr_build_info") {
		t.Error("scrape output missing mediarr_build_info")
	}
	if !strings.Contains(body, `version="1.2.3"`) {
		t.Error("scrape output missing stamped version label")
	}
	// Runtime collectors are registered too.
	if !strings.Contains(body, "go_goroutines") {
		t.Error("scrape output missing go runtime collector")
	}
}

func TestScrapeCollectorDependencyGauges(t *testing.T) {
	checker := health.NewChecker(&fakeStore{}, &fakeCache{pingErr: errors.New("redis down")})
	m := New(Options{Registry: prometheus.NewRegistry(), Checker: checker})
	body := gatherBody(t, m)

	if !strings.Contains(body, `mediarr_dependency_up{name="postgres"} 1`) {
		t.Errorf("scrape output missing postgres up=1:\n%s", body)
	}
	if !strings.Contains(body, `mediarr_dependency_up{name="redis"} 0`) {
		t.Errorf("scrape output missing redis up=0:\n%s", body)
	}
}

func TestScrapeCollectorWithoutCheckerOrDB(t *testing.T) {
	// DB nil + Checker nil: no scrape collector at all; scrape still works.
	m := New(Options{Registry: prometheus.NewRegistry()})
	body := gatherBody(t, m)
	if strings.Contains(body, "mediarr_dependency_up") {
		t.Error("dependency gauges should not exist without a checker")
	}
}

func TestFormatName(t *testing.T) {
	if got := FormatName("http", "requests_total"); got != "mediarr_http_requests_total" {
		t.Errorf("FormatName = %q", got)
	}
	if got := FormatName("", "build_info"); got != "build_info" {
		t.Errorf("FormatName empty subsystem = %q", got)
	}
}
