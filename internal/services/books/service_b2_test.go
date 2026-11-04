package books

import (
	"context"
	"errors"
	"testing"

	dom "github.com/brandenk514/mediarr/internal/domains/books"
)

// fakeBookSearcher is a stub dom.BookSearcher for the combined-lookup unit
// tests (Readarr B2). It returns canned hits and can fail either search leg.
type fakeBookSearcher struct {
	authors   []dom.BookSearchHit
	works     []dom.BookSearchHit
	authorErr error
	workErr   error
}

func (f *fakeBookSearcher) SearchAuthors(ctx context.Context, q string) ([]dom.BookSearchHit, error) {
	if f.authorErr != nil {
		return nil, f.authorErr
	}
	return f.authors, nil
}

func (f *fakeBookSearcher) SearchWorks(ctx context.Context, author, title string) ([]dom.BookSearchHit, error) {
	if f.workErr != nil {
		return nil, f.workErr
	}
	return f.works, nil
}

func strPtr(s string) *string { return &s }

// TestService_AddAuthorB2Fields verifies the Readarr B2 add-author fields are
// stored and read back (monitor policy, quality/metadata profile, root
// folder, tags), and that an empty policy is the legacy "all" default.
func TestService_AddAuthorB2Fields(t *testing.T) {
	repo := newMemRepo()
	svc := New(Deps{Repo: repo, DefaultProfile: "Best"})
	ctx := context.Background()

	id, err := svc.AddAuthor(ctx, AddAuthorInput{
		Name:            "Ursula K. Le Guin",
		Monitored:       true,
		MonitorPolicy:   dom.PolicyFirst,
		QualityProfile:  "Best",
		MetadataProfile: "Default",
		RootFolder:      "/media/books",
		Tags:            []string{"favorites", "sci-fi"},
	})
	if err != nil {
		t.Fatalf("AddAuthor: %v", err)
	}
	got, err := svc.GetAuthor(ctx, id)
	if err != nil {
		t.Fatalf("GetAuthor: %v", err)
	}
	if got.MonitorPolicy != dom.PolicyFirst {
		t.Errorf("MonitorPolicy = %q, want %q", got.MonitorPolicy, dom.PolicyFirst)
	}
	if got.QualityProfile != "Best" || got.MetadataProfile != "Default" || got.RootFolder != "/media/books" {
		t.Errorf("profiles/root = %q/%q/%q, want Best/Default//media/books",
			got.QualityProfile, got.MetadataProfile, got.RootFolder)
	}
	if len(got.Tags) != 2 || got.Tags[0] != "favorites" || got.Tags[1] != "sci-fi" {
		t.Errorf("Tags = %v, want [favorites sci-fi]", got.Tags)
	}
}

// TestService_AddAuthorDefaults verifies an author added with no B2 fields
// gets the empty defaults ("" = all / none / server default).
func TestService_AddAuthorDefaults(t *testing.T) {
	repo := newMemRepo()
	svc := New(Deps{Repo: repo, DefaultProfile: "Best"})
	ctx := context.Background()

	id, _ := svc.AddAuthor(ctx, AddAuthorInput{Name: "Legacy", Monitored: true})
	got, err := svc.GetAuthor(ctx, id)
	if err != nil {
		t.Fatalf("GetAuthor: %v", err)
	}
	if got.MonitorPolicy != "" || got.QualityProfile != "" || got.MetadataProfile != "" || got.RootFolder != "" {
		t.Errorf("default fields not empty: %+v", got)
	}
	if len(got.Tags) != 0 {
		t.Errorf("default Tags = %v, want empty", got.Tags)
	}
}

// TestService_AddAuthorInvalidMonitorPolicy verifies an unknown monitor policy
// is rejected with ErrInvalidAddParams and no author is created.
func TestService_AddAuthorInvalidMonitorPolicy(t *testing.T) {
	repo := newMemRepo()
	svc := New(Deps{Repo: repo, DefaultProfile: "Best"})
	ctx := context.Background()

	_, err := svc.AddAuthor(ctx, AddAuthorInput{Name: "X", Monitored: true, MonitorPolicy: "weekly"})
	if !errors.Is(err, ErrInvalidAddParams) {
		t.Fatalf("AddAuthor bad policy: err = %v, want ErrInvalidAddParams", err)
	}
	if authors, _ := svc.ListAuthors(ctx); len(authors) != 0 {
		t.Errorf("author created despite invalid policy: %+v", authors)
	}
}

