package books

import (
	"errors"
	"strings"
	"testing"
)

// TestIsSupportedFormat is a table-driven check of the PLAN §15.4 accepted
// book formats: epub + mobi + azw3 are supported; everything else is not.
func TestIsSupportedFormat(t *testing.T) {
	cases := []struct {
		format string
		want   bool
	}{
		{"epub", true},
		{"mobi", true},
		{"azw3", true},
		// Case-insensitive.
		{"EPUB", true},
		{"Mobi", true},
		{"AZW3", true},
		// Unsupported / physical / non-ebook.
		{"pdf", false},
		{"docx", false},
		{"txt", false},
		{"kindle", false},
		{"", false},
		{"epubx", false},
	}
	for _, c := range cases {
		if got := IsSupportedFormat(c.format); got != c.want {
			t.Errorf("IsSupportedFormat(%q) = %v, want %v", c.format, got, c.want)
		}
	}
}

// TestValidateFormat_TypedError confirms supported formats pass and
// unsupported ones are rejected with a typed error that matches
// ErrUnsupportedFormat and carries the offending format (issue #25: "unsupported
// formats rejected with typed errors").
func TestValidateFormat_TypedError(t *testing.T) {
	if err := ValidateFormat("epub"); err != nil {
		t.Errorf("ValidateFormat(epub) = %v, want nil", err)
	}

	err := ValidateFormat("pdf")
	if err == nil {
		t.Fatal("ValidateFormat(pdf) = nil, want an error")
	}
	if !errors.Is(err, ErrUnsupportedFormat) {
		t.Errorf("errors.Is(%v, ErrUnsupportedFormat) = false, want true", err)
	}
	var uf *UnsupportedFormatError
	if !errors.As(err, &uf) {
		t.Fatalf("errors.As(*UnsupportedFormatError) = false, want true (%T)", err)
	}
	if uf.Format != "pdf" {
		t.Errorf("UnsupportedFormatError.Format = %q, want pdf", uf.Format)
	}
	if msg := uf.Error(); !strings.Contains(msg, "pdf") {
		t.Errorf("Error() = %q, want it to name the offending format", msg)
	}
}

// TestProfileBuiltIn confirms the built-in book format profiles are present and
// name-addressable, ordered best-first.
func TestProfileBuiltIn(t *testing.T) {
	for _, name := range []string{"Best", "EPUB", "Kindle", "Any"} {
		p, ok := ProfileByName(name)
		if !ok {
			t.Fatalf("ProfileByName(%q) not found", name)
		}
		if len(p.Items) == 0 {
			t.Errorf("ProfileByName(%q) has no items", name)
		}
	}
}

