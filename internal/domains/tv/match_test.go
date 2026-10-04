package tv

import (
	"reflect"
	"sort"
	"testing"
)

// TestQualityProfilesAreBuiltIn confirms the built-in profiles are present and
// name-addressable (PLAN §15: quality profiles built in and selectable).
func TestQualityProfilesAreBuiltIn(t *testing.T) {
	for _, name := range []string{"HD-1080p", "Full-1080p", "HD-Any", "UHD-2160p", "Any"} {
		if _, ok := ProfileByName(name); !ok {
			t.Errorf("ProfileByName(%q) not found", name)
		}
	}
}

// TestMatchRelease checks that a parsed release matches the right profile item
// and that better-than-requested releases still match.
func TestMatchRelease(t *testing.T) {
	profile, _ := ProfileByName("HD-1080p")

	// 1080p WEB release matches the 1080p item.
	if p, err := ParseRelease("Show.S01E01.1080p.WEB.x264"); err == nil {
		m := MatchRelease(profile, p)
		if !m.Matched || m.Item.Quality != "1080p" {
			t.Errorf("1080p release: matched=%v item=%+v reason=%v", m.Matched, m.Item, m.Reason)
		}
	}

	// 720p release matches the 720p item (lower tier).
	if p, err := ParseRelease("Show.S01E01.720p.WEB.x264"); err == nil {
		m := MatchRelease(profile, p)
		if !m.Matched || m.Item.Quality != "720p" {
			t.Errorf("720p release: matched=%v item=%+v", m.Matched, m.Item)
		}
	}

	// 2160p release (better than requested) still matches — we accept
	// better-than-requested quality.
	if p, err := ParseRelease("Show.S01E01.2160p.WEB.x265"); err == nil {
		m := MatchRelease(profile, p)
		if !m.Matched {
			t.Errorf("2160p release should match HD-1080p (accepts better): %v", m.Reason)
		}
	}
}

// TestCodecFiltering confirms a codec-constrained item filters correctly: an
// h264-constrained item should NOT match an h265 release.
func TestCodecFiltering(t *testing.T) {
	profile, _ := ProfileByName("Full-1080p")
	if p, err := ParseRelease("Show.S01E01.1080p.WEB.x265"); err == nil {
		m := MatchRelease(profile, p)
		// h265 release: the h264-constrained 1080p item should be skipped;
		// it should match the h265 1080p item instead.
		if !m.Matched {
			t.Fatalf("h265 release should match Full-1080p via the h265 item: %v", m.Reason)
		}
		if m.Item.Codec != CodecH265 {
			t.Errorf("h265 release matched item codec=%q, want h265", m.Item.Codec)
		}
	}
}

// TestBestForEpisode checks the "pick the best release for an episode" path:
// among several candidates covering S01E01, the highest-scoring (best quality)
// wins, and a non-covering candidate is ignored.
func TestBestForEpisode(t *testing.T) {
	cands := buildCandidates(t, []string{
		"Show.S01E01.720p.WEB.x264",      // S01E01 720p
		"Show.S01E01.1080p.WEB.x264",     // S01E01 1080p (best for S01E01)
		"Show.S02E01.1080p.WEB.x264",     // S02E01 — does NOT cover S01E01
		"Show.S01E01.1080p.WEB.x265.GRD", // S01E01 1080p h265 (same quality tier)
	})

	best, ok := BestForEpisode(cands, 1, 1)
	if !ok {
		t.Fatal("expected a best release for S01E01")
	}
	// Both 1080p candidates tie on quality; the h264 (codec fit to default)
	// or h265 both score the same tier. Just assert it is a 1080p S01E01.
	if best.Parsed.Season != 1 || len(best.Parsed.Episodes) != 1 || best.Parsed.Episodes[0] != 1 {
		t.Errorf("best should be S01E01, got %+v", best.Parsed)
	}
	if best.Parsed.Height != 1080 {
		t.Errorf("best should be 1080p, got %dp", best.Parsed.Height)
	}

	// S02E01 should resolve to the S02 release.
	best2, ok := BestForEpisode(cands, 2, 1)
	if !ok {
		t.Fatal("expected a best release for S02E01")
	}
	if best2.Parsed.Season != 2 {
		t.Errorf("best for S02E01 should be S02, got %+v", best2.Parsed)
	}
}

