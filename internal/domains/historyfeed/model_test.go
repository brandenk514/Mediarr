package historyfeed

import (
	"testing"
	"time"
)

func ts(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestFilter_Normalize(t *testing.T) {
	f := Filter{}
	f.Normalize()
	if f.Limit != DefaultLimit {
		t.Errorf("zero limit: got %d, want %d", f.Limit, DefaultLimit)
	}
	f = Filter{Limit: 10_000}
	f.Normalize()
	if f.Limit != MaxLimit {
		t.Errorf("huge limit: got %d, want %d", f.Limit, MaxLimit)
	}
	f = Filter{Limit: 7, Event: "  imported "}
	f.Normalize()
	if f.Limit != 7 || f.Event != "imported" {
		t.Errorf("limit/event: got %d/%q", f.Limit, f.Event)
	}
}

func TestFilter_Apply(t *testing.T) {
	in := []Item{
		{Kind: "movie", ParentID: 1, Name: "Dune", Event: "imported", At: ts("2026-10-11T12:00:00Z")},
		{Kind: "tv", ParentID: 2, Name: "Severance", Event: "search-requested", At: ts("2026-10-11T11:00:00Z")},
		{Kind: "tv", ParentID: 2, Name: "Severance", Event: "added", At: ts("2026-10-10T11:00:00Z")},
		{Kind: "music", ParentID: 3, Name: "Radiohead", Event: "downloaded", At: ts("2026-10-09T11:00:00Z")},
	}

	// Since is an inclusive lower bound.
	since := ts("2026-10-11T11:00:00Z")
	got := Filter{Since: &since}.Apply(in)
	if len(got) != 2 || got[0].ParentID != 1 {
		t.Fatalf("since: got %v", got)
	}

	// Event match is case-insensitive.
	got = Filter{Event: "IMPORTED"}.Apply(in)
	if len(got) != 1 || got[0].Kind != "movie" {
		t.Fatalf("event: got %v", got)
	}

	// Limit caps the result; combined with since + event it still works.
	got = Filter{Since: &since, Limit: 1}.Apply(in)
	if len(got) != 1 {
		t.Fatalf("limit: got %v", got)
	}

	// No match -> non-nil empty slice.
	got = Filter{Event: "nope"}.Apply(in)
	if got == nil || len(got) != 0 {
		t.Fatalf("no match: got %v, want non-nil empty", got)
	}
}
