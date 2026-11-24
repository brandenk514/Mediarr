package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ---- Unified Discover feeds (issue #118, A2) ----
//
// MusicBrainz is the keyless music-metadata source for the unified
// Discover page's music cells. MusicBrainz has NO popularity signal
// (that would need a Last.fm-style service — deferred), so the
// trending:discover cell is a documented proxy: release groups whose
// first release falls in the window, ordered most-recently-released
// first. MusicBrainz answers transient 503 "busy" responses under load
// (observed live), so feed requests retry a bounded number of times.

// mbFeedRetries is the number of extra attempts after a transient 503
// "busy" answer (3 total attempts for a feed request).
const mbFeedRetries = 2

// mbFeedBackoff is the retry delay base; the n-th retry waits
// n*mbFeedBackoff (500ms, then 1s).
const mbFeedBackoff = 500 * time.Millisecond

// mbFeedGet is getJSON with a bounded retry for transient 503 "busy"
// answers (MusicBrainz overload behavior). Non-503 failures return on
// the first attempt, exactly like getJSON.
func (m *MusicBrainz) mbFeedGet(ctx context.Context, path string, values url.Values, out any) error {
	var lastErr error
	for attempt := 0; attempt <= mbFeedRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt) * mbFeedBackoff):
			}
		}
		var status int
		status, lastErr = m.doFeedRequest(ctx, path, values, out)
		if lastErr == nil {
			return nil
		}
		if status != http.StatusServiceUnavailable {
			return lastErr
		}
	}
	return lastErr
}

// doFeedRequest performs one feed HTTP request. It mirrors Client.getJSON
// but also returns the upstream status so mbFeedGet can retry 503 "busy"
// answers (the shared getJSON folds the status into the error text only).
func (m *MusicBrainz) doFeedRequest(ctx context.Context, path string, values url.Values, out any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.Base+path, nil)
	if err != nil {
		return 0, err
	}
	if values != nil {
		req.URL.RawQuery = values.Encode()
	}
	req.Header.Set("Accept", "application/json")
	if m.AuthHeader != "" {
		req.Header.Set("X-Api-Key", m.AuthHeader)
	}
	if m.UserAgent != "" {
		req.Header.Set("User-Agent", m.UserAgent)
	}
	resp, err := m.client().Do(req)
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrBadResponse, err)
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return resp.StatusCode, fmt.Errorf("%w: read body: %v", ErrBadResponse, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.StatusCode, fmt.Errorf("%w: %s returned %d: %s", ErrBadResponse, m.Name(), resp.StatusCode, snippet(data, 200))
	}
	if err := json.Unmarshal(data, out); err != nil {
		return resp.StatusCode, fmt.Errorf("%w: invalid json: %v", ErrBadResponse, err)
	}
	return resp.StatusCode, nil
}

// mbReleaseGroupRows is the /release-group search payload subset.
//
// Field-name trap (live-probed): the Lucene query field is
// `firstreleasedate` (no hyphen — a hyphenated form is silently ignored
// and filters nothing), while the response payload field is
// `first-release-date` (hyphenated). The query string and the unmarshal
// target use different spellings on purpose.
type mbReleaseGroupRows struct {
	ReleaseGroups []struct {
		ID                string `json:"id"`
		Title             string `json:"title"`
		FirstReleasedDate string `json:"first-release-date"` // partial: "YYYY", "YYYY-MM", or full
		ArtistCredit      []struct {
			Name string `json:"name"`
		} `json:"artist-credit"`
	} `json:"release-groups"`
}

