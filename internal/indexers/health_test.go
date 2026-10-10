package indexers

import (
	"testing"
	"time"
)

// TestHealthTracker_RecordsSuccessAndFailure is the #34 core test: the tracker
// rolls up per-indexer success/fail counts, last success, last error, and
// latency.
func TestHealthTracker_RecordsSuccessAndFailure(t *testing.T) {
	tk := NewHealthTracker()

	tk.Record("alpha", true, 100*time.Millisecond, "")
	tk.Record("alpha", true, 300*time.Millisecond, "")
	tk.Record("alpha", false, 500*time.Millisecond, "status 429")
	tk.Record("beta", true, 50*time.Millisecond, "")

	// alpha: 3 calls, 2 success, 1 failure, avg latency = (100+300+500)/3 = 300ms
	a := tk.Stats("alpha")
	if a.Name != "alpha" {
		t.Fatalf("expected name alpha, got %q", a.Name)
	}
	if a.Successes != 2 {
		t.Errorf("alpha: expected 2 successes, got %d", a.Successes)
	}
	if a.Failures != 1 {
		t.Errorf("alpha: expected 1 failure, got %d", a.Failures)
	}
	if a.Calls != 3 {
		t.Errorf("alpha: expected 3 calls, got %d", a.Calls)
	}
	if a.LastError != "status 429" {
		t.Errorf("alpha: expected last error 'status 429', got %q", a.LastError)
	}
	if a.LastSuccess == nil {
		t.Error("alpha: expected non-nil LastSuccess")
	}
	// Avg latency 300ms (total 900ms / 3).
	if a.AvgLatency != 300*time.Millisecond {
		t.Errorf("alpha: expected avg 300ms, got %v (total %v)", a.AvgLatency, a.TotalLatencyNs)
	}
	// TotalLatencyNs must reflect every call including the most recent one —
	// a regression guard: it once published the sum *before* adding the
	// current call's latency, so it trailed the true total by the last call.
	if want := int64(900 * time.Millisecond); a.TotalLatencyNs != want {
		t.Errorf("alpha: expected TotalLatencyNs %v, got %v", want, a.TotalLatencyNs)
	}

	// beta: 1 call, 1 success.
	b := tk.Stats("beta")
	if b.Successes != 1 || b.Calls != 1 {
		t.Errorf("beta: expected 1 success / 1 call, got %+v", b)
	}
}

// TestHealthTracker_UnknownStats returns zero values with the right name.
func TestHealthTracker_UnknownStats(t *testing.T) {
	tk := NewHealthTracker()
	s := tk.Stats("ghost")
	if s.Name != "ghost" {
		t.Fatalf("expected name ghost, got %q", s.Name)
	}
	if s.Calls != 0 || s.Successes != 0 || s.Failures != 0 {
		t.Errorf("expected zero stats, got %+v", s)
	}
}

// TestHealthTracker_AllSorted confirms All() returns every known indexer,
// sorted by name (the ordering the status API relies on for stable output).
func TestHealthTracker_AllSorted(t *testing.T) {
	tk := NewHealthTracker()
	tk.Record("zeta", true, time.Millisecond, "")
	tk.Record("alpha", true, time.Millisecond, "")
	tk.Record("mid", false, time.Millisecond, "boom")

	all := tk.All()
	if len(all) != 3 {
		t.Fatalf("expected 3 indexers, got %d", len(all))
	}
	want := []string{"alpha", "mid", "zeta"}
	for i, name := range want {
		if all[i].Name != name {
			t.Fatalf("expected order %v, got %v at index %d", want, names(all), i)
		}
	}
}

// TestHealthTracker_Reset clears one indexer's rollup, leaving others intact.
func TestHealthTracker_Reset(t *testing.T) {
	tk := NewHealthTracker()
	tk.Record("a", true, time.Millisecond, "")
	tk.Record("b", true, time.Millisecond, "")
	tk.Reset("a")

	if s := tk.Stats("a"); s.Calls != 0 {
		t.Errorf("expected a reset to zero, got %+v", s)
	}
	if s := tk.Stats("b"); s.Calls != 1 {
		t.Errorf("expected b untouched, got %+v", s)
	}
	if all := tk.All(); len(all) != 1 || all[0].Name != "b" {
		t.Errorf("expected only b in All() after reset, got %v", names(all))
	}
}

func names(all []IndexerStats) []string {
	out := make([]string, len(all))
	for i, s := range all {
		out[i] = s.Name
	}
	return out
}
