// Package imageproxy is the same-origin artwork proxy (issue #149): a
// translation of the Seerr fork's image proxy (server/routes/imageproxy.ts
// + server/lib/imageproxy.ts, MIT (c) 2020 sct) from Express/TS to Go.
//
// It streams bytes through from two allowlisted upstream image hosts
// (TMDB's image CDN and TheTVDB's artwork CDN) with:
//   - a path-segment whitelist (word/dot/dash characters, plus the Seerr
//     duotone-filter segment form) — '.'/'..' segments and '//'/'://'
//     joined-path forms are rejected (403/400),
//   - an on-disk cache with a 1 GiB cap, oldest-first eviction, and stale
//     entries expired by timestamp,
//   - a per-host rate limit (20 concurrent requests / 50 requests per
//     second),
//   - no re-encoding or resizing: the upstream bytes are passed through
//     unchanged.
//
// The route itself is PUBLIC (registered like /version): #155's login
// backdrop consumes it pre-auth, and its safety comes from the upstream
// allowlist, the segment whitelist, the rate limit, and the cache cap —
// not from auth.
package imageproxy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// DefaultMaxCacheSize is the on-disk cache cap (the fork's 1 GiB
// MAX_CACHE_SIZE).
const DefaultMaxCacheSize int64 = 1024 * 1024 * 1024

// defaultMaxAge is the cache TTL served when the upstream sends no
// Cache-Control max-age (the fork's CACHE_CONTROL default of one day).
const defaultMaxAge = 86400

// cacheVersion is mixed into every cache key, so a future format change
// can invalidate the whole on-disk cache by bumping it (the fork's
// versioned cache path).
const cacheVersion = "v1"

// Sentinel errors. The API layer maps them to status codes:
// ErrForbidden → 403, ErrInvalidPath → 400, ErrUnsupportedType → 400,
// ErrUpstreamNotFound → 404, ErrUpstream → 502.
var (
	// ErrForbidden: the joined path starts with '//' or contains '://'.
	ErrForbidden = errors.New("imageproxy: invalid url for image proxy")
	// ErrInvalidPath: a path segment is not whitelisted ('.', '..', or
	// characters outside [\w.-] and the duotone filter form).
	ErrInvalidPath = errors.New("imageproxy: invalid image path")
	// ErrUnsupportedType: the {type} segment is not "tmdb" or "tvdb".
	ErrUnsupportedType = errors.New("imageproxy: unsupported image type")
	// ErrUpstreamNotFound: the upstream answered 404.
	ErrUpstreamNotFound = errors.New("imageproxy: upstream image not found")
	// ErrUpstream: the upstream fetch failed or answered non-2xx/non-404.
	ErrUpstream = errors.New("imageproxy: failed to load image")
	// ErrNotImplemented marks the RED-phase stub.
	ErrNotImplemented = errors.New("imageproxy: not implemented")
)

// Upstream image hosts (issue #149 upstream allowlist — the ONLY two).
const (
	tmdbUpstream = "https://image.tmdb.org/t/p"
	tvdbUpstream = "https://artworks.thetvdb.com"
)

// Config builds a Proxy. CacheDir is required; everything else has a
// production default. TMDBBaseURL/TVDBBaseURL exist so tests can point
// the proxy at a stub upstream.
type Config struct {
	// CacheDir is the on-disk image cache root (…/cache/images).
	CacheDir string
	// MaxCacheSize caps the on-disk cache; 0 → DefaultMaxCacheSize.
	MaxCacheSize int64
	// Client is the upstream HTTP client; nil → a default client.
	Client *http.Client
	// TMDBBaseURL overrides the TMDB upstream (tests only).
	TMDBBaseURL string
	// TVDBBaseURL overrides the TVDB upstream (tests only).
	TVDBBaseURL string
}

// Proxy fetches artwork from the allowlisted upstreams through the cache
// and rate limiter. It is safe for concurrent use.
type Proxy struct {
	cfg    Config
	cache  *diskCache
	mu     sync.Mutex
	limits map[string]*hostLimiter
}

// New builds a Proxy over cfg.
func New(cfg Config) *Proxy {
	max := cfg.MaxCacheSize
	if max <= 0 {
		max = DefaultMaxCacheSize
	}
	p := &Proxy{cfg: cfg, limits: map[string]*hostLimiter{}}
	if cfg.CacheDir != "" {
		p.cache = newDiskCache(cfg.CacheDir, max)
	}
	return p
}

// Result is one proxied image.
type Result struct {
	// Data is the image bytes (passed through unmodified).
	Data []byte
	// ContentType is the image's MIME type (derived from the stored
	// extension; "application/octet-stream" when unknown).
	ContentType string
	// MaxAge is the upstream cache-control max-age in seconds (the fork's
	// default 86400 when the upstream sent none).
	MaxAge int
	// CacheKey is the cache entry key (sha256 over type+version+path).
	CacheKey string
	// CacheMiss reports whether the bytes were fetched from upstream
	// (false = served from the on-disk cache).
	CacheMiss bool
}

