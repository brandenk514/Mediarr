package books

import (
	"context"

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
	out := make([]dom.BookSearchHit, 0, len(res))
	for _, r := range res {
		out = append(out, dom.BookSearchHit{
			ID:       r.ID,
			Name:     r.Title,
			Author:   r.Extra["author"],
			Year:     r.Year,
			CoverURL: r.Extra["cover"],
		})
	}
	return out, nil
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
	out := make([]dom.BookSearchHit, 0, len(res))
	for _, r := range res {
		out = append(out, dom.BookSearchHit{
			ID:       r.ID,
			Name:     r.Title,
			Author:   r.Extra["author"],
			Year:     r.Year,
			CoverURL: r.Extra["cover"],
		})
	}
	return out, nil
}
