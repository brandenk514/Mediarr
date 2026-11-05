package books

import (
	"context"
	"errors"
	"testing"

	dom "github.com/brandenk514/mediarr/internal/domains/books"
	"github.com/brandenk514/mediarr/internal/providers"
)

// idFakeSearcher implements dom.BookSearcher plus every MT1 id surface
// (SearchWorkByISBN / SearchWorkByASIN / SearchWorkByKey) and counts each
// surface's calls, so the routing tests can assert WHICH leg the query took.
type idFakeSearcher struct {
	authors   []dom.BookSearchHit
	works     []dom.BookSearchHit
	keyWorks  map[string][]dom.BookSearchHit
	isbnWorks map[string][]dom.BookSearchHit
	asinWorks map[string][]dom.BookSearchHit
	isbnErr   error

	authorCalls, workCalls, keyCalls, isbnCalls, asinCalls int
	lastISBN, lastTitle                                    string
}

func (f *idFakeSearcher) SearchAuthors(ctx context.Context, q string) ([]dom.BookSearchHit, error) {
	f.authorCalls++
	return f.authors, nil
}

func (f *idFakeSearcher) SearchWorks(ctx context.Context, author, title string) ([]dom.BookSearchHit, error) {
	f.workCalls++
	f.lastTitle = title
	return f.works, nil
}

func (f *idFakeSearcher) SearchWorkByKey(ctx context.Context, key string) ([]dom.BookSearchHit, error) {
	f.keyCalls++
	return f.keyWorks[key], nil
}

func (f *idFakeSearcher) SearchWorkByISBN(ctx context.Context, isbn string) ([]dom.BookSearchHit, error) {
	f.isbnCalls++
	f.lastISBN = isbn
	if f.isbnErr != nil {
		return nil, f.isbnErr
	}
	return f.isbnWorks[isbn], nil
}

func (f *idFakeSearcher) SearchWorkByASIN(ctx context.Context, asin string) ([]dom.BookSearchHit, error) {
	f.asinCalls++
	return f.asinWorks[asin], nil
}

// fakeGR is the unit fake for GoodreadsResolver (the MT1 Goodreads→ISBN
// surface).
type fakeGR struct {
	isbn  string
	err   error
	calls int
}

func (f *fakeGR) BookISBN(ctx context.Context, bookID string) (string, error) {
	f.calls++
	return f.isbn, f.err
}

// newIDService wires a Service with fakes for the id-routing tests (same
// package as the MT3 tests, so newMT3Store/newMemRepo are shared).
func newIDService(t *testing.T, srch dom.BookSearcher, store *mt3Store, gr GoodreadsResolver) *Service {
	t.Helper()
	return New(Deps{
		Repo:              newMemRepo(),
		Searcher:          srch,
		MetadataStore:     store,
		GoodreadsResolver: gr,
	})
}

