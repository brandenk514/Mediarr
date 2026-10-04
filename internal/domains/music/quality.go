package music

import (
	"sort"
)

// quality.go carries the music quality-profile machinery. It mirrors the
// TV/movie quality-matching patterns (PLAN §4: per-domain, decoupled) but is
// tuned to audio: a quality item is an audio format plus an optional minimum
// bit-rate, rather than a video resolution plus a codec.
//
// Dependency rule: nothing in this package imports database, redis, or http
// packages. Everything here is pure and testable without any I/O.

// Audio format identifiers used in quality profiles.
const (
	FormatAny  = "any"
	FormatFLAC = "flac"
	FormatAPE  = "ape"
	FormatWAV  = "wav"
	FormatMP3  = "mp3"
	FormatAAC  = "aac"
	FormatM4A  = "m4a"
	FormatOpus = "opus"
)

// Lossless formats: any of these is bit-exact. A release in one of these
// formats always satisfies a "Lossless" profile item.
var losslessFormats = map[string]bool{
	FormatFLAC: true,
	FormatAPE:  true,
	FormatWAV:  true,
}

// Profile is a quality profile: an ordered (best-first) set of quality items.
// A release matches if it satisfies any one item; the highest matching item
// wins for ranking.
type Profile struct {
	ID    int64
	Name  string
	Items []ProfileItem // ordered best-first
}

// ProfileItem is a single selectable quality in a profile.
type ProfileItem struct {
	ID         int64
	Format     string // an audio format (see Format* consts) or FormatAny
	MinBitrate int    // min kbps; 0 = no restriction (lossless items use 0)
	Rank       int    // profile-internal order (lower = better)
}

// BuiltInProfiles are the default music quality profiles, best-first. "Lossless"
// is the default for monitored albums (the PLAN's §4 default profile).
func BuiltInProfiles() []Profile {
	return []Profile{
		{Name: "Lossless", Items: qualityItems(
			FormatFLAC, 0, 0,
			FormatAPE, 0, 1,
			FormatWAV, 0, 2,
		)},
		{Name: "Lossless-or-320", Items: qualityItems(
			FormatFLAC, 0, 0,
			FormatAPE, 0, 1,
			FormatMP3, 320, 2,
			FormatAAC, 320, 3,
			FormatM4A, 320, 4,
		)},
		{Name: "320K", Items: qualityItems(
			FormatMP3, 320, 0,
			FormatAAC, 320, 1,
			FormatM4A, 320, 2,
		)},
		{Name: "Any", Items: qualityItems(
			FormatAny, 0, 0,
		)},
	}
}

func qualityItems(kv ...any) []ProfileItem {
	var items []ProfileItem
	for i := 0; i+2 < len(kv); i += 3 {
		items = append(items, ProfileItem{
			ID:         int64(i/3 + 1),
			Format:     kv[i].(string),
			MinBitrate: kv[i+1].(int),
			Rank:       kv[i+2].(int),
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

// ParsedRelease is the result of parsing a scene release name into music
// fields (format, bit-rate). Title/artist/album extraction lives in match.go.
type ParsedRelease struct {
	Format  string // a Format* const, or FormatAny/"" if undetected
	Bitrate int    // kbps; 0 if undetected (lossless or unknown)
	Source  string // "lossless" / "compressed" hint, if detected
}

// ProfileMatch is the result of matching a parsed release against a profile.
type ProfileMatch struct {
	Matched bool
	Item    ProfileItem
	Format  string
	Bitrate int
	// Score is a ranking score (higher is better) used to pick the best
	// release among candidates. Lossless dominates, then bit-rate.
	Score  int
	Reason string
}

// MatchRelease evaluates a parsed release against a profile and returns the
// best matching item (or Matched=false).
func MatchRelease(p Profile, rel ParsedRelease) ProfileMatch {
	best := ProfileMatch{Format: rel.Format, Bitrate: rel.Bitrate}
	bestFound := false

	for _, item := range p.Items {
		if !itemMatches(item, rel) {
			continue
		}
		score := scoreFor(item, rel)
		if !bestFound || score > best.Score {
			best = ProfileMatch{
				Matched: true,
				Item:    item,
				Format:  rel.Format,
				Bitrate: rel.Bitrate,
				Score:   score,
				Reason:  describeMatch(item, rel),
			}
			bestFound = true
		}
	}
	return best
}

func itemMatches(item ProfileItem, rel ParsedRelease) bool {
	if item.Format == FormatAny {
		return bitrateOK(item.MinBitrate, rel)
	}
	// A specific format item matches only the same format (audio formats are
	// not interchangeable the way video resolutions ladder up).
	if rel.Format != "" && rel.Format != item.Format {
		return false
	}
	return bitrateOK(item.MinBitrate, rel)
}

func bitrateOK(min int, rel ParsedRelease) bool {
	if min <= 0 {
		return true
	}
	return rel.Bitrate >= min
}

// scoreFor builds a ranking score used to pick the best release among
// candidates: lossless dominates (a large constant so no compressed release can
// outrank a lossless one regardless of bit-rate), then the item's bit-rate tier
// (x10) and the release's own bit-rate break ties among compressed releases.
func scoreFor(item ProfileItem, rel ParsedRelease) int {
	const losslessBonus = 1_000_000
	score := 0
	if rel.Format != "" && losslessFormats[rel.Format] {
		score += losslessBonus
	}
	score += item.MinBitrate * 10
	score += rel.Bitrate
	return score
}

func describeMatch(item ProfileItem, rel ParsedRelease) string {
	if item.Format == FormatAny {
		return "format=any bitrate=" + itoa(rel.Bitrate)
	}
	return "format=" + item.Format + " minBitrate=" + itoa(item.MinBitrate)
}

// SortReleases orders candidate matches best-first by score (desc).
func SortReleases(matches []ProfileMatch) {
	sort.SliceStable(matches, func(i, j int) bool {
		return matches[i].Score > matches[j].Score
	})
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
