package books

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	dom "github.com/brandenk514/mediarr/internal/domains/books"
	"github.com/brandenk514/mediarr/internal/settings"
)

// --- MT4 fakes ---

// mt4ProfileProvider is the test MetadataProfileProvider (a named set of
// book metadata profiles, no store).
type mt4ProfileProvider struct {
	profiles []settings.BookMetadataProfile
	err      error
}

func (p *mt4ProfileProvider) BookMetadataProfiles(ctx context.Context) ([]settings.BookMetadataProfile, error) {
	return p.profiles, p.err
}

func mt4Profiles(ps ...settings.BookMetadataProfile) *mt4ProfileProvider {
	return &mt4ProfileProvider{profiles: ps}
}

// mt4Searcher is a fake dom.BookSearcher that ALSO implements the
// workKeySearcher, isbnLookup and workDetailsFetcher surfaces, with
// per-method call counters (the tier-2 call/cap assertions).
type mt4Searcher struct {
	authors      []dom.BookSearchHit
	works        []dom.BookSearchHit
	isbnWorks    map[string][]dom.BookSearchHit
	workDetails  map[string]dom.WorkDetails
	keyWorks     map[string][]dom.BookSearchHit
	detailsErr   error
	authorCalls  int
	workCalls    int
	isbnCalls    int
	keyCalls     int
	detailsCalls int
}

func (s *mt4Searcher) SearchAuthors(ctx context.Context, q string) ([]dom.BookSearchHit, error) {
	s.authorCalls++
	return s.authors, nil
}

func (s *mt4Searcher) SearchWorks(ctx context.Context, author, title string) ([]dom.BookSearchHit, error) {
	s.workCalls++
	return s.works, nil
}

func (s *mt4Searcher) SearchWorkByISBN(ctx context.Context, isbn string) ([]dom.BookSearchHit, error) {
	s.isbnCalls++
	return s.isbnWorks[isbn], nil
}

func (s *mt4Searcher) SearchWorkByKey(ctx context.Context, key string) ([]dom.BookSearchHit, error) {
	s.keyCalls++
	return s.keyWorks[key], nil
}

func (s *mt4Searcher) GetWorkDetails(ctx context.Context, workKey string) (dom.WorkDetails, error) {
	s.detailsCalls++
	if s.detailsErr != nil {
		return dom.WorkDetails{}, s.detailsErr
	}
	return s.workDetails[workKey], nil
}

// mt4Profile builds a named profile for the tests.
func mt4Profile(name string, p settings.BookMetadataProfile) settings.BookMetadataProfile {
	p.ID = 1
	p.Name = name
	return p
}

// editionsJSON serializes editions for a store row's EditionsJSON.
func mt4EditionsJSON(t *testing.T, eds []dom.WorkEdition) []byte {
	t.Helper()
	b, err := json.Marshal(eds)
	if err != nil {
		t.Fatalf("marshal editions: %v", err)
	}
	return b
}

// --- MT4 profile resolution ---

// TestProfileFilters_UnknownName: an unknown profile name (and a lookup for
// a profile that does not exist in the live set) is
// ErrUnknownMetadataProfile — the API 400s.
func TestProfileFilters_UnknownName(t *testing.T) {
	srch := &mt4Searcher{works: []dom.BookSearchHit{{ID: "OL1W", Name: "X", Languages: []string{"eng"}}}}
	svc := New(Deps{
		Repo:             newMemRepo(),
		Searcher:         srch,
		MetadataProfiles: mt4Profiles(mt4Profile("Strict", settings.BookMetadataProfile{SkipNoISBNASIN: true})),
	})
	_, err := svc.BooksLookupProfile(context.Background(), "query", "NoSuchProfile")
	if !errors.Is(err, ErrUnknownMetadataProfile) {
		t.Fatalf("err = %v, want ErrUnknownMetadataProfile", err)
	}
	if _, err := svc.GetWorkEditionsProfile(context.Background(), "OL1W", "NoSuchProfile"); !errors.Is(err, ErrUnknownMetadataProfile) {
		t.Fatalf("editions err = %v, want ErrUnknownMetadataProfile", err)
	}
}

