package music

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// match.go is the pure music matching + release-parsing layer. It mirrors the
// TV/movie matching patterns (BuildCandidate / BestFor*) but is tuned to audio:
// a release is an "Artist - Album [format tag]" and quality is a format +
// bit-rate rather than a video resolution. Everything here is pure and
// unit-tested; it operates on domain types, never on indexer types, so the
// service wires indexer results into it.
//
// Dependency rule: no database, redis, http, or indexers imports.

// TrackRef identifies a single track by (disc, number), the same specifier used
// by the Track domain type. It is parse-local (no ids) so a parsed release can
// carry which tracks it delivers before any persistence.
type TrackRef struct {
	Disc   int
	Number int
}

// formatTags maps a scene tag to a Format* const. Checked case-insensitively
// against the release name. Lossless tags rank above compressed ones.
var formatTags = []struct {
	tag    string
	format string
}{
	{"flac", FormatFLAC},
	{"ape", FormatAPE},
	{"wavpack", FormatWAV},
	{"wv", FormatWAV},
	{"wav", FormatWAV},
	{"320", FormatMP3}, // "320k" / "320kbps" / "320kbps" → mp3 by default
	{"m4a", FormatM4A},
	{"aac", FormatAAC},
	{"opus", FormatOpus},
	{"mp3", FormatMP3},
}

// bitrateRE matches a bit-rate in a release name: "320k", "320kbps", "320kbps",
// "320 kbps". Captures the number.
var bitrateRE = regexp.MustCompile(`(?i)\b(\d{2,4})\s?kbps?\b`)

// wholeAlbumRE matches release names that are a whole album ("COMPLETE",
// "ALBUM", "FULL ALBUM") rather than a single track.
var wholeAlbumRE = regexp.MustCompile(`(?i)\b(complete|full\s+album|album)\b`)

// trailingTagRE matches a trailing bracketed group ("[...]" or "(...)") at the
// end of a string. The group's inner content is group 1.
var trailingTagRE = regexp.MustCompile(`\s*[(\[]([^\[\]()]+)[)\]]\s*$`)

// qualityTagRE matches the *contents* of a bracketed group that describe audio
// quality (a format, a bit-rate, or a lossless/hi-res keyword) rather than a
// real album subtitle. Used to decide whether a trailing tag should be stripped
// from the album title.
var qualityTagRE = regexp.MustCompile(`(?i)(\bflac\b|\bape\b|\bwavpack\b|\bwv\b|\bwav\b|\bmp3\b|\baac\b|\bm4a\b|\bopus\b|\d+\s?kbps|\d+k\b|lossless|\d+\s?bit|\d+\s?hz|hi-?res|hi-?loss)`)

// stripQualityTag removes a trailing bracketed quality tag from an album title
// so two releases of the same album at different qualities parse to the same
// clean album. A trailing group is only stripped when its content looks like a
// quality descriptor (see qualityTagRE); a real subtitle such as "Legend (1977)"
// is left untouched.
func stripQualityTag(s string) string {
	for {
		m := trailingTagRE.FindStringSubmatchIndex(s)
		if m == nil {
			return strings.TrimSpace(s)
		}
		if qualityTagRE.MatchString(s[m[2]:m[3]]) {
			s = strings.TrimSpace(s[:m[0]] + s[m[1]:])
			continue
		}
		return strings.TrimSpace(s)
	}
}

// ParsedName is the structured parse of a scene release name.
type ParsedName struct {
	Raw    string
	Artist string // best-effort artist (first "part" before a separator)
	Album  string // best-effort album (the part after the artist)
	// Tracks are the (disc, number) pairs detected, when the release names
	// discrete tracks; empty for a whole-album release.
	Tracks []TrackRef
	// WholeAlbum is true when the release is a whole album (no discrete tracks).
	WholeAlbum bool
}

// ParseName parses a scene release name into artist/album + track structure.
// Music scene naming is looser than TV, so the parse is best-effort: it splits
// on the first " - " / "-" / "." boundary into (artist, album) and detects
// explicit track numbers ("01", "1-5"). When no discrete tracks are present,
// the release is treated as a whole album.
func ParseName(name string) ParsedName {
	out := ParsedName{Raw: name}
	s := strings.TrimSpace(name)
	if s == "" {
		return out
	}

	// Best-effort artist/album split on the first " - " (space dash space) or
	// " -" (space dash) boundary. The split offset accounts for the full
	// separator width so no dash leaks into the album.
	if i := strings.Index(s, " - "); i >= 0 {
		out.Artist = strings.TrimSpace(s[:i])
		out.Album = strings.TrimSpace(s[i+3:])
	} else if i := strings.Index(s, " -"); i >= 0 {
		out.Artist = strings.TrimSpace(s[:i])
		out.Album = strings.TrimSpace(s[i+2:])
	} else {
		out.Album = s
	}
	// Strip a trailing bracketed quality tag (e.g. "Legend [FLAC 320kbps]" →
	// "Legend"): the quality is parsed separately by ParseQuality, and the
	// clean album title is what BestForAlbum compares against the monitored
	// album. Without this, two releases of the same album at different
	// qualities would fail to match each other.
	out.Album = stripQualityTag(out.Album)

	// Discrete track detection: "01", "02", "1-5". A leading 2-digit number
	// followed by a non-alphanumeric separator is treated as a track number.
	out.Tracks = detectTracks(s)
	out.WholeAlbum = len(out.Tracks) == 0 && wholeAlbumRE.MatchString(s)
	if !out.WholeAlbum && len(out.Tracks) == 0 {
		// No explicit tracks and no whole-album marker: still deliver as album.
		out.WholeAlbum = true
	}
	return out
}

