package providers

import (
	"context"
	"net/url"
	"strconv"
)

// ---- Unified Discover feeds (issue #118, A2) ----
//
// Open Library is the keyless book-metadata source for the unified
// Discover page's book cells. The trending:discover cell is the
// /trending/weekly feed; the new_releases:discover cell's keyless
// fallback is the first_publish_year search (the Google Books keyed
// feed is the primary when configured — see googlebooks_discover.go).

// olTrendingResponse is the /trending/weekly.json payload subset. The
// .json suffix is MANDATORY — the bare /trending/weekly path answers
// HTML, not JSON (live-probed).
type olTrendingResponse struct {
	Works []struct {
		Key              string   `json:"key"` // "/works/OL…W"
		Title            string   `json:"title"`
		AuthorName       []string `json:"author_name"`
		CoverI           int      `json:"cover_i"` // cover id; 0 = no cover
		FirstPublishYear int      `json:"first_publish_year"`
	} `json:"works"`
}

// TrendingWeekly fetches Open Library's weekly trending works (the
// trending:discover book cell). Rows without a key or title are
// dropped; the server order is preserved.
func (o *OpenLibrary) TrendingWeekly(ctx context.Context) ([]DiscoverItem, error) {
	o.Base = o.baseURL()
	var resp olTrendingResponse
	if err := o.getJSON(ctx, "/trending/weekly.json", nil, &resp); err != nil {
		return nil, err
	}
	out := make([]DiscoverItem, 0, len(resp.Works))
	for _, w := range resp.Works {
		if w.Key == "" || w.Title == "" {
			continue
		}
		out = append(out, olWorkItem(w.Key, w.Title, w.AuthorName, w.CoverI, w.FirstPublishYear))
	}
	return out, nil
}

// NewReleasesByYear fetches works first published in year (the
// new_releases:discover keyless fallback cell). Filter choice
// (live-probed): `q=first_publish_year:YYYY` matches the work-level
// first-publish year. Never `sort=new` (that is date-ADDED-to-OL —
// garbage for this cell) and not the `publish_year` field (it matches
// any edition, so reissues pollute the feed).
func (o *OpenLibrary) NewReleasesByYear(ctx context.Context, year int) ([]DiscoverItem, error) {
	values := url.Values{}
	values.Set("q", "first_publish_year:"+strconv.Itoa(year))
	values.Set("limit", "25")
	resp, err := o.search(ctx, values)
	if err != nil {
		return nil, err
	}
	out := make([]DiscoverItem, 0, len(resp.Docs))
	for _, d := range resp.Docs {
		if d.Key == "" || d.Title == "" {
			continue
		}
		out = append(out, olWorkItem(d.Key, d.Title, d.AuthorName, d.CoverI, d.FirstPublishYear))
	}
	return out, nil
}

// olWorkItem folds one Open Library work row into a book DiscoverItem:
// the bare work id as the source id, the first author as the subtitle,
// and the documented covers.openlibrary.org URL when the row has a
// cover (the UI renders the initials stand-in when it does not).
func olWorkItem(key, title string, authorName []string, coverI, year int) DiscoverItem {
	item := DiscoverItem{
		Kind:      "book",
		SourceID:  NormalizeOLID(key),
		Title:     title,
		Year:      year,
		PosterURL: olCoverURL(coverI),
	}
	if len(authorName) > 0 {
		item.Subtitle = authorName[0]
	}
	return item
}
