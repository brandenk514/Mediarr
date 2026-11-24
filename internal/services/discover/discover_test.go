package discover

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	cdom "github.com/brandenk514/mediarr/internal/domains/calendar"
	"github.com/brandenk514/mediarr/internal/providers"
	calsvc "github.com/brandenk514/mediarr/internal/services/calendar"
)

// fakeTMDB implements TMDBFeeds with canned cells and an optional error.
type fakeTMDB struct {
	trendingMovies []providers.DiscoverItem
	trendingTV     []providers.DiscoverItem
	nowPlaying     []providers.DiscoverItem
	recentTV       []providers.DiscoverItem
	err            error
	recentSince    time.Time
	recentUntil    time.Time
}

func (f *fakeTMDB) TrendingMovies(context.Context) ([]providers.DiscoverItem, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.trendingMovies, nil
}

func (f *fakeTMDB) TrendingTV(context.Context) ([]providers.DiscoverItem, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.trendingTV, nil
}

func (f *fakeTMDB) NowPlayingMovies(context.Context) ([]providers.DiscoverItem, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.nowPlaying, nil
}

func (f *fakeTMDB) RecentTV(_ context.Context, since, until time.Time) ([]providers.DiscoverItem, error) {
	f.recentSince, f.recentUntil = since, until
	if f.err != nil {
		return nil, f.err
	}
	return f.recentTV, nil
}

// fakeCalendar implements Calendar with canned entries per kind, an
// optional error, and the window the service passed down.
type fakeCalendar struct {
	movieEntries []cdom.Entry
	tvEntries    []cdom.Entry
	err          error
	start, end   time.Time
	kinds        []cdom.Kind
}

func (f *fakeCalendar) Calendar(_ context.Context, kind cdom.Kind, start, end time.Time) ([]cdom.Entry, error) {
	f.kinds = append(f.kinds, kind)
	f.start, f.end = start, end
	if f.err != nil {
		return nil, f.err
	}
	if kind == cdom.KindMovie {
		return f.movieEntries, nil
	}
	return f.tvEntries, nil
}

// feedItem builds a provider feed row for the n-th item of a kind.
func feedItem(kind string, n int) providers.DiscoverItem {
	return providers.DiscoverItem{
		Kind:     kind,
		SourceID: fmt.Sprintf("%s-%d", kind, n),
		Title:    fmt.Sprintf("%s %d", kind, n),
		Year:     2024,
		Date:     time.Date(2024, 5, 1, 0, 0, 0, 0, time.UTC),
	}
}

// TestDiscover_FullPage: every cell populates and the response shape is
// the OpenAPI DiscoverPage (rows + empty unavailable).
func TestDiscover_FullPage(t *testing.T) {
	tmdb := &fakeTMDB{
		trendingMovies: []providers.DiscoverItem{feedItem("movie", 1), feedItem("movie", 2)},
		trendingTV:     []providers.DiscoverItem{feedItem("tv", 1)},
		nowPlaying:     []providers.DiscoverItem{feedItem("movie", 3)},
		recentTV:       []providers.DiscoverItem{feedItem("tv", 2)},
	}
	cal := &fakeCalendar{
		movieEntries: []cdom.Entry{{Kind: "movie", SeriesID: 11, SeriesTitle: "Upcoming Movie", Year: 2026, AirStart: time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)}},
		tvEntries:    []cdom.Entry{{Kind: "tv", SeriesID: 21, SeriesTitle: "Upcoming Show", Season: 1, Episode: 2, SXE: "S01E02", AirStart: time.Date(2026, 12, 2, 21, 0, 0, 0, time.UTC)}},
	}
	s := New(tmdb, cal)

	res, err := s.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(res.Trending) != 3 {
		t.Fatalf("trending = %d items, want 3 (movie 2 + tv 1)", len(res.Trending))
	}
	if res.Trending[0].Kind != "movie" || res.Trending[0].SourceID != "movie-1" {
		t.Fatalf("trending[0] = %+v", res.Trending[0])
	}
	if res.Trending[2].Kind != "tv" || res.Trending[2].SourceID != "tv-1" {
		t.Fatalf("trending[2] = %+v", res.Trending[2])
	}
	if res.Trending[0].Date != "2024-05-01T00:00:00Z" || res.Trending[0].Year != 2024 {
		t.Fatalf("trending[0] date/year = %q/%d", res.Trending[0].Date, res.Trending[0].Year)
	}
	if len(res.Upcoming) != 2 {
		t.Fatalf("upcoming = %d items, want 2", len(res.Upcoming))
	}
	if res.Upcoming[0].Kind != "movie" || res.Upcoming[0].SourceID != "11" || res.Upcoming[0].Subtitle != "" {
		t.Fatalf("upcoming[0] = %+v", res.Upcoming[0])
	}
	if res.Upcoming[1].Kind != "tv" || res.Upcoming[1].SourceID != "21" || res.Upcoming[1].Subtitle != "S01E02" ||
		res.Upcoming[1].Date != "2026-12-02T21:00:00Z" {
		t.Fatalf("upcoming[1] = %+v", res.Upcoming[1])
	}
	if len(res.NewReleases) != 2 {
		t.Fatalf("newReleases = %d items, want 2", len(res.NewReleases))
	}
	if res.NewReleases[0].Kind != "movie" || res.NewReleases[1].Kind != "tv" {
		t.Fatalf("newReleases kinds = %v, %v", res.NewReleases[0].Kind, res.NewReleases[1].Kind)
	}
	if len(res.Unavailable) != 0 {
		t.Fatalf("unavailable = %+v, want empty", res.Unavailable)
	}
}

