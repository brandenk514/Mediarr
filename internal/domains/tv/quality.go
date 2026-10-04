package tv

import (
	"sort"
)

// quality.go carries the TV quality-profile machinery. It mirrors the M1 movie
// quality-matching patterns (PLAN §14: "reuses M1 quality-matching patterns
// where they fit") but lives in the tv package so the two domains stay decoupled
// and independently testable.
//
// Dependency rule: nothing in this package imports database, redis, or http
// packages. Everything here is pure and testable without any I/O.

// Codec, audio, and source identifiers used in quality profiles.
const (
	CodecAny   = "any"
	CodecH264  = "h264"
	CodecH265  = "h265"
	CodecAV1   = "av1"
	CodecMPEG2 = "mpeg2"

	AudioAny  = "any"
	SourceAny = "any"
)

// Resolution is a discrete video resolution in a quality profile.
type Resolution struct {
	// Name is the canonical label, e.g. "2160p", "1080p".
	Name string
	// MinHeight is the minimum frame height (px) a release must meet.
	MinHeight int
}

// Resolutions ordered best-first (highest first). A release's parsed height is
// matched against the highest profile entry it satisfies.
var Resolutions = []Resolution{
	{Name: "2160p", MinHeight: 2160},
	{Name: "1440p", MinHeight: 1440},
	{Name: "1080p", MinHeight: 1080},
	{Name: "720p", MinHeight: 720},
	{Name: "576p", MinHeight: 576},
	{Name: "480p", MinHeight: 480},
	{Name: "sd", MinHeight: 0},
}

// Profile is a quality profile: a set of quality items, each with an optional
// codec constraint. A release matches if it satisfies any one of them; the
// highest matching item wins for ranking.
type Profile struct {
	ID        int64
	Name      string
	Items     []ProfileItem // ordered best-first
	CutoffID  int64         // item ID below which we stop wanting (0 = none)
	UseCutoff bool
}

// ProfileItem is a single selectable quality in a profile.
type ProfileItem struct {
	ID      int64
	Quality string // a resolution name (see Resolutions) or "any"
	Codec   string // CodecAny means no restriction
	Rank    int    // profile-internal order (lower = better)
}

// BuiltInProfiles are the default quality profiles, in the same order as movies
// so a single default name ("HD-1080p") means the same thing across domains.
func BuiltInProfiles() []Profile {
	return []Profile{
		{Name: "HD-1080p", Items: qualityItems(
			"1080p", CodecAny, 0,
			"720p", CodecAny, 1,
		)},
		{Name: "Full-1080p", Items: qualityItems(
			"1080p", CodecH264, 0,
			"1080p", CodecH265, 1,
			"720p", CodecAny, 2,
		)},
		{Name: "HD-Any", Items: qualityItems(
			"1080p", CodecAny, 0,
			"720p", CodecAny, 1,
			"576p", CodecAny, 2,
		)},
		{Name: "UHD-2160p", Items: qualityItems(
			"2160p", CodecAny, 0,
			"1080p", CodecAny, 1,
		)},
		{Name: "Any", Items: qualityItems(
			"any", CodecAny, 0,
		)},
	}
}

