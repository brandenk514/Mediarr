package system

import (
	"context"
	"regexp"
	"strings"

	"github.com/brandenk514/mediarr/internal/providers"
)

// olWorkKeyRe is the OpenLibrary work-id shape ("OL12345W"). A source
// foreignBookId matching it is already an OpenLibrary work key (the "source"
// fast path of the id swap).
var olWorkKeyRe = regexp.MustCompile(`^OL[0-9]+W$`)

// isOLWorkKey reports whether s is a bare OpenLibrary work id.
func isOLWorkKey(s string) bool { return olWorkKeyRe.MatchString(strings.TrimSpace(s)) }

// olBookIDLookup is the minimal OpenLibrary surface the resolver needs (the
// B6/MT1 id-based lookups + the plain-text work search fallback).
// *providers.OpenLibrary satisfies it directly; tests use a fake.
type olBookIDLookup interface {
	SearchWorkByKey(ctx context.Context, key string) ([]providers.Result, error)
	SearchWorkByISBN(ctx context.Context, isbn string) ([]providers.Result, error)
	SearchWorks(ctx context.Context, author, title string) ([]providers.Result, error)
}

// OpenLibraryBookIDResolver is the ReadarrBookIDResolver implementation: it
// performs the one-time ISBN/ASIN/foreign-id -> OpenLibrary work-key swap.
// Precedence (the first hit wins, and a hit is recorded by method):
//
//  1. "source": the source foreignBookId is already an OpenLibrary work key
//     (Readarr's own books are OL-backed, so most books take this path).
//  2. "isbn":   the book's ISBN13 (or ASIN, when Readarr stored an Amazon id
//     as the foreign id) resolves through OL's id-based search.
//  3. "search": title + author plain-text search (the last resort).
//
// A miss on every path returns ("", "search", nil) — the migrator then keys
// the book by its source id with a report note (no book is dropped).
type OpenLibraryBookIDResolver struct {
	OL olBookIDLookup
}

// NewOpenLibraryBookIDResolver builds the resolver over an Open Library
// adapter.
func NewOpenLibraryBookIDResolver(ol *providers.OpenLibrary) *OpenLibraryBookIDResolver {
	return &OpenLibraryBookIDResolver{OL: ol}
}

// ResolveWorkKey implements ReadarrBookIDResolver.
func (r *OpenLibraryBookIDResolver) ResolveWorkKey(ctx context.Context, id ReadarrBookIdentity) (string, string, error) {
	// 1. The source already carries an OL work key — confirm it resolves.
	if key := providers.NormalizeOLID(id.ForeignBookID); key != "" && isOLWorkKey(key) {
		if r.OL != nil {
			if hits, err := r.OL.SearchWorkByKey(ctx, key); err == nil && len(hits) > 0 {
				return hits[0].ID, "source", nil
			}
			// A miss is not fatal: the id is well-formed, so trust it.
			return key, "source", nil
		}
		return key, "source", nil
	}

	if r.OL == nil {
		return "", "source", nil
	}

	// 2. ISBN-based id lookup. Readarr stores the ISBN (or, for Amazon
	//    sources, the ASIN) in foreignBookId; the edition's ISBN13 is the
	//    authoritative one when present.
	for _, candidate := range []string{id.ISBN13, id.ForeignBookID, id.ASIN} {
		c := strings.TrimSpace(candidate)
		if c == "" {
			continue
		}
		if isOLWorkKey(c) {
			continue // handled above
		}
		if hits, err := r.OL.SearchWorkByISBN(ctx, c); err == nil && len(hits) > 0 {
			return hits[0].ID, "isbn", nil
		}
	}

	// 3. Plain-text title + author search.
	if id.Title != "" {
		hits, err := r.OL.SearchWorks(ctx, id.Author, id.Title)
		if err != nil {
			return "", "search", err
		}
		if len(hits) > 0 {
			return hits[0].ID, "search", nil
		}
	}
	return "", "search", nil
}
