// Package movies contains the pure (no I/O) business logic for the movie
// domain: quality profile evaluation and release-name parsing. It is the
// first of the four media domains (movies, tv, music, books).
//
// Dependency rule: nothing in this package imports database, redis, or http
// packages. Everything here is testable without any I/O.
package movies

import (
	"sort"
	"strconv"
)

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

// Resolutions ordered best-first (highest first). A release's parsed height
// is matched against the highest profile entry it satisfies.
var Resolutions = []Resolution{
	{Name: "2160p", MinHeight: 2160},
	{Name: "1440p", MinHeight: 1440},
	{Name: "1080p", MinHeight: 1080},
	{Name: "720p", MinHeight: 720},
	{Name: "576p", MinHeight: 576},
	{Name: "480p", MinHeight: 480},
	{Name: "sd", MinHeight: 0},
}

// Profile is a quality profile: a set of quality items, each with an
// optional codec constraint, an allowed source, and a cutoff (below the
// cutoff we stop wanting — the release is skipped).
//
// The model follows Radarr: a list of quality items; a release matches if it
// satisfies any one of them. The highest matching item wins for ranking.
type Profile struct {
	ID        int64
	Name      string
	Items     []ProfileItem // ordered best-first
	CutoffID  int64         // item ID below which we stop wanting (0 = none)
	UseCutoff bool
}

// ProfileItem is a single selectable quality in a profile.
type ProfileItem struct {
	ID int64
	// Quality is a resolution name (see Resolutions) or "Any".
	Quality string
	// Codec restricts the item to a codec; CodecAny means no restriction.
	Codec string
	// Rank is the profile-internal order (lower = better).
	Rank int
}

// BuiltInProfiles are the default quality profiles, in Radarr order.
func BuiltInProfiles() []Profile {
	// A small, safe set of defaults. IDs are stable so cutoffs can reference
	// them; they are assigned per-profile below.
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

// ProfileByID returns a built-in profile by name.
func ProfileByName(name string) (Profile, bool) {
	for _, p := range BuiltInProfiles() {
		if p.Name == name {
			return p, true
		}
	}
	return Profile{}, false
}

// ParsedRelease is the result of parsing a scene release name.
type ParsedRelease struct {
	// Title is the cleaned movie title (best-effort).
	Title string
	// Year, if present (e.g. 2019).
	Year int
	// Height in px (e.g. 1080). 0 if undetected.
	Height int
	// Codec, if detected (e.g. "h265"). CodecAny/empty if undetected.
	Codec string
	// Container/source hints, e.g. "blu-ray", "web", "hdtv".
	Source string
	// QualityName is the resolved resolution label (see Resolutions).
	QualityName string
}

// ProfileMatch is the result of matching a parsed release against a profile.
type ProfileMatch struct {
	// Matched is true if the release satisfies at least one profile item.
	Matched bool
	// Item is the highest (best) matching profile item.
	Item ProfileItem
	// QualityName is the resolution label the release resolves to.
	QualityName string
	// Score is a ranking score (higher is better) used to pick the best
	// release among candidates. Resolution dominates, then codec fit.
	Score int
	// Reason is a human-readable explanation (for logging/history).
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
		if !itemMatches(p, item, rel, relQuality) {
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

func itemMatches(p Profile, item ProfileItem, rel ParsedRelease, relQuality string) bool {
	// "any" quality item matches everything.
	if item.Quality == "any" {
		return codecOK(item.Codec, rel.Codec)
	}
	// A specific quality item matches if the release's quality is at or above
	// the item's quality (we accept better-than-requested, e.g. a 2160p
	// release satisfies a 1080p item).
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
			return len(Resolutions) - i // 2160p=7 ... sd=1
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

// scoreFor builds a ranking score. The release's resolution dominates
// (x1000) so better-than-requested releases rank higher; the matched item's
// own quality tier (x10) breaks ties within a release so a 1080p item beats
// a 720p fallback for the same file; a +10 bonus is added when the item's
// codec constraint is satisfied (always true for a matched item today, but
// kept so codec weighting can evolve independently).
func scoreFor(item ProfileItem, rel ParsedRelease, relQuality string) int {
	score := qualityRank(relQuality) * 1000
	score += qualityRank(item.Quality) * 10
	if item.Codec == CodecAny || item.Codec == rel.Codec {
		score += 10
	}
	return score
}

func describeMatch(item ProfileItem, rel ParsedRelease, relQuality string) string {
	return "quality=" + relQuality + " item=" + item.Quality +
		"/" + item.Codec
}

// SortReleases orders candidate releases best-first by score (desc).
func SortReleases(matches []ProfileMatch) {
	sort.SliceStable(matches, func(i, j int) bool {
		return matches[i].Score > matches[j].Score
	})
}

// heightToken converts a scene height token like "1080p" to px.
func heightToken(tok string) (int, bool) {
	tok = trimTok(tok)
	if len(tok) < 3 || !endsWith(tok, "p") {
		return 0, false
	}
	n, err := strconv.Atoi(tok[:len(tok)-1])
	if err != nil {
		return 0, false
	}
	return n, true
}

func trimTok(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == '.' || s[start] == '_' || s[start] == '-' || s[start] == '(' || s[start] == '[') {
		start++
	}
	for end > start && (s[end-1] == '.' || s[end-1] == '_' || s[end-1] == '-' || s[end-1] == ')' || s[end-1] == ']') {
		end--
	}
	return s[start:end]
}

func endsWith(s, suf string) bool {
	return len(s) >= len(suf) && s[len(s)-len(suf):] == suf
}