// TrendingReleaseGroups fetches the release groups whose first release
// falls in [since, until] (the trending:discover music cell — the
// documented proxy for "trending", since MusicBrainz has no popularity
// signal). Results are ordered most-recently-released first (the server
// answer is relevance-ordered, so the date-descending sort is applied
// here; ties keep the server order) and de-duplicated by release-group
// id. Page size is capped at 25 rows by the API.
func (m *MusicBrainz) TrendingReleaseGroups(ctx context.Context, since, until time.Time) ([]DiscoverItem, error) {
	values := url.Values{}
	values.Set("query", "firstreleasedate:["+since.Format("2006-01-02")+" TO "+until.Format("2006-01-02")+"]")
	values.Set("fmt", "json")
	values.Set("limit", "25")

	m.Base = m.baseURL()
	var resp mbReleaseGroupRows
	if err := m.mbFeedGet(ctx, "/release-group", values, &resp); err != nil {
		return nil, err
	}
	type row struct {
		item DiscoverItem
		sort time.Time
	}
	seen := make(map[string]bool)
	rows := make([]row, 0, len(resp.ReleaseGroups))
	for _, rg := range resp.ReleaseGroups {
		if rg.ID == "" || rg.Title == "" || seen[rg.ID] {
			continue
		}
		seen[rg.ID] = true
		year, fullDate := mbPartialDate(rg.FirstReleasedDate)
		items := DiscoverItem{
			Kind:     "music",
			SourceID: rg.ID,
			Title:    rg.Title,
			Year:     year,
			// MusicBrainz rows carry no art (the Cover Art Archive is
			// not consulted for feed cells) → the UI renders the
			// initials stand-in.
			PosterURL: "",
		}
		if len(rg.ArtistCredit) > 0 {
			items.Subtitle = rg.ArtistCredit[0].Name
		}
		sortKey := fullDate
		if sortKey.IsZero() {
			// Year-only (and absent) dates: sort at the START of the
			// year — a partial date must not out-rank a fully dated
			// release in the same year (that would overstate a
			// known-recent release's recency with an unknown one).
			sortKey = time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
		}
		items.Date = fullDate
		rows = append(rows, row{item: items, sort: sortKey})
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].sort.After(rows[j].sort) })
	out := make([]DiscoverItem, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.item)
	}
	return out, nil
}

// mbReleaseRows is the /release search payload subset: the top-level
// artist-credit plus the release-group reference (live-probed shape).
type mbReleaseRows struct {
	Releases []struct {
		ID           string `json:"id"`
		Title        string `json:"title"`
		Date         string `json:"date"` // partial, overwhelmingly year-only
		ArtistCredit []struct {
			Name string `json:"name"`
		} `json:"artist-credit"`
		ReleaseGroup struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"release-group"`
	} `json:"releases"`
}

// NewReleases fetches the releases whose release date falls in
// [since, until] (the new_releases:discover music cell). Release rows
// without an id or title are dropped; the server order is preserved
// (most-recently-dated first within the window's relevance).
func (m *MusicBrainz) NewReleases(ctx context.Context, since, until time.Time) ([]DiscoverItem, error) {
	values := url.Values{}
	values.Set("query", "date:["+since.Format("2006-01-02")+" TO "+until.Format("2006-01-02")+"]")
	values.Set("fmt", "json")
	values.Set("limit", "25")

	m.Base = m.baseURL()
	var resp mbReleaseRows
	if err := m.mbFeedGet(ctx, "/release", values, &resp); err != nil {
		return nil, err
	}
	out := make([]DiscoverItem, 0, len(resp.Releases))
	for _, rel := range resp.Releases {
		if rel.ID == "" || rel.Title == "" {
			continue
		}
		year, fullDate := mbPartialDate(rel.Date)
		item := DiscoverItem{
			Kind:     "music",
			SourceID: rel.ID, // the release id (the release-group id is display-only context)
			Title:    rel.Title,
			Year:     year,
			Date:     fullDate,
		}
		if len(rel.ArtistCredit) > 0 {
			item.Subtitle = rel.ArtistCredit[0].Name
		}
		out = append(out, item)
	}
	return out, nil
}

// mbPartialDate maps a MusicBrainz partial date — "YYYY-MM-DD", "YYYY-MM",
// "YYYY", or "" — to (year, fullDate): fullDate is non-zero only for a
// complete calendar date (UTC midnight); year is the leading four digits
// (0 when absent).
func mbPartialDate(s string) (int, time.Time) {
	s = strings.TrimSpace(s)
	year := 0
	if len(s) >= 4 {
		if y, err := strconv.Atoi(s[:4]); err == nil {
			year = y
		}
	}
	if len(s) == 10 {
		if d, err := time.Parse("2006-01-02", s); err == nil {
			return year, d
		}
	}
	return year, time.Time{}
}