// TestDiscover_CellCap: each (row × kind) cell is capped at cellCap items
// (8 fed → 6 served).
func TestDiscover_CellCap(t *testing.T) {
	tmdb := &fakeTMDB{
		trendingMovies: feedItems("movie", 8),
		trendingTV:     feedItems("tv", 8),
		nowPlaying:     feedItems("movie", 8),
		recentTV:       feedItems("tv", 8),
	}
	s := New(tmdb, &fakeCalendar{})

	res, err := s.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(res.Trending) != 2*cellCap {
		t.Fatalf("trending = %d items, want %d (6+6)", len(res.Trending), 2*cellCap)
	}
	if len(res.NewReleases) != 2*cellCap {
		t.Fatalf("newReleases = %d items, want %d", len(res.NewReleases), 2*cellCap)
	}
}

func feedItems(kind string, n int) []providers.DiscoverItem {
	out := make([]providers.DiscoverItem, 0, n)
	for i := 1; i <= n; i++ {
		out = append(out, feedItem(kind, i))
	}
	return out
}

// TestDiscover_KeylessTMDB: the not-configured sentinel degrades all four
// TMDB cells to CellErrors with the R3 keyless wording while the calendar
// cells still populate — 200, never 5xx.
func TestDiscover_KeylessTMDB(t *testing.T) {
	cal := &fakeCalendar{
		movieEntries: []cdom.Entry{{Kind: "movie", SeriesID: 11, SeriesTitle: "Upcoming Movie", AirStart: time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)}},
		tvEntries:    []cdom.Entry{{Kind: "tv", SeriesID: 21, SeriesTitle: "Upcoming Show", AirStart: time.Date(2026, 12, 2, 21, 0, 0, 0, time.UTC)}},
	}
	s := New(&fakeTMDB{err: providers.ErrNotConfigured}, cal)

	res, err := s.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover must not fail on feed degradation: %v", err)
	}
	if len(res.Trending) != 0 || len(res.NewReleases) != 0 {
		t.Fatalf("degraded rows must be empty: trending=%d newReleases=%d", len(res.Trending), len(res.NewReleases))
	}
	if len(res.Upcoming) != 2 {
		t.Fatalf("upcoming must still populate: %d items", len(res.Upcoming))
	}
	want := []string{"movie:trending", "tv:trending", "movie:new_releases", "tv:new_releases"}
	if len(res.Unavailable) != len(want) {
		t.Fatalf("unavailable = %+v", res.Unavailable)
	}
	for i, c := range res.Unavailable {
		if c.Cell != want[i] || c.Reason != "tmdb is not configured (missing API key)" {
			t.Fatalf("unavailable[%d] = %+v, want %s with the R3 keyless wording", i, c, want[i])
		}
	}
}