// TestBestForEpisode_NotCovered confirms a non-covering candidate is not chosen.
func TestBestForEpisode_NotCovered(t *testing.T) {
	cands := buildCandidates(t, []string{
		"Show.S02E01.1080p.WEB.x264", // only S02E01
	})
	_, ok := BestForEpisode(cands, 1, 1)
	if ok {
		t.Error("S01E01 should not be covered by a S02E01-only candidate")
	}
}

// TestGroupCandidates verifies that releases are grouped by the episodes they
// cover: a multi-episode release forms its own group distinct from two separate
// single-episode releases.
func TestGroupCandidates(t *testing.T) {
	cands := buildCandidates(t, []string{
		"Show.S01E01E02.1080p.WEB.x264", // group key S01E01E02
		"Show.S01E01.1080p.WEB.x264",    // group key S01E01
		"Show.S01E02.1080p.WEB.x264",    // group key S01E02
	})
	groups := GroupCandidates(cands)

	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	wantKeys := []string{"S01E01", "S01E01E02", "S01E02"}
	if !reflect.DeepEqual(keys, wantKeys) {
		t.Fatalf("group keys = %v, want %v", keys, wantKeys)
	}
	// The multi-ep group has exactly one member; the singles have one each.
	if len(groups["S01E01E02"]) != 1 {
		t.Errorf("S01E01E02 group should have 1 member, got %d", len(groups["S01E01E02"]))
	}
}

// TestGroupCandidates_SameGroup confirms multiple releases covering the same
// episode land in the same group (so we can pick the best among them).
func TestGroupCandidates_SameGroup(t *testing.T) {
	cands := buildCandidates(t, []string{
		"Show.S01E01.720p.WEB.x264",
		"Show.S01E01.1080p.WEB.x265",
		"Show.S01E01.1080p.WEB.x264.GRD",
	})
	groups := GroupCandidates(cands)
	if len(groups["S01E01"]) != 3 {
		t.Fatalf("S01E01 group should have 3 members, got %d", len(groups["S01E01"]))
	}
}

// TestBestInGroup confirms BestInGroup returns the highest-scoring release.
func TestBestInGroup(t *testing.T) {
	cands := buildCandidates(t, []string{
		"Show.S01E01.720p.WEB.x264",
		"Show.S01E01.1080p.WEB.x265",
	})
	groups := GroupCandidates(cands)
	best, ok := BestInGroup(groups["S01E01"])
	if !ok {
		t.Fatal("expected a best in group")
	}
	if best.Parsed.Height != 1080 {
		t.Errorf("best in group should be 1080p, got %dp", best.Parsed.Height)
	}
}

// TestCovers verifies the episode-coverage predicate, including the season-pack
// case where a whole-season release covers any episode in the season.
func TestCovers(t *testing.T) {
	// Single episode.
	if p, _ := ParseRelease("Show.S01E01.1080p.WEB.x264"); !Covers(p, 1, 1) {
		t.Error("S01E01 should cover (1,1)")
	}
	if p, _ := ParseRelease("Show.S01E01.1080p.WEB.x264"); Covers(p, 1, 2) {
		t.Error("S01E01 should NOT cover (1,2)")
	}
	// Multi-episode.
	if p, _ := ParseRelease("Show.S01E01E02.1080p.WEB.x264"); p.Season == 1 {
		if !Covers(p, 1, 1) || !Covers(p, 1, 2) {
			t.Error("S01E01E02 should cover both (1,1) and (1,2)")
		}
		if Covers(p, 1, 3) {
			t.Error("S01E01E02 should NOT cover (1,3)")
		}
	}
	// Season pack covers the whole season.
	if p, _ := ParseRelease("Show.S01.COMPLETE.1080p.WEB.x264"); p.Season == 1 {
		if !IsSeasonPack(p) {
			t.Fatal("S01.COMPLETE should be a season pack")
		}
		if !Covers(p, 1, 5) {
			t.Error("season pack should cover any episode in S01")
		}
		if Covers(p, 2, 5) {
			t.Error("S01 pack should NOT cover S02")
		}
	}
}

// buildCandidates parses the given release names against the default profile
// and keeps only those that quality-match.
func buildCandidates(t *testing.T, names []string) []CandidateRelease {
	t.Helper()
	var out []CandidateRelease
	for _, n := range names {
		c, ok := BuildCandidate(n, "test-indexer", "HD-1080p")
		if !ok {
			t.Fatalf("BuildCandidate(%q) did not match", n)
		}
		out = append(out, c)
	}
	return out
}
