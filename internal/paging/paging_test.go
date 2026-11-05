package paging

import (
	"net/url"
	"strings"
	"testing"
)

type item struct {
	Name      string
	Monitored bool
}

func match(it item) (string, bool) { return it.Name, it.Monitored }

func seed() []item {
	return []item{
		{Name: "Dune", Monitored: true},
		{Name: "dune part two", Monitored: false},
		{Name: "Inception", Monitored: true},
		{Name: "Oppenheimer", Monitored: true},
		{Name: "The Matrix", Monitored: false},
	}
}

func boolPtr(b bool) *bool { return &b }

func TestApply_NoFilters_ReturnsDefaultPage(t *testing.T) {
	p := Apply(seed(), Query{}, match, nil)
	if p.Total != 5 {
		t.Errorf("Total = %d, want 5", p.Total)
	}
	if p.Page != 1 || p.PageSize != DefaultPageSize {
		t.Errorf("page/size = %d/%d, want 1/%d", p.Page, p.PageSize, DefaultPageSize)
	}
	if len(p.Items) != 5 {
		t.Errorf("len(Items) = %d, want 5 (default page size > 5)", len(p.Items))
	}
}

func TestApply_Q_IsCaseInsensitiveSubstring(t *testing.T) {
	p := Apply(seed(), Query{Q: "DUNE"}, match, nil)
	if p.Total != 2 {
		t.Fatalf("Total = %d, want 2 (dune, dune part two)", p.Total)
	}
}

func TestApply_MonitoredFilter(t *testing.T) {
	p := Apply(seed(), Query{Monitored: boolPtr(true)}, match, nil)
	if p.Total != 3 {
		t.Errorf("monitored=true Total = %d, want 3", p.Total)
	}
	p = Apply(seed(), Query{Monitored: boolPtr(false)}, match, nil)
	if p.Total != 2 {
		t.Errorf("monitored=false Total = %d, want 2", p.Total)
	}
}

func TestApply_CombinedFilters(t *testing.T) {
	p := Apply(seed(), Query{Q: "dune", Monitored: boolPtr(true)}, match, nil)
	if p.Total != 1 || p.Items[0].Name != "Dune" {
		t.Errorf("want just Dune, got %+v", p.Items)
	}
}

func TestApply_Pagination(t *testing.T) {
	p := Apply(seed(), Query{Page: 2, PageSize: 2}, match, nil)
	if p.Total != 5 {
		t.Fatalf("Total = %d, want 5", p.Total)
	}
	if len(p.Items) != 2 {
		t.Fatalf("len(Items) = %d, want 2", len(p.Items))
	}
	if p.Items[0].Name != "Inception" || p.Items[1].Name != "Oppenheimer" {
		t.Errorf("page 2 = %+v, want Inception, Oppenheimer", p.Items)
	}
}

func TestApply_PagePastEnd_ReturnsEmptySlice(t *testing.T) {
	p := Apply(seed(), Query{Page: 99, PageSize: 1}, match, nil)
	if p.Total != 5 {
		t.Errorf("Total = %d, want 5", p.Total)
	}
	if p.Items == nil {
		t.Error("Items must be non-nil (JSON []) on an empty page")
	}
	if len(p.Items) != 0 {
		t.Errorf("len(Items) = %d, want 0", len(p.Items))
	}
}

func TestApply_NegativePageAndZeroSize_ClampToDefaults(t *testing.T) {
	p := Apply(seed(), Query{Page: -3, PageSize: 0}, match, nil)
	if p.Page != 1 || p.PageSize != DefaultPageSize {
		t.Errorf("page/size = %d/%d, want 1/%d", p.Page, p.PageSize, DefaultPageSize)
	}
}

func TestApply_HugePageSize_Clamped(t *testing.T) {
	p := Apply(seed(), Query{PageSize: 10_000}, match, nil)
	if p.PageSize != MaxPageSize {
		t.Errorf("PageSize = %d, want %d", p.PageSize, MaxPageSize)
	}
	if len(p.Items) != 5 {
		t.Errorf("len(Items) = %d, want 5 (all fit in the clamped page)", len(p.Items))
	}
}