// TestService_AddAuthorStartSearchBestEffort verifies that StartSearch runs the
// pipeline best-effort: with no pending wanted the pipeline errors, but the
// add still succeeds and the failure is recorded in history (never fails the
// add — Radarr R2 / Readarr B2).
func TestService_AddAuthorStartSearchBestEffort(t *testing.T) {
	repo := newMemRepo()
	svc := New(Deps{Repo: repo, DefaultProfile: "Best"})
	ctx := context.Background()

	id, err := svc.AddAuthor(ctx, AddAuthorInput{Name: "Start Searcher", Monitored: true, StartSearch: true})
	if err != nil {
		t.Fatalf("AddAuthor(StartSearch) should not fail: %v", err)
	}
	if _, err := svc.GetAuthor(ctx, id); err != nil {
		t.Fatalf("author missing after best-effort start-search: %v", err)
	}
	hist, err := svc.GetAuthorHistory(ctx, id, 100)
	if err != nil {
		t.Fatalf("GetAuthorHistory: %v", err)
	}
	var sawAdded, sawFailed bool
	for _, h := range hist {
		if h.Event == "added" {
			sawAdded = true
		}
		if h.Event == "start-search-failed" {
			sawFailed = true
		}
	}
	if !sawAdded {
		t.Errorf("expected an 'added' history entry, got %+v", hist)
	}
	if !sawFailed {
		t.Errorf("expected a 'start-search-failed' entry (pipeline had nothing to do), got %+v", hist)
	}
}

// TestService_UpdateAuthorMerge verifies the Edit Author modal merge semantics:
// omitted (nil) fields keep their current value, provided fields are applied,
// and a monitored toggle fans out to known titles.
func TestService_UpdateAuthorMerge(t *testing.T) {
	repo := newMemRepo()
	svc := New(Deps{Repo: repo, DefaultProfile: "Best"})
	ctx := context.Background()

	id, _ := svc.AddAuthor(ctx, AddAuthorInput{
		Name: "M", Monitored: true, MonitorPolicy: dom.PolicyAll,
		QualityProfile: "Best", RootFolder: "/root", Tags: []string{"a"},
	})
	// A known title so the monitored toggle has something to fan out to.
	_, _ = svc.AddTitle(ctx, id, "A Title", true)

	// Only change the quality profile; everything else is nil = keep.
	newTags := []string{"a", "b"}
	updated, err := svc.UpdateAuthor(ctx, id, UpdateAuthorInput{
		QualityProfile: strPtr("Good"),
		Tags:           &newTags,
	})
	if err != nil {
		t.Fatalf("UpdateAuthor: %v", err)
	}
	if updated.QualityProfile != "Good" {
		t.Errorf("QualityProfile = %q, want Good", updated.QualityProfile)
	}
	if updated.MonitorPolicy != dom.PolicyAll {
		t.Errorf("MonitorPolicy clobbered = %q, want %q", updated.MonitorPolicy, dom.PolicyAll)
	}
	if updated.RootFolder != "/root" {
		t.Errorf("RootFolder clobbered = %q, want /root", updated.RootFolder)
	}
	if len(updated.Tags) != 2 || updated.Tags[1] != "b" {
		t.Errorf("Tags = %v, want [a b]", updated.Tags)
	}
	// Monitored unchanged (still true) and the title stays monitored.
	titles, _ := svc.ListTitles(ctx, id)
	if len(titles) != 1 || !titles[0].Monitored {
		t.Errorf("title monitoring changed: %+v", titles)
	}

	// Toggle monitored off via the modal; it must fan out to the title.
	off := false
	if _, err := svc.UpdateAuthor(ctx, id, UpdateAuthorInput{Monitored: &off}); err != nil {
		t.Fatalf("UpdateAuthor(monitored=false): %v", err)
	}
	titles, _ = svc.ListTitles(ctx, id)
	if len(titles) != 1 || titles[0].Monitored {
		t.Errorf("monitored fan-out failed: %+v", titles)
	}
}

// TestService_UpdateAuthorInvalidMonitorPolicy verifies an unknown policy in
// the edit modal is rejected with ErrInvalidAddParams.
func TestService_UpdateAuthorInvalidMonitorPolicy(t *testing.T) {
	repo := newMemRepo()
	svc := New(Deps{Repo: repo, DefaultProfile: "Best"})
	ctx := context.Background()

	id, _ := svc.AddAuthor(ctx, AddAuthorInput{Name: "P", Monitored: true})
	_, err := svc.UpdateAuthor(ctx, id, UpdateAuthorInput{MonitorPolicy: strPtr("monthly")})
	if !errors.Is(err, ErrInvalidAddParams) {
		t.Fatalf("UpdateAuthor bad policy: err = %v, want ErrInvalidAddParams", err)
	}
}

