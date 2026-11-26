package discover

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	bdomain "github.com/brandenk514/mediarr/internal/domains/books"
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

// fakeMusicBrainz implements MusicBrainzFeeds with canned cells and an
// optional error. It records the windows the service passes down.
type fakeMusicBrainz struct {
	trending               []providers.DiscoverItem
	releases               []providers.DiscoverItem
	err                    error
	trendSince, trendUntil time.Time
	relSince, relUntil     time.Time
}

func (f *fakeMusicBrainz) TrendingReleaseGroups(_ context.Context, since, until time.Time) ([]providers.DiscoverItem, error) {
	f.trendSince, f.trendUntil = since, until
	if f.err != nil {
		return nil, f.err
	}
	return f.trending, nil
}

func (f *fakeMusicBrainz) NewReleases(_ context.Context, since, until time.Time) ([]providers.DiscoverItem, error) {
	f.relSince, f.relUntil = since, until
	if f.err != nil {
		return nil, f.err
	}
	return f.releases, nil
}

// fakeOpenLibrary implements OpenLibraryFeeds with canned cells and an
// optional error. It records the year the service passes down.
type fakeOpenLibrary struct {
	trending    []providers.DiscoverItem
	releases    []providers.DiscoverItem
	err         error
	releaseYear int
}

func (f *fakeOpenLibrary) TrendingWeekly(context.Context) ([]providers.DiscoverItem, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.trending, nil
}

func (f *fakeOpenLibrary) NewReleasesByYear(_ context.Context, year int) ([]providers.DiscoverItem, error) {
	f.releaseYear = year
	if f.err != nil {
		return nil, f.err
	}
	return f.releases, nil
}

// fakeGoogleBooks implements GoogleBooksFeeds with a canned cell and an
// optional error.
type fakeGoogleBooks struct {
	releases []providers.DiscoverItem
	err      error
	calls    int
}

func (f *fakeGoogleBooks) NewReleasesBooks(context.Context) ([]providers.DiscoverItem, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.releases, nil
}

// fakeBooks implements BookLibrary: a flat book list plus per-title
// editions.
type fakeBooks struct {
	books    []bdomain.Book
	editions map[int64][]bdomain.Edition
	err      error
}

func (f *fakeBooks) ListBooks(context.Context) ([]bdomain.Book, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.books, nil
}

func (f *fakeBooks) ListEditions(_ context.Context, titleID int64) ([]bdomain.Edition, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.editions[titleID], nil
}

// fakeCalendar implements Calendar with canned entries per kind, an
// optional error, and the window the service passed down.
type fakeCalendar struct {
	entries map[cdom.Kind][]cdom.Entry
	err     error
	start   time.Time
	end     time.Time
	kinds   []cdom.Kind
}

func (f *fakeCalendar) Calendar(_ context.Context, kind cdom.Kind, start, end time.Time) ([]cdom.Entry, error) {
	f.kinds = append(f.kinds, kind)
	f.start, f.end = start, end
	if f.err != nil {
		return nil, f.err
	}
	if f.entries != nil {
		return f.entries[kind], nil
	}
	return nil, nil
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

// allDeps builds a Deps with every feed populated for one item per cell
// (the full-page fixture): 4 kinds × 3 rows = 12 items.
func allDeps() (Deps, *fakeMusicBrainz, *fakeOpenLibrary, *fakeGoogleBooks, *fakeBooks, *fakeCalendar) {
	tmdb := &fakeTMDB{
		trendingMovies: []providers.DiscoverItem{feedItem("movie", 1)},
		trendingTV:     []providers.DiscoverItem{feedItem("tv", 1)},
		nowPlaying:     []providers.DiscoverItem{feedItem("movie", 3)},
		recentTV:       []providers.DiscoverItem{feedItem("tv", 2)},
	}
	mb := &fakeMusicBrainz{
		trending: []providers.DiscoverItem{feedItem("music", 1)},
		releases: []providers.DiscoverItem{feedItem("music", 2)},
	}
	ol := &fakeOpenLibrary{
		trending: []providers.DiscoverItem{feedItem("book", 1)},
		releases: []providers.DiscoverItem{feedItem("book", 2)},
	}
	gb := &fakeGoogleBooks{releases: []providers.DiscoverItem{feedItem("book", 5)}}
	books := &fakeBooks{
		books:    []bdomain.Book{{Title: bdomain.Title{ID: 7, Name: "Upcoming Book", Monitored: true}, AuthorName: "An Author"}},
		editions: map[int64][]bdomain.Edition{7: {{ID: 1, TitleID: 7, Year: time.Now().Year() + 1}}},
	}
	cal := &fakeCalendar{entries: map[cdom.Kind][]cdom.Entry{
		cdom.KindMovie: {{Kind: "movie", SeriesID: 11, SeriesTitle: "Upcoming Movie", Year: 2026, AirStart: time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)}},
		cdom.KindTV:    {{Kind: "tv", SeriesID: 21, SeriesTitle: "Upcoming Show", Season: 1, Episode: 2, SXE: "S01E02", AirStart: time.Date(2026, 12, 2, 21, 0, 0, 0, time.UTC)}},
		cdom.KindMusic: {{Kind: "music", SeriesID: 31, SeriesTitle: "Upcoming Album", AirStart: time.Date(2026, 12, 3, 0, 0, 0, 0, time.UTC)}},
	}}
	return Deps{TMDB: tmdb, Calendar: cal, MusicBrainz: mb, OpenLibrary: ol, GoogleBooks: gb, Books: books}, mb, ol, gb, books, cal
}

