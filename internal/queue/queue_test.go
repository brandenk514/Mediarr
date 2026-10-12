package queue

import (
	"testing"
	"time"
)

func item(kind Kind, parent string, release, state string) Item {
	return Item{
		Kind:         kind,
		ParentID:     1,
		ParentName:   parent,
		ReleaseTitle: release,
		State:        state,
		UpdatedAt:    time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC),
	}
}

func TestApply_DefaultsReturnFirstPage(t *testing.T) {
	items := []Item{
		item(KindMovie, "Dune", "Dune.Part.Two.2160p", "downloading"),
		item(KindTV, "Severance", "Severance.S01E01", "queued"),
		item(KindMusic, "Radiohead", "Kid.A.1994.flac", "complete"),
		item(KindBook, "Tolkien", "Hobbit.epub", "failed"),
	}
	page, total := Apply(items, Filter{})
	if total != 4 {
		t.Fatalf("total = %d, want 4", total)
	}
	if len(page) != 4 {
		t.Fatalf("len(page) = %d, want 4", len(page))
	}
}

func TestApply_NormalizePageDefaults(t *testing.T) {
	f := Filter{}
	f.Normalize()
	if f.Page != 1 || f.PageSize != DefaultPageSize {
		t.Fatalf("Normalize = (%d,%d), want (1,%d)", f.Page, f.PageSize, DefaultPageSize)
	}
	f2 := Filter{Page: 0, PageSize: 99999}
	f2.Normalize()
	if f2.PageSize != MaxPageSize {
		t.Fatalf("PageSize = %d, want clamped to %d", f2.PageSize, MaxPageSize)
	}
	if f2.Page != 1 {
		t.Fatalf("Page = %d, want 1", f2.Page)
	}
}

func TestApply_FiltersByKindAndState(t *testing.T) {
	items := []Item{
		item(KindMovie, "Dune", "Dune.Part.Two.2160p", "downloading"),
		item(KindTV, "Severance", "Severance.S01E01", "queued"),
		item(KindMovie, "Inception", "Inception.1080p", "queued"),
		item(KindTV, "Severance", "Severance.S01E02", "downloading"),
	}
	// Kind filter (case-insensitive).
	page, total := Apply(items, Filter{Kind: "MOVIE"})
	if total != 2 || len(page) != 2 {
		t.Fatalf("movie filter: total=%d len=%d, want 2/2", total, len(page))
	}
	// Kind + state.
	page, total = Apply(items, Filter{Kind: "tv", State: "DOWNLOADING"})
	if total != 1 || len(page) != 1 {
		t.Fatalf("tv+downloading: total=%d len=%d, want 1/1", total, len(page))
	}
	if page[0].ReleaseTitle != "Severance.S01E02" {
		t.Fatalf("wrong item: %q", page[0].ReleaseTitle)
	}
	// A state nobody has yields an empty (non-nil) page.
	page, total = Apply(items, Filter{State: "complete"})
	if total != 0 {
		t.Fatalf("complete: total = %d, want 0", total)
	}
	if page == nil {
		t.Fatal("empty page must be non-nil (encodes as [] not null)")
	}
}

func TestApply_QMatchesReleaseOrParent(t *testing.T) {
	items := []Item{
		item(KindMovie, "Dune", "Dune.Part.Two.2160p", "downloading"),
		item(KindTV, "Severance", "Severance.S01E01", "queued"),
		item(KindMusic, "Radiohead", "In Rainbows.1994.flac", "queued"),
	}
	// Matches the release title.
	page, total := Apply(items, Filter{Q: "part.two"})
	if total != 1 || page[0].ParentName != "Dune" {
		t.Fatalf("release match: total=%d got=%q", total, page[0].ParentName)
	}
	// Matches the parent name (release does not contain it).
	page, total = Apply(items, Filter{Q: "radiohead"})
	if total != 1 || page[0].Kind != KindMusic {
		t.Fatalf("parent match: total=%d got=%q", total, page[0].Kind)
	}
	// No match.
	_, total = Apply(items, Filter{Q: "zzz-nothing"})
	if total != 0 {
		t.Fatalf("no match: total = %d, want 0", total)
	}
}

func TestApply_Pagination(t *testing.T) {
	items := make([]Item, 0, 7)
	for i := 0; i < 7; i++ {
		items = append(items, item(KindMovie, "m", "r", "queued"))
	}
	f := Filter{Page: 2, PageSize: 3}
	page, total := Apply(items, f)
	if total != 7 {
		t.Fatalf("total = %d, want 7", total)
	}
	if len(page) != 3 {
		t.Fatalf("page 2 len = %d, want 3", len(page))
	}
	// Page past the end is an empty non-nil page.
	page, total = Apply(items, Filter{Page: 10, PageSize: 3})
	if total != 7 || len(page) != 0 || page == nil {
		t.Fatalf("past-end page = %v, want [] total=7", page)
	}
	// Last partial page.
	page, total = Apply(items, Filter{Page: 3, PageSize: 3})
	if total != 7 || len(page) != 1 {
		t.Fatalf("last page len = %d, want 1 (total %d)", len(page), total)
	}
}
