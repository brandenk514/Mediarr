// Package discover is the unified Discover page's service layer (issue
// #118, work order A1): it composes the TMDB trending / now-playing /
// recent-TV feeds with the calendar's upcoming rows into the page's cells
// (trending, upcoming, newReleases × movie/tv).
//
// Per-cell failure NEVER fails the request: an unavailable or
// misconfigured feed becomes a 200 response with the cell named in
// `unavailable` (CellError), mirroring the R3 keyless wording. Only a
// total handler failure is a 5xx. Each (row × kind) cell is capped at
// cellCap items server-side.
package discover

import (
	"context"
	"errors"
	"strconv"
	"time"

	cdom "github.com/brandenk514/mediarr/internal/domains/calendar"
	"github.com/brandenk514/mediarr/internal/providers"
)

const (
	// cellCap is the server-side cap on items per (row × kind) cell.
	cellCap = 6
	// recentWindowDays is how far back the new_releases:tv cell reaches
	// (first_air_date within the last ~30 days).
	recentWindowDays = 30
	// upcomingWindowDays is the upcoming row's horizon: today .. today+14d.
	upcomingWindowDays = 14
)

// reasonTMDBNotConfigured mirrors the R3 keyless wording so a degraded
// cell reads the same as the Radarr R3 page's 503 reason.
const reasonTMDBNotConfigured = "tmdb is not configured (missing API key)"

// Item is one cell row, in the OpenAPI DiscoverItem wire shape.
type Item struct {
	Kind      string `json:"kind"`
	SourceID  string `json:"sourceId"`
	Title     string `json:"title"`
	Year      int    `json:"year"`
	Subtitle  string `json:"subtitle"`
	PosterURL string `json:"posterUrl"`
	// Date is RFC3339; "" when n/a.
	Date string `json:"date"`
}

// CellError names one unavailable feed cell. Cell is "<kind>:<row>"
// (e.g. "tv:trending").
type CellError struct {
	Cell   string `json:"cell"`
	Reason string `json:"reason"`
}

// Response is the OpenAPI DiscoverPage wire shape: the three rows plus the
// unavailable cells. All arrays serialize as [] (never null).
type Response struct {
	Trending    []Item      `json:"trending"`
	Upcoming    []Item      `json:"upcoming"`
	NewReleases []Item      `json:"newReleases"`
	Unavailable []CellError `json:"unavailable"`
}

// TMDBFeeds is the narrow TMDB surface the unified Discover page needs
// (issue #118 A1). *providers.TMDB satisfies it; tests use a fake.
type TMDBFeeds interface {
	TrendingMovies(ctx context.Context) ([]providers.DiscoverItem, error)
	TrendingTV(ctx context.Context) ([]providers.DiscoverItem, error)
	NowPlayingMovies(ctx context.Context) ([]providers.DiscoverItem, error)
	RecentTV(ctx context.Context, since, until time.Time) ([]providers.DiscoverItem, error)
}

// Calendar is the narrow calendar surface: upcoming rows per kind.
// *services/calendar.Service satisfies it; tests use a fake.
type Calendar interface {
	Calendar(ctx context.Context, kind cdom.Kind, start, end time.Time) ([]cdom.Entry, error)
}

// Service composes the Discover cells from its two dependencies.
type Service struct {
	tmdb     TMDBFeeds // nil → every TMDB cell unavailable
	calendar Calendar  // nil → both upcoming cells unavailable
	now      func() time.Time
}

// New builds the service. Either dependency may be nil (keyless dev /
// partial wiring): the page still answers 200 with the affected cells in
// `unavailable`.
func New(tmdb TMDBFeeds, cal Calendar) *Service {
	return &Service{tmdb: tmdb, calendar: cal, now: time.Now}
}