// TestDiscover_FullPage: every cell populates (4 kinds × 3 rows) and the
// response shape is the OpenAPI DiscoverPage (rows + empty unavailable).
func TestDiscover_FullPage(t *testing.T) {
	deps, _, _, _, _, _ := allDeps()
	s := New(deps)

	res, err := s.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	for _, row := range []struct {
		name string
		got  []Item
	}{{"trending", res.Trending}, {"upcoming", res.Upcoming}, {"newReleases", res.NewReleases}} {
		if len(row.got) != 4 {
			t.Fatalf("%s = %d items, want 4 (one per kind): %+v", row.name, len(row.got), row.got)
		}
		wantKinds := []string{"movie", "tv", "music", "book"}
		for i, item := range row.got {
			if item.Kind != wantKinds[i] {
				t.Fatalf("%s[%d].kind = %q, want %q (deterministic order movie,tv,music,book)", row.name, i, item.Kind, wantKinds[i])
			}
		}
	}
	if len(res.Trending[0].SourceID) == 0 || res.Trending[0].Kind != "movie" {
		t.Fatalf("trending[0] = %+v", res.Trending[0])
	}
	if res.Trending[0].Date != "2024-05-01T00:00:00Z" || res.Trending[0].Year != 2024 {
		t.Fatalf("trending[0] date/year = %q/%d", res.Trending[0].Date, res.Trending[0].Year)
	}
	if res.Upcoming[2].Kind != "music" || res.Upcoming[2].SourceID != "31" {
		t.Fatalf("upcoming[2] = %+v, want the calendar music row", res.Upcoming[2])
	}
	if res.Upcoming[3].Kind != "book" || res.Upcoming[3].SourceID != "7" || res.Upcoming[3].Title != "Upcoming Book" {
		t.Fatalf("upcoming[3] = %+v, want the library book row", res.Upcoming[3])
	}
	// Keyed Google Books is the new_releases:book primary (no OL fallback).
	if res.NewReleases[3].SourceID != "book-5" {
		t.Fatalf("newReleases[3] = %+v, want the Google Books row", res.NewReleases[3])
	}
	if len(res.Unavailable) != 0 {
		t.Fatalf("unavailable = %+v, want empty", res.Unavailable)
	}
}