// TestDiscover_NilTMDB: an unwired TMDB dependency degrades the same way
// as a keyless one (health-only boot keeps the page answering 200).
func TestDiscover_NilTMDB(t *testing.T) {
	s := New(nil, &fakeCalendar{})
	res, err := s.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(res.Unavailable) != 4 {
		t.Fatalf("unavailable = %+v, want 4 TMDB cells", res.Unavailable)
	}
	for _, c := range res.Unavailable {
		if c.Reason != "tmdb is not configured (missing API key)" {
			t.Fatalf("cell %s reason = %q", c.Cell, c.Reason)
		}
	}
}

// TestDiscover_CalendarError: a calendar failure degrades only the
// upcoming cells; the TMDB cells are untouched.
func TestDiscover_CalendarError(t *testing.T) {
	tmdb := &fakeTMDB{
		trendingMovies: []providers.DiscoverItem{feedItem("movie", 1)},
		trendingTV:     []providers.DiscoverItem{feedItem("tv", 1)},
		nowPlaying:     []providers.DiscoverItem{feedItem("movie", 2)},
		recentTV:       []providers.DiscoverItem{feedItem("tv", 2)},
	}
	s := New(tmdb, &fakeCalendar{err: errors.New("db down")})

	res, err := s.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover must not fail on a calendar error: %v", err)
	}
	if len(res.Upcoming) != 0 {
		t.Fatalf("upcoming must be empty: %d items", len(res.Upcoming))
	}
	if len(res.Trending) != 2 || len(res.NewReleases) != 2 {
		t.Fatalf("TMDB rows must be intact: trending=%d newReleases=%d", len(res.Trending), len(res.NewReleases))
	}
	if len(res.Unavailable) != 2 {
		t.Fatalf("unavailable = %+v, want 2 upcoming cells", res.Unavailable)
	}
	if res.Unavailable[0].Cell != "movie:upcoming" || res.Unavailable[0].Reason != "db down" {
		t.Fatalf("unavailable[0] = %+v", res.Unavailable[0])
	}
	if res.Unavailable[1].Cell != "tv:upcoming" {
		t.Fatalf("unavailable[1] = %+v", res.Unavailable[1])
	}
}

// TestDiscover_NilCalendar: an unwired calendar degrades only the
// upcoming cells.
func TestDiscover_NilCalendar(t *testing.T) {
	s := New(&fakeTMDB{trendingMovies: []providers.DiscoverItem{feedItem("movie", 1)}}, nil)
	res, err := s.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(res.Unavailable) != 2 || res.Unavailable[0].Cell != "movie:upcoming" || res.Unavailable[1].Cell != "tv:upcoming" {
		t.Fatalf("unavailable = %+v", res.Unavailable)
	}
	if len(res.Trending) != 1 {
		t.Fatalf("trending must be intact: %d items", len(res.Trending))
	}
}

// TestDiscover_UpcomingWindow: the service asks the calendar for exactly
// [today UTC, today+14d UTC] and for both kinds (movie, tv).
func TestDiscover_UpcomingWindow(t *testing.T) {
	now := time.Now().UTC()
	cal := &fakeCalendar{}
	s := New(&fakeTMDB{}, cal)

	_, err := s.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	wantStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	wantEnd := wantStart.AddDate(0, 0, upcomingWindowDays)
	if !cal.start.Equal(wantStart) || !cal.end.Equal(wantEnd) {
		t.Fatalf("window = %s .. %s, want %s .. %s", cal.start, cal.end, wantStart, wantEnd)
	}
	if len(cal.kinds) != 2 || cal.kinds[0] != cdom.KindMovie || cal.kinds[1] != cdom.KindTV {
		t.Fatalf("kinds = %v, want [movie tv]", cal.kinds)
	}
}

// TestDiscover_RecentTVWindow: the new_releases:tv cell reaches back
// exactly recentWindowDays — since and until both derive from the same
// service-side `now`, so their span is asserted exactly while the anchor
// against the test clock gets a small drift tolerance.
func TestDiscover_RecentTVWindow(t *testing.T) {
	now := time.Now().UTC()
	tmdb := &fakeTMDB{}
	s := New(tmdb, &fakeCalendar{})

	_, err := s.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if !tmdb.recentSince.Equal(tmdb.recentUntil.AddDate(0, 0, -recentWindowDays)) {
		t.Fatalf("recent window span = %s .. %s, want exactly %d days", tmdb.recentSince, tmdb.recentUntil, recentWindowDays)
	}
	if d := tmdb.recentUntil.Sub(now); d < -2*time.Second || d > 2*time.Second {
		t.Fatalf("recentUntil = %s, want within 2s of %s", tmdb.recentUntil, now)
	}
}