// TestProfileFilters_NoNameNoFiltering: an empty profile name resolves no
// filters at all — the provider is never consulted and nothing is
// filtered (the pre-MT4 behavior).
func TestProfileFilters_NoNameNoFiltering(t *testing.T) {
	// A provider that would blow up if consulted.
	srch := &mt4Searcher{works: []dom.BookSearchHit{{ID: "OL1W", Name: "X", Languages: nil}}}
	provider := &mt4ProfileProvider{err: errors.New("must not be called")}
	svc := New(Deps{Repo: newMemRepo(), Searcher: srch, MetadataProfiles: provider})

	results, err := svc.BooksLookupProfile(context.Background(), "query", "")
	if err != nil {
		t.Fatalf("BooksLookupProfile: %v", err)
	}
	if len(results) != 1 || results[0].Name != "X" {
		t.Fatalf("results = %+v, want the unfiltered hit", results)
	}
	if srch.detailsCalls != 0 {
		t.Errorf("detailsCalls = %d, want 0 (no filtering, no tier-2 fetch)", srch.detailsCalls)
	}
}

// --- MT4 lookup filtering ---

// TestBooksLookupProfile_LanguageFilter: a tier-1 allowed-languages filter
// drops a non-allowlisted work, keeps the allowlisted one, and NEVER
// touches author hits (Readarr profiles filter books, not authors). The
// dropped work earns no provenance row.
func TestBooksLookupProfile_LanguageFilter(t *testing.T) {
	srch := &mt4Searcher{
		authors: []dom.BookSearchHit{{ID: "", Name: "Ursula K. Le Guin"}},
		works: []dom.BookSearchHit{
			{ID: "OL1W", Name: "The Dispossessed", Author: "Ursula K. Le Guin", Year: 1974, Languages: []string{"eng"}},
			{ID: "OL2W", Name: "L'Étranger", Author: "Camus", Year: 1942, Languages: []string{"fra"}},
		},
	}
	store := newMT3Store()
	svc := New(Deps{
		Repo:             newMemRepo(),
		Searcher:         srch,
		MetadataStore:    store,
		MetadataProfiles: mt4Profiles(mt4Profile("Eng only", settings.BookMetadataProfile{AllowedLanguages: []string{"eng"}})),
	})

	results, err := svc.BooksLookupProfile(context.Background(), "query", "Eng only")
	if err != nil {
		t.Fatalf("BooksLookupProfile: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2 (1 author + 1 book): %+v", len(results), results)
	}
	// Author hit first, never filtered.
	if results[0].Kind != dom.LookupAuthor || results[0].Name != "Ursula K. Le Guin" {
		t.Errorf("results[0] = %+v, want the author hit (unfiltered)", results[0])
	}
	// The French book is dropped; the English one stays.
	if results[1].Kind != dom.LookupBook || results[1].Name != "The Dispossessed" {
		t.Errorf("results[1] = %+v, want the English book", results[1])
	}
	if _, ok, _ := store.GetWork(context.Background(), "OL2W"); ok {
		t.Error("the dropped work earned a provenance row (want none)")
	}
	if _, ok, _ := store.GetWork(context.Background(), "OL1W"); !ok {
		t.Error("the kept work has no provenance row")
	}
}

