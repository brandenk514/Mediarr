// Package health implements liveness and readiness probes plus version
// reporting. The process is "alive" as long as it can serve HTTP; it is
// "ready" only when it can also reach Postgres and Redis.
package health

import (
	"context"
	"encoding/json"
	"net/http"
	"runtime"
	"time"

	"github.com/brandenk514/mediarr/internal/store"
)

// Build-time stamped values (see -ldflags in Makefile / Dockerfile).
var (
	version   = "dev"
	commit    = "none"
	buildTime = "unknown"
)

// SetVersion is called from main (or tests) to override stamped values.
func SetVersion(v, c, b string) {
	if v != "" {
		version = v
	}
	if c != "" {
		commit = c
	}
	if b != "" {
		buildTime = b
	}
}

// Info describes the running build.
type Info struct {
	Version   string    `json:"version"`
	Commit    string    `json:"commit"`
	BuildTime string    `json:"build_time"`
	GoVersion string    `json:"go_version"`
	StartedAt time.Time `json:"started_at"`
}

// InfoFunc returns the current build info.
func InfoFunc() Info {
	return Info{
		Version:   version,
		Commit:    commit,
		BuildTime: buildTime,
		GoVersion: runtime.Version(),
	}
}

// Checker performs readiness checks against external dependencies.
type Checker struct {
	store store.Store
	cache store.Cache
	// timeout bounds each dependency ping so a hung DB cannot stall readiness.
	timeout time.Duration
}

// NewChecker builds a Checker with the given dependencies. A 2s default
// timeout keeps readiness responsive even when a dependency is wedged.
func NewChecker(st store.Store, cache store.Cache) *Checker {
	return &Checker{store: st, cache: cache, timeout: 2 * time.Second}
}

// Status is the readiness report for a single dependency.
type Status struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Latency string `json:"latency,omitempty"`
	Error   string `json:"error,omitempty"`
}

// Report is the full readiness body.
type Report struct {
	Status  string   `json:"status"` // "ok" | "degraded"
	Checks  []Status `json:"checks"`
	Version string   `json:"version"`
}

// Check pings each dependency and returns a readiness report.
func (c *Checker) Check(ctx context.Context) Report {
	rep := Report{Status: "ok", Version: version}
	for _, chk := range []struct {
		name string
		fn   func(context.Context) error
	}{
		{"postgres", c.pingStore},
		{"redis", c.pingCache},
	} {
		st := Status{Name: chk.name, OK: true}
		start := time.Now()
		err := withTimeout(ctx, c.timeout, chk.fn)
		st.Latency = time.Since(start).Round(time.Microsecond).String()
		if err != nil {
			st.OK = false
			st.Error = err.Error()
			rep.Status = "degraded"
		}
		rep.Checks = append(rep.Checks, st)
	}
	return rep
}

func (c *Checker) pingStore(ctx context.Context) error {
	if c.store == nil {
		return errUnavailable("postgres not configured")
	}
	return c.store.Ping(ctx)
}

func (c *Checker) pingCache(ctx context.Context) error {
	if c.cache == nil {
		return errUnavailable("redis not configured")
	}
	return c.cache.Ping(ctx)
}

type unavailableError string

func (u unavailableError) Error() string { return string(u) }

func errUnavailable(msg string) error { return unavailableError(msg) }

func withTimeout(ctx context.Context, d time.Duration, fn func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	return fn(ctx)
}

// Handlers wires the health endpoints onto a mux.
type Handlers struct {
	ready *Checker
}

// NewHandlers builds health Handlers.
func NewHandlers(ready *Checker) *Handlers {
	return &Handlers{ready: ready}
}

// Liveness reports whether the process is alive. Always 200 when the handler
// is reachable; the body carries version info for debugging.
func (h *Handlers) Liveness(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(InfoFunc())
}

// Readiness reports dependency health. 200 when all checks pass, 503 when
// degraded. Orchestration platforms key off this.
func (h *Handlers) Readiness(w http.ResponseWriter, r *http.Request) {
	rep := h.ready.Check(r.Context())
	w.Header().Set("Content-Type", "application/json")
	status := http.StatusOK
	if rep.Status != "ok" {
		status = http.StatusServiceUnavailable
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(rep)
}
