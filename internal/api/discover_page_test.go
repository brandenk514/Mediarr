package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/brandenk514/mediarr/internal/auth"
	bdomain "github.com/brandenk514/mediarr/internal/domains/books"
	cdom "github.com/brandenk514/mediarr/internal/domains/calendar"
	"github.com/brandenk514/mediarr/internal/health"
	"github.com/brandenk514/mediarr/internal/providers"
	calsvc "github.com/brandenk514/mediarr/internal/services/calendar"
	discoversvc "github.com/brandenk514/mediarr/internal/services/discover"
	moviesvc "github.com/brandenk514/mediarr/internal/services/movies"
)

// fakePageTMDB implements discoversvc.TMDBFeeds for the endpoint tests.
type fakePageTMDB struct {
	items map[string][]providers.DiscoverItem
	err   error
}

func (f *fakePageTMDB) TrendingMovies(context.Context) ([]providers.DiscoverItem, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.items["movie:trending"], nil
}

func (f *fakePageTMDB) TrendingTV(context.Context) ([]providers.DiscoverItem, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.items["tv:trending"], nil
}

func (f *fakePageTMDB) NowPlayingMovies(context.Context) ([]providers.DiscoverItem, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.items["movie:new_releases"], nil
}

func (f *fakePageTMDB) RecentTV(context.Context, time.Time, time.Time) ([]providers.DiscoverItem, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.items["tv:new_releases"], nil
}

// fakePageMusicBrainz implements discoversvc.MusicBrainzFeeds for the
// endpoint tests.
type fakePageMusicBrainz struct {
	trending []providers.DiscoverItem
	releases []providers.DiscoverItem
	err      error
}

func (f *fakePageMusicBrainz) TrendingReleaseGroups(context.Context, time.Time, time.Time) ([]providers.DiscoverItem, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.trending, nil
}

func (f *fakePageMusicBrainz) NewReleases(context.Context, time.Time, time.Time) ([]providers.DiscoverItem, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.releases, nil
}

// fakePageOpenLibrary implements discoversvc.OpenLibraryFeeds for the
// endpoint tests.
type fakePageOpenLibrary struct {
	trending []providers.DiscoverItem
	releases []providers.DiscoverItem
	err      error
}

func (f *fakePageOpenLibrary) TrendingWeekly(context.Context) ([]providers.DiscoverItem, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.trending, nil
}

func (f *fakePageOpenLibrary) NewReleasesByYear(context.Context, int) ([]providers.DiscoverItem, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.releases, nil
}

// fakePageGoogleBooks implements discoversvc.GoogleBooksFeeds for the
// endpoint tests.
type fakePageGoogleBooks struct {
	releases []providers.DiscoverItem
	err      error
}

func (f *fakePageGoogleBooks) NewReleasesBooks(context.Context) ([]providers.DiscoverItem, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.releases, nil
}

// fakePageBooks implements discoversvc.BookLibrary for the endpoint tests.
type fakePageBooks struct {
	books    []bdomain.Book
	editions map[int64][]bdomain.Edition
	err      error
}

func (f *fakePageBooks) ListBooks(context.Context) ([]bdomain.Book, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.books, nil
}

func (f *fakePageBooks) ListEditions(_ context.Context, titleID int64) ([]bdomain.Edition, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.editions[titleID], nil
}

// newDiscoverPageTestServer wires an authenticated server with the unified
// Discover route over the given dependencies.
func newDiscoverPageTestServer(t *testing.T, deps discoversvc.Deps) (*Server, string) {
	t.Helper()
	store := newFakeStore()
	u, _ := store.CreateUser(context.Background(), "admin", "", "admin", "hash")
	pair, err := auth.NewToken("ma_live_")
	if err != nil {
		t.Fatalf("new token: %v", err)
	}
	if _, err := store.CreateToken(context.Background(), u, "test", pair, nil); err != nil {
		t.Fatalf("create token: %v", err)
	}
	s := NewServer(health.NewChecker(nil, nil))
	s.SetAuth(&AuthDeps{Repo: store})
	s.SetDiscoverPage(&DiscoverPageDeps{Svc: discoversvc.New(deps)})
	return s, pair.Raw
}