// TestDiscover_CellCap: each feed (row × kind) cell is capped at cellCap
// items (8 fed → 6 served), including the new music/book feed cells. The
// upcoming row is capped the same way; the fixture here feeds one
// calendar/library item per kind, so the row carries exactly 4.
func TestDiscover_CellCap(t *testing.T) {
	deps, _, _, _, _, _ := allDeps()
	deps.TMDB = &fakeTMDB{
		trendingMovies: feedItems("movie", 8),
		trendingTV:     feedItems("tv", 8),
		nowPlaying:     feedItems("movie", 8),
		recentTV:       feedItems("tv", 8),
	}
	deps.MusicBrainz = &fakeMusicBrainz{trending: feedItems("music", 8), releases: feedItems("music", 8)}
	deps.OpenLibrary = &fakeOpenLibrary{trending: feedItems("book", 8), releases: feedItems("book", 8)}
	deps.GoogleBooks = &fakeGoogleBooks{releases: feedItems("book", 8)}
	deps.Books = &fakeBooks{
		books:    []bdomain.Book{{Title: bdomain.Title{ID: 7, Name: "B", Monitored: true}, AuthorName: "A"}},
		editions: map[int64][]bdomain.Edition{7: {{ID: 1, TitleID: 7, Year: 2099}}},
	}
	s := New(deps)

	res, err := s.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	for _, row := range []struct {
		name string
		got  []Item
	}{{"trending", res.Trending}, {"newReleases", res.NewReleases}} {
		if len(row.got) != 4*cellCap {
			t.Fatalf("%s = %d items, want %d (4 kinds × %d)", row.name, len(row.got), 4*cellCap, cellCap)
		}
	}
	if len(res.Upcoming) != 4 {
		t.Fatalf("upcoming = %d items, want 4 (1 per kind from the fixture)", len(res.Upcoming))
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
// TMDB cells to CellErrors with the R3 keyless wording while the
// calendar cells still populate — 200, never 5xx.
func TestDiscover_KeylessTMDB(t *testing.T) {
	deps, _, _, _, _, _ := allDeps()
	deps.TMDB = &fakeTMDB{err: providers.ErrNotConfigured}
	s := New(deps)

	res, err := s.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover must not fail on feed degradation: %v", err)
	}
	tmdbCells := 0
	for _, c := range res.Unavailable {
		if tmdbCells < 4 && (c.Cell == "movie:trending" || c.Cell == "tv:trending" || c.Cell == "movie:new_releases" || c.Cell == "tv:new_releases") {
			tmdbCells++
			if c.Reason != "tmdb is not configured (missing API key)" {
				t.Fatalf("cell %s reason = %q, want the R3 keyless wording", c.Cell, c.Reason)
			}
		}
	}
	if tmdbCells != 4 {
		t.Fatalf("unavailable = %+v, want the 4 TMDB cells", res.Unavailable)
	}
	// The non-TMDB cells are untouched.
	if len(res.Upcoming) != 4 {
		t.Fatalf("upcoming must still populate all 4 kinds: %d items", len(res.Upcoming))
	}
}

// TestDiscover_NilTMDB: an unwired TMDB dependency degrades the same way
// as a keyless one (health-only boot keeps the page answering 200).
func TestDiscover_NilTMDB(t *testing.T) {
	deps, _, _, _, _, _ := allDeps()
	deps.TMDB = nil
	s := New(deps)

	res, err := s.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	tmdbCells := 0
	for _, c := range res.Unavailable {
		if c.Reason == "tmdb is not configured (missing API key)" {
			tmdbCells++
		}
	}
	if tmdbCells != 4 {
		t.Fatalf("unavailable = %+v, want 4 TMDB cells", res.Unavailable)
	}
}

// TestDiscover_MusicBrainzOutage: a MusicBrainz failure degrades only the
// two music feed cells; every other cell is intact.
func TestDiscover_MusicBrainzOutage(t *testing.T) {
	deps, _, _, _, _, _ := allDeps()
	deps.MusicBrainz = &fakeMusicBrainz{err: errors.New("mb down")}
	s := New(deps)

	res, err := s.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover must not fail on a MusicBrainz outage: %v", err)
	}
	want := []string{"music:trending", "music:new_releases"}
	if len(res.Unavailable) != len(want) {
		t.Fatalf("unavailable = %+v, want only the two music feed cells", res.Unavailable)
	}
	for i, c := range res.Unavailable {
		if c.Cell != want[i] || c.Reason != "mb down" {
			t.Fatalf("unavailable[%d] = %+v", i, c)
		}
	}
	// trending/upcoming/newReleases each still carry the 3 intact kinds.
	if len(res.Trending) != 3 || len(res.Upcoming) != 4 || len(res.NewReleases) != 3 {
		t.Fatalf("rows = %d/%d/%d, want 3/4/3 (music feed cells only degraded)", len(res.Trending), len(res.Upcoming), len(res.NewReleases))
	}
}

