package providers

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// defaultOpenLibraryBase is the Open Library search API.
const defaultOpenLibraryBase = "https://openlibrary.org"

// OpenLibrary searches books against the Open Library
// (https://openlibrary.org/developers/api). It is keyless. Two entry points:
//   - SearchAuthors: resolves an author name to candidate authors, so the
//     add flow can add the author first (the books model is hierarchical).
//   - SearchWorks: finds works (titles) for an author + title query. A
//     selected work is what the UI turns into (author, title) — the books
//     model keys a title by (author_id, name); there is no provider id to
//     store, the work key is display-only.
type OpenLibrary struct {
	Client
	// Base defaults to defaultOpenLibraryBase when empty (tests point it at
	// a stub).
	BaseURL string
}

// NewOpenLibrary builds the adapter.
func NewOpenLibrary() *OpenLibrary {
	return &OpenLibrary{Client: Client{Name: "openlibrary"}}
}

func (o *OpenLibrary) baseURL() string {
	if o.BaseURL != "" {
		return o.BaseURL
	}
	return defaultOpenLibraryBase
}

// Name implements Searcher.
func (o *OpenLibrary) Name() string { return "openlibrary" }

// olSearchResponse is the subset of /search.json we consume.
type olSearchResponse struct {
	NumFound int `json:"numFound"`
	Docs     []struct {
		Key              string   `json:"key"` // work key, e.g. "/works/OL8189755W"
		Title            string   `json:"title"`
		AuthorName       []string `json:"author_name"`
		FirstPublishYear int      `json:"first_publish_year"`
		CoverI           int      `json:"cover_i"` // cover id; 0 = no cover
	} `json:"docs"`
}

func (o *OpenLibrary) search(ctx context.Context, values url.Values) (*olSearchResponse, error) {
	o.Base = o.baseURL()
	var resp olSearchResponse
	if err := o.getJSON(ctx, "/search.json", values, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// olCoverURL is the Open Library cover image for a search result's cover id
// (the documented covers.openlibrary.org scheme); empty when the doc has no
// cover. It feeds the MT3 provenance store's cover_url.
func olCoverURL(coverI int) string {
	if coverI <= 0 {
		return ""
	}
	return fmt.Sprintf("https://covers.openlibrary.org/b/id/%d-M.jpg", coverI)
}

// SearchAuthors resolves an author name to distinct candidate authors (name
// only — Open Library has no stable author id in search results).
func (o *OpenLibrary) SearchAuthors(ctx context.Context, q string) ([]Result, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, nil
	}
	values := url.Values{}
	values.Set("author", q)
	values.Set("limit", "25")
	resp, err := o.search(ctx, values)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	out := []Result{}
	for _, d := range resp.Docs {
		for _, name := range d.AuthorName {
			name = strings.TrimSpace(name)
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, Result{
				ID:    name, // no provider id exists; the name is the identity
				Title: name,
			})
			if len(out) >= 25 {
				return out, nil
			}
		}
	}
	return out, nil
}

