package indexers

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// nzbKind is the registry key for the usenet NZB adapter (#33).
const nzbKind = "nzb"

// NZBIndexer is the usenet NZB protocol adapter (#33). It is a *generic*
// usenet adapter: there is no single maintained Go NZB *search* library, so
// (per PLAN §7 "generic usenet adapter with documented gaps") it:
//
//  1. polls an index's "search/NZB" endpoint (the common API shape shared by
//     NZBGeek / NZBsuck / Usenet-Crawler-style indexes: a JSON or XML list of
//     releases, each with a downloadable .nzb link),
//  2. parses the results into the shared release model, surfacing the
//     usenet-specific fields — age, article count, parity/overflow (#33:
//     "usenet-specific fields surfaced to ranking") — into ReleaseInfo, and
//  3. on Test(), performs a single cheap liveness request.
//
// Documented gap: the *content* of a .nzb (its per-article par/segment
// structure) is not fetched here; the adapter surfaces the index's own
// article-count/parity metadata, which is what ranking needs. Fetching and
// verifying an NZB is the download client's concern (SABnzbd #37), not the
// indexer's.
type NZBIndexer struct {
	name    string
	baseURL string
	apiKey  string
	deps    AdapterDeps
	limiter *RateLimiter
	log     *slog.Logger
}

// NewNZBIndexer builds a usenet NZB adapter for the given definition.
func NewNZBIndexer(def Definition, deps AdapterDeps) *NZBIndexer {
	return &NZBIndexer{
		name:    def.Name,
		baseURL: strings.TrimRight(def.BaseURL, "/"),
		apiKey:  def.APIKey,
		deps:    deps,
		limiter: NewRateLimiter(DefaultPollRate, DefaultBurst),
		log:     slog.Default(),
	}
}

// Name implements Searcher.
func (n *NZBIndexer) Name() string { return n.name }

// Kind implements Provider.
func (n *NZBIndexer) Kind() string { return nzbKind }

// Test implements Provider. A single cheap liveness request (an "articleinfo"
// or "search" probe); 2xx means the index is reachable and authenticating.
func (n *NZBIndexer) Test(ctx context.Context) error {
	body, err := n.deps.httpGet(ctx, n.name, n.baseURL, n.query("search", 1))
	if err != nil {
		return err
	}
	defer body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(body, 1<<20))
	return nil
}

// Search implements Searcher. It polls the index for releases matching the
// query term and parses them into SearchResults with usenet fields populated.
func (n *NZBIndexer) Search(ctx context.Context, q SearchQuery) ([]SearchResult, error) {
	if err := n.limiter.Wait(ctx); err != nil {
		return nil, newError(n.name+" search", err, "rate-limit wait")
	}
	params := n.query("search", 0)
	if q.Term != "" {
		params.Set("q", q.Term)
	}
	if q.Year > 0 {
		params.Set("y", strconv.Itoa(q.Year))
	}
	body, err := n.deps.httpGet(ctx, n.name, n.baseURL, params)
	if err != nil {
		return nil, err
	}
	defer body.Close()

	data, err := io.ReadAll(io.LimitReader(body, 32<<20))
	if err != nil {
		return nil, newError(n.name+" search", err, "read response")
	}
	return parseNZB(n.name, data)
}

// query builds the base query for the NZB index API, appending the API key.
func (n *NZBIndexer) query(action string, limit int) url.Values {
	q := url.Values{}
	q.Set("t", action)
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	if n.apiKey != "" {
		q.Set("apikey", n.apiKey)
	}
	return q
}

// RegisterNZB registers the usenet NZB constructor with a registry under its
// protocol kind.
func RegisterNZB(r *Registry) {
	r.Register(nzbKind, func(def Definition) (Provider, error) {
		return NewNZBIndexer(def, r.Deps), nil
	})
}

// ---- response model ----
//
// The response is JSON (the dominant usenet-index API shape) with a "results"
// array. Each result carries the metadata needed to surface usenet fields.

type nzbResponse struct {
	Results []nzbResult `json:"results"`
}

