package providers

import (
	"context"
	"net/url"
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
		out = append(out, Result{
			ID:    strings.TrimPrefix(d.Key, "/works/"),
			Title: d.Title,
			Year:  d.FirstPublishYear,
			Extra: map[string]string{"author": author},
		})
	}
	return out, nil
}
