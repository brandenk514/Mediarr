package providers

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"
	"time"
)

// TestTMDBTrendingMovies: the trending:movie cell's feed — path, title/
// release-date folding, poster URL, empty-title drop, and keyless sentinel.
func TestTMDBTrendingMovies(t *testing.T) {
	routes := map[string]http.HandlerFunc{
		"/trending/movie/week": jsonRoute(t, http.StatusOK, `{
			"results": [
				{"id": 1, "title": "Alpha", "release_date": "2024-05-01", "poster_path": "/p1.jpg"},
				{"id": 2, "title": "Beta", "release_date": ""},
				{"id": 3, "title": ""}
			],
			"total_results": 3
		}`),
	}
	m, paths := newTMDBRouter(t, routes)

	items, err := m.TrendingMovies(context.Background())
	if err != nil {
		t.Fatalf("TrendingMovies: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2 (empty title dropped)", len(items))
	}
	wantDate, _ := time.Parse("2006-01-02", "2024-05-01")
	if items[0].Kind != "movie" || items[0].SourceID != "1" || items[0].Title != "Alpha" ||
		items[0].Year != 2024 || items[0].Date != wantDate ||
		items[0].PosterURL != tmdbImageBase+"/p1.jpg" {
		t.Fatalf("item 0 = %+v", items[0])
	}
	if items[1].Year != 0 || !items[1].Date.IsZero() || items[1].PosterURL != "" {
		t.Fatalf("item 1 (no date) = %+v", items[1])
	}
	if (*paths)[0] != "/trending/movie/week" {
		t.Fatalf("path = %q", (*paths)[0])
	}
}

// TestTMDBTrendingTV: the trending:tv cell — /trending/tv/week folds
// name/first_air_date and tags the rows kind=tv.
func TestTMDBTrendingTV(t *testing.T) {
	routes := map[string]http.HandlerFunc{
		"/trending/tv/week": jsonRoute(t, http.StatusOK, `{
			"results": [
				{"id": 7, "name": "Show A", "first_air_date": "2019-03-10", "poster_path": "/t1.jpg"}
			],
			"total_results": 1
		}`),
	}
	m, paths := newTMDBRouter(t, routes)

	items, err := m.TrendingTV(context.Background())
	if err != nil {
		t.Fatalf("TrendingTV: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].Kind != "tv" || items[0].SourceID != "7" || items[0].Title != "Show A" ||
		items[0].Year != 2019 || items[0].PosterURL != tmdbImageBase+"/t1.jpg" {
		t.Fatalf("item 0 = %+v", items[0])
	}
	if (*paths)[0] != "/trending/tv/week" {
		t.Fatalf("path = %q", (*paths)[0])
	}
}

// TestTMDBNowPlayingMovies: the new_releases:movie cell — /movie/now_playing.
func TestTMDBNowPlayingMovies(t *testing.T) {
	routes := map[string]http.HandlerFunc{
		"/movie/now_playing": jsonRoute(t, http.StatusOK, `{
			"results": [
				{"id": 11, "title": "Now A", "release_date": "2026-11-01"},
				{"id": 12, "title": "Now B", "release_date": ""}
			],
			"total_results": 2
		}`),
	}
	m, paths := newTMDBRouter(t, routes)

	items, err := m.NowPlayingMovies(context.Background())
	if err != nil {
		t.Fatalf("NowPlayingMovies: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
	if items[0].Kind != "movie" || items[0].Title != "Now A" || items[0].Year != 2026 {
		t.Fatalf("item 0 = %+v", items[0])
	}
	if items[1].Year != 0 {
		t.Fatalf("item 1 year = %d, want 0 (unknown date)", items[1].Year)
	}
	if (*paths)[0] != "/movie/now_playing" {
		t.Fatalf("path = %q", (*paths)[0])
	}
}

// TestTMDBRecentTV: the new_releases:tv cell — /discover/tv with the
// first_air_date window and the recency sort, exactly as the live TMDB v3
// API documents them.
func TestTMDBRecentTV(t *testing.T) {
	var q url.Values
	routes := map[string]http.HandlerFunc{
		"/discover/tv": func(w http.ResponseWriter, r *http.Request) {
			q = r.URL.Query()
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"results": [
					{"id": 21, "name": "Fresh Show", "first_air_date": "2026-11-20"},
					{"id": 22, "name": "Newer Show", "first_air_date": "2026-11-10"}
				],
				"total_results": 2
			}`))
		},
	}
	m, paths := newTMDBRouter(t, routes)

	since := time.Date(2026, 10, 25, 12, 30, 0, 0, time.UTC)
	until := time.Date(2026, 11, 24, 8, 0, 0, 0, time.UTC)
	items, err := m.RecentTV(context.Background(), since, until)
	if err != nil {
		t.Fatalf("RecentTV: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
	if q.Get("first_air_date.gte") != "2026-10-25" {
		t.Errorf("first_air_date.gte = %q, want 2026-10-25", q.Get("first_air_date.gte"))
	}
	if q.Get("first_air_date.lte") != "2026-11-24" {
		t.Errorf("first_air_date.lte = %q, want 2026-11-24", q.Get("first_air_date.lte"))
	}
	if q.Get("sort_by") != "first_air_date.desc" {
		t.Errorf("sort_by = %q, want first_air_date.desc", q.Get("sort_by"))
	}
	if items[0].Kind != "tv" || items[0].Title != "Fresh Show" || items[0].Year != 2026 {
		t.Fatalf("item 0 = %+v", items[0])
	}
	if (*paths)[0] != "/discover/tv" {
		t.Fatalf("path = %q", (*paths)[0])
	}
}

// TestTMDBFeedItems_NotConfigured: all four unified-Discover feeds return
// the not-configured sentinel keyless (the page degrades the cells to
// CellErrors).
func TestTMDBFeedItems_NotConfigured(t *testing.T) {
	m := NewTMDB("")
	since, until := time.Now().AddDate(0, 0, -30), time.Now()
	if _, err := m.TrendingMovies(context.Background()); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("TrendingMovies: err = %v, want ErrNotConfigured", err)
	}
	if _, err := m.TrendingTV(context.Background()); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("TrendingTV: err = %v, want ErrNotConfigured", err)
	}
	if _, err := m.NowPlayingMovies(context.Background()); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("NowPlayingMovies: err = %v, want ErrNotConfigured", err)
	}
	if _, err := m.RecentTV(context.Background(), since, until); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("RecentTV: err = %v, want ErrNotConfigured", err)
	}
}

// TestTMDBFeedItems_UpstreamError: a non-2xx upstream answer surfaces as
// ErrBadResponse (the page maps it to a per-cell CellError, not a 5xx).
func TestTMDBFeedItems_UpstreamError(t *testing.T) {
	routes := map[string]http.HandlerFunc{
		"/trending/tv/week": jsonRoute(t, http.StatusInternalServerError, `{"status_message": "boom"}`),
	}
	m, _ := newTMDBRouter(t, routes)

	if _, err := m.TrendingTV(context.Background()); !errors.Is(err, ErrBadResponse) {
		t.Fatalf("err = %v, want ErrBadResponse", err)
	}
}