func TestQueryFromURLValues(t *testing.T) {
	q := QueryFromURLValues(url.Values{
		"q":         {"  Dune  "},
		"monitored": {"true"},
		"page":      {"3"},
		"page_size": {"10"},
	})
	if q.Q != "Dune" {
		t.Errorf("Q = %q, want trimmed Dune", q.Q)
	}
	if q.Monitored == nil || !*q.Monitored {
		t.Errorf("Monitored = %v, want true", q.Monitored)
	}
	if q.Page != 3 || q.PageSize != 10 {
		t.Errorf("page/size = %d/%d, want 3/10", q.Page, q.PageSize)
	}
}

func TestQueryFromURLValues_MalformedValuesFallBack(t *testing.T) {
	q := QueryFromURLValues(url.Values{
		"monitored": {"banana"}, // not a bool -> ignored
		"page":      {"x"},      // not an int -> 0 -> page 1
		"page_size": {"-4"},     // negative -> default
	})
	if q.Monitored != nil {
		t.Errorf("Monitored = %v, want nil for non-bool", *q.Monitored)
	}
	if q.Page != 0 || q.PageSize != -4 {
		t.Errorf("page/size = %d/%d, want raw 0/-4 (Normalize clamps later)", q.Page, q.PageSize)
	}
	q.Normalize()
	if q.Page != 1 || q.PageSize != DefaultPageSize {
		t.Errorf("after Normalize page/size = %d/%d, want 1/%d", q.Page, q.PageSize, DefaultPageSize)
	}
}

func TestApply_SortSurfacesRowsBeyondFirstPage(t *testing.T) {
	// Natural order is the insertion order A..E. At a page size of 2 the
	// natural page 1 is A,B, so E and D sit on later pages. A descending sort
	// must move E,D to page 1 (#70: the client used to sort only the fetched
	// page, so E could never surface regardless of the sort selection).
	items := []item{{Name: "A"}, {Name: "B"}, {Name: "C"}, {Name: "D"}, {Name: "E"}}
	desc := func(a, b item) int { return strings.Compare(b.Name, a.Name) }
	p := Apply(items, Query{Page: 1, PageSize: 2, Sort: "desc"}, match, desc)
	if p.Total != 5 {
		t.Fatalf("Total = %d, want 5", p.Total)
	}
	if len(p.Items) != 2 {
		t.Fatalf("len(Items) = %d, want 2", len(p.Items))
	}
	if p.Items[0].Name != "E" || p.Items[1].Name != "D" {
		t.Errorf("sorted page 1 = %q,%q, want E,D", p.Items[0].Name, p.Items[1].Name)
	}
}

func TestApply_NilCmpKeepsNaturalOrder(t *testing.T) {
	// A nil comparator (no sort, or a kind without one yet) must preserve the
	// natural order — the pre-#70 behavior every other kind relies on.
	items := []item{{Name: "B"}, {Name: "A"}, {Name: "C"}}
	p := Apply(items, Query{Sort: "ignored"}, match, nil)
	if p.Items[0].Name != "B" || p.Items[1].Name != "A" || p.Items[2].Name != "C" {
		t.Errorf("nil cmp must keep natural order, got %+v", p.Items)
	}
}

func TestApply_SortAppliesAfterFilter(t *testing.T) {
	// The monitored filter must run before the sort, so the sort reorders the
	// surviving (filtered) set, not the raw input.
	mon := boolPtr(true)
	p := Apply(seed(), Query{Monitored: mon, Sort: "desc"}, match,
		func(a, b item) int { return strings.Compare(b.Name, a.Name) })
	// Monitored seed items: Dune, Inception, Oppenheimer. Descending by name:
	// Oppenheimer, Inception, Dune.
	if p.Total != 3 {
		t.Fatalf("Total = %d, want 3 (monitored)", p.Total)
	}
	want := []string{"Oppenheimer", "Inception", "Dune"}
	for i, w := range want {
		if p.Items[i].Name != w {
			t.Errorf("sorted[%d] = %q, want %q", i, p.Items[i].Name, w)
		}
	}
}

func TestQueryFromURLValues_ParsesSort(t *testing.T) {
	q := QueryFromURLValues(url.Values{"sort": {"  added-desc  "}})
	if q.Sort != "added-desc" {
		t.Errorf("Sort = %q, want trimmed added-desc", q.Sort)
	}
}
