package books

import (
	"testing"

	"github.com/brandenk514/mediarr/internal/settings"
)

// TestABookCodecFromExtension covers the a-book codec detector: the
// extension→codec-family mapping (with or without the leading dot, any
// case), the AAC family collapsing (m4a/aac/alac), and unrecognized files.
func TestABookCodecFromExtension(t *testing.T) {
	cases := []struct {
		ext  string
		want string
	}{
		{"flac", ABookFLAC},
		{".FLAC", ABookFLAC},
		{"Mp3", ABookMP3},
		{".mp3", ABookMP3},
		{"m4a", ABookAAC},
		{".M4A", ABookAAC},
		{"alac", ABookAAC},
		{"aac", ABookAAC},
		{"ogg", ABookOGG},
		{".oga", ABookOGG},
		{"opus", ABookOPUS},
		{"wav", ABookWAV},
		{"ape", ABookAPE},
		{"wv", ABookWavPack},
		{".wvpack", ABookWavPack},
		{"wma", ABookWMA},
		{"", ""},
		{".txt", ""},
		{"epub", ""}, // e-book files are not a-books
		{".mp3 ", ABookMP3},
	}
	for _, c := range cases {
		if got := ABookCodecFromExtension(c.ext); got != c.want {
			t.Errorf("ABookCodecFromExtension(%q) = %q, want %q", c.ext, got, c.want)
		}
	}
}

// TestABookLevelID covers codec-family→book level mapping: each family maps
// to its family's highest seeded level (the extension-alone claim), and an
// unrecognized family maps to the seeded Unknown level.
func TestABookLevelID(t *testing.T) {
	cases := []struct {
		codec string
		want  int
	}{
		{ABookFLAC, settings.BookLevelFLAC},
		{ABookMP3, settings.BookLevelMP3320},
		{ABookAAC, settings.BookLevelAAC320},
		{ABookOGG, settings.BookLevelOGGQ10},
		{ABookWAV, settings.BookLevelWAV},
		{ABookAPE, settings.BookLevelAPE},
		{ABookWavPack, settings.BookLevelWavPack},
		{ABookWMA, settings.BookLevelWMA},
		{"", settings.BookLevelUnknown},
		{"MPEG4", settings.BookLevelUnknown},
		{" flac ", settings.BookLevelFLAC}, // any case, trimmed
	}
	for _, c := range cases {
		if got := ABookLevelID(c.codec); got != c.want {
			t.Errorf("ABookLevelID(%q) = %d, want %d", c.codec, got, c.want)
		}
	}
}

// TestEBookLevelID covers the e-book twin: supported formats map to the
// e-book entries of the unified table (EPUB/AZW3/MOBI, any case);
// unsupported/unknown formats map to 0 (e-books have no Unknown level).
func TestEBookLevelID(t *testing.T) {
	cases := []struct {
		format string
		want   int
	}{
		{FormatEPUB, settings.BookLevelEPUB},
		{".EPUB", settings.BookLevelEPUB},
		{FormatAZW3, settings.BookLevelAZW3},
		{FormatMOBI, settings.BookLevelMOBI},
		{"Mobi", settings.BookLevelMOBI},
		{"pdf", 0},
		{"", 0},
	}
	for _, c := range cases {
		if got := EBookLevelID(c.format); got != c.want {
			t.Errorf("EBookLevelID(%q) = %d, want %d", c.format, got, c.want)
		}
	}
}

