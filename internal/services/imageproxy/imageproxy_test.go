package imageproxy

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---- path validation (the fork's safety matrix) ----

func TestValidateImagePath(t *testing.T) {
	cases := []struct {
		name    string
		joined  string
		wantErr error
	}{
		{"plain", "/w342/abc.jpg", nil},
		{"nested", "/w342/dir/sub/a-b_c.d.jpg", nil},
		{"duotone size", "/w500_filter(duotone,2c2c54,ff8c53)/abc.jpg", nil},
		{"duotone uppercase hex", "/w500_filter(duotone,2C2C54,FF8C53)/abc.jpg", nil},
		{"duotone non-hex", "/w500_filter(duotone,gggggg,000000)/abc.jpg", ErrInvalidPath},
		{"duotone bad form", "/w500_filter(grayscale,000000,111111)/abc.jpg", ErrInvalidPath},
		{"dot segment", "/w342/.", ErrInvalidPath},
		{"dotdot segment", "/w342/..", ErrInvalidPath},
		{"leading double slash", "//w342/abc.jpg", ErrForbidden},
		{"scheme smuggle", "/w342/a://b.jpg", ErrForbidden},
		{"space in segment", "/w342/a b.jpg", ErrInvalidPath},
		{"unicode in segment", "/w342/héllo.jpg", ErrInvalidPath},
		{"slash in segment", "/w342/a/b", nil}, // 'b' is a valid nested segment
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ValidateImagePath(tc.joined)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("ValidateImagePath(%q) = %v, want ok", tc.joined, err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("ValidateImagePath(%q) = %v, want %v", tc.joined, err, tc.wantErr)
			}
		})
	}
}

// The decoded '%2e%2e' smuggle: net/http decodes escaped path segments
// before the handler sees them, so the whitelist must reject a decoded
// ".." just like a literal one.
func TestValidateImagePath_DecodedDotDot(t *testing.T) {
	if _, err := ValidateImagePath("/w342/.."); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("literal '..': err=%v", err)
	}
}

func TestUpstreamURL(t *testing.T) {
	cfg := Config{}
	u, err := upstreamURL(cfg, "tmdb", "/w342/abc.jpg")
	if err != nil || u != "https://image.tmdb.org/t/p/w342/abc.jpg" {
		t.Fatalf("tmdb upstream = %q, %v", u, err)
	}
	u, err = upstreamURL(cfg, "tvdb", "/w342/abc.jpg")
	if err != nil || u != "https://artworks.thetvdb.com/w342/abc.jpg" {
		t.Fatalf("tvdb upstream = %q, %v", u, err)
	}
	if _, err := upstreamURL(cfg, "posters", "/w342/abc.jpg"); !errors.Is(err, ErrUnsupportedType) {
		t.Fatalf("unknown type: err=%v, want ErrUnsupportedType", err)
	}
}

// ---- disk cache ----

func TestDiskCache_PutGet(t *testing.T) {
	c := newDiskCache(t.TempDir(), DefaultMaxCacheSize)
	if ev := c.put("k1", []byte("image-one"), 3600, "jpg"); len(ev) != 0 {
		t.Fatalf("first put evicted %v", ev)
	}
	data, ext, maxAge, hit := c.get("k1")
	if !hit || string(data) != "image-one" || ext != "jpg" || maxAge != 3600 {
		t.Fatalf("get = %q %q %d %v", data, ext, maxAge, hit)
	}
	if _, _, _, hit := c.get("missing"); hit {
		t.Fatal("unknown key reported as hit")
	}
}

func TestDiskCache_StaleExpiry(t *testing.T) {
	dir := t.TempDir()
	c := newDiskCache(dir, DefaultMaxCacheSize)
	// max-age 0: the entry expires the instant it is written.
	c.put("k1", []byte("gone"), 0, "jpg")
	time.Sleep(20 * time.Millisecond)
	if _, _, _, hit := c.get("k1"); hit {
		t.Fatal("stale entry served as a hit")
	}
	// The stale file was removed from disk.
	if _, err := os.Stat(filepath.Join(dir, "k1")); !os.IsNotExist(err) {
		t.Fatalf("stale entry dir still exists: %v", err)
	}
}

