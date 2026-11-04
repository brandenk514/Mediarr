package settings

import (
	"fmt"
)

// ---------------------------------------------------------------------------
// Readarr B3: book quality definitions + book quality/metadata profiles
//
// DATA, NOT TYPES (the L3 convention): the book quality model is a seeded
// data table — book QualityLevel rows and book profile documents — not
// per-format Go types. The release-matching engine lives in
// domains/books (format.go for e-books, quality.go for a-books).
//
// ID space: TV levels 1-10, movie levels 100-112, music levels 200-237,
// book levels 300+ — so a book level id can never collide with a TV, movie
// or music level id. Book quality PROFILE ids also live in the 300+ space
// (the movie/music convention: profile ids and level ids share the kind's
// numeric space; they are distinct concepts referenced in distinct fields).
//
// One unified book level table (the Readarr B3 "quality definitions"):
//   - the e-book format-preference entries EPUB / AZW3 / MOBI (300-302) —
//     e-books have no lossless/lossy axis, they are pure format entries;
//   - the a-book (audiobook) lossless/lossy ladder (310-347), which shares
//     its seed values 1:1 with the music ladder (L3): book a-book level id =
//     music level id + 110, same names, groups and size limits — so books
//     a-books and music profiles speak the same quality language.
// ---------------------------------------------------------------------------

// E-book level ids (the format-preference entries of the unified table).
const (
	BookLevelEPUB = 300
	BookLevelAZW3 = 301
	BookLevelMOBI = 302
)

// A-book level ids (310-347). Each is the matching music level id + 110, so
// the a-book ladder shares its seed values with the music ladder 1:1.
const (
	BookLevelUnknown  = 310 // music 200: Unknown
	BookLevelFLAC     = 316 // music 206
	BookLevelALAC     = 317 // music 207
	BookLevelAAC320   = 321 // music 211
	BookLevelAACVBR   = 322 // music 212
	BookLevelWAV      = 323 // music 213
	BookLevelOGGQ10   = 324 // music 214
	BookLevelOGGQ9    = 325 // music 215
	BookLevelFLAC24   = 331 // music 221
	BookLevelWMA      = 330 // music 220
	BookLevelMP3320   = 314 // music 204
	BookLevelMP3VBRV0 = 312 // music 202
	BookLevelAPE      = 345 // music 235
	BookLevelWavPack  = 346 // music 236
	BookLevelALAC24   = 347 // music 237
	BookLevelMP38     = 342 // music 232 (the ladder's worst level)
)

// bookABookOffset maps a music level id (200+) to its book a-book twin (310+).
const bookABookOffset = 110

// bookEBookLevels are the e-book format-preference entries, best first
// (the canonical e-book portability ranking, the same order as the domain
// formatRank in domains/books).
var bookEBookLevels = []QualityLevel{
	{ID: BookLevelEPUB, Name: "EPUB", Group: "E-book", SizeLimitMB: 0},
	{ID: BookLevelAZW3, Name: "AZW3", Group: "E-book", SizeLimitMB: 0},
	{ID: BookLevelMOBI, Name: "MOBI", Group: "E-book", SizeLimitMB: 0},
}

// DefaultBookLevels returns the unified book quality definitions (41 rows):
// the e-book format-preference entries, then the a-book lossless/lossy
// ladder — shared 1:1 with the music ladder (L3), i.e. the same names,
// groups, size limits and order, re-based into the book 300+ id space
// (music id + 110). Unknown stays last, as in the music ladder.
func DefaultBookLevels() []QualityLevel {
	out := make([]QualityLevel, 0, len(bookEBookLevels)+len(DefaultMusicLevels()))
	out = append(out, bookEBookLevels...)
	for _, m := range DefaultMusicLevels() {
		out = append(out, QualityLevel{
			ID:          m.ID + bookABookOffset,
			Name:        m.Name,
			Group:       m.Group,
			SizeLimitMB: m.SizeLimitMB,
		})
	}
	return out
}

// seedBookLevelIDs returns the seeded book level ids — the reference set
// book profile items/cutoffs are validated against.
func seedBookLevelIDs() map[int]bool {
	out := map[int]bool{}
	for _, l := range DefaultBookLevels() {
		out[l.ID] = true
	}
	return out
}

// DefaultBookQualityProfile is the Readarr default book quality profile
// name; an empty quality profile on an add resolves to it. The name matches
// the domain built-in ("Best"), so existing author rows that store profile
// names stay compatible.
const DefaultBookQualityProfile = "Best"

// bookProfileSpec is one seeded book profile: id (the 300+ space), name, the
// approved book level ids, and the cutoff level id.
type bookProfileSpec struct {
	id      int
	name    string
	approve []int
	cutoff  int
}

// allBookLevelIDsExcept returns the seeded book level ids in table order,
// optionally dropping a single id (the "Best" vs "Any" split: Best approves
// every real quality, Any additionally approves Unknown).
func allBookLevelIDsExcept(exclude ...int) []int {
	skip := map[int]bool{}
	for _, e := range exclude {
		skip[e] = true
	}
	var out []int
	for _, l := range DefaultBookLevels() {
		if !skip[l.ID] {
			out = append(out, l.ID)
		}
	}
	return out
}

