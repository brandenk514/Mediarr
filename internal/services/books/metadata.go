package books

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	dom "github.com/brandenk514/mediarr/internal/domains/books"
	"github.com/brandenk514/mediarr/internal/providers"
)

// workKeySearcher is the MT3 key-based work lookup surface the combined
// books lookup needs on top of dom.BookSearcher: it resolves a known
// OpenLibrary work key (the cached lookup path and the refresh command).
// *openLibrarySearcher implements it; a plain fake searcher without it makes
// the service degrade to the plain-text path (pre-MT3 behavior).
type workKeySearcher interface {
	SearchWorkByKey(ctx context.Context, key string) ([]dom.BookSearchHit, error)
}

// metadataTTL is the MT3 cache TTL: the configured positive value, or the
// domain default (7d) when unset/invalid.
func (s *Service) metadataTTL() time.Duration {
	if s.deps.MetadataTTL > 0 {
		return s.deps.MetadataTTL
	}
	return dom.DefaultMetadataTTL
}

// lookupWorkByKey is the MT3 cached work-key lookup: the provenance store is
// consulted FIRST — a fresh row (within the TTL) is served without an Open
// Library call; an expired or missing row triggers a re-fetch through the
// searcher's key lookup that upserts the row (re-anchoring fetched_at). A
// store read failure degrades to the re-fetch path (the cache must never
// break the lookup); a searcher without a key lookup surface degrades to
// the plain-text path.
//
// (MT4) A named profile (non-nil f) filters the resulting book hits; a
// store-served row's tier-1 facts come from the row's cached editions
// (zero provider calls) — a row without cached editions carries unknown
// facts, and the filters treat unknown as "cannot verify" per the domain
// rules (e.g. a non-empty language allowlist drops an unknown-language
// hit).
func (s *Service) lookupWorkByKey(ctx context.Context, key string, f *dom.ProfileFilters) ([]dom.LookupResult, error) {
	if s.deps.MetadataStore != nil {
		if w, ok, err := s.deps.MetadataStore.GetWork(ctx, key); err == nil && ok {
			if dom.IsFresh(w.FetchedAt, time.Now(), s.metadataTTL()) {
				hit := dom.BookSearchHit{
					ID: w.WorkKey, Name: w.Title, Author: w.Author,
					Year: w.Year, CoverURL: w.CoverURL,
				}
				// MT4: derive the tier-1 facts from the row's cached
				// editions when present (the work document's data, no
				// provider call) — the filtered view stays stable across
				// the cache boundary.
				hit.Languages, hit.EditionCount = tier1FactsFromRowEditions(w.EditionsJSON)
				hits := []dom.BookSearchHit{hit}
				if f != nil {
					hits = s.filterWorkHits(ctx, hits, f)
				}
				return s.workLookupResults(ctx, hits)
			}
		}
		// Missing row (or a store error): fall through to the re-fetch.
	}
	ks, ok := s.deps.Searcher.(workKeySearcher)
	if !ok {
		return s.lookupText(ctx, key, f)
	}
	hits, err := ks.SearchWorkByKey(ctx, key)
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

// workLookupResults turns work hits into kind=book LookupResults, computing
// the `added` flag and the library ids against the books library
// (case-insensitive, trimmed — the same matching as the B2 lookup). Every
// caller (lookupText, lookupWorkByKey, lookupIDWork, lookupGoodreads)
// routes its work hits through here, so this is the single place the
// MT2 work key is set: the hit's ID is the OpenLibrary work key the add
// flow uses to fetch editions.
func (s *Service) workLookupResults(ctx context.Context, workHits []dom.BookSearchHit) ([]dom.LookupResult, error) {
	books, berr := s.deps.Repo.ListBooks(ctx)
	if berr != nil {
		return nil, fmt.Errorf("books: lookup: list books: %w", berr)
	}
	titleID := make(map[string]int64, len(books))
	titleAuthor := make(map[string]int64, len(books))
	for _, b := range books {
		titleID[foldName(b.AuthorName)+"\x00"+foldName(b.Name)] = b.ID
		titleAuthor[foldName(b.AuthorName)+"\x00"+foldName(b.Name)] = b.AuthorID
	}

	out := make([]dom.LookupResult, 0, len(workHits))
	for _, h := range workHits {
		aid, ok := titleID[foldName(h.Author)+"\x00"+foldName(h.Name)]
		out = append(out, dom.LookupResult{
			Kind:      dom.LookupBook,
			Name:      h.Name,
			Author:    h.Author,
			Year:      h.Year,
			WorkKey:   h.ID, // the OL work key (the hit's ID) — MT2 editions fetch
			Added:     ok,
			LibraryID: aid,
			AuthorID:  titleAuthor[foldName(h.Author)+"\x00"+foldName(h.Name)],
		})
	}
	return out, nil
}

// recordWorkMetadata upserts work hits into the provenance store (the MT3
// write-through that keeps the store the single source for "was this
// resolved from Open Library, when, under what key"). Hits without a
// well-formed work key are skipped. Callers treat a returned error as
// best-effort for the lookup (see lookupText) — the cache is an
// enhancement, not a precondition.
func (s *Service) recordWorkMetadata(ctx context.Context, hits []dom.BookSearchHit) error {
	if s.deps.MetadataStore == nil {
		return nil
	}
	now := time.Now()
	for _, h := range hits {
		key := providers.NormalizeOLID(h.ID)
		if !providers.IsOLWorkID(key) {
			continue
		}
		w := dom.WorkMetadata{
			WorkKey:   key,
			Source:    dom.SourceOpenLibrary,
			SourceID:  key,
			Title:     h.Name,
			Author:    h.Author,
			Year:      h.Year,
			CoverURL:  h.CoverURL,
			FetchedAt: now,
		}
		if err := s.deps.MetadataStore.UpsertWork(ctx, w); err != nil {
			return err
		}
	}
	return nil
}

// RefreshWorkMetadata is the MT3 refresh command (extended by MT2): it
// forces a re-fetch of one work from Open Library, bypassing the TTL,
// upserts the provenance row (re-anchoring fetched_at), and returns the
// refreshed row. key may be bare ("OL8189755W") or a full key
// ("/works/OL8189755W"); it is normalized first. It requires BOTH a
// searcher (with the key lookup surface) and the provenance store — a
// refresh is a store operation by definition.
//
// MT2: when the searcher also has the work-details surface, the refresh
// fetches the work document and stores its editions on the row (a work-key
// refresh refreshes the editions too). The editions leg is best-effort: a
// failed details fetch keeps the search-hit-based row and does not fail the
// refresh.
func (s *Service) RefreshWorkMetadata(ctx context.Context, key string) (dom.WorkMetadata, error) {
	key = providers.NormalizeOLID(key)
	if key == "" {
		return dom.WorkMetadata{}, ErrWorkKeyRequired
	}
	if s.deps.Searcher == nil {
		return dom.WorkMetadata{}, ErrSearcherNotConfigured
	}
	if s.deps.MetadataStore == nil {
		return dom.WorkMetadata{}, ErrMetadataStoreNotConfigured
	}
	ks, ok := s.deps.Searcher.(workKeySearcher)
	if !ok {
		return dom.WorkMetadata{}, ErrSearcherNotConfigured
	}
	// Forced: the TTL is deliberately bypassed, so even a fresh row is
	// re-fetched from Open Library.
	hits, err := ks.SearchWorkByKey(ctx, key)
	if err != nil {
		return dom.WorkMetadata{}, err
	}
	if len(hits) == 0 {
		return dom.WorkMetadata{}, fmt.Errorf("%w: %s", ErrWorkNotFound, key)
	}
	hit := hits[0]
	w := dom.WorkMetadata{
		WorkKey:   key,
		Source:    dom.SourceOpenLibrary,
		SourceID:  key,
		Title:     hit.Name,
		Author:    hit.Author,
		Year:      hit.Year,
		CoverURL:  hit.CoverURL,
		FetchedAt: time.Now(),
	}
	// MT2: refresh the editions on the same row (best-effort — a details
	// failure keeps the search-hit-based row).
	if fd, ok := s.deps.Searcher.(workDetailsFetcher); ok {
		if details, derr := fd.GetWorkDetails(ctx, key); derr == nil {
			if ed, jerr := json.Marshal(details.Editions); jerr == nil {
				w.EditionsJSON = ed
			}
		}
	}
	if err := s.deps.MetadataStore.UpsertWork(ctx, w); err != nil {
		return dom.WorkMetadata{}, fmt.Errorf("books: refresh work metadata: %w", err)
	}
	return w, nil
}
