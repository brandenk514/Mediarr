package providers

import (
	"context"
	"net/url"
	"time"
)

// ---- Unified Discover feeds (issue #118, A2) ----
//
// The Google Books volumes API is the new_releases:discover PRIMARY
// when the adapter is configured (a non-empty API key; config:
// MEDIARR_GOOGLE_BOOKS_API_KEY). Keyless it is unusable for this cell
// (live-probed: the anonymous quota is exhausted — HTTP 429
// RESOURCE_EXHAUSTED on /volumes?orderBy=newest), so the unified
// Discover page's keyless state serves the cell from the Open Library
// first_publish_year fallback feed instead (the discover service owns
// that switch; see services/discover).

// gbDiscoverQuery is the new_releases:discover query: a broad
// subject scope (fiction + nonfiction) so the feed stays general
// rather than one-genre-biased.
const gbDiscoverQuery = "subject:fiction OR subject:nonfiction"

// NewReleasesBooks fetches the most-recently-published volumes
// (orderBy=newest) for the new_releases:discover cell. An unset API key
// returns ErrNotConfigured without a network call (the discover service
// treats that as the keyless fallback state, never a 5xx).
func (g *GoogleBooks) NewReleasesBooks(ctx context.Context) ([]DiscoverItem, error) {
	if !g.Configured() {
		return nil, ErrNotConfigured
	}
	values := url.Values{}
	values.Set("q", gbDiscoverQuery)
	values.Set("orderBy", "newest")
	values.Set("maxResults", "25")
	values.Set("key", g.APIKey)

	g.Base = g.baseURL()
	var resp gbVolumesResponse
	if err := g.getJSON(ctx, "/volumes", values, &resp); err != nil {
		return nil, err
	}
	out := make([]DiscoverItem, 0, len(resp.Items))
	for _, v := range resp.Items {
		item, ok := g.mapDiscoverVolume(v)
		if !ok {
			continue
		}
		out = append(out, item)
	}
	return out, nil
}

// mapDiscoverVolume maps one volumes item onto a book DiscoverItem: the
// Google volume id as the source id, the volumeInfo title/first
// author/publication date, and the thumbnail as the poster ("" when the
// volume has no cover → the UI renders the initials stand-in). A row
// without a title is not mappable (ok=false).
func (g *GoogleBooks) mapDiscoverVolume(v gbVolume) (DiscoverItem, bool) {
	vi := v.VolumeInfo
	if vi.Title == "" {
		return DiscoverItem{}, false
	}
	item := DiscoverItem{
		Kind:      "book",
		SourceID:  v.ID,
		Title:     vi.Title,
		Year:      gbYear(vi.PublishedDate),
		PosterURL: vi.ImageLinks.Thumbnail,
	}
	if len(vi.Authors) > 0 {
		item.Subtitle = vi.Authors[0]
	}
	if vi.PublishedDate != "" {
		if d, err := time.Parse("2006-01-02", vi.PublishedDate); err == nil {
			item.Date = d
		}
	}
	return item, true
}