// TestBooksLookupProfile_Tier2ISBN: a tier-2 skip-no-ISBN/ASIN filter
// fetches the work documents of the survivors (only the survivors — a
// tier-1-dropped hit costs no fetch), drops the work whose editions carry
// no ISBN/ASIN, and keeps the identified one.
func TestBooksLookupProfile_Tier2ISBN(t *testing.T) {
	srch := &mt4Searcher{
		works: []dom.BookSearchHit{
			{ID: "OL1W", Name: "No IDs", Author: "A", Year: 2000, Languages: []string{"eng"}, EditionCount: 3},
			{ID: "OL2W", Name: "With ISBN", Author: "B", Year: 2001, Languages: []string{"eng"}, EditionCount: 3},
			{ID: "OL3W", Name: "Dropped by tier 1", Author: "C", Year: 2002, Languages: []string{"fra"}, EditionCount: 3},
		},
		workDetails: map[string]dom.WorkDetails{
			"OL1W": {WorkKey: "OL1W", Title: "No IDs", Editions: []dom.WorkEdition{{Key: "OL1M", Languages: []string{"eng"}}}},
			"OL2W": {WorkKey: "OL2W", Title: "With ISBN", Editions: []dom.WorkEdition{{Key: "OL2M", ISBN: "9780060559155"}}},
		},
	}
	store := newMT3Store()
	svc := New(Deps{
		Repo: newMemRepo(), Searcher: srch, MetadataStore: store,
		MetadataProfiles: mt4Profiles(
			mt4Profile("Strict", settings.BookMetadataProfile{SkipNoISBNASIN: true, AllowedLanguages: []string{"eng"}}),
		),
	})

	results, err := svc.BooksLookupProfile(context.Background(), "query", "Strict")
	if err != nil {
		t.Fatalf("BooksLookupProfile: %v", err)
	}
	if len(results) != 1 || results[0].Name != "With ISBN" {
		t.Fatalf("results = %+v, want only the identified work", results)
	}
	// Tier 2 fetched only the two tier-1 survivors (not the dropped one).
	if srch.detailsCalls != 2 {
		t.Errorf("detailsCalls = %d, want 2 (only tier-1 survivors; the dropped work costs no fetch)", srch.detailsCalls)
	}
	// The dropped works earn no rows; the kept work's row is the write-through.
	for _, key := range []string{"OL1W", "OL3W"} {
		if _, ok, _ := store.GetWork(context.Background(), key); ok {
			t.Errorf("%s earned a provenance row (want none)", key)
		}
	}
	if _, ok, _ := store.GetWork(context.Background(), "OL2W"); !ok {
		t.Error("OL2W (kept) has no provenance row")
	}
}

// TestBooksLookupProfile_PopularityProxy: a tier-1 minimum-popularity
// filter (the indexed-edition count proxy) drops a 1-edition work at
// minimum 2 and keeps a 3-edition one; no tier-2 fetch happens (the filter
// is tier 1).
func TestBooksLookupProfile_PopularityProxy(t *testing.T) {
	srch := &mt4Searcher{
		works: []dom.BookSearchHit{
			{ID: "OL1W", Name: "Rare", Author: "A", Year: 1990, Languages: []string{"eng"}, EditionCount: 1},
			{ID: "OL2W", Name: "Common", Author: "B", Year: 1991, Languages: []string{"eng"}, EditionCount: 3},
			{ID: "OL3W", Name: "Unknown Count", Author: "C", Year: 1992, Languages: []string{"eng"}, EditionCount: 0},
		},
	}
	svc := New(Deps{
		Repo:             newMemRepo(),
		Searcher:         srch,
		MetadataProfiles: mt4Profiles(mt4Profile("Popular", settings.BookMetadataProfile{MinimumPopularity: 2})),
	})

	results, err := svc.BooksLookupProfile(context.Background(), "query", "Popular")
	if err != nil {
		t.Fatalf("BooksLookupProfile: %v", err)
	}
	if len(results) != 1 || results[0].Name != "Common" {
		t.Fatalf("results = %+v, want only the 3-edition work (1 and unknown fail a minimum of 2)", results)
	}
	if srch.detailsCalls != 0 {
		t.Errorf("detailsCalls = %d, want 0 (tier-1 filter only)", srch.detailsCalls)
	}
}

