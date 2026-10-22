// Package metrics is the observability layer (M7 #46). It owns the
// Prometheus registry and every mediarr_* metric, and exposes:
//
//   - a /metrics scrape handler (the API layer gates it behind auth),
//   - nil-safe recording helpers for the HTTP middleware, the four pipeline
//     services, and the indexer health tracker, and
//   - a scrape-time collector for DB-derived gauges (queue depth, library
//     sizes) and dependency liveness, so the app code never has to push
//     gauge values — a scrape is always current.
//
// Cardinality policy: every label is bounded. kind ∈ {movie, tv, music,
// book}; state ∈ the queue state enum; result ∈ {ok, error, skip, success,
// failure}; name is the configured indexer name; route is a Go 1.22 mux
// pattern (never a raw path with ids in it).
package metrics

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/brandenk514/mediarr/internal/health"
	"github.com/brandenk514/mediarr/internal/indexers"
)

// BuildInfo carries the ldflags-stamped build identity for
// mediarr_build_info.
type BuildInfo struct {
	Version   string
	Commit    string
	BuildTime string
}

// Options configures New. Registry may be nil (a fresh registry is built);
// DB and Checker are optional and enable the scrape-time collector pieces;
// Build fills mediarr_build_info.
type Options struct {
	Registry *prometheus.Registry
	DB       *sql.DB
	Checker  *health.Checker
	Build    BuildInfo
}

// Pipeline result label values.
const (
	ResultOK      = "ok"
	ResultError   = "error"
	ResultSkip    = "skip"
	ResultSuccess = "success"
	ResultFailure = "failure"
)

// HTTP route label fallback for requests the mux did not match.
const RouteUnmatched = "unmatched"

// requestBuckets spans a fast in-memory API (<10ms) to a pipeline run that
// waits on a download (minutes).
var requestBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300, 600}

// Metrics bundles every mediarr_* metric. Zero-value / nil *Metrics is safe:
// every recording method no-ops, so services can carry an optional
// *Metrics dep without nil checks at the call site.
type Metrics struct {
	reg *prometheus.Registry

	httpRequests *prometheus.CounterVec
	httpDuration *prometheus.HistogramVec

	pipelineRuns *prometheus.CounterVec
	imports      *prometheus.CounterVec
	pipelineDur  *prometheus.HistogramVec

	indexerCalls   *prometheus.CounterVec
	indexerLatency *prometheus.HistogramVec
	indexerHealthy *prometheus.GaugeVec

	buildInfo *prometheus.GaugeVec
}

// New builds a Metrics on a fresh (or supplied) registry and registers all
// collectors.
func New(opts Options) *Metrics {
	reg := opts.Registry
	if reg == nil {
		reg = prometheus.NewRegistry()
	}
	m := &Metrics{
		reg: reg,
		httpRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "mediarr", Subsystem: "http",
			Name: "requests_total",
			Help: "Total HTTP requests, partitioned by method and status code.",
		}, []string{"method", "status"}),
		httpDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "mediarr", Subsystem: "http",
			Name:    "request_duration_seconds",
			Help:    "HTTP request latency in seconds, partitioned by method and route pattern.",
			Buckets: requestBuckets,
		}, []string{"method", "route"}),
		pipelineRuns: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "mediarr", Subsystem: "pipeline",
			Name: "runs_total",
			Help: "Pipeline runs by media kind and outcome (ok, error, skip).",
		}, []string{"kind", "result"}),
		imports: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "mediarr", Subsystem: "pipeline",
			Name: "imports_total",
			Help: "Total media items imported through a pipeline run, by kind.",
		}, []string{"kind"}),
		pipelineDur: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "mediarr", Subsystem: "pipeline",
			Name:    "run_duration_seconds",
			Help:    "Full pipeline run latency (search → download → import) in seconds, by kind.",
			Buckets: requestBuckets,
		}, []string{"kind"}),
		indexerCalls: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "mediarr", Subsystem: "indexer",
			Name: "calls_total",
			Help: "Total indexer calls (searches and health probes), by indexer and outcome.",
		}, []string{"name", "result"}),
		indexerLatency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "mediarr", Subsystem: "indexer",
			Name:    "call_latency_seconds",
			Help:    "Indexer call latency in seconds, by indexer.",
			Buckets: requestBuckets,
		}, []string{"name"}),
		indexerHealthy: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "mediarr", Subsystem: "indexer",
			Name: "healthy",
			Help: "Indexer health: 1 if the most recent call succeeded, 0 if it failed or never succeeded.",
		}, []string{"name"}),
		buildInfo: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "mediarr",
			Name:      "build_info",
			Help:      "Build information (constant 1 per build).",
		}, []string{"version", "commit", "build_time"}),
	}

	reg.MustRegister(
		m.httpRequests, m.httpDuration,
		m.pipelineRuns, m.imports, m.pipelineDur,
		m.indexerCalls, m.indexerLatency, m.indexerHealthy,
		m.buildInfo,
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	if opts.DB != nil || opts.Checker != nil {
		reg.MustRegister(newScrapeCollector(opts.DB, opts.Checker))
	}
	if opts.Build != (BuildInfo{}) {
		m.buildInfo.WithLabelValues(opts.Build.Version, opts.Build.Commit, opts.Build.BuildTime).Set(1)
	}
	return m
}

