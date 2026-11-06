// Package books (services) is the use-case layer for the books domain. It
// orchestrates the full download pipeline — add author → add title/edition →
// want (at title or edition granularity) → search → e-book format match → pick
// best → download → import → satisfy — against the pure domain (domains/books)
// and the swappable adapter interfaces (indexers, downloads).
//
// It depends on interfaces, never on Postgres/Redis concretely, so the pipeline
// is testable end-to-end with fakes and a real Postgres repository alike (the
// same pattern as services/music, services/tv, and services/movies).
package books

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	dom "github.com/brandenk514/mediarr/internal/domains/books"
	"github.com/brandenk514/mediarr/internal/downloads"
	"github.com/brandenk514/mediarr/internal/indexers"
	"github.com/brandenk514/mediarr/internal/metrics"
	"github.com/brandenk514/mediarr/internal/paging"
	"github.com/brandenk514/mediarr/internal/providers"
	"github.com/brandenk514/mediarr/internal/queue"
	"github.com/brandenk514/mediarr/internal/store"
)

// ErrNoMatch is returned when no indexer release satisfies a wanted title's
// format profile.
var ErrNoMatch = fmt.Errorf("books: no release matched the format profile")

// ErrNoWanted is returned when an author has no pending wanted titles/editions
// to service (nothing to do).
var ErrNoWanted = fmt.Errorf("books: no pending wanted titles or editions")

// ErrAuthorNotFound, ErrTitleNotFound, ErrEditionNotFound, and
// ErrSeriesNotFound are re-exported from the pure domain so API/transport
// layers can map them to 404 without importing the domain directly.
var (
	ErrAuthorNotFound  = dom.ErrAuthorNotFound
	ErrTitleNotFound   = dom.ErrTitleNotFound
	ErrEditionNotFound = dom.ErrEditionNotFound
	ErrSeriesNotFound  = dom.ErrSeriesNotFound
)

// Deps bundles the Service's collaborators. Everything is an interface (or the
// pure domain), so the service is testable with a fake indexer, a mock download
// client, and either a real Postgres repo or an in-memory fake.
type Deps struct {
	// Repo persists authors, titles, editions, wanted, queue, and history.
	Repo dom.Repo
	// Indexers are searched (fan-out) for candidate releases.
	Indexers []indexers.Searcher
	// Client is the download client that fetches the chosen release.
	Client downloads.Client
	// MediaRoot is where imported files are placed (e.g. /media/books).
	MediaRoot string
	// DefaultProfile is used when a title has no profile set.
	DefaultProfile string
	// Events, when non-nil, receives queue.state and history pub/sub events as
	// the pipeline advances (#40 live dashboard). It is optional so the
	// pipeline runs without a bus (e.g. tests).
	Events store.Events
	// Metrics, when non-nil, observes pipeline runs and imports (M7 #46).
	// Nil-safe: the pipeline runs identically without it.
	Metrics *metrics.Metrics
	// Searcher, when non-nil, backs the combined plain-text books lookup
	// (Readarr B2). It is the keyless Open Library adapter in production; nil
	// (tests, no provider) makes the lookup answer a not-configured error the
	// API maps to 503. When it also implements workKeySearcher, a query that
	// is itself an OpenLibrary work key takes the MT3 cached path.
	Searcher dom.BookSearcher
	// MetadataStore, when non-nil, is the MT3 resolved-work provenance
	// store: the lookup consults it before Open Library (a fresh row is
	// served without a provider call), work resolutions upsert into it, and
	// the refresh command forces a re-fetch into it. Nil (tests without a
	// Postgres pool) = uncached lookup; refresh answers not-configured.
	MetadataStore dom.MetadataStore
	// MetadataTTL bounds how long a store row is fresh before a re-fetch
	// (MT3). <= 0 falls back to dom.DefaultMetadataTTL (7d).
	MetadataTTL time.Duration
	// GoodreadsResolver, when non-nil, backs the MT1 goodreads:<id> id
	// syntax: it resolves a Goodreads book id to its ISBN, which the
	// lookup chains into the OpenLibrary work (SearchWorkByISBN). It is
	// the keyless Goodreads page adapter in production. Nil (tests, no
	// provider) makes a goodreads: query degrade to an empty result —
	// a Goodreads outage never breaks the lookup.
	GoodreadsResolver GoodreadsResolver
	// MetadataProfiles, when non-nil, is the MT4 named metadata-profile
	// source: lookup/editions requests with profile=<name> resolve the
	// name against the live book metadata profiles (the settings profiles
	// section document). Nil (tests without settings) falls back to the
	// seeded defaults.
	MetadataProfiles MetadataProfileProvider
	// GoogleBooks, when non-nil AND configured, is the MT5 optional
	// Google Books secondary source: the combined plain-text lookup runs
	// it only when Open Library produced zero book hits for a
	// plain-text query (OL remains the source of truth; GB never
	// supplements, it fills the empty gap). Nil or not-configured =
	// leg disabled (skip, no error, no 503 — the lookup is unchanged).
	GoogleBooks GoogleBooksSearcher
}