// SearchWorks finds works matching title for the given author (either may be
// empty; both set narrows to that author's works).
func (o *OpenLibrary) SearchWorks(ctx context.Context, author, title string) ([]Result, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, nil
	}
	values := url.Values{}
	values.Set("title", title)
	if a := strings.TrimSpace(author); a != "" {
		values.Set("author", a)
	}
	values.Set("limit", "25")
	resp, err := o.search(ctx, values)
	if err != nil {
		return nil, err
	}
	out := make([]Result, 0, len(resp.Docs))
	for _, d := range resp.Docs {
		if d.Title == "" {
			continue
		}
		author := ""
		if len(d.AuthorName) > 0 {
			author = d.AuthorName[0]
		}
		extra := map[string]string{"author": author}
		if c := olCoverURL(d.CoverI); c != "" {
			extra["cover"] = c
		}
		out = append(out, Result{
			ID:    strings.TrimPrefix(d.Key, "/works/"),
			Title: d.Title,
			Year:  d.FirstPublishYear,
			Extra: extra,
		})
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Readarr B6 (MT1 minimal) / MT1: id-based lookups. The drop-in migration
// (B6) resolves a source book to its OpenLibrary work key by a stable id —
// the "metadata-source swap" of the migration; MT1 extends the same lookups
// to ASINs and routes them in the user-facing lookup.
//
// Filter choice (verified against the keyless production API): search.json
// only accepts the declared solr query via `q` — a field-prefixed query
// (`isbn:…`, `key:…`) or a plain full-text term. The legacy `id=` parameter
// is not part of the public API and is silently ignored (0 docs), so every
// id lookup below goes through `q`.
// ---------------------------------------------------------------------------

// NormalizeOLID folds a work key ("/works/OL8189755W", "OL8189755W", or a
// bare key path) to the bare OpenLibrary id ("OL8189755W").
func NormalizeOLID(key string) string {
	k := strings.TrimSpace(key)
	if i := strings.LastIndex(k, "/"); i >= 0 {
		k = k[i+1:]
	}
	return k // the bare OL id, e.g. "OL8189755W"
}

// olWorkIDRe is the bare OpenLibrary work-id shape ("OL12345W").
var olWorkIDRe = regexp.MustCompile(`^OL[0-9]+W$`)

// IsOLWorkID reports whether s is a bare OpenLibrary work id (NormalizeOLID
// folds the key forms to it).
func IsOLWorkID(s string) bool { return olWorkIDRe.MatchString(strings.TrimSpace(s)) }

// normalizeISBN drops the hyphens and spaces an ISBN may carry (Open Library
// normalizes the same way before indexing its isbn field).
func normalizeISBN(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r != '-' && r != ' ' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// SearchWorkByISBN resolves an ISBN (13 or 10 digit) to its OpenLibrary work
// (a single Result carrying the work key as ID) via the search.json `isbn:`
// field query. The ISBN is normalized first (hyphens/spaces stripped). Empty
// slice when no work is indexed for the ISBN.
func (o *OpenLibrary) SearchWorkByISBN(ctx context.Context, isbn string) ([]Result, error) {
	isbn = normalizeISBN(strings.TrimSpace(isbn))
	if isbn == "" {
		return nil, nil
	}
	values := url.Values{}
	values.Set("q", "isbn:"+isbn)
	values.Set("limit", "5")
	resp, err := o.search(ctx, values)
	if err != nil {
		return nil, err
	}
	return olWorkResults(resp), nil
}

// SearchWorkByASIN resolves an Amazon ASIN to its OpenLibrary work via a
// full-text `q=<ASIN>` search: Open Library indexes each edition's amazon
// identifier (aggregated onto the work), and the ASIN token matches it.
// There is no declared `amazon_id` filter on search.json (the `id=amazon_id:`
// form is silently ignored); the full-text term is the reliable keyless path
// (verified against production). Empty slice when no work carries the ASIN.
func (o *OpenLibrary) SearchWorkByASIN(ctx context.Context, asin string) ([]Result, error) {
	asin = strings.TrimSpace(asin)
	if asin == "" {
		return nil, nil
	}
	values := url.Values{}
	values.Set("q", asin)
	values.Set("limit", "5")
	resp, err := o.search(ctx, values)
	if err != nil {
		return nil, err
	}
	return olWorkResults(resp), nil
}

// SearchWorkByKey resolves a known OpenLibrary work key (or bare OL id) to its
// work via the search.json `key:` field query. This is the "already an OL key"
// fast path of the migration's id swap (method "source") and the MT3 cached
// lookup's re-fetch.
func (o *OpenLibrary) SearchWorkByKey(ctx context.Context, key string) ([]Result, error) {
	k := NormalizeOLID(key)
	if k == "" {
		return nil, nil
	}
	values := url.Values{}
	values.Set("q", "key:"+k)
	values.Set("limit", "5")
	resp, err := o.search(ctx, values)
	if err != nil {
		return nil, err
	}
	return olWorkResults(resp), nil
}

// olWorkResults maps a search.json response to book Results keyed by work id
// (the migration's id-swap consumes the ID field as the OpenLibrary work key).
func olWorkResults(resp *olSearchResponse) []Result {
	out := make([]Result, 0, len(resp.Docs))
	for _, d := range resp.Docs {
		if d.Key == "" {
			continue
		}
		author := ""
		if len(d.AuthorName) > 0 {
			author = d.AuthorName[0]
		}
		extra := map[string]string{"author": author}
		if c := olCoverURL(d.CoverI); c != "" {
			extra["cover"] = c
		}
		out = append(out, Result{
			ID:    strings.TrimPrefix(d.Key, "/works/"),
			Title: d.Title,
			Year:  d.FirstPublishYear,
			Extra: extra,
		})
	}
	return out
}