// TestParseFormat is a table-driven check of format detection from a scene
// release name or file name.
func TestParseFormat(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		// Bracketed format tag.
		{"The Dispossessed (Le Guin) [epub]", "epub"},
		{"Dune - Frank Herbert [azw3, 896p]", "azw3"},
		{"A Wizard of Earthsea (KF8) [mobi]", "mobi"},
		// File extension.
		{"book.mobi", "mobi"},
		{"The Left Hand of Darkness.epub", "epub"},
		{"/path/to/vol.1 (2020).azw3", "azw3"},
		// Bare token.
		{"The Dispossessed epub", "epub"},
		{"Dune (abridged, mobi edition)", "mobi"},
		// No supported format → undetected. ParseFormat only ever reports the
		// three supported formats, so a pdf/hardcover name is undetected.
		{"The Dispossessed (paperback)", ""},
		{"A Wizard of Earthsea (hardcover, 2019)", ""},
		{"the-dispossessed-1974.pdf", ""},
		{"", ""},
	}
	for _, c := range cases {
		got := ParseFormat(c.name).Format
		if got != c.want {
			t.Errorf("ParseFormat(%q).Format = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestMatchRelease_BestFormatPick confirms that against the default "Best"
// profile the formats rank epub > azw3 > mobi, and that SortReleases orders a
// candidate pool best-first (issue #25: "best-format pick").
func TestMatchRelease_BestFormatPick(t *testing.T) {
	best, _ := ProfileByName("Best")

	var cands []ProfileMatch
	for _, f := range []string{"mobi", "azw3", "epub"} {
		m := MatchRelease(best, ParsedRelease{Format: f})
		if !m.Matched {
			t.Fatalf("%s should match the Best profile: %s", f, m.Reason)
		}
		cands = append(cands, m)
	}

	// Before sorting: mobi, azw3, epub (the order above).
	SortReleases(cands)
	got := []string{cands[0].Format, cands[1].Format, cands[2].Format}
	want := []string{"epub", "azw3", "mobi"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("best-first order = %v, want %v", got, want)
		}
	}
}

// TestMatchRelease_ProfileSpecificity confirms a format profile only accepts
// the formats it lists (issue #25: "per-format ... matching against the book
// profile").
func TestMatchRelease_ProfileSpecificity(t *testing.T) {
	// EPUB-only profile rejects mobi and azw3.
	epubProf, _ := ProfileByName("EPUB")
	if m := MatchRelease(epubProf, ParsedRelease{Format: "epub"}); !m.Matched {
		t.Errorf("epub should match the EPUB profile: %s", m.Reason)
	}
	if m := MatchRelease(epubProf, ParsedRelease{Format: "mobi"}); m.Matched {
		t.Errorf("mobi should NOT match the EPUB profile")
	}
	if m := MatchRelease(epubProf, ParsedRelease{Format: "azw3"}); m.Matched {
		t.Errorf("azw3 should NOT match the EPUB profile")
	}

	// Kindle profile accepts azw3 and mobi but not epub.
	kindle, _ := ProfileByName("Kindle")
	if m := MatchRelease(kindle, ParsedRelease{Format: "azw3"}); !m.Matched {
		t.Errorf("azw3 should match the Kindle profile: %s", m.Reason)
	}
	if m := MatchRelease(kindle, ParsedRelease{Format: "mobi"}); !m.Matched {
		t.Errorf("mobi should match the Kindle profile: %s", m.Reason)
	}
	if m := MatchRelease(kindle, ParsedRelease{Format: "epub"}); m.Matched {
		t.Errorf("epub should NOT match the Kindle profile")
	}

	// Any profile accepts every supported format.
	anyProf, _ := ProfileByName("Any")
	for _, f := range []string{"epub", "mobi", "azw3"} {
		if m := MatchRelease(anyProf, ParsedRelease{Format: f}); !m.Matched {
			t.Errorf("%s should match the Any profile: %s", f, m.Reason)
		}
	}
}

// TestMatchRelease_UnsupportedRejected confirms an unsupported release format
// is not matched (even by an "Any" profile) — unsupported formats are rejected,
// not quietly imported (issue #25: "unsupported formats rejected").
func TestMatchRelease_UnsupportedRejected(t *testing.T) {
	for _, name := range []string{"Any", "Best", "EPUB"} {
		prof, _ := ProfileByName(name)
		if m := MatchRelease(prof, ParsedRelease{Format: "pdf"}); m.Matched {
			t.Errorf("%s: pdf should NOT match profile %q", name, name)
		}
	}
}

// TestSortReleases_BestFirst confirms a mixed-format candidate pool is ordered
// by the profile's preference, with a matched candidate outranking a
// non-matched one.
func TestSortReleases_BestFirst(t *testing.T) {
	best, _ := ProfileByName("Best")
	cands := []ProfileMatch{
		MatchRelease(best, ParsedRelease{Format: "mobi"}),
		MatchRelease(best, ParsedRelease{Format: "azw3"}),
		{Matched: false, Format: "pdf"}, // non-matched sits last
		MatchRelease(best, ParsedRelease{Format: "epub"}),
	}
	SortReleases(cands)
	if cands[0].Format != "epub" {
		t.Errorf("best candidate = %q, want epub", cands[0].Format)
	}
	if !cands[0].Matched {
		t.Error("best candidate should be matched")
	}
	if cands[len(cands)-1].Format != "pdf" || cands[len(cands)-1].Matched {
		t.Errorf("worst candidate = %+v, want the unmatched pdf", cands[len(cands)-1])
	}
}
