package indexers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// rssFixture is a realistic TorrentRSS feed exercising the interesting cases:
// a magnet item, a .torrent item, a malformed item (no title, no link), an
// item filtered by category, and one filtered by seeders.
const rssFixture = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Test Feed</title>
    <item>
      <title>Show.Name.S01E01.1080p.WEB-DL</title>
      <guid>g1</guid>
      <link>http://example.com/show.torrent</link>
      <enclosure url="magnet:?xt=urn:btih:abcd" length="100"/>
      <category>5000</category>
      <seeders>42</seeders>
      <size>100</size>
    </item>
    <item>
      <title>No.Link.No.Title.item</title>
      <guid>g2</guid>
    </item>
    <item>
      <title></title>
      <guid>g3</guid>
    </item>
    <item>
      <title>Filtered.By.Category</title>
      <guid>g4</guid>
      <link>http://example.com/other.torrent</link>
      <category>9999</category>
      <seeders>10</seeders>
      <size>200</size>
    </item>
    <item>
      <title>Too.Few.Seeders</title>
      <guid>g5</guid>
      <link>http://example.com/low.torrent</link>
      <category>5000</category>
      <seeders>1</seeders>
      <size>300</size>
    </item>
  </channel>
</rss>`

func newRSSAdapter(t *testing.T, body string, code int, settings string) (*TorrentRSSIndexer, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if code != http.StatusOK {
			w.WriteHeader(code)
			return
		}
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	def := Definition{Name: "rss", BaseURL: srv.URL, SettingsJSON: settings}
	return NewTorrentRSSIndexer(def, AdapterDeps{}), srv
}

// TestTorrentRSS_Search_MatchesAndFilters verifies term matching, category
// filtering, and seed-threshold filtering all apply, and that the surviving
// item carries the magnet URL (#32 core behavior).
func TestTorrentRSS_Search_MatchesAndFilters(t *testing.T) {
	// Accept category 5000 and require >=10 seeders.
	adp, _ := newRSSAdapter(t, rssFixture, 200, `{"categories":["5000"],"min_seeders":10}`)
	res, err := adp.Search(context.Background(), SearchQuery{Term: "Show"})
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("expected exactly 1 surviving item, got %d: %+v", len(res), res)
	}
	if res[0].Title != "Show.Name.S01E01.1080p.WEB-DL" {
		t.Fatalf("expected the S01E01 item, got %q", res[0].Title)
	}
	if res[0].Info.URL != "magnet:?xt=urn:btih:abcd" {
		t.Errorf("expected magnet url, got %q", res[0].Info.URL)
	}
	if res[0].Info.Seeders != 42 {
		t.Errorf("expected 42 seeders, got %d", res[0].Info.Seeders)
	}
}

// TestTorrentRSS_Search_EmptyTermReturnsAllFiltered is a feed-snapshot search:
// an empty term returns everything that passes the category/seed filters.
func TestTorrentRSS_Search_EmptyTermReturnsAllFiltered(t *testing.T) {
	// No term, accept category 5000, no seed minimum → the S01E01 item and the
	// low-seeder item both pass category; only S01E01 has a real term match...
	// but with an empty term EVERYTHING that passes the category filter is
	// returned. The "Filtered.By.Category" item is category 9999 (out), the
	// two malformed items are dropped, so we expect the two 5000-category
	// items.
	adp, _ := newRSSAdapter(t, rssFixture, 200, `{"categories":["5000"]}`)
	res, err := adp.Search(context.Background(), SearchQuery{})
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("expected 2 items (both 5000-category, non-malformed), got %d: %+v", len(res), res)
	}
}

// TestTorrentRSS_Search_SkipsMalformedItems confirms malformed items (no title
// and no link) are skipped, not fatal, and that a feed of *only* malformed
// items yields an empty result rather than an error (#32: "malformed-entry
// skip").
func TestTorrentRSS_Search_SkipsMalformedItems(t *testing.T) {
	malformed := `<?xml version="1.0"?>
<rss><channel>
  <item><guid>a</guid></item>
  <item><guid>b</guid></item>