// pageDeps builds the full A2 dependency set for the endpoint tests: a
// canned TMDB, a calendar with one airing/release per kind, one feed item
// per music/book cell, and a books library with one upcoming title.
func pageDeps() discoversvc.Deps {
	item := func(kind, id string) providers.DiscoverItem {
		return providers.DiscoverItem{Kind: kind, SourceID: id, Title: kind + " " + id, Year: 2024, Date: time.Date(2024, 5, 1, 0, 0, 0, 0, time.UTC)}
	}
	now := time.Now().UTC()
	air := func(d int) *time.Time {
		t := now.AddDate(0, 0, d)
		return &t
	}
	return discoversvc.Deps{
		TMDB: &fakePageTMDB{items: map[string][]providers.DiscoverItem{
			"movie:trending":     {item("movie", "m1")},
			"tv:trending":        {item("tv", "t1")},
			"movie:new_releases": {item("movie", "m2")},
			"tv:new_releases":    {item("tv", "t2")},
		}},
		Calendar: calsvc.New(&memCalendarRepo{
			rows: []cdom.EpisodeRow{
				{SeriesID: 1, SeriesTitle: "ShowA", Season: 1, Episode: 1, EpisodeTitle: "Pilot", AirDate: air(1), Monitored: true},
			},
			movies: []cdom.MovieRow{
				{ID: 9, Title: "MovieA", Year: 2026, ReleaseDate: air(1), Monitored: true, QualityProfile: "HD-1080p"},
			},
			music: []cdom.MusicRow{
				{ArtistID: 31, ArtistTitle: "ArtistA", AlbumID: 32, AlbumTitle: "AlbumA", Year: 2026, ReleaseDate: air(2), Monitored: true},
			},
		}),
		MusicBrainz: &fakePageMusicBrainz{
			trending: []providers.DiscoverItem{item("music", "mu1")},
			releases: []providers.DiscoverItem{item("music", "mu2")},
		},
		OpenLibrary: &fakePageOpenLibrary{
			trending: []providers.DiscoverItem{item("book", "b1")},
			releases: []providers.DiscoverItem{item("book", "b2")},
		},
		GoogleBooks: &fakePageGoogleBooks{releases: []providers.DiscoverItem{item("book", "gb1")}},
		Books: &fakePageBooks{
			books:    []bdomain.Book{{Title: bdomain.Title{ID: 7, Name: "Upcoming Book", Monitored: true}, AuthorName: "An Author"}},
			editions: map[int64][]bdomain.Edition{7: {{ID: 1, TitleID: 7, Year: now.Year() + 1}}},
		},
	}
}

