package providers

import (
	"context"
	"net/url"
	"strconv"
	"strings"
)

// defaultGoogleBooksBase is the Google Books v1 REST API.
const defaultGoogleBooksBase = "https://www.googleapis.com/books/v1"

// GoogleBooks is the MT5 secondary book-metadata source (the plan: an
// optional Google Books leg for coverage gaps — "especially useful for
// audiobooks", so the mapping keeps printType and deliberately does NOT
// filter by it). It is NEVER the primary source: Open Library stays the
// source of truth, and the books service runs this leg only for
// plain-text queries that OL missed (see services/books).
//
// A non-empty API key is the enablement flag (config:
// MEDIARR_GOOGLE_BOOKS_API_KEY): no separate "enabled" flag. The key is
// sent as the `key` query parameter (Google convention, unlike TMDB's
// X-API-KEY header).
type GoogleBooks struct {
	Client
	// BaseURL, when set, overrides defaultGoogleBooksBase (tests point it
	// at a stub).
	BaseURL string
	// APIKey is the Google Books API key (the `key` parameter). Empty =
	// not configured: Configured() is false and SearchBooks returns
	// ErrNotConfigured — the direct-use 503 mapping, the TMDB pattern.
	// The books service checks Configured() first and treats a missing
	// key as "leg disabled" (skip, no error, no 503 on the lookup).
	APIKey string
}

// NewGoogleBooks builds the adapter. apiKey may be empty (see APIKey).
func NewGoogleBooks(apiKey string) *GoogleBooks {
	return &GoogleBooks{Client: Client{Name: "googlebooks"}, APIKey: apiKey}
}

// Configured reports whether a non-empty API key was supplied (the MT5
// enablement flag).
func (g *GoogleBooks) Configured() bool { return g.APIKey != "" }

// Name identifies the provider in errors and logs.
func (g *GoogleBooks) Name() string { return "googlebooks" }

func (g *GoogleBooks) baseURL() string {
	if g.BaseURL != "" {
		return g.BaseURL
	}
	return defaultGoogleBooksBase
}

// gbIdentifier is one volumeInfo.industryIdentifiers entry (type is
// "ISBN_13", "ISBN_10", "ISBN", ...).
type gbIdentifier struct {
	Type       string `json:"type"`
	Identifier string `json:"identifier"`
}

// gbSaleInfo is the volumeInfo-adjacent saleInfo block (the API's
// availability + pricing). Only the availability code is mapped
// (display-only).
type gbSaleInfo struct {
	Availability int `json:"availability"`
}

// gbVolumeInfo is the per-volume metadata of the volumes API.
type gbVolumeInfo struct {
	Title               string         `json:"title"`
	Subtitle            string         `json:"subtitle"`
	Authors             []string       `json:"authors"`
	Publisher           string         `json:"publisher"`
	PublishedDate       string         `json:"publishedDate"` // "2020", "1942-06-01", or ""
	Language            string         `json:"language"`      // ISO-639-1-style code (e.g. "en")
	PageCount           int            `json:"pageCount"`
	PrintType           string         `json:"printType"` // BOOK, AUDIOBOOK, MAGAZINE, ...
	IndustryIdentifiers []gbIdentifier `json:"industryIdentifiers"`
}

// gbVolume is one items[] row of the volumes API. (saleInfo sits at the
// item level in the real API, not inside volumeInfo.)
type gbVolume struct {
	ID         string       `json:"id"` // the Google volume id (e.g. "AbCdefGhIjK")
	VolumeInfo gbVolumeInfo `json:"volumeInfo"`
	SaleInfo   gbSaleInfo   `json:"saleInfo"`
}

// gbVolumesResponse is the GET /volumes payload.
type gbVolumesResponse struct {
	Kind       string     `json:"kind"`
	TotalItems int        `json:"totalItems"`
	Items      []gbVolume `json:"items"`
}

