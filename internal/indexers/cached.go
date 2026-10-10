package indexers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/brandenk514/mediarr/internal/store"
)

// defaultResultTTL is the indexer search-result cache TTL (PLAN §5: "indexer
// search results (TTL 10min)"). It is overridable in tests so expiry can be
// exercised without waiting ten minutes.
const defaultResultTTL = 10 * time.Minute

// cacheKeyPrefix namespaces indexer result keys so they cannot collide with the
// provider-lookup cache or other keys in the shared Redis (PLAN §5).
const cacheKeyPrefix = "indexer:results:"

// CacheSearcher decorates a Searcher with a store.Cache-backed result cache.
// It is a Searcher itself, so it drops into a Fanout (or anywhere a Searcher is
// expected) without changing call sites.
//
// Caching rule (PLAN §5: "anything in Redis is re-derivable from Postgres"):
// cached search results are pure derivations of live indexer state; a cache
// miss or a stale entry degrades to "search the indexer again", never to a
// wrong answer. The cache is a latency/cost optimisation, not a source of truth.
//
// Failure isolation: a cache error (Redis down) must not break search. A read
// error is treated as a miss; a write error is discarded. Search always falls
// through to the underlying searcher.
type CacheSearcher struct {
	inner Searcher
	cache store.Cache
	ttl   time.Duration
}

// NewCacheSearcher wraps inner with cache. ttl <= 0 uses defaultResultTTL.
func NewCacheSearcher(inner Searcher, cache store.Cache, ttl time.Duration) *CacheSearcher {
	if ttl <= 0 {
		ttl = defaultResultTTL
	}
	return &CacheSearcher{inner: inner, cache: cache, ttl: ttl}
}

// Name implements Searcher, delegating to the wrapped indexer.
func (c *CacheSearcher) Name() string { return c.inner.Name() }

// Search implements Searcher. It returns cached results on a hit (no call to
// the indexer), otherwise searches the indexer and stores the result.
func (c *CacheSearcher) Search(ctx context.Context, q SearchQuery) ([]SearchResult, error) {
	key := c.cacheKey(q)

	if c.cache != nil {
		if raw, ok := c.cache.Get(ctx, key); ok {
			if results, err := unmarshalResults(raw); err == nil {
				return results, nil
			}
			// Corrupt entry: treat as a miss and fall through to re-search.
		}
	}

	results, err := c.inner.Search(ctx, q)
	if err != nil {
		return nil, err
	}

	if c.cache != nil && len(results) > 0 {
		if raw, err := json.Marshal(results); err == nil {
			_ = c.cache.Set(ctx, key, string(raw), c.ttl) // best-effort
		}
	}
	return results, nil
}

// cacheKey derives a stable cache key from the indexer and query. The term and
// year fully determine the result set, and the indexer's name is included so two
// indexers searching the same term do not share an entry.
func (c *CacheSearcher) cacheKey(q SearchQuery) string {
	return fmt.Sprintf("%s%s|%d", cacheKeyPrefix, c.inner.Name(), q.Year) + "|" + q.Term
}

func unmarshalResults(raw string) ([]SearchResult, error) {
	var out []SearchResult
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	return out, nil
}
