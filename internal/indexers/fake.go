package indexers

import (
	"context"
	"strings"
)

// FakeIndexer is a scripted indexer used by the M1 spike and end-to-end tests.
//
// It returns the releases registered via AddRelease, filtered (loosely) by the
// search term so a "search" behaves plausibly: a release is returned if any of
// its lowercased tokens contain the lowercased search term. This lets the
// pipeline exercise the full search → parse → match → pick path without any
// network I/O.
//
// It is deliberately not a real indexer; it exists to prove the architecture.
type FakeIndexer struct {
	Name_ string
	// Releases is the pool of candidate releases this indexer serves.
	Releases []SearchResult
	// SearchFn, when non-nil, overrides the default term-match behaviour.
	SearchFn func(q SearchQuery) []SearchResult
}

// NewFakeIndexer builds a FakeIndexer with the given name.
//
// The release pool starts empty so tests register exactly the candidates they
// assert on. The dev server (cmd/mediarr) seeds a small demo pool separately so
// a live "search" returns plausible candidates for any title.
func NewFakeIndexer(name string) *FakeIndexer {
	return &FakeIndexer{Name_: name}
}

// AddRelease registers a candidate release returned by this indexer.
func (f *FakeIndexer) AddRelease(r SearchResult) {
	if r.Indexer == "" {
		r.Indexer = f.Name_
	}
	f.Releases = append(f.Releases, r)
}

// Name implements Searcher.
func (f *FakeIndexer) Name() string { return f.Name_ }

// Kind implements Provider. The fake's protocol kind is "fake"; it is the
// reference Provider in the registry (see NewDefaultRegistry) and the kind the
// `fake` definition maps to in the definition store.
func (f *FakeIndexer) Kind() string { return "fake" }

// Test implements Provider. The fake indexer is always healthy: it performs no
// network I/O, so a health probe trivially succeeds. Real adapters implement
// this as a live request.
func (f *FakeIndexer) Test(ctx context.Context) error { return nil }

// Search implements Searcher.
func (f *FakeIndexer) Search(ctx context.Context, q SearchQuery) ([]SearchResult, error) {
	if f.SearchFn != nil {
		return f.SearchFn(q), nil
	}
	out := make([]SearchResult, 0, len(f.Releases))
	term := strings.ToLower(strings.ReplaceAll(q.Term, " ", " "))
	for _, r := range f.Releases {
		if term == "" || termMatches(r.Title, q.Term) {
			out = append(out, r)
		}
	}
	return out, nil
}

// termMatches reports whether the release title plausibly matches the search
// term: every token of the term (length >= 3) appears somewhere in the lowercased
// release name. A one-word term like "Inception" matches "Inception.2010.1080p".
func termMatches(title, term string) bool {
	t := strings.ToLower(title)
	for _, tok := range strings.Fields(term) {
		tok = strings.ToLower(tok)
		if len(tok) < 3 {
			continue
		}
		if !strings.Contains(t, tok) {
			return false
		}
	}
	return true
}
