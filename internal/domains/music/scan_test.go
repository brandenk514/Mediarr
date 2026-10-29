package music

// L4 (Lidarr library import) domain tests: folder-name parsing, audio-file
// recognition, and candidate matching.

import "testing"

func TestL4_ParseFolderName(t *testing.T) {
	tests := []struct {
		in    string
		title string
		year  int
	}{
		{"Radiohead", "Radiohead", 0},
		{"  Queen (1977)  ", "Queen", 1977},
		{"Depeche Mode (1980) Live", "Depeche Mode (1980) Live", 0}, // year must be trailing
		{"Radiohead (1995) ", "Radiohead", 1995},
	}
	for _, tc := range tests {
		title, year := ParseFolderName(tc.in)
		if title != tc.title || year != tc.year {
			t.Errorf("ParseFolderName(%q) = (%q, %d), want (%q, %d)", tc.in, title, year, tc.title, tc.year)
		}
	}
}

func TestL4_IsAudioFile(t *testing.T) {
	yes := []string{"a.flac", "b.mp3", "c.M4A", "d.aac", "e.ogg", "f.oga", "g.opus", "h.wav", "i.wv", "j.ape", "k.alac", "l.wma"}
	for _, name := range yes {
		if !IsAudioFile(name) {
			t.Errorf("IsAudioFile(%q) = false, want true", name)
		}
	}
	no := []string{"notes.txt", "cover.jpg", "noext", "track.flac.bak"}
	for _, name := range no {
		if IsAudioFile(name) {
			t.Errorf("IsAudioFile(%q) = true, want false", name)
		}
	}
}

func TestL4_CandidateMatchesArtist(t *testing.T) {
	a := Artist{ID: 1, Name: "Radiohead"}
	if !CandidateMatchesArtist("radiohead", a) {
		t.Error("case-insensitive match failed")
	}
	if !CandidateMatchesArtist("  RADIOHEAD  ", a) {
		t.Error("trimmed match failed")
	}
	if CandidateMatchesArtist("Queen", a) {
		t.Error("different artist matched")
	}
	if CandidateMatchesArtist("Radiohead (1995)", a) {
		t.Error("year-in-parens title must not match (the year is stripped before the call)")
	}
}
