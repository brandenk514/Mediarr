package indexers

import (
	"context"
	"errors"
	"testing"
	"time"
)

// memCache is an in-memory store.Cache for cache unit tests (no Redis needed).
type memCache struct {
	data map[string]string
	// failRead / failWrite simulate a Redis outage to test failure isolation.
	failRead  bool
	failWrite bool
}

func newMemCache() *memCache { return &memCache{data: map[string]string{}} }

func (c *memCache) Get(ctx context.Context, key string) (string, bool) {
	if c.failRead {
		return "", false
	}
	v, ok := c.data[key]
	return v, ok
}
func (c *memCache) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	if c.failWrite {
		return errors.New("redis down")
	}
	c.data[key] = value
	return nil
}
func (c *memCache) Delete(ctx context.Context, key string) error {
	delete(c.data, key)
	return nil
}
func (c *memCache) Ping(ctx context.Context) error { return nil }

func TestCacheSearcher_MissThenHit(t *testing.T) {
	calls := 0
	inner := &countingSearcher{name: "ix", fn: func() []SearchResult {
		calls++
		return []SearchResult{{Title: "R", SizeBytes: 1, Indexer: "ix"}}
	}}
	cache := newMemCache()
	cs := NewCacheSearcher(inner, cache, time.Minute)
	q := SearchQuery{Term: "inception", Year: 2010}

	// First search: a miss. The inner indexer is consulted and the result is
	// stored.
	r1, err := cs.Search(context.Background(), q)
	if err != nil {
		t.Fatalf("first search: %v", err)
	}
	if len(r1) != 1 || r1[0].Title != "R" {
		t.Fatalf("first search results = %v", r1)
	}
	if calls != 1 {
		t.Fatalf("after first search calls = %d, want 1 (miss)", calls)
	}

	// Second search, same query: a hit. The inner indexer is NOT consulted.
	r2, err := cs.Search(context.Background(), q)
	if err != nil {
		t.Fatalf("second search: %v", err)
	}
	if len(r2) != 1 || r2[0].Title != "R" {
		t.Fatalf("second search results = %v", r2)
	}
	if calls != 1 {
		t.Fatalf("after second search calls = %d, want 1 (served from cache, hit)", calls)
	}
}

func TestCacheSearcher_DifferentQuery_MissesAgain(t *testing.T) {
	calls := 0
	inner := &countingSearcher{name: "ix", fn: func() []SearchResult {
		calls++
		return []SearchResult{{Title: "R", Indexer: "ix"}}
	}}
	cs := NewCacheSearcher(inner, newMemCache(), time.Minute)

	_, _ = cs.Search(context.Background(), SearchQuery{Term: "a"})
	_, _ = cs.Search(context.Background(), SearchQuery{Term: "b"})
	if calls != 2 {
		t.Errorf("different terms should each miss: calls = %d, want 2", calls)
	}
}

func TestCacheSearcher_DifferentIndexer_DoesNotShareEntry(t *testing.T) {
	aCalls, bCalls := 0, 0
	// Two indexers with the same name would be a bug; use distinct names. The
	// cache key includes the indexer name, so identical queries to two
	// differently-named indexers must not share a cache entry.
	a := &countingSearcher{name: "alpha", fn: func() []SearchResult {
		aCalls++
		return []SearchResult{{Title: "A", Indexer: "alpha"}}
	}}
	b := &countingSearcher{name: "beta", fn: func() []SearchResult {
		bCalls++
		return []SearchResult{{Title: "B", Indexer: "beta"}}
	}}
	cache := newMemCache()
	ca := NewCacheSearcher(a, cache, time.Minute)
	cb := NewCacheSearcher(b, cache, time.Minute)
	q := SearchQuery{Term: "same", Year: 2020}

	_, _ = ca.Search(context.Background(), q)
	_, _ = cb.Search(context.Background(), q) // must be a miss for beta, not beta's entry
	if aCalls != 1 || bCalls != 1 {
		t.Errorf("aCalls=%d bCalls=%d, want 1,1 (distinct keys per indexer)", aCalls, bCalls)
	}
	// Second round: each still served from its own entry.
	_, _ = ca.Search(context.Background(), q)
	_, _ = cb.Search(context.Background(), q)
	if aCalls != 1 || bCalls != 1 {
		t.Errorf("after round 2 aCalls=%d bCalls=%d, want 1,1 (hits)", aCalls, bCalls)
	}
}

func TestCacheSearcher_ReadError_FallsThroughToSearch(t *testing.T) {
	calls := 0
	inner := &countingSearcher{name: "ix", fn: func() []SearchResult {
		calls++
		return []SearchResult{{Title: "R", Indexer: "ix"}}
	}}
	cache := newMemCache()
	cache.failRead = true // simulate Redis read failure
	cs := NewCacheSearcher(inner, cache, time.Minute)

	// With reads failing, every search is a forced miss but must still return
	// the real results (failure isolation: a Redis outage degrades to "no
	// cache", never to an error).
	r, err := cs.Search(context.Background(), SearchQuery{Term: "x"})
	if err != nil {
		t.Fatalf("search with failing cache read: %v", err)
	}
	if len(r) != 1 || r[0].Title != "R" {
		t.Errorf("results = %v", r)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}

func TestCacheSearcher_WriteError_DoesNotFailSearch(t *testing.T) {
	calls := 0
	inner := &countingSearcher{name: "ix", fn: func() []SearchResult {
		calls++
		return []SearchResult{{Title: "R", Indexer: "ix"}}
	}}
	cache := newMemCache()
	cache.failWrite = true
	cs := NewCacheSearcher(inner, cache, time.Minute)

	r, err := cs.Search(context.Background(), SearchQuery{Term: "x"})
	if err != nil {
		t.Fatalf("search with failing cache write: %v", err)
	}
	if len(r) != 1 {
		t.Errorf("results = %v", r)
	}
}

func TestCacheSearcher_EmptyResults_NotCached(t *testing.T) {
	calls := 0
	inner := &countingSearcher{name: "ix", fn: func() []SearchResult {
		calls++
		return nil // empty result set
	}}
	cache := newMemCache()
	cs := NewCacheSearcher(inner, cache, time.Minute)
	q := SearchQuery{Term: "nothing"}

	if _, err := cs.Search(context.Background(), q); err != nil {
		t.Fatalf("search: %v", err)
	}
	// Empty results are deliberately not cached (caching "no results" would
	// suppress a later legitimate hit). The inner indexer is consulted again.
	if _, err := cs.Search(context.Background(), q); err != nil {
		t.Fatalf("search: %v", err)
	}
	if calls != 2 {
		t.Errorf("empty results should not be cached: calls = %d, want 2", calls)
	}
}

// countingSearcher is a Searcher that counts how many times it is consulted, so
// cache hit/miss can be asserted by observing the underlying call count.
type countingSearcher struct {
	name string
	fn   func() []SearchResult
}

func (c *countingSearcher) Name() string { return c.name }
func (c *countingSearcher) Search(ctx context.Context, q SearchQuery) ([]SearchResult, error) {
	out := c.fn()
	for i := range out {
		if out[i].Indexer == "" {
			out[i].Indexer = c.name
		}
	}
	return out, nil
}