// Handler returns the Prometheus text-format scrape handler.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{})
}

// ObserveHTTPRequest records one completed HTTP request. route should be the
// mux pattern (Server.MuxPattern) — never a raw path.
func (m *Metrics) ObserveHTTPRequest(method, route string, status int, d time.Duration) {
	if m == nil {
		return
	}
	m.httpRequests.WithLabelValues(method, strconv.Itoa(status)).Inc()
	if route == "" {
		route = RouteUnmatched
	}
	m.httpDuration.WithLabelValues(method, route).Observe(d.Seconds())
}

// RecordPipeline records one completed pipeline run: the outcome, its total
// duration, and how many media items it imported (0 for error/skip runs).
func (m *Metrics) RecordPipeline(kind, result string, d time.Duration, imported int) {
	if m == nil {
		return
	}
	m.pipelineRuns.WithLabelValues(kind, result).Inc()
	if imported > 0 {
		m.imports.WithLabelValues(kind).Add(float64(imported))
	}
	m.pipelineDur.WithLabelValues(kind).Observe(d.Seconds())
}

// RecordIndexerCall records one completed indexer call (search, on-demand
// test, or periodic health probe).
func (m *Metrics) RecordIndexerCall(name, result string, d time.Duration) {
	if m == nil {
		return
	}
	m.indexerCalls.WithLabelValues(name, result).Inc()
	m.indexerLatency.WithLabelValues(name).Observe(d.Seconds())
	if result == ResultSuccess {
		m.indexerHealthy.WithLabelValues(name).Set(1)
	} else {
		m.indexerHealthy.WithLabelValues(name).Set(0)
	}
}

// IndexerRecorder is a thin HealthTracker wrapper: it forwards every call to
// the inner tracker (which keeps the stats rollup the status API reads) and
// mirrors it into Prometheus.
type IndexerRecorder struct {
	inner indexers.HealthTracker
	m     *Metrics
}

// NewIndexerRecorder wraps tracker with Prometheus mirroring.
func NewIndexerRecorder(tracker indexers.HealthTracker, m *Metrics) *IndexerRecorder {
	return &IndexerRecorder{inner: tracker, m: m}
}

// Record implements indexers.HealthTracker.
func (r *IndexerRecorder) Record(name string, ok bool, latency time.Duration, err string) {
	if r == nil {
		return
	}
	if r.inner != nil {
		r.inner.Record(name, ok, latency, err)
	}
	result := ResultSuccess
	if !ok {
		result = ResultFailure
	}
	r.m.RecordIndexerCall(name, result, latency)
}

// Stats implements indexers.HealthTracker (forwards to the inner rollup).
func (r *IndexerRecorder) Stats(name string) indexers.IndexerStats {
	if r == nil || r.inner == nil {
		return indexers.IndexerStats{Name: name}
	}
	return r.inner.Stats(name)
}

