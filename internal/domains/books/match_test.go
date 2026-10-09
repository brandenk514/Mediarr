package books

import "testing"

func TestParseName(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		author string
		title  string
	}{
		{"author-title epub", "Ursula K. Le Guin - The Dispossessed [EPUB]", "Ursula K. Le Guin", "The Dispossessed"},
		{"author-title azw3", "Brandon Sanderson - Mistborn [AZW3]", "Brandon Sanderson", "Mistborn"},
		{"paren tag stripped", "Aldous Huxley - Brave New World (e-book)", "Aldous Huxley", "Brave New World"},
		{"no author", "The Dispossessed [EPUB]", "", "The Dispossessed"},
		{"empty", "", "", ""},
		{"keeps real subtitle", "X - Title: A Novel [EPUB]", "X", "Title: A Novel"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseName(tt.in)
			if got.Author != tt.author {
				t.Errorf("Author = %q, want %q", got.Author, tt.author)
			}
			if got.Title != tt.title {
				t.Errorf("Title = %q, want %q", got.Title, tt.title)
			}
		})
	}
}

func TestBuildCandidate_Match(t *testing.T) {
	tests := []struct {
		name    string
		release string
		profile string
		wantOK  bool
		wantFmt string
	}{
		{"epub best", "A - T [EPUB]", "Best", true, FormatEPUB},
		{"mobi best", "A - T [MOBI]", "Best", true, FormatMOBI},
		{"azw3 kindle", "A - T [AZW3]", "Kindle", true, FormatAZW3},
		{"mobi not in EPUB profile", "A - T [MOBI]", "EPUB", false, FormatMOBI},
		{"unsupported pdf", "A - T [PDF]", "Best", false, ""},
		{"paperback is physical, no match", "A - T [Paperback]", "Best", false, ""},
		{"any accepts supported", "A - T [MOBI]", "Any", true, FormatMOBI},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, ok := BuildCandidate(tt.release, "idx", tt.profile)
			if ok != tt.wantOK {
				t.Fatalf("BuildCandidate matched = %v, want %v", ok, tt.wantOK)
			}
			if tt.wantOK && c.Format.Format != tt.wantFmt {
				t.Errorf("format = %q, want %q", c.Format.Format, tt.wantFmt)
			}
		})
	}
}

func TestBestForTitle_RanksFormats(t *testing.T) {
	// Under the "Best" profile an EPUB should beat a MOBI for the same title.
	epub, _ := BuildCandidate("A - T [EPUB]", "idx", "Best")
	mobi, _ := BuildCandidate("A - T [MOBI]", "idx", "Best")
	// A distractor that does not match the title.
	other, _ := BuildCandidate("B - Other [EPUB]", "idx", "Best")

	best, ok := BestForTitle([]CandidateRelease{mobi, epub, other}, "A", "T")
	if !ok {
		t.Fatal("BestForTitle ok = false, want true")
	}
	if best.Title != "A - T [EPUB]" {
		t.Fatalf("best = %q, want the EPUB release (beats MOBI)", best.Title)
	}
}

func TestBestForTitle_NoMatch(t *testing.T) {
	mobi, _ := BuildCandidate("A - T [MOBI]", "idx", "Best")
	_, ok := BestForTitle([]CandidateRelease{mobi}, "Different", "T")
	if ok {
		t.Fatal("BestForTitle ok = true for a non-matching author, want false")
	}
	if _, ok := BestForTitle(nil, "A", "T"); ok {
		t.Fatal("BestForTitle ok = true for no candidates, want false")
	}
}

func TestNormalise(t *testing.T) {
	tests := []struct{ in, want string }{
		{"The  Great  Gatsby", "the great gatsby"},
		{"A Novel!", "a novel"},
		{"  spaced  ", "spaced"},
	}
	for _, tt := range tests {
		if got := Normalise(tt.in); got != tt.want {
			t.Errorf("Normalise(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSortCandidates(t *testing.T) {
	epub, _ := BuildCandidate("A - T [EPUB]", "i", "Best")
	mobi, _ := BuildCandidate("A - T [MOBI]", "i", "Best")
	cands := []CandidateRelease{mobi, epub}
	SortCandidates(cands)
	if cands[0].Title != "A - T [EPUB]" {
		t.Fatalf("after sort, [0] = %q, want the EPUB (higher score first)", cands[0].Title)
	}
}
