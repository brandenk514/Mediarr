package imageproxy

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

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

// keyDir is the on-disk home of one cache key.
func (c *diskCache) keyDir(key string) string {
	return filepath.Join(c.dir, key)
}

// entryName is the stored file name for one cached image: the max-age,
// the expiry timestamp (ms), the (currently unused, fork-parity) etag
// slot, and the extension, dot-separated.
func entryName(maxAge, expireAtMs int64, ext string) string {
	return fmt.Sprintf("%d.%d..%s", maxAge, expireAtMs, ext)
}

// parseEntryName decodes entryName back into (maxAge, expireAtMs, ext).
func parseEntryName(name string) (maxAge, expireAtMs int64, ext string) {
	parts := strings.SplitN(name, ".", 4)
	if len(parts) != 4 {
		return 0, 0, ""
	}
	maxAge, _ = strconv.ParseInt(parts[0], 10, 64)
	expireAtMs, _ = strconv.ParseInt(parts[1], 10, 64)
	return maxAge, expireAtMs, parts[3]
}

// newDiskCache builds the cache rooted at dir (created when missing) with
// the given size cap, seeding the index from the files already on disk
// (ordered by mtime, the fork's loadCacheIndex behavior).
func newDiskCache(dir string, maxSize int64) *diskCache {
	c := &diskCache{dir: dir, maxSize: maxSize, sizes: map[string]int64{}}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return c
	}
	type seed struct {
		key  string
		mod  time.Time
		size int64
	}
	var seeds []seed
	entries, err := os.ReadDir(dir)
	if err != nil {
		return c
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(dir, e.Name()))
		if err != nil || len(files) == 0 {
			continue
		}
		info, err := files[0].Info()
		if err != nil {
			continue
		}
		seeds = append(seeds, seed{key: e.Name(), mod: info.ModTime(), size: info.Size() + entryOverhead})
	}
	sort.Slice(seeds, func(i, j int) bool { return seeds[i].mod.Before(seeds[j].mod) })
	for _, s := range seeds {
		c.order = append(c.order, s.key)
		c.sizes[s.key] = s.size
		c.total += s.size
	}
	return c
}

// get returns the cached image for key. A stale entry (past its expireAt
// timestamp) is deleted and reported as a miss. hit=false when the key is
// unknown; ext is the stored file extension ("jpg", "png", "" when
// unknown), maxAge the stored cache-control max-age.
func (c *diskCache) get(key string) (data []byte, ext string, maxAge int, hit bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.sizes[key]; !ok {
		return nil, "", 0, false
	}
	files, err := os.ReadDir(c.keyDir(key))
	if err != nil || len(files) == 0 {
		c.dropLocked(key)
		return nil, "", 0, false
	}
	entryMaxAge, expireAtMs, entryExt := parseEntryName(files[0].Name())
	if time.Now().UnixMilli() > expireAtMs {
		_ = os.RemoveAll(c.keyDir(key))
		c.dropLocked(key)
		return nil, "", 0, false
	}
	b, err := os.ReadFile(filepath.Join(c.keyDir(key), files[0].Name()))
	if err != nil {
		c.dropLocked(key)
		return nil, "", 0, false
	}
	return b, entryExt, int(entryMaxAge), true
}

// put writes the image under key and records its size (file size +
// entryOverhead) in the write-order index, then evicts the oldest entries
// until the total is within maxSize. It returns the evicted keys.
func (c *diskCache) put(key string, data []byte, maxAge int, ext string) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.maxSize <= 0 {
		return nil
	}
	expireAtMs := time.Now().Add(time.Duration(maxAge) * time.Second).UnixMilli()
	keyDir := c.keyDir(key)
	// A re-put replaces the entry wholesale (fresh file, fresh write order).
	if _, exists := c.sizes[key]; exists {
		_ = os.RemoveAll(keyDir)
		c.dropLocked(key)
	}
	if err := os.MkdirAll(keyDir, 0o750); err != nil {
		return nil
	}
	if err := os.WriteFile(filepath.Join(keyDir, entryName(int64(maxAge), expireAtMs, ext)), data, 0o600); err != nil {
		return nil
	}
	size := int64(len(data)) + entryOverhead
	c.order = append(c.order, key)
	c.sizes[key] = size
	c.total += size

	var evicted []string
	for c.total > c.maxSize && len(c.order) > 0 {
		oldest := c.order[0]
		// Never evict the entry we just wrote: a pathological
		// single-entry-over-cap write still keeps its own bytes.
		if oldest == key {
			break
		}
		c.order = c.order[1:]
		if oldestSize, ok := c.sizes[oldest]; ok {
			c.total -= oldestSize
			delete(c.sizes, oldest)
		}
		_ = os.RemoveAll(c.keyDir(oldest))
		evicted = append(evicted, oldest)
	}
	return evicted
}

// dropLocked removes key from the in-memory index (c.mu held).
func (c *diskCache) dropLocked(key string) {
	if size, ok := c.sizes[key]; ok {
		c.total -= size
		delete(c.sizes, key)
	}
	for i, k := range c.order {
		if k == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			break
		}
	}
}
