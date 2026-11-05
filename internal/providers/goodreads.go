package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

// defaultGoodreadsBase is the Goodreads site. Goodreads has no public
// book-search API, so this adapter scrapes the book page.
const defaultGoodreadsBase = "https://www.goodreads.com"

// goodreadsUserAgent is a browser-like User-Agent: Goodreads serves the book
// page to browsers and walls other clients (the Cloudflare challenge arrives
// as a 403, which the adapter maps to a clean 502-class error).
const goodreadsUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

// Goodreads is the MT1 Goodreads→ISBN resolver: it fetches the book page
// (https://www.goodreads.com/book/show/<id>) and extracts the book's ISBN,
// which the books lookup then chains into the OpenLibrary work
// (SearchWorkByISBN). Goodreads is a best-effort source (scraped HTML,
// Cloudflare-walled at times) — every failure degrades to ErrBadResponse so
// a Goodreads outage never breaks the rest of the lookup.
type Goodreads struct {
	Client
	// BaseURL defaults to defaultGoodreadsBase when empty (tests point it
	// at a stub).
	BaseURL string
}

// NewGoodreads builds the adapter.
func NewGoodreads() *Goodreads {
	return &Goodreads{Client: Client{Name: "goodreads"}}
}

// Name implements the provider naming convention.
func (g *Goodreads) Name() string { return "goodreads" }

var grBookIDRe = regexp.MustCompile(`^\d+$`)

// BookISBN fetches the book page for a numeric Goodreads book id and returns
// its ISBN (hyphens stripped; 10 or 13 digits). An invalid id, a walled or
// non-2xx page (a Cloudflare 403 challenge included), or a page with no
// parseable ISBN returns ErrBadResponse (the API maps it to a 502) — it
// never panics or breaks the rest of the lookup.
func (g *Goodreads) BookISBN(ctx context.Context, bookID string) (string, error) {
	id := strings.TrimSpace(bookID)
	if !grBookIDRe.MatchString(id) {
		return "", fmt.Errorf("%w: invalid goodreads book id %q", ErrBadResponse, bookID)
	}
	page, err := g.fetchPage(ctx, id)
	if err != nil {
		return "", err
	}
	if isbn := extractISBN(page); isbn != "" {
		return isbn, nil
	}
	return "", fmt.Errorf("%w: no isbn found on goodreads book page", ErrBadResponse)
}

// fetchPage GETs the book page and returns its HTML body (capped at 4 MiB,
// like the shared getJSON). Non-2xx responses become ErrBadResponse with the
// status in the message.
func (g *Goodreads) fetchPage(ctx context.Context, id string) (string, error) {
	if g.BaseURL != "" {
		g.Base = g.BaseURL
	} else {
		g.Base = defaultGoodreadsBase
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.Base+"/book/show/"+id, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("User-Agent", goodreadsUserAgent)
	resp, err := g.client().Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrBadResponse, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", fmt.Errorf("%w: read body: %v", ErrBadResponse, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("%w: %s returned %d: %s", ErrBadResponse, g.Name(), resp.StatusCode, snippet(data, 200))
	}
	return string(data), nil
}

// extractISBN pulls the book's ISBN out of a Goodreads book page, most
// reliable source first:
//
//  1. the JSON-LD (schema.org) block — an @type "Book" node with an "isbn"
//     (or "isbn13"/"isbn10") value;
//  2. the embedded application state — "isbn13" / "isbn" JSON fields;
//  3. the legacy details table — an <td>ISBN</td> row.
//
// It returns "" when nothing parseable is present (the caller turns that
// into a clean ErrBadResponse).
func extractISBN(page string) string {
	if isbn := isbnFromJSONLD(page); isbn != "" {
		return isbn
	}
	if m := grISBN13StateRe.FindStringSubmatch(page); m != nil {
		return m[1]
	}
	if m := grISBN10StateRe.FindStringSubmatch(page); m != nil {
		return m[1]
	}
	if m := grDetailsTableRe.FindStringSubmatch(page); m != nil {
		return normalizeISBN(m[1])
	}
	return ""
}

// jsonLDBlockRe matches a <script type="application/ld+json"> block.
var jsonLDBlockRe = regexp.MustCompile(`(?is)<script[^>]*type=["']application/ld\+json["'][^>]*>(.*?)</script>`)

// isbnFromJSONLD walks every JSON-LD block for a schema.org Book node and
// returns its ISBN.
func isbnFromJSONLD(page string) string {
	for _, m := range jsonLDBlockRe.FindAllStringSubmatch(page, -1) {
		var node any
		if err := json.Unmarshal([]byte(m[1]), &node); err != nil {
			continue // malformed block: skip, the regex fallbacks still apply
		}
		if isbn := walkJSONLD(node); isbn != "" {
			return isbn
		}
	}
	return ""
}

// walkJSONLD breadth-first-scans a decoded JSON-LD value for an @type "Book"
// node and returns its ISBN ("isbn", then "isbn13", then "isbn10").
func walkJSONLD(node any) string {
	queue := []any{node}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		switch v := cur.(type) {
		case map[string]any:
			if isBookNode(v) {
				for _, field := range []string{"isbn", "isbn13", "isbn10"} {
					if raw, ok := v[field]; ok {
						if isbn := isbnFromValue(raw); isbn != "" {
							return isbn
						}
					}
				}
			}
			for _, field := range v {
				queue = append(queue, field)
			}
		case []any:
			queue = append(queue, v...)
		}
	}
	return ""
}

// isBookNode reports whether a JSON-LD object is a schema.org Book (the
// @type may be a string or a list).
func isBookNode(node map[string]any) bool {
	t, ok := node["@type"]
	if !ok {
		return false
	}
	switch tv := t.(type) {
	case string:
		return strings.EqualFold(tv, "Book")
	case []any:
		for _, e := range tv {
			if s, ok := e.(string); ok && strings.EqualFold(s, "Book") {
				return true
			}
		}
	}
	return false
}

// isbnFromValue extracts a 10/13-digit ISBN from a JSON-LD isbn value (a
// string, or a list of strings).
func isbnFromValue(v any) string {
	switch tv := v.(type) {
	case string:
		return isbnDigitsValid(tv)
	case []any:
		for _, e := range tv {
			if s, ok := e.(string); ok {
				if isbn := isbnDigitsValid(s); isbn != "" {
					return isbn
				}
			}
		}
	}
	return ""
}

// isbnDigitsValid keeps only the digits of s and accepts it when it is a 10-
// or 13-digit ISBN.
func isbnDigitsValid(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	d := b.String()
	if len(d) == 10 || len(d) == 13 {
		return d
	}
	return ""
}

// The application-state fallbacks. The closing quote is anchored so a 13-digit
// "isbn" value is not mis-read as its first 10 digits.
var (
	grISBN13StateRe  = regexp.MustCompile(`"isbn13"\s*:\s*"(\d{13})"`)
	grISBN10StateRe  = regexp.MustCompile(`"isbn"\s*:\s*"(\d{10})"`)
	grDetailsTableRe = regexp.MustCompile(`(?is)<t[dh][^>]*>\s*ISBN\s*</t[dh]>\s*<t[dh][^>]*>(?:<a[^>]*>)?\s*([0-9Xx-]{10,17})`)
)
