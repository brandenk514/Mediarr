package indexers

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// TestResult is the outcome of probing a single indexer. It is returned by the
// health worker (#34) and surfaced by the on-demand test endpoint (#35).
//
// It deliberately carries only the definition's identity (id/name/kind/url) —
// NEVER the APIKey. A probe result is a loggable, API-serialisable value, and
// "keys never returned, only masked presence" (#35) is enforced by making the
// key unrepresentable here rather than by every caller remembering to strip it.
type TestResult struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	BaseURL string `json:"base_url"`
	Enabled bool   `json:"enabled"`
	// OK is true when Test() returned nil.
	OK bool `json:"ok"`
	// Error is the credential-free failure reason ("" on success). It is
	// already redacted by the adapter's error path (redact.go).
	Error string `json:"error,omitempty"`
	// Duration is how long the probe took.
	Duration time.Duration `json:"duration"`
	// At is when the probe completed (UTC).
	At time.Time `json:"at"`
}

// HealthWorker periodically probes every enabled indexer and, on demand,
// probes a single one (#34: "health worker runs on schedule + on demand;
// failures recorded per indexer"). It composes the three seams this package
// already owns — the definition store (IndexerRepo), the provider factory
// (Registry), and the per-indexer rollup (HealthTracker) — so a probe is the
// same live request the search path makes, and its outcome lands in the same
// tracker the stats endpoint reads.
//
// The worker materialises a Provider per probe through the registry with the
// shared AdapterDeps (HTTP client + tracker), so a failed periodic test shows
// up in the stats alongside a failed search — the "error tracking" in PLAN §7.
type HealthWorker struct {
	repo     IndexerRepo
	registry *Registry
	deps     AdapterDeps
	interval time.Duration
	log      *slog.Logger
}

// NewHealthWorker builds a worker. interval <= 0 disables the periodic loop
// (Run performs a single sweep and returns); the on-demand TestOne always
// works. deps is the shared AdapterDeps handed to every real adapter so the
// probe and the live search share one tracker and one HTTP client.
func NewHealthWorker(repo IndexerRepo, registry *Registry, deps AdapterDeps, interval time.Duration, log *slog.Logger) *HealthWorker {
	if log == nil {
		log = slog.Default()
	}
	return &HealthWorker{
		repo:     repo,
		registry: registry,
		deps:     deps,
		interval: interval,
		log:      log,
	}
}

// Run sweeps every enabled indexer immediately, then on each tick, until ctx
// is cancelled. A single indexer failing never stops the loop. A non-positive
// interval performs the initial sweep and returns (boot-time "check now").
func (w *HealthWorker) Run(ctx context.Context) error {
	if err := w.TestAll(ctx); err != nil {
		w.log.Warn("indexer health sweep failed", "error", err)
	}
	if w.interval <= 0 {
		return nil
	}
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := w.TestAll(ctx); err != nil {
				w.log.Warn("indexer health sweep failed", "error", err)
			}
		}
	}
}

// TestAll probes every enabled indexer concurrently and records each outcome.
// It returns an error only on a hard sweep failure (the definition list could
// not be loaded); per-indexer failures are captured in the rollup, not
// propagated — one bad indexer must not take the sweep down.
func (w *HealthWorker) TestAll(ctx context.Context) error {
	defs, err := w.repo.ListEnabled(ctx)
	if err != nil {
		return err
	}
	results := make([]TestResult, len(defs))
	var wg sync.WaitGroup
	for i, def := range defs {
		wg.Add(1)
		go func(i int, def Definition) {
			defer wg.Done()
			results[i] = w.probe(ctx, def)
		}(i, def)
	}
	wg.Wait()
	for _, r := range results {
		w.log.Debug("indexer health probe", "name", r.Name, "ok", r.OK, "duration", r.Duration)
	}
	return nil
}

// TestOne probes a single indexer by id. It loads the definition regardless of
// its enabled flag so a disabled indexer can still be diagnosed on demand
// (#35's POST /indexers/{id}/test). It records the outcome like a sweep does.
func (w *HealthWorker) TestOne(ctx context.Context, id int64) (TestResult, error) {
	def, err := w.repo.Get(ctx, id)
	if err != nil {
		return TestResult{}, err
	}
	return w.probe(ctx, *def), nil
}

// probe materialises a Provider for def, runs its health probe, records the
// outcome to the shared tracker, and persists last_test. Every failure mode
// (unbuildable kind, transport error, non-2xx) is captured in the TestResult
// rather than propagated: a bad indexer degrades to a reported failure, never
// a broken endpoint or a dead sweep.
func (w *HealthWorker) probe(ctx context.Context, def Definition) TestResult {
	start := time.Now()
	res := TestResult{
		ID:      def.ID,
		Name:    def.Name,
		Kind:    def.Kind,
		BaseURL: def.BaseURL,
		Enabled: def.Enabled,
		At:      time.Now().UTC(),
	}

	deps := w.deps
	if p, err := w.registry.Build(def); err != nil {
		// A definition that cannot be built still belongs in the rollup: record
		// it as a failure so the stats reflect the configured set.
		res.Error = "build provider: " + redactURL(err.Error())
		deps.health().Record(def.Name, false, time.Since(start), res.Error)
		return res
	} else {
		// Bound the probe so one hung indexer cannot stall a concurrent sweep.
		pctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if err := p.Test(pctx); err != nil {
			res.Error = redactURL(err.Error())
		} else {
			res.OK = true
		}
		deps.health().Record(def.Name, res.OK, time.Since(start), res.Error)
	}

	// Persist the probe timestamp (best-effort: a DB hiccup must not mask the
	// health outcome, which is already in the tracker).
	_ = w.repo.SetLastTest(ctx, def.ID, &res.At)
	res.Duration = time.Since(start)
	return res
}
