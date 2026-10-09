package indexers

import (
	"context"
	"sort"
	"sync"
	"time"
)

// DefaultSearchTimeout is applied per-indexer when a Fanout is built without an
// explicit timeout. It bounds a single adapter's network call so one slow or
// wedged indexer cannot hold the whole search open; the others still return
// their results.
const DefaultSearchTimeout = 30 * time.Second

// Fanout searches N indexers concurrently and returns one merged, deduped,
// deterministically-ordered result set. It is the M5 (#30) replacement for the
// per-service sequential search loops: it adds (a) concurrency, (b) a
// per-indexer timeout, and (c) a stable merge/dedupe order, while preserving
// the long-standing rule that a single indexer failing does not fail the whole
// search.
//
// A Fanout is a thin, dependency-free combinator over []Searcher. Real adapters
// and the fake all satisfy Searcher, so a Fanout works identically over fakes
// (tests/dev) and over providers built from the Registry (production).
type Fanout struct {
	searchers []Searcher
	// timeout bounds each individual indexer's search. Zero means the parent
	// context is used as-is (no extra bound); NewFanout sets DefaultSearchTimeout.
	timeout time.Duration
	// rank, when non-nil, orders the merged results (a "less" function). It is
	// the single place the fan-out layer can express an ordering preference;
	// domains that need quality-based ordering inject it here. The default
	// (nil) preserves deterministic indexer-position order.
	rank func(a, b SearchResult) bool
}

// NewFanout builds a Fanout over the given searchers with the default
// per-indexer timeout. An empty/nil searcher list is legal and simply returns
// no results.
func NewFanout(searchers ...Searcher) *Fanout {
	return &Fanout{searchers: searchers, timeout: DefaultSearchTimeout}
}

// NewFanoutWithTimeout builds a Fanout with an explicit per-indexer timeout.
func NewFanoutWithTimeout(searchers []Searcher, timeout time.Duration) *Fanout {
	return &Fanout{searchers: searchers, timeout: timeout}
}

// WithRank returns a copy of the Fanout that orders merged results using rank
// (a "less" function). It is the seam for domain-specific ordering without
// changing the search itself.
func (f *Fanout) WithRank(rank func(a, b SearchResult) bool) *Fanout {
	c := *f
	c.rank = rank
	return &c
}

// Name is not meaningful for a fan-out of many indexers; it is provided only so
// a *Fanout can be embedded where a Searcher is expected. Callers should use
// Search directly rather than treat the fan-out as a named indexer.
func (f *Fanout) Name() string { return "fanout" }

// idxResult is the per-indexer contribution to a fan-out merge, tagged with the
// indexer's position so the merge can dedupe deterministically ("first indexer
// wins") regardless of which goroutine finished first.
type idxResult struct {
	position int
	results  []SearchResult
}

// Search fans out to every indexer concurrently, each bounded by the per-
// indexer timeout, then merges the surviving results.
//
// Semantics (deliberately matching the pre-M5 per-service behaviour):
//   - A single indexer returning an error (or timing out) does NOT fail the
//     search; its results are simply absent. Only the parent context being
//     done fails the whole call.
//   - Results are deduped by release Title. When two indexers return the same
//     title, the result from the lower-positioned indexer wins — deterministic,
//     and equivalent to the old "first indexer wins".
//   - The returned slice is deterministically ordered: by default in indexer
//     position, then by size descending, then by title; or by f.rank if set.
func (f *Fanout) Search(ctx context.Context, q SearchQuery) ([]SearchResult, error) {
	if len(f.searchers) == 0 {
		return nil, nil
	}

	// results[i] is indexed by the searcher's position so the merge can be
	// deterministic regardless of which goroutine finishes first.
	results := make([]idxResult, len(f.searchers))
	var wg sync.WaitGroup

	for i, s := range f.searchers {
		wg.Add(1)
		go func(pos int, s Searcher) {
			defer wg.Done()
			sctx := ctx
			var cancel context.CancelFunc
			if f.timeout > 0 {
				sctx, cancel = context.WithTimeout(ctx, f.timeout)
				defer cancel()
			}
			res, err := s.Search(sctx, q)
			if err != nil {
				// Tolerate per-indexer failure: record nothing for this indexer.
				return
			}
			results[pos] = idxResult{position: pos, results: res}
		}(i, s)
	}

	wg.Wait()
	if ctx.Err() != nil {
		// The whole search was cancelled/aborted: report it, don't return a
		// partial set as if it succeeded.
		return nil, ctx.Err()
	}

	merged := f.mergeAndDedupe(results)
	return merged, nil
}

// mergeAndDedupe combines per-indexer results into one deterministic, deduped
// slice. Dedupe key is release Title; on a collision the lower-positioned
// indexer wins.
func (f *Fanout) mergeAndDedupe(perIndexer []idxResult) []SearchResult {
	// Collect in position order so "first indexer wins" is well-defined.
	ordered := make([]idxResult, 0, len(perIndexer))
	for _, r := range perIndexer {
		if r.results != nil {
			ordered = append(ordered, r)
		}
	}
	// Sort by position to guarantee deterministic "first wins" semantics.
	for i := 1; i < len(ordered); i++ {
		for j := i; j > 0 && ordered[j-1].position > ordered[j].position; j-- {
			ordered[j-1], ordered[j] = ordered[j], ordered[j-1]
		}
	}

	seen := make(map[string]int) // title -> position of the winning indexer
	var out []SearchResult
	for _, r := range ordered {
		for _, res := range r.results {
			if res.Title == "" {
				continue
			}
			if p, ok := seen[res.Title]; ok && p <= r.position {
				continue // an equal-or-earlier indexer already claimed this title
			}
			seen[res.Title] = r.position
			out = append(out, res)
		}
	}

	f.order(out)
	return out
}

// order applies the rank function if set, otherwise a stable default ordering
// (size descending, then title) over the deduped set.
func (f *Fanout) order(out []SearchResult) {
	if len(out) <= 1 {
		return
	}
	if f.rank != nil {
		sort.Slice(out, func(i, j int) bool { return f.rank(out[i], out[j]) })
		return
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SizeBytes != out[j].SizeBytes {
			return out[i].SizeBytes > out[j].SizeBytes
		}
		return out[i].Title < out[j].Title
	})
}
