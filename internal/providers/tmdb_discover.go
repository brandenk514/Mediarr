package providers

import (
	"context"
	"net/url"
	"strconv"
	"time"
)

// ---- Unified Discover feeds (issue #118) ----

// DiscoverItem is one normalized feed row for the unified Discover page
// (issue #118): a kind-tagged title carrying the minimum the cell card
// needs. Providers that can feed the page produce these; the discover
// service composes them with calendar rows into the page's cells and caps
// each cell.
type DiscoverItem struct {
	Kind      string // "movie" | "tv" | "music" | "book"
	SourceID  string // TMDB id (TMDB-sourced rows)
	Title     string
	Year      int // release/first-air year; 0 when unknown
	Subtitle  string
	PosterURL string // "" → the UI renders an initials stand-in
	// Date is the release/first-air date (UTC midnight); zero when unknown.
	Date time.Time
}

// tmdbFeedItems folds a TMDB list payload (/trending/{type}/{window},
// /movie/now_playing, /discover/tv — all share the paged "results" shape)
// into DiscoverItems. kind selects which result fields carry the title and
// date (movies: title/release_date; tv: name/first_air_date). Rows without
// a title are dropped; an unknown date leaves Year 0 and Date zero.
func (t *TMDB) tmdbFeedItems(ctx context.Context, path string, extra url.Values, kind string) ([]DiscoverItem, error) {
	if t.AuthHeader == "" {
		return nil, ErrNotConfigured
	}
	values := url.Values{}
	for k, vs := range extra {
		values[k] = vs
	}
	t.Base = t.baseURL()
	var resp tmdbTrendingResponse
	if err := t.getJSON(ctx, path, values, &resp); err != nil {
		return nil, err
	}
	isTV := kind == "tv"
	items := make([]DiscoverItem, 0, len(resp.Results))
	for _, r := range resp.Results {
		title, date := r.Title, r.ReleaseDate
		if isTV {
			title, date = r.Name, r.FirstAir
		}
		if title == "" {
			continue
		}
		item := DiscoverItem{
			Kind:      kind,
			SourceID:  strconv.Itoa(r.ID),
			Title:     title,
			PosterURL: t.posterURL(r.PosterPath),
		}
		if date != "" {
			if d, err := time.Parse("2006-01-02", date); err == nil {
				item.Date = d
			}
			if len(date) >= 4 {
				item.Year, _ = strconv.Atoi(date[:4])
			}
		}
		items = append(items, item)
	}
	return items, nil
}

// TrendingMovies fetches the TMDB trending-movies feed
// (/trending/movie/week) — the unified Discover page's trending:movie cell.
// An unset API key returns ErrNotConfigured (the page degrades the cell to
// a CellError; a cell failure never fails the request).
func (t *TMDB) TrendingMovies(ctx context.Context) ([]DiscoverItem, error) {
	return t.tmdbFeedItems(ctx, "/trending/movie/week", nil, "movie")
}

// TrendingTV fetches the TMDB trending-TV feed (/trending/tv/week) — the
// trending:tv cell. Same keying and degradation as TrendingMovies.
func (t *TMDB) TrendingTV(ctx context.Context) ([]DiscoverItem, error) {
	return t.tmdbFeedItems(ctx, "/trending/tv/week", nil, "tv")
}

// NowPlayingMovies fetches /movie/now_playing — the new_releases:movie cell.
func (t *TMDB) NowPlayingMovies(ctx context.Context) ([]DiscoverItem, error) {
	return t.tmdbFeedItems(ctx, "/movie/now_playing", nil, "movie")
}

// RecentTV fetches /discover/tv for shows first-aired within [since, until],
// sorted most-recently-aired first (first_air_date.desc) — the
// new_releases:tv cell. The window comes from the discover service (last
// ~30 days).
func (t *TMDB) RecentTV(ctx context.Context, since, until time.Time) ([]DiscoverItem, error) {
	extra := url.Values{}
	extra.Set("first_air_date.gte", since.Format("2006-01-02"))
	extra.Set("first_air_date.lte", until.Format("2006-01-02"))
	extra.Set("sort_by", "first_air_date.desc")
	return t.tmdbFeedItems(ctx, "/discover/tv", extra, "tv")
}
