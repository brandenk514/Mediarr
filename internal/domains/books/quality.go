package books

import (
	"sort"
	"strings"

	"github.com/brandenk514/mediarr/internal/settings"
)

// quality.go carries the Readarr B3 a-book (audiobook) quality machinery.
// The unified book quality model splits along the axis the e-book pipeline
// never needed:
//
//   - E-books: format-based — EPUB / AZW3 / MOBI are format-preference
//     entries with no lossless/lossy axis. The matching machinery already
//     lives in format.go (Profile / MatchRelease) and is unchanged.
//   - A-books: a lossless/lossy ladder — the same audio codec families as
//     the music L3 ladder, sharing its seed values 1:1 (the settings book
//     levels rebase the music ladder into the 300+ space).
//
// Dependency rule: like the rest of this package, nothing here imports
// database, redis, or http packages (the settings import is the L3
// convention for level-id constants).

// A-book codec families (the a-book portion of the unified definitions;
// the e-book formats live in format.go). These are the upper-case family
// names, the same vocabulary music's CodecFromExtension emits.
const (
	ABookFLAC    = "FLAC"
	ABookMP3     = "MP3"
	ABookAAC     = "AAC"
	ABookOGG     = "OGG"
	ABookOPUS    = "OPUS"
	ABookWAV     = "WAV"
	ABookAPE     = "APE"
	ABookWavPack = "WAVPACK"
	ABookWMA     = "WMA"
)

// ABookCodecFromExtension is the a-book codec detector: a file extension
// (with or without the leading dot, any case) mapped to the upper-case
// codec family name; "" = not a recognized a-book file. The mapping mirrors
// music's CodecFromExtension (m4a/aac/alac all report the AAC family) so a
// file is the "same quality" in books and music.
func ABookCodecFromExtension(ext string) string {
	ext = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(ext), "."))
	switch ext {
	case "flac":
		return ABookFLAC
	case "mp3":
		return ABookMP3
	case "m4a", "aac", "alac":
		return ABookAAC
	case "ogg", "oga":
		return ABookOGG
	case "opus":
		return ABookOPUS
	case "wav":
		return ABookWAV
	case "ape":
		return ABookAPE
	case "wv", "wvpack":
		return ABookWavPack
	case "wma":
		return ABookWMA
	}
	return ""
}

// ABookLevelID maps an a-book codec family (any case) to its seeded book
// quality level ID (the settings 300+ book space). An unrecognized family
// maps to the seeded Unknown level (the book twin of music's Unknown) —
// the "Any" profile approves it, the other seeded profiles do not. Without
// tag-level bitrates the codec family's highest seeded level is the level
// (an MP3 is MP3-320, an OGG Vorbis is Q10): that is the most the extension
// alone can claim (the music L3 convention).
func ABookLevelID(codec string) int {
	switch strings.ToUpper(strings.TrimSpace(codec)) {
	case ABookFLAC:
		return settings.BookLevelFLAC
	case ABookMP3:
		return settings.BookLevelMP3320
	case ABookAAC:
		return settings.BookLevelAAC320
	case ABookOGG:
		return settings.BookLevelOGGQ10
	case ABookWAV:
		return settings.BookLevelWAV
	case ABookAPE:
		return settings.BookLevelAPE
	case ABookWavPack:
		return settings.BookLevelWavPack
	case ABookWMA:
		return settings.BookLevelWMA
	}
	return settings.BookLevelUnknown
}

// EBookLevelID maps a supported e-book format to its seeded book level ID
// (the settings 300+ book space); 0 = unsupported/undetected format. It is
// the e-book twin of ABookLevelID: the e-book side of the unified table.
func EBookLevelID(format string) int {
	f := strings.ToLower(strings.TrimSpace(format))
	f = strings.TrimPrefix(f, ".")
	switch f {
	case FormatEPUB:
		return settings.BookLevelEPUB
	case FormatAZW3:
		return settings.BookLevelAZW3
	case FormatMOBI:
		return settings.BookLevelMOBI
	}
	return 0
}

// BookLevelRank returns the best-first rank of a seeded book level id in
// the unified book level table (0 = best; e-book entries rank first, then
// the a-book ladder); -1 when the id is not seeded (unknown). Rank is the
// cutoff comparison axis: a release ranks at or above the cutoff when its
// rank is <= the cutoff's rank.
func BookLevelRank(level int) int {
	for i, l := range settings.DefaultBookLevels() {
		if l.ID == level {
			return i
		}
	}
	return -1
}

// MeetsCutoff reports whether a release at level ranks at or above the
// cutoff level on the seeded book ladder (lower rank = better) — the
// author's minimum-acceptance axis, shared by the e-book and a-book
// profiles. A cutoff of 0 (no minimum) is always met. Unknown levels rank
// below everything, so they only meet an Unknown cutoff.
func MeetsCutoff(level, cutoff int) bool {
	if cutoff <= 0 {
		return level > 0
	}
	r, cr := BookLevelRank(level), BookLevelRank(cutoff)
	if r < 0 || cr < 0 {
		return false
	}
	return r <= cr
}

// ABookProfile is a book quality profile restricted to its a-book (ladder)
// axis: the approved book level ids plus the cutoff level id — the minimum
// quality an author will accept. It is the a-book twin of the e-book
// Profile in format.go (which is format-based and carries no cutoff).
type ABookProfile struct {
	Name   string
	Levels []int // approved book level ids (seeded 300+ space)
	Cutoff int   // minimum quality the author accepts; 0 = none
}

// ABookMatch is the result of matching an a-book release level against an
// ABookProfile.
type ABookMatch struct {
	Matched  bool
	Level    int  // the release's book level id (0 = unknown)
	CutoffOK bool // true when the release ranks at or above the cutoff (the author's minimum)
	Reason   string
}

// MatchABookRelease matches an a-book release at level against the profile:
// the release matches when its level is one of the profile's approved
// levels; an unknown release (level 0) never matches. CutoffOK reports the
// author's minimum-acceptance axis independently of approval (a release
// approved by the profile but below the cutoff is Matched=true, CutoffOK
// false — the re-download/upgrade trigger of the cutoff concept).
func MatchABookRelease(p ABookProfile, level int) ABookMatch {
	out := ABookMatch{Level: level}
	if level <= 0 {
		out.Reason = "unknown a-book quality"
		return out
	}
	approved := false
	for _, l := range p.Levels {
		if l == level {
			approved = true
			break
		}
	}
	out.Matched = approved
	if approved {
		out.Reason = "approved level " + itoa(level)
	} else {
		out.Reason = "level " + itoa(level) + " not in profile"
	}
	if p.Cutoff > 0 {
		out.CutoffOK = MeetsCutoff(level, p.Cutoff)
	} else {
		out.CutoffOK = true
	}
	return out
}

// itoa formats an int (the book-domain twin of the music helper; the
// domain packages stay import-isolated from each other).
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

// SortABookLevels orders book level ids best-first per the seeded ladder
// (stable; unknown ids sort last).
func SortABookLevels(levels []int) {
	sort.SliceStable(levels, func(i, j int) bool {
		ri, rj := BookLevelRank(levels[i]), BookLevelRank(levels[j])
		if ri == rj {
			return levels[i] < levels[j]
		}
		if ri < 0 {
			return false
		}
		if rj < 0 {
			return true
		}
		return ri < rj
	})
}
