package books

import (
	"strings"
	"testing"
)

// testContext returns a fully-populated BookNamingContext for token tests.
func testContext() BookNamingContext {
	return BookNamingContext{
		AuthorName:                  "Ursula K. Le Guin",
		AuthorSortName:              "Le Guin, Ursula K.",
		AuthorDisambiguation:        "Ursula K. Le Guin (author)",
		BookTitle:                   "The Dispossessed",
		BookSubtitle:                "An Ecology of Impossibilities",
		BookType:                    "Book",
		SeriesTitle:                 "Hain",
		SeriesPosition:              "2",
		ReleaseYear:                 1974,
		ReleaseYearFirst:            1974,
		EditionYear:                 1978,
		PartNumber:                  1,
		PartCount:                   1,
		QualityTitle:                "AZW3",
		QualityProper:               "Proper",
		OriginalTitle:               "the.dispossessed.1974.azw3",
		OriginalFilename:            "the.dispossessed.1974",
		ReleaseGroup:                "YIFY",
		MediaInfoAudioCodec:         "MP3",
		MediaInfoAudioChannels:      "2",
		MediaInfoAudioBitRate:       "320",
		MediaInfoAudioBitsPerSample: "16",
		MediaInfoAudioSampleRate:    "44100",
		CustomFormats:               []string{"Prefer AZW3"},
	}
}

func single(t *testing.T, template string) string {
	t.Helper()
	got, err := RenderBookFileName(DefaultNamingOptions, template, testContext())
	if err != nil {
		t.Fatalf("render %q: %v", template, err)
	}
	return got
}

func TestRenderBookFileName_TokenSet(t *testing.T) {
	cases := []struct {
		template string
		want     string
	}{
		// Author
		{"{Author Name}", "Ursula K. Le Guin"},
		{"{Author CleanName}", "Ursula K. Le Guin"},
		{"{Author NameThe}", "Ursula K. Le Guin"},
		{"{Author SortName}", "Le Guin, Ursula K"},
		{"{Author NameFirstCharacter}", "U"},
		{"{Author Disambiguation}", "Ursula K. Le Guin (author)"},
		// Book
		{"{Book Title}", "The Dispossessed"},
		{"{Book TitleThe}", "Dispossessed, The"},
		{"{Book Subtitle}", "An Ecology of Impossibilities"},
		{"{Book SubtitleThe}", "Ecology of Impossibilities, An"},
		{"{Book Type}", "Book"},
		{"{Book Series}", "Hain"},
		{"{Book SeriesPosition}", "2"},
		{"{Book SeriesTitle}", "Hain #2"},
		// Year
		{"{Release Year}", "1974"},
		{"{Release YearFirst}", "1974"},
		{"{Edition Year}", "1978"},
		// Quality / file
		{"{Quality Full}", "AZW3"},
		{"{Quality Title}", "AZW3"},
		{"{Quality Proper}", "Proper"},
		{"{Original Title}", "the.dispossessed.1974.azw3"},
		{"{Original Filename}", "the.dispossessed.1974"},
		{"{Release Group}", "YIFY"},
		// Media info
		{"{MediaInfo AudioCodec}", "MP3"},
		{"{MediaInfo AudioChannels}", "2"},
		{"{MediaInfo AudioBitRate}", "320"},
		{"{MediaInfo AudioBitsPerSample}", "16"},
		{"{MediaInfo AudioSampleRate}", "44100"},
		// Custom formats
		{"{Custom Formats}", "Prefer AZW3"},
		// Multi-token + path
		{`{Author Name}/{Book Title} ({Release Year})`, "Ursula K. Le Guin/The Dispossessed (1974)"},
		{"{Book Title} - {Book Subtitle}", "The Dispossessed - An Ecology of Impossibilities"},
	}
	for _, tc := range cases {
		got := single(t, tc.template)
		if got != tc.want {
			t.Errorf("template %q = %q, want %q", tc.template, got, tc.want)
		}
	}
}

