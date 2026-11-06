package books

import "strings"

// ---------------------------------------------------------------------------
// MT4 (metadata profiles): the server-side filters applied to book lookup
// results and work-edition output.
//
// The filters are split into two tiers because keyless Open Library exposes
// different data per surface:
//
//   - Tier 1 (search document): the /search.json result document itself
//     carries the language list, first_publish_year, and the indexed-edition
//     count, so allowed-languages, skip-missing-release-date, and
//     minimum-popularity evaluate with NO extra provider call.
//   - Tier 2 (work document): skip-no-ISBN/ASIN, skip-broken-parts, and
//     skip-secondary-series need edition-level data (ISBN/ASIN, series
//     membership) that lives only in the /works/OL…W.json document. The
//     service fetches (or reads through the MT3 provenance store) the work
//     document ONLY for hits that survive tier 1 and only when a tier-2
//     filter is active.
// ---------------------------------------------------------------------------

// ProfileFilters is the server-side MT4 metadata-profile filter set: the
// domain mirror of the settings BookMetadataProfile filter fields. The
// service maps a named profile onto this type (the domain stays pure and
// never imports the settings package). A zero ProfileFilters passes
// everything (the seeded "Default" profile has no filters, so an explicit
// profile=Default request is behaviorally identical to no profile).
type ProfileFilters struct {
	// MinimumPopularity — the documented popularity proxy. Keyless Open
	// Library exposes no popularity score, so "popularity" IS the work's
	// indexed-edition count: a hit passes only when it has >= N indexed
	// editions. A hit with an UNKNOWN (0) edition count fails any positive
	// minimum — an unknown count cannot satisfy one (Google Books
	// secondary-source hits, MT5, carry no edition count, so they pass the
	// popularity filter only when the minimum is 0).
	MinimumPopularity int
	// SkipMissingReleaseDate: the hit (lookup) or edition (editions
	// endpoint) must carry a release year; a missing year (0) is dropped.
	SkipMissingReleaseDate bool
	// SkipNoISBNASIN: at WORK level the work document must contain at least
	// one edition carrying an ISBN or an ASIN (all editions lacking one
	// drops the work); at EDITION level the edition itself must carry an
	// ISBN (or ASIN).
	SkipNoISBNASIN bool
	// SkipBrokenParts: the hit is dropped when it holds a NUMBERED (primary)
	// series membership in a series the work document indexes with fewer
	// than two parts — a broken/incomplete series set. A "part" is a
	// distinct volume label: across all of the work document's editions, a
	// series referenced with N distinct volume/part labels has N parts (a
	// one-book series, or a series of which the document holds a single
	// numbered volume, is broken). Works with no numbered membership pass.
	//
	// Data caveat (documented degradation): Open Library carries series
	// references on edition documents ("series": [{title, volume}, ...])
	// only for a subset of works; a work document without series references
	// simply has nothing to act on, so the filter passes it rather than
	// guessing. The filter degrades to a no-op per-work, never per-request.
	SkipBrokenParts bool
	// SkipSecondarySeries: the hit is dropped when its series membership is
	// SECONDARY-ONLY — the work document references one or more series but
	// NONE of the references carries a numbered volume/part (the work
	// merely "belongs to" the series, e.g. an anthology cross-listing,
	// rather than being volume N of it). A numbered membership in any
	// series passes; a work with no series references passes (same
	// documented degradation as SkipBrokenParts).
	SkipSecondarySeries bool
	// AllowedLanguages: when non-empty, a hit (lookup) or edition (editions
	// endpoint) passes only when at least one of its language codes is in
	// the list. Comparison is case-insensitive and EXACT per code — no
	// language-family expansion, so an allowlist of "en" does not match
	// Open Library's "eng" (the codes come from each source: OL uses
	// ISO-639-3-style codes, Google Books ISO-639-1-style). An empty list
	// passes everything. A hit/edition with NO language data fails a
	// non-empty allowlist: the filter cannot verify membership, and the
	// profile's language list is an explicit allowlist (the Readarr
	// semantics).
	AllowedLanguages []string
}

// Active reports whether any tier-1 or tier-2 filter is set.
func (f *ProfileFilters) Active() bool {
	return f.Tier1Active() || f.Tier2Active()
}

// Tier1Active reports whether a filter that reads the search document
// (tier 1) is set.
func (f *ProfileFilters) Tier1Active() bool {
	return f.MinimumPopularity > 0 || f.SkipMissingReleaseDate || len(f.AllowedLanguages) > 0
}

// Tier2Active reports whether a filter that reads the work document
// (tier 2) is set — the service fetches (or store-reads) work details
// only when this is true.
func (f *ProfileFilters) Tier2Active() bool {
	return f.SkipNoISBNASIN || f.SkipBrokenParts || f.SkipSecondarySeries
}

// Tier1Facts is the search-document data the tier-1 filters evaluate for
// one book hit. All fields are optional provider data: nil languages /
// year 0 / count 0 mean "the source did not say".
type Tier1Facts struct {
	// Languages: the hit's language codes (nil = unknown).
	Languages []string
	// ReleaseYear: first publish year (0 = missing).
	ReleaseYear int
	// EditionCount: indexed-edition count — the popularity proxy
	// (0 = unknown/none).
	EditionCount int
}

