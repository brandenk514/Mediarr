package indexers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestSearchAll_CapturesPerIndexerErrors confirms SearchAll returns one
// ManualResult per definition, in input order, and surfaces per-indexer
// failures instead of swallowing them (the behaviour the manual-search history
// depends on): a live indexer succeeds, a dead indexer reports a search error,
// and an unregistered kind reports a build error.
func TestSearchAll_CapturesPerIndexerErrors(t *testing.T) {
	reg := NewDefaultRegistry()

	feed := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:torznab="https://torznab.com/schemas/spec/">
  <channel>
    <item>
      <title>Live.Release.2024.1080p</title>
      <link>magnet:?xt=urn:btih:live</link>
      <torznab:category value="5040" name="TV/Anime"/>
    </item>
  </channel>
</rss>`
	live := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(feed))
	}))
	defer live.Close()

	// Empty API key is legal for torznab (some indexes auth via the URL); the
	// adapter simply omits the apikey parameter.
	defs := []Definition{
		{Name: "live-ix", Kind: torznabKind, BaseURL: live.URL},
		{Name: "dead-ix", Kind: torznabKind, BaseURL: "http://127.0.0.1:9"}, // discard port: refused
		{Name: "bad-ix", Kind: "no-such-kind"},
	}
	out := SearchAll(context.Background(), reg, defs, SearchQuery{Term: "x"}, 5*time.Second)
	if len(out) != 3 {
		t.Fatalf("expected 3 ManualResults, got %d", len(out))
	}
	// input order preserved
	wantNames := []string{"live-ix", "dead-ix", "bad-ix"}
	for i, w := range wantNames {
		if out[i].Name != w {
			t.Fatalf("order: out[%d].Name = %q, want %q", i, out[i].Name, w)
		}
	}
	// live: success with one result, category parsed
	if out[0].Err != nil {
		t.Fatalf("live-ix: expected no error, got %v", out[0].Err)
	}
	if len(out[0].Results) != 1 {
		t.Fatalf("live-ix: expected 1 result, got %d", len(out[0].Results))
	}
	if got := out[0].Results[0].Category; got != "TV/Anime" {
		t.Errorf("live-ix: expected category TV/Anime, got %q", got)
	}
	if out[0].Elapsed < 0 {
		t.Errorf("live-ix: expected non-negative elapsed, got %v", out[0].Elapsed)
	}
	// dead: search error, no results, name preserved
	if out[1].Err == nil {
		t.Fatalf("dead-ix: expected a search error, got nil")
	}
	if out[1].Results != nil {
		t.Errorf("dead-ix: expected nil results, got %v", out[1].Results)
	}
	// bad: build error (unknown kind), name falls back to the definition's name
	if out[2].Err == nil {
		t.Fatalf("bad-ix: expected a build error, got nil")
	}
	if out[2].Results != nil {
		t.Errorf("bad-ix: expected nil results, got %v", out[2].Results)
	}
}

// TestSearchAll_EmptyDefs confirms an empty definition list is a no-op (nil).
func TestSearchAll_EmptyDefs(t *testing.T) {
	reg := NewDefaultRegistry()
	if got := SearchAll(context.Background(), reg, nil, SearchQuery{Term: "x"}, 5*time.Second); got != nil {
		t.Fatalf("expected nil for no defs, got %v", got)
	}
}

// TestSearchAll_Cancellation confirms a cancelled parent ctx fails every
// in-flight attempt (the caller then fails the whole request).
func TestSearchAll_Cancellation(t *testing.T) {
	reg := NewDefaultRegistry()
	// A live server that would otherwise succeed.
	live := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<?xml version="1.0"?><rss version="2.0" xmlns:torznab="https://torznab.com/schemas/spec/"><channel><item><title>r</title><link>magnet:?xt=urn:btih:x</link></item></channel></rss>`))
	}))
	defer live.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled before the search starts

	defs := []Definition{{Name: "live-ix", Kind: torznabKind, BaseURL: live.URL}}
	out := SearchAll(ctx, reg, defs, SearchQuery{Term: "x"}, 5*time.Second)
	if len(out) != 1 {
		t.Fatalf("expected 1 ManualResult, got %d", len(out))
	}
	if out[0].Err == nil {
		t.Fatalf("expected an error after cancellation, got nil")
	}
}
