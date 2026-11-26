// Package discover is the unified Discover page's service layer (issue
// #118): it composes the TMDB trending / now-playing / recent-TV feeds
// (A1), the MusicBrainz release-group / release feeds, the Open Library
// trending + first-publish-year feeds, the optional keyed Google Books
// feed, the calendar's upcoming rows, and the books library's upcoming
// titles into the page's cells (trending, upcoming, newReleases ×
// movie/tv/music/book).
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

	bdomain "github.com/brandenk514/mediarr/internal/domains/books"
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
	// musicTrendingWindowDays is how far back the trending:music cell
	// reaches (release-group first-release within the last ~30 days).
	// DOCUMENTED PROXY: MusicBrainz has no popularity signal (a true
	// popularity feed would need Last.fm-style data — deferred), so
	// "trending" here means "most recently first-released".
	musicTrendingWindowDays = 30
	// musicNewReleasesWindowDays is how far back the new_releases:music
	// cell reaches (release date within the last ~14 days).
	musicNewReleasesWindowDays = 14
)

// Not-configured reasons. The keyed ones mirror the R3 keyless wording so
// a degraded cell reads the same as the Radarr R3 page's 503 reason.
const (
	reasonTMDBNotConfigured        = "tmdb is not configured (missing API key)"
	reasonGoogleBooksNotConfigured = "google books is not configured (missing API key)"
	reasonMusicBrainzNotConfigured = "musicbrainz is not configured"
	reasonOpenLibraryNotConfigured = "openlibrary is not configured"
	reasonCalendarNotConfigured    = "calendar is not configured"
	reasonBooksNotConfigured       = "books library is not configured"
)

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

// MusicBrainzFeeds is the narrow MusicBrainz surface for the music cells
// (issue #118 A2). *providers.MusicBrainz satisfies it; tests use a fake.
type MusicBrainzFeeds interface {
	// TrendingReleaseGroups: the trending:music cell (the documented
	// "recently first-released" proxy; window [since, until]).
	TrendingReleaseGroups(ctx context.Context, since, until time.Time) ([]providers.DiscoverItem, error)
	// NewReleases: the new_releases:music cell (window [since, until]).
	NewReleases(ctx context.Context, since, until time.Time) ([]providers.DiscoverItem, error)
}

// OpenLibraryFeeds is the narrow Open Library surface for the book cells
// (issue #118 A2). *providers.OpenLibrary satisfies it; tests use a fake.
type OpenLibraryFeeds interface {
	// TrendingWeekly: the trending:book cell.
	TrendingWeekly(ctx context.Context) ([]providers.DiscoverItem, error)
	// NewReleasesByYear: the new_releases:book keyless fallback feed
	// (works first published in year).
	NewReleasesByYear(ctx context.Context, year int) ([]providers.DiscoverItem, error)
}

// GoogleBooksFeeds is the narrow Google Books surface for the
// new_releases:book cell (issue #118 A2). *providers.GoogleBooks
// satisfies it; tests use a fake.
type GoogleBooksFeeds interface {
	// NewReleasesBooks: the keyed primary feed (orderBy=newest).
	NewReleasesBooks(ctx context.Context) ([]providers.DiscoverItem, error)
}

// BookLibrary is the narrow library surface for the upcoming:book cell
// (issue #118 A2): monitored titles with a future edition that has not
// been imported yet. *services/books.Service satisfies it; tests use a
// fake.
type BookLibrary interface {
	ListBooks(ctx context.Context) ([]bdomain.Book, error)
	ListEditions(ctx context.Context, titleID int64) ([]bdomain.Edition, error)
}

// Calendar is the narrow calendar surface: upcoming rows per kind.
// *services/calendar.Service satisfies it; tests use a fake.
type Calendar interface {
	Calendar(ctx context.Context, kind cdom.Kind, start, end time.Time) ([]cdom.Entry, error)
}

// Deps are the unified Discover page's dependencies (issue #118). Each
// is optional: a nil dependency degrades its cells to CellError entries
// (the page still answers 200 — per-cell failure never fails the
// request).
type Deps struct {
	TMDB        TMDBFeeds
	Calendar    Calendar
	MusicBrainz MusicBrainzFeeds
	OpenLibrary OpenLibraryFeeds
	GoogleBooks GoogleBooksFeeds
	Books       BookLibrary
}

// Service composes the Discover cells from its dependencies.
type Service struct {
	deps Deps
	now  func() time.Time
}

// New builds the service over its (possibly partial) dependencies:
// keyless dev / partial wiring / health-only boot all stay 200 with the
// affected cells in `unavailable`.
func New(d Deps) *Service {
	return &Service{deps: d, now: time.Now}
}

