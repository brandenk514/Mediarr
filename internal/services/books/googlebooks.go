package books

import (
	"context"

	dom "github.com/brandenk514/mediarr/internal/domains/books"
	"github.com/brandenk514/mediarr/internal/providers"
)

// ---------------------------------------------------------------------------
// MT5 (Google Books secondary source): the optional fallback leg of the
// combined books lookup. Google Books is NEVER the primary source — Open
// Library stays the source of truth. The leg runs only for a PLAIN-TEXT
// query (id-lookup syntax never reaches it: ids are unambiguous and OL is
// their source of truth) whose OL leg produced ZERO book hits, and only
// when a Google Books API key is configured (the enablement flag; no
// separate flag). Its hits are capped, MT4-profile-filtered like any other
// book hits, never written to the MT3 provenance store, and never carry a
// work key (the UI's manual-add fallback handles them — by design).
// ---------------------------------------------------------------------------

// googleBooksLegCap bounds the number of Google Books hits a single
// fallback leg contributes (the documented cap: GB's free-text ranking is
// a gap-filler, not a catalog — ten results is plenty of candidates for
// the add modal).
const googleBooksLegCap = 10

// GoogleBooksSearcher is the MT5 secondary-source surface on the Google
// Books adapter: the service depends on this interface (never on the
// provider concretely) so the leg is unit-testable with a fake. A nil
// dep (tests, no wiring) or a not-configured adapter disables the leg
// (skip, no error, no 503 on the lookup).
type GoogleBooksSearcher interface {
	// Configured reports whether the secondary leg is enabled (a
	// non-empty API key).
	Configured() bool
	// SearchBooks returns the secondary leg's book hits for a plain-text
	// (or id-syntax) query string.
	SearchBooks(ctx context.Context, q string) ([]dom.BookSearchHit, error)
}

// googleBooksSearcher adapts the providers.GoogleBooks adapter to the
// service's GoogleBooksSearcher surface, mapping the provider's volume
// hits onto the domain's BookSearchHit with the Source marker
// (dom.SourceGoogleBooks) — the wire hit shape does NOT gain a field:
// the hit's ID is the Google volume id and the lookup's work key is left
// empty for it (the manual-add fallback, by design).
type googleBooksSearcher struct {
	gb *providers.GoogleBooks
}

// NewGoogleBooksSearcher wraps the Google Books provider as the MT5
// secondary-source searcher (production wiring in main.go).
func NewGoogleBooksSearcher(gb *providers.GoogleBooks) GoogleBooksSearcher {
	return &googleBooksSearcher{gb: gb}
}

func (g *googleBooksSearcher) Configured() bool { return g.gb.Configured() }

func (g *googleBooksSearcher) SearchBooks(ctx context.Context, q string) ([]dom.BookSearchHit, error) {
	res, err := g.gb.SearchBooks(ctx, q)
	if err != nil {
		return nil, err
	}
	out := make([]dom.BookSearchHit, 0, len(res))
	for _, r := range res {
		out = append(out, dom.BookSearchHit{
			ID:     r.ID, // the Google volume id (NOT an OpenLibrary work key)
			Name:   r.Title,
			Author: r.Extra["author"],
			Year:   r.Year,
			// GB volumeInfo carries a single language code; the
			// comma-joined "languages" extra keeps the one shared
			// mapping path with the OL leg.
			Languages: splitExtras(r.Extra["languages"]),
			// No indexed-edition count exists for a GB volume: the
			// popularity proxy is N/A, so a GB hit passes
			// minimum_popularity only when the minimum is 0
			// (documented in the domain filter + the spec).
			EditionCount: 0,
			Source:       dom.SourceGoogleBooks,
		})
	}
	return out, nil
}

// googleBooksLeg is the MT5 fallback: run the Google Books secondary leg
// for a plain-text query whose Open Library leg produced ZERO book hits
// (after MT4 profile filtering — the filtered view is what the user sees,
// so a profile that drops every OL hit still lets GB fill the gap).
//
// The leg is best-effort: a not-configured or failing GB leg is skipped
// silently (a secondary source never breaks the lookup — the same
// graceful-degradation convention as the other search legs), and its hits
// are capped at googleBooksLegCap before the (optional) profile filter
// runs. The filter is the same two-tier filter as the OL hits: GB hits
// pass tier 2 by construction (a Google Books volume has no OpenLibrary
// work document to read — the documented non-OL rule in
// filterWorkHits), and the popularity proxy is N/A for them (they pass
// only when the minimum is 0 — EditionCount 0).
//
// The returned hits are NOT written to the MT3 provenance store (the
// store is OL provenance for the editions flow; GB hits never get
// editions) — the caller appends them AFTER its OL write-through.
func (s *Service) googleBooksLeg(ctx context.Context, q string, f *dom.ProfileFilters) []dom.BookSearchHit {
	gb := s.deps.GoogleBooks
	if gb == nil || !gb.Configured() {
		return nil
	}
	hits, err := gb.SearchBooks(ctx, q)
	if err != nil {
		return nil
	}
	if len(hits) > googleBooksLegCap {
		hits = hits[:googleBooksLegCap]
	}
	if f != nil {
		hits = s.filterWorkHits(ctx, hits, f)
	}
	return hits
}
