package tv

import (
	"fmt"
	"sort"
	"strings"
)

// match.go is the pure TV matching + release-grouping layer. It reuses the M1
// quality-matching patterns (MatchRelease / ProfileMatch) and adds the TV
// specifics: which episodes a release covers, grouping multi-episode releases
// together, and picking the best release per group. Everything here is pure and
// unit-tested; it operates on ParsedRelease (a domain type), never on the
// indexer types, so the service wires indexer results into it.
//
// Dependency rule: no database, redis, http, or indexers imports.

// EpisodeRef identifies a single episode by (season, episode).
type EpisodeRef struct {
	Season  int
	Episode int
}

// Key returns the canonical, sortable key for an episode (e.g. "S01E02").
func (e EpisodeRef) Key() string {
	return fmt.Sprintf("S%02dE%02d", e.Season, e.Episode)
}

// String returns a human label (e.g. "S01/E02").
func (e EpisodeRef) String() string {
	return fmt.Sprintf("S%02d/E%02d", e.Season, e.Episode)
}

// NewEpisodeRef builds an EpisodeRef from a season and episode number.
func NewEpisodeRef(season, episode int) EpisodeRef {
	return EpisodeRef{Season: season, Episode: episode}
}

// CandidateRelease is a parsed indexer release plus its quality match against a
// profile. The service constructs one per candidate; the pure helpers below
// reason about them without knowing where they came from.
type CandidateRelease struct {
	// Title is the raw scene release name (for logging/history).
	Title string
	// Indexer is the name of the indexer that produced it (for logging).
	Indexer string
	// Parsed is the structured parse of the release name.
	Parsed ParsedRelease
	// Quality is the result of matching Parsed against the profile.
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
	parsed, err := ParseRelease(title)
	if err != nil {
		return CandidateRelease{}, false
	}
	match := MatchRelease(profile, parsed)
	c := CandidateRelease{
		Title:   title,
		Indexer: indexer,
		Parsed:  parsed,
		Quality: match,
		Score:   match.Score,
	}
	return c, match.Matched
}

// CoveredEpisodes expands a parsed release into the concrete episodes it
// delivers. A single-episode release yields one; a multi-episode release
// (S01E01E02) yields several; a season pack (no discrete episodes) yields none
// — season packs are handled by IsSeasonPack instead, because the episode count
// is unknown without provider metadata.
func CoveredEpisodes(p ParsedRelease) []EpisodeRef {
	if p.Season == 0 {
		return nil
	}
	out := make([]EpisodeRef, 0, len(p.Episodes))
	for _, e := range p.Episodes {
		out = append(out, NewEpisodeRef(p.Season, e))
	}
	return out
}

// IsSeasonPack reports whether the release is a whole-season pack with no
// discrete episode list (e.g. "Show.S01.COMPLETE").
func IsSeasonPack(p ParsedRelease) bool {
	return p.Season != 0 && p.SeasonPack && len(p.Episodes) == 0
}

// Covers reports whether the release delivers the given episode. For a
// season-pack release it returns true for any episode in the matching season.
func Covers(p ParsedRelease, season, episode int) bool {
	if p.Season != season {
		return false
	}
	if len(p.Episodes) > 0 {
		for _, e := range p.Episodes {
			if e == episode {
				return true
			}
		}
		return false
	}
	// No discrete episodes: a season pack covers the whole season.
	return p.SeasonPack
}

// CoverageKey is the canonical identifier of the set of episodes a release
// covers. Releases that cover the same set belong to the same group. A season
// pack gets a distinct key ("<season>:pack") so it groups with other packs of
// the same season rather than with single-episode releases.
func CoverageKey(p ParsedRelease) string {
	if p.Season == 0 {
		return "none"
	}
	if IsSeasonPack(p) {
		return fmt.Sprintf("S%02d:pack", p.Season)
	}
	eps := append([]int(nil), p.Episodes...)
	sort.Ints(eps)
	var b strings.Builder
	b.WriteString("S")
	fmt.Fprintf(&b, "%02d", p.Season)
	for _, e := range eps {
		fmt.Fprintf(&b, "E%02d", e)
	}
	return b.String()
}

// GroupCandidates groups candidate releases by the episodes they cover. A
// multi-episode release (S01E01E02) forms its own group distinct from two
// separate single-episode releases of the same episodes, so a single physical
// download can satisfy several wanted entries at once.
func GroupCandidates(cands []CandidateRelease) map[string][]CandidateRelease {
	groups := make(map[string][]CandidateRelease)
	for _, c := range cands {
		key := CoverageKey(c.Parsed)
		groups[key] = append(groups[key], c)
	}
	return groups
}

// SortCandidates orders a slice of candidates best-first by score (desc), with
// deterministic tie-breaks: more covered episodes first (a multi-ep release that
// satisfies more wanted items wins on a score tie), then lexicographic title.
func SortCandidates(cands []CandidateRelease) {
	sort.SliceStable(cands, func(i, j int) bool {
		a, b := cands[i], cands[j]
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		na, nb := len(a.Parsed.Episodes), len(b.Parsed.Episodes)
		if na != nb {
			return na > nb
		}
		return a.Title < b.Title
	})
}

// BestInGroup returns the highest-scoring release in a group. Tie-breaks are
// the same as SortCandidates (more episodes, then title) and are documented
// here per PLAN §14: score (quality) is the primary ordering, episode coverage
// second, and title last for determinism.
func BestInGroup(group []CandidateRelease) (CandidateRelease, bool) {
	if len(group) == 0 {
		return CandidateRelease{}, false
	}
	sorted := append([]CandidateRelease(nil), group...)
	SortCandidates(sorted)
	return sorted[0], true
}

// BestForEpisode returns the best candidate (by score, with the same tie-breaks)
// that covers the given season/episode. A season-pack release covering the
// season is eligible. ok is false when no candidate covers the episode.
func BestForEpisode(cands []CandidateRelease, season, episode int) (CandidateRelease, bool) {
	var eligible []CandidateRelease
	for _, c := range cands {
		if Covers(c.Parsed, season, episode) {
			eligible = append(eligible, c)
		}
	}
	if len(eligible) == 0 {
		return CandidateRelease{}, false
	}
	SortCandidates(eligible)
	return eligible[0], true
}
