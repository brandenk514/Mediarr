package blocklist

import (
	"testing"
	"time"
)

func TestKind_Valid(t *testing.T) {
	for _, k := range []Kind{KindMovie, KindTV, KindMusic, KindBook} {
		if !k.Valid() {
			t.Errorf("%q: want valid", k)
		}
	}
	for _, k := range []Kind{"", "MOVIE", "series", "anime"} {
		if k.Valid() {
			t.Errorf("%q: want invalid", k)
		}
	}
}

func TestFilter_Apply(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	in := []Entry{
		{ID: 1, Kind: KindMovie, ParentID: 1, ReleaseTitle: "Dune.2160p"},
		{ID: 2, Kind: KindTV, ParentID: 2, ReleaseTitle: "Severance.S01E01"},
		{ID: 3, Kind: KindTV, ParentID: 3, ReleaseTitle: "Wire.S01E01"},
		{ID: 4, Kind: KindMusic, ParentID: 4, ReleaseTitle: "Radiohead.flac"},
	}
	_ = now

	if got := (Filter{}).Apply(in); len(got) != 4 {
		t.Fatalf("zero filter: got %d, want 4", len(got))
	}
	if got := (Filter{Kind: KindTV}).Apply(in); len(got) != 2 || got[0].ID != 2 {
		t.Fatalf("kind filter: got %v", got)
	}
	got := (Filter{Kind: KindTV, ParentID: 3}).Apply(in)
	if len(got) != 1 || got[0].ID != 3 {
		t.Fatalf("kind+parent filter: got %v", got)
	}
	if got := (Filter{Kind: KindBook}).Apply(in); len(got) != 0 {
		t.Fatalf("no matches: got %v, want empty", got)
	}
}