// TestDiscover_OpenLibraryOutage: an Open Library failure degrades only
// the book feed cells (trending + the keyless-fallback new-releases
// path). Here Google Books is wired but unconfigured, so the
// new_releases:book cell is the keyless fallback state: the outage
// surfaces as the OpenLibrary error and the cell carries no items.
func TestDiscover_OpenLibraryOutage(t *testing.T) {
	deps, _, _, _, _, _ := allDeps()
	deps.GoogleBooks = nil
	deps.OpenLibrary = &fakeOpenLibrary{err: errors.New("ol down")}
	s := New(deps)

	res, err := s.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover must not fail on an OpenLibrary outage: %v", err)
	}
	if len(res.Unavailable) != 2 {
		t.Fatalf("unavailable = %+v, want the two book feed cells", res.Unavailable)
	}
	if res.Unavailable[0].Cell != "book:trending" || res.Unavailable[0].Reason != "ol down" {
		t.Fatalf("unavailable[0] = %+v", res.Unavailable[0])
	}
	if res.Unavailable[1].Cell != "book:new_releases" || res.Unavailable[1].Reason != "ol down" {
		t.Fatalf("unavailable[1] = %+v", res.Unavailable[1])
	}
	// The music and movie/tv cells are intact; upcoming:book (library) too.
	if len(res.Trending) != 3 || len(res.Upcoming) != 4 || len(res.NewReleases) != 3 {
		t.Fatalf("rows = %d/%d/%d, want 3/4/3", len(res.Trending), len(res.Upcoming), len(res.NewReleases))
	}
}

// TestDiscover_GoogleBooksOutage: a configured-but-failing Google Books
// feed degrades only the new_releases:book cell (no fallback for a
// broken primary); the OpenLibrary trending cell is intact.
func TestDiscover_GoogleBooksOutage(t *testing.T) {
	deps, _, ol, _, _, _ := allDeps()
	deps.GoogleBooks = &fakeGoogleBooks{err: errors.New("gb down")}
	s := New(deps)

	res, err := s.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover must not fail on a GoogleBooks outage: %v", err)
	}
	if len(res.Unavailable) != 1 {
		t.Fatalf("unavailable = %+v, want only book:new_releases", res.Unavailable)
	}
	if res.Unavailable[0].Cell != "book:new_releases" || res.Unavailable[0].Reason != "gb down" {
		t.Fatalf("unavailable[0] = %+v", res.Unavailable[0])
	}
	if len(res.Trending) != 4 || len(res.NewReleases) != 3 {
		t.Fatalf("rows = %d/%d, want 4/3 (only the GB cell degraded)", len(res.Trending), len(res.NewReleases))
	}
	// The OpenLibrary fallback must NOT be consulted when the primary is
	// merely failing.
	if ol.releaseYear != 0 {
		t.Fatalf("OL NewReleasesByYear consulted in an outage (year=%d), want only in the keyless state", ol.releaseYear)
	}
}

// TestDiscover_KeylessGoogleBooks: a missing Google Books key (the normal
// keyless-dev state) serves the new_releases:book cell from the
// documented OpenLibrary first_publish_year fallback AND names the
// missing key in `unavailable` with the R3 keyless wording — 200, never
// a 5xx.
func TestDiscover_KeylessGoogleBooks(t *testing.T) {
	deps, _, _, _, _, _ := allDeps()
	// Unconfigured adapter: the provider reports ErrNotConfigured.
	deps.GoogleBooks = &fakeGoogleBooks{err: providers.ErrNotConfigured}
	s := New(deps)

	res, err := s.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover must not fail in the keyless GB state: %v", err)
	}
	// The fallback items ARE in the row (the documented keyless fallback
	// feed).
	found := false
	for _, it := range res.NewReleases {
		if it.SourceID == "book-2" {
			found = true
		}
	}
	if !found {
		t.Fatalf("new_releases = %+v, want the OpenLibrary fallback row", res.NewReleases)
	}
	// AND the missing key is named in unavailable.
	var cellErr *CellError
	for i := range res.Unavailable {
		if res.Unavailable[i].Cell == "book:new_releases" {
			cellErr = &res.Unavailable[i]
		}
	}
	if cellErr == nil {
		t.Fatalf("unavailable = %+v, want book:new_releases named", res.Unavailable)
	}
	if cellErr.Reason != "google books is not configured (missing API key)" {
		t.Fatalf("reason = %q, want the R3 keyless wording", cellErr.Reason)
	}
}