// ErrSearcherNotConfigured is returned by BooksLookup when no metadata
// searcher is wired (the API maps it to 503, the keyless-adapter convention).
var ErrSearcherNotConfigured = fmt.Errorf("books: metadata searcher not configured")

// ErrMetadataStoreNotConfigured is returned by RefreshWorkMetadata when the
// MT3 provenance store is not wired (the API maps it to 503).
var ErrMetadataStoreNotConfigured = fmt.Errorf("books: metadata store not configured")

// ErrWorkNotFound is returned by RefreshWorkMetadata when Open Library has
// no work under the requested key (the API maps it to 404).
var ErrWorkNotFound = fmt.Errorf("books: work not found in openlibrary")

// ErrWorkKeyRequired is returned when a work-key operation gets an empty key
// (the API validates first and maps it to 400).
var ErrWorkKeyRequired = fmt.Errorf("books: work key is required")

// ErrInvalidAddParams is returned when an add/update request carries a value
// that is not one of the known enum values (Readarr B2); the API maps it to
// a 400.
var ErrInvalidAddParams = fmt.Errorf("books: invalid add-author parameter")

// Service implements the books use-cases.
type Service struct {
	deps Deps
}

// New builds a Service.
func New(deps Deps) *Service {
	if deps.DefaultProfile == "" {
		deps.DefaultProfile = "Best"
	}
	return &Service{deps: deps}
}

// AddAuthorInput is the add-author use-case input (Readarr B2 add modal).
// Omitted optional fields keep pre-B2 behavior: an empty monitor policy
// stores "" (= all), an empty quality/metadata profile stores "" (= none), an
// empty root folder uses the server default, and empty tags store none.
// StartSearch runs the pipeline best-effort after the add.
type AddAuthorInput struct {
	Name            string
	Monitored       bool
	MonitorPolicy   string   // PolicyAll/...; "" = all
	QualityProfile  string   // author's default book quality profile name; "" = none
	MetadataProfile string   // the author's metadata profile name; "" = none
	RootFolder      string   // root folder path; "" = server default
	Tags            []string // free-form tags; empty = none
	StartSearch     bool     // run the pipeline best-effort after the add
}

// AddAuthor creates an author (if new), records the add in history, and — with
// StartSearch — runs the pipeline best-effort: a pipeline failure is recorded
// in history and never fails the add (the author stays in the library, Radarr
// R2 / Readarr B2). Returns the author id.
//
// The monitor policy is validated against the known enum (Readarr B2); an
// unknown value returns ErrInvalidAddParams (the API maps it to 400).
func (s *Service) AddAuthor(ctx context.Context, in AddAuthorInput) (int64, error) {
	if strings.TrimSpace(in.Name) == "" {
		return 0, fmt.Errorf("books: author name is required")
	}
	if !dom.ValidMonitorPolicy(in.MonitorPolicy) {
		return 0, fmt.Errorf("%w: monitor_policy %q", ErrInvalidAddParams, in.MonitorPolicy)
	}
	a := dom.Author{
		Name:            strings.TrimSpace(in.Name),
		Monitored:       in.Monitored,
		MonitorPolicy:   in.MonitorPolicy,
		QualityProfile:  in.QualityProfile,
		MetadataProfile: in.MetadataProfile,
		RootFolder:      in.RootFolder,
		Tags:            in.Tags,
	}
	id, err := s.deps.Repo.CreateAuthor(ctx, a)
	if err != nil {
		return 0, err
	}
	s.record(ctx, id, "added", "author added to library")
	if in.StartSearch {
		// Best-effort pipeline run (B2): the add already succeeded, so a
		// failure here is only recorded — the author stays in the library.
		if _, perr := s.RunPipeline(ctx, id); perr != nil {
			s.record(ctx, id, "start-search-failed", perr.Error())
		}
	}
	return id, nil
}

// AddTitle creates a known title within an author (if new) and, when the title
// is monitored, ensures a pending whole-title wanted row for it. Returns the
// title id.
func (s *Service) AddTitle(ctx context.Context, authorID int64, name string, monitored bool) (int64, error) {
	if strings.TrimSpace(name) == "" {
		return 0, fmt.Errorf("books: title name is required")
	}
	title := dom.Title{
		AuthorID:  authorID,
		Name:      strings.TrimSpace(name),
		Monitored: monitored,
	}
	id, err := s.deps.Repo.CreateTitle(ctx, title)
	if err != nil {
		return 0, err
	}
	if monitored {
		if err := s.deps.Repo.EnsureWantedTitle(ctx, id); err != nil {
			return 0, err
		}
	}
	s.record(ctx, authorID, "title-added", name)
	return id, nil
}

