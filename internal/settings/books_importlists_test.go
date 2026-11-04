package settings

import (
	"encoding/json"
	"strings"
	"testing"
)

// B5: the books import-lists section decodes over its empty defaults, both
// for an unsaved (nil) document and a partial stored document.
func TestB5_BookImportListsDecodeDefaults(t *testing.T) {
	doc, err := SectionBookImportLists.DecodeDocument(nil)
	if err != nil {
		t.Fatalf("decode nil: %v", err)
	}
	bil := doc.(*BookImportListsSettings)
	if len(bil.Lists) != 0 || len(bil.Exclusions) != 0 {
		t.Fatalf("defaults = %+v, want empty (non-nil) lists/exclusions", bil)
	}

	// A partial stored document merges over the defaults (a saved list
	// survives; the exclusions array stays the stored one).
	partial, _ := json.Marshal(BookImportListsSettings{
		Lists: []BookImportList{{ID: 5, Name: "My Lists", Kind: "goodreads", BaseURL: "https://www.goodreads.com", ListID: 42, Enabled: true}},
	})
	doc, err = SectionBookImportLists.DecodeDocument(partial)
	if err != nil {
		t.Fatalf("decode partial: %v", err)
	}
	bil = doc.(*BookImportListsSettings)
	if len(bil.Lists) != 1 || bil.Lists[0].ListID != 42 {
		t.Fatalf("partial decode = %+v, want the stored list", bil)
	}
}

// B5: the kind-specific validation rules.
func TestB5_BookImportListsValidateErrors(t *testing.T) {
	cases := []struct {
		name string
		list BookImportList
		want string
	}{
		{"id below 1", BookImportList{ID: 0, Name: "x", Kind: "goodreads", BaseURL: "https://www.goodreads.com", ListID: 1}, "id must be >= 1"},
		{"missing name", BookImportList{ID: 1, Kind: "goodreads", BaseURL: "https://www.goodreads.com", ListID: 1}, "name is required"},
		{"unknown kind", BookImportList{ID: 1, Name: "x", Kind: "trakt"}, "unknown kind"},
		{"goodreads missing list id", BookImportList{ID: 1, Name: "x", Kind: "goodreads", BaseURL: "https://www.goodreads.com"}, "list_id must be >= 1"},
		{"goodreads missing base url", BookImportList{ID: 1, Name: "x", Kind: "goodreads", ListID: 1}, "base_url is required"},
		{"lazylibrarian bad url", BookImportList{ID: 1, Name: "x", Kind: "lazylibrarian", BaseURL: "not-a-url"}, "base_url must be an http/https URL"},
		{"readarr bad url", BookImportList{ID: 1, Name: "x", Kind: "readarr", BaseURL: "ftp://nope"}, "base_url must be an http/https URL"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := (BookImportListsSettings{Lists: []BookImportList{tc.list}}).Validate()
			if err == "" {
				t.Fatal("expected a validation error, got none")
			}
			if !strings.Contains(err, tc.want) {
				t.Fatalf("error = %q, want it to contain %q", err, tc.want)
			}
		})
	}

	// Duplicate ids.
	dup := BookImportListsSettings{Lists: []BookImportList{
		{ID: 1, Name: "a", Kind: "goodreads", BaseURL: "https://www.goodreads.com", ListID: 1},
		{ID: 1, Name: "b", Kind: "goodreads", BaseURL: "https://www.goodreads.com", ListID: 2},
	}}
	if err := dup.Validate(); !strings.Contains(err, "duplicate id") {
		t.Fatalf("duplicate id: got %q", err)
	}
}