// TestDiscover_NilGoogleBooks: an unwired Google Books dep is the same
// keyless state (fallback items + the missing-key CellError).
func TestDiscover_NilGoogleBooks(t *testing.T) {
	deps, _, _, _, _, _ := allDeps()
	deps.GoogleBooks = nil
	s := New(deps)

	res, err := s.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	var cellErr *CellError
	for i := range res.Unavailable {
		if res.Unavailable[i].Cell == "book:new_releases" {
			cellErr = &res.Unavailable[i]
		}
	}
	if cellErr == nil || cellErr.Reason != "google books is not configured (missing API key)" {
		t.Fatalf("unavailable = %+v, want book:new_releases with the keyless wording", res.Unavailable)
	}
}

// TestDiscover_CalendarError: a calendar failure degrades only the three
// calendar-backed upcoming cells (movie, tv, music); the library-sourced
// book cell and all feeds are untouched.
func TestDiscover_CalendarError(t *testing.T) {
	deps, _, _, _, _, _ := allDeps()
	deps.Calendar = &fakeCalendar{err: errors.New("db down")}
	s := New(deps)

	res, err := s.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover must not fail on a calendar error: %v", err)
	}
	if len(res.Upcoming) != 1 || res.Upcoming[0].Kind != "book" {
		t.Fatalf("upcoming = %+v, want only the library book cell", res.Upcoming)
	}
	if len(res.Unavailable) != 3 {
		t.Fatalf("unavailable = %+v, want the 3 calendar upcoming cells", res.Unavailable)
	}
	for i, want := range []string{"movie:upcoming", "tv:upcoming", "music:upcoming"} {
		if res.Unavailable[i].Cell != want || res.Unavailable[i].Reason != "db down" {
			t.Fatalf("unavailable[%d] = %+v, want %s", i, res.Unavailable[i], want)
		}
	}
	if len(res.Trending) != 4 || len(res.NewReleases) != 4 {
		t.Fatalf("feed rows must be intact: trending=%d newReleases=%d", len(res.Trending), len(res.NewReleases))
	}
}

// TestDiscover_NilCalendar: an unwired calendar degrades only the
// calendar-backed upcoming cells.
func TestDiscover_NilCalendar(t *testing.T) {
	deps, _, _, _, _, _ := allDeps()
	deps.Calendar = nil
	s := New(deps)

	res, err := s.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(res.Upcoming) != 1 || res.Upcoming[0].Kind != "book" {
		t.Fatalf("upcoming = %+v, want only the library book cell", res.Upcoming)
	}
	if len(res.Unavailable) != 3 {
		t.Fatalf("unavailable = %+v, want 3 calendar cells", res.Unavailable)
	}
	if len(res.Trending) != 4 {
		t.Fatalf("trending must be intact: %d items", len(res.Trending))
	}
}

// TestDiscover_NilMusicBrainz: an unwired MusicBrainz degrades only the
// two music feed cells (upcoming:music is calendar-sourced and intact).
func TestDiscover_NilMusicBrainz(t *testing.T) {
	deps, _, _, _, _, _ := allDeps()
	deps.MusicBrainz = nil
	s := New(deps)

	res, err := s.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(res.Unavailable) != 2 {
		t.Fatalf("unavailable = %+v, want the 2 music feed cells", res.Unavailable)
	}
	for i, want := range []string{"music:trending", "music:new_releases"} {
		if res.Unavailable[i].Cell != want || res.Unavailable[i].Reason != "musicbrainz is not configured" {
			t.Fatalf("unavailable[%d] = %+v, want %s", i, res.Unavailable[i], want)
		}
	}
}