// Get streams one image: cache lookup, upstream fetch on miss, cache
// write + oldest-first eviction, per-host rate limiting. imagePath is
// the joined "/{size}/{path...}" form.
func (p *Proxy) Get(ctx context.Context, typ, imagePath string) (*Result, error) {
	if _, err := ValidateImagePath(imagePath); err != nil {
		return nil, err
	}
	target, err := upstreamURL(p.cfg, typ, imagePath)
	if err != nil {
		return nil, err
	}
	limiter := p.limiterFor(hostOf(target))
	if err := limiter.acquire(ctx); err != nil {
		return nil, err
	}
	defer limiter.release()

	key := cacheKey(typ, imagePath)
	if data, ext, maxAge, hit := p.cache.get(key); hit {
		return &Result{
			Data:        data,
			ContentType: contentTypeFor(ext),
			MaxAge:      maxAge,
			CacheKey:    key,
			CacheMiss:   false,
		}, nil
	}

	client := p.cfg.Client
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrUpstreamNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("%w: status %d", ErrUpstream, resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUpstream, err)
	}

	maxAge := parseMaxAge(resp.Header.Get("Cache-Control"))
	ext := extOfPath(imagePath)
	p.cache.put(key, data, maxAge, ext)
	return &Result{
		Data:        data,
		ContentType: contentTypeFor(ext),
		MaxAge:      maxAge,
		CacheKey:    key,
		CacheMiss:   true,
	}, nil
}

// limiterFor returns (creating on first use) the rate limiter for one
// upstream host.
func (p *Proxy) limiterFor(host string) *hostLimiter {
	p.mu.Lock()
	defer p.mu.Unlock()
	l, ok := p.limits[host]
	if !ok {
		l = newHostLimiter(maxConcurrentPerHost, maxRPSPerHost)
		p.limits[host] = l
	}
	return l
}

// cacheKey hashes type+version+path into the on-disk cache key (a
// hex-encoded sha256, safe as a file name).
func cacheKey(typ, imagePath string) string {
	h := sha256.Sum256([]byte(typ + "\x00" + cacheVersion + "\x00" + imagePath))
	return hex.EncodeToString(h[:])
}

// hostOf extracts the rate-limit key of an upstream URL (its host, or
// the full URL when unparsable — never "").
func hostOf(target string) string {
	if u, err := url.Parse(target); err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	return target
}

// parseMaxAge extracts the cache-control max-age, defaulting to the
// fork's one-day TTL when the header is absent or malformed.
func parseMaxAge(cacheControl string) int {
	for _, part := range strings.Split(cacheControl, ",") {
		part = strings.TrimSpace(part)
		if v, ok := strings.CutPrefix(strings.ToLower(part), "max-age="); ok {
			if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n >= 0 {
				return n
			}
		}
	}
	return defaultMaxAge
}

// extOfPath derives the stored file extension from the final path
// segment's extension ("poster.jpg" → "jpg"; none/oversized → "").
func extOfPath(imagePath string) string {
	seg := path.Base(imagePath)
	dot := strings.LastIndexByte(seg, '.')
	if dot < 0 {
		return ""
	}
	ext := strings.ToLower(seg[dot+1:])
	if ext == "" || len(ext) > 8 {
		return ""
	}
	for _, r := range ext {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return ""
		}
	}
	return ext
}

// contentTypeFor maps a stored extension to its MIME type; unknown
// extensions stream as application/octet-stream (the browser sniffs).
func contentTypeFor(ext string) string {
	switch ext {
	case "jpg", "jpeg":
		return "image/jpeg"
	case "png":
		return "image/png"
	case "webp":
		return "image/webp"
	case "gif":
		return "image/gif"
	default:
		return "application/octet-stream"
	}
}

// segmentRe whitelists one path segment: word/dot/dash characters only
// (the fork's \w.- class; Go's \w is ASCII, so unicode is rejected).
var segmentRe = regexp.MustCompile(`^[\w.-]+$`)

// duotoneRe whitelists the fork's duotone filter size segment,
// w<size>_filter(duotone,<hex6>,<hex6>), hex case-insensitive.
var duotoneRe = regexp.MustCompile(`^[wW]\d+_filter\([dD]uotone,[0-9a-fA-F]{6},[0-9a-fA-F]{6}\)$`)

// ValidateImagePath applies the fork's path safety rules to the joined
// image path ("/{size}/{path...}"): it must not start with "//" nor
// contain "://" (ErrForbidden), and every non-empty segment must be
// whitelisted (ErrInvalidPath): [\w.-]+ or the duotone filter form
// w\d+_filter\(duotone,<hex6>,<hex6>) (case-insensitive hex). It returns
// the validated segments.
func ValidateImagePath(joined string) ([]string, error) {
	if strings.HasPrefix(joined, "//") {
		return nil, ErrForbidden
	}
	if strings.Contains(joined, "://") {
		return nil, ErrForbidden
	}
	var segs []string
	for _, seg := range strings.Split(joined, "/") {
		if seg == "" {
			continue
		}
		if seg == "." || seg == ".." {
			return nil, ErrInvalidPath
		}
		if !segmentRe.MatchString(seg) && !duotoneRe.MatchString(seg) {
			return nil, ErrInvalidPath
		}
		segs = append(segs, seg)
	}
	return segs, nil
}

// upstreamURL maps (type, validated image path) to the absolute upstream
// URL. The tmdb type targets the image CDN's /t/p root (the path supplies
// the /{size} segment); the tvdb type passes the path to the artwork CDN
// as-is (size segment included, mirroring the fork).
func upstreamURL(cfg Config, typ, imagePath string) (string, error) {
	switch typ {
	case "tmdb":
		base := cfg.TMDBBaseURL
		if base == "" {
			base = tmdbUpstream
		}
		return base + imagePath, nil
	case "tvdb":
		base := cfg.TVDBBaseURL
		if base == "" {
			base = tvdbUpstream
		}
		return base + imagePath, nil
	default:
		return "", ErrUnsupportedType
	}
}
