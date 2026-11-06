package books

import (
	"context"
	"strconv"
	"strings"

	dom "github.com/brandenk514/mediarr/internal/domains/books"
	"github.com/brandenk514/mediarr/internal/providers"
)

// openLibrarySearcher adapts the keyless Open Library provider to
// dom.BookSearcher (the combined plain-text books lookup, Readarr B2). It maps
// the provider's search hits to the domain's BookSearchHit: author hits carry
// only a name (Open Library has no stable author id, so no year/author); work
// hits carry the first author name (from the provider's "author" extra) and
// the first publish year.
type openLibrarySearcher struct {
	ol *providers.OpenLibrary
}

// NewOpenLibrarySearcher wraps the Open Library provider as the books
// combined-lookup searcher (Readarr B2). Plain-text only: ISBN/ASIN/Goodreads
// id-lookup syntax is a separate metadata work item (MT1), not the B2 lookup.
func NewOpenLibrarySearcher(ol *providers.OpenLibrary) dom.BookSearcher {
	return &openLibrarySearcher{ol: ol}
}

// SearchAuthors resolves q to candidate author names.
func (o *openLibrarySearcher) SearchAuthors(ctx context.Context, q string) ([]dom.BookSearchHit, error) {
	res, err := o.ol.SearchAuthors(ctx, q)
	if err != nil {
		return nil, err
	}
	out := make([]dom.BookSearchHit, 0, len(res))
	for _, r := range res {
		out = append(out, dom.BookSearchHit{ID: r.ID, Name: r.Title})
	}
	return out, nil
}

// SearchWorks finds works matching title for the given author (either may be
// empty; the combined lookup passes an empty author for a plain-text title
// search).
func (o *openLibrarySearcher) SearchWorks(ctx context.Context, author, title string) ([]dom.BookSearchHit, error) {
	res, err := o.ol.SearchWorks(ctx, author, title)
	if err != nil {
		return nil, err
	}
	return mapWorkHits(res), nil
}

// mapWorkHits maps Open Library work-level provider hits to domain hits:
// first author name (from the provider's "author" extra) and the first
// publish year, plus the MT4 tier-1 facts (the search document's language
// list and indexed-edition count — the popularity proxy — from the
// provider's "languages"/"edition_count" extras; absent extras map to
// nil/0 = unknown).
func mapWorkHits(res []providers.Result) []dom.BookSearchHit {
	out := make([]dom.BookSearchHit, 0, len(res))
	for _, r := range res {
		out = append(out, dom.BookSearchHit{
			ID:       r.ID,
			Name:     r.Title,
			Author:   r.Extra["author"],
			Year:     r.Year,
			CoverURL: r.Extra["cover"],
			// MT4 tier-1 facts: the search document's languages (comma-
			// joined extra) and indexed-edition count (the popularity
			// proxy).
			Languages:    splitExtras(r.Extra["languages"]),
			EditionCount: intExtras(r.Extra["edition_count"]),
		})
	}
	return out
}

// splitExtras splits a comma-joined extras string into trimmed codes
// (nil when empty) — the provider's "languages" extra carries the
// search document's language list.
func splitExtras(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// intExtras parses an extras string as an int (0 when absent/unparseable)
// — the provider's "edition_count" extra.
func intExtras(s string) int {
	if strings.TrimSpace(s) == "" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}

// SearchWorkByISBN implements the MT1 isbnLookup surface: it resolves an
// ISBN to its Open Library work (the books lookup's `isbn:` / bare-13-digit
// id syntax, and the Goodreads-id chain's second leg).
func (o *openLibrarySearcher) SearchWorkByISBN(ctx context.Context, isbn string) ([]dom.BookSearchHit, error) {
	res, err := o.ol.SearchWorkByISBN(ctx, isbn)
	if err != nil {
		return nil, err
	}
	return mapWorkHits(res), nil
}

// SearchWorkByASIN implements the MT1 asinLookup surface: it resolves an
// Amazon ASIN to its Open Library work (the books lookup's `asin:` / bare
// "B"+9-alnum id syntax).
func (o *openLibrarySearcher) SearchWorkByASIN(ctx context.Context, asin string) ([]dom.BookSearchHit, error) {
	res, err := o.ol.SearchWorkByASIN(ctx, asin)
	if err != nil {
		return nil, err
	}
	return mapWorkHits(res), nil
}

// GetWorkDetails implements the MT2 workDetailsFetcher surface: it fetches
// the work document (title/authors/cover + editions) for a known
// OpenLibrary work key, mapping the provider shape to the domain's (the
// adapter never imports domains — the repo's rule).
func (o *openLibrarySearcher) GetWorkDetails(ctx context.Context, key string) (dom.WorkDetails, error) {
	d, err := o.ol.GetWorkDetails(ctx, key)
	if err != nil {
		return dom.WorkDetails{}, err
	}
	return mapWorkDetails(d), nil
}

// mapWorkDetails maps the provider's WorkDetails onto the domain's
// WorkDetails (including the MT4 edition fields: the edition's Amazon
// identifier and series references — the tier-2 filter data).
func mapWorkDetails(d providers.WorkDetails) dom.WorkDetails {
	eds := make([]dom.WorkEdition, 0, len(d.Editions))
	for _, e := range d.Editions {
		series := make([]dom.SeriesRef, 0, len(e.Series))
		for _, r := range e.Series {
			series = append(series, dom.SeriesRef{Title: r.Title, Volume: r.Volume})
		}
		eds = append(eds, dom.WorkEdition{
			Key:         e.Key,
			Title:       e.Title,
			ISBN:        e.ISBN,
			ASIN:        e.ASIN,
			Formats:     e.Formats,
			Languages:   e.Languages,
			Publishers:  e.Publishers,
			PublishDate: e.PublishDate,
			Year:        e.Year,
			Pages:       e.Pages,
			Series:      series,
		})
	}
	return dom.WorkDetails{
		WorkKey:          d.WorkKey,
		Title:            d.Title,
		Authors:          d.Authors,
		CoverURL:         d.CoverURL,
		FirstPublishYear: d.FirstPublishYear,
		Editions:         eds,
	}
}

// SearchWorkByKey resolves a known OpenLibrary work key to its work — the
// MT3 cached-path surface on top of dom.BookSearcher (the combined lookup
// consults the provenance store before calling it, and the refresh command
// drives it with a forced re-fetch). It wraps the provider's MT1-minimal
// id lookup; workKeySearcher in the service type-asserts for it.
func (o *openLibrarySearcher) SearchWorkByKey(ctx context.Context, key string) ([]dom.BookSearchHit, error) {
	res, err := o.ol.SearchWorkByKey(ctx, key)
	if err != nil {
		return nil, err
	}
	return mapWorkHits(res), nil
}
