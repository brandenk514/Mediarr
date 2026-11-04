package books

import (
	"os"
	"path/filepath"
	"testing"
)

// TestIsBookFile covers the extension-based book-file detector: the PLAN
// §15.4 formats (case-insensitive), and non-book files (including a file
// whose NAME contains a format token but whose extension is not a book
// format — the ParseFormat token match must not leak into the walk).
func TestIsBookFile(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"book.epub", true},
		{"Book.EPUB", true},
		{"book.mobi", true},
		{"book.azw3", true},
		{"book.pdf", false},
		{"book.txt", false},
		{"notes.docx", false},
		{"book", false},
		{"mobi.epub", true},       // extension wins; name token irrelevant
		{"epub notes.txt", false}, // name token in a non-book file
	}
	for _, c := range cases {
		if got := IsBookFile(c.name); got != c.want {
			t.Errorf("IsBookFile(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

// writeBookTree lays out the shared unmapped-scan fixture under root:
//
//	<root>/Ursula K. Le Guin/the-dispossessed.epub  (monitored author -> mapped)
//	<root>/tolkien/hobbit.epub                      (case-variant   -> mapped)
//	<root>/Other Author/novel.mobi                  (unmonitored    -> unmapped)
//	<root>/loose.azw3                               (loose          -> unmapped)
//	<root>/notes.txt, <root>/Other Author/scan.pdf  (non-book files -> ignored)
func writeBookTree(t *testing.T, root string) {
	t.Helper()
	dirs := []string{
		filepath.Join(root, "Ursula K. Le Guin"),
		filepath.Join(root, "tolkien"),
		filepath.Join(root, "Other Author"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		filepath.Join(root, "Ursula K. Le Guin", "the-dispossessed.epub"): "epub-data",
		filepath.Join(root, "tolkien", "hobbit.epub"):                     "azw3-data",
		filepath.Join(root, "Other Author", "novel.mobi"):                 "mobi-data",
		filepath.Join(root, "loose.azw3"):                                 "azw3-data",
		filepath.Join(root, "notes.txt"):                                  "not a book",
		filepath.Join(root, "Other Author", "scan.pdf"):                   "not a book",
	}
	for p, content := range files {
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestScanUnmappedFiles is the B3 "Unmapped Files" domain contract: a
// read-only walk of the media root that lists book files NOT under a
// top-level folder named after a monitored author. Mapping is
// case-insensitive (the folder "tolkien" maps to the author "Tolkien");
// unmonitored authors' files and loose root files are unmapped; non-book
// files are ignored; the report is sorted by path.
func TestScanUnmappedFiles(t *testing.T) {
	root := t.TempDir()
	writeBookTree(t, root)
	monitored := map[string]bool{
		"Ursula K. Le Guin": true,
		"Tolkien":           true, // folder on disk is "tolkien" (case variant)
	}
	files, err := ScanUnmappedFiles(root, monitored)
	if err != nil {
		t.Fatalf("ScanUnmappedFiles: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("want 2 unmapped files, got %d: %+v", len(files), files)
	}
	// Sorted by path; assert the exact expected set + qualities.
	byBase := map[string]UnmappedFile{}
	for i, f := range files {
		byBase[filepath.Base(f.Path)] = f
		if i > 0 && files[i-1].Path >= f.Path {
			t.Errorf("report not sorted by path: %q before %q", files[i-1].Path, f.Path)
		}
		for _, mapped := range []string{"the-dispossessed", "hobbit"} {
			if filepath.Base(f.Path) == mapped+".epub" {
				t.Errorf("mapped file leaked into the unmapped report: %s", f.Path)
			}
		}
		if f.Size == 0 {
			t.Errorf("file %s: size should be non-zero", f.Path)
		}
		if f.DateAdded.IsZero() {
			t.Errorf("file %s: date_added should be set", f.Path)
		}
	}
	if f, ok := byBase["novel.mobi"]; !ok || f.Quality != "MOBI" {
		t.Errorf("unmapped novel.mobi: want quality MOBI, got %+v (present=%v)", f, ok)
	}
	if f, ok := byBase["loose.azw3"]; !ok || f.Quality != "AZW3" {
		t.Errorf("unmapped loose.azw3: want quality AZW3, got %+v (present=%v)", f, ok)
	}
}

// TestScanUnmappedFiles_UnmonitorSurfacesFiles is the Readarr semantic:
// unmonitoring an author surfaces its files in the report (the same folder
// is mapped while monitored and unmapped after).
func TestScanUnmappedFiles_UnmonitorSurfacesFiles(t *testing.T) {
	root := t.TempDir()
	writeBookTree(t, root)
	monitored := map[string]bool{"Ursula K. Le Guin": true}

	mapped, err := ScanUnmappedFiles(root, monitored)
	if err != nil {
		t.Fatalf("ScanUnmappedFiles (monitored): %v", err)
	}
	for _, f := range mapped {
		if filepath.Base(f.Path) == "the-dispossessed.epub" {
			t.Errorf("monitored author's file listed unmapped: %s", f.Path)
		}
	}

	// Unmonitor: the monitored set no longer contains the author (key
	// absence, the same contract as the music L5 scan — the service
	// pre-filters by Monitored before calling).
	delete(monitored, "Ursula K. Le Guin")
	unmapped, err := ScanUnmappedFiles(root, monitored)
	if err != nil {
		t.Fatalf("ScanUnmappedFiles (unmonitored): %v", err)
	}
	found := false
	for _, f := range unmapped {
		if filepath.Base(f.Path) == "the-dispossessed.epub" {
			found = true
			if f.Quality != "EPUB" {
				t.Errorf("the-dispossessed.epub: want quality EPUB, got %q", f.Quality)
			}
		}
	}
	if !found {
		t.Error("unmonitoring the author must surface its files")
	}
}

// TestScanUnmappedFiles_EmptyAndMissing covers the empty-report contract
// (an empty list, never nil) and the root-missing error (the API
// pre-checks, but the domain must not swallow it).
func TestScanUnmappedFiles_EmptyAndMissing(t *testing.T) {
	empty := t.TempDir()
	files, err := ScanUnmappedFiles(empty, map[string]bool{"Anyone": true})
	if err != nil {
		t.Fatalf("empty root: %v", err)
	}
	if files == nil {
		t.Fatal("empty scan must return an empty list, not nil")
	}
	if len(files) != 0 {
		t.Fatalf("empty scan must be empty, got %+v", files)
	}

	if _, err := ScanUnmappedFiles(filepath.Join(empty, "does-not-exist"), nil); err == nil {
		t.Error("missing root: want an error, got nil")
	}
}