// TestBooksLookupProfile_Tier2StoreReadThrough: a CACHED store row (with
// editions) serves the tier-2 facts with ZERO Open Library calls and ZERO
// store writes — the profile-filter read is read-only: it must not mint or
// refresh provenance rows.
func TestBooksLookupProfile_Tier2StoreReadThrough(t *testing.T) {
	srch := &mt4Searcher{}
	store := newMT3Store()
	row := mt3Row("OL1W", "The Dispossessed", "Ursula K. Le Guin", 1974, time.Now())
	row.EditionsJSON = mt4EditionsJSON(t, []dom.WorkEdition{{Key: "OL1M", ISBN: "9780060559155", Languages: []string{"eng"}, Year: 1974}})
	store.rows["openlibrary\x00OL1W"] = row

	svc := New(Deps{
		Repo: newMemRepo(), Searcher: srch, MetadataStore: store,
		MetadataProfiles: mt4Profiles(mt4Profile("Strict", settings.BookMetadataProfile{SkipNoISBNASIN: true})),
	})

	results, err := svc.BooksLookupProfile(context.Background(), "OL1W", "Strict")
	if err != nil {
		t.Fatalf("BooksLookupProfile: %v", err)
	}
	if len(results) != 1 || results[0].Name != "The Dispossessed" {
		t.Fatalf("results = %+v, want the identified work (its cached editions carry an ISBN)", results)
	}
	if srch.detailsCalls != 0 || srch.keyCalls != 0 {
		t.Errorf("provider calls = details:%d key:%d, want 0/0 (cached row, read-through)", srch.detailsCalls, srch.keyCalls)
	}
	if store.upserts != 0 {
		t.Errorf("upserts = %d, want 0 (a profile-filter read must NOT write the store)", store.upserts)
	}
}

// TestBooksLookupProfile_Tier2UncachedNoWrite: an UNCACHED work is fetched
// for its tier-2 facts, and the fetch itself writes no store row — only
// the normal MT3 write-through for PRESENTED hits writes. A hit the
// tier-2 filter drops earns no row.
func TestBooksLookupProfile_Tier2UncachedNoWrite(t *testing.T) {
	srch := &mt4Searcher{
		works: []dom.BookSearchHit{
			{ID: "OL1W", Name: "No IDs", Author: "A", Year: 2000, Languages: []string{"eng"}, EditionCount: 2},
			{ID: "OL2W", Name: "With ISBN", Author: "B", Year: 2001, Languages: []string{"eng"}, EditionCount: 2},
		},
		workDetails: map[string]dom.WorkDetails{
			"OL1W": {WorkKey: "OL1W", Editions: []dom.WorkEdition{{Key: "OL1M"}}},
			"OL2W": {WorkKey: "OL2W", Editions: []dom.WorkEdition{{Key: "OL2M", ISBN: "9780060559155"}}},
		},
	}
	store := newMT3Store()
	svc := New(Deps{
		Repo: newMemRepo(), Searcher: srch, MetadataStore: store,
		MetadataProfiles: mt4Profiles(mt4Profile("Strict", settings.BookMetadataProfile{SkipNoISBNASIN: true})),
	})

	results, err := svc.BooksLookupProfile(context.Background(), "query", "Strict")
	if err != nil {
		t.Fatalf("BooksLookupProfile: %v", err)
	}
	if len(results) != 1 || results[0].Name != "With ISBN" {
		t.Fatalf("results = %+v, want only the identified work", results)
	}
	if srch.detailsCalls != 2 {
		t.Errorf("detailsCalls = %d, want 2 (both uncached survivors fetched)", srch.detailsCalls)
	}
	// Only the PRESENTED hit (OL2W) has a row — the write-through, not the
	// filter read, wrote it. The dropped hit has none.
	if _, ok, _ := store.GetWork(context.Background(), "OL1W"); ok {
		t.Error("the dropped work earned a row (want none — the filter read is write-free)")
	}
	if _, ok, _ := store.GetWork(context.Background(), "OL2W"); !ok {
		t.Error("the presented work has no row (write-through)")
	}
}

