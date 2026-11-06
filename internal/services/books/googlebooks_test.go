package books

import (
	"context"
	"errors"
	"testing"

	dom "github.com/brandenk514/mediarr/internal/domains/books"
	"github.com/brandenk514/mediarr/internal/settings"
)

// --- MT5 (Google Books secondary source) ---

// mt5GB is a fake GoogleBooksSearcher with a call counter (the
// "GB NOT called" assertions) and a settable error (the best-effort
// leg-failure test).
type mt5GB struct {
	enabled bool
	hits    []dom.BookSearchHit
	err     error
	calls   int
	lastQ   string
}

func (g *mt5GB) Configured() bool { return g.enabled }

func (g *mt5GB) SearchBooks(ctx context.Context, q string) ([]dom.BookSearchHit, error) {
	g.calls++
	g.lastQ = q
	if g.err != nil {
		return nil, g.err
	}
	out := make([]dom.BookSearchHit, len(g.hits))
	for i, h := range g.hits {
		h.Source = dom.SourceGoogleBooks // the real wrapper sets this marker
		out[i] = h
	}
	return out, nil
}

func mt5GBHit(id, name, author string, year int, langs ...string) dom.BookSearchHit {
	return dom.BookSearchHit{ID: id, Name: name, Author: author, Year: year, Languages: langs}
}

func TestGoogleBooksLeg_FillsEmptyGap(t *testing.T) {
	// Plain-text query, OL leg empty, GB configured: the GB hits are
	// appended, source-marked, and earn NO provenance row (the store is
	// OL provenance for the editions flow; GB hits never get editions).
	gb := &mt5GB{enabled: true, hits: []dom.BookSearchHit{
		mt5GBHit("vol1", "The Dispossessed", "Ursula K. Le Guin", 1974, "eng"),
	}}
	store := newMT3Store()
	svc := New(Deps{
		Repo:          newMemRepo(),
		Searcher:      &mt4Searcher{works: nil}, // OL leg: zero book hits
		MetadataStore: store,
		GoogleBooks:   gb,
	})

	results, err := svc.BooksLookup(context.Background(), "the dispossessed")
	if err != nil {
		t.Fatalf("BooksLookup: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1 GB hit: %+v", len(results), results)
	}
	if results[0].Kind != dom.LookupBook || results[0].Name != "The Dispossessed" {
		t.Errorf("results[0] = %+v, want the GB book hit", results[0])
	}
	// By design: a GB hit carries no work key (the UI's manual-add
	// fallback handles it — no editions fetch for it).
	if results[0].WorkKey != "" {
		t.Errorf("WorkKey = %q, want empty for a GB hit", results[0].WorkKey)
	}
	if gb.calls != 1 || gb.lastQ != "the dispossessed" {
		t.Errorf("GB calls = %d (last %q), want 1 with the plain query", gb.calls, gb.lastQ)
	}
	// No provenance row for the GB hit (volume id is not an OL key).
	if _, ok, _ := store.GetWork(context.Background(), "vol1"); ok {
		t.Error("the GB hit earned a provenance row (want none)")
	}
}

func TestGoogleBooksLeg_OLWithHitsNeverCallsGB(t *testing.T) {
	gb := &mt5GB{enabled: true, hits: []dom.BookSearchHit{mt5GBHit("vol1", "X", "Y", 0)}}
	svc := New(Deps{
		Repo: newMemRepo(),
		Searcher: &mt4Searcher{works: []dom.BookSearchHit{
			{ID: "OL1W", Name: "The Dispossessed", Author: "Ursula K. Le Guin", Year: 1974},
		}},
		GoogleBooks: gb,
	})

	results, err := svc.BooksLookup(context.Background(), "the dispossessed")
	if err != nil {
		t.Fatalf("BooksLookup: %v", err)
	}
	if len(results) != 1 || results[0].Name != "The Dispossessed" {
		t.Fatalf("results = %+v, want the OL hit only", results)
	}
	if results[0].WorkKey != "OL1W" {
		t.Errorf("WorkKey = %q, want OL1W (OL hit unchanged)", results[0].WorkKey)
	}
	if gb.calls != 0 {
		t.Errorf("GB calls = %d, want 0 (OL-with-hits => OL only)", gb.calls)
	}
}

