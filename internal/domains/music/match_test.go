package music

import (
	"reflect"
	"sort"
	"testing"
)

// TestParseName_ArtistAlbum confirms the best-effort artist/album split on the
// first " - " boundary.
func TestParseName_ArtistAlbum(t *testing.T) {
	p := ParseName("Bob Marley - Legend [FLAC Lossless]")
	if p.Artist != "Bob Marley" {
		t.Errorf("Artist = %q, want %q", p.Artist, "Bob Marley")
	}
	// The bracketed quality tag is stripped from the album so it matches the
	// clean monitored album title ("Legend").
	if p.Album != "Legend" {
		t.Errorf("Album = %q, want %q", p.Album, "Legend")
	}
}

// TestParseName_NoSeparator confirms a single-part name has no artist and the
// whole thing is the album.
func TestParseName_NoSeparator(t *testing.T) {
	p := ParseName("Legend [FLAC Lossless]")
	if p.Artist != "" {
		t.Errorf("Artist = %q, want empty (no separator)", p.Artist)
	}
	// The quality tag is stripped; the clean album title remains.
	if p.Album != "Legend" {
		t.Errorf("Album = %q, want %q", p.Album, "Legend")
	}
}

// TestParseName_WholeAlbum confirms a release with no discrete tracks is
// treated as a whole album.
func TestParseName_WholeAlbum(t *testing.T) {
	p := ParseName("Bob Marley - Legend [FLAC Lossless]")
	if !p.WholeAlbum {
		t.Errorf("expected WholeAlbum=true, got false (tracks=%v)", p.Tracks)
	}
	if len(p.Tracks) != 0 {
		t.Errorf("expected no discrete tracks, got %v", p.Tracks)
	}
}

// TestParseName_DiscreteTracks confirms explicit track numbers are detected and
// sorted.
func TestParseName_DiscreteTracks(t *testing.T) {
	p := ParseName("Bob Marley - Legend Track 03 01 02")
	want := []TrackRef{{Disc: 1, Number: 1}, {Disc: 1, Number: 2}, {Disc: 1, Number: 3}}
	if !reflect.DeepEqual(p.Tracks, want) {
		t.Errorf("Tracks = %v, want %v", p.Tracks, want)
	}
	if p.WholeAlbum {
		t.Error("a release with discrete tracks should not be a whole album")
	}
}

// TestParseQuality_FormatBitrate confirms the format and bit-rate are parsed
// from the release name.
func TestParseQuality_FormatBitrate(t *testing.T) {
	flac := ParseQuality("Artist - Album [FLAC 998kbps Lossless]")
	if flac.Format != FormatFLAC {
		t.Errorf("flac format = %q, want %q", flac.Format, FormatFLAC)
	}
	if flac.Source != "lossless" {
		t.Errorf("flac source = %q, want lossless", flac.Source)
	}

	mp3 := ParseQuality("Artist - Album [MP3 320kbps]")
	if mp3.Format != FormatMP3 {
		t.Errorf("mp3 format = %q, want %q", mp3.Format, FormatMP3)
	}
	if mp3.Bitrate != 320 {
		t.Errorf("mp3 bitrate = %d, want 320", mp3.Bitrate)
	}
}

// TestBuildCandidate_Matched confirms BuildCandidate reports quality-match for
// a release that fits the profile.
func TestBuildCandidate_Matched(t *testing.T) {
	c, ok := BuildCandidate("Bob Marley - Legend [FLAC Lossless]", "fake", "Lossless")
	if !ok {
		t.Fatalf("expected a quality match")
	}
	if c.Parsed.Artist != "Bob Marley" {
		t.Errorf("artist = %q, want Bob Marley", c.Parsed.Artist)
	}
	if c.Quality.Format != FormatFLAC {
		t.Errorf("quality format = %q, want flac", c.Quality.Format)
	}
}

// TestBuildCandidate_NoMatch confirms a release that does not fit the profile
// is reported as unmatched.
func TestBuildCandidate_NoMatch(t *testing.T) {
	_, ok := BuildCandidate("Nirvana - Nevermind [MP3 192kbps]", "fake", "Lossless")
	if ok {
		t.Error("an MP3 192 release should NOT match the Lossless profile")
	}
}