// TestBooksLookupProfile_Tier2Cap: 30 hits with a tier-2 filter active
// trigger AT MOST 25 work-details fetches (the documented cap); hits
// beyond the cap are kept UNVERIFIED (best-effort filtering is not a gate).
func TestBooksLookupProfile_Tier2Cap(t *testing.T) {
	const n = 30
	works := make([]dom.BookSearchHit, 0, n)
	details := map[string]dom.WorkDetails{}
	for i := 1; i <= n; i++ {
		key := olKey(i)
		works = append(works, dom.BookSearchHit{ID: key, Name: "Book " + key, Author: "A", Year: 2000, Languages: []string{"eng"}, EditionCount: 5})
		details[key] = dom.WorkDetails{WorkKey: key, Editions: []dom.WorkEdition{{Key: "M" + key, ISBN: "9780060559155"}}}
	}
	srch := &mt4Searcher{works: works, workDetails: details}
	svc := New(Deps{
		Repo: newMemRepo(), Searcher: srch, // no store: the cap test is about OL traffic
		MetadataProfiles: mt4Profiles(mt4Profile("Strict", settings.BookMetadataProfile{SkipNoISBNASIN: true})),
	})

	results, err := svc.BooksLookupProfile(context.Background(), "query", "Strict")
	if err != nil {
		t.Fatalf("BooksLookupProfile: %v", err)
	}
	if srch.detailsCalls != tier2DetailFetchCap {
		t.Errorf("detailsCalls = %d, want %d (the documented cap)", srch.detailsCalls, tier2DetailFetchCap)
	}
	// The 25 verified works pass (they have ISBNs); the 5 beyond the cap
	// are kept unverified.
	if len(results) != n {
		t.Errorf("results = %d, want %d (verified survivors + unverified kept)", len(results), n)
	}
}

// TestBooksLookupProfile_Tier2UnverifiableKept: a hit whose work document
// cannot be read (a fetch failure with no store row) is KEPT — best-effort
// filtering is not a gate.
func TestBooksLookupProfile_Tier2UnverifiableKept(t *testing.T) {
	srch := &mt4Searcher{
		works:      []dom.BookSearchHit{{ID: "OL1W", Name: "X", Author: "A", Year: 2000, Languages: []string{"eng"}, EditionCount: 3}},
		detailsErr: errors.New("OL down"),
	}
	svc := New(Deps{
		Repo: newMemRepo(), Searcher: srch,
		MetadataProfiles: mt4Profiles(mt4Profile("Strict", settings.BookMetadataProfile{SkipNoISBNASIN: true})),
	})
	results, err := svc.BooksLookupProfile(context.Background(), "query", "Strict")
	if err != nil {
		t.Fatalf("BooksLookupProfile: %v", err)
	}
	if len(results) != 1 || results[0].Name != "X" {
		t.Fatalf("results = %+v, want the unverifiable hit kept", results)
	}
}

// --- MT4 editions endpoint filtering ---

// TestGetWorkEditionsProfile_PerEditionFilters: the per-edition subset
// (allowed-languages, skip-missing-release-date, skip-no-ISBN/ASIN) drops
// the ineligible editions; the work-level filters do not apply per edition.
// The store is write-throughed with the UNFILTERED editions (the cache is
// the source of truth; filtering is a read-time view).
func TestGetWorkEditionsProfile_PerEditionFilters(t *testing.T) {
	eds := []dom.WorkEdition{
		{Key: "E1", ISBN: "9780060559155", Year: 1974, Languages: []string{"eng"}}, // passes
		{Key: "E2", Year: 1974, Languages: []string{"eng"}},                        // no ISBN/ASIN
		{Key: "E3", ISBN: "9780060559156", Languages: []string{"eng"}},             // no year
		{Key: "E4", ISBN: "9780060559157", Year: 1974, Languages: []string{"fra"}}, // wrong language
	}
	srch := &mt4Searcher{workDetails: map[string]dom.WorkDetails{
		"OL1W": {WorkKey: "OL1W", Title: "The Dispossessed", FirstPublishYear: 1974, Editions: eds},
	}}
	store := newMT3Store()
	svc := New(Deps{
		Repo: newMemRepo(), Searcher: srch, MetadataStore: store,
		MetadataProfiles: mt4Profiles(mt4Profile("Strict", settings.BookMetadataProfile{
			SkipNoISBNASIN: true, SkipMissingReleaseDate: true, AllowedLanguages: []string{"eng"},
		})),
	})

	d, err := svc.GetWorkEditionsProfile(context.Background(), "OL1W", "Strict")
	if err != nil {
		t.Fatalf("GetWorkEditionsProfile: %v", err)
	}
	if len(d.Editions) != 1 || d.Editions[0].Key != "E1" {
		t.Fatalf("editions = %+v, want only E1 (language/year/ISBN per-edition filters)", d.Editions)
	}
	// The cache keeps the UNFILTERED set (the write-through is pre-filter).
	if _, ok, _ := store.GetWork(context.Background(), "OL1W"); !ok {
		t.Fatal("no row write-throughed")
	}
	// A second call is served from the fresh cached row (no re-fetch) and
	// applies the same filter.
	d2, err := svc.GetWorkEditionsProfile(context.Background(), "OL1W", "Strict")
	if err != nil {
		t.Fatalf("GetWorkEditionsProfile(2): %v", err)
	}
	if len(d2.Editions) != 1 || d2.Editions[0].Key != "E1" {
		t.Fatalf("editions(2) = %+v, want the filtered cached view", d2.Editions)
	}
	if srch.detailsCalls != 1 {
		t.Errorf("detailsCalls = %d, want 1 (second call served from cache)", srch.detailsCalls)
	}
}

