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
	"errors"
	"net/http"
	"sync"
)

// DefaultMaxCacheSize is the on-disk cache cap (the fork's 1 GiB
// MAX_CACHE_SIZE).
const DefaultMaxCacheSize int64 = 1024 * 1024 * 1024

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
	return &Proxy{cfg: cfg, limits: map[string]*hostLimiter{}}
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
	return nil, ErrNotImplemented
}

// ValidateImagePath applies the fork's path safety rules to the joined
// image path ("/{size}/{path...}"): it must not start with "//" nor
// contain "://" (ErrForbidden), and every non-empty segment must be
// whitelisted (ErrInvalidPath): [\w.-]+ or the duotone filter form
// w\d+_filter\(duotone,<hex6>,<hex6>) (case-insensitive hex). It returns
// the validated segments.
func ValidateImagePath(joined string) ([]string, error) {
	return nil, ErrNotImplemented
}

// upstreamURL maps (type, validated image path) to the absolute upstream
// URL. The tmdb type targets the image CDN's /t/p root (the path supplies
// the /{size} segment); the tvdb type passes the path to the artwork CDN
// as-is (size segment included, mirroring the fork).
func upstreamURL(cfg Config, typ, imagePath string) (string, error) {
	return "", ErrNotImplemented
}
