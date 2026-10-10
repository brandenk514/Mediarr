package indexers

import (
	"context"
	"encoding/xml"
	"io"
	"net/url"
	"strconv"
	"strings"
)

// torznabKind is the registry key for the Torznab adapter (#31).
const torznabKind = "torznab"

// TorznabIndexer is the Torznab protocol adapter (#31). Torznab is a
// Torrentrss-style API: a single base URL answers XML "search" queries,
// authenticated by an API key. It covers both torrent and usenet backends
// (the link is a magnet, a .torrent, or an NZB), which is why the shared
// release model carries a protocol-specific Info rather than a fixed field.
//
// It implements Provider: Search (title/season/episode), Test (a single
// capability request as a cheap liveness/auth probe), and the lifecycle
// surface the framework needs. It never does HTTP in the domain layer — it
// sits behind the Searcher/Provider seam (PLAN §3/§7).
type TorznabIndexer struct {
	name    string
	baseURL string
	apiKey  string
	deps    AdapterDeps
	limiter *RateLimiter
}

// NewTorznabIndexer builds a Torznab adapter for the given definition. The API
// key is the *decrypted* key handed by the repository (PLAN §7). A nil deps
// uses built-in defaults (see AdapterDeps).
func NewTorznabIndexer(def Definition, deps AdapterDeps) *TorznabIndexer {
	// A Torznab key is optional: some indexes authenticate via a path or token
	// embedded in the base URL rather than a separate apikey parameter. An empty
	// key simply means the adapter omits the apikey query parameter (see
	// Test/Search below), so an empty def.APIKey is legal, not an error.
	return &TorznabIndexer{
		name:    def.Name,
		baseURL: strings.TrimRight(def.BaseURL, "/"),
		apiKey:  def.APIKey,
		deps:    deps,
		limiter: NewRateLimiter(DefaultPollRate, DefaultBurst),
	}
}

// Name implements Searcher.
func (t *TorznabIndexer) Name() string { return t.name }

// Kind implements Provider.
func (t *TorznabIndexer) Kind() string { return torznabKind }

// Test implements Provider. It performs a single, cheap capability request
// (t=caps): the canonical Torznab "is this indexer reachable and
// authenticating" check. It returns the typed error on failure (an 401/403 is
// ErrUnauthorized, so a bad key is distinguishable from a network fault).
func (t *TorznabIndexer) Test(ctx context.Context) error {
	q := queryParam("t", "caps")
	if t.apiKey != "" {
		q.Set("apikey", t.apiKey)
	}
	return t.deps.httpStatus(ctx, t.name, t.baseURL, q)
}

// Search implements Searcher. It maps a SearchQuery onto the Torznab "search"
// API (t=search for a bare title, with imdbid/season/ep when the query carries
// them) and parses the XML response into SearchResults.
func (t *TorznabIndexer) Search(ctx context.Context, q SearchQuery) ([]SearchResult, error) {
	params := url.Values{}
	params.Set("t", "search")
	if q.Term != "" {
		params.Set("q", q.Term)
	}
	if q.Year > 0 {
		params.Set("year", strconv.Itoa(q.Year))
	}
	if q.Season > 0 {
		params.Set("season", strconv.Itoa(q.Season))
	}
	if q.Episode > 0 {
		params.Set("ep", strconv.Itoa(q.Episode))
	}
	if t.apiKey != "" {
		params.Set("apikey", t.apiKey)
	}

	body, err := t.deps.httpGet(ctx, t.name, t.baseURL, params)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	return parseTorznab(t.name, body)
}

// ---- XML model (Torznab RSS) ----

type torznabFeed struct {
	Channel torznabChannel `xml:"channel"`
}

type torznabChannel struct {
	Items []torznabItem `xml:"item"`
}

type torznabItem struct {
	Title       string      `xml:"title"`
	Guid        string      `xml:"guid"`
	Link        string      `xml:"link"`
	Description string      `xml:"description"`
	Size        string      `xml:"size"`
	Enclosure   torznabEncl `xml:"enclosure"`
}

type torznabEncl struct {
	URL    string `xml:"url,attr"`
	Length string `xml:"length,attr"`
}

// parseTorznab decodes a Torznab RSS feed into SearchResults. A response with
// no items is a legal "no matches" (nil, nil). Malformed XML is a
// non-fatal ErrBadResponse so a single corrupt response cannot take down a
// search fan-out (the other indexers still contribute).
func parseTorznab(indexer string, r io.Reader) ([]SearchResult, error) {
	data, err := io.ReadAll(io.LimitReader(r, 32<<20)) // 32 MiB ceiling
	if err != nil {
		return nil, newError(indexer+" parse", err, "read feed")
	}
	var feed torznabFeed
	if err := xml.Unmarshal(data, &feed); err != nil {
		return nil, newError(indexer+" parse", ErrBadResponse, "invalid xml: "+err.Error())
	}
	var out []SearchResult
	for _, it := range feed.Channel.Items {
		res, ok := torznabItemToResult(indexer, it)
		if !ok {
			continue // skip items with no usable title or link
		}
		out = append(out, res)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// torznabItemToResult maps one <item> to a SearchResult, deriving the
// download URL (enclosure preferred, then <link>) and its protocol. It
// returns ok=false for items that have neither a title nor a usable link.
func torznabItemToResult(indexer string, it torznabItem) (SearchResult, bool) {
	title := strings.TrimSpace(it.Title)
	if title == "" {
		return SearchResult{}, false
	}
	link := strings.TrimSpace(it.Enclosure.URL)
	if link == "" {
		link = strings.TrimSpace(it.Link)
	}
	info := ReleaseInfo{
		URL: link,
	}
	if link != "" {
		info.Protocol = protocolForURL(link)
	}
	size, _ := parseTorznabSize(it)
	return SearchResult{
		Title:     title,
		SizeBytes: size,
		Indexer:   indexer,
		Info:      info,
	}, true
}

// parseTorznabSize prefers the <size> element, falling back to the enclosure
// length attribute.
func parseTorznabSize(it torznabItem) (int64, bool) {
	if s := strings.TrimSpace(it.Size); s != "" {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return n, true
		}
	}
	if s := strings.TrimSpace(it.Enclosure.Length); s != "" {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return n, true
		}
	}
	return 0, false
}

// RegisterTorznab registers the Torznab constructor with a registry under its
// protocol kind. Adapters self-register so the running build can materialize a
// configured "torznab" definition (PLAN §7, #31).
func RegisterTorznab(r *Registry) {
	r.Register(torznabKind, func(def Definition) (Provider, error) {
		return NewTorznabIndexer(def, r.Deps), nil
	})
}