// AddEdition creates a known edition within a title (if new) and, when the
// edition is monitored, ensures a pending edition-level wanted row for it.
// The edition is the (format, language, monitored) triple (Readarr B1);
// language may be "" (unknown). Returns the edition id.
func (s *Service) AddEdition(ctx context.Context, titleID int64, format, language, isbn, publisher string, year, pages int, monitored bool) (int64, error) {
	if err := dom.ValidateFormat(format); err != nil {
		return 0, err
	}
	e := dom.Edition{
		TitleID:   titleID,
		Format:    strings.ToLower(strings.TrimSpace(format)),
		Language:  strings.ToLower(strings.TrimSpace(language)),
		ISBN:      isbn,
		Publisher: publisher,
		Year:      year,
		Pages:     pages,
		Monitored: monitored,
	}
	id, err := s.deps.Repo.CreateEdition(ctx, e)
	if err != nil {
		return 0, err
	}
	if monitored {
		if err := s.deps.Repo.EnsureWantedEdition(ctx, titleID, id); err != nil {
			return 0, err
		}
	}
	return id, nil
}

// ListAuthors returns all authors (used by GET /api/v1/books).
func (s *Service) ListAuthors(ctx context.Context) ([]dom.Author, error) {
	return s.deps.Repo.ListAuthors(ctx)
}

// ListAuthorsQuery returns a server-side filtered and paginated page of
// authors (M6 #41). The books list view expands an author to its titles and
// editions lazily; the page envelope applies to the author level.
func (s *Service) ListAuthorsQuery(ctx context.Context, q paging.Query) (paging.Page[dom.Author], error) {
	all, err := s.deps.Repo.ListAuthors(ctx)
	if err != nil {
		return paging.Page[dom.Author]{}, err
	}
	return paging.Apply(all, q, func(a dom.Author) (string, bool) {
		return a.Name, a.Monitored
	}, nil), nil
}

// GetAuthor loads an author (used by GET /api/v1/books/{id}).
func (s *Service) GetAuthor(ctx context.Context, id int64) (*dom.Author, error) {
	return s.deps.Repo.GetAuthor(ctx, id)
}

// BooksLookup is the combined books lookup (Readarr B2, extended by MT1):
// an id-syntax query (isbn: / asin: / goodreads: / olid: and the
// unambiguous bare forms) routes to the matching id resolver; everything
// else is the combined plain-text lookup. It fans the plain-text path out
// to the metadata searcher's author search and work search and returns a
// single kind-tagged list (author hits first, then book hits). Each hit
// carries `added` (true when that author — kind=author — or that author +
// title — kind=book — already exists in the books library, so the UI
// navigates instead of re-adding) and `library_id` (the matching row id; 0
// when not added).
//
// One failing search leg degrades gracefully: the other leg's hits are still
// returned (best-effort, like the other keyless search endpoints). Both legs
// failing — or a nil searcher (no provider wired) — surfaces an error the API
// maps to 502/503.
//
// (MT4) The optional named metadata profile is BooksLookupProfile's
// profileName parameter; this entry point is the no-profile form (the
// pre-MT4 behavior, byte-identical).
func (s *Service) BooksLookup(ctx context.Context, q string) ([]dom.LookupResult, error) {
	return s.BooksLookupProfile(ctx, q, "")
}

// BooksLookupProfile is BooksLookup with an optional named metadata
// profile (MT4): the named profile is resolved against the live book
// metadata profiles (the settings profiles section document; an unknown
// name is ErrUnknownMetadataProfile, the API 400s), and the profile's
// filters are applied to the lookup's BOOK hits — two tiers, per the
// domain (search-document facts first; work-document data only for the
// survivors, via the MT3 store or a capped details fetch). Author hits
// are never filtered (Readarr metadata profiles filter books, not
// authors). profileName "" = no filtering (the pre-MT4 behavior).
func (s *Service) BooksLookupProfile(ctx context.Context, q, profileName string) ([]dom.LookupResult, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return []dom.LookupResult{}, nil
	}
	if s.deps.Searcher == nil {
		return nil, ErrSearcherNotConfigured
	}
	f, err := s.profileFilters(ctx, profileName)
	if err != nil {
		return nil, err
	}
	// MT1: id-lookup syntax routes to the id resolver instead of the
	// plain-text search (the documented scheme, in the spec's lookup
	// description).
	if idq, ok := dom.ParseIDQuery(q); ok {
		switch idq.Kind {
		case dom.IDKindOL:
			return s.lookupWorkByKey(ctx, providers.NormalizeOLID(idq.Value), f)
		case dom.IDKindISBN:
			return s.lookupIDWork(ctx, dom.IDKindISBN, idq.Value, f)
		case dom.IDKindASIN:
			return s.lookupIDWork(ctx, dom.IDKindASIN, idq.Value, f)
		case dom.IDKindGoodreads:
			return s.lookupGoodreads(ctx, idq.Value, f)
		}
	}
	// MT3: a query that is itself an OpenLibrary work key in the full key
	// form ("/works/OL…W" — the parser covers only the bare form) takes the
	// cached path: a fresh provenance-store row is served without an Open
	// Library call; an expired or missing row triggers a re-fetch that
	// upserts the row. (The store is consulted FIRST, so a repeated key
	// lookup never hammers keyless OL while the row is fresh.)
	if key := providers.NormalizeOLID(q); providers.IsOLWorkID(key) {
		return s.lookupWorkByKey(ctx, key, f)
	}
	return s.lookupText(ctx, q, f, true)
}