// TestDiscover_NilOpenLibrary: an unwired OpenLibrary degrades the book
// feed cells; the library-sourced book cells are intact.
func TestDiscover_NilOpenLibrary(t *testing.T) {
	deps, _, _, _, _, _ := allDeps()
	deps.OpenLibrary = nil
	deps.GoogleBooks = &fakeGoogleBooks{} // keep new_releases:book primary intact
	s := New(deps)

	res, err := s.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(res.Unavailable) != 1 || res.Unavailable[0].Cell != "book:trending" {
		t.Fatalf("unavailable = %+v, want only book:trending", res.Unavailable)
	}
	if len(res.Upcoming) != 4 {
		t.Fatalf("upcoming must keep all 4 kinds: %+v", res.Upcoming)
	}
}

// TestDiscover_NilBooks: an unwired library degrades only upcoming:book.
func TestDiscover_NilBooks(t *testing.T) {
	deps, _, _, _, _, _ := allDeps()
	deps.Books = nil
	s := New(deps)

	res, err := s.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(res.Unavailable) != 1 || res.Unavailable[0].Cell != "book:upcoming" {
		t.Fatalf("unavailable = %+v, want only book:upcoming", res.Unavailable)
	}
	if len(res.Upcoming) != 3 {
		t.Fatalf("upcoming = %+v, want the 3 non-book cells", res.Upcoming)
	}
}

// TestDiscover_BooksLibraryError: a library query failure degrades only
// upcoming:book (the feed-backed book cells are intact).
func TestDiscover_BooksLibraryError(t *testing.T) {
	deps, _, _, _, _, _ := allDeps()
	deps.Books = &fakeBooks{err: errors.New("books db down")}
	s := New(deps)

	res, err := s.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover must not fail on a books error: %v", err)
	}
	if len(res.Unavailable) != 1 || res.Unavailable[0].Cell != "book:upcoming" || res.Unavailable[0].Reason != "books db down" {
		t.Fatalf("unavailable = %+v", res.Unavailable)
	}
	if len(res.Trending) != 4 || len(res.NewReleases) != 4 {
		t.Fatalf("feed rows must be intact: %+v", res)
	}
}

// fixedESTNow is a fixed instant in a zone behind UTC whose UTC date is one
// day AHEAD of its local date (2026-11-25 21:00 EST = 2026-11-26 02:00
// UTC) — the band in which local-date truncation diverges from the
// documented UTC-date semantics (issue #139). The window tests inject it so
// they never read the wall clock.
var fixedESTNow = time.Date(2026, 11, 25, 21, 0, 0, 0, time.FixedZone("EST", -5*3600))

// TestDiscover_UpcomingWindow: the service asks the calendar for exactly
// [today UTC, today+14d UTC] and for all three calendar kinds (movie, tv,
// music) in that order. The injected now is non-UTC with a local date one
// day behind its UTC date (issue #139): the window must still be anchored
// on the UTC date.
func TestDiscover_UpcomingWindow(t *testing.T) {
	deps, _, _, _, _, cal := allDeps()
	s := New(deps)
	s.now = func() time.Time { return fixedESTNow }

	_, err := s.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	// fixedESTNow is 2026-11-26 02:00 UTC: the window starts at the UTC
	// date's midnight, not the (earlier) local date's.
	wantStart := time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC)
	wantEnd := wantStart.AddDate(0, 0, upcomingWindowDays)
	if !cal.start.Equal(wantStart) || !cal.end.Equal(wantEnd) {
		t.Fatalf("window = %s .. %s, want %s .. %s", cal.start, cal.end, wantStart, wantEnd)
	}
	if len(cal.kinds) != 3 || cal.kinds[0] != cdom.KindMovie || cal.kinds[1] != cdom.KindTV || cal.kinds[2] != cdom.KindMusic {
		t.Fatalf("kinds = %v, want [movie tv music]", cal.kinds)
	}
}

// TestDiscover_UpcomingWindowUTCAnchored: the calendar window is anchored on
// the UTC calendar date of `now`, not its local date: a fixed now in a zone
// behind UTC whose local date lags the UTC date by one day still yields
// exactly [2026-11-26 00:00 UTC, 2026-12-10 00:00 UTC] (issue #139).
func TestDiscover_UpcomingWindowUTCAnchored(t *testing.T) {
	deps, _, _, _, _, cal := allDeps()
	s := New(deps)
	s.now = func() time.Time { return fixedESTNow }

	_, err := s.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	wantStart := time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2026, 12, 10, 0, 0, 0, 0, time.UTC)
	if !cal.start.Equal(wantStart) || !cal.end.Equal(wantEnd) {
		t.Fatalf("window = %s .. %s, want %s .. %s", cal.start, cal.end, wantStart, wantEnd)
	}
}