type nzbResult struct {
	Title    string      `json:"title"`
	Guid     string      `json:"guid"`
	Link     string      `json:"link"` // .nzb download link
	Size     flexibleInt `json:"size"` // numeric or human string ("1.2 GB")
	Age      string      `json:"age"`  // e.g. "3 days" / "2 hours"
	AgeHours float64     `json:"age_hours"`
	Articles int         `json:"articles"` // total article/segment count
	Parity   bool        `json:"parity"`   // has overflow/parity articles
	Indexer  string      `json:"indexer"`
}

// flexibleInt unmarshals a JSON value that may be a number or a human size
// string into int64 bytes. It tolerates both encodings because different
// usenet indexes expose "size" differently.
type flexibleInt int64

func (f *flexibleInt) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		return nil
	}
	if v, err := strconv.ParseInt(s, 10, 64); err == nil {
		*f = flexibleInt(v)
		return nil
	}
	if v, err := parseHumanSize(s); err == nil {
		*f = flexibleInt(v)
		return nil
	}
	return nil // unparseable size degrades to 0, not an error
}

// parseNZB decodes a usenet index response into SearchResults. It tolerates a
// few size/age encodings and skips items with no usable title.
func parseNZB(indexer string, data []byte) ([]SearchResult, error) {
	var resp nzbResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, newError(indexer+" parse", ErrBadResponse, "invalid json: "+err.Error())
	}
	var out []SearchResult
	for _, r := range resp.Results {
		title := strings.TrimSpace(r.Title)
		if title == "" {
			continue
		}
		link := strings.TrimSpace(r.Link)
		info := ReleaseInfo{
			URL:      link,
			Protocol: ProtocolNZB,
			AgeHours: r.AgeHours,
			Articles: r.Articles,
			Parity:   r.Parity,
		}
		if info.AgeHours == 0 && r.Age != "" {
			info.AgeHours = parseNZBAge(r.Age)
		}
		out = append(out, SearchResult{
			Title:     title,
			SizeBytes: nzbSize(r),
			Indexer:   indexer,
			Info:      info,
		})
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// nzbSize returns the release size in bytes. The flexibleInt field already
// resolves both the numeric and human-string encodings, so this is a direct
// conversion.
func nzbSize(r nzbResult) int64 {
	return int64(r.Size)
}

// parseNZBAge converts a human age string ("3 days", "2 hours", "45 min") into
// hours. Returns 0 when it cannot parse.
func parseNZBAge(s string) float64 {
	l := strings.ToLower(strings.TrimSpace(s))
	words := strings.Fields(l)
	for i, w := range words {
		switch w {
		case "days", "day":
			if n, err := strconv.Atoi(num(words, i)); err == nil {
				return float64(n) * 24
			}
		case "hours", "hour":
			if n, err := strconv.Atoi(num(words, i)); err == nil {
				return float64(n)
			}
		case "min", "mins", "minutes", "minute":
			if n, err := strconv.Atoi(num(words, i)); err == nil {
				return float64(n) / 60.0
			}
		}
	}
	if m := ageRe.FindStringSubmatch(l); m != nil {
		if n, err := strconv.ParseFloat(m[1], 64); err == nil {
			return n
		}
	}
	return 0
}

func num(words []string, i int) string {
	if i == 0 {
		return ""
	}
	return words[i-1]
}

var ageRe = regexp.MustCompile(`(\d+(?:\.\d+)?)\s*h`)

// parseHumanSize converts a human size string ("1.2 GB", "500 MB") to bytes.
func parseHumanSize(s string) (int64, error) {
	l := strings.ToLower(strings.TrimSpace(s))
	var mult int64
	switch {
	case strings.HasSuffix(l, "gb") || strings.HasSuffix(l, "gib"):
		mult = 1024 * 1024 * 1024
		l = strings.TrimSuffix(l, "gib")
		l = strings.TrimSuffix(l, "gb")
	case strings.HasSuffix(l, "mb") || strings.HasSuffix(l, "mib"):
		mult = 1024 * 1024
		l = strings.TrimSuffix(l, "mib")
		l = strings.TrimSuffix(l, "mb")
	case strings.HasSuffix(l, "kb") || strings.HasSuffix(l, "kib"):
		mult = 1024
		l = strings.TrimSuffix(l, "kib")
		l = strings.TrimSuffix(l, "kb")
	default:
		mult = 1
	}
	l = strings.TrimSpace(l)
	n, err := strconv.ParseFloat(l, 64)
	if err != nil {
		return 0, err
	}
	return int64(n * float64(mult)), nil
}