// lookupText is the Readarr B2 combined plain-text lookup: one query fans
// out to the metadata searcher's author search and work search and returns
// a single kind-tagged list (author hits first, then book hits). Each hit
// carries `added` (true when that author — kind=author — or that author +
// title — kind=book — already exists in the books library, so the UI
// navigates instead of re-adding) and `library_id` (the matching row id, 0
// when not added).
//
// One failing search leg degrades gracefully: the other leg's hits are
// still returned (best-effort, like the other keyless search endpoints).
// (MT3) Work hits are additionally upserted into the provenance store —
// the write-through that keeps it the single source for "was this resolved
// from Open Library, when, under what key". (MT4) A named profile filters
// the work hits BEFORE the write-through: a hit the profile drops is not
// presented, so it earns no provenance row (the store records resolved
// works, not filtered-out candidates).
func (s *Service) lookupText(ctx context.Context, q string, f *dom.ProfileFilters, plainText bool) ([]dom.LookupResult, error) {
	authorHits, aerr := s.deps.Searcher.SearchAuthors(ctx, q)
	workHits, werr := s.deps.Searcher.SearchWorks(ctx, "", q)
	if aerr != nil && werr != nil {
		return nil, fmt.Errorf("books: lookup: %w", aerr)
	}
	if f != nil {
		workHits = s.filterWorkHits(ctx, workHits, f)
	}
	// Best-effort write-through (MT3): a store failure must not fail the
	// lookup — the cache/provenance is an enhancement over the keyless
	// search, not a precondition for it. (MT5: the Google Books leg is
	// appended AFTER this, so its hits earn no provenance rows — the
	// store is OL provenance for the editions flow.)
	_ = s.recordWorkMetadata(ctx, workHits)

	// (MT5) Google Books is the secondary source: it fills a gap, never
	// supplements. A plain-text query (the `plainText` flag: id-syntax
	// paths that degrade to this text search must NOT run the leg — ids
	// are unambiguous and OL stays their source of truth) whose OL leg
	// produced zero book hits (after MT4 profile filtering) gets GB's
	// hits (capped, profile-filtered) appended. OL-with-hits + GB
	// configured => OL only.
	if plainText && len(workHits) == 0 {
		workHits = s.googleBooksLeg(ctx, q, f)
	}

	// Index the library for the `added` flag (case-insensitive, trimmed).
	authors, aerr2 := s.deps.Repo.ListAuthors(ctx)
	if aerr2 != nil {
		return nil, fmt.Errorf("books: lookup: list authors: %w", aerr2)
	}
	authorID := make(map[string]int64, len(authors))
	for _, a := range authors {
		authorID[foldName(a.Name)] = a.ID
	}

	out := make([]dom.LookupResult, 0, len(authorHits)+len(workHits))
	for _, h := range authorHits {
		id, ok := authorID[foldName(h.Name)]
		out = append(out, dom.LookupResult{
			Kind:      dom.LookupAuthor,
			Name:      h.Name,
			Added:     ok,
			LibraryID: id,
			AuthorID:  id,
		})
	}
	workResults, err := s.workLookupResults(ctx, workHits)
	if err != nil {
		return nil, err
	}
	return append(out, workResults...), nil
}