// Discover assembles the page. Per-cell failures are captured as CellError
// entries in deterministic cell order: movie:trending, tv:trending,
// movie:upcoming, tv:upcoming, movie:new_releases, tv:new_releases. The
// returned error is reserved for total (non-cell) failures.
func (s *Service) Discover(ctx context.Context) (*Response, error) {
	now := s.now()
	res := &Response{
		Trending:    []Item{},
		Upcoming:    []Item{},
		NewReleases: []Item{},
		Unavailable: []CellError{},
	}

	addTMDBCell := func(kind, row string, fetch func() ([]providers.DiscoverItem, error)) {
		if s.tmdb == nil {
			res.Unavailable = append(res.Unavailable, CellError{Cell: kind + ":" + row, Reason: reasonTMDBNotConfigured})
			return
		}
		items, err := fetch()
		if err != nil {
			res.Unavailable = append(res.Unavailable, CellError{Cell: kind + ":" + row, Reason: tmdbReason(err)})
			return
		}
		cell := make([]Item, 0, len(items))
		for _, it := range items {
			cell = append(cell, toItem(it))
		}
		capCell(&cell)
		switch row {
		case "trending":
			res.Trending = append(res.Trending, cell...)
		case "new_releases":
			res.NewReleases = append(res.NewReleases, cell...)
		}
	}

	addTMDBCell("movie", "trending", func() ([]providers.DiscoverItem, error) {
		return s.tmdb.TrendingMovies(ctx)
	})
	addTMDBCell("tv", "trending", func() ([]providers.DiscoverItem, error) {
		return s.tmdb.TrendingTV(ctx)
	})

	// upcoming: [today, today+upcomingWindowDays] from the calendar
	// (the domain also drops already-airing/released rows).
	addCalendarCell := func(kind string, k cdom.Kind) {
		if s.calendar == nil {
			res.Unavailable = append(res.Unavailable, CellError{Cell: kind + ":upcoming", Reason: "calendar is not configured"})
			return
		}
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		end := start.AddDate(0, 0, upcomingWindowDays)
		entries, err := s.calendar.Calendar(ctx, k, start, end)
		if err != nil {
			res.Unavailable = append(res.Unavailable, CellError{Cell: kind + ":upcoming", Reason: err.Error()})
			return
		}
		cell := make([]Item, 0, len(entries))
		for _, e := range entries {
			cell = append(cell, toCalendarItem(e))
		}
		capCell(&cell)
		res.Upcoming = append(res.Upcoming, cell...)
	}
	addCalendarCell("movie", cdom.KindMovie)
	addCalendarCell("tv", cdom.KindTV)

	addTMDBCell("movie", "new_releases", func() ([]providers.DiscoverItem, error) {
		return s.tmdb.NowPlayingMovies(ctx)
	})
	addTMDBCell("tv", "new_releases", func() ([]providers.DiscoverItem, error) {
		since := now.AddDate(0, 0, -recentWindowDays)
		return s.tmdb.RecentTV(ctx, since, now)
	})

	return res, nil
}

// capCell truncates a cell to cellCap items (the server-side cap per
// (row × kind) cell).
func capCell(cell *[]Item) {
	if len(*cell) > cellCap {
		*cell = (*cell)[:cellCap]
	}
}

// tmdbReason maps a feed error to the cell's user-facing reason: the
// not-configured sentinel keeps the R3 keyless wording, everything else
// passes through.
func tmdbReason(err error) string {
	if errors.Is(err, providers.ErrNotConfigured) {
		return reasonTMDBNotConfigured
	}
	return err.Error()
}

// toItem folds a provider feed row into the wire Item (date as RFC3339).
func toItem(it providers.DiscoverItem) Item {
	out := Item{
		Kind:      it.Kind,
		SourceID:  it.SourceID,
		Title:     it.Title,
		Year:      it.Year,
		Subtitle:  it.Subtitle,
		PosterURL: it.PosterURL,
	}
	if !it.Date.IsZero() {
		out.Date = it.Date.UTC().Format(time.RFC3339)
	}
	return out
}

// toCalendarItem folds an upcoming calendar entry into an Item: the entry's
// local-library id as SourceID, the air/release start as the date, and the
// episode's SxE as the TV meta line (movies carry none).
func toCalendarItem(e cdom.Entry) Item {
	out := Item{
		Kind:     e.Kind,
		SourceID: strconv.FormatInt(e.SeriesID, 10),
		Title:    e.SeriesTitle,
		Year:     e.Year,
	}
	if e.SXE != "" {
		out.Subtitle = e.SXE
	}
	if !e.AirStart.IsZero() {
		out.Date = e.AirStart.UTC().Format(time.RFC3339)
	}
	return out
}