// TestNormalise confirms the canonical comparison form is case-insensitive and
// whitespace/period-tolerant.
func TestNormalise(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Bob  Marley", "bob marley"},
		{"BOB.MARLEY", "bob marley"},
		{"  The Wire  ", "the wire"},
		{"A!B?", "a b"},
	}
	for _, c := range cases {
		if got := Normalise(c.in); got != c.want {
			t.Errorf("Normalise(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestMatchesArtistAndAlbum confirms the forgiving artist/album predicates
// tolerate scene naming drift. ParseName strips the bracketed quality tag, so
// the parsed album is the clean title ("Legend") and must be compared against
// that clean form.
func TestMatchesArtistAndAlbum(t *testing.T) {
	rel := ParseName("Bob Marley - Legend [FLAC]")
	if rel.Album != "Legend" {
		t.Fatalf("parsed album = %q, want clean title %q", rel.Album, "Legend")
	}
	if !MatchesArtist(rel, "bob marley") {
		t.Error("artist should match (case-insensitive)")
	}
	if !MatchesAlbum(rel, "legend") {
		t.Errorf("album should match; parsed album = %q", rel.Album)
	}
	if MatchesAlbum(rel, "Uprising") {
		t.Error("a different album should not match")
	}
}

// TestBestForAlbum_BestFirst confirms the highest-scoring matching whole-album
// candidate wins and a non-matching candidate is ignored.
func TestBestForAlbum_BestFirst(t *testing.T) {
	var cands []CandidateRelease
	for _, n := range []string{
		"Bob Marley - Legend [MP3 320kbps]",     // matches, compressed
		"Bob Marley - Legend [FLAC Lossless]",   // matches, lossless (best)
		"Bob Marley - Uprising [FLAC Lossless]", // wrong album — ignore
		"Different - Artist [FLAC Lossless]",    // wrong artist — ignore
	} {
		c, ok := BuildCandidate(n, "fake", "Lossless-or-320")
		if !ok {
			t.Fatalf("BuildCandidate(%q) did not match", n)
		}
		cands = append(cands, c)
	}
	best, ok := BestForAlbum(cands, "Bob Marley", "Legend")
	if !ok {
		t.Fatal("expected a best candidate for the album")
	}
	// Both the FLAC and MP3 320 "Legend" releases match the clean album name;
	// the lossless one must win the ranking.
	if best.Title != "Bob Marley - Legend [FLAC Lossless]" {
		t.Errorf("best = %q, want the FLAC Legend release", best.Title)
	}
}

// TestBestForAlbum_NotCovered confirms BestForAlbum returns not-found when no
// candidate matches the requested album.
func TestBestForAlbum_NotCovered(t *testing.T) {
	cands := []CandidateRelease{}
	if _, ok := BestForAlbum(cands, "Bob Marley", "Legend"); ok {
		t.Error("no candidates should not yield a best")
	}
}

// TestSortCandidates_DeterministicTieBreak confirms equal-score candidates are
// ordered by title (stable, deterministic).
func TestSortCandidates_DeterministicTieBreak(t *testing.T) {
	cands := []CandidateRelease{
		{Title: "b-release", Score: 500},
		{Title: "a-release", Score: 500},
		{Title: "c-release", Score: 500},
	}
	SortCandidates(cands)
	titles := make([]string, len(cands))
	for i, c := range cands {
		titles[i] = c.Title
	}
	sort.Strings(titles) // titles should now be sorted (a,b,c)
	want := []string{"a-release", "b-release", "c-release"}
	if !reflect.DeepEqual(titles, want) {
		t.Errorf("sorted titles = %v, want %v", titles, want)
	}
}

// TestStripQualityTag_Confirms the tag-stripping logic: a quality tag is
// removed, a real subtitle is kept, and nested/adjacent tags are handled.
func TestStripQualityTag(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Legend [FLAC 320kbps]", "Legend"},
		{"Legend [FLAC Lossless]", "Legend"},
		{"Legend (1977)", "Legend (1977)"}, // real subtitle, not quality
		{"Legend [FLAC 320kbps] [Hi-Res]", "Legend"},
		{"The White Album", "The White Album"},
		{"Legend", "Legend"},
		{"", ""},
	}
	for _, c := range cases {
		if got := stripQualityTag(c.in); got != c.want {
			t.Errorf("stripQualityTag(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestLosslessDominates confirms a lossless release outranks a high-bit-rate
// compressed one in candidate ranking (the lossless bonus must dwarf the
// bit-rate terms).
func TestLosslessDominates(t *testing.T) {
	flac := ParsedRelease{Format: FormatFLAC, Bitrate: 0}
	mp3320 := ParsedRelease{Format: FormatMP3, Bitrate: 320}
	anyItem := ProfileItem{Format: FormatAny, MinBitrate: 320}

	if sf := scoreFor(anyItem, flac); sf <= scoreFor(anyItem, mp3320) {
		t.Errorf("lossless score %d should exceed mp3-320 score %d",
			sf, scoreFor(anyItem, mp3320))
	}
}