// SearchBooks searches the Google Books volumes API. q is the caller's raw
// query string; the id-lookup syntax is mapped here (the MT1 scheme):
// `isbn:NNN` and a bare 13-digit ISBN become the volumes API's
// `q=isbn:NNN` form, and `asin:` (or a bare ASIN) returns an empty result
// with no HTTP call — Google Books has no ASIN identifier, and the
// service's asin: leg is Open Library's alone. Everything else is free
// text (title/author) passed through as `q`.
//
// Not-configured (no API key) returns ErrNotConfigured — the direct-use
// 503 mapping (the TMDB pattern); the books service checks Configured()
// first and skips the leg instead.
func (g *GoogleBooks) SearchBooks(ctx context.Context, q string) ([]Result, error) {
	if !g.Configured() {
		return nil, ErrNotConfigured
	}
	q = strings.TrimSpace(q)
	if q == "" {
		return []Result{}, nil
	}

	// Id-lookup syntax mapping (documented above). A bare 10-alnum
	// string starting with B/b is the MT1 bare-ASIN syntax — in this
	// app such a query is an asin: id-lookup (the service routes it to
	// Open Library and never to this leg); the standalone mapping
	// keeps the same semantics.
	switch {
	case strings.HasPrefix(q, "asin:") || isBareASIN(q):
		// Google Books has no ASIN identifier: the asin: leg is Open
		// Library's alone. No call, no results.
		return []Result{}, nil
	case strings.HasPrefix(q, "isbn:"):
		q = "isbn:" + strings.TrimPrefix(q, "isbn:")
	case isBareISBN13(q):
		q = "isbn:" + q
	}

	values := url.Values{}
	values.Set("q", q)
	values.Set("key", g.APIKey)

	var resp gbVolumesResponse
	g.Base = g.baseURL()
	if err := g.getJSON(ctx, "/volumes", values, &resp); err != nil {
		return nil, err
	}
	out := make([]Result, 0, len(resp.Items))
	for _, v := range resp.Items {
		out = append(out, g.mapVolume(v))
	}
	return out, nil
}

// isBareASIN recognizes the MT1 bare-ASIN form ("B" or "b" + 9
// alphanumerics, the domain's idBareASINRe) so the asin: mapping above
// covers both the prefixed and bare syntaxes.
func isBareASIN(q string) bool {
	if len(q) != 10 || (q[0] != 'B' && q[0] != 'b') {
		return false
	}
	for _, c := range q[1:] {
		if (c < '0' || c > '9') && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') {
			return false
		}
	}
	return true
}

// isBareISBN13 recognizes the MT1 bare-ISBN form (13 digits).
func isBareISBN13(q string) bool {
	if len(q) != 13 {
		return false
	}
	for _, c := range q {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// mapVolume maps one volumes item onto the provider Result: the volume id
// becomes the hit's ID (the source tag "googlebooks" is applied by the
// service's wrapper), the volumeInfo carries the display fields, and
// industryIdentifiers yield the ISBN (ISBN_13 preferred over ISBN_10).
// printType is kept (display-only; the plan's audiobook note) and no
// printType is filtered out.
func (g *GoogleBooks) mapVolume(v gbVolume) Result {
	vi := v.VolumeInfo
	r := Result{
		ID:    v.ID,
		Title: vi.Title,
		Year:  gbYear(vi.PublishedDate),
		Extra: map[string]string{},
	}
	if len(vi.Authors) > 0 {
		r.Extra["author"] = vi.Authors[0]
	}
	if vi.Language != "" {
		r.Extra["languages"] = vi.Language
	}
	if vi.Publisher != "" {
		r.Extra["publisher"] = vi.Publisher
	}
	if vi.PrintType != "" {
		r.Extra["print_type"] = vi.PrintType
	}
	if vi.PageCount > 0 {
		r.Extra["page_count"] = strconv.Itoa(vi.PageCount)
	}
	for _, id := range vi.IndustryIdentifiers {
		switch id.Type {
		case "ISBN_13":
			if r.Extra["isbn"] == "" {
				r.Extra["isbn"] = id.Identifier
			}
		case "ISBN_10":
			if r.Extra["isbn"] == "" && r.Extra["isbn10"] == "" {
				r.Extra["isbn10"] = id.Identifier
			}
		}
	}
	if r.Extra["isbn"] == "" && r.Extra["isbn10"] != "" {
		r.Extra["isbn"] = r.Extra["isbn10"] // 10-digit-only volume: still an ISBN
	}
	r.Extra["sale_info"] = strconv.Itoa(v.SaleInfo.Availability)
	return r
}

// gbYear derives the release year from a publishedDate (a full date, a
// partial one like "1942-06", or a bare year; "" = 0 = unknown).
func gbYear(publishedDate string) int {
	for i := 0; i+4 <= len(publishedDate); i++ {
		c := publishedDate[i]
		if c < '0' || c > '9' {
			continue
		}
		ok := true
		for j := 0; j < 4; j++ {
			if publishedDate[i+j] < '0' || publishedDate[i+j] > '9' {
				ok = false
				break
			}
		}
		if ok {
			n, _ := strconv.Atoi(publishedDate[i : i+4])
			return n
		}
	}
	return 0
}