// Discover assembles the page. Per-cell failures are captured as CellError
// entries in deterministic cell order: per row, movie, tv, music, book
// (movie:trending, tv:trending, music:trending, book:trending,
// movie:upcoming, ..., movie:new_releases, tv:new_releases,
// music:new_releases, book:new_releases). The returned error is reserved
// for total (non-cell) failures.
func (s *Service) Discover(ctx context.Context) (*Response, error) {
	now := s.now()
	res := &Response{
		Trending:    []Item{},
		Upcoming:    []Item{},
		NewReleases: []Item{},
		Unavailable: []CellError{},
	}

	// add records one (row × kind) cell in call order (the documented
	// cell order): a non-nil error lands in `unavailable`, a non-empty
	// item slice is capped and appended to its row. A cell may carry
	// BOTH (book:new_releases in keyless dev: the OpenLibrary fallback
	// items plus the missing Google Books key named in `unavailable`).
	add := func(kind, row string, items []Item, err error) {
		if err != nil {
			res.Unavailable = append(res.Unavailable, CellError{Cell: kind + ":" + row, Reason: err.Error()})
		}
		if len(items) == 0 {
			return
		}
		cell := make([]Item, 0, len(items))
		cell = append(cell, items...)
		capCell(&cell)
		switch row {
		case "trending":
			res.Trending = append(res.Trending, cell...)
		case "upcoming":
			res.Upcoming = append(res.Upcoming, cell...)
		case "new_releases":
			res.NewReleases = append(res.NewReleases, cell...)
		}
	}

	// tmdbFold fetches one TMDB feed cell, degrading a nil/failed feed to
	// its CellError (the R3 keyless wording for the not-configured
	// sentinel).
	tmdbFold := func(fetch func() ([]providers.DiscoverItem, error)) ([]Item, error) {
		if s.deps.TMDB == nil {
			return nil, errors.New(reasonTMDBNotConfigured)
		}
		items, err := fetch()
		if err != nil {
			return nil, errors.New(tmdbReason(err))
		}
		return foldItems(items), nil
	}

	// ---- trending row: movie, tv, music, book ----
	items, err := tmdbFold(func() ([]providers.DiscoverItem, error) {
		return s.deps.TMDB.TrendingMovies(ctx)
	})
	add("movie", "trending", items, err)
	items, err = tmdbFold(func() ([]providers.DiscoverItem, error) {
		return s.deps.TMDB.TrendingTV(ctx)
	})
	add("tv", "trending", items, err)
	items, err = s.musicCell(ctx, now, musicTrendingWindowDays, true)
	add("music", "trending", items, err)
	items, err = s.bookTrending(ctx)
	add("book", "trending", items, err)

	// ---- upcoming row: movie, tv, music, book ----
	// Window is [today UTC, today+upcomingWindowDays UTC] from the calendar
	// (the domain also drops already-airing/released rows).
	addUpcoming := func(kind string, k cdom.Kind) {
		var items []Item
		var err error
		if s.deps.Calendar == nil {
			err = errors.New(reasonCalendarNotConfigured)
		} else {
			items, err = s.calendarCell(ctx, k, now)
		}
		add(kind, "upcoming", items, err)
	}
	addUpcoming("movie", cdom.KindMovie)
	addUpcoming("tv", cdom.KindTV)
	addUpcoming("music", cdom.KindMusic)
	items, err = s.bookUpcoming(ctx, now)
	add("book", "upcoming", items, err)

	// ---- new_releases row: movie, tv, music, book ----
	items, err = tmdbFold(func() ([]providers.DiscoverItem, error) {
		return s.deps.TMDB.NowPlayingMovies(ctx)
	})
	add("movie", "new_releases", items, err)
	items, err = tmdbFold(func() ([]providers.DiscoverItem, error) {
		since := now.AddDate(0, 0, -recentWindowDays)
		return s.deps.TMDB.RecentTV(ctx, since, now)
	})
	add("tv", "new_releases", items, err)
	items, err = s.musicCell(ctx, now, musicNewReleasesWindowDays, false)
	add("music", "new_releases", items, err)
	items, err = s.bookNewReleases(ctx, now)
	add("book", "new_releases", items, err)

	return res, nil
}

// calendarCell fetches one kind's upcoming entries for the fixed window
// [today UTC, today+upcomingWindowDays UTC] and folds them into items. The
// window is anchored on the UTC calendar date of `now`, not its local date
// (issue #139).
func (s *Service) calendarCell(ctx context.Context, k cdom.Kind, now time.Time) ([]Item, error) {
	n := now.UTC()
	start := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, upcomingWindowDays)
	entries, err := s.deps.Calendar.Calendar(ctx, k, start, end)
	if err != nil {
		return nil, err
	}
	out := make([]Item, 0, len(entries))
	for _, e := range entries {
		out = append(out, toCalendarItem(e))
	}
	return out, nil
}

