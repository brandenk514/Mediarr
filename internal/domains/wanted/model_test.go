package wanted

import (
	"testing"
	"time"

	tv "github.com/brandenk514/mediarr/internal/domains/tv"
)

func TestTab_ValidateAndNormalize(t *testing.T) {
	if !TabMissing.Valid() || !TabCutoff.Valid() {
		t.Fatal("missing/cutoff should be valid")
	}
	if Tab("nope").Valid() {
		t.Fatal("nope should not be valid")
	}
	if Tab("").Normalize() != TabMissing {
		t.Fatal("empty tab should default to missing")
	}
	if TabCutoff.Normalize() != TabCutoff {
		t.Fatal("cutoff should stay cutoff")
	}
}

func TestCutoffItem(t *testing.T) {
	// No items -> not evaluable.
	if CutoffItem(tv.Profile{Name: "x"}) != nil {
		t.Fatal("no items: want nil")
	}

	// Explicit cutoff enabled -> that item.
	p, _ := tv.ProfileByName("HD-1080p")
	if p.UseCutoff {
		// find the item id of the cutoff and check resolution
		want := CutoffItem(p)
		if want == nil {
			t.Fatal("cutoff profile: want non-nil")
		}
		if want.ID != p.CutoffID {
			t.Fatalf("cutoff item id = %d, want %d", want.ID, p.CutoffID)
		}
	}

	// Cutoff disabled (or absent id) -> lowest item (last in best-first list).
	p2 := p
	p2.UseCutoff = false
	got := CutoffItem(p2)
	if got == nil {
		t.Fatal("no cutoff: want lowest item")
	}
	if *got != p2.Items[len(p2.Items)-1] {
		t.Fatalf("lowest item = %v, want %v", *got, p2.Items[len(p2.Items)-1])
	}
}

func TestBelowCutoff(t *testing.T) {
	p, ok := tv.ProfileByName("HD-1080p")
	if !ok {
		t.Fatal("HD-1080p profile missing")
	}
	cut := CutoffItem(p)

	// Nil cutoff / "any" -> never below.
	if BelowCutoff("720p", nil) {
		t.Fatal("nil cutoff: want false")
	}
	if BelowCutoff("720p", &tv.ProfileItem{Quality: tv.CodecAny}) {
		t.Fatal("any cutoff: want false")
	}
	// Unknown file quality -> unevaluable -> false.
	if BelowCutoff("gibberish", cut) {
		t.Fatal("unknown file quality: want false")
	}
	// A file strictly below the cutoff resolution is flagged.
	if cut != nil && cut.Quality != tv.CodecAny {
		below := &tv.ProfileItem{Quality: "720p", Codec: tv.CodecAny, ID: 99}
		if !BelowCutoff("720p", &tv.ProfileItem{Quality: "1080p", Codec: tv.CodecAny, ID: 1}) {
			t.Fatal("720p below 1080p: want true")
		}
		_ = below
	}
}

func TestFilter_ApplyMissing(t *testing.T) {
	air := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	rows := []EpisodeRow{
		{SeriesID: 1, SeriesTitle: "Alpha", Season: 1, Episode: 1, Monitored: true, HasFile: false},
		{SeriesID: 1, SeriesTitle: "Alpha", Season: 1, Episode: 2, Monitored: true, HasFile: true, FileQuality: "720p"},
		{SeriesID: 1, SeriesTitle: "Alpha", Season: 1, Episode: 3, Monitored: false, HasFile: false},
		{SeriesID: 2, SeriesTitle: "Beta", Season: 1, Episode: 1, Monitored: true, HasFile: false, AirDate: &air},
	}
	got := Filter{Tab: TabMissing}.Apply(rows, nil)
	if len(got) != 2 {
		t.Fatalf("missing: got %d, want 2", len(got))
	}
	if got[0].Status != "missing:no file" {
		t.Fatalf("status = %q", got[0].Status)
	}
	if got[0].Kind != "tv" || got[1].SeriesID != 2 {
		t.Fatalf("rows = %+v", got)
	}
	// Q narrows by series title, case-insensitive.
	got = Filter{Tab: TabMissing, Q: "alpha"}.Apply(rows, nil)
	if len(got) != 1 || got[0].SeriesTitle != "Alpha" {
		t.Fatalf("q filter: got %v", got)
	}
}

func TestFilter_ApplyCutoff(t *testing.T) {
	cut1080 := &tv.ProfileItem{Quality: "1080p", Codec: tv.CodecAny, ID: 1}
	cut720 := &tv.ProfileItem{Quality: "720p", Codec: tv.CodecAny, ID: 2}
	cutoffs := map[int64]*tv.ProfileItem{
		1: cut1080, // series 1 cutoff is 1080p
		2: cut720,  // series 2 cutoff is 720p
		3: nil,     // series 3 not evaluable
	}
	rows := []EpisodeRow{
		{SeriesID: 1, SeriesTitle: "A", Season: 1, Episode: 1, Monitored: true, HasFile: true, FileQuality: "720p"},  // below 1080 -> in
		{SeriesID: 1, SeriesTitle: "A", Season: 1, Episode: 2, Monitored: true, HasFile: true, FileQuality: "1080p"}, // at cutoff -> out
		{SeriesID: 2, SeriesTitle: "B", Season: 1, Episode: 1, Monitored: true, HasFile: true, FileQuality: "720p"},  // at cutoff -> out
		{SeriesID: 3, SeriesTitle: "C", Season: 1, Episode: 1, Monitored: true, HasFile: true, FileQuality: "720p"},  // not evaluable -> out
		{SeriesID: 1, SeriesTitle: "A", Season: 1, Episode: 3, Monitored: false, HasFile: true, FileQuality: "720p"}, // unmonitored -> out
		{SeriesID: 1, SeriesTitle: "A", Season: 1, Episode: 4, Monitored: true, HasFile: false},                      // no file -> out of cutoff tab
	}
	got := Filter{Tab: TabCutoff}.Apply(rows, cutoffs)
	if len(got) != 1 {
		t.Fatalf("cutoff: got %d, want 1; %+v", len(got), got)
	}
	want := "cutoff:720p below cutoff 1080p"
	if got[0].Status != want {
		t.Fatalf("status = %q, want %q", got[0].Status, want)
	}
}

func TestFilter_ApplyUnknownTab(t *testing.T) {
	got := Filter{Tab: "bogus"}.Apply(nil, nil)
	if got == nil || len(got) != 0 {
		t.Fatalf("unknown tab: want non-nil empty, got %v", got)
	}
}