// TestBookLevelRank covers the best-first rank of the unified table: the
// e-book entries rank first (EPUB best), then the a-book ladder; unseeded
// ids rank -1.
func TestBookLevelRank(t *testing.T) {
	if r := BookLevelRank(settings.BookLevelEPUB); r != 0 {
		t.Errorf("EPUB rank = %d, want 0 (best)", r)
	}
	if r := BookLevelRank(settings.BookLevelAZW3); r != 1 {
		t.Errorf("AZW3 rank = %d, want 1", r)
	}
	if r := BookLevelRank(settings.BookLevelMOBI); r != 2 {
		t.Errorf("MOBI rank = %d, want 2", r)
	}
	if r := BookLevelRank(settings.BookLevelUnknown); r != len(settings.DefaultBookLevels())-1 {
		t.Errorf("Unknown rank = %d, want last (%d)", r, len(settings.DefaultBookLevels())-1)
	}
	// An a-book level ranks after the e-book entries.
	if BookLevelRank(settings.BookLevelFLAC) <= BookLevelRank(settings.BookLevelMOBI) {
		t.Error("FLAC must rank after the e-book entries")
	}
	if BookLevelRank(999) != -1 {
		t.Errorf("unseeded id rank = %d, want -1", BookLevelRank(999))
	}
	if BookLevelRank(0) != -1 {
		t.Error("level 0 is not seeded, rank must be -1")
	}
}

// TestMeetsCutoff covers the author's minimum-acceptance axis: no cutoff
// (0) accepts every seeded release; a release meets a cutoff when it ranks
// at or above it (e-book and a-book profiles share the axis); unknown
// releases meet no real cutoff.
func TestMeetsCutoff(t *testing.T) {
	// No cutoff: any seeded level is accepted, level 0 is not.
	if !MeetsCutoff(settings.BookLevelEPUB, 0) || !MeetsCutoff(settings.BookLevelWAV, 0) {
		t.Error("cutoff 0 must accept every seeded level")
	}
	if MeetsCutoff(0, 0) {
		t.Error("cutoff 0 must not accept a non-seeded level")
	}
	// EPUB cutoff (the 'Best' profile): only EPUB itself meets it.
	if !MeetsCutoff(settings.BookLevelEPUB, settings.BookLevelEPUB) {
		t.Error("EPUB must meet an EPUB cutoff")
	}
	if MeetsCutoff(settings.BookLevelAZW3, settings.BookLevelEPUB) {
		t.Error("AZW3 ranks below the EPUB cutoff")
	}
	if MeetsCutoff(settings.BookLevelFLAC, settings.BookLevelEPUB) {
		t.Error("a-book levels rank below the e-book entries, not above")
	}
	// FLAC cutoff (a lossless minimum): lossless meets it, lossy does not.
	if !MeetsCutoff(settings.BookLevelFLAC, settings.BookLevelFLAC) {
		t.Error("FLAC must meet a FLAC cutoff")
	}
	if MeetsCutoff(settings.BookLevelMP3320, settings.BookLevelFLAC) {
		t.Error("MP3-320 ranks below the FLAC cutoff")
	}
	if !MeetsCutoff(settings.BookLevelWAV, settings.BookLevelFLAC) {
		t.Error("WAV ranks above FLAC in the seeded ladder")
	}
	// Unknown cutoff: everything seeded meets it.
	if !MeetsCutoff(settings.BookLevelEPUB, settings.BookLevelUnknown) ||
		!MeetsCutoff(settings.BookLevelMP38, settings.BookLevelUnknown) {
		t.Error("the Unknown cutoff is the floor: everything meets it")
	}
	// A non-seeded release level meets no real cutoff.
	if MeetsCutoff(0, settings.BookLevelFLAC) {
		t.Error("level 0 must not meet a real cutoff")
	}
}

