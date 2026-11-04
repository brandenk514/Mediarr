package books

import "context"

// LookupKind tags one hit of the combined plain-text books lookup (Readarr
// B2): an author-name match or a work/title match.
type LookupKind string

const (
	// LookupAuthor is a hit on the author-name search (kind = "author").
	LookupAuthor LookupKind = "author"
	// LookupBook is a hit on the work search (kind = "book").
	LookupBook LookupKind = "book"
)

// BookSearchHit is one plain-text metadata search hit (an author candidate
// or a work) coming from a BookSearcher. Display-only: the books model keys
// an author by name and a title by (author, name), so no provider id is
// stored (the work key, when present, is display-only).
type BookSearchHit struct {
	ID     string // provider key; "" for authors (no stable provider id exists)
	Name   string // author name (author hits) or work title (book hits)
	Author string // book hits: the work's author ("" = unknown); "" for author hits
	Year   int    // book hits: first publish year (0 = unknown); 0 for author hits
}

// BookSearcher is the keyless plain-text metadata search the combined books
// lookup uses (Readarr B2). The Open Library adapter (services/books)
// implements it. ISBN/ASIN/Goodreads id-lookup syntax is a separate
// metadata work item (MT1) — the B2 lookup is plain-text only.
type BookSearcher interface {
	// SearchAuthors resolves q to distinct candidate authors.
	SearchAuthors(ctx context.Context, q string) ([]BookSearchHit, error)
	// SearchWorks finds works matching title for the given author (either
	// may be empty; both set narrows to that author's works).
	SearchWorks(ctx context.Context, author, title string) ([]BookSearchHit, error)
}

// LookupResult is one row of the combined plain-text books lookup:
// kind-tagged, with the `added` flag computed against the books library so
// the UI navigates to an already-added author/book instead of re-adding it.
// LibraryID is the matching author id (kind=author) or title id
// (kind=book); 0 when not added.
type LookupResult struct {
	Kind      LookupKind
	Name      string // author name (kind=author) or work title (kind=book)
	Author    string // kind=book: the work's author; "" when unknown
	Year      int    // kind=book: first publish year; 0 when unknown
	Added     bool
	LibraryID int64
}