// TestService_BooksLookupKindTagging verifies the combined lookup returns a
// single kind-tagged list (author hits first, then book hits) with the
// `added`/`library_id` flags computed against the library.
func TestService_BooksLookupKindTagging(t *testing.T) {
	repo := newMemRepo()
	searcher := &fakeBookSearcher{
		authors: []dom.BookSearchHit{
			{ID: "ursula", Name: "Ursula K. Le Guin"},
			{ID: "unknown", Name: "Nobody Famous"},
		},
		works: []dom.BookSearchHit{
			{ID: "OL1W", Name: "The Dispossessed", Author: "Ursula K. Le Guin", Year: 1974},
			{ID: "OL2W", Name: "Other Book", Author: "Someone Else", Year: 2000},
		},
	}
	svc := New(Deps{Repo: repo, DefaultProfile: "Best", Searcher: searcher})
	ctx := context.Background()

	// The library already has the author and one of their books.
	authorID, _ := svc.AddAuthor(ctx, AddAuthorInput{Name: "Ursula K. Le Guin", Monitored: true})
	titleID, _ := svc.AddTitle(ctx, authorID, "The Dispossessed", true)

	results, err := svc.BooksLookup(ctx, "ursula")
	if err != nil {
		t.Fatalf("BooksLookup: %v", err)
	}
	// 2 author hits then 2 book hits = 4.
	if len(results) != 4 {
		t.Fatalf("BooksLookup = %d hits, want 4: %+v", len(results), results)
	}
	// Author hits first.
	if results[0].Kind != dom.LookupAuthor || results[1].Kind != dom.LookupAuthor {
		t.Errorf("first two should be author hits, got %v,%v", results[0].Kind, results[1].Kind)
	}
	if results[2].Kind != dom.LookupBook || results[3].Kind != dom.LookupBook {
		t.Errorf("last two should be book hits, got %v,%v", results[2].Kind, results[3].Kind)
	}
	// The known author is added with its id; the unknown is not.
	if !results[0].Added || results[0].LibraryID != authorID {
		t.Errorf("known author: added=%v id=%d, want true/%d", results[0].Added, results[0].LibraryID, authorID)
	}
	if results[1].Added || results[1].LibraryID != 0 {
		t.Errorf("unknown author: added=%v id=%d, want false/0", results[1].Added, results[1].LibraryID)
	}
	// The known book is added with its title id; the other is not.
	if !results[2].Added || results[2].LibraryID != titleID {
		t.Errorf("known book: added=%v id=%d, want true/%d", results[2].Added, results[2].LibraryID, titleID)
	}
	if results[3].Added || results[3].LibraryID != 0 {
		t.Errorf("unknown book: added=%v id=%d, want false/0", results[3].Added, results[3].LibraryID)
	}
}

// TestService_BooksLookupEmptyAndNotConfigured verifies the degenerate lookup
// paths: an empty query returns no hits, and a nil searcher surfaces the
// not-configured error (the API maps it to 503).
func TestService_BooksLookupEmptyAndNotConfigured(t *testing.T) {
	repo := newMemRepo()
	svc := New(Deps{Repo: repo, DefaultProfile: "Best", Searcher: &fakeBookSearcher{}})
	ctx := context.Background()

	if results, err := svc.BooksLookup(ctx, "   "); err != nil || len(results) != 0 {
		t.Errorf("BooksLookup(empty) = (%v, %v), want ([], nil)", results, err)
	}

	// A searcher that fails both legs surfaces an error.
	both := &fakeBookSearcher{authorErr: errors.New("boom"), workErr: errors.New("boom")}
	if _, err := New(Deps{Repo: repo, Searcher: both}).BooksLookup(ctx, "x"); err == nil {
		t.Error("BooksLookup(both legs fail) should error")
	}

	// A nil searcher surfaces ErrSearcherNotConfigured.
	if _, err := New(Deps{Repo: repo}).BooksLookup(ctx, "x"); !errors.Is(err, ErrSearcherNotConfigured) {
		t.Errorf("BooksLookup(nil searcher) = %v, want ErrSearcherNotConfigured", err)
	}
}

// TestService_BooksLookupOneLegFails verifies graceful degradation: when one
// search leg fails, the other leg's hits are still returned.
func TestService_BooksLookupOneLegFails(t *testing.T) {
	repo := newMemRepo()
	searcher := &fakeBookSearcher{
		authorErr: errors.New("author search down"),
		works:     []dom.BookSearchHit{{ID: "OL1W", Name: "The Dispossessed", Author: "Ursula", Year: 1974}},
	}
	svc := New(Deps{Repo: repo, DefaultProfile: "Best", Searcher: searcher})
	results, err := svc.BooksLookup(context.Background(), "ursula")
	if err != nil {
		t.Fatalf("BooksLookup should degrade, not error: %v", err)
	}
	if len(results) != 1 || results[0].Kind != dom.LookupBook {
		t.Errorf("expected only the work hit, got %+v", results)
	}
}