// Tier2Facts is the work-document data the tier-2 filters evaluate for
// one book hit.
type Tier2Facts struct {
	// HasISBNOrASIN: true when the work document contains at least one
	// edition carrying an ISBN or an ASIN.
	HasISBNOrASIN bool
	// Series: every series reference found on the work document's
	// editions (flattened, in document order).
	Series []SeriesRef
}

// SeriesRef is one series reference from an edition document: the series
// title and the numbered volume/part label when the membership is primary
// ("" = unnumbered, i.e. secondary membership). It is the provider's series
// data on the work document — NOT the library's Series entity (the B4
// bookshelves model), which the add flow may link to separately.
type SeriesRef struct {
	// Title: the series name.
	Title string
	// Volume: the numbered volume/part label ("1", "2", ...); "" when the
	// reference is unnumbered.
	Volume string
}

// Tier2FactsFromEditions derives the tier-2 facts from a work document's
// editions (fetched or store-cached).
func Tier2FactsFromEditions(eds []WorkEdition) Tier2Facts {
	var t Tier2Facts
	for _, e := range eds {
		if e.ISBN != "" || e.ASIN != "" {
			t.HasISBNOrASIN = true
		}
		t.Series = append(t.Series, e.Series...)
	}
	return t
}

// languageAllowed implements the allowed-languages rule (see
// ProfileFilters.AllowedLanguages): an empty allowlist passes everything;
// otherwise the item must declare at least one language AND at least one
// declared code must be in the allowlist (case-insensitive, exact code).
func (f *ProfileFilters) languageAllowed(codes []string) bool {
	if len(f.AllowedLanguages) == 0 {
		return true
	}
	if len(codes) == 0 {
		return false // unknown language fails an explicit allowlist
	}
	for _, c := range codes {
		for _, a := range f.AllowedLanguages {
			if strings.EqualFold(c, a) {
				return true
			}
		}
	}
	return false
}

// PassTier1 reports whether a book hit passes every active tier-1 filter
// (the search-document tier: languages, release date, popularity proxy).
func (f *ProfileFilters) PassTier1(t Tier1Facts) bool {
	if !f.languageAllowed(t.Languages) {
		return false
	}
	if f.SkipMissingReleaseDate && t.ReleaseYear == 0 {
		return false
	}
	if f.MinimumPopularity > 0 && t.EditionCount < f.MinimumPopularity {
		return false
	}
	return true
}

// PassTier2 reports whether a book hit passes every active tier-2 filter
// (the work-document tier: identifiers, broken parts, secondary series).
func (f *ProfileFilters) PassTier2(t Tier2Facts) bool {
	if f.SkipNoISBNASIN && !t.HasISBNOrASIN {
		return false
	}
	if f.SkipBrokenParts && !passBrokenParts(t.Series) {
		return false
	}
	if f.SkipSecondarySeries && !passSecondarySeries(t.Series) {
		return false
	}
	return true
}

// passBrokenParts implements SkipBrokenParts (see the field doc for the
// rule and the data caveat). No numbered membership = pass; a numbered
// membership in a series with < 2 indexed parts (distinct volume labels in
// the document) = broken.
func passBrokenParts(refs []SeriesRef) bool {
	hasPrimary := false
	parts := make(map[string]map[string]struct{}) // series title -> distinct volume labels
	for _, r := range refs {
		if r.Title == "" || r.Volume == "" {
			continue
		}
		hasPrimary = true
		if parts[r.Title] == nil {
			parts[r.Title] = make(map[string]struct{})
		}
		parts[r.Title][r.Volume] = struct{}{}
	}
	if !hasPrimary {
		return true
	}
	for _, vols := range parts {
		if len(vols) < 2 {
			return false
		}
	}
	return true
}

// passSecondarySeries implements SkipSecondarySeries (see the field doc
// for the rule): a work with series references but no numbered membership
// anywhere is secondary-only and fails; everything else passes.
func passSecondarySeries(refs []SeriesRef) bool {
	any, primary := false, false
	for _, r := range refs {
		if r.Title == "" {
			continue
		}
		any = true
		if r.Volume != "" {
			primary = true
		}
	}
	return !any || primary
}

// PassEdition applies the per-edition subset of the profile to one work
// edition (the editions endpoint's output). Only the per-edition filters
// apply: allowed-languages (the edition's languages),
// skip-missing-release-date (the edition's publish year), and
// skip-no-ISBN/ASIN (the edition's own ISBN/ASIN). minimum_popularity,
// skip_broken_parts, and skip_secondary_series are WORK-level properties
// and deliberately do NOT apply per edition: the editions endpoint filters
// which editions of an already-selected work are offered, not whether the
// work itself is eligible (that decision was made at lookup time).
func (f *ProfileFilters) PassEdition(ed WorkEdition) bool {
	if !f.languageAllowed(ed.Languages) {
		return false
	}
	if f.SkipMissingReleaseDate && ed.Year == 0 {
		return false
	}
	if f.SkipNoISBNASIN && ed.ISBN == "" && ed.ASIN == "" {
		return false
	}
	return true
}
