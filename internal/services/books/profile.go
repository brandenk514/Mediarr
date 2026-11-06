package books

import (
	"context"
	"encoding/json"
	"fmt"

	dom "github.com/brandenk514/mediarr/internal/domains/books"
	"github.com/brandenk514/mediarr/internal/providers"
	"github.com/brandenk514/mediarr/internal/settings"
)

// ---------------------------------------------------------------------------
// MT4 (metadata profiles): the server-side filters applied to the lookup's
// book hits and the editions endpoint's output, resolved from the named
// book metadata profiles (the settings profiles section document).
//
// The two-tier design (see domains/books/profile.go): tier 1 reads the
// search-document facts already carried on each hit (languages, release
// year, indexed-edition count — the popularity proxy); tier 2 reads the
// work document per surviving hit (ISBN/ASIN, series membership), via the
// MT3 provenance store when cached (zero provider calls) or a capped
// details fetch otherwise.
// ---------------------------------------------------------------------------

// ErrUnknownMetadataProfile is returned when a lookup/editions request
// carries a profile name that is not in the live book metadata profiles
// (the API maps it to 400).
var ErrUnknownMetadataProfile = fmt.Errorf("books: unknown metadata profile")

// MetadataProfileProvider is the MT4 seam for the named metadata profiles:
// it returns the live book metadata profiles (the profiles section
// document's book_metadata_profiles array). The production wiring (main.go)
// adapts the settings store; a nil provider (tests without settings) falls
// back to the seeded defaults.
type MetadataProfileProvider interface {
	BookMetadataProfiles(ctx context.Context) ([]settings.BookMetadataProfile, error)
}

// settingsProfileProvider is the production MetadataProfileProvider: the
// profiles section document from the settings store, decoded over the
// section defaults (the same read as the settings section endpoint). An
// empty or missing book_metadata_profiles array (an empty stored document,
// or a document without the array) falls back to the seeded defaults.
type settingsProfileProvider struct {
	store settings.Store
}

// NewSettingsProfileProvider wraps a settings store as the MT4 profile
// provider (the live profiles section document).
func NewSettingsProfileProvider(store settings.Store) MetadataProfileProvider {
	return &settingsProfileProvider{store: store}
}

func (p *settingsProfileProvider) BookMetadataProfiles(ctx context.Context) ([]settings.BookMetadataProfile, error) {
	raw, err := p.store.GetSectionDocument(ctx, settings.SectionProfiles)
	if err != nil {
		return nil, err
	}
	if raw == nil {
		return settings.DefaultBookMetadataProfiles(), nil
	}
	doc, err := settings.SectionProfiles.DecodeDocument(raw)
	if err != nil {
		return nil, err
	}
	profiles := doc.(*settings.ProfilesSettings)
	if len(profiles.BookMetadataProfiles) == 0 {
		return settings.DefaultBookMetadataProfiles(), nil
	}
	return profiles.BookMetadataProfiles, nil
}

// profileFilters resolves the optional profile name of a lookup/editions
// request: "" (absent) = no filtering (nil, pre-MT4 behavior); a named
// profile is resolved against the live book metadata profiles (an unknown
// name is ErrUnknownMetadataProfile, the API 400s). A provider error
// (store failure) propagates — it is a server-side error, not a profile
// problem.
func (s *Service) profileFilters(ctx context.Context, name string) (*dom.ProfileFilters, error) {
	if name == "" {
		return nil, nil
	}
	var profiles []settings.BookMetadataProfile
	var err error
	if p := s.deps.MetadataProfiles; p != nil {
		profiles, err = p.BookMetadataProfiles(ctx)
	}
	if err != nil || len(profiles) == 0 {
		// No provider (tests) or a provider error: fall back to the seeded
		// defaults — "Default" stays resolvable, a custom name 400s.
		profiles = settings.DefaultBookMetadataProfiles()
	}
	for _, p := range profiles {
		if p.Name == name {
			return &dom.ProfileFilters{
				MinimumPopularity:      p.MinimumPopularity,
				SkipMissingReleaseDate: p.SkipMissingReleaseDate,
				SkipNoISBNASIN:         p.SkipNoISBNASIN,
				SkipBrokenParts:        p.SkipBrokenParts,
				SkipSecondarySeries:    p.SkipSecondarySeries,
				AllowedLanguages:       p.AllowedLanguages,
			}, nil
		}
	}
	return nil, fmt.Errorf("%w: %q", ErrUnknownMetadataProfile, name)
}

// tier2DetailFetchCap bounds the per-request Open Library work-details
// fetches a lookup triggers for tier-2 profile filtering (the documented
// cap: a text query can return many hits, and tier 2 needs one work
// document per hit — the cap keeps a single search from hammering keyless
// Open Library). Store-cached works are not counted against the cap (a
// cached read is a zero-provider-call read); hits beyond the cap are kept
// unverified (profile filtering is best-effort, not a gate — see
// filterWorkHits).
const tier2DetailFetchCap = 25