// detectTracks pulls 1-2 digit track numbers (and "a-b" ranges) out of a
// release name. Returns them sorted, de-duped, disc 1.
func detectTracks(s string) []TrackRef {
	var nums []int
	seen := map[int]bool{}
	for _, tok := range strings.Fields(s) {
		t := strings.Trim(tok, ".-–,;()")
		if r, ok := rangeInt(t); ok {
			addNum(&nums, seen, r)
			continue
		}
		// Range "1-5".
		if i := strings.IndexByte(t, '-'); i > 0 && i < len(t)-1 {
			if lo, ok := rangeInt(t[:i]); ok {
				if hi, ok := rangeInt(t[i+1:]); ok && hi >= lo {
					for n := lo; n <= hi; n++ {
						addNum(&nums, seen, n)
					}
				}
			}
		}
	}
	out := make([]TrackRef, 0, len(nums))
	for _, n := range nums {
		out = append(out, TrackRef{Disc: 1, Number: n})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	return out
}

func addNum(nums *[]int, seen map[int]bool, n int) {
	if n < 1 || seen[n] {
		return
	}
	seen[n] = true
	*nums = append(*nums, n)
}

func rangeInt(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return n, true
}

// ParsedRelease (quality.go) carries the format/bit-rate; ParseQuality extracts
// those from a release name so the service can build a full CandidateRelease.
func ParseQuality(name string) ParsedRelease {
	rel := ParsedRelease{Format: FormatAny}
	n := strings.ToLower(name)
	for _, ft := range formatTags {
		if strings.Contains(n, ft.tag) {
			rel.Format = ft.format
			if losslessFormats[ft.format] {
				rel.Source = "lossless"
			} else {
				rel.Source = "compressed"
			}
			break
		}
	}
	if m := bitrateRE.FindStringSubmatch(n); m != nil {
		if b, err := strconv.Atoi(m[1]); err == nil {
			rel.Bitrate = b
			if rel.Format == FormatAny {
				rel.Format = FormatMP3
			}
		}
	}
	return rel
}

// CandidateRelease is a parsed indexer release plus its quality match against a
// profile. The service constructs one per candidate; the pure helpers below
// reason about them without knowing where they came from.
type CandidateRelease struct {
	// Title is the raw scene release name (for logging/history).
	Title string
	// Indexer is the name of the indexer that produced it (for logging).
	Indexer string
	// Parsed is the structured parse of the release name (artist/album/tracks).
	Parsed ParsedName
	// Quality is the result of matching the release's format/bit-rate against
	// the profile.
	Quality ProfileMatch
	// Score is the ranking score (mirrors Quality.Score).
	Score int
}

// BuildCandidate parses a release name and matches it against a profile,
// returning the candidate and whether it quality-matched.
func BuildCandidate(title, indexer, profileName string) (CandidateRelease, bool) {
	profile, ok := ProfileByName(profileName)
	if !ok {
		profile, _ = ProfileByName("Any")
	}
	parsedName := ParseName(title)
	quality := MatchRelease(profile, ParseQuality(title))
	c := CandidateRelease{
		Title:   title,
		Indexer: indexer,
		Parsed:  parsedName,
		Quality: quality,
		Score:   quality.Score,
	}
	return c, quality.Matched
}

// Normalise is a canonical, lower-cased, non-alphanumeric-stripped form of a
// title used to compare a release's artist/album against a monitored
// artist/album. It is intentionally forgiving so scene naming drift (punctuation,
// "featuring", extra spaces) still matches.
func Normalise(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, ".", " ")
	s = strings.ReplaceAll(s, "!", " ")
	s = strings.ReplaceAll(s, "?", " ")
	return strings.Join(strings.Fields(s), " ")
}

// MatchesArtist reports whether the release's (parsed) artist matches the
// monitored artist name.
func MatchesArtist(rel ParsedName, artistName string) bool {
	if rel.Artist == "" {
		// No artist parsed (e.g. "Album Artist" order or a single part):
		// compare the whole raw name.
		return Normalise(rel.Raw) == Normalise(artistName)
	}
	return Normalise(rel.Artist) == Normalise(artistName)
}

// MatchesAlbum reports whether the release's (parsed) album matches the
// monitored album name.
func MatchesAlbum(rel ParsedName, albumName string) bool {
	return Normalise(rel.Album) == Normalise(albumName)
}

// SortCandidates orders a slice of candidates best-first by score (desc), with
// deterministic tie-breaks: lossless before compressed, then higher bit-rate,
// then lexicographic title.
func SortCandidates(cands []CandidateRelease) {
	sort.SliceStable(cands, func(i, j int) bool {
		a, b := cands[i], cands[j]
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		return a.Title < b.Title
	})
}

// BestForAlbum returns the best candidate (by the same ordering as
// SortCandidates) that matches the given artist + album and delivers it as a
// whole album. ok is false when no candidate matches.
func BestForAlbum(cands []CandidateRelease, artistName, albumName string) (CandidateRelease, bool) {
	var eligible []CandidateRelease
	for _, c := range cands {
		if !MatchesArtist(c.Parsed, artistName) {
			continue
		}
		if !MatchesAlbum(c.Parsed, albumName) {
			continue
		}
		if !c.Parsed.WholeAlbum {
			continue
		}
		eligible = append(eligible, c)
	}
	if len(eligible) == 0 {
		return CandidateRelease{}, false
	}
	SortCandidates(eligible)
	return eligible[0], true
}