// TestDiscoverPage_Full: 200 with all twelve cells (4 kinds × 3 rows) and
// an empty unavailable list.
func TestDiscoverPage_Full(t *testing.T) {
	s, token := newDiscoverPageTestServer(t, pageDeps())

	rec := doReq(t, s.Handler(), http.MethodGet, "/api/v1/discover", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var res discoversvc.Response
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, row := range []struct {
		name string
		got  []discoversvc.Item
	}{{"trending", res.Trending}, {"upcoming", res.Upcoming}, {"newReleases", res.NewReleases}} {
		if len(row.got) != 4 {
			t.Fatalf("%s = %d items, want 4 (one per kind): %+v", row.name, len(row.got), row.got)
		}
		wantKinds := []string{"movie", "tv", "music", "book"}
		for i, it := range row.got {
			if it.Kind != wantKinds[i] {
				t.Fatalf("%s[%d].kind = %q, want %q", row.name, i, it.Kind, wantKinds[i])
			}
		}
	}
	if len(res.Unavailable) != 0 {
		t.Fatalf("unavailable = %+v, want empty", res.Unavailable)
	}
	if res.Trending[2].SourceID != "mu1" || res.Trending[3].SourceID != "b1" {
		t.Fatalf("trending music/book = %+v", res.Trending[2:])
	}
	if res.Upcoming[2].Title != "AlbumA" || res.Upcoming[3].Title != "Upcoming Book" {
		t.Fatalf("upcoming music/book = %+v", res.Upcoming[2:])
	}
	// Keyed Google Books is the new_releases:book primary.
	if res.NewReleases[3].SourceID != "gb1" {
		t.Fatalf("newReleases[3] = %+v, want the Google Books row", res.NewReleases[3])
	}
}

// TestDiscoverPage_KeylessTMDB: a not-configured TMDB is a 200 with the
// four TMDB cells in unavailable (R3 keyless wording) while every other
// cell — including all music/book cells — still populates. Never a 5xx.
func TestDiscoverPage_KeylessTMDB(t *testing.T) {
	deps := pageDeps()
	deps.TMDB.(*fakePageTMDB).err = providers.ErrNotConfigured
	s, token := newDiscoverPageTestServer(t, deps)

	rec := doReq(t, s.Handler(), http.MethodGet, "/api/v1/discover", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", rec.Code, rec.Body.String())
	}
	var res discoversvc.Response
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(res.Unavailable) != 4 {
		t.Fatalf("unavailable = %+v, want the 4 TMDB cells", res.Unavailable)
	}
	for _, c := range res.Unavailable {
		if c.Reason != "tmdb is not configured (missing API key)" {
			t.Fatalf("cell %s reason = %q", c.Cell, c.Reason)
		}
	}
	if len(res.Trending) != 2 || len(res.Upcoming) != 4 || len(res.NewReleases) != 2 {
		t.Fatalf("rows = %d/%d/%d, want 2/4/2 (only the TMDB cells degraded)", len(res.Trending), len(res.Upcoming), len(res.NewReleases))
	}
}

// TestDiscoverPage_MusicBrainzOutage: a MusicBrainz failure degrades only
// the two music feed cells; the calendar-sourced music:upcoming and every
// other cell stay intact — per-kind degradation at the handler level.
func TestDiscoverPage_MusicBrainzOutage(t *testing.T) {
	deps := pageDeps()
	deps.MusicBrainz.(*fakePageMusicBrainz).err = errors.New("mb down")
	s, token := newDiscoverPageTestServer(t, deps)

	rec := doReq(t, s.Handler(), http.MethodGet, "/api/v1/discover", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", rec.Code, rec.Body.String())
	}
	var res discoversvc.Response
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(res.Unavailable) != 2 {
		t.Fatalf("unavailable = %+v, want the 2 music feed cells", res.Unavailable)
	}
	if res.Unavailable[0].Cell != "music:trending" || res.Unavailable[1].Cell != "music:new_releases" {
		t.Fatalf("unavailable = %+v", res.Unavailable)
	}
	if len(res.Upcoming) != 4 {
		t.Fatalf("upcoming = %+v, want all 4 kinds (music:upcoming is calendar-sourced)", res.Upcoming)
	}
}

// TestDiscoverPage_OpenLibraryOutage: an Open Library failure degrades
// only the book feed cells (trending + the keyless-fallback new-releases
// path, with the Google Books dep unwired); the library-sourced
// book:upcoming cell is intact.
func TestDiscoverPage_OpenLibraryOutage(t *testing.T) {
	deps := pageDeps()
	deps.GoogleBooks = nil
	deps.OpenLibrary.(*fakePageOpenLibrary).err = errors.New("ol down")
	s, token := newDiscoverPageTestServer(t, deps)

	rec := doReq(t, s.Handler(), http.MethodGet, "/api/v1/discover", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", rec.Code, rec.Body.String())
	}
	var res discoversvc.Response
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(res.Unavailable) != 2 {
		t.Fatalf("unavailable = %+v, want the 2 book feed cells", res.Unavailable)
	}
	if res.Unavailable[0].Cell != "book:trending" || res.Unavailable[1].Cell != "book:new_releases" {
		t.Fatalf("unavailable = %+v", res.Unavailable)
	}
	if len(res.Upcoming) != 4 {
		t.Fatalf("upcoming = %+v, want all 4 kinds", res.Upcoming)
	}
}

// TestDiscoverPage_KeylessGoogleBooks: a missing Google Books key (the
// normal keyless-dev state) is a 200: the new_releases:book cell serves
// the OpenLibrary first_publish_year fallback AND names the missing key in
// unavailable with the R3 keyless wording.
func TestDiscoverPage_KeylessGoogleBooks(t *testing.T) {
	deps := pageDeps()
	deps.GoogleBooks.(*fakePageGoogleBooks).err = providers.ErrNotConfigured
	s, token := newDiscoverPageTestServer(t, deps)

	rec := doReq(t, s.Handler(), http.MethodGet, "/api/v1/discover", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", rec.Code, rec.Body.String())
	}
	var res discoversvc.Response
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// The OpenLibrary fallback item IS in the row.
	var bookNR *discoversvc.Item
	for i := range res.NewReleases {
		if res.NewReleases[i].Kind == "book" {
			bookNR = &res.NewReleases[i]
		}
	}
	if bookNR == nil || bookNR.SourceID != "b2" {
		t.Fatalf("newReleases book = %+v, want the OpenLibrary fallback row", bookNR)
	}
	// AND the missing key is named in unavailable.
	var cellErr *discoversvc.CellError
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

// TestDiscoverPage_EmptyIs200Arrays: with every dependency unwired the
// response is a 200 of [] (never null) plus all twelve cells in
// unavailable.
func TestDiscoverPage_EmptyIs200Arrays(t *testing.T) {
	s, token := newDiscoverPageTestServer(t, discoversvc.Deps{})

	rec := doReq(t, s.Handler(), http.MethodGet, "/api/v1/discover", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{`"trending":[]`, `"upcoming":[]`, `"newReleases":[]`} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %s: %s", want, body)
		}
	}
	var res discoversvc.Response
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(res.Unavailable) != 12 {
		t.Fatalf("unavailable = %+v, want all 12 cells", res.Unavailable)
	}
}

// TestDiscoverPage_Auth: unauthenticated requests are rejected.
func TestDiscoverPage_Auth(t *testing.T) {
	s, _ := newDiscoverPageTestServer(t, pageDeps())

	rec := doReq(t, s.Handler(), http.MethodGet, "/api/v1/discover", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// TestDiscoverPage_R3RouteUnchanged: both routes coexist on one server —
// the R3 /discover/movies behavior is untouched by the new /discover.
func TestDiscoverPage_R3RouteUnchanged(t *testing.T) {
	s, token := newDiscoverPageTestServer(t, pageDeps())
	// R3 route on the same server: a nil discoverer keeps the R3 keyless
	// 503 shape.
	deps := moviesvc.Deps{Repo: newListMoviesRepo(), DefaultProfile: "HD-1080p"}
	svc := moviesvc.New(deps)
	s.SetMovies(&MoviesDeps{Svc: svc})
	s.SetDiscover(&DiscoverDeps{Svc: svc})

	rec := doReq(t, s.Handler(), http.MethodGet, "/api/v1/discover/movies", token)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("R3 status = %d body=%s, want 503 (nil discoverer)", rec.Code, rec.Body.String())
	}

	rec = doReq(t, s.Handler(), http.MethodGet, "/api/v1/discover", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("page status = %d body=%s", rec.Code, rec.Body.String())
	}
}