// TestDiscover_UpcomingWindowEndToEnd: through the real calendar service
// the 14-day horizon excludes a +15d airing while +1d/+13d are in; the
// future cutoff drops yesterday.
func TestDiscover_UpcomingWindowEndToEnd(t *testing.T) {
	now := time.Now().UTC()
	air := func(d int) *time.Time {
		t := now.AddDate(0, 0, d)
		return &t
	}
	repo := &memCalRepo{
		rows: []cdom.EpisodeRow{
			{SeriesID: 1, SeriesTitle: "InOne", Season: 1, Episode: 1, AirDate: air(1), Monitored: true},
			{SeriesID: 2, SeriesTitle: "InThirteen", Season: 1, Episode: 1, AirDate: air(13), Monitored: true},
			{SeriesID: 3, SeriesTitle: "OutFifteen", Season: 1, Episode: 1, AirDate: air(15), Monitored: true},
			{SeriesID: 4, SeriesTitle: "Past", Season: 1, Episode: 1, AirDate: air(-1), Monitored: true},
		},
	}
	s := New(&fakeTMDB{}, calsvc.New(repo))

	res, err := s.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(res.Upcoming) != 2 {
		t.Fatalf("upcoming = %d items, want 2 (the +1d and +13d airings): %+v", len(res.Upcoming), res.Upcoming)
	}
	if res.Upcoming[0].Title != "InOne" || res.Upcoming[1].Title != "InThirteen" {
		t.Fatalf("upcoming titles = %q, %q", res.Upcoming[0].Title, res.Upcoming[1].Title)
	}
}

// memCalRepo is an in-memory calendar.Repo for the end-to-end window test.
type memCalRepo struct{ rows []cdom.EpisodeRow }

func (m *memCalRepo) Episodes(context.Context) ([]cdom.EpisodeRow, error) { return m.rows, nil }
func (m *memCalRepo) MovieRows(context.Context) ([]cdom.MovieRow, error)  { return nil, nil }
func (m *memCalRepo) MusicRows(context.Context) ([]cdom.MusicRow, error)  { return nil, nil }

// TestDiscover_WireShape: the response marshals to the OpenAPI wire shape
// (camelCase item fields, snake-free row keys, [] never null).
func TestDiscover_WireShape(t *testing.T) {
	s := New(&fakeTMDB{trendingMovies: []providers.DiscoverItem{feedItem("movie", 1)}}, &fakeCalendar{})
	res, err := s.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, k := range []string{"trending", "upcoming", "newReleases", "unavailable"} {
		if _, ok := m[k]; !ok {
			t.Fatalf("missing top-level key %q in %s", k, b)
		}
	}
	var items []map[string]any
	if err := json.Unmarshal(m["trending"], &items); err != nil {
		t.Fatalf("trending: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("trending = %d items, want 1", len(items))
	}
	for _, k := range []string{"kind", "sourceId", "title", "year", "subtitle", "posterUrl", "date"} {
		if _, ok := items[0][k]; !ok {
			t.Fatalf("missing item field %q in %s", k, b)
		}
	}
}

// TestDiscover_CellOrder: with every feed down, unavailable lists all six
// cells in the documented cell order.
func TestDiscover_CellOrder(t *testing.T) {
	s := New(nil, nil)
	res, err := s.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	want := []string{
		"movie:trending", "tv:trending",
		"movie:upcoming", "tv:upcoming",
		"movie:new_releases", "tv:new_releases",
	}
	if len(res.Unavailable) != len(want) {
		t.Fatalf("unavailable = %+v", res.Unavailable)
	}
	for i, c := range res.Unavailable {
		if c.Cell != want[i] {
			t.Fatalf("unavailable[%d] = %q, want %q", i, c.Cell, want[i])
		}
	}
}
