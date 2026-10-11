// Package paging is the shared server-side pagination and filtering
// primitive for the per-kind list endpoints (M6 #41). It is pure (no I/O,
// no stdlib-internal state) so the filter/page math is unit-testable in
// isolation.
//
// The v1 list contract: every top-level per-kind list (movies, tv series,
// music artists, book authors) is filtered and paginated server-side and
// returned as a Page envelope {items, total, page, page_size}. Child
// collections (episodes, albums, tracks, titles, editions) are fetched
// lazily by the UI on expand and remain plain arrays.
package paging

import (
	"net/url"
	"strconv"
	"strings"
)

// Query carries a list request's query parameters.
type Query struct {
	// Q is a case-insensitive substring search on the item's name.
	Q string
	// Monitored, when non-nil, keeps only items whose monitored flag equals
	// *Monitored. Nil means no monitoring filter.
	Monitored *bool
	// Page is the 1-based page number. <= 0 means the first page.
	Page int
	// PageSize is the page size. <= 0 means the default (25).
	PageSize int
}

// DefaultPageSize is used when the caller does not pass ?page_size.
const DefaultPageSize = 25

// MaxPageSize bounds ?page_size so a hostile or buggy client cannot request
// an unreasonably large page (mirrors the dashboard's recent clamp).
const MaxPageSize = 100

// Page is one page of a paginated list.
type Page[T any] struct {
	Items    []T `json:"items"`
	Total    int `json:"total"`
	Page     int `json:"page"`
	PageSize int `json:"page_size"`
}

// Normalize fills defaults (page 1, page size DefaultPageSize) and clamps
// PageSize to [1, MaxPageSize] in place. Callers should invoke it before
// doing offset math so out-of-range input can never panic or underflow.
func (q *Query) Normalize() {
	if q.Page <= 0 {
		q.Page = 1
	}
	if q.PageSize <= 0 {
		q.PageSize = DefaultPageSize
	}
	if q.PageSize > MaxPageSize {
		q.PageSize = MaxPageSize
	}
}

// QueryFromURLValues builds a Query from parsed URL query values. Recognized
// keys: q, monitored (bool), page, page_size. Missing or malformed values
// fall back to defaults rather than erroring: a list request is never made
// invalid by a bad query parameter, it just broadens or shifts.
func QueryFromURLValues(v url.Values) Query {
	q := Query{Q: strings.TrimSpace(v.Get("q"))}
	if m := v.Get("monitored"); m != "" {
		if b, err := strconv.ParseBool(m); err == nil {
			q.Monitored = &b
		}
	}
	page, _ := strconv.Atoi(v.Get("page"))
	q.Page = page
	size, _ := strconv.Atoi(v.Get("page_size"))
	q.PageSize = size
	return q
}

// Apply filters items with Q (case-insensitive substring on name) and
// Monitored, then returns the requested page. Total counts matches before
// paging. The match callback extracts each item's searchable name and
// monitored flag so this one function serves all four kinds.
//
// The returned page always has a non-nil Items slice so the JSON encoding is
// [] (not null) for an empty result.
func Apply[T any](items []T, q Query, match func(T) (name string, monitored bool)) Page[T] {
	q.Normalize()
	needle := strings.ToLower(q.Q)
	filtered := make([]T, 0, len(items))
	for _, it := range items {
		name, monitored := match(it)
		if q.Monitored != nil && monitored != *q.Monitored {
			continue
		}
		if needle != "" && !strings.Contains(strings.ToLower(name), needle) {
			continue
		}
		filtered = append(filtered, it)
	}
	total := len(filtered)
	start := (q.Page - 1) * q.PageSize
	if start >= total {
		return Page[T]{Items: []T{}, Total: total, Page: q.Page, PageSize: q.PageSize}
	}
	end := start + q.PageSize
	if end > total {
		end = total
	}
	return Page[T]{Items: filtered[start:end], Total: total, Page: q.Page, PageSize: q.PageSize}
}
