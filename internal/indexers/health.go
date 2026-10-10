package indexers

import (
	"sort"
	"sync"
	"time"
)

// IndexerStats is the per-indexer health rollup (#34: "per-indexer stats:
// success/fail counts, last success, avg latency"). It is what the status API
// (#35) reports and what the health worker (#34) maintains.
type IndexerStats struct {
	// Name is the indexer name (Definition.Name).
	Name string
	// Successes is the running count of successful searches/tests.
	Successes int64
	// Failures is the running count of failed searches/tests.
	Failures int64
	// LastSuccess is when the most recent success happened, if ever.
	LastSuccess *time.Time
	// LastError is the most recent failure reason (credential-free), if any.
	LastError string
	// LastLatency is the most recent observed latency.
	LastLatency time.Duration
	// AvgLatency is the running average latency across all observed calls.
	AvgLatency time.Duration
	// Calls is the total number of observed calls (Successes + Failures).
	Calls int64
	// TotalLatencyNs is the accumulated latency; with Calls it is the source of
	// AvgLatency. Exposed so a reporter can compute the average precisely.
	TotalLatencyNs int64
}

// HealthTracker records the outcome of indexer searches/tests and answers
// per-indexer stats queries. It is the seam between the adapters (which record
// each call) and the health worker + status API (which read the rollup).
//
// The in-memory implementation (NewHealthTracker) is the default; a
// persistence-backed one could be swapped in without touching adapters.
type HealthTracker interface {
	// Record observes a completed call. ok is whether it succeeded;
	// latency is how long it took; err is a credential-free reason string on
	// failure (empty on success).
	Record(name string, ok bool, latency time.Duration, err string)
	// Stats returns the rollup for one indexer, or zero values if unknown.
	Stats(name string) IndexerStats
	// All returns stats for every known indexer, sorted by name.
	All() []IndexerStats
	// Reset clears the rollup for one indexer (e.g. after a config change or a
	// fresh on-demand test).
	Reset(name string)
}

// tracker is the in-memory HealthTracker.
type tracker struct {
	mu    sync.Mutex
	stats map[string]*tEntry
}

type tEntry struct {
	stats        IndexerStats
	latencySumNs int64
}

// NewHealthTracker builds an in-memory HealthTracker.
func NewHealthTracker() HealthTracker {
	return &tracker{stats: map[string]*tEntry{}}
}

func (t *tracker) Record(name string, ok bool, latency time.Duration, err string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	e, exists := t.stats[name]
	if !exists {
		e = &tEntry{stats: IndexerStats{Name: name}}
		t.stats[name] = e
	}
	e.stats.Calls++
	e.stats.LastLatency = latency
	if ok {
		e.stats.Successes++
		now := time.Now().UTC()
		e.stats.LastSuccess = &now
		e.stats.LastError = ""
	} else {
		e.stats.Failures++
		e.stats.LastError = err
	}
	// Accumulate first, then publish the rollup: TotalLatencyNs and AvgLatency
	// must both reflect the call just recorded, not the one before it.
	e.latencySumNs += int64(latency)
	e.stats.TotalLatencyNs = e.latencySumNs
	e.stats.AvgLatency = t.avg(e)
}

func (t *tracker) avg(e *tEntry) time.Duration {
	if e.stats.Calls == 0 {
		return 0
	}
	return time.Duration(e.latencySumNs / e.stats.Calls)
}

func (t *tracker) Stats(name string) IndexerStats {
	t.mu.Lock()
	defer t.mu.Unlock()
	if e, ok := t.stats[name]; ok {
		s := e.stats
		return s
	}
	return IndexerStats{Name: name}
}

func (t *tracker) All() []IndexerStats {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]IndexerStats, 0, len(t.stats))
	for _, e := range t.stats {
		out = append(out, e.stats)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (t *tracker) Reset(name string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.stats, name)
}
