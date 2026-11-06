package books

import (
	"context"
	"fmt"

	dom "github.com/brandenk514/mediarr/internal/domains/books"
)

// isbnLookup is the MT1 work-by-ISBN surface on top of dom.BookSearcher:
// the lookup's `isbn:` / bare-13-digit id syntax, and the Goodreads-id
// chain's second leg, resolve through it. *openLibrarySearcher implements
// it; a plain fake searcher without it degrades the id query to the
// plain-text path (pre-MT1 behavior).
type isbnLookup interface {
	SearchWorkByISBN(ctx context.Context, isbn string) ([]dom.BookSearchHit, error)
}

// asinLookup is the MT1 work-by-ASIN surface on top of dom.BookSearcher:
// the lookup's `asin:` / bare "B"+9-alnum id syntax.
type asinLookup interface {
	SearchWorkByASIN(ctx context.Context, asin string) ([]dom.BookSearchHit, error)
}

// GoodreadsResolver is the MT1 Goodreads→ISBN surface: it resolves a
// Goodreads book id to the ISBN the lookup then chains into the OpenLibrary
// work. *providers.Goodreads implements it. Nil (not wired) makes a
// goodreads: query degrade to an empty result (the mapping source is
// best-effort).
type GoodreadsResolver interface {
	BookISBN(ctx context.Context, bookID string) (string, error)
}

// lookupIDWork routes an MT1 id query (ISBN or ASIN) to the matching work
// lookup surface and returns kind=book hits. Id-resolved works are upserted
// into the provenance store (MT1 keeps the MT3 store the single source:
// source "openlibrary", source_id the OL work key) — best-effort, as the
// MT3 write-through is.
//
// Degradation: a searcher without the surface falls back to the plain-text
// path for the raw id value; an Open Library failure surfaces as an error
// the API maps to 502 (the keyless-adapter convention).
//
// (MT4) A named profile (non-nil f) filters the resolved book hits before
// the write-through (a dropped hit earns no provenance row).
func (s *Service) lookupIDWork(ctx context.Context, kind dom.IDKind, value string, f *dom.ProfileFilters) ([]dom.LookupResult, error) {
	var lookup func(ctx context.Context, id string) ([]dom.BookSearchHit, error)
	var ok bool
	switch kind {
	case dom.IDKindISBN:
		var v isbnLookup
		if v, ok = s.deps.Searcher.(isbnLookup); !ok {
			return s.lookupText(ctx, value, f)
		}
		lookup = v.SearchWorkByISBN
	case dom.IDKindASIN:
		var v asinLookup
		if v, ok = s.deps.Searcher.(asinLookup); !ok {
			return s.lookupText(ctx, value, f)
		}
		lookup = v.SearchWorkByASIN
	default:
		return nil, fmt.Errorf("books: lookup: unsupported id kind %q", kind)
	}
	hits, err := lookup(ctx, value)
	if err != nil {
		return nil, fmt.Errorf("books: lookup: %w", err)
	}
	// MT4: filter before the write-through (a dropped hit earns no
	// provenance row).
	if f != nil {
		hits = s.filterWorkHits(ctx, hits, f)
	}
	// Best-effort write-through (MT1, MT3 convention): a store failure must
	// not fail the lookup — the provenance is an enhancement over the
	// keyless search, not a precondition for it.
	_ = s.recordWorkMetadata(ctx, hits)
	return s.workLookupResults(ctx, hits)
}

// lookupGoodreads chains an MT1 goodreads:<id> query: Goodreads book id →
// ISBN → OpenLibrary work. A Goodreads failure — walled page, no ISBN on
// the page, or a not-wired resolver — degrades to an EMPTY result list,
// never an error: Goodreads is a best-effort scraped source and an outage
// must not break the rest of the lookup (the UI shows "no results"). The
// Open Library leg (once the ISBN is known) fails as usual.
//
// (MT4) A named profile (non-nil f) filters the resolved book hits before
// the write-through (a dropped hit earns no provenance row).
func (s *Service) lookupGoodreads(ctx context.Context, id string, f *dom.ProfileFilters) ([]dom.LookupResult, error) {
	if s.deps.GoodreadsResolver == nil {
		return []dom.LookupResult{}, nil
	}
	isbn, err := s.deps.GoodreadsResolver.BookISBN(ctx, id)
	if err != nil || isbn == "" {
		return []dom.LookupResult{}, nil
	}
	v, ok := s.deps.Searcher.(isbnLookup)
	if !ok {
		return []dom.LookupResult{}, nil
	}
	hits, err := v.SearchWorkByISBN(ctx, isbn)
	if err != nil {
		return nil, fmt.Errorf("books: lookup: %w", err)
	}
	// MT4: filter before the write-through (a dropped hit earns no
	// provenance row).
	if f != nil {
		hits = s.filterWorkHits(ctx, hits, f)
	}
	_ = s.recordWorkMetadata(ctx, hits)
	return s.workLookupResults(ctx, hits)
}