func qualityItems(kv ...any) []ProfileItem {
	var items []ProfileItem
	for i := 0; i+2 < len(kv); i += 3 {
		items = append(items, ProfileItem{
			ID:      int64(i/3 + 1),
			Quality: kv[i].(string),
			Codec:   kv[i+1].(string),
			Rank:    kv[i+2].(int),
		})
	}
	return items
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

// ParsedRelease is the result of parsing a scene release name into TV fields.
type ParsedRelease struct {
	// Title is the cleaned series title (best-effort).
	Title string
	// Year, if present (e.g. 2019).
	Year int
	// Season is the detected season number.
	Season int
	// Episodes are the individual episode numbers detected (S01E01E02 ->
	// [1, 2]; S01E01-04 -> [1, 2, 3, 4]). Empty for a season pack.
	Episodes []int
	// SeasonPack is true when the release is a whole-season pack (e.g.
	// "S01.COMPLETE") rather than discrete episodes.
	SeasonPack bool
	// Part is a multi-part part number (Part 1 of 2) when detected; 0 = n/a.
	Part int
	// Height in px (e.g. 1080). 0 if undetected.
	Height int
	// Codec, if detected (e.g. "h265"). CodecAny/empty if undetected.
	Codec string
	// Source hints, e.g. "blu-ray", "web", "hdtv".
	Source string
	// QualityName is the resolved resolution label (see Resolutions).
	QualityName string
}

// ProfileMatch is the result of matching a parsed release against a profile.
type ProfileMatch struct {
	Matched     bool
	Item        ProfileItem
	QualityName string
	// Score is a ranking score (higher is better) used to pick the best
	// release among candidates. Resolution dominates, then codec fit.
	Score  int
	Reason string
}

// MatchRelease evaluates a parsed release against a profile and returns the
// best matching item (or Matched=false).
func MatchRelease(p Profile, rel ParsedRelease) ProfileMatch {
	relQuality := qualityNameForHeight(rel.Height)
	if rel.QualityName == "" {
		rel.QualityName = relQuality
	}

	best := ProfileMatch{QualityName: relQuality}
	bestFound := false

	for _, item := range p.Items {
		if !itemMatches(item, rel, relQuality) {
			continue
		}
		score := scoreFor(item, rel, relQuality)
		if !bestFound || score > best.Score {
			best = ProfileMatch{
				Matched:     true,
				Item:        item,
				QualityName: relQuality,
				Score:       score,
				Reason:      describeMatch(item, rel, relQuality),
			}
			bestFound = true
		}
	}
	return best
}

func itemMatches(item ProfileItem, rel ParsedRelease, relQuality string) bool {
	if item.Quality == "any" {
		return codecOK(item.Codec, rel.Codec)
	}
	// A specific quality item matches if the release's quality is at or above
	// the item's quality (we accept better-than-requested).
	if qualityRank(relQuality) < qualityRank(item.Quality) {
		return false
	}
	return codecOK(item.Codec, rel.Codec)
}

func codecOK(want, got string) bool {
	if want == "" || want == CodecAny {
		return true
	}
	return got == want
}

// qualityRank returns an order where higher = better. Unknown -> 0.
func qualityRank(name string) int {
	for i := range Resolutions { // Resolutions is best-first
		if Resolutions[i].Name == name {
			return len(Resolutions) - i
		}
	}
	return 0
}

func qualityNameForHeight(h int) string {
	for _, r := range Resolutions { // best-first
		if h >= r.MinHeight {
			return r.Name
		}
	}
	return "sd"
}

// scoreFor builds a ranking score: resolution dominates (x1000), the matched
// item's own quality tier (x10) breaks ties, and a +10 codec-fit bonus.
func scoreFor(item ProfileItem, rel ParsedRelease, relQuality string) int {
	score := qualityRank(relQuality) * 1000
	score += qualityRank(item.Quality) * 10
	if item.Codec == CodecAny || item.Codec == rel.Codec {
		score += 10
	}
	return score
}

func describeMatch(item ProfileItem, rel ParsedRelease, relQuality string) string {
	return "quality=" + relQuality + " item=" + item.Quality + "/" + item.Codec
}

// SortReleases orders candidate matches best-first by score (desc).
func SortReleases(matches []ProfileMatch) {
	sort.SliceStable(matches, func(i, j int) bool {
		return matches[i].Score > matches[j].Score
	})
}

func atoiSafe(s string) (int, error) {
	var n int
	for _, c := range s {
		if c < '0' || c > '9' {
			return n, errNotInt
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}

// atoi is atoiSafe ignoring the error.
func atoi(s string) int {
	n, _ := atoiSafe(s)
	return n
}

var errNotInt = errType("not an integer")

type errType string

func (e errType) Error() string { return string(e) }