// musicCell fetches one MusicBrainz feed cell over the [now-windowDays,
// now] window; an unwired MusicBrainz degrades to its CellError.
// trending selects the release-group feed, the new_releases row the
// release feed.
func (s *Service) musicCell(ctx context.Context, now time.Time, windowDays int, trending bool) ([]Item, error) {
	if s.deps.MusicBrainz == nil {
		return nil, errors.New(reasonMusicBrainzNotConfigured)
	}
	since := now.AddDate(0, 0, -windowDays)
	var items []providers.DiscoverItem
	var err error
	if trending {
		items, err = s.deps.MusicBrainz.TrendingReleaseGroups(ctx, since, now)
	} else {
		items, err = s.deps.MusicBrainz.NewReleases(ctx, since, now)
	}
	if err != nil {
		return nil, err
	}
	return foldItems(items), nil
}

// bookTrending fetches the trending:book cell (Open Library weekly);
// an unwired Open Library degrades to its CellError.
func (s *Service) bookTrending(ctx context.Context) ([]Item, error) {
	if s.deps.OpenLibrary == nil {
		return nil, errors.New(reasonOpenLibraryNotConfigured)
	}
	items, err := s.deps.OpenLibrary.TrendingWeekly(ctx)
	if err != nil {
		return nil, err
	}
	return foldItems(items), nil
}

// bookNewReleases assembles the new_releases:book cell (issue #118 A2):
// the keyed Google Books feed (orderBy=newest) is the PRIMARY when
// configured; a configured-but-failing feed degrades the cell to its
// CellError (the fallback does not substitute for a broken primary).
// Keyless dev — the normal state (no Google Books key; the adapter is
// wired but unconfigured) — serves the documented OpenLibrary
// first_publish_year fallback feed AND names the missing key in
// `unavailable` with the R3 keyless wording: 200, never a 5xx.
func (s *Service) bookNewReleases(ctx context.Context, now time.Time) ([]Item, error) {
	if s.deps.GoogleBooks != nil {
		items, err := s.deps.GoogleBooks.NewReleasesBooks(ctx)
		if err == nil {
			return foldItems(items), nil
		}
		if !errors.Is(err, providers.ErrNotConfigured) {
			// A configured-but-failing primary: the cell is
			// unavailable with the upstream reason; the keyless
			// fallback does not substitute for a broken primary.
			return nil, err
		}
		// Present but unconfigured (no key): fall through to the
		// keyless state below.
	}
	items, olErr := s.openLibraryNewReleases(ctx, now)
	if olErr != nil {
		// Neither primary nor fallback available: the cell is
		// unavailable; report the fallback's failure.
		return nil, olErr
	}
	return items, errors.New(reasonGoogleBooksNotConfigured)
}

// openLibraryNewReleases fetches the new_releases:book keyless fallback
// feed: works first published in the current year. An unwired Open
// Library degrades to its CellError.
func (s *Service) openLibraryNewReleases(ctx context.Context, now time.Time) ([]Item, error) {
	if s.deps.OpenLibrary == nil {
		return nil, errors.New(reasonOpenLibraryNotConfigured)
	}
	items, err := s.deps.OpenLibrary.NewReleasesByYear(ctx, now.Year())
	if err != nil {
		return nil, err
	}
	return foldItems(items), nil
}

// bookUpcoming renders the upcoming:book cell (issue #118 A2): monitored
// titles with an edition whose year indicates a release in the current
// year or later and that are not yet in the library (year-granular, as
// the domain's edition model holds a year, not a full date). The books
// domain expresses this query through ListBooks/ListEditions, so the
// cell is never the empty-by-design exception: no matching titles is a
// legitimately empty cell, not an error. An unwired library degrades to
// its CellError.
func (s *Service) bookUpcoming(ctx context.Context, now time.Time) ([]Item, error) {
	if s.deps.Books == nil {
		return nil, errors.New(reasonBooksNotConfigured)
	}
	books, err := s.deps.Books.ListBooks(ctx)
	if err != nil {
		return nil, err
	}
	year := now.Year()
	out := []Item{}
	for _, b := range books {
		if !b.Monitored || b.HasFile {
			continue
		}
		editions, err := s.deps.Books.ListEditions(ctx, b.ID)
		if err != nil {
			return nil, err
		}
		upcomingYear := 0
		for _, e := range editions {
			// Year-granular "current year or later"; the earliest
			// such edition is the cell's date.
			if e.Year >= year && (upcomingYear == 0 || e.Year < upcomingYear) {
				upcomingYear = e.Year
			}
		}
		if upcomingYear == 0 {
			continue
		}
		out = append(out, Item{
			Kind:     "book",
			SourceID: strconv.FormatInt(b.ID, 10), // local library id (library-sourced row)
			Title:    b.Name,
			Year:     upcomingYear,
			Subtitle: b.AuthorName,
		})
	}
	return out, nil
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

// foldItems converts provider feed rows to wire Items.
func foldItems(items []providers.DiscoverItem) []Item {
	out := make([]Item, 0, len(items))
	for _, it := range items {
		out = append(out, toItem(it))
	}
	return out
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