func TestDiskCache_Eviction_OldestFirst(t *testing.T) {
	// Cap sized for exactly two entries of len("aaaa")+entryOverhead.
	payload := []byte("aaaa")
	capFor := int64(len(payload)+int(entryOverhead)) * 2
	c := newDiskCache(t.TempDir(), capFor)
	c.put("old", payload, 3600, "jpg")
	time.Sleep(5 * time.Millisecond)
	c.put("mid", payload, 3600, "jpg")
	time.Sleep(5 * time.Millisecond)
	ev := c.put("new", payload, 3600, "jpg")
	if len(ev) != 1 || ev[0] != "old" {
		t.Fatalf("evicted = %v, want [old]", ev)
	}
	if _, _, _, hit := c.get("old"); hit {
		t.Fatal("evicted entry still a hit")
	}
	if _, _, _, hit := c.get("mid"); !hit {
		t.Fatal("mid evicted, want kept")
	}
	if _, _, _, hit := c.get("new"); !hit {
		t.Fatal("new missing, want kept")
	}
}

// ---- per-host rate limiting ----

func TestHostLimiter_BurstAndRefill(t *testing.T) {
	l := newHostLimiter(maxConcurrentPerHost, maxRPSPerHost)
	ctx := context.Background()
	// The burst of 20 acquires immediately.
	start := time.Now()
	for i := 0; i < maxConcurrentPerHost; i++ {
		if err := l.acquire(ctx); err != nil {
			t.Fatalf("acquire %d: %v", i, err)
		}
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("burst took %v, want immediate", elapsed)
	}
	// The 21st waits for a refill token (50/s → ~20ms) — never fails.
	acq := make(chan error, 1)
	go func() { acq <- l.acquire(ctx) }()
	select {
	case err := <-acq:
		if err != nil {
			t.Fatalf("21st acquire: %v", err)
		}
		t.Log("21st acquire admitted (refill)")
	case <-time.After(2 * time.Second):
		t.Fatal("21st acquire never admitted")
	}
	for i := 0; i < maxConcurrentPerHost+1; i++ {
		l.release()
	}
}

func TestHostLimiter_ConcurrencyCap(t *testing.T) {
	l := newHostLimiter(2, 1000) // small cap so the test is quick
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if err := l.acquire(ctx); err != nil {
			t.Fatalf("acquire %d: %v", i, err)
		}
	}
	done := make(chan struct{})
	go func() { _ = l.acquire(ctx); close(done) }()
	select {
	case <-done:
		t.Fatal("third acquire admitted while two slots held")
	case <-time.After(80 * time.Millisecond):
	}
	l.release()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("third acquire never admitted after release")
	}
}

// ---- end-to-end Get (stub upstream) ----

type upstreamRecorder struct {
	mu     sync.Mutex
	hits   int
	body   []byte
	ctype  string
	maxAge string
}

