// Package books (Readarr B5) — book import lists.
//
// The import-list section document (settings.BookImportListsSettings) names
// the sources — a Goodreads list, a LazyLibrarian instance, or another
// Readarr instance. This file is the domain side of the fetch-then-import
// pipeline: the normalized ExternalBook value a fetched list item becomes,
// the BookImporter interface the books library exposes to an import run,
// and FilterExcluded, which applies the document's list exclusions
// (settings.BookImportListExclusion) to a fetched batch before matching.
package books

import (
	"context"
	"strings"

	"github.com/brandenk514/mediarr/internal/settings"
)

// ExternalBook is one book pulled from an external import-list source
// (Goodreads list, LazyLibrarian, or a Readarr instance), normalized before
// exclusion filtering, exists-checking and import. Source-specific raw
// shapes are mapped onto this by the fetcher; the import run only ever sees
// ExternalBook.
type ExternalBook struct {
	// ForeignID is the source-side identity (the Goodreads list book id,
	// the LazyLibrarian id, or the source Readarr id). Exclusions match on
	// it (exact), and the import report carries it.
	ForeignID string
	// ISBN / ASIN when the source carries them (the strongest match keys;
	// "" = the source did not provide one).
	ISBN string
	ASIN string

	// Title / Author are the book's display metadata (the fetcher's
	// normalization of the source's title/author fields).
	Title  string
	Author string
	// Year is the release year ("" = unknown).
	Year string

	// Monitored carries the source's monitor state (the Readarr-instance
	// bridge; other sources import monitored).
	Monitored bool
	// Source is the configured list's name (the import report labels
	// each item with it).
	Source string
}

// BookImporter is the slice of the books library an import-list run uses:
// the adder and the current-library listing the exists-check matches
// against. The service layer (services/books) satisfies it.
type BookImporter interface {
	// AddBook adds one book to the library and returns its stored id.
	AddBook(ctx context.Context, b ExternalBook) (int64, error)
	// ListBooks returns the books currently in the library (the
	// exists-check and the exclusion name-match compare against it).
	ListBooks(ctx context.Context) ([]ExternalBook, error)
}

// exclusionKey is a normalized exclusion value (lower-cased, trimmed).
type exclusionKey string

// FilterExcluded removes the books an import-list document says should never
// be imported. A settings.BookImportListExclusion matches when its ForeignID
// equals the book's source id (exact, case-insensitive) OR its Name equals
// the book's title (case-insensitive). Books not excluded are kept in their
// original order; the result is never nil (an empty batch comes back as an
// empty slice, not nil).
func FilterExcluded(books []ExternalBook, exclusions []settings.BookImportListExclusion) []ExternalBook {
	byID := map[exclusionKey]bool{}
	byName := map[exclusionKey]bool{}
	for _, ex := range exclusions {
		if id := exclusionKey(strings.ToLower(strings.TrimSpace(ex.ForeignID))); id != "" {
			byID[id] = true
		}
		if name := exclusionKey(strings.ToLower(strings.TrimSpace(ex.Name))); name != "" {
			byName[name] = true
		}
	}
	out := make([]ExternalBook, 0, len(books))
	for _, b := range books {
		if byID[exclusionKey(strings.ToLower(strings.TrimSpace(b.ForeignID)))] {
			continue
		}
		if byName[exclusionKey(strings.ToLower(strings.TrimSpace(b.Title)))] {
			continue
		}
		out = append(out, b)
	}
	return out
}
