// format.go carries the books format-rule machinery (issue #25, PLAN §15.4).
// It is pure and testable without any I/O: nothing in this package imports
// database, redis, or http.
//
// Unlike music, where a quality profile ladders lossless vs. lossy by
// bit-rate, book formats have no lossless/lossy axis — epub, mobi, and azw3 are
// all "e-books" of comparable fidelity. So a books profile is a *set of allowed
// formats* plus a canonical *format preference*, and matching reduces to
// "is this release's format supported, is it allowed by the profile, and —
// among the candidates — which is the best format?".
//
// PLAN §15.4 fixes the supported set as epub + mobi + azw3 (full Readarr
// parity). Physical editions (paperback / hardcover / audiobook) and non-ebook
// documents (pdf, docx, txt) are deliberately NOT supported: they cannot be
// matched, searched, or imported by the pipeline, so they are rejected with a
// typed error rather than silently imported (issue #25: "unsupported formats
// rejected with typed errors").
//
// Edition disambiguation. The format is what disambiguates one edition of a
// title from another: a "paperback" and an "e-book" of the same title are
// different editions, but only the e-book editions (epub/mobi/azw3) are
// addressable by this matcher. A "paperback"/"hardcover" token therefore means
// "physical edition, not importable" and ParseFormat reports it as no format
// (""), which the pipeline treats as NoMatch. This is the edition
// disambiguation the issue asks us to document: the format string is the edition
// discriminator for the ebook subset.
package books

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Supported book formats (PLAN §15.4).
const (
	FormatEPUB = "epub"
	FormatMOBI = "mobi"
	FormatAZW3 = "azw3"
	// FormatAny is the wildcard profile item that accepts every supported format.
	FormatAny = "any"
)

// ErrUnsupportedFormat is returned (and wrapped) whenever a book format is not
// in the supported set. Callers use errors.Is / errors.As on it.
var ErrUnsupportedFormat = errors.New("books: unsupported book format")

// UnsupportedFormatError is the typed, inspectable error for a rejected
// format. It carries the offending format so the UI / API can name it.
type UnsupportedFormatError struct {
	Format string
}

func (e *UnsupportedFormatError) Error() string {
	return fmt.Sprintf("books: unsupported book format %q (supported: %s, %s, %s)",
		e.Format, FormatEPUB, FormatMOBI, FormatAZW3)
}

// Unwrap makes errors.Is(err, ErrUnsupportedFormat) true.
func (e *UnsupportedFormatError) Unwrap() error { return ErrUnsupportedFormat }

// IsSupportedFormat reports whether format is one of the PLAN §15.4 book
// formats. Matching is case-insensitive and trims surrounding whitespace.
func IsSupportedFormat(format string) bool {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case FormatEPUB, FormatMOBI, FormatAZW3:
		return true
	}
	return false
}

// ValidateFormat returns nil if the format is supported, or a typed
// *UnsupportedFormatError (matching ErrUnsupportedFormat) otherwise.
func ValidateFormat(format string) error {
	if IsSupportedFormat(format) {
		return nil
	}
	return &UnsupportedFormatError{Format: format}
}

// formatRank is the canonical book-format preference, best first. It is
// independent of any profile: it answers "given two supported-format releases,
// which is the better one?" The order follows the usual e-book portability
// ranking — EPUB is the most open/forward-compatible, KF8/azw3 next, and the
// older Kindle mobi last.
var formatRank = map[string]int{
	FormatEPUB: 0,
	FormatAZW3: 1,
	FormatMOBI: 2,
}

// Profile is a book format profile: the ordered set of formats a release must
// be in to be acceptable. "Best" is the default for monitored titles (the
// PLAN's default-profile concept); it accepts all supported formats and ranks
// them best-first.
type Profile struct {
	ID    int64
	Name  string
	Items []ProfileItem // ordered best-first
}

// ProfileItem is a single selectable format in a profile.
type ProfileItem struct {
	ID     int64
	Format string // FormatEPUB / FormatMOBI / FormatAZW3, or FormatAny
	Rank   int    // profile-internal order (lower = better)
}

// BuiltInProfiles are the default book format profiles. "Best" is the default.
func BuiltInProfiles() []Profile {
	return []Profile{
		{Name: "Best", Items: []ProfileItem{
			{ID: 1, Format: FormatEPUB, Rank: 0},
			{ID: 2, Format: FormatAZW3, Rank: 1},
			{ID: 3, Format: FormatMOBI, Rank: 2},
		}},
		{Name: "EPUB", Items: []ProfileItem{
			{ID: 1, Format: FormatEPUB, Rank: 0},
		}},
		{Name: "Kindle", Items: []ProfileItem{
			{ID: 1, Format: FormatAZW3, Rank: 0},
			{ID: 2, Format: FormatMOBI, Rank: 1},
		}},
		{Name: "Any", Items: []ProfileItem{
			{ID: 1, Format: FormatAny, Rank: 0},
		}},
	}
}