// All implements indexers.HealthTracker.
func (r *IndexerRecorder) All() []indexers.IndexerStats {
	if r == nil || r.inner == nil {
		return nil
	}
	return r.inner.All()
}

// Reset implements indexers.HealthTracker.
func (r *IndexerRecorder) Reset(name string) {
	if r == nil || r.inner == nil {
		return
	}
	r.inner.Reset(name)
}

// ---- scrape-time collector ---------------------------------------------

// Scrape-time gauges: queue depth and library sizes are read straight from
// Postgres and dependency liveness is re-pinged during the scrape, so the
// endpoint never reports a stale pushed value.

const (
	queueDepthDesc = "mediarr_queue_depth"
	libraryDesc    = "mediarr_library_items"
	depUpDesc      = "mediarr_dependency_up"
)

// queueDepthSQL unions the four queue tables into (kind, state, n) rows.
const queueDepthSQL = `
SELECT kind, state, COUNT(*) AS n FROM (
    SELECT 'movie' AS kind, state FROM queue
    UNION ALL SELECT 'tv', state FROM tv_queue
    UNION ALL SELECT 'music', state FROM music_queue
    UNION ALL SELECT 'book', state FROM book_queue
) q GROUP BY kind, state`

// librarySQL counts the top-level media item of each library.
const librarySQL = `
SELECT 'movie' AS kind, COUNT(*) AS n FROM movies
UNION ALL SELECT 'tv', COUNT(*) FROM tv_series
UNION ALL SELECT 'music', COUNT(*) FROM music_artists
UNION ALL SELECT 'book', COUNT(*) FROM book_authors`

type scrapeCollector struct {
	db      *sql.DB
	checker *health.Checker
}

func newScrapeCollector(db *sql.DB, checker *health.Checker) *scrapeCollector {
	return &scrapeCollector{db: db, checker: checker}
}

// Describe implements prometheus.Collector.
func (c *scrapeCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- prometheus.NewDesc(queueDepthDesc, "Queue jobs by media kind and state (scraped from Postgres).", []string{"kind", "state"}, nil)
	ch <- prometheus.NewDesc(libraryDesc, "Top-level media items per library (scraped from Postgres).", []string{"kind"}, nil)
	if c.checker != nil {
		ch <- prometheus.NewDesc(depUpDesc, "External dependency liveness re-checked at scrape time: 1 up, 0 down.", []string{"name"}, nil)
	}
}

// Collect implements prometheus.Collector.
func (c *scrapeCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if c.db != nil {
		if rows, err := c.db.QueryContext(ctx, queueDepthSQL); err == nil {
			defer func() { _ = rows.Close() }()
			for rows.Next() {
				var kind, state string
				var n int64
				if rows.Scan(&kind, &state, &n) == nil {
					ch <- prometheus.MustNewConstMetric(
						prometheus.NewDesc(queueDepthDesc, "", []string{"kind", "state"}, nil),
						prometheus.GaugeValue, float64(n), kind, state)
				}
			}
		}
		if rows, err := c.db.QueryContext(ctx, librarySQL); err == nil {
			defer func() { _ = rows.Close() }()
			for rows.Next() {
				var kind string
				var n int64
				if rows.Scan(&kind, &n) == nil {
					ch <- prometheus.MustNewConstMetric(
						prometheus.NewDesc(libraryDesc, "", []string{"kind"}, nil),
						prometheus.GaugeValue, float64(n), kind)
				}
			}
		}
	}

	if c.checker != nil {
		rep := c.checker.Check(ctx)
		for _, chk := range rep.Checks {
			v := 0.0
			if chk.OK {
				v = 1
			}
			ch <- prometheus.MustNewConstMetric(
				prometheus.NewDesc(depUpDesc, "", []string{"name"}, nil),
				prometheus.GaugeValue, v, chk.Name)
		}
	}
}

// FormatName is a test/verification helper: the fully-qualified metric name
// for a subsystem+name pair (e.g. ("http", "requests_total")).
func FormatName(subsystem, name string) string {
	if subsystem == "" {
		return name
	}
	return fmt.Sprintf("mediarr_%s_%s", subsystem, name)
}