</channel></rss>`
	adp, _ := newRSSAdapter(t, malformed, 200, "")
	res, err := adp.Search(context.Background(), SearchQuery{})
	if err != nil {
		t.Fatalf("expected nil error (skip, not fatal), got %v", err)
	}
	if len(res) != 0 {
		t.Fatalf("expected 0 results, got %d", len(res))
	}
}

// TestTorrentRSS_Search_MalformedFeed maps a non-XML feed body to
// ErrBadResponse (the feed itself is corrupt — distinct from skipping a single
// bad item).
func TestTorrentRSS_Search_MalformedFeed(t *testing.T) {
	adp, _ := newRSSAdapter(t, "not xml <<<", 200, "")
	_, err := adp.Search(context.Background(), SearchQuery{})
	if !errors.Is(err, ErrBadResponse) {
		t.Fatalf("expected ErrBadResponse, got %v", err)
	}
}

// TestTorrentRSS_Test_ProbesFeed confirms Test() succeeds on a valid feed and
// returns ErrBadResponse on a non-feed body.
func TestTorrentRSS_Test_ProbesFeed(t *testing.T) {
	adp, _ := newRSSAdapter(t, rssFixture, 200, "")
	if err := adp.Test(context.Background()); err != nil {
		t.Fatalf("Test on valid feed should pass, got %v", err)
	}

	bad, _ := newRSSAdapter(t, "garbage", 200, "")
	if err := bad.Test(context.Background()); !errors.Is(err, ErrBadResponse) {
		t.Fatalf("Test on garbage feed: expected ErrBadResponse, got %v", err)
	}
}

// TestTorrentRSS_RateLimit_BoundsRequests confirms the adapter's rate limiter
// actually delays a burst of back-to-back searches past the token-bucket
// window (#32: "rate limiting"). With rate=1 rps, burst=1, three consecutive
// searches must take at least ~2s total (two token refills).
func TestTorrentRSS_RateLimit_BoundsRequests(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<?xml version="1.0"?><rss><channel></channel></rss>`))
	}))
	defer srv.Close()

	def := Definition{Name: "rss", BaseURL: srv.URL}
	adp := NewTorrentRSSIndexer(def, AdapterDeps{})
	// Replace the limiter with a strict one: 1 req/s, burst 1.
	adp.limiter = NewRateLimiter(1.0, 1)

	start := time.Now()
	for i := 0; i < 3; i++ {
		if _, err := adp.Search(context.Background(), SearchQuery{}); err != nil {
			t.Fatalf("search %d error: %v", i, err)
		}
	}
	elapsed := time.Since(start)
	if elapsed < 1500*time.Millisecond {
		t.Fatalf("expected ~2s of rate-limit delay for 3 searches at 1 rps burst 1, got %v", elapsed)
	}
}

// TestRateLimiter_BurstThenThrottle is a focused unit test on the token bucket:
// a burst of N passes immediately, the (N+1)th blocks until refill.
func TestRateLimiter_BurstThenThrottle(t *testing.T) {
	rl := NewRateLimiter(10.0, 2) // 10 rps, burst 2
	ctx := context.Background()

	// Two immediate.
	start := time.Now()
	if err := rl.Wait(ctx); err != nil {
		t.Fatalf("first wait: %v", err)
	}
	if err := rl.Wait(ctx); err != nil {
		t.Fatalf("second wait: %v", err)
	}
	if d := time.Since(start); d > 50*time.Millisecond {
		t.Fatalf("burst of 2 should be immediate, took %v", d)
	}
	// Third must wait ~100ms for a token.
	if err := rl.Wait(ctx); err != nil {
		t.Fatalf("third wait: %v", err)
	}
	if d := time.Since(start); d < 80*time.Millisecond {
		t.Fatalf("expected the 3rd wait to block ~100ms, total elapsed %v", d)
	}
}

// TestRateLimiter_DisabledWhenRateZero confirms rate <= 0 disables limiting.
func TestRateLimiter_DisabledWhenRateZero(t *testing.T) {
	rl := NewRateLimiter(0, 1)
	start := time.Now()
	for i := 0; i < 100; i++ {
		if err := rl.Wait(context.Background()); err != nil {
			t.Fatalf("wait %d: %v", i, err)
		}
	}
	if d := time.Since(start); d > 50*time.Millisecond {
		t.Fatalf("disabled limiter should not block, took %v", d)
	}
}

// TestRateLimiter_ContextCancel confirms a cancelled context aborts the wait
// instead of blocking forever.
func TestRateLimiter_ContextCancel(t *testing.T) {
	rl := NewRateLimiter(1.0, 1)
	if err := rl.Wait(context.Background()); err != nil {
		t.Fatalf("consume the single burst token: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately
	if err := rl.Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}