// filterWorkHits applies an MT4 profile to the lookup's BOOK hits (the
// two-tier filter of the domain). Author hits never reach here: Readarr
// metadata profiles filter BOOKS, and the author hits of a combined lookup
// are author search results, not books (documented in the spec).
//
// Tier 1 evaluates the search-document facts already on each hit (no
// provider call). Tier 2, only when active, reads the work document for
// each surviving hit via tier2Facts. A hit that tier 2 cannot verify
// (beyond the fetch cap, a non-OpenLibrary hit, a store row without
// cached editions and a failed/missing fetch, or no details surface) is
// KEPT: the filters are an enhancement over the keyless search, not a
// precondition for it — the same graceful-degradation convention as the
// Goodreads leg and the OL leg failures.
func (s *Service) filterWorkHits(ctx context.Context, hits []dom.BookSearchHit, f *dom.ProfileFilters) []dom.BookSearchHit {
	if f == nil || !f.Active() {
		return hits
	}
	var t1 []dom.BookSearchHit
	if f.Tier1Active() {
		t1 = make([]dom.BookSearchHit, 0, len(hits))
		for _, h := range hits {
			if f.PassTier1(dom.Tier1Facts{
				Languages:    h.Languages,
				ReleaseYear:  h.Year,
				EditionCount: h.EditionCount,
			}) {
				t1 = append(t1, h)
			}
		}
	} else {
		t1 = hits
	}
	if !f.Tier2Active() {
		return t1
	}
	out := make([]dom.BookSearchHit, 0, len(t1))
	capLeft := tier2DetailFetchCap
	for _, h := range t1 {
		key := providers.NormalizeOLID(h.ID)
		if !providers.IsOLWorkID(key) {
			// A non-OpenLibrary hit (the MT5 Google Books secondary leg)
			// has no work document to read: its tier-2 filters (ISBN/ASIN,
			// series) are unassessable, so the hit passes — documented in
			// the spec's profile parameter.
			out = append(out, h)
			continue
		}
		facts, ok := s.tier2Facts(ctx, key, &capLeft)
		if !ok {
			out = append(out, h) // unverifiable: keep (best-effort)
			continue
		}
		if f.PassTier2(facts) {
			out = append(out, h)
		}
	}
	return out
}

// tier2Facts reads the work-document facts for one hit's work key: the MT3
// provenance store first (a row with cached editions means ZERO Open
// Library calls), then a live details fetch through the searcher's
// work-details surface.
//
// READ-ONLY by design: a profile-filter read must NOT upsert the store.
// The MT3 store records the provenance of works the flow RESOLVED (the
// recordWorkMetadata write-through of the lookup legs and the
// upsertWorkDetails of the editions path); a search filter minting rows
// for works the user never picked would corrupt that provenance. A later
// explicit lookup/editions call for a work still upserts as usual.
//
// The boolean result is false when the facts are unobtainable (no store
// row and no/failed fetch, or the per-request fetch cap is exhausted) —
// the caller keeps the hit (see filterWorkHits).
func (s *Service) tier2Facts(ctx context.Context, key string, capLeft *int) (dom.Tier2Facts, bool) {
	if s.deps.MetadataStore != nil {
		if w, ok, err := s.deps.MetadataStore.GetWork(ctx, key); err == nil && ok && len(w.EditionsJSON) > 0 {
			var eds []dom.WorkEdition
			if json.Unmarshal(w.EditionsJSON, &eds) == nil && len(eds) > 0 {
				return dom.Tier2FactsFromEditions(eds), true
			}
		}
	}
	if capLeft == nil || *capLeft <= 0 {
		return dom.Tier2Facts{}, false
	}
	fetcher, ok := s.deps.Searcher.(workDetailsFetcher)
	if !ok {
		return dom.Tier2Facts{}, false
	}
	details, err := fetcher.GetWorkDetails(ctx, key)
	if err != nil {
		return dom.Tier2Facts{}, false
	}
	*capLeft--
	return dom.Tier2FactsFromEditions(details.Editions), true
}

// tier1FactsFromRowEditions derives the tier-1 facts of a cached store row
// from its cached editions JSON (the MT3 cached lookup path): the editions'
// language codes (union) and the edition count (the popularity-proxy
// value — the document's indexed editions; the search document's
// edition_count is the sharper value on the text path, but a row never
// stores it). nil/0 when the row has no cached editions (a
// search-hit-based row or a pre-MT2 row) — the filter then treats the
// facts as unknown (documented in the domain).
func tier1FactsFromRowEditions(editionsJSON []byte) ([]string, int) {
	if len(editionsJSON) == 0 {
		return nil, 0
	}
	var eds []dom.WorkEdition
	if json.Unmarshal(editionsJSON, &eds) != nil {
		return nil, 0
	}
	seen := make(map[string]bool, 8)
	var langs []string
	for _, e := range eds {
		for _, l := range e.Languages {
			if !seen[l] {
				seen[l] = true
				langs = append(langs, l)
			}
		}
	}
	return langs, len(eds)
}

// filterEditions applies the per-edition profile subset to a work's
// editions (the editions endpoint's output). Only the per-edition filters
// run: allowed-languages, skip-missing-release-date (the edition's publish
// year), skip-no-ISBN/ASIN (the edition's own identifiers). The work-level
// filters (minimum-popularity, broken parts, secondary series) do NOT
// apply per edition — they decide whether the WORK is eligible, which the
// lookup already decided (see dom.ProfileFilters.PassEdition).
func (s *Service) filterEditions(eds []dom.WorkEdition, f *dom.ProfileFilters) []dom.WorkEdition {
	if !f.Tier1Active() && !f.SkipNoISBNASIN {
		return eds
	}
	out := make([]dom.WorkEdition, 0, len(eds))
	for _, e := range eds {
		if f.PassEdition(e) {
			out = append(out, e)
		}
	}
	return out
}