// foldName normalizes a name for case-insensitive library matching.
func foldName(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// ListTitles returns an author's known titles (used by
// GET /api/v1/books/{id}/titles).
func (s *Service) ListTitles(ctx context.Context, authorID int64) ([]dom.Title, error) {
	return s.deps.Repo.ListTitles(ctx, authorID)
}

// GetTitle loads a title (used by GET /api/v1/books/titles/{id}).
func (s *Service) GetTitle(ctx context.Context, id int64) (*dom.Title, error) {
	return s.deps.Repo.GetTitle(ctx, id)
}

// ListEditions returns a title's known editions (used by
// GET /api/v1/books/titles/{id}/editions).
func (s *Service) ListEditions(ctx context.Context, titleID int64) ([]dom.Edition, error) {
	return s.deps.Repo.ListEditions(ctx, titleID)
}

// GetEdition loads an edition (used by GET /api/v1/books/editions/{id}).
func (s *Service) GetEdition(ctx context.Context, id int64) (*dom.Edition, error) {
	return s.deps.Repo.GetEdition(ctx, id)
}

// ListWanted returns a title's wanted entries (title-level and edition-level)
// with their status (used by GET /api/v1/books/titles/{id}/wanted).
func (s *Service) ListWanted(ctx context.Context, titleID int64) ([]dom.Wanted, error) {
	return s.deps.Repo.ListWanted(ctx, titleID)
}

// GetAuthorHistory returns the author's recent history (newest first) — the
// books analogue of music's per-artist history (shared P3 surface).
func (s *Service) GetAuthorHistory(ctx context.Context, authorID int64, limit int) ([]dom.HistoryEntry, error) {
	if _, err := s.GetAuthor(ctx, authorID); err != nil {
		return nil, err
	}
	return s.deps.Repo.ListHistory(ctx, authorID, limit)
}

// ListBooks returns every book across all authors (the flat Books index) with
// each book's author name, sorted by author name then title name.
func (s *Service) ListBooks(ctx context.Context) ([]dom.Book, error) {
	return s.deps.Repo.ListBooks(ctx)
}

// GetBookFiles returns a title's imported e-book files with their media info
// (pages, size, format, language, added date), oldest first (used by
// GET /api/v1/books/titles/{id}/files).
func (s *Service) GetBookFiles(ctx context.Context, titleID int64) ([]dom.BookFile, error) {
	if _, err := s.GetTitle(ctx, titleID); err != nil {
		return nil, err
	}
	return s.deps.Repo.ListBookFilesByTitle(ctx, titleID)
}

// ListSeries returns all series (used by GET /api/v1/books/series).
func (s *Service) ListSeries(ctx context.Context) ([]dom.Series, error) {
	return s.deps.Repo.ListSeries(ctx)
}

// AddSeries creates a series by name (idempotent: an existing series returns
// its existing id). Returns the series id.
func (s *Service) AddSeries(ctx context.Context, name string) (int64, error) {
	id, err := s.deps.Repo.CreateSeries(ctx, dom.Series{Name: strings.TrimSpace(name)})
	if err != nil {
		return 0, err
	}
	return id, nil
}

// GetSeries loads a series (used by GET /api/v1/books/series/{id}).
func (s *Service) GetSeries(ctx context.Context, id int64) (*dom.Series, error) {
	return s.deps.Repo.GetSeries(ctx, id)
}

// ListSeriesBooks returns the books in a series (used by
// GET /api/v1/books/series/{id}/books).
func (s *Service) ListSeriesBooks(ctx context.Context, seriesID int64) ([]dom.SeriesBook, error) {
	return s.deps.Repo.ListSeriesBooks(ctx, seriesID)
}

// ListTitleSeries returns the series a book belongs to (used by
// GET /api/v1/books/titles/{id}/series).
func (s *Service) ListTitleSeries(ctx context.Context, titleID int64) ([]dom.Series, error) {
	if _, err := s.GetTitle(ctx, titleID); err != nil {
		return nil, err
	}
	return s.deps.Repo.ListTitleSeries(ctx, titleID)
}

// ListAuthorSeries returns the series the author's books belong to, with the
// member titles by this author (the author detail's series links).
func (s *Service) ListAuthorSeries(ctx context.Context, authorID int64) ([]dom.AuthorSeries, error) {
	if _, err := s.GetAuthor(ctx, authorID); err != nil {
		return nil, err
	}
	return s.deps.Repo.ListAuthorSeries(ctx, authorID)
}

// LinkTitleToSeries links a book to a series (idempotent). Both entities must
// exist. Used by the manual data entry until MT1's id-lookup joins them from
// OpenLibrary.
func (s *Service) LinkTitleToSeries(ctx context.Context, titleID, seriesID int64) error {
	if _, err := s.GetTitle(ctx, titleID); err != nil {
		return err
	}
	if err := s.deps.Repo.LinkTitleToSeries(ctx, titleID, seriesID); err != nil {
		return err
	}
	series, err := s.GetSeries(ctx, seriesID)
	if err != nil {
		return err
	}
	title, err := s.GetTitle(ctx, titleID)
	if err != nil {
		return err
	}
	s.record(ctx, title.AuthorID, "series-linked",
		fmt.Sprintf("%q linked to %q", title.Name, series.Name))
	return nil
}

// UpdateAuthorInput is the Edit Author modal input (Readarr B2, mirroring
// Lidarr L2's UpdateArtistInput). Pointer fields mean "apply this value"; nil
// means "keep the current value" — the pre-B2 monitored toggle sends only
// Monitored and must not clobber the other fields. When Monitored is applied
// and differs from the current state it fans out to the author's known titles
// (the PLAN §4 author-level toggle).
type UpdateAuthorInput struct {
	Monitored       *bool
	MonitorPolicy   *string
	QualityProfile  *string
	MetadataProfile *string
	RootFolder      *string
	Tags            *[]string
}

// UpdateAuthor applies the Edit Author modal fields to one author and, when
// the monitored state changed, propagates it to every known title (the PLAN
// §4 author-level fan-out). It returns the updated author (the API responds
// with the full row). A monitor policy change is validated; an unknown value
// returns ErrInvalidAddParams (the API maps it to 400).
func (s *Service) UpdateAuthor(ctx context.Context, id int64, in UpdateAuthorInput) (*dom.Author, error) {
	cur, err := s.deps.Repo.GetAuthor(ctx, id)
	if err != nil {
		return nil, err
	}
	a := *cur
	if in.MonitorPolicy != nil && !dom.ValidMonitorPolicy(*in.MonitorPolicy) {
		return nil, fmt.Errorf("%w: monitor_policy %q", ErrInvalidAddParams, *in.MonitorPolicy)
	}
	if in.Monitored != nil {
		a.Monitored = *in.Monitored
	}
	if in.MonitorPolicy != nil {
		a.MonitorPolicy = *in.MonitorPolicy
	}
	if in.QualityProfile != nil {
		a.QualityProfile = *in.QualityProfile
	}
	if in.MetadataProfile != nil {
		a.MetadataProfile = *in.MetadataProfile
	}
	if in.RootFolder != nil {
		a.RootFolder = *in.RootFolder
	}
	if in.Tags != nil {
		a.Tags = *in.Tags
	}
	if err := s.deps.Repo.UpdateAuthor(ctx, a); err != nil {
		return nil, err
	}
	if in.Monitored != nil && *in.Monitored != cur.Monitored {
		if err := s.SetAuthorMonitored(ctx, id, *in.Monitored); err != nil {
			return nil, err
		}
	}
	return &a, nil
}

// SetAuthorMonitored toggles the author-level monitor (PLAN §4) and propagates
// it to every known title of the author, ensuring a pending whole-title wanted
// row for each on toggle-on. The wanted rows are the pipeline's work queue.
func (s *Service) SetAuthorMonitored(ctx context.Context, authorID int64, monitored bool) error {
	if err := s.deps.Repo.SetAuthorMonitored(ctx, authorID, monitored); err != nil {
		return err
	}
	titles, err := s.deps.Repo.ListTitles(ctx, authorID)
	if err != nil {
		return err
	}
	for _, t := range titles {
		if err := s.SetTitleMonitored(ctx, t.ID, monitored); err != nil {
			return err
		}
	}
	event := "monitored"
	if !monitored {
		event = "unmonitored"
	}
	s.record(ctx, authorID, event, "author-level toggle")
	return nil
}

// SetTitleMonitored toggles a single title's monitoring; when turning on, it
// ensures a pending whole-title wanted row (so a re-monitored title is wanted
// again).
func (s *Service) SetTitleMonitored(ctx context.Context, titleID int64, monitored bool) error {
	if err := s.deps.Repo.SetTitleMonitored(ctx, titleID, monitored); err != nil {
		return err
	}
	if monitored {
		return s.deps.Repo.EnsureWantedTitle(ctx, titleID)
	}
	return nil
}

// SetEditionMonitored toggles a single edition's monitoring; when turning on,
// it ensures a pending edition-level wanted row.
func (s *Service) SetEditionMonitored(ctx context.Context, titleID, editionID int64, monitored bool) error {
	if err := s.deps.Repo.SetEditionMonitored(ctx, editionID, monitored); err != nil {
		return err
	}
	if !monitored {
		return nil
	}
	return s.deps.Repo.EnsureWantedEdition(ctx, titleID, editionID)
}

// RunPipeline drives the full pipeline for one author: it services every
// pending wanted title/edition by searching indexers, matching the best e-book
// release for the title's format profile, downloading it, importing it under
// the media root, and marking the wanted entries satisfied.
//
// A whole-title release satisfies the title-level wanted and every edition-
// level wanted within that title (the file delivers the title in the requested
// format). The pipeline is idempotent: wanted entries already satisfied are
// skipped, and an author with no pending wanted entries returns ErrNoWanted
// without searching.
func (s *Service) RunPipeline(ctx context.Context, authorID int64) (imported int, err error) {
	start := time.Now()
	defer func() {
		result := metrics.ResultOK
		if err != nil {
			result = metrics.ResultError
		}
		s.deps.Metrics.RecordPipeline(string(queue.KindBook), result, time.Since(start), imported)
	}()

	author, err := s.deps.Repo.GetAuthor(ctx, authorID)
	if err != nil {
		return 0, err
	}

	// Collect the pending wanted per title.
	titles, err := s.deps.Repo.ListTitles(ctx, authorID)
	if err != nil {
		return 0, err
	}
	type titleWork struct {
		title   dom.Title
		pending []dom.Wanted
	}
	var work []titleWork
	for _, t := range titles {
		wants, err := s.deps.Repo.ListWanted(ctx, t.ID)
		if err != nil {
			return 0, err
		}
		var pending []dom.Wanted
		for _, w := range wants {
			if w.Status == dom.WantedPending {
				pending = append(pending, w)
			}
		}
		if len(pending) > 0 {
			work = append(work, titleWork{title: t, pending: pending})
		}
	}
	if len(work) == 0 {
		return 0, ErrNoWanted
	}

	if s.deps.Client == nil {
		s.record(ctx, authorID, "no-client", "no download client configured")
		return 0, fmt.Errorf("books: no download client configured")
	}

	// 1. Search (once for the author).
	results, err := s.searchAll(ctx, author.Name)
	if err != nil {
		s.record(ctx, authorID, "search", "search failed: "+err.Error())
		return 0, err
	}
	s.record(ctx, authorID, "searched", fmt.Sprintf("%d candidate release(s)", len(results)))

	// 2. Service each title that has pending wanted entries.
	for _, w := range work {
		profile := s.deps.DefaultProfile
		cands := s.buildCandidates(author, w.title, profile, results)
		best, ok := dom.BestForTitle(cands, author.Name, w.title.Name)
		if !ok {
			s.record(ctx, authorID, "no-match",
				fmt.Sprintf("no release satisfied %q (%s)", w.title.Name, profile))
			continue
		}
		n, err := s.serviceTitle(ctx, author, w.title, best, w.pending)
		if err != nil {
			s.record(ctx, authorID, "import-failed", err.Error())
			continue
		}
		imported += n
	}
	if imported == 0 {
		return 0, ErrNoMatch
	}
	return imported, nil
}

// searchAll fans out to every indexer concurrently (via the shared M5 Fanout),
// merges the results, and dedupes by release title (first indexer wins). The
// domain pickers re-score and re-order the candidates, so the merged order
// does not affect which release is chosen; the Fanout adds concurrency, a
// per-indexer timeout, and (when the indexers are wrapped in a CacheSearcher)
// a Redis result cache — see PLAN §5 and #30.
func (s *Service) searchAll(ctx context.Context, term string) ([]indexers.SearchResult, error) {
	q := indexers.SearchQuery{Term: term}
	f := indexers.NewFanout(s.deps.Indexers...)
	return f.Search(ctx, q)
}

// buildCandidates parses and format-matches every indexer result against the
// title's profile, dropping releases that do not parse or satisfy it.
func (s *Service) buildCandidates(author *dom.Author, title dom.Title, profile string, results []indexers.SearchResult) []dom.CandidateRelease {
	var cands []dom.CandidateRelease
	for _, r := range results {
		c, ok := dom.BuildCandidate(r.Title, r.Indexer, profile)
		if !ok {
			continue
		}
		cands = append(cands, c)
	}
	return cands
}

// serviceTitle downloads the best release once, imports it under the media
// root, records the imported book file, and satisfies every pending wanted
// entry the release delivers (the title level plus any edition-level wanted
// within the title). Returns the number of wanted entries satisfied.
func (s *Service) serviceTitle(ctx context.Context, author *dom.Author, title dom.Title, rel dom.CandidateRelease, pending []dom.Wanted) (int, error) {
	// Download once.
	queueID, err := s.sendToClient(ctx, author, title, rel)
	if err != nil {
		return 0, err
	}

	// Import the e-book file.
	destPath, err := s.importTitle(ctx, author, title, rel, queueID)
	if err != nil {
		return 0, err
	}

	// Record the imported file and its media info (Readarr B1 book files).
	// A failure here does not fail the import: the file is in place; only the
	// read surface is degraded.
	s.recordBookFile(ctx, author, title, rel, destPath)

	// Satisfy every pending wanted the release delivers.
	satisfied := 0
	for _, w := range pending {
		if err := s.deps.Repo.MarkWantedSatisfied(ctx, w.TitleID, w.EditionID, rel.Title); err != nil {
			s.record(ctx, author.ID, "satisfy-failed", err.Error())
			continue
		}
		satisfied++
	}
	s.record(ctx, author.ID, "imported",
		fmt.Sprintf("imported %q satisfying %d wanted", rel.Title, satisfied))
	return satisfied, nil
}

// sendToClient queues the release, sends it to the download client, and records
// the queue entry. Returns the queue entry id.
func (s *Service) sendToClient(ctx context.Context, author *dom.Author, title dom.Title, rel dom.CandidateRelease) (int64, error) {
	qe := dom.QueueEntry{
		AuthorID:       author.ID,
		TitleID:        title.ID,
		ReleaseTitle:   rel.Title,
		Indexer:        rel.Indexer,
		DownloadClient: s.deps.Client.Name(),
		State:          dom.QueueQueued,
		Progress:       0,
	}
	id, err := s.deps.Repo.CreateQueue(ctx, qe)
	if err != nil {
		return 0, err
	}
	qe.ID = id
	s.emitQueue(ctx, qe)
	qe.State = dom.QueueDownloading
	_ = s.deps.Repo.UpdateQueue(ctx, qe)
	s.emitQueue(ctx, qe)

	if err := s.deps.Client.Add(ctx, downloads.Release{
		Title:    rel.Title,
		FileName: fileNameFor(title, rel),
	}); err != nil {
		qe.State = dom.QueueFailed
		qe.Progress = 0
		_ = s.deps.Repo.UpdateQueue(ctx, qe)
		s.emitQueue(ctx, qe)
		return 0, err
	}
	return id, nil
}

// importTitle polls the client until the release is complete and imports the
// e-book file under the media root, marking the queue complete. Returns the
// destination path of the imported file (for book-file recording).
func (s *Service) importTitle(ctx context.Context, author *dom.Author, title dom.Title, rel dom.CandidateRelease, queueID int64) (string, error) {
	file, err := s.waitComplete(ctx, rel.Title)
	if err != nil {
		return "", err
	}
	if file == "" {
		return "", fmt.Errorf("books: download did not produce a file for %q", rel.Title)
	}

	plan, err := dom.PlanImport(s.deps.MediaRoot, author.Name, title.Name, file)
	if err != nil {
		if !errors.Is(err, dom.ErrCollision) {
			return "", err
		}
		// The file already exists; reuse its destination in place.
		dest, derr := dom.DestPath(s.deps.MediaRoot, author.Name, title.Name, file)
		if derr != nil {
			return "", derr
		}
		plan = &dom.Import{DestPath: dest}
	}

	// Copy the file into place unless it is already there.
	if _, statErr := os.Stat(plan.DestPath); os.IsNotExist(statErr) {
		if err := copyFile(plan.SourcePath, plan.DestPath); err != nil {
			return "", err
		}
	}

	// Remove the source file now that it has been imported.
	_ = os.Remove(file)

	qe := dom.QueueEntry{ID: queueID, State: dom.QueueComplete, Progress: 100}
	if err := s.deps.Repo.UpdateQueue(ctx, qe); err != nil {
		return "", err
	}
	s.emitQueue(ctx, qe)
	return plan.DestPath, nil
}

// recordBookFile records the imported file as a book file with its media info
// (Readarr B1). The file is tied to the edition whose format matches the
// release when one exists (edition_id 0 otherwise); the file's size comes
// from disk and its page count falls back to the matching edition's known
// page count. CreateBookFile is idempotent on (title_id, path), so a
// re-import updates the row in place.
func (s *Service) recordBookFile(ctx context.Context, author *dom.Author, title dom.Title, rel dom.CandidateRelease, destPath string) {
	size := int64(0)
	if st, err := os.Stat(destPath); err == nil {
		size = st.Size()
	}
	var editionID int64
	var pages int
	var lang string
	if editions, err := s.deps.Repo.ListEditions(ctx, title.ID); err == nil {
		for _, e := range editions {
			if strings.EqualFold(e.Format, rel.Format.Format) {
				editionID = e.ID
				pages = e.Pages
				lang = e.Language
				break
			}
		}
	}
	bf := dom.BookFile{
		TitleID:   title.ID,
		EditionID: editionID,
		Path:      filepath.Base(destPath),
		Format:    strings.ToLower(rel.Format.Format),
		Size:      size,
		Pages:     pages,
		Language:  lang,
	}
	if _, err := s.deps.Repo.CreateBookFile(ctx, bf); err != nil {
		s.record(ctx, author.ID, "book-file-failed", err.Error())
		return
	}
	s.record(ctx, author.ID, "book-file-added", bf.Path)
}

// waitComplete polls the download client until the release reports complete
// (bounded by a deadline) and returns the file path.
func (s *Service) waitComplete(ctx context.Context, title string) (string, error) {
	deadline := time.Now().Add(30 * time.Second)
	for {
		st, err := s.deps.Client.Status(ctx, title)
		if err != nil {
			return "", err
		}
		if st.Complete && st.File != "" {
			return st.File, nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("books: timed out waiting for download of %q", title)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// emitQueue publishes a "queue.updated" pub/sub event for a queue entry. It is
// a no-op when no bus is configured and never fails the pipeline: a dead Redis
// must not stop a download, so publish errors are deliberately ignored here
// (the queue table is the source of truth; the bus is a best-effort push).
func (s *Service) emitQueue(ctx context.Context, qe dom.QueueEntry) {
	if s.deps.Events == nil {
		return
	}
	payload, err := json.Marshal(map[string]any{
		"author_id":     qe.AuthorID,
		"title_id":      qe.TitleID,
		"queue_id":      qe.ID,
		"release_title": qe.ReleaseTitle,
		"indexer":       qe.Indexer,
		"client":        qe.DownloadClient,
		"state":         qe.State,
		"progress":      qe.Progress,
	})
	if err != nil {
		return
	}
	_ = s.deps.Events.Publish(ctx, store.Event{Type: "queue.updated", Payload: payload})
}

// record appends a history entry, ignoring errors (history is best-effort),
// and emits a "history.appended" pub/sub event so the live dashboard's history
// feed refreshes (the queue is the source of truth; the bus is best-effort).
func (s *Service) record(ctx context.Context, authorID int64, event, detail string) {
	_ = s.deps.Repo.AddHistory(ctx, dom.HistoryEntry{
		AuthorID: authorID, Event: event, Detail: detail,
	})
	if s.deps.Events == nil {
		return
	}
	payload, err := json.Marshal(map[string]any{
		"author_id": authorID,
		"event":     event,
		"detail":    detail,
	})
	if err != nil {
		return
	}
	_ = s.deps.Events.Publish(ctx, store.Event{Type: "history.appended", Payload: payload})
}

// fileNameFor derives a safe download-client file name from a title and a
// parsed release: a cleaned title name plus the e-book extension inferred from
// the release's format.
func fileNameFor(title dom.Title, rel dom.CandidateRelease) string {
	name := title.Name
	if name == "" {
		name = rel.Title
	}
	if name == "" {
		name = "book"
	}
	name = strings.ReplaceAll(name, ".", " ")
	name = strings.ReplaceAll(name, "-", " ")
	name = strings.Join(strings.Fields(name), " ")
	return strings.ToLower(name) + extForFormat(rel.Format.Format)
}

func extForFormat(format string) string {
	switch format {
	case dom.FormatEPUB:
		return ".epub"
	case dom.FormatMOBI:
		return ".mobi"
	case dom.FormatAZW3:
		return ".azw3"
	default:
		return ".epub"
	}
}

// copyFile copies src to dst, creating dst's directory as needed.
func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil { //nolint:gosec // media tree dirs must stay group/world-readable for media-server users (Plex, etc.)
		return err
	}
	data, err := os.ReadFile(src) //nolint:gosec // src is the download client's output path, built by the pipeline
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644) //nolint:gosec // media files must be world-readable for media-server users
}
