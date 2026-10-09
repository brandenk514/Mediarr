package books

import (
	"regexp"
	"strings"
)

// match.go is the pure books matching + release-parsing layer. It mirrors the
// music/TV/movie matching patterns (BuildCandidate / BestFor*) but is tuned to
// books: a release is an "Author - Title [format tag]" and the matching axis is
// the e-book format (via the profile machinery in format.go), not an audio
// bit-rate. Everything here is pure and unit-tested; it operates on domain
// types, never on indexer types, so the service wires indexer results into it.
//
// Dependency rule: no database, redis, http, or indexers imports.

// trailingTagRE matches a trailing bracketed group ("[...]" or "(...)") at the
// end of a string. The group's inner content is group 1.
var trailingTagRE = regexp.MustCompile(`\s*[(\[](?P<inner>[^\[\]()]+)[)\]]\s*$`)

// formatTagRE matches the *contents* of a trailing bracketed group that
// describe a book format (an e-book extension or a physical-edition keyword)
// rather than a real title subtitle. Used to decide whether a trailing tag
// should be stripped from the title.
var formatTagRE = regexp.MustCompile(`(?i)(\bepub\b|\bmobi\b|\bazw3\b|\bpaperback\b|\bhardcover\b|\be-?book\b|\baudiobook\b|\bdigital\b|\bprint\b)`)

// stripFormatTag removes a trailing bracketed format tag from a title so two
// releases of the same title in different formats parse to the same clean
// title. A trailing group is only stripped when its content looks like a
// format descriptor (see formatTagRE); a real subtitle is left untouched.
func stripFormatTag(s string) string {
	for {
		loc := trailingTagRE.FindStringSubmatchIndex(s)
		if loc == nil {
			return strings.TrimSpace(s)
		}
		if formatTagRE.MatchString(s[loc[2]:loc[3]]) {
			s = strings.TrimSpace(s[:loc[0]] + s[loc[1]:])
			continue
		}
		return strings.TrimSpace(s)
	}
}

// ParsedName is the structured parse of a scene release name.
type ParsedName struct {
	Raw    string
	Author string // best-effort author (first part before a separator)
	Title  string // best-effort title (the part after the author)
}

// ParseName parses a scene release name into author/title. Book scene naming
// is looser than TV, so the parse is best-effort: it splits on the first
// " - " / "-" boundary into (author, title) and strips a trailing format tag
// from the title.
func ParseName(name string) ParsedName {
	out := ParsedName{Raw: name}
	s := strings.TrimSpace(name)
	if s == "" {
		return out
	}

	if i := strings.Index(s, " - "); i >= 0 {
		out.Author = strings.TrimSpace(s[:i])
		out.Title = strings.TrimSpace(s[i+3:])
	} else if i := strings.Index(s, " -"); i >= 0 {
		out.Author = strings.TrimSpace(s[:i])
		out.Title = strings.TrimSpace(s[i+2:])
	} else {
		out.Title = s
	}
	// Strip a trailing bracketed format tag (e.g. "The Dispossessed [EPUB]"
	// → "The Dispossessed"): the format is parsed separately by ParseFormat,
	// and the clean title is what BestForTitle compares against the monitored
	// title.
	out.Title = stripFormatTag(out.Title)
	return out
}

// CandidateRelease is a parsed indexer release plus its format match against a
// profile. The service constructs one per candidate; the pure helpers below
// reason about them without knowing where they came from.
type CandidateRelease struct {
	// Title is the raw scene release name (for logging/history).
	Title string
	// Indexer is the name of the indexer that produced it (for logging).
	Indexer string
	// Parsed is the structured parse of the release name (author/title).
	Parsed ParsedName
	// Format is the result of matching the release's e-book format against the
	// profile.
	Format ProfileMatch
	// Score is the ranking score (mirrors ProfileMatch.Score).
	Score int
}

// BuildCandidate parses a release name and matches it against a profile,
// returning the candidate and whether it format-matched.
func BuildCandidate(title, indexer, profileName string) (CandidateRelease, bool) {
	profile, ok := ProfileByName(profileName)
	if !ok {
		profile, _ = ProfileByName("Best")
	}
	parsed := ParseName(title)
	m := MatchRelease(profile, ParseFormat(title))
	c := CandidateRelease{
		Title:   title,
		Indexer: indexer,
		Parsed:  parsed,
		Format:  m,
		Score:   m.Score,
	}
	return c, m.Matched
}

// Normalise is a canonical, lower-cased form of a name used to compare a
// release's author/title against a monitored author/title. It is intentionally
// forgiving so scene naming drift (punctuation, extra spaces) still matches.
func Normalise(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, ".", " ")
	s = strings.ReplaceAll(s, "!", " ")
	s = strings.ReplaceAll(s, "?", " ")
	return strings.Join(strings.Fields(s), " ")
}

// MatchesAuthor reports whether the release's (parsed) author matches the
// monitored author name.
func MatchesAuthor(rel ParsedName, authorName string) bool {
	if rel.Author == "" {
		// No author parsed (single part): compare the whole raw name.
		return Normalise(rel.Raw) == Normalise(authorName)
	}
	return Normalise(rel.Author) == Normalise(authorName)
}

// MatchesTitle reports whether the release's (parsed) title matches the
// monitored title name.
func MatchesTitle(rel ParsedName, titleName string) bool {
	return Normalise(rel.Title) == Normalise(titleName)
}

// SortCandidates orders a slice of candidates best-first by score (desc), with
// a deterministic lexicographic title tie-break.
func SortCandidates(cands []CandidateRelease) {
	sortCandidates(cands)
}

// BestForTitle returns the best candidate (by the same ordering as
// SortCandidates) that matches the given author + title. ok is false when no
// candidate matches.
func BestForTitle(cands []CandidateRelease, authorName, titleName string) (CandidateRelease, bool) {
	var eligible []CandidateRelease
	for _, c := range cands {
		if !MatchesAuthor(c.Parsed, authorName) {
			continue
		}
		if !MatchesTitle(c.Parsed, titleName) {
			continue
		}
		eligible = append(eligible, c)
	}
	if len(eligible) == 0 {
		return CandidateRelease{}, false
	}
	sortCandidates(eligible)
	return eligible[0], true
}

func sortCandidates(cands []CandidateRelease) {
	n := len(cands)
	for i := 0; i < n; i++ {
		best := i
		for j := i + 1; j < n; j++ {
			if cands[j].Score > cands[best].Score ||
				(cands[j].Score == cands[best].Score && cands[j].Title < cands[best].Title) {
				best = j
			}
		}
		if best != i {
			cands[i], cands[best] = cands[best], cands[i]
		}
	}
}
