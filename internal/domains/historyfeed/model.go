// Package historyfeed is the pure domain for the global Activity > History
// feed (P3): every lifecycle event across all four media kinds, unified so
// the UI can show one date/event-filterable feed and deep-link each row to
// its media item. It is the cross-kind counterpart of the per-kind
// *_history tables and of the queue view's per-parent history drill-down.
// No I/O lives here.
package historyfeed

import (
	"context"
	"strings"
	"time"
)

// Item is one event in the global feed. Kind + ParentID identify the media
// item the event belongs to (the UI links TV rows to /tv/{id}); Name is its
// display name, resolved by the repository at read time.
type Item struct {
	Kind     string    `json:"kind"`
	ParentID int64     `json:"parent_id"`
	Name     string    `json:"name"`
	Event    string    `json:"event"`
	Detail   string    `json:"detail"`
	At       time.Time `json:"at"`
}

// Filter narrows the feed. Since is an inclusive lower bound on the event
// time; Event is an exact (case-insensitive) event-type match. Limit caps
// how many rows are returned (see Normalize).
type Filter struct {
	Since *time.Time
	Event string
	Limit int
}

// DefaultLimit is used when a caller does not pass ?limit.
const DefaultLimit = 50

// MaxLimit bounds ?limit so a hostile or buggy client cannot request an
// unreasonably large page.
const MaxLimit = 200

// Normalize fills defaults and clamps Limit to [1, MaxLimit] in place.
func (f *Filter) Normalize() {
	f.Event = strings.TrimSpace(f.Event)
	if f.Limit <= 0 {
		f.Limit = DefaultLimit
	}
	if f.Limit > MaxLimit {
		f.Limit = MaxLimit
	}
}

// Apply narrows items in memory to those matching the filter, preserving the
// input order (the repository returns newest-first). The event comparison is
// case-insensitive. The returned slice is always non-nil (an empty result
// encodes as [] rather than null).
func (f Filter) Apply(in []Item) []Item {
	f.Normalize()
	out := make([]Item, 0, len(in))
	for _, it := range in {
		if f.Since != nil && it.At.Before(*f.Since) {
			continue
		}
		if f.Event != "" && !strings.EqualFold(it.Event, f.Event) {
			continue
		}
		out = append(out, it)
		if len(out) == f.Limit {
			break
		}
	}
	return out
}

// Repo is the feed's persistence surface.
type Repo interface {
	// List returns the feed events matching f, newest first.
	List(ctx context.Context, f Filter) ([]Item, error)
}