// B5: valid documents pass — one of each kind, with the readarr filters set.
func TestB5_BookImportListsValidateValid(t *testing.T) {
	doc := BookImportListsSettings{
		Lists: []BookImportList{
			{ID: 1, Name: "GR", Kind: "goodreads", BaseURL: "https://www.goodreads.com", ListID: 12345, Enabled: true},
			{ID: 2, Name: "LL", Kind: "lazylibrarian", BaseURL: "http://ll.example:52352", Enabled: true},
			{ID: 3, Name: "RA", Kind: "readarr", BaseURL: "https://readarr.example:8787", ProfileIDs: []int{1}, TagIDs: []int{2}, RootFolderPaths: []string{"/data/books"}},
		},
		Exclusions: []BookImportListExclusion{{ForeignID: "9", Name: "The Unwanted"}},
	}
	if err := doc.Validate(); err != "" {
		t.Fatalf("valid doc rejected: %q", err)
	}
}

// B5: masking blanks every list's API key (and only the key), and
// MergeSecrets completes the masked round-trip: a blank incoming key keeps
// the stored (plaintext) key, a new incoming key replaces it, and the
// *_set flag tracks presence.
func TestB5_BookImportListsMaskAndMergeSecrets(t *testing.T) {
	stored := &BookImportListsSettings{Lists: []BookImportList{
		{ID: 1, Name: "LL", Kind: "lazylibrarian", BaseURL: "http://ll.example:52352", APIKey: "s3cret", APIKeySet: true},
		{ID: 2, Name: "GR", Kind: "goodreads", BaseURL: "https://www.goodreads.com", ListID: 1},
	}}

	masked := SectionBookImportLists.MaskDocument(stored).(*BookImportListsSettings)
	if masked.Lists[0].APIKey != "" {
		t.Fatalf("mask leaked the stored key: %q", masked.Lists[0].APIKey)
	}
	if masked.Lists[0].APIKeySet != true || masked.Lists[0].BaseURL == "" {
		t.Fatalf("mask must keep api_key_set and the other fields: %+v", masked.Lists[0])
	}
	// Masking must not mutate the stored document.
	if stored.Lists[0].APIKey != "s3cret" {
		t.Fatal("masking mutated the stored document")
	}

	// The value (non-pointer) form masks too (the section decode hands
	// back the pointer form; the API tests exercise the pointer form).
	maskedVal := SectionBookImportLists.MaskDocument(*stored).(BookImportListsSettings)
	if maskedVal.Lists[0].APIKey != "" {
		t.Fatal("value-form mask leaked the key")
	}

	// Round-trip 1: the UI posts the masked document back (blank key on
	// list 1, untouched on list 2) → the stored key is restored.
	in := *masked
	SectionBookImportLists.MergeSecrets(stored, &in)
	if in.Lists[0].APIKey != "s3cret" || !in.Lists[0].APIKeySet {
		t.Fatalf("blank incoming key = %+v, want stored key restored + set flag", in.Lists[0])
	}
	if in.Lists[1].APIKey != "" || in.Lists[1].APIKeySet {
		t.Fatalf("keyless list = %+v, want no key, not set", in.Lists[1])
	}

	// Round-trip 2: a NEW key replaces the stored one.
	in2 := *masked
	in2.Lists[0].APIKey = "newkey"
	SectionBookImportLists.MergeSecrets(stored, &in2)
	if in2.Lists[0].APIKey != "newkey" || !in2.Lists[0].APIKeySet {
		t.Fatalf("new incoming key = %+v, want the new key + set flag", in2.Lists[0])
	}
}

// B5: the document reaches the generic section machinery (parse, validate,
// encode round-trip).
func TestB5_BookImportListsSectionMachinery(t *testing.T) {
	if _, ok := ParseSection("book-import-lists"); !ok {
		t.Fatal("book-import-lists is not a parseable section")
	}
	doc := DefaultBookImportLists()
	_ = SectionBookImportLists.ValidateDocument(doc)
	enc, err := SectionBookImportLists.EncodeDocument(doc)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	back, err := SectionBookImportLists.DecodeDocument(enc)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(back.(*BookImportListsSettings).Lists) != 0 {
		t.Fatal("round-trip changed the empty document")
	}
}