func TestRenderBookFileName_NoValueTokensRenderNothing(t *testing.T) {
	c := testContext()
	c.AuthorDisambiguation = "" // unambiguous author
	c.BookSubtitle = ""         // no subtitle
	c.ReleaseGroup = ""         // unknown group

	got, err := RenderBookFileName(DefaultNamingOptions,
		"{Book Title} {(Author Disambiguation)} {[Book Subtitle]} by {Release Group}", c)
	if err != nil {
		t.Fatal(err)
	}
	// The empty tokens (and their in-brace prefix/suffix) vanish entirely, and
	// the unknown release group falls back to "Readarr".
	want := "The Dispossessed by Readarr"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderBookFileName_PartsAndMedium(t *testing.T) {
	c := testContext()
	c.PartNumber = 2
	c.PartCount = 3

	cases := []struct {
		template string
		want     string
	}{
		{"{Book Title} {PartNumber}", "The Dispossessed 2"},
		{"{Book Title} {PartNumber:00}", "The Dispossessed 02"},
		{"{Book Title} {PartCount:00}", "The Dispossessed 03"},
		{"{Book Title} {medium:00}", "The Dispossessed 02"},
		{"{Book Title} {medium:0}", "The Dispossessed 2"},
		{"{Book Title} {Medium Format}", "The Dispossessed CD 2"},
	}
	for _, tc := range cases {
		got, err := RenderBookFileName(DefaultNamingOptions, tc.template, c)
		if err != nil {
			t.Fatalf("render %q: %v", tc.template, err)
		}
		if got != tc.want {
			t.Errorf("template %q = %q, want %q", tc.template, got, tc.want)
		}
	}

	// Single-part book: part tokens vanish entirely.
	c.PartCount = 1
	got, err := RenderBookFileName(DefaultNamingOptions, "{Book Title} {PartNumber:00}", c)
	if err != nil {
		t.Fatal(err)
	}
	if want := "The Dispossessed"; got != want {
		t.Errorf("single-part: got %q, want %q", got, want)
	}
}

func TestRenderBookFileName_TokenCaseRule(t *testing.T) {
	cases := []struct {
		template string
		want     string
	}{
		{"{Book Title}", "The Dispossessed"},   // mixed → as-is
		{"{book title}", "the dispossessed"},   // all-lower → lowercase
		{"{BOOK TITLE}", "THE DISPOSSESSED"},   // all-upper → uppercase
		{"{Book_Title}", "The_Dispossessed"},   // underscore separator
		{"{author_name}", "ursula_k._le_guin"}, // lowercase + separator
	}
	for _, tc := range cases {
		got := single(t, tc.template)
		if got != tc.want {
			t.Errorf("template %q = %q, want %q", tc.template, got, tc.want)
		}
	}
}

func TestRenderBookFileName_TitleTheNoSub(t *testing.T) {
	c := testContext()
	c.BookTitle = "The Hobbit: An Unexpected Journey"
	c.BookSubtitle = ""

	cases := []struct {
		template string
		want     string
	}{
		{"{Book TitleNoSub}", "The Hobbit"},
		{"{Book CleanTitleNoSub}", "The Hobbit"},
		{"{Book TitleTheNoSub}", "Hobbit, The"},
	}
	for _, tc := range cases {
		got, err := RenderBookFileName(DefaultNamingOptions, tc.template, c)
		if err != nil {
			t.Fatal(err)
		}
		if got != tc.want {
			t.Errorf("template %q = %q, want %q", tc.template, got, tc.want)
		}
	}
}

func TestRenderBookFileName_CleanTitle(t *testing.T) {
	c := testContext()
	c.BookTitle = "A! Book & Another (2020)"
	got, err := RenderBookFileName(DefaultNamingOptions, "{Book CleanTitle}", c)
	if err != nil {
		t.Fatal(err)
	}
	// "&" → "and", the ( ) brackets dropped; "!" is NOT whitespace-bounded so it
	// stays: "A! Book and Another 2020".
	if want := "A! Book and Another 2020"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderBookFileName_YearUnknown(t *testing.T) {
	c := testContext()
	c.ReleaseYear = 0
	c.ReleaseYearFirst = 0
	c.EditionYear = 0
	got, err := RenderBookFileName(DefaultNamingOptions,
		"{Book Title} ({Release Year}) ({Release YearFirst}) ({Edition Year})", c)
	if err != nil {
		t.Fatal(err)
	}
	want := "The Dispossessed (Unknown) (Unknown) (Unknown)"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderBookFileName_RenameBooksOff(t *testing.T) {
	o := DefaultNamingOptions
	o.RenameBooks = false
	got, err := RenderBookFileName(o, "{Author Name}/{Book Title}", testContext())
	if err != nil {
		t.Fatal(err)
	}
	if want := "the.dispossessed.1974.azw3"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderBookFileName_EmptyTemplate(t *testing.T) {
	if _, err := RenderBookFileName(DefaultNamingOptions, "  ", testContext()); err == nil {
		t.Error("expected error for empty template")
	}
}

func TestRenderBookFileName_SpaceAndCaseHandling(t *testing.T) {
	c := testContext()

	o := DefaultNamingOptions
	o.SpaceHandling = "period"
	got, err := RenderBookFileName(o, "{Book Title}", c)
	if err != nil {
		t.Fatal(err)
	}
	if want := "The.Dispossessed"; got != want {
		t.Errorf("space=period: got %q, want %q", got, want)
	}

	o2 := DefaultNamingOptions
	o2.CaseHandling = "lowercase"
	got2, _ := RenderBookFileName(o2, "{Book Title}", c)
	if want := "the dispossessed"; got2 != want {
		t.Errorf("case=lowercase: got %q, want %q", got2, want)
	}

	o3 := DefaultNamingOptions
	o3.SpaceHandling = "underscore"
	got3, _ := RenderBookFileName(o3, "{Book Title}", c)
	if want := "The_Dispossessed"; got3 != want {
		t.Errorf("space=underscore: got %q, want %q", got3, want)
	}
}

func TestRenderBookFileName_ColonReplacement(t *testing.T) {
	c := testContext()
	c.BookTitle = "The Hobbit: An Unexpected Journey"

	o := DefaultNamingOptions
	o.ColonReplacement = " - "
	got, err := RenderBookFileName(o, "{Book Title}", c)
	if err != nil {
		t.Fatal(err)
	}
	if want := "The Hobbit - An Unexpected Journey"; got != want {
		t.Errorf("colon repl: got %q, want %q", got, want)
	}

	// No replacement char → colon removed (spaces preserved).
	o2 := DefaultNamingOptions
	got2, _ := RenderBookFileName(o2, "{Book Title}", c)
	if want := "The Hobbit An Unexpected Journey"; got2 != want {
		t.Errorf("colon removed: got %q, want %q", got2, want)
	}
}

func TestRenderBookFileName_IllegalCharacters(t *testing.T) {
	c := testContext()
	c.BookTitle = `Weird <Book> | "Quoted" ? *`
	o := DefaultNamingOptions
	o.ReplaceIllegalCharacters = true
	got, err := RenderBookFileName(o, "{Book Title}", c)
	if err != nil {
		t.Fatal(err)
	}
	// < > ? * | " cleaned; the rest preserved.
	if strings.ContainsAny(got, `<>|?"*`) {
		t.Errorf("illegal chars present in %q", got)
	}
}

func TestRenderAuthorFolder(t *testing.T) {
	cases := []struct {
		template string
		want     string
	}{
		{"{Author Name}", "Ursula K. Le Guin"},
		{"{Author CleanName}", "Ursula K. Le Guin"},
		{"{Author NameFirstCharacter}/{Author Name}", "U/Ursula K. Le Guin"},
		{"{Author NameThe}", "Ursula K. Le Guin"},
		{"{Author SortName}", "Le Guin, Ursula K"},
	}
	for _, tc := range cases {
		got, err := RenderAuthorFolder(DefaultNamingOptions, tc.template, testContext())
		if err != nil {
			t.Fatalf("author folder %q: %v", tc.template, err)
		}
		if got != tc.want {
			t.Errorf("author folder %q = %q, want %q", tc.template, got, tc.want)
		}
	}
}

func TestRenderAuthorFolder_EmptyTemplate(t *testing.T) {
	if _, err := RenderAuthorFolder(DefaultNamingOptions, "  ", testContext()); err == nil {
		t.Error("expected error for empty author folder template")
	}
}
