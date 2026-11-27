package imageproxy

import "sync"

// entryOverhead is the per-entry disk cost the fork charges on top of the
// file size (a directory block plus file slack): the index counts it so
// the 1 GiB cap bounds the cache's real footprint.
const entryOverhead int64 = 8192

// diskCache is the on-disk image cache (the fork's ImageProxy + CacheIndex
// semantics): one directory per cache key under the cache root, one file
// per entry named "{maxAge}.{expireAtMs}.{etag}.{ext}", an in-memory
// write-order index for oldest-first eviction, and stale entries dropped
// on read (expired by timestamp).
type diskCache struct {
	dir     string
	maxSize int64
	mu      sync.Mutex
	order   []string // write order, oldest first
	sizes   map[string]int64
	total   int64
}

// newDiskCache builds the cache rooted at dir (created when missing) with
// the given size cap, seeding the index from the files already on disk
// (ordered by mtime, the fork's loadCacheIndex behavior).
func newDiskCache(dir string, maxSize int64) *diskCache {
	return nil
}

// get returns the cached image for key. A stale entry (past its expireAt
// timestamp) is deleted and reported as a miss. hit=false when the key is
// unknown; ext is the stored file extension ("jpg", "png", "" when
// unknown), maxAge the stored cache-control max-age.
func (c *diskCache) get(key string) (data []byte, ext string, maxAge int, hit bool) {
	return nil, "", 0, false
}

// put writes the image under key and records its size (file size +
// entryOverhead) in the write-order index, then evicts the oldest entries
// until the total is within maxSize. It returns the evicted keys.
func (c *diskCache) put(key string, data []byte, maxAge int, ext string) []string {
	return nil
}
