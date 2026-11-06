package providers

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
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
		// MT4 tier-1 facts: the search document already carries the
		// language list and the indexed-edition count, so the profile
		// filters (allowed languages, missing release date, the
		// popularity proxy) evaluate without an extra provider call.
		Language     []string `json:"language"` // ISO codes, e.g. ["eng"]
		EditionCount int      `json:"edition_count"`
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

// olSearchExtras builds the display/MT4 extras shared by the two work-level
// result mappers: the first author, the cover URL (when the doc has one),
// and the MT4 tier-1 facts (the doc's language list and indexed-edition
// count — the popularity proxy; absent fields map to ""/0 = unknown).
func olSearchExtras(authorName []string, coverI int, language []string, editionCount int) map[string]string {
	author := ""
	if len(authorName) > 0 {
		author = authorName[0]
	}
	extra := map[string]string{"author": author}
	if c := olCoverURL(coverI); c != "" {
		extra["cover"] = c
	}
	if len(language) > 0 {
		extra["languages"] = strings.Join(language, ",")
	}
	if editionCount > 0 {
		extra["edition_count"] = strconv.Itoa(editionCount)
	}
	return extra
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
		out = append(out, Result{
			ID:    strings.TrimPrefix(d.Key, "/works/"),
			Title: d.Title,
			Year:  d.FirstPublishYear,
			Extra: olSearchExtras(d.AuthorName, d.CoverI, d.Language, d.EditionCount),
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
		out = append(out, Result{
			ID:    strings.TrimPrefix(d.Key, "/works/"),
			Title: d.Title,
			Year:  d.FirstPublishYear,
			Extra: olSearchExtras(d.AuthorName, d.CoverI, d.Language, d.EditionCount),
		})
	}
	return out
}

// ---------------------------------------------------------------------------
// MT2 (automatic edition resolution): the work-details fetch.
// ---------------------------------------------------------------------------

// MaxWorkEditions caps the editions mapped from a work document: the editions
// array of a popular work can run to thousands of entries, and the add flow
// only offers a workable subset (the UI lists them for selection).
const MaxWorkEditions = 50

// WorkEdition is one OpenLibrary edition of a work as fetched by the MT2
// work-details call. The service maps it onto the domain's WorkEdition
// (provider layer never imports domains — the repo's adapter convention).
type WorkEdition struct {
	Key         string      // bare edition id, e.g. "OL47044678M"
	Title       string      // edition title; "" when absent
	ISBN        string      // preferred ISBN (first ISBN-13, else ISBN-10); "" when none
	ASIN        string      // Amazon identifier; "" when absent (MT4: counts as an identifier)
	Formats     []string    // file formats, e.g. ["EPUB", "PDF"]
	Languages   []string    // ISO 639-1 codes, e.g. ["eng"]
	Publishers  []string    // publisher names
	PublishDate string      // raw OL publish-date string ("2017-06", "2017", ...)
	Year        int         // leading year of PublishDate; 0 when unknown
	Pages       int         // page count; 0 when unknown
	Series      []SeriesRef // series membership (MT4); empty when the doc has none
}

// SeriesRef is one series reference from an OpenLibrary edition document
// (MT4): the series title and the numbered volume label. The service maps
// it onto the domain's SeriesRef.
type SeriesRef struct {
	// Title is the series name.
	Title string
	// Volume is the numbered volume/part label ("1", "2", ...); "" when the
	// reference is unnumbered (secondary membership).
	Volume string
}

// WorkDetails is the Open Library work document as fetched by
// GetWorkDetails (MT2): the work-level title/authors/cover plus the work's
// editions. The service maps it onto the domain's WorkDetails.
type WorkDetails struct {
	WorkKey          string
	Title            string
	Authors          []string
	CoverURL         string
	FirstPublishYear int
	Editions         []WorkEdition
}

// olWorkDoc is the subset of an OpenLibrary work document
// (GET /works/OL…W.json) we consume.
type olWorkDoc struct {
	Key              string         `json:"key"` // "/works/OL66554W"
	Title            string         `json:"title"`
	Authors          []olAuthorRef  `json:"authors"`
	Covers           []int          `json:"covers"` // cover ids; [0] = primary
	FirstPublishYear int            `json:"first_publish_year"`
	Editions         []olEditionDoc `json:"editions"`
}

// olAuthorRef is one author reference of a work document (an object with a
// name, or a bare string in OL's looser documents).
type olAuthorRef struct {
	Name string `json:"name"`
}

// olEditionDoc is the subset of an OpenLibrary edition document.
type olEditionDoc struct {
	Key           string        `json:"key"` // "/works/OL66554W/OL47044678M"
	Title         string        `json:"title"`
	ISBN10        []string      `json:"isbn_10"`
	ISBN13        []string      `json:"isbn_13"`
	ASIN          string        `json:"amazon_id"` // Amazon identifier; "" when absent (MT4)
	Formats       []string      `json:"formats"`   // e.g. ["EPUB", "PDF"]
	Languages     []any         `json:"languages"` // strings, or objects with key/code
	Publishers    []string      `json:"publishers"`
	PublishDate   string        `json:"publish_date"` // raw: "2017-06", "2017", ...
	NumberOfPages int           `json:"number_of_pages"`
	Series        []olSeriesRef `json:"series"` // series membership (MT4); absent for most works
}

// olSeriesRef is the series membership on an OpenLibrary edition document
// (MT4): "series" is a list of {"title": ..., "volume": N} objects. Volume
// is present (a number or a string) when the membership is primary — the
// edition is volume N of the series — and absent when the reference is
// unnumbered/secondary (the work merely belongs to the series).
type olSeriesRef struct {
	Title  string `json:"title"`
	Volume any    `json:"volume"` // number, string, or nil
}

// olSeriesVolume renders a raw series volume value into its label: "1",
// "2.5", ... for numbers (integral values render without a decimal point),
// the trimmed string as-is, and "" for nil/zero/unknown.
func olSeriesVolume(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(t)
	case float64: // json decodes numbers to float64
		if t == 0 {
			return ""
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case int:
		if t == 0 {
			return ""
		}
		return strconv.Itoa(t)
	default:
		return ""
	}
}

// GetWorkDetails fetches the OpenLibrary work document for a work key (bare
// id or full "/works/…W" key) and maps it to a WorkDetails: the work-level
// title/authors/cover plus the work's editions (capped at MaxWorkEditions,
// in document order). A missing work or any non-2xx / malformed answer is
// ErrBadResponse (the API maps it to a 502; the service degrades to a
// cached row when it has one).
func (o *OpenLibrary) GetWorkDetails(ctx context.Context, workKey string) (WorkDetails, error) {
	k := NormalizeOLID(workKey)
	if k == "" {
		return WorkDetails{}, fmt.Errorf("%w: invalid openlibrary work key %q", ErrBadResponse, workKey)
	}
	o.Base = o.baseURL()
	var doc olWorkDoc
	if err := o.getJSON(ctx, "/works/"+k+".json", nil, &doc); err != nil {
		return WorkDetails{}, err
	}
	details := WorkDetails{
		WorkKey:          NormalizeOLID(doc.Key),
		Title:            doc.Title,
		Authors:          make([]string, 0, len(doc.Authors)),
		FirstPublishYear: doc.FirstPublishYear,
	}
	if details.WorkKey == "" {
		details.WorkKey = k
	}
	for _, a := range doc.Authors {
		if n := strings.TrimSpace(a.Name); n != "" {
			details.Authors = append(details.Authors, n)
		}
	}
	if len(doc.Covers) > 0 {
		details.CoverURL = olCoverURL(doc.Covers[0])
	}
	details.Editions = olWorkEditions(doc.Editions)
	return details, nil
}

// olWorkEditions maps edition documents to WorkEdition (capped at
// MaxWorkEditions, document order).
func olWorkEditions(eds []olEditionDoc) []WorkEdition {
	out := make([]WorkEdition, 0, len(eds))
	for _, e := range eds {
		if len(out) >= MaxWorkEditions {
			break
		}
		out = append(out, WorkEdition{
			Key:         editionID(e.Key),
			Title:       e.Title,
			ISBN:        editionISBN(e.ISBN13, e.ISBN10),
			ASIN:        e.ASIN,
			Formats:     e.Formats,
			Languages:   editionLanguages(e.Languages),
			Publishers:  e.Publishers,
			PublishDate: e.PublishDate,
			Year:        editionYear(e.PublishDate),
			Pages:       e.NumberOfPages,
			Series:      editionSeries(e.Series),
		})
	}
	return out
}

// editionSeries normalizes the edition document's series references (skips
// entries without a title); empty when the document has none.
func editionSeries(refs []olSeriesRef) []SeriesRef {
	out := make([]SeriesRef, 0, len(refs))
	for _, r := range refs {
		if strings.TrimSpace(r.Title) == "" {
			continue
		}
		out = append(out, SeriesRef{Title: r.Title, Volume: olSeriesVolume(r.Volume)})
	}
	return out
}

// editionID folds a full edition key ("/works/OL…W/OL…M") to the bare edition
// id ("OL…M"); an empty key yields "".
func editionID(key string) string {
	k := strings.TrimSpace(key)
	if i := strings.LastIndex(k, "/"); i >= 0 {
		k = k[i+1:]
	}
	return k
}

// editionISBN picks the edition's preferred ISBN: the first ISBN-13, else the
// first ISBN-10.
func editionISBN(isbn13, isbn10 []string) string {
	if s := firstNonEmpty(isbn13); s != "" {
		return s
	}
	return firstNonEmpty(isbn10)
}

// firstNonEmpty returns the first non-blank string of s ("" when none).
func firstNonEmpty(s []string) string {
	for _, v := range s {
		if t := strings.TrimSpace(v); t != "" {
			return t
		}
	}
	return ""
}

// editionLanguages extracts the ISO 639-1 codes from an edition's languages
// field. OL's documents carry either bare strings ("eng") or language
// objects — with a key ("/languages/eng") or a code — so all three shapes
// parse.
func editionLanguages(raw []any) []string {
	out := make([]string, 0, len(raw))
	seen := make(map[string]bool, len(raw))
	for _, v := range raw {
		var code string
		switch tv := v.(type) {
		case string:
			code = strings.TrimSpace(tv)
		case map[string]any:
			for _, field := range []string{"code", "key"} {
				if s, ok := tv[field].(string); ok {
					code = s
					break
				}
			}
			if i := strings.LastIndex(code, "/"); i >= 0 {
				code = code[i+1:] // "/languages/eng" -> "eng"
			}
		}
		if code != "" && !seen[code] {
			seen[code] = true
			out = append(out, code)
		}
	}
	return out
}

// editionYear parses the leading year of a raw Open Library publish-date
// string ("2017-06" -> 2017, "1974" -> 1974); 0 when absent/unknown.
func editionYear(publishDate string) int {
	s := strings.TrimSpace(publishDate)
	if len(s) < 4 {
		return 0
	}
	var n int
	digits := 0
	for _, r := range s[:4] {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
		digits++
	}
	if digits != 4 {
		return 0
	}
	return n
}