// ProfileByName returns a built-in profile by name.
func ProfileByName(name string) (Profile, bool) {
	for _, p := range BuiltInProfiles() {
		if p.Name == name {
			return p, true
		}
	}
	return Profile{}, false
}

// ParsedRelease is the result of parsing a scene release name / file name into
// a book format.
type ParsedRelease struct {
	Format string // a Format* const, or "" if no supported format was detected
	Source string // "extension" / "tag" (where the format was found) or ""
}

// formatTokens are the regexes used to detect a format token in a release name,
// checked best-first. Word boundaries keep "mobi" from matching inside a longer
// word, and the extension is handled separately by ParseFormat.
var (
	azw3Re = regexp.MustCompile(`(?i)\bazw3\b`)
	mobiRe = regexp.MustCompile(`(?i)\bmobi\b`)
	epubRe = regexp.MustCompile(`(?i)\bepub\b`)
)

// ParseFormat detects the book format from a release or file name. A file
// extension (.epub / .mobi / .azw3) wins; otherwise a format token in the name
// is used, best-first. Returns Format "" when no supported format is present —
// e.g. a "paperback" / "hardcover" / ".pdf" name (see edition disambiguation in
// the package docs).
func ParseFormat(name string) ParsedRelease {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return ParsedRelease{}
	}

	// 1. File extension: the substring after the last dot in the basename.
	if slash := strings.LastIndex(name, "/"); slash >= 0 {
		name = name[slash+1:]
	}
	if dot := strings.LastIndex(name, "."); dot >= 0 && dot < len(name)-1 {
		if ext := IsSupportedFormat(name[dot+1:]); ext {
			return ParsedRelease{Format: name[dot+1:], Source: "extension"}
		}
	}

	// 2. Format token in the name, best-first.
	if epubRe.MatchString(name) {
		return ParsedRelease{Format: FormatEPUB, Source: "tag"}
	}
	if azw3Re.MatchString(name) {
		return ParsedRelease{Format: FormatAZW3, Source: "tag"}
	}
	if mobiRe.MatchString(name) {
		return ParsedRelease{Format: FormatMOBI, Source: "tag"}
	}
	return ParsedRelease{}
}

// ProfileMatch is the result of matching a parsed release against a profile.
type ProfileMatch struct {
	Matched bool
	Item    ProfileItem
	Format  string
	// Score ranks candidates best-first (higher is better); a matched release
	// always outranks an unmatched one.
	Score  int
	Reason string
}

// MatchRelease evaluates a parsed release against a profile. A release matches
// if its format is supported AND allowed by some profile item (a FormatAny item
// allows every supported format). Among matching items the lowest-ranked (best)
// wins. An unsupported or undetected format never matches.
func MatchRelease(p Profile, rel ParsedRelease) ProfileMatch {
	if !IsSupportedFormat(rel.Format) {
		return ProfileMatch{
			Matched: false,
			Format:  rel.Format,
			Reason:  "unsupported or undetected format: " + rel.Format,
		}
	}

	best := ProfileMatch{Format: rel.Format}
	bestFound := false
	for _, item := range p.Items {
		if !itemMatches(item, rel) {
			continue
		}
		if !bestFound || item.Rank < best.Item.Rank {
			best = ProfileMatch{
				Matched: true,
				Item:    item,
				Format:  rel.Format,
				Score:   scoreFor(rel.Format),
				Reason:  describeMatch(item, rel),
			}
			bestFound = true
		}
	}
	return best
}

func itemMatches(item ProfileItem, rel ParsedRelease) bool {
	if item.Format == FormatAny {
		return true
	}
	return rel.Format == item.Format
}

// scoreFor ranks a matched release by its format's canonical preference. A large
// base keeps every matched release strictly above an unmatched one (score 0).
func scoreFor(format string) int {
	const base = 1_000_000
	rank, ok := formatRank[strings.ToLower(format)]
	if !ok {
		return 0
	}
	return base - rank
}

func describeMatch(item ProfileItem, rel ParsedRelease) string {
	if item.Format == FormatAny {
		return "format=any (release " + rel.Format + ")"
	}
	return "format=" + item.Format
}

// SortReleases orders candidate matches best-first by score (desc), stable for
// equal scores. Matched releases always sort before unmatched ones.
func SortReleases(matches []ProfileMatch) {
	sort.SliceStable(matches, func(i, j int) bool {
		return matches[i].Score > matches[j].Score
	})
}
