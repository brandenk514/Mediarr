package music

import (
	"testing"
)

// TestQualityProfilesAreBuiltIn confirms the built-in music profiles are
// present and name-addressable (PLAN §15: quality profiles built in and
// selectable).
func TestQualityProfilesAreBuiltIn(t *testing.T) {
	for _, name := range []string{"Lossless", "Lossless-or-320", "320K", "Any"} {
		if _, ok := ProfileByName(name); !ok {
			t.Errorf("ProfileByName(%q) not found", name)
		}
	}
}

// TestMatchRelease_Lossless confirms a FLAC release matches the default
// Lossless profile, an MP3 320 does not, and an MP3 320 matches the
// Lossless-or-320 profile.
func TestMatchRelease_Lossless(t *testing.T) {
	lossless, _ := ProfileByName("Lossless")

	p := ParseQuality("Bob Marley - Legend [FLAC 998kbps Lossless]")
	if p.Format != FormatFLAC {
		t.Errorf("ParseQuality format = %q, want %q", p.Format, FormatFLAC)
	}
	if m := MatchRelease(lossless, p); !m.Matched {
		t.Errorf("FLAC release should match Lossless: %v", m.Reason)
	}

	mp3 := ParseQuality("Nirvana - Nevermind [MP3 320kbps]")
	if m := MatchRelease(lossless, mp3); m.Matched {
		t.Errorf("MP3 320 should NOT match Lossless-only profile: %v", m.Reason)
	}

	lossy320, _ := ProfileByName("Lossless-or-320")
	if m := MatchRelease(lossy320, mp3); !m.Matched {
		t.Errorf("MP3 320 should match Lossless-or-320: %v", m.Reason)
	}
}

// TestMatchRelease_BitrateFloor confirms a 320k profile item rejects a lower
// bit-rate release (the item's MinBitrate is a floor, not a target).
func TestMatchRelease_BitrateFloor(t *testing.T) {
	p320, _ := ProfileByName("320K")

	low := ParsedRelease{Format: FormatMP3, Bitrate: 192}
	if m := MatchRelease(p320, low); m.Matched {
		t.Errorf("192k MP3 should NOT match 320K profile: %v", m.Reason)
	}

	high := ParsedRelease{Format: FormatMP3, Bitrate: 320}
	if m := MatchRelease(p320, high); !m.Matched {
		t.Errorf("320k MP3 should match 320K profile: %v", m.Reason)
	}

	// Any-format with a bit-rate floor: the format is unconstrained but the
	// bit-rate still applies.
	anyProf := Profile{Items: []ProfileItem{{Format: FormatAny, MinBitrate: 320}}}
	if m := MatchRelease(anyProf, low); m.Matched {
		t.Error("192k release should NOT match an any-format 320k-floor item")
	}
}

// TestSortCandidates_BestFirst confirms lossless dominates over compressed and
// higher bit-rate wins among compressed releases.
func TestSortCandidates_BestFirst(t *testing.T) {
	cands := []CandidateRelease{
		{Title: "mp3-320", Score: 320 + 320*10, Quality: ProfileMatch{Format: FormatMP3, Bitrate: 320}},
		{Title: "mp3-192", Score: 192 + 320*10, Quality: ProfileMatch{Format: FormatMP3, Bitrate: 192}},
		{Title: "flac", Score: 1000 + 320*10, Quality: ProfileMatch{Format: FormatFLAC, Bitrate: 0}},
	}
	SortCandidates(cands)
	if cands[0].Title != "flac" {
		t.Errorf("best candidate = %q, want flac", cands[0].Title)
	}
	if cands[1].Title != "mp3-320" {
		t.Errorf("second candidate = %q, want mp3-320", cands[1].Title)
	}
}