func (u *upstreamRecorder) handler(w http.ResponseWriter, r *http.Request) {
	u.mu.Lock()
	u.hits++
	body, ctype, maxAge := u.body, u.ctype, u.maxAge
	u.mu.Unlock()
	w.Header().Set("Content-Type", ctype)
	if maxAge != "" {
		w.Header().Set("Cache-Control", maxAge)
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func newTestProxy(t *testing.T, up *upstreamRecorder) *Proxy {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(up.handler))
	t.Cleanup(srv.Close)
	return New(Config{CacheDir: t.TempDir(), TMDBBaseURL: srv.URL})
}

func TestGet_MissThenHit(t *testing.T) {
	up := &upstreamRecorder{body: []byte("jpeg-bytes-here"), ctype: "image/jpeg", maxAge: "public, max-age=3600"}
	p := newTestProxy(t, up)

	res, err := p.Get(context.Background(), "tmdb", "/w342/poster.jpg")
	if err != nil {
		t.Fatalf("first Get: %v", err)
	}
	if !res.CacheMiss {
		t.Fatal("first Get reported a cache hit")
	}
	if string(res.Data) != "jpeg-bytes-here" {
		t.Fatalf("data = %q", res.Data)
	}
	if res.ContentType != "image/jpeg" {
		t.Fatalf("content type = %q", res.ContentType)
	}
	if res.MaxAge != 3600 {
		t.Fatalf("max-age = %d", res.MaxAge)
	}

	res2, err := p.Get(context.Background(), "tmdb", "/w342/poster.jpg")
	if err != nil {
		t.Fatalf("second Get: %v", err)
	}
	if res2.CacheMiss {
		t.Fatal("second Get reported a miss")
	}
	if string(res2.Data) != "jpeg-bytes-here" {
		t.Fatalf("cached data = %q", res2.Data)
	}
	if res2.CacheKey != res.CacheKey {
		t.Fatalf("cache key changed: %q vs %q", res2.CacheKey, res.CacheKey)
	}
	if up.hits != 1 {
		t.Fatalf("upstream hits = %d, want 1", up.hits)
	}
}

func TestGet_RejectsBeforeUpstream(t *testing.T) {
	up := &upstreamRecorder{body: []byte("x"), ctype: "image/jpeg"}
	p := newTestProxy(t, up)
	for joined, want := range map[string]error{
		"/w342/..":                  ErrInvalidPath,
		"/w342/evil://thing":        ErrForbidden,
		"/w342/a b.jpg":             ErrInvalidPath,
		"/w500_filter(x,y,z)/a.jpg": ErrInvalidPath,
	} {
		if _, err := p.Get(context.Background(), "tmdb", joined); !errors.Is(err, want) {
			t.Errorf("Get(%q) = %v, want %v", joined, err, want)
		}
	}
	if _, err := p.Get(context.Background(), "posters", "/w342/a.jpg"); !errors.Is(err, ErrUnsupportedType) {
		t.Errorf("unknown type: %v", err)
	}
	if up.hits != 0 {
		t.Fatalf("rejected request reached upstream (%d hits)", up.hits)
	}
}

func TestGet_DuotoneSegment(t *testing.T) {
	up := &upstreamRecorder{body: []byte("duo"), ctype: "image/jpeg", maxAge: "public, max-age=60"}
	p := newTestProxy(t, up)
	res, err := p.Get(context.Background(), "tmdb", "/w500_filter(duotone,2c2c54,ff8c53)/art.jpg")
	if err != nil {
		t.Fatalf("duotone Get: %v", err)
	}
	if !strings.Contains(string(res.Data), "duo") || up.hits != 1 {
		t.Fatalf("duotone fetch failed: hits=%d data=%q", up.hits, res.Data)
	}
}

func TestGet_UpstreamNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	p := New(Config{CacheDir: t.TempDir(), TMDBBaseURL: srv.URL})
	if _, err := p.Get(context.Background(), "tmdb", "/w342/nope.jpg"); !errors.Is(err, ErrUpstreamNotFound) {
		t.Fatalf("err = %v, want ErrUpstreamNotFound", err)
	}
}

func TestGet_DefaultMaxAgeWithoutCacheControl(t *testing.T) {
	up := &upstreamRecorder{body: []byte("x"), ctype: "image/jpeg"} // no Cache-Control
	p := newTestProxy(t, up)
	res, err := p.Get(context.Background(), "tmdb", "/w342/na.jpg")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if res.MaxAge != 86400 {
		t.Fatalf("default max-age = %d, want 86400", res.MaxAge)
	}
}