// TestDiscover_MusicWindows: the music cells reach back exactly
// musicTrendingWindowDays (trending) and musicNewReleasesWindowDays
// (new_releases), both anchored at the service-side `now`.
func TestDiscover_MusicWindows(t *testing.T) {
	now := time.Now().UTC()
	deps, mb, _, _, _, _ := allDeps()
	s := New(deps)

	_, err := s.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if !mb.trendSince.Equal(mb.trendUntil.AddDate(0, 0, -musicTrendingWindowDays)) {
		t.Fatalf("trending window span = %s .. %s, want exactly %d days", mb.trendSince, mb.trendUntil, musicTrendingWindowDays)
	}
	if d := mb.trendUntil.Sub(now); d < -2*time.Second || d > 2*time.Second {
		t.Fatalf("trendUntil = %s, want within 2s of %s", mb.trendUntil, now)
	}
	if !mb.relSince.Equal(mb.relUntil.AddDate(0, 0, -musicNewReleasesWindowDays)) {
		t.Fatalf("releases window span = %s .. %s, want exactly %d days", mb.relSince, mb.relUntil, musicNewReleasesWindowDays)
	}
}

// TestDiscover_RecentTVWindow: the new_releases:tv cell reaches back
// exactly recentWindowDays — since and until both derive from the same
// service-side `now`, so their span is asserted exactly while the anchor
// against the test clock gets a small drift tolerance.
func TestDiscover_RecentTVWindow(t *testing.T) {
	now := time.Now().UTC()
	tmdb := &fakeTMDB{}
	deps, _, _, _, _, _ := allDeps()
	deps.TMDB = tmdb
	s := New(deps)

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
// future cutoff drops yesterday. Both services share one fixed non-UTC now
// (issue #139) and the air dates are anchored on its UTC date, so the test
// never reads the wall clock.
func TestDiscover_UpcomingWindowEndToEnd(t *testing.T) {
	// fixedESTNow is 2026-11-26 02:00 UTC; air dates are anchored on that
	// UTC date at 21:00 UTC.
	air := func(d int) *time.Time {
		t := time.Date(2026, 11, 26, 21, 0, 0, 0, time.UTC).AddDate(0, 0, d)
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
	cal := calsvc.NewWithNow(repo, func() time.Time { return fixedESTNow })
	s := New(Deps{Calendar: cal})
	s.now = func() time.Time { return fixedESTNow }

	res, err := s.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	// The [2026-11-26, 2026-12-10] UTC window keeps the +1d and +13d
	// airings and excludes the +15d one and yesterday (2026-11-25 UTC).
	if len(res.Upcoming) != 2 {
		t.Fatalf("upcoming = %+v, want 2 (the tv +1d and +13d airings)", res.Upcoming)
	}
	if res.Upcoming[0].Title != "InOne" || res.Upcoming[1].Title != "InThirteen" {
		t.Fatalf("upcoming = %+v", res.Upcoming)
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
	deps, _, _, _, _, _ := allDeps()
	s := New(deps)
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
	if len(items) != 4 {
		t.Fatalf("trending = %d items, want 4", len(items))
	}
	for _, k := range []string{"kind", "sourceId", "title", "year", "subtitle", "posterUrl", "date"} {
		if _, ok := items[0][k]; !ok {
			t.Fatalf("missing item field %q in %s", k, b)
		}
	}
}

// TestDiscover_CellOrder: with every feed down, unavailable lists all
// twelve cells in the documented cell order (per row: movie, tv, music,
// book).
func TestDiscover_CellOrder(t *testing.T) {
	s := New(Deps{})
	res, err := s.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	want := []string{
		"movie:trending", "tv:trending", "music:trending", "book:trending",
		"movie:upcoming", "tv:upcoming", "music:upcoming", "book:upcoming",
		"movie:new_releases", "tv:new_releases", "music:new_releases", "book:new_releases",
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

// TestDiscover_BookUpcoming: the upcoming:book cell keeps only monitored
// titles with a current-or-later-year edition that are not in the
// library; the earliest future edition year is the cell's year.
func TestDiscover_BookUpcoming(t *testing.T) {
	thisYear := time.Now().UTC().Year()
	books := &fakeBooks{
		books: []bdomain.Book{
			{Title: bdomain.Title{ID: 1, Name: "Future", Monitored: true}, AuthorName: "A"},
			{Title: bdomain.Title{ID: 2, Name: "InLibrary", Monitored: true}, AuthorName: "B", HasFile: true},
			{Title: bdomain.Title{ID: 3, Name: "Unmonitored", Monitored: false}, AuthorName: "C"},
			{Title: bdomain.Title{ID: 4, Name: "Past", Monitored: true}, AuthorName: "D"},
			{Title: bdomain.Title{ID: 5, Name: "NoFutureEdition", Monitored: true}, AuthorName: "E"},
		},
		editions: map[int64][]bdomain.Edition{
			1: {{ID: 11, TitleID: 1, Year: thisYear + 2}, {ID: 12, TitleID: 1, Year: thisYear}},
			2: {{ID: 21, TitleID: 2, Year: thisYear + 1}},
			3: {{ID: 31, TitleID: 3, Year: thisYear + 1}},
			4: {{ID: 41, TitleID: 4, Year: thisYear - 1}},
			5: {{ID: 51, TitleID: 5, Year: 2010}, {ID: 52, TitleID: 5, Year: 0}},
		},
	}
	s := New(Deps{Books: books})

	res, err := s.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	var bookItem *Item
	for i := range res.Upcoming {
		if res.Upcoming[i].Kind == "book" {
			bookItem = &res.Upcoming[i]
		}
	}
	if bookItem == nil {
		t.Fatalf("upcoming = %+v, want the book cell with the Future title", res.Upcoming)
	}
	if bookItem.SourceID != "1" || bookItem.Title != "Future" || bookItem.Subtitle != "A" {
		t.Fatalf("book item = %+v", bookItem)
	}
	// The EARLIEST future edition year wins (thisYear beats thisYear+2).
	if bookItem.Year != thisYear {
		t.Fatalf("book year = %d, want the earliest future edition year %d", bookItem.Year, thisYear)
	}
	// Only one book item: InLibrary (has a file), Unmonitored, Past
	// (last year), and NoFutureEdition are all excluded.
	if len(res.Upcoming) != 1 {
		t.Fatalf("upcoming = %+v, want exactly the Future title", res.Upcoming)
	}
}

// TestDiscover_BookUpcomingEmptyIsNotError: a library with no matching
// titles renders an empty (NOT unavailable) book:upcoming cell — the
// domain expresses the query, so no empty-by-design exception.
func TestDiscover_BookUpcomingEmptyIsNotError(t *testing.T) {
	s := New(Deps{Books: &fakeBooks{books: []bdomain.Book{{Title: bdomain.Title{ID: 1, Name: "NothingSoon", Monitored: true}, AuthorName: "A"}}}})

	res, err := s.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(res.Upcoming) != 0 {
		t.Fatalf("upcoming = %+v, want empty", res.Upcoming)
	}
	for _, c := range res.Unavailable {
		if c.Cell == "book:upcoming" {
			t.Fatalf("book:upcoming must not be unavailable for a legitimately empty cell: %+v", res.Unavailable)
		}
	}
}

// TestDiscover_GoogleBooksKeylessWording: the keyless cell reason is
// exactly the R3-style wording (mirroring the TMDB keyless reason).
func TestDiscover_GoogleBooksKeylessWording(t *testing.T) {
	s := New(Deps{GoogleBooks: &fakeGoogleBooks{err: providers.ErrNotConfigured}, OpenLibrary: &fakeOpenLibrary{}})
	res, err := s.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	var reason string
	for _, c := range res.Unavailable {
		if c.Cell == "book:new_releases" {
			reason = c.Reason
		}
	}
	if reason != reasonGoogleBooksNotConfigured {
		t.Fatalf("reason = %q, want %q", reason, reasonGoogleBooksNotConfigured)
	}
}