// defaultBookProfileSpecs are the 4 Readarr default book quality profiles.
// The e-book profiles mirror the domain built-ins (Best / EPUB / Kindle);
// "Any" approves everything including Unknown. Cutoffs sit at the profile's
// best approved level (the TV/movie convention) — "Any" keeps the cutoff at
// Unknown so nothing is ever rejected.
var defaultBookProfileSpecs = []bookProfileSpec{
	{300, "Best", allBookLevelIDsExcept(BookLevelUnknown), BookLevelEPUB},
	{301, "EPUB", []int{BookLevelEPUB}, BookLevelEPUB},
	{302, "Kindle", []int{BookLevelAZW3, BookLevelMOBI}, BookLevelAZW3},
	{303, "Any", allBookLevelIDsExcept(), BookLevelUnknown},
}

// DefaultBookProfiles returns the seeded book quality profiles (ids in the
// 300+ space, items referencing book level ids). "Best" is first — the same
// convention as the seeded movie/music profiles.
func DefaultBookProfiles() []QualityProfile {
	out := make([]QualityProfile, 0, len(defaultBookProfileSpecs))
	for _, s := range defaultBookProfileSpecs {
		prof := QualityProfile{ID: s.id, Name: s.name, Cutoff: s.cutoff}
		prof.Items = make([]QualityProfileItem, 0, len(s.approve))
		for _, q := range s.approve {
			prof.Items = append(prof.Items, QualityProfileItem{Quality: q, Approved: true})
		}
		out = append(out, prof)
	}
	return out
}

// BookProfileNames returns the seeded book profile names — the set an
// add-author quality_profile parameter is validated against (B3).
func BookProfileNames() map[string]bool {
	out := map[string]bool{}
	for _, p := range DefaultBookProfiles() {
		out[p.Name] = true
	}
	return out
}

// ---------------------------------------------------------------------------
// Readarr B3: book metadata profiles (the Readarr "metadata profile"
// content filters)
//
// Unlike the Lidarr metadata profiles (which metadata FILES to generate),
// a book metadata profile filters which BOOKS are eligible: minimum
// popularity, skip no-release-date / no-ISBN-ASIN / broken parts /
// secondary-series books, allowed languages. B3 ships the model + listing
// (seeded in the profiles document, like the music metadata profiles);
// applying the filters to search results is the separate MT4 work item.
// ---------------------------------------------------------------------------

// BookMetadataProfile is one named set of book metadata filters.
type BookMetadataProfile struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// MinimumPopularity is the lowest book popularity score accepted;
	// 0 = no minimum.
	MinimumPopularity int `json:"minimum_popularity"`
	// SkipMissingReleaseDate skips books with no release date.
	SkipMissingReleaseDate bool `json:"skip_missing_release_date"`
	// SkipNoISBNASIN skips books with no ISBN/ASIN.
	SkipNoISBNASIN bool `json:"skip_no_isbn_asin"`
	// SkipBrokenParts skips books that are a part of a broken (incomplete)
	// set.
	SkipBrokenParts bool `json:"skip_broken_parts"`
	// SkipSecondarySeries skips books whose series membership is only
	// secondary.
	SkipSecondarySeries bool `json:"skip_secondary_series"`
	// AllowedLanguages restricts to these languages (ISO codes); empty =
	// any language.
	AllowedLanguages []string `json:"allowed_languages"`
}

// DefaultBookMetadataProfiles returns the seeded book metadata profile: a
// single "Default" with no filters (every book eligible) — matching Readarr's
// factory default and the author rows that already store "Default".
func DefaultBookMetadataProfiles() []BookMetadataProfile {
	return []BookMetadataProfile{{
		ID:               1,
		Name:             "Default",
		Description:      "No filters: every book is eligible.",
		AllowedLanguages: []string{},
	}}
}

// BookMetadataProfileNames returns the seeded book metadata profile names —
// the set an add-author metadata_profile parameter is validated against
// (B3).
func BookMetadataProfileNames() map[string]bool {
	out := map[string]bool{}
	for _, p := range DefaultBookMetadataProfiles() {
		out[p.Name] = true
	}
	return out
}

// validateBookMetadataProfiles applies the per-profile rules of a
// book-metadata-profiles array and returns a prefixed error message ("" when
// valid).
func validateBookMetadataProfiles(profiles []BookMetadataProfile) string {
	if msg := validateNamedIDs("book_metadata_profiles", len(profiles), func(i int) (int, string) {
		return profiles[i].ID, profiles[i].Name
	}); msg != "" {
		return msg
	}
	for i, p := range profiles {
		if p.MinimumPopularity < 0 {
			return fmt.Sprintf("book_metadata_profiles[%d]: minimum_popularity must be >= 0", i)
		}
	}
	return ""
}
