package settings

import (
	"strings"
	"testing"
)

// B5: the books metadata block (write tags to files) lives on the shared
// metadata section document — defaults decode, validation propagates, and
// the fields survive an encode/decode round-trip alongside the consumers.
func TestB5_MetadataSectionBookTagWrite(t *testing.T) {
	doc, err := SectionMetadata.DecodeDocument(nil)
	if err != nil {
		t.Fatalf("decode nil: %v", err)
	}
	md := doc.(*MetadataSettings)
	if md.BookTags != DefaultBookTagWrite() {
		t.Fatalf("default book_tag_write = %+v, want %+v", md.BookTags, DefaultBookTagWrite())
	}
	if len(md.Consumers) == 0 {
		t.Fatal("consumers must still be seeded on the metadata document")
	}

	// Both tag-type vocabularies are accepted (no/all/book, no/all/audio).
	md.BookTags = BookTagWrite{BookTagType: "book", AudioTagType: "audio"}
	if msg := SectionMetadata.ValidateDocument(md); msg != "" {
		t.Fatalf("book/audio tag types must validate: %q", msg)
	}

	// Round-trip: the book tag types + consumers survive encode/decode.
	md.Consumers[0].Enabled = true
	enc, err := SectionMetadata.EncodeDocument(md)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	back, err := SectionMetadata.DecodeDocument(enc)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	b := back.(*MetadataSettings)
	if b.BookTags != md.BookTags {
		t.Fatalf("round-trip book_tag_write = %+v, want %+v", b.BookTags, md.BookTags)
	}
	if len(b.Consumers) != len(md.Consumers) || !b.Consumers[0].Enabled {
		t.Fatalf("round-trip consumers = %+v, want %+v", b.Consumers, md.Consumers)
	}
}

// B5: a bad book tag type is rejected by the section's validate (the book
// block errors surface through MetadataSettings.Validate).
func TestB5_MetadataSectionBookTagWriteValidate(t *testing.T) {
	md := DefaultMetadata()
	md.BookTags = BookTagWrite{BookTagType: "bogus", AudioTagType: "all"}
	if msg := SectionMetadata.ValidateDocument(md); msg == "" {
		t.Fatal("bad book_tag_type must fail the metadata section validate")
	}
	md.BookTags = BookTagWrite{BookTagType: "all", AudioTagType: "bogus"}
	if msg := SectionMetadata.ValidateDocument(md); msg == "" {
		t.Fatal("bad audio_tag_type must fail the metadata section validate")
	}
	md.BookTags = DefaultBookTagWrite()
	if msg := SectionMetadata.ValidateDocument(md); msg != "" {
		t.Fatalf("default book_tag_write must pass the metadata section validate: %q", msg)
	}
}

// B5: the value (non-pointer) form of the metadata section validate — the
// decode path hands back the pointer form, so exercise the value form the
// API handlers use.
func TestB5_MetadataSectionBookTagWriteValidateValue(t *testing.T) {
	md := DefaultMetadata()
	md.BookTags = BookTagWrite{BookTagType: "bogus", AudioTagType: "no"}
	if msg := SectionMetadata.ValidateDocument(md); msg == "" {
		t.Fatal("value-form validate must reject the bad book tag type")
	}
	if !strings.Contains(md.BookTags.Validate(), "book_tag_type") {
		t.Fatal("book tag type error text must name book_tag_type")
	}
}
