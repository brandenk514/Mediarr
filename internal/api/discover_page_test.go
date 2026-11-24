package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/brandenk514/mediarr/internal/auth"
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

// newDiscoverPageTestServer wires an authenticated server with the unified
// Discover route over a fake TMDB and a real calendar service on the
// in-memory repo (shared with the calendar endpoint tests).
func newDiscoverPageTestServer(t *testing.T, tmdb *fakePageTMDB, repo *memCalendarRepo) (*Server, string) {
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
	s.SetDiscoverPage(&DiscoverPageDeps{Svc: discoversvc.New(tmdb, calsvc.New(repo))})
	return s, pair.Raw
}

func pageRows() *memCalendarRepo {
	now := time.Now().UTC()
	air := func(d int) *time.Time {
		t := now.AddDate(0, 0, d)
		return &t
	}
	return &memCalendarRepo{
		rows: []cdom.EpisodeRow{
			{SeriesID: 1, SeriesTitle: "ShowA", Season: 1, Episode: 1, EpisodeTitle: "Pilot", AirDate: air(1), Monitored: true},
		},
		movies: []cdom.MovieRow{
			{ID: 9, Title: "MovieA", Year: 2026, ReleaseDate: air(1), Monitored: true, QualityProfile: "HD-1080p"},
		},
	}
}

func pageTMDB() *fakePageTMDB {
	item := func(kind, id string) providers.DiscoverItem {
		return providers.DiscoverItem{Kind: kind, SourceID: id, Title: kind + " " + id, Year: 2024, Date: time.Date(2024, 5, 1, 0, 0, 0, 0, time.UTC)}
	}
	return &fakePageTMDB{items: map[string][]providers.DiscoverItem{
		"movie:trending":     {item("movie", "m1")},
		"tv:trending":        {item("tv", "t1")},
		"movie:new_releases": {item("movie", "m2")},
		"tv:new_releases":    {item("tv", "t2")},
	}}
}

// TestDiscoverPage_Full: 200 with movie+tv cells in all three rows and an
// empty unavailable list.
func TestDiscoverPage_Full(t *testing.T) {
	s, token := newDiscoverPageTestServer(t, pageTMDB(), pageRows())

	rec := doReq(t, s.Handler(), http.MethodGet, "/api/v1/discover", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var res discoversvc.Response
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(res.Trending) != 2 || len(res.Upcoming) != 2 || len(res.NewReleases) != 2 {
		t.Fatalf("rows = %d/%d/%d, want 2/2/2", len(res.Trending), len(res.Upcoming), len(res.NewReleases))
	}
	if len(res.Unavailable) != 0 {
		t.Fatalf("unavailable = %+v, want empty", res.Unavailable)
	}
	if res.Trending[0].Kind != "movie" || res.Trending[0].SourceID != "m1" {
		t.Fatalf("trending[0] = %+v", res.Trending[0])
	}
	if res.Trending[1].Kind != "tv" || res.Trending[1].SourceID != "t1" {
		t.Fatalf("trending[1] = %+v", res.Trending[1])
	}
	if res.Upcoming[0].Kind != "movie" || res.Upcoming[0].SourceID != "9" || res.Upcoming[0].Title != "MovieA" {
		t.Fatalf("upcoming[0] = %+v", res.Upcoming[0])
	}
	if res.Upcoming[1].Kind != "tv" || res.Upcoming[1].SourceID != "1" || res.Upcoming[1].Subtitle != "S01E01" {
		t.Fatalf("upcoming[1] = %+v", res.Upcoming[1])
	}
	if res.NewReleases[0].Kind != "movie" || res.NewReleases[1].Kind != "tv" {
		t.Fatalf("newReleases = %+v", res.NewReleases)
	}
}

// TestDiscoverPage_Keyless: a not-configured TMDB is a 200 with the four
// TMDB cells in unavailable (R3 keyless wording) while the upcoming cells
// still populate from the calendar — never a 5xx.
func TestDiscoverPage_Keyless(t *testing.T) {
	tmdb := pageTMDB()
	tmdb.err = providers.ErrNotConfigured
	s, token := newDiscoverPageTestServer(t, tmdb, pageRows())

	rec := doReq(t, s.Handler(), http.MethodGet, "/api/v1/discover", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", rec.Code, rec.Body.String())
	}
	var res discoversvc.Response
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(res.Trending) != 0 || len(res.NewReleases) != 0 {
		t.Fatalf("degraded rows must be empty: %+v", res)
	}
	if len(res.Upcoming) != 2 {
		t.Fatalf("upcoming must still populate: %+v", res.Upcoming)
	}
	if len(res.Unavailable) != 4 {
		t.Fatalf("unavailable = %+v, want 4", res.Unavailable)
	}
	for _, c := range res.Unavailable {
		if c.Reason != "tmdb is not configured (missing API key)" {
			t.Fatalf("cell %s reason = %q", c.Cell, c.Reason)
		}
	}
}

// TestDiscoverPage_EmptyIs200Arrays: with a keyless TMDB and an empty
// calendar the response is a 200 of [] (never null) plus the four TMDB
// cells in unavailable.
func TestDiscoverPage_EmptyIs200Arrays(t *testing.T) {
	tmdb := &fakePageTMDB{err: providers.ErrNotConfigured}
	s, token := newDiscoverPageTestServer(t, tmdb, &memCalendarRepo{})

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
	if len(res.Unavailable) != 4 {
		t.Fatalf("unavailable = %+v, want the 4 TMDB cells", res.Unavailable)
	}
}

// TestDiscoverPage_Auth: unauthenticated requests are rejected.
func TestDiscoverPage_Auth(t *testing.T) {
	s, _ := newDiscoverPageTestServer(t, pageTMDB(), pageRows())

	rec := doReq(t, s.Handler(), http.MethodGet, "/api/v1/discover", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// TestDiscoverPage_R3RouteUnchanged: both routes coexist on one server —
// the R3 /discover/movies behavior is untouched by the new /discover.
func TestDiscoverPage_R3RouteUnchanged(t *testing.T) {
	s, token := newDiscoverPageTestServer(t, pageTMDB(), pageRows())
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
