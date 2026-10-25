package tv

import "testing"

// Tests for the pure library-scan helpers (folder-name parsing and
// candidate matching).

func TestParseFolderName(t *testing.T) {
	cases := []struct {
		in        string
		wantTitle string
		wantYear  int
	}{
		{"Breaking Bad (2008)", "Breaking Bad", 2008},
		{"Breaking Bad", "Breaking Bad", 0},
		{"  The Wire (2002) ", "The Wire", 2002},
		{"No year here", "No year here", 0},
	}
	for _, c := range cases {
		title, year := ParseFolderName(c.in)
		if title != c.wantTitle || year != c.wantYear {
			t.Errorf("ParseFolderName(%q) = (%q, %d), want (%q, %d)",
				c.in, title, year, c.wantTitle, c.wantYear)
		}
	}
}

func TestMatchesSeries(t *testing.T) {
	series := Series{Title: "Fringe", Year: 2008}

	if !MatchesSeries("  fringe ", 0, series) {
		t.Error("case-insensitive title without year should match")
	}
	if !MatchesSeries("Fringe", 2008, series) {
		t.Error("matching year should match")
	}
	if MatchesSeries("Fringe", 2009, series) {
		t.Error("conflicting year must not match")
	}
	if MatchesSeries("Fringe S1", 0, series) {
		t.Error("different title must not match")
	}
}

func TestIsVideoFile(t *testing.T) {
	for _, name := range []string{"a.mkv", "b.MP4", "c.avi", "d.mov", "e.m4v", "f.wmv", "g.ts"} {
		if !IsVideoFile(name) {
			t.Errorf("IsVideoFile(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"readme.txt", "sample.mp3", "notes.md", "noext"} {
		if IsVideoFile(name) {
			t.Errorf("IsVideoFile(%q) = true, want false", name)
		}
	}
}
