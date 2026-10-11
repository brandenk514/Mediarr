package indexers

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"io"
	"log/slog"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// torrentRSSKind is the registry key for the TorrentRSS adapter (#32).
const torrentRSSKind = "torrent-rss"

// TorrentRSSIndexer is the TorrentRSS protocol adapter (#32). Unlike Torznab,
// a TorrentRSS feed is an RSS/Atom feed of *currently available* torrents that
// you poll; there is no per-request search. The adapter's Search therefore:
//
//  1. waits on its rate limiter (PLAN §5: "rate-limited polling" — #32),
//  2. polls the feed,
//  3. parses the items,
//  4. filters to items that (a) match the query term and (b) pass the
//     configured category + seed thresholds,
//  5. skips (and logs) malformed items rather than failing the search.
//
// Because the feed is a snapshot of what the index currently has, a "no match"
// is the common case and simply returns no results.
type TorrentRSSIndexer struct {
	name    string
	baseURL string
	apiKey  string
	deps    AdapterDeps
	limiter *RateLimiter
	setting rssSettings
	log     *slog.Logger
}

// rssSettings is the optional per-indexer configuration parsed from
// Definition.SettingsJSON (PLAN §4). All fields are optional; zero values mean
// "accept everything".
type rssSettings struct {
	// Categories is the set of accepted torrent categories (e.g.
	// ["2000","5000"] for movies+TV on a Nyaa-style feed). Empty accepts all.
	Categories []string `json:"categories,omitempty"`
	// MinSeeders is the minimum seeder count to accept an item. 0 (default)
	// imposes no threshold.
	MinSeeders int `json:"min_seeders,omitempty"`
}

func parseRSSSettings(raw string) rssSettings {
	var s rssSettings
	if strings.TrimSpace(raw) == "" {
		return s
	}
	_ = json.Unmarshal([]byte(raw), &s) // tolerant: a bad config degrades to "accept all"
	return s
}

// NewTorrentRSSIndexer builds a TorrentRSS adapter for the given definition.
func NewTorrentRSSIndexer(def Definition, deps AdapterDeps) *TorrentRSSIndexer {
	return &TorrentRSSIndexer{
		name:    def.Name,
		baseURL: strings.TrimRight(def.BaseURL, "/"),
		apiKey:  def.APIKey,
		deps:    deps,
		limiter: NewRateLimiter(DefaultPollRate, DefaultBurst),
		setting: parseRSSSettings(def.SettingsJSON),
		log:     slog.Default(),
	}
}

// Name implements Searcher.
func (t *TorrentRSSIndexer) Name() string { return t.name }

// Kind implements Provider.
func (t *TorrentRSSIndexer) Kind() string { return torrentRSSKind }

// Test implements Provider. A feed that returns 2xx and parses as an RSS feed
// is healthy. A single cheap poll.
func (t *TorrentRSSIndexer) Test(ctx context.Context) error {
	body, err := t.deps.httpGet(ctx, t.name, t.baseURL, t.authQuery())
	if err != nil {
		return err
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, 32<<20))
	if err != nil {
		return newError(t.name+" test", err, "read feed")
	}
	var feed rssFeed
	if err := xml.Unmarshal(data, &feed); err != nil {
		return newError(t.name+" test", ErrBadResponse, "feed is not valid RSS")
	}
	return nil
}