// TestMatchABookRelease covers the a-book profile matcher: approval of
// seeded levels, rejection of unapproved levels, the Unknown level
// (approved only by profiles that approve it), and the cutoff axis
// (matched-but-below-cutoff).
func TestMatchABookRelease(t *testing.T) {
	best := ABookProfile{Name: "Best", Levels: []int{
		settings.BookLevelEPUB, settings.BookLevelAZW3, settings.BookLevelMOBI,
		settings.BookLevelFLAC, settings.BookLevelWAV, settings.BookLevelMP3320,
	}, Cutoff: settings.BookLevelEPUB}
	anyProf := ABookProfile{Name: "Any", Levels: []int{
		settings.BookLevelMP3320, settings.BookLevelUnknown,
	}, Cutoff: 0}

	// Approved by the profile but below the cutoff (the re-download signal):
	// a-books rank below the e-book entries, so they never meet the EPUB
	// cutoff of 'Best'.
	if m := MatchABookRelease(best, settings.BookLevelFLAC); !m.Matched || m.CutoffOK {
		t.Errorf("FLAC vs Best = %+v, want matched=true cutoff=false", m)
	}
	if m := MatchABookRelease(best, settings.BookLevelEPUB); !m.Matched || !m.CutoffOK {
		t.Errorf("EPUB vs Best = %+v, want matched + cutoff ok", m)
	}
	// Approved by the profile but below the cutoff (the re-download signal).
	if m := MatchABookRelease(best, settings.BookLevelAZW3); !m.Matched || m.CutoffOK {
		t.Errorf("AZW3 vs Best = %+v, want matched=true cutoff=false (below the EPUB cutoff)", m)
	}
	if m := MatchABookRelease(best, settings.BookLevelWAV); !m.Matched || m.CutoffOK {
		t.Errorf("WAV vs Best = %+v, want matched=true cutoff=false (WAV ranks below the EPUB cutoff)", m)
	}
	if m := MatchABookRelease(best, settings.BookLevelAPE); m.Matched {
		t.Errorf("APE vs Best = %+v, want unmatched (not in the approve set)", m)
	}
	// Unknown release level: never matched, even by 'Any'.
	if m := MatchABookRelease(anyProf, 0); m.Matched || m.Reason == "" {
		t.Errorf("level 0 vs Any = %+v, want unmatched with a reason", m)
	}
	// The seeded Unknown level: matched only when the profile approves it.
	if m := MatchABookRelease(anyProf, settings.BookLevelUnknown); !m.Matched || !m.CutoffOK {
		t.Errorf("Unknown vs Any = %+v, want matched + cutoff ok (no cutoff)", m)
	}
	if m := MatchABookRelease(ABookProfile{Name: "EPUB", Levels: []int{settings.BookLevelEPUB}}, settings.BookLevelUnknown); m.Matched {
		t.Errorf("Unknown vs EPUB-only = %+v, want unmatched", m)
	}
	// A cutoff set on the a-book axis.
	flacMin := ABookProfile{Name: "Lossless Min", Levels: []int{
		settings.BookLevelFLAC, settings.BookLevelWAV, settings.BookLevelMP3320,
	}, Cutoff: settings.BookLevelFLAC}
	if m := MatchABookRelease(flacMin, settings.BookLevelMP3320); !m.Matched || m.CutoffOK {
		t.Errorf("MP3-320 vs flacMin = %+v, want matched=true cutoff=false", m)
	}
	if m := MatchABookRelease(flacMin, settings.BookLevelFLAC); !m.Matched || !m.CutoffOK {
		t.Errorf("FLAC vs flacMin = %+v, want matched + cutoff ok", m)
	}
}

// TestSortABookLevels covers best-first ordering of book level ids (the
// seeded ladder order) with unknown ids last.
func TestSortABookLevels(t *testing.T) {
	in := []int{
		settings.BookLevelMP3320, settings.BookLevelMOBI, settings.BookLevelEPUB,
		settings.BookLevelFLAC, 999, settings.BookLevelWAV,
	}
	SortABookLevels(in)
	want := []int{
		settings.BookLevelEPUB, settings.BookLevelMOBI,
		settings.BookLevelWAV, settings.BookLevelFLAC,
		settings.BookLevelMP3320, 999,
	}
	for i := range want {
		if in[i] != want[i] {
			t.Fatalf("sorted = %v, want %v", in, want)
		}
	}
}
