package indexers

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"testing"
	"time"
)

// --- test helpers -----------------------------------------------------------

// scriptedSearcher is a controllable Searcher for fan-out tests: it returns a
// fixed set of results or an error, and can be made to block for a duration so
// the per-indexer timeout can be exercised.
type scriptedSearcher struct {
	name    string
	results []SearchResult
	err     error
	delay   time.Duration
	calls   *int // shared counter to observe how many times it was searched
}

func (s *scriptedSearcher) Name() string { return s.name }

func (s *scriptedSearcher) Search(ctx context.Context, q SearchQuery) ([]SearchResult, error) {
	if s.calls != nil {
		*s.calls++
	}
	if s.delay > 0 {
		select {
		case <-time.After(s.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if s.err != nil {
		return nil, s.err
	}
	out := make([]SearchResult, 0, len(s.results))
	for _, r := range s.results {
		if r.Indexer == "" {
			r.Indexer = s.name
		}
		out = append(out, r)
	}
	return out, nil
}

func titleSet(res []SearchResult) map[string]bool {
	m := make(map[string]bool, len(res))
	for _, r := range res {
		m[r.Title] = true
	}
	return m
}

func mustTitles(t *testing.T, res []SearchResult) []string {
	t.Helper()
	out := make([]string, 0, len(res))
	for _, r := range res {
		out = append(out, r.Title)
	}
	sort.Strings(out)
	return out
}

// --- merge / dedup / ordering ----------------------------------------------

func TestFanout_MergesAndDedupes(t *testing.T) {
	a := &scriptedSearcher{name: "a", results: []SearchResult{
		{Title: "X.1080p", SizeBytes: 100},
		{Title: "Y.1080p", SizeBytes: 200},
	}}
	b := &scriptedSearcher{name: "b", results: []SearchResult{
		{Title: "X.1080p", SizeBytes: 999}, // same title as a -> deduped, a wins
		{Title: "Z.2160p", SizeBytes: 300},
	}}
	f := NewFanout(a, b)

	res, err := f.Search(context.Background(), SearchQuery{Term: "X"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	// Expect X (from a, first-wins), Y, Z -> 3 results, not 4.
	if len(res) != 3 {
		t.Fatalf("got %d results, want 3: %v", len(res), mustTitles(t, res))
	}
	// X must carry a's size (100), proving the lower-positioned indexer won.
	for _, r := range res {
		if r.Title == "X.1080p" && r.SizeBytes != 100 {
			t.Errorf("X SizeBytes = %d, want 100 (first indexer wins)", r.SizeBytes)
		}
	}
}

func TestFanout_DefaultOrder_IsSizeDescThenTitle(t *testing.T) {
	a := &scriptedSearcher{name: "a", results: []SearchResult{
		{Title: "small", SizeBytes: 10},
		{Title: "big", SizeBytes: 1000},
		{Title: "mid", SizeBytes: 100},
	}}
	f := NewFanout(a)
	res, err := f.Search(context.Background(), SearchQuery{})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	want := []string{"big", "mid", "small"}
	if len(res) != 3 {
		t.Fatalf("got %d results", len(res))
	}
	got := make([]string, 0, 3)
	for _, r := range res {
		got = append(got, r.Title)
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("default order = %v, want %v", got, want)
	}
}

func TestFanout_WithRank_OrdersByRanker(t *testing.T) {
	a := &scriptedSearcher{name: "a", results: []SearchResult{
		{Title: "A", SizeBytes: 1},
		{Title: "B", SizeBytes: 2},
		{Title: "C", SizeBytes: 3},
	}}
	// Rank by title descending (C, B, A) — proves the ranker overrides the
	// default size ordering.
	f := NewFanout(a).WithRank(func(x, y SearchResult) bool { return x.Title > y.Title })
	res, err := f.Search(context.Background(), SearchQuery{})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	var got []string
	for _, r := range res {
		got = append(got, r.Title)
	}
	if fmt.Sprint(got) != fmt.Sprint([]string{"C", "B", "A"}) {
		t.Errorf("ranked order = %v, want [C B A]", got)
	}
}

// --- concurrency -----------------------------------------------------------

func TestFanout_RunsConcurrently(t *testing.T) {
	// Two indexers each take 500ms. If the fan-out runs them sequentially the
	// whole search takes >=1s; if concurrent it takes ~500ms. The assertion
	// below is generous (700ms) to avoid flakiness on slow CI but is well
	// below the 1000ms a sequential run would guarantee.
	a := &scriptedSearcher{name: "a", delay: 500 * time.Millisecond, results: []SearchResult{{Title: "a"}}}
	b := &scriptedSearcher{name: "b", delay: 500 * time.Millisecond, results: []SearchResult{{Title: "b"}}}
	f := NewFanout(a, b)

	start := time.Now()
	res, err := f.Search(context.Background(), SearchQuery{})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("got %d results, want 2", len(res))
	}
	if elapsed > 900*time.Millisecond {
		t.Errorf("search took %v; expected concurrent execution (<~1s)", elapsed)
	}
}

// --- failure tolerance ------------------------------------------------------

func TestFanout_SingleIndexerFailure_DoesNotFailSearch(t *testing.T) {
	ok := &scriptedSearcher{name: "ok", results: []SearchResult{{Title: "good"}}}
	bad := &scriptedSearcher{name: "bad", err: errors.New("boom")}
	f := NewFanout(ok, bad)

	res, err := f.Search(context.Background(), SearchQuery{})
	if err != nil {
		t.Fatalf("search returned error from one bad indexer: %v", err)
	}
	got := titleSet(res)
	if !got["good"] || len(res) != 1 {
		t.Errorf("got %v, want only [good]", got)
	}
}

func TestFanout_AllIndexersFail_ReturnsEmptyNotError(t *testing.T) {
	bad1 := &scriptedSearcher{name: "b1", err: errors.New("x")}
	bad2 := &scriptedSearcher{name: "b2", err: errors.New("y")}
	f := NewFanout(bad1, bad2)
	res, err := f.Search(context.Background(), SearchQuery{})
	if err != nil {
		t.Fatalf("all-fail should not error: %v", err)
	}
	if len(res) != 0 {
		t.Errorf("got %d results, want 0", len(res))
	}
}

func TestFanout_EmptySearchers_ReturnsNil(t *testing.T) {
	f := NewFanout()
	res, err := f.Search(context.Background(), SearchQuery{})
	if err != nil {
		t.Fatalf("empty fanout should not error: %v", err)
	}
	if res != nil {
		t.Errorf("expected nil results for empty fanout, got %v", res)
	}
}

// --- per-indexer timeout ----------------------------------------------------

func TestFanout_PerIndexerTimeout_DropsSlowIndexer(t *testing.T) {
	fast := &scriptedSearcher{name: "fast", results: []SearchResult{{Title: "fast"}}}
	// slow blocks for 2s; the 100ms per-indexer timeout should cut it off.
	slow := &scriptedSearcher{name: "slow", delay: 2 * time.Second, results: []SearchResult{{Title: "slow"}}}

	f := NewFanoutWithTimeout([]Searcher{fast, slow}, 100*time.Millisecond)
	start := time.Now()
	res, err := f.Search(context.Background(), SearchQuery{})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	// "fast" returned; "slow" was cut off by its timeout.
	if titleSet(res)["slow"] {
		t.Errorf("slow indexer should have been timed out, but its result is present")
	}
	if !titleSet(res)["fast"] {
		t.Errorf("fast indexer result missing")
	}
	// The whole call must return shortly after the slow one is dropped, not
	// wait the full 2s.
	if elapsed > 1500*time.Millisecond {
		t.Errorf("search took %v; slow indexer should have been cut off at ~100ms", elapsed)
	}
}

// --- parent context cancellation -------------------------------------------

func TestFanout_ParentContextCancellation_FailsSearch(t *testing.T) {
	a := &scriptedSearcher{name: "a", results: []SearchResult{{Title: "a"}}}
	f := NewFanout(a)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel
	_, err := f.Search(ctx, SearchQuery{})
	if err == nil {
		t.Fatal("expected an error from a pre-cancelled parent context, got nil")
	}
}