// TestGetWorkEditionsProfile_NoProfileUnchanged: an empty profile name
// serves the work's editions unfiltered (the pre-MT2... pre-MT4 shape).
func TestGetWorkEditionsProfile_NoProfileUnchanged(t *testing.T) {
	eds := []dom.WorkEdition{
		{Key: "E1", ISBN: "9780060559155", Year: 1974, Languages: []string{"eng"}},
		{Key: "E2", Year: 1974, Languages: []string{"eng"}},            // no ISBN
		{Key: "E3", ISBN: "9780060559156", Languages: []string{"eng"}}, // no year
	}
	srch := &mt4Searcher{workDetails: map[string]dom.WorkDetails{
		"OL1W": {WorkKey: "OL1W", Editions: eds},
	}}
	svc := New(Deps{
		Repo: newMemRepo(), Searcher: srch,
		MetadataProfiles: mt4Profiles(mt4Profile("Strict", settings.BookMetadataProfile{SkipNoISBNASIN: true, SkipMissingReleaseDate: true})),
	})
	d, err := svc.GetWorkEditionsProfile(context.Background(), "OL1W", "")
	if err != nil {
		t.Fatalf("GetWorkEditionsProfile: %v", err)
	}
	if len(d.Editions) != 3 {
		t.Fatalf("editions = %+v, want all 3 (no profile = no filtering)", d.Editions)
	}
}

// TestGetWorkEditionsProfile_FilterDataIntact: the filtered editions keep
// the domain filter data (ASIN/Series) intact in the service output — the
// wire struct (the API layer) is what decides what is serialized; the
// e2e tests assert the wire shape has no new fields.
func TestGetWorkEditionsProfile_FilterDataIntact(t *testing.T) {
	eds := []dom.WorkEdition{{Key: "E1", ISBN: "9780060559155", Year: 1974, Languages: []string{"eng"}, ASIN: "B000S5MU0K", Series: []dom.SeriesRef{{Title: "S", Volume: "1"}}}}
	srch := &mt4Searcher{workDetails: map[string]dom.WorkDetails{
		"OL1W": {WorkKey: "OL1W", Editions: eds},
	}}
	svc := New(Deps{
		Repo: newMemRepo(), Searcher: srch,
		MetadataProfiles: mt4Profiles(mt4Profile("Strict", settings.BookMetadataProfile{SkipNoISBNASIN: true})),
	})
	d, err := svc.GetWorkEditionsProfile(context.Background(), "OL1W", "Strict")
	if err != nil {
		t.Fatalf("GetWorkEditionsProfile: %v", err)
	}
	if len(d.Editions) != 1 {
		t.Fatalf("editions = %+v, want E1 (it carries an ISBN)", d.Editions)
	}
	if d.Editions[0].ASIN != "B000S5MU0K" || len(d.Editions[0].Series) != 1 {
		t.Errorf("edition = %+v, want the filter data (ASIN/series) intact", d.Editions[0])
	}
}

// helper: build "OL{n}W" keys.
func olKey(i int) string {
	return fmt.Sprintf("OL%dW", i)
}
