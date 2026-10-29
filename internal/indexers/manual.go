package indexers

import (
	"context"
	"sync"
	"time"
)

// ManualResult is the outcome of one indexer's manual search attempt. Unlike a
// Fanout result (which drops failures), ManualResult always carries the
// indexer's Name and the attempt's error/duration, so a caller can write one
// history row per attempted indexer — including indexers that failed to build
// or to search.
type ManualResult struct {
	// Name is the indexer's Name(). It falls back to the definition's name when
	// the provider could not be built.
	Name string
	// Results is the indexer's releases; nil on failure.
	Results []SearchResult
	// Err is nil on success, or the build/search error otherwise.
	Err error
	// Elapsed is how long this attempt took (build + search).
	Elapsed time.Duration
}

// SearchAll runs a manual search over the given definitions concurrently, each
// with an independent per-indexer timeout, and returns one ManualResult per
// definition (in input order).
//
// It is the manual-search workhorse (POST /api/v1/search). Unlike Fanout, it
// surfaces per-indexer failures instead of swallowing them, so the caller can
// record a history row per attempted indexer. The parent ctx drives
// cancellation: cancelling it aborts every in-flight attempt and the results
// carry the context error (the caller checks ctx.Err() to fail the whole call).
func SearchAll(ctx context.Context, reg *Registry, defs []Definition, q SearchQuery, timeout time.Duration) []ManualResult {
	if len(defs) == 0 {
		return nil
	}
	if timeout <= 0 {
		timeout = DefaultSearchTimeout
	}
	out := make([]ManualResult, len(defs))
	var wg sync.WaitGroup
	for i, def := range defs {
		wg.Add(1)
		go func(i int, def Definition) {
			defer wg.Done()
			res := runManualSearch(ctx, reg, def, q, timeout)
			out[i] = res
		}(i, def)
	}
	wg.Wait()
	return out
}

// runManualSearch builds one provider from its definition and runs the search
// under a per-indexer timeout, returning a ManualResult. A cancelled parent
// ctx makes the per-indexer timeout context cancel too, so the search fails
// with the context error.
func runManualSearch(ctx context.Context, reg *Registry, def Definition, q SearchQuery, timeout time.Duration) ManualResult {
	start := time.Now()
	res := ManualResult{Name: def.Name, Elapsed: time.Since(start)}

	prov, err := reg.Build(def)
	if err != nil {
		res.Err = err
		res.Elapsed = time.Since(start)
		return res
	}
	res.Name = prov.Name()

	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	results, err := prov.Search(cctx, q)
	res.Elapsed = time.Since(start)
	if err != nil {
		res.Err = err
		return res
	}
	res.Results = results
	return res
}
