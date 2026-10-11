package paging

import (
	"net/url"
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
	p := Apply(seed(), Query{}, match)
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
	p := Apply(seed(), Query{Q: "DUNE"}, match)
	if p.Total != 2 {
		t.Fatalf("Total = %d, want 2 (dune, dune part two)", p.Total)
	}
}

func TestApply_MonitoredFilter(t *testing.T) {
	p := Apply(seed(), Query{Monitored: boolPtr(true)}, match)
	if p.Total != 3 {
		t.Errorf("monitored=true Total = %d, want 3", p.Total)
	}
	p = Apply(seed(), Query{Monitored: boolPtr(false)}, match)
	if p.Total != 2 {
		t.Errorf("monitored=false Total = %d, want 2", p.Total)
	}
}

func TestApply_CombinedFilters(t *testing.T) {
	p := Apply(seed(), Query{Q: "dune", Monitored: boolPtr(true)}, match)
	if p.Total != 1 || p.Items[0].Name != "Dune" {
		t.Errorf("want just Dune, got %+v", p.Items)
	}
}

func TestApply_Pagination(t *testing.T) {
	p := Apply(seed(), Query{Page: 2, PageSize: 2}, match)
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
	p := Apply(seed(), Query{Page: 99, PageSize: 1}, match)
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
	p := Apply(seed(), Query{Page: -3, PageSize: 0}, match)
	if p.Page != 1 || p.PageSize != DefaultPageSize {
		t.Errorf("page/size = %d/%d, want 1/%d", p.Page, p.PageSize, DefaultPageSize)
	}
}

func TestApply_HugePageSize_Clamped(t *testing.T) {
	p := Apply(seed(), Query{PageSize: 10_000}, match)
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