// TestIDLookupISBNSyntax pins the MT1 isbn: query: it routes to
// SearchWorkByISBN (normalized value), returns kind=book hits, never touches
// the author/work text-search legs, and write-throughs the resolved work
// into the provenance store (MT1 keeps the MT3 store the single source).
func TestIDLookupISBNSyntax(t *testing.T) {
	srch := &idFakeSearcher{
		isbnWorks: map[string][]dom.BookSearchHit{
			"9780140449335": {{ID: "OL66554W", Name: "Pride and Prejudice", Author: "Jane Austen", Year: 1813}},
		},
	}
	store := newMT3Store()
	svc := newIDService(t, srch, store, nil)
	res, err := svc.BooksLookup(context.Background(), "isbn:9780140449335")
	if err != nil {
		t.Fatalf("BooksLookup: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("got %d results, want 1: %+v", len(res), res)
	}
	if res[0].Kind != dom.LookupBook || res[0].Name != "Pride and Prejudice" || res[0].Author != "Jane Austen" {
		t.Fatalf("bad hit: %+v", res[0])
	}
	if res[0].Added || res[0].LibraryID != 0 {
		t.Fatalf("id-resolved work must not be marked added: %+v", res[0])
	}
	if srch.isbnCalls != 1 || srch.lastISBN != "9780140449335" {
		t.Fatalf("isbnCalls=%d lastISBN=%q; want 1 call with the normalized ISBN", srch.isbnCalls, srch.lastISBN)
	}
	if srch.authorCalls != 0 || srch.workCalls != 0 {
		t.Fatalf("text-search legs must not run for an id query: authorCalls=%d workCalls=%d", srch.authorCalls, srch.workCalls)
	}
	if _, ok := store.rows["openlibrary\x00OL66554W"]; !ok {
		t.Fatal("the id-resolved work was NOT upserted into the provenance store (MT1 single-source rule)")
	}
}

// TestIDLookupISBNBare13 pins the unambiguous bare form: a bare 13-digit
// query is an ISBN-13.
func TestIDLookupISBNBare13(t *testing.T) {
	srch := &idFakeSearcher{
		isbnWorks: map[string][]dom.BookSearchHit{
			"9780140449335": {{ID: "OL66554W", Name: "Pride and Prejudice", Author: "Jane Austen"}},
		},
	}
	store := newMT3Store()
	svc := newIDService(t, srch, store, nil)
	if _, err := svc.BooksLookup(context.Background(), "9780140449335"); err != nil {
		t.Fatalf("BooksLookup: %v", err)
	}
	if srch.isbnCalls != 1 {
		t.Fatalf("isbnCalls=%d, want 1 (bare 13 digits = ISBN-13)", srch.isbnCalls)
	}
	if srch.authorCalls != 0 || srch.workCalls != 0 {
		t.Fatal("bare 13 digits must not fall through to the plain-text path")
	}
}

// TestIDLookupASINSyntax pins the MT1 asin: query: it routes to
// SearchWorkByASIN (raw value).
func TestIDLookupASINSyntax(t *testing.T) {
	srch := &idFakeSearcher{
		asinWorks: map[string][]dom.BookSearchHit{
			"B000S5MU0K": {{ID: "OL17732W", Name: "Bible", Author: "Various"}},
		},
	}
	store := newMT3Store()
	svc := newIDService(t, srch, store, nil)
	res, err := svc.BooksLookup(context.Background(), "asin:B000S5MU0K")
	if err != nil {
		t.Fatalf("BooksLookup: %v", err)
	}
	if len(res) != 1 || res[0].Name != "Bible" || res[0].Author != "Various" {
		t.Fatalf("got %+v", res)
	}
	if srch.asinCalls != 1 || srch.isbnCalls != 0 {
		t.Fatalf("asinCalls=%d isbnCalls=%d; want the asin leg only", srch.asinCalls, srch.isbnCalls)
	}
}

// TestIDLookupASINBare pins the unambiguous bare form: a bare "B"+9-alnum
// query is an ASIN.
func TestIDLookupASINBare(t *testing.T) {
	srch := &idFakeSearcher{
		asinWorks: map[string][]dom.BookSearchHit{
			"B000S5MU0K": {{ID: "OL17732W", Name: "Bible", Author: "Various"}},
		},
	}
	store := newMT3Store()
	svc := newIDService(t, srch, store, nil)
	if _, err := svc.BooksLookup(context.Background(), "B000S5MU0K"); err != nil {
		t.Fatalf("BooksLookup: %v", err)
	}
	if srch.asinCalls != 1 || srch.isbnCalls != 0 || srch.authorCalls != 0 {
		t.Fatalf("asinCalls=%d isbnCalls=%d authorCalls=%d; want the asin leg only", srch.asinCalls, srch.isbnCalls, srch.authorCalls)
	}
}

// TestIDLookupOLidSyntax pins the olid: query: it routes to the MT3 cached
// work-key path (store consulted, then the key lookup for the refetch).
func TestIDLookupOLidSyntax(t *testing.T) {
	srch := &idFakeSearcher{
		keyWorks: map[string][]dom.BookSearchHit{
			"OL59863W": {{ID: "OL59863W", Name: "The Dispossessed", Author: "Ursula K. Le Guin", Year: 1974}},
		},
	}
	store := newMT3Store()
	svc := newIDService(t, srch, store, nil)
	res, err := svc.BooksLookup(context.Background(), "olid:OL59863W")
	if err != nil {
		t.Fatalf("BooksLookup: %v", err)
	}
	if len(res) != 1 || res[0].Name != "The Dispossessed" || res[0].Author != "Ursula K. Le Guin" {
		t.Fatalf("got %+v", res)
	}
	if srch.keyCalls != 1 || srch.isbnCalls != 0 || srch.asinCalls != 0 {
		t.Fatalf("keyCalls=%d; want the cached key path only", srch.keyCalls)
	}
}

// TestIDLookupGoodreadsChain pins the MT1 goodreads: chain: Goodreads id →
// ISBN (resolver) → work (SearchWorkByISBN), with the write-through.
func TestIDLookupGoodreadsChain(t *testing.T) {
	srch := &idFakeSearcher{
		isbnWorks: map[string][]dom.BookSearchHit{
			"9780140449335": {{ID: "OL66554W", Name: "Pride and Prejudice", Author: "Jane Austen"}},
		},
	}
	gr := &fakeGR{isbn: "9780140449335"}
	store := newMT3Store()
	svc := newIDService(t, srch, store, gr)
	res, err := svc.BooksLookup(context.Background(), "goodreads:14")
	if err != nil {
		t.Fatalf("BooksLookup: %v", err)
	}
	if len(res) != 1 || res[0].Name != "Pride and Prejudice" || res[0].Author != "Jane Austen" {
		t.Fatalf("got %+v", res)
	}
	if gr.calls != 1 {
		t.Fatalf("goodreads resolver calls=%d, want 1", gr.calls)
	}
	if srch.isbnCalls != 1 || srch.lastISBN != "9780140449335" {
		t.Fatalf("isbnCalls=%d lastISBN=%q; want the chained ISBN lookup", srch.isbnCalls, srch.lastISBN)
	}
	if _, ok := store.rows["openlibrary\x00OL66554W"]; !ok {
		t.Fatal("goodreads-chain work was NOT upserted into the provenance store")
	}
}

// TestIDLookupGoodreadsFails pins the degradation: a Goodreads failure
// (walled page / no ISBN) returns an EMPTY result list with no error — the
// rest of the lookup is never broken.
func TestIDLookupGoodreadsFails(t *testing.T) {
	srch := &idFakeSearcher{}
	gr := &fakeGR{err: providers.ErrBadResponse}
	store := newMT3Store()
	svc := newIDService(t, srch, store, gr)
	res, err := svc.BooksLookup(context.Background(), "goodreads:14")
	if err != nil {
		t.Fatalf("BooksLookup: %v; a Goodreads failure must not surface an error", err)
	}
	if len(res) != 0 {
		t.Fatalf("got %d results, want 0: %+v", len(res), res)
	}
	if srch.isbnCalls != 0 {
		t.Fatalf("the ISBN leg must not run without an ISBN (calls=%d)", srch.isbnCalls)
	}
}

// TestIDLookupGoodreadsNotWired pins the not-wired degradation: a nil
// resolver (no Goodreads provider) returns an empty result list with no
// error.
func TestIDLookupGoodreadsNotWired(t *testing.T) {
	srch := &idFakeSearcher{}
	store := newMT3Store()
	svc := newIDService(t, srch, store, nil)
	res, err := svc.BooksLookup(context.Background(), "goodreads:14")
	if err != nil {
		t.Fatalf("BooksLookup: %v", err)
	}
	if len(res) != 0 {
		t.Fatalf("got %d results, want 0: %+v", len(res), res)
	}
}

// TestIDLookupBare10DigitsPlainText pins the ambiguity resolution: a bare
// 10-digit query is NOT an ISBN (the parser needs the isbn: prefix for it)
// and falls through to the plain-text path.
func TestIDLookupBare10DigitsPlainText(t *testing.T) {
	srch := &idFakeSearcher{}
	store := newMT3Store()
	svc := newIDService(t, srch, store, nil)
	if _, err := svc.BooksLookup(context.Background(), "0140449335"); err != nil {
		t.Fatalf("BooksLookup: %v", err)
	}
	if srch.authorCalls != 1 || srch.workCalls != 1 {
		t.Fatalf("authorCalls=%d workCalls=%d; want the plain-text path", srch.authorCalls, srch.workCalls)
	}
	if srch.isbnCalls != 0 || srch.asinCalls != 0 {
		t.Fatal("bare 10 digits must not be treated as an id")
	}
}

// TestIDLookupInvalidPrefixPlainText pins the invalid-value fallback:
// isbn:<non-ISBN> is not an id query, so the RAW query (prefix included)
// goes to the plain-text search.
func TestIDLookupInvalidPrefixPlainText(t *testing.T) {
	srch := &idFakeSearcher{}
	store := newMT3Store()
	svc := newIDService(t, srch, store, nil)
	if _, err := svc.BooksLookup(context.Background(), "isbn:abc"); err != nil {
		t.Fatalf("BooksLookup: %v", err)
	}
	if srch.workCalls != 1 || srch.lastTitle != "isbn:abc" {
		t.Fatalf("workCalls=%d lastTitle=%q; want the raw query as a plain-text search", srch.workCalls, srch.lastTitle)
	}
	if srch.isbnCalls != 0 {
		t.Fatal("an invalid isbn: value must not hit the ISBN leg")
	}
}

// TestIDLookupSearcherWithoutISBNSurface pins the legacy-fake degradation:
// a searcher implementing only dom.BookSearcher (pre-MT1 surface, like the
// MT3 key fake) makes an isbn: query fall back to the plain-text path.
func TestIDLookupSearcherWithoutISBNSurface(t *testing.T) {
	srch := &mt3KeySearcher{} // only dom.BookSearcher + SearchWorkByKey
	store := newMT3Store()
	svc := newIDService(t, srch, store, nil)
	if _, err := svc.BooksLookup(context.Background(), "isbn:9780140449335"); err != nil {
		t.Fatalf("BooksLookup: %v", err)
	}
	if srch.authorCalls != 1 || srch.workCalls != 1 {
		t.Fatalf("authorCalls=%d workCalls=%d; want the plain-text fallback", srch.authorCalls, srch.workCalls)
	}
}

// TestIDLookupISBNOLError pins the upstream failure: an Open Library failure
// on the ISBN leg surfaces as an error (the API maps it to 502) with no
// results — NOT a silent empty list (the user's input was unambiguous).
func TestIDLookupISBNOLError(t *testing.T) {
	srch := &idFakeSearcher{isbnErr: providers.ErrBadResponse}
	store := newMT3Store()
	svc := newIDService(t, srch, store, nil)
	_, err := svc.BooksLookup(context.Background(), "isbn:9780140449335")
	if !errors.Is(err, providers.ErrBadResponse) {
		t.Fatalf("err = %v, want ErrBadResponse", err)
	}
}

// TestIDLookupWriteThroughStoreError pins the best-effort write-through: a
// provenance-store failure does NOT fail an id lookup — the work hits are
// still returned (MT3 convention).
func TestIDLookupWriteThroughStoreError(t *testing.T) {
	srch := &idFakeSearcher{
		isbnWorks: map[string][]dom.BookSearchHit{
			"9780140449335": {{ID: "OL66554W", Name: "Pride and Prejudice", Author: "Jane Austen"}},
		},
	}
	store := newMT3Store()
	store.upsertErr = errors.New("boom")
	svc := newIDService(t, srch, store, nil)
	res, err := svc.BooksLookup(context.Background(), "isbn:9780140449335")
	if err != nil {
		t.Fatalf("BooksLookup: %v; a store failure must not fail the lookup", err)
	}
	if len(res) != 1 || res[0].Name != "Pride and Prejudice" || res[0].Author != "Jane Austen" {
		t.Fatalf("got %+v", res)
	}
}
