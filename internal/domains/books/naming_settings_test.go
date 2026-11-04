package books

import (
	"strings"
	"testing"

	"github.com/brandenk514/mediarr/internal/settings"
)

// B5 integration: the books naming CONFIG (settings) drives the working
// naming engine end-to-end — the default config renders the default template,
// and the space/case-handling knobs change the output.
func TestB5_RenderBookPathFromConfig(t *testing.T) {
	ctx := BookNamingContext{
		AuthorName:  "Ursula K. Le Guin",
		BookTitle:   "The Dispossessed",
		ReleaseYear: 1974,
	}

	got, err := RenderBookPath(settings.DefaultBookNaming(), ctx)
	if err != nil {
		t.Fatalf("RenderBookPath: %v", err)
	}
	if want := "Ursula K. Le Guin - The Dispossessed (1974)"; got != want {
		t.Errorf("default render = %q, want %q", got, want)
	}
}

func TestB5_RenderBookPath_SpaceCaseHandling(t *testing.T) {
	ctx := BookNamingContext{
		AuthorName:  "Asimov, Isaac",
		BookTitle:   "Foundation",
		ReleaseYear: 1951,
	}

	// underscore + lowercase
	c := settings.DefaultBookNaming()
	c.SpaceHandling = "underscore"
	c.CaseHandling = "lowercase"
	got, err := RenderBookPath(c, ctx)
	if err != nil {
		t.Fatalf("RenderBookPath: %v", err)
	}
	if !strings.Contains(got, "asimov") || !strings.Contains(got, "_") {
		t.Errorf("expected lowercased underscore output, got %q", got)
	}

	// dash + uppercase
	c2 := settings.DefaultBookNaming()
	c2.SpaceHandling = "dash"
	c2.CaseHandling = "uppercase"
	got2, err := RenderBookPath(c2, ctx)
	if err != nil {
		t.Fatalf("RenderBookPath: %v", err)
	}
	if !strings.Contains(got2, "ASIMOV") || !strings.Contains(got2, "-") {
		t.Errorf("expected uppercased dash output, got %q", got2)
	}
}

func TestB5_RenderBookPath_CustomTemplate(t *testing.T) {
	ctx := BookNamingContext{
		AuthorName:     "Le Guin, Ursula K.",
		BookTitle:      "A Wizard of Earthsea",
		ReleaseYear:    1968,
		SeriesTitle:    "Earthsea",
		SeriesPosition: "1",
	}
	c := settings.DefaultBookNaming()
	c.StandardFormat = "{Author Name} - {Book Title} ({Release Year}) [#{Book SeriesPosition} {Book Series}]"
	got, err := RenderBookPath(c, ctx)
	if err != nil {
		t.Fatalf("RenderBookPath: %v", err)
	}
	if !strings.Contains(got, "Le Guin, Ursula K.") || !strings.Contains(got, "Earthsea") {
		t.Errorf("expected author + series in %q", got)
	}
}

func TestB5_RenderAuthorFolderFromConfig(t *testing.T) {
	ctx := BookNamingContext{AuthorName: "Ursula K. Le Guin"}
	got, err := RenderBookAuthorFolder(settings.DefaultBookNaming(), ctx)
	if err != nil {
		t.Fatalf("RenderBookAuthorFolder: %v", err)
	}
	if got != "Ursula K. Le Guin" {
		t.Errorf("author folder = %q, want %q", got, "Ursula K. Le Guin")
	}
}

// B5: RenameBooks=false keeps the original filename (no template applied).
func TestB5_RenderBookPath_NoRename(t *testing.T) {
	ctx := BookNamingContext{
		AuthorName:       "X",
		BookTitle:        "Y",
		ReleaseYear:      2000,
		OriginalFilename: "scene-release.epub",
	}
	c := settings.DefaultBookNaming()
	c.RenameBooks = false
	got, err := RenderBookPath(c, ctx)
	if err != nil {
		t.Fatalf("RenderBookPath: %v", err)
	}
	if got != "scene-release.epub" {
		t.Errorf("no-rename should keep the original filename, got %q", got)
	}
}