// Search implements Searcher. It polls the feed (rate-limited) and returns the
// items that match the query term and pass the configured filters. Malformed
// items are logged and skipped, never fatal (#32).
func (t *TorrentRSSIndexer) Search(ctx context.Context, q SearchQuery) ([]SearchResult, error) {
	if err := t.limiter.Wait(ctx); err != nil {
		return nil, newError(t.name+" search", err, "rate-limit wait")
	}
	body, err := t.deps.httpGet(ctx, t.name, t.baseURL, t.authQuery())
	if err != nil {
		return nil, err
	}
	defer body.Close()

	data, err := io.ReadAll(io.LimitReader(body, 32<<20))
	if err != nil {
		return nil, newError(t.name+" search", err, "read feed")
	}
	var feed rssFeed
	if err := xml.Unmarshal(data, &feed); err != nil {
		// A corrupt feed is a bad-response, not a fatal search failure — the
		// other indexers in the fan-out still contribute.
		t.log.Warn("torrent-rss feed parse failed", "indexer", t.name, "error", err)
		return nil, newError(t.name+" search", ErrBadResponse, "invalid feed xml")
	}

	var out []SearchResult
	for _, it := range feed.Channel.Items {
		res, ok, malformed := t.itemToResult(q, it)
		if malformed {
			t.log.Warn("torrent-rss skipping malformed item",
				"indexer", t.name, "title", strings.TrimSpace(it.Title))
			continue // #32: skip + log, not fatal
		}
		if !ok {
			continue // filtered out (category/seed/no-term-match)
		}
		out = append(out, res)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// authQuery builds the feed query, appending the API key as a query param when
// configured (many TorrentRSS indexes use a ?key= form).
func (t *TorrentRSSIndexer) authQuery() url.Values {
	q := url.Values{}
	if t.apiKey != "" {
		q.Set("key", t.apiKey)
	}
	return q
}

// itemToResult maps one feed item to a SearchResult. It returns:
//   - ok: the item passed all filters and should be included;
//   - malformed: the item is structurally unusable (no title and no link) and
//     should be logged+skipped.
//
// Filtering: term match (every len>=3 token of the term in the title), then
// category (if configured), then minimum seeders (if configured).
func (t *TorrentRSSIndexer) itemToResult(q SearchQuery, it rssItem) (SearchResult, bool, bool) {
	title := strings.TrimSpace(it.Title)
	link := strings.TrimSpace(it.Enclosure.URL)
	if link == "" {
		link = strings.TrimSpace(it.Link)
	}
	if title == "" && link == "" {
		return SearchResult{}, false, true // malformed
	}
	if title == "" {
		return SearchResult{}, false, false // no title to match on; skip silently
	}

	// Term match: a TorrentRSS "search" is client-side filtering of the feed.
	// An empty term means "return everything that passes the filters".
	if q.Term != "" && !termMatches(title, q.Term) {
		return SearchResult{}, false, false
	}
	if !t.categoryOK(it.Category) {
		return SearchResult{}, false, false
	}
	if t.setting.MinSeeders > 0 && it.Seeders < t.setting.MinSeeders {
		return SearchResult{}, false, false
	}

	info := ReleaseInfo{URL: link, Seeders: it.Seeders}
	if info.Seeders == 0 {
		info.Seeders = parseSeeders(it.Description, it.Comments)
	}
	if link != "" {
		info.Protocol = protocolForURL(link)
	}
	return SearchResult{
		Title:     title,
		SizeBytes: it.Size,
		Indexer:   t.name,
		Info:      info,
	}, true, false
}

// categoryOK reports whether the item's category is accepted. An empty
// configured set or an item with no category both pass (accept-all).
func (t *TorrentRSSIndexer) categoryOK(cat string) bool {
	if len(t.setting.Categories) == 0 {
		return true
	}
	if cat == "" {
		return false // a category is required but the item has none
	}
	for _, c := range t.setting.Categories {
		if strings.EqualFold(c, cat) {
			return true
		}
	}
	return false
}

// RegisterTorrentRSS registers the TorrentRSS constructor with a registry
// under its protocol kind.
func RegisterTorrentRSS(r *Registry) {
	r.Register(torrentRSSKind, func(def Definition) (Provider, error) {
		return NewTorrentRSSIndexer(def, r.Deps), nil
	})
}

// ---- RSS model ----

type rssFeed struct {
	XMLName xml.Name   `xml:"rss"`
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Items []rssItem `xml:"item"`
}

type rssItem struct {
	Title       string  `xml:"title"`
	Guid        string  `xml:"guid"`
	Link        string  `xml:"link"`
	Description string  `xml:"description"`
	Comments    string  `xml:"comments"`
	Size        int64   `xml:"size"`
	Category    string  `xml:"category"`
	Seeders     int     `xml:"seeders"`
	Enclosure   rssEncl `xml:"enclosure"`
}

type rssEncl struct {
	URL    string `xml:"url,attr"`
	Length string `xml:"length,attr"`
}

// parseSeeders extracts a seeder count from the free-text fields of a feed
// item when there is no explicit <seeders> element. It recognises the common
// "seeders: N" / "seeds N" / "s: N / l: M" patterns. Returns 0 when absent.
func parseSeeders(description, comments string) int {
	text := strings.ToLower(description + " " + comments)
	if m := seedRe.FindStringSubmatch(text); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil {
			return n
		}
	}
	return 0
}

var seedRe = regexp.MustCompile(`seede?rs?\s*[:=]?\s*(\d+)`)
