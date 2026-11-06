package books

import (
	"context"
	"encoding/json"
	"time"

	dom "github.com/brandenk514/mediarr/internal/domains/books"
	"github.com/brandenk514/mediarr/internal/providers"
)

// workDetailsFetcher is the MT2 work-details fetch surface on top of
// dom.BookSearcher: it fetches the work document (title/authors/cover +
// editions) for a known OpenLibrary work key. *openLibrarySearcher
// implements it; a searcher without it (test fakes) degrades the editions
// path to the store-only view (nothing to fetch).
type workDetailsFetcher interface {
	GetWorkDetails(ctx context.Context, workKey string) (dom.WorkDetails, error)
}

// GetWorkEditions is the MT2 editions read path (the
// GET /api/v1/search/books/works/{workKey}/editions endpoint). The
// provenance store is consulted FIRST (the same TTL machinery as MT3): a
// fresh row serves its cached editions WITHOUT an Open Library call. A
// missing/expired row (or a store read failure) triggers a work-details
// fetch that write-throughs the row (the same TTL/refresh path — a refresh
// for the work key refreshes the editions too).
//
// Degradation (the add flow must never 500): a failed fetch with a (stale)
// store row serves the row's cached data; no searcher or fetch surface
// serves the store's view (not-configured when the store is empty too);
// nothing anywhere surfaces the fetch error (the API maps it to 502).
//
// (MT4) The optional named metadata profile is GetWorkEditionsProfile's
// profileName parameter; this entry point is the no-profile form (the
// pre-MT4 behavior, byte-identical).
func (s *Service) GetWorkEditions(ctx context.Context, workKey string) (dom.WorkDetails, error) {
	return s.GetWorkEditionsProfile(ctx, workKey, "")
}

// GetWorkEditionsProfile is GetWorkEditions with an optional named metadata
// profile (MT4): the named profile is resolved against the live book
// metadata profiles (an unknown name is ErrUnknownMetadataProfile, the API
// 400s), and the per-edition filter subset is applied to the work's
// editions: allowed-languages, skip-missing-release-date (the edition's
// publish year), and skip-no-ISBN/ASIN (the edition's own identifiers).
// The work-level filters (minimum-popularity, broken parts, secondary
// series) do NOT apply per edition — they decide whether the WORK is
// eligible, which the lookup already decided (see
// dom.ProfileFilters.PassEdition). profileName "" = no filtering.
func (s *Service) GetWorkEditionsProfile(ctx context.Context, workKey, profileName string) (dom.WorkDetails, error) {
	f, err := s.profileFilters(ctx, profileName)
	if err != nil {
		return dom.WorkDetails{}, err
	}
	key := providers.NormalizeOLID(workKey)
	if key == "" {
		return dom.WorkDetails{}, ErrWorkKeyRequired
	}

	// The provenance store first (the MT3 TTL cache): a fresh row serves its
	// cached editions without a provider call.
	var cached *dom.WorkMetadata
	if s.deps.MetadataStore != nil {
		if w, ok, err := s.deps.MetadataStore.GetWork(ctx, key); err == nil && ok {
			cached = &w
		}
		// Missing row (or a store error): fall through to the fetch.
	}
	if cached != nil && dom.IsFresh(cached.FetchedAt, time.Now(), s.metadataTTL()) {
		return s.applyEditionProfile(detailsFromRow(*cached), f), nil
	}

	// The fetch path needs a searcher with the work-details surface.
	var fetcher workDetailsFetcher
	ok := false
	if s.deps.Searcher != nil {
		fetcher, ok = s.deps.Searcher.(workDetailsFetcher)
	}
	if !ok {
		// Cannot fetch (no searcher, or a plain fake): a (stale) store row
		// beats nothing; otherwise not-configured (the API 503s).
		if cached != nil {
			return s.applyEditionProfile(detailsFromRow(*cached), f), nil
		}
		return dom.WorkDetails{}, ErrSearcherNotConfigured
	}

	details, err := fetcher.GetWorkDetails(ctx, key)
	if err != nil {
		// Degraded: a (stale) store row beats a failed fetch.
		if cached != nil {
			return s.applyEditionProfile(detailsFromRow(*cached), f), nil
		}
		return dom.WorkDetails{}, err
	}

	// Write-through (the MT3 single-source rule, extended by MT2): the row is
	// refreshed with the fetched editions — best-effort, it must not fail
	// the read.
	_ = s.upsertWorkDetails(ctx, details)
	return s.applyEditionProfile(details, f), nil
}

// applyEditionProfile applies the per-edition profile subset (MT4) to a
// work's editions: a nil filter is the no-op (pre-MT4 output, unchanged).
func (s *Service) applyEditionProfile(details dom.WorkDetails, f *dom.ProfileFilters) dom.WorkDetails {
	if f == nil {
		return details
	}
	details.Editions = s.filterEditions(details.Editions, f)
	if details.Editions == nil {
		details.Editions = []dom.WorkEdition{} // the wire shape serializes []
	}
	return details
}

// detailsFromRow materializes a WorkDetails from a provenance-store row: the
// row's work-level fields plus the row's cached editions (an empty row
// carries an empty list, not nil — the API serializes it as []).
func detailsFromRow(w dom.WorkMetadata) dom.WorkDetails {
	d := dom.WorkDetails{
		WorkKey:          w.WorkKey,
		Title:            w.Title,
		Authors:          []string{},
		CoverURL:         w.CoverURL,
		FirstPublishYear: w.Year,
		Editions:         []dom.WorkEdition{},
	}
	if w.Author != "" {
		d.Authors = []string{w.Author}
	}
	if len(w.EditionsJSON) > 0 {
		var eds []dom.WorkEdition
		if err := json.Unmarshal(w.EditionsJSON, &eds); err == nil {
			d.Editions = eds
		}
	}
	return d
}

// upsertWorkDetails upserts the provenance row for fetched work details (the
// MT2 write-through: the row's editions JSON is refreshed with the row, so a
// work-key refresh refreshes the editions too).
func (s *Service) upsertWorkDetails(ctx context.Context, d dom.WorkDetails) error {
	if s.deps.MetadataStore == nil {
		return nil
	}
	ed, err := json.Marshal(d.Editions)
	if err != nil {
		return err // WorkEdition is a flat struct; this cannot happen
	}
	author := ""
	if len(d.Authors) > 0 {
		author = d.Authors[0]
	}
	return s.deps.MetadataStore.UpsertWork(ctx, dom.WorkMetadata{
		WorkKey:      d.WorkKey,
		Source:       dom.SourceOpenLibrary,
		SourceID:     d.WorkKey,
		Title:        d.Title,
		Author:       author,
		Year:         d.FirstPublishYear,
		CoverURL:     d.CoverURL,
		FetchedAt:    time.Now(),
		EditionsJSON: ed,
	})
}
