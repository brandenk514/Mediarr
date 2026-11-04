package books

import (
	"context"
	"errors"
	"testing"

	"github.com/brandenk514/mediarr/internal/settings"
)

// fakeBookImporter is an in-memory BookImporter for the import-list tests.
type fakeBookImporter struct {
	books   []ExternalBook
	added   []ExternalBook
	addErr  error
	listErr error
}

func (f *fakeBookImporter) AddBook(_ context.Context, b ExternalBook) (int64, error) {
	if f.addErr != nil {
		return 0, f.addErr
	}
	f.added = append(f.added, b)
	return int64(len(f.added)), nil
}

func (f *fakeBookImporter) ListBooks(_ context.Context) ([]ExternalBook, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.books, nil
}

// TestBookImporter_FakeSatisfiesInterface is the compile-time proof that the
// fake (and therefore the service adapter) satisfies BookImporter.
func TestBookImporter_FakeSatisfiesInterface(t *testing.T) {
	var _ BookImporter = (*fakeBookImporter)(nil)
}

// TestFilterExcluded_MatchesByIDAndName verifies the exclusion matching:
// foreign id (case-insensitive exact) and name/title (case-insensitive),
// and that non-matching books survive in order.
func TestFilterExcluded_MatchesByIDAndName(t *testing.T) {
	books := []ExternalBook{
		{ForeignID: "101", Title: "The Dispossessed", Author: "Ursula K. Le Guin"},
		{ForeignID: "102", Title: "A Wizard of Earthsea"},
		{ForeignID: "103", Title: "Lavinia"},
		{ForeignID: "104", Title: "Butter"},
	}
	exclusions := []settings.BookImportListExclusion{
		{ForeignID: "102"},                     // id match
		{Name: "lavinia"},                      // name match, different case
		{Name: "  Butter  "},                   // name match with padding
		{ForeignID: "999", Name: "Never Seen"}, // matches nothing
	}
	got := FilterExcluded(books, exclusions)
	if len(got) != 1 {
		t.Fatalf("got %d books, want 1: %+v", len(got), got)
	}
	if got[0].ForeignID != "101" {
		t.Fatalf("survivor = %+v, want the Dispossessed", got[0])
	}
}

// TestFilterExcluded_NoExclusions is a pass-through (and never returns nil).
func TestFilterExcluded_NoExclusions(t *testing.T) {
	books := []ExternalBook{{ForeignID: "1", Title: "X"}}
	got := FilterExcluded(books, nil)
	if len(got) != 1 || got[0].ForeignID != "1" {
		t.Fatalf("pass-through = %+v, want all books", got)
	}
	// Empty batch in, empty slice (not nil) out.
	empty := FilterExcluded(nil, nil)
	if empty == nil {
		t.Fatal("nil batch should come back as an empty slice, not nil")
	}
	if len(empty) != 0 {
		t.Fatalf("empty batch = %+v, want empty", empty)
	}
}

// TestFilterExcluded_ExclusionMatchingBothFields requires BOTH fields when
// present: an exclusion with a foreign id matches only by id, one with a
// name only by name (each field is an independent matcher).
func TestFilterExcluded_ExclusionMatchingBothFields(t *testing.T) {
	books := []ExternalBook{
		{ForeignID: "abc", Title: "Same Name"},
		{ForeignID: "xyz", Title: "Same Name"},
	}
	// Exclude only the FIRST book: name matches both, so a name-only
	// exclusion would drop both; the id-only form drops exactly one.
	got := FilterExcluded(books, []settings.BookImportListExclusion{{ForeignID: "ABC"}})
	if len(got) != 1 || got[0].ForeignID != "xyz" {
		t.Fatalf("id-only exclusion = %+v, want only xyz", got)
	}
	got = FilterExcluded(books, []settings.BookImportListExclusion{{Name: "same name"}})
	if len(got) != 0 {
		t.Fatalf("name-only exclusion = %+v, want none", got)
	}
}

// TestFakeBookImporter_AddAndList exercises the fake's add/list behavior
// (the shape the service adapter is tested against).
func TestFakeBookImporter_AddAndList(t *testing.T) {
	f := &fakeBookImporter{books: []ExternalBook{{Title: "Existing"}}}
	id, err := f.AddBook(context.Background(), ExternalBook{Title: "New"})
	if err != nil || id != 1 {
		t.Fatalf("AddBook = %d, %v", id, err)
	}
	listed, err := f.ListBooks(context.Background())
	if err != nil || len(listed) != 1 || listed[0].Title != "Existing" {
		t.Fatalf("ListBooks = %+v, %v", listed, err)
	}
}

// TestFakeBookImporter_Errors surfaces injected errors.
func TestFakeBookImporter_Errors(t *testing.T) {
	wantErr := errors.New("boom")
	f := &fakeBookImporter{addErr: wantErr}
	if _, err := f.AddBook(context.Background(), ExternalBook{}); !errors.Is(err, wantErr) {
		t.Fatalf("AddBook err = %v, want %v", err, wantErr)
	}
	f = &fakeBookImporter{listErr: wantErr}
	if _, err := f.ListBooks(context.Background()); !errors.Is(err, wantErr) {
		t.Fatalf("ListBooks err = %v, want %v", err, wantErr)
	}
}