func TestGoogleBooksLeg_IDLookupNeverCallsGB(t *testing.T) {
	// An id-lookup query (a plain-text query's OL leg would be empty)
	// must NOT run the GB leg: ids are unambiguous and OL is their
	// source of truth.
	gb := &mt5GB{enabled: true, hits: []dom.BookSearchHit{mt5GBHit("vol1", "X", "Y", 0)}}
	svc := New(Deps{
		Repo: newMemRepo(),
		// The searcher has the isbn surface, so the id path does NOT
		// degrade to the text path.
		Searcher:    &mt4Searcher{isbnWorks: map[string][]dom.BookSearchHit{"9780441478792": {}}},
		GoogleBooks: gb,
	})

	results, err := svc.BooksLookup(context.Background(), "isbn:9780441478792")
	if err != nil {
		t.Fatalf("BooksLookup: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("results = %+v, want none (empty OL isbn result, no GB)", results)
	}
	if gb.calls != 0 {
		t.Errorf("GB calls = %d, want 0 (id-lookup never runs the GB leg)", gb.calls)
	}
}

func TestGoogleBooksLeg_IDLookupDegradedToTextStillNoGB(t *testing.T) {
	// Even the degraded path (a searcher WITHOUT the isbn surface falls
	// back to the text search) must not run the GB leg: the query is
	// still id syntax.
	gb := &mt5GB{enabled: true, hits: []dom.BookSearchHit{mt5GBHit("vol1", "X", "Y", 0)}}
	svc := New(Deps{
		Repo:        newMemRepo(),
		Searcher:    bareTextSearcher{}, // no isbnLookup surface: degrades to text
		GoogleBooks: gb,
	})

	results, err := svc.BooksLookup(context.Background(), "isbn:9780441478792")
	if err != nil {
		t.Fatalf("BooksLookup: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("results = %+v, want none", results)
	}
	if gb.calls != 0 {
		t.Errorf("GB calls = %d, want 0 (degraded id path is still id syntax)", gb.calls)
	}
}

// bareTextSearcher is the minimal dom.BookSearcher (no MT1 id surfaces):
// the fake that makes the id path degrade to the plain-text search.
type bareTextSearcher struct{}

func (bareTextSearcher) SearchAuthors(ctx context.Context, q string) ([]dom.BookSearchHit, error) {
	return nil, nil
}

func (bareTextSearcher) SearchWorks(ctx context.Context, author, title string) ([]dom.BookSearchHit, error) {
	return nil, nil
}

func TestGoogleBooksLeg_NotConfiguredNoLeg(t *testing.T) {
	gb := &mt5GB{enabled: false, hits: []dom.BookSearchHit{mt5GBHit("vol1", "X", "Y", 0)}}
	svc := New(Deps{
		Repo:        newMemRepo(),
		Searcher:    &mt4Searcher{works: nil},
		GoogleBooks: gb,
	})

	results, err := svc.BooksLookup(context.Background(), "the dispossessed")
	if err != nil {
		t.Fatalf("BooksLookup: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("results = %+v, want none (leg disabled)", results)
	}
	if gb.calls != 0 {
		t.Errorf("GB calls = %d, want 0 (not configured)", gb.calls)
	}
}

func TestGoogleBooksLeg_FailureIsBestEffort(t *testing.T) {
	gb := &mt5GB{enabled: true, err: errors.New("GB exploded")}
	svc := New(Deps{
		Repo:        newMemRepo(),
		Searcher:    &mt4Searcher{works: nil},
		GoogleBooks: gb,
	})

	// A failing secondary source never breaks the lookup: the OL-only
	// (empty) result is returned, no error.
	results, err := svc.BooksLookup(context.Background(), "the dispossessed")
	if err != nil {
		t.Fatalf("BooksLookup: %v, want nil (best-effort leg)", err)
	}
	if len(results) != 0 {
		t.Fatalf("results = %+v, want none", results)
	}
}

func TestGoogleBooksLeg_Capped(t *testing.T) {
	hits := make([]dom.BookSearchHit, 0, googleBooksLegCap+5)
	for i := 0; i < googleBooksLegCap+5; i++ {
		hits = append(hits, mt5GBHit("vol"+string(rune('a'+i)), "Book", "Author", 0))
	}
	gb := &mt5GB{enabled: true, hits: hits}
	svc := New(Deps{
		Repo:        newMemRepo(),
		Searcher:    &mt4Searcher{works: nil},
		GoogleBooks: gb,
	})

	results, err := svc.BooksLookup(context.Background(), "the dispossessed")
	if err != nil {
		t.Fatalf("BooksLookup: %v", err)
	}
	if len(results) != googleBooksLegCap {
		t.Fatalf("results = %d, want the cap (%d)", len(results), googleBooksLegCap)
	}
}

func TestGoogleBooksLeg_ProfileFiltersApplyToGBHits(t *testing.T) {
	// A tier-1 language filter drops a GB hit with a non-allowlisted
	// language (GB provides the language from volumeInfo; the filter is
	// the same one as for OL hits).
	gb := &mt5GB{enabled: true, hits: []dom.BookSearchHit{
		mt5GBHit("vol-eng", "The Dispossessed", "Ursula K. Le Guin", 1974, "eng"),
		mt5GBHit("vol-fra", "L'Étranger", "Camus", 1942, "fra"),
	}}
	svc := New(Deps{
		Repo:     newMemRepo(),
		Searcher: &mt4Searcher{works: nil},
		MetadataProfiles: mt4Profiles(mt4Profile("Eng only", settings.BookMetadataProfile{
			AllowedLanguages: []string{"eng"},
		})),
		GoogleBooks: gb,
	})

	results, err := svc.BooksLookupProfile(context.Background(), "query", "Eng only")
	if err != nil {
		t.Fatalf("BooksLookupProfile: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1 (the fr hit dropped): %+v", len(results), results)
	}
	if results[0].Name != "The Dispossessed" {
		t.Errorf("results[0] = %+v, want the eng GB hit", results[0])
	}
}

func TestGoogleBooksLeg_PopularityProxy(t *testing.T) {
	// GB hits have no indexed-edition count: the popularity proxy is
	// N/A, so they pass minimum_popularity ONLY when the minimum is 0.
	mkSvc := func(name string, min int) *Service {
		return New(Deps{
			Repo:     newMemRepo(),
			Searcher: &mt4Searcher{works: nil},
			MetadataProfiles: mt4Profiles(mt4Profile(name, settings.BookMetadataProfile{
				MinimumPopularity: min,
			})),
			GoogleBooks: &mt5GB{enabled: true, hits: []dom.BookSearchHit{
				mt5GBHit("vol1", "The Dispossessed", "Ursula K. Le Guin", 1974, "eng"),
			}},
		})
	}

	// Minimum 5: the GB hit (edition count 0) is dropped.
	results, err := mkSvc("Min5", 5).BooksLookupProfile(context.Background(), "query", "Min5")
	if err != nil {
		t.Fatalf("BooksLookupProfile: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("results = %+v, want none (GB hit fails the popularity minimum)", results)
	}

	// Minimum 0: the GB hit passes.
	results, err = mkSvc("Min0", 0).BooksLookupProfile(context.Background(), "query", "Min0")
	if err != nil {
		t.Fatalf("BooksLookupProfile: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %+v, want the GB hit (minimum 0 passes)", results)
	}
}
