package tv

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestPlanImport_Layout checks the destination path follows the per-series
// template: root/<Series (Year)>/Season <NN>/<Series> S<xx>E<yy>[ Title].<ext>
// (PLAN §6).
func TestPlanImport_Layout(t *testing.T) {
	root := filepath.Join(t.TempDir(), "media")
	os.MkdirAll(root, 0o755)

	src := filepath.Join(root, "downloads", "Breaking.Bad.S01E01.Piloto.1080p.WEB.x264.mkv")
	imp, err := PlanImport(root, "Breaking Bad", 2008, 1, 1, "Piloto", src)
	if err != nil {
		t.Fatalf("PlanImport: %v", err)
	}

	wantSeriesDir := filepath.Join(root, "Breaking Bad (2008)")
	wantSeasonDir := filepath.Join(wantSeriesDir, "Season 01")
	wantName := "Breaking Bad S01E01 Piloto.mkv"
	wantDest := filepath.Join(wantSeasonDir, wantName)

	if imp.DestPath != wantDest {
		t.Errorf("DestPath = %q, want %q", imp.DestPath, wantDest)
	}
	if imp.DestName != wantName {
		t.Errorf("DestName = %q, want %q", imp.DestName, wantName)
	}
	if imp.Extension != ".mkv" {
		t.Errorf("Extension = %q, want .mkv", imp.Extension)
	}
	if imp.Season != 1 || imp.Episode != 1 {
		t.Errorf("Season/Episode = %d/%d, want 1/1", imp.Season, imp.Episode)
	}
}

// TestPlanImport_NoYear checks the series dir omits the year when it is 0.
func TestPlanImport_NoYear(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "x.S01E01.mkv")
	imp, err := PlanImport(root, "Some Show", 0, 1, 1, "", src)
	if err != nil {
		t.Fatalf("PlanImport: %v", err)
	}
	wantSeriesDir := filepath.Join(root, "Some Show")
	if imp.DestPath != filepath.Join(wantSeriesDir, "Season 01", "Some Show S01E01.mkv") {
		t.Errorf("DestPath = %q, want under %q", imp.DestPath, wantSeriesDir)
	}
}

// TestPlanImport_Collision confirms that if the destination file already exists
// the planner returns ErrCollision rather than overwriting it (PLAN §6).
func TestPlanImport_Collision(t *testing.T) {
	root := filepath.Join(t.TempDir(), "media")
	seriesDir := filepath.Join(root, "Breaking Bad (2008)", "Season 01")
	os.MkdirAll(seriesDir, 0o755)
	dest := filepath.Join(seriesDir, "Breaking Bad S01E01 Piloto.mkv")
	if err := os.WriteFile(dest, []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}

	src := filepath.Join(root, "downloads", "Breaking.Bad.S01E01.Piloto.1080p.WEB.x264.mkv")
	_, err := PlanImport(root, "Breaking Bad", 2008, 1, 1, "Piloto", src)
	if err == nil {
		t.Fatal("expected ErrCollision, got nil")
	}
	if !errors.Is(err, ErrCollision) {
		t.Errorf("expected ErrCollision, got %v", err)
	}
}

// TestPlanImport_PathTraversal confirms a release/series name containing
// "../" cannot escape the media root (PLAN §8). The planner neutralises the
// separators so the destination is guaranteed to stay under the root — it never
// imports outside it.
func TestPlanImport_PathTraversal(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "x.S01E01.mkv")

	// A series title with a traversal component is neutralised into a safe
	// single path segment; the destination must remain under the root.
	imp, err := PlanImport(root, "../../etc/passwd", 2008, 1, 1, "", src)
	if err != nil {
		t.Fatalf("expected a neutralised (safe) destination, got error: %v", err)
	}
	if !withinRoot(root, imp.DestPath) {
		t.Errorf("destination %q escaped media root %q", imp.DestPath, root)
	}
	// The traversal must not survive as a directory component.
	if filepath.Dir(imp.DestPath) == filepath.Join(root, "..") {
		t.Error("traversal component was not neutralised")
	}

	// An episode title with a traversal component is likewise neutralised.
	imp2, err := PlanImport(root, "Safe Show", 2008, 1, 1, "../../evil", src)
	if err != nil {
		t.Fatalf("expected a neutralised destination, got error: %v", err)
	}
	if !withinRoot(root, imp2.DestPath) {
		t.Errorf("destination %q escaped media root %q", imp2.DestPath, root)
	}
}

// TestPlanImport_EpisodeTitleSanitised confirms the episode title is cleaned of
// path separators so it can never introduce a directory into the filename.
func TestPlanImport_EpisodeTitleSanitised(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "x.S01E01.mkv")
	imp, err := PlanImport(root, "Show", 2020, 2, 3, "ep/sub/dir", src)
	if err != nil {
		t.Fatalf("PlanImport: %v", err)
	}
	// Path separators in the episode title are stripped (not turned into
	// subdirectories), so the result is a single safe filename component.
	if imp.DestName != "Show S02E03 epsubdir.mkv" {
		t.Errorf("DestName = %q, want %q", imp.DestName, "Show S02E03 epsubdir.mkv")
	}
}

// TestPlanImport_EmptyRoot guards the empty-root edge case.
func TestPlanImport_EmptyRoot(t *testing.T) {
	_, err := PlanImport("", "Show", 2020, 1, 1, "", "/tmp/x.S01E01.mkv")
	if err == nil {
		t.Fatal("expected an error for an empty media root")
	}
}

// TestWithinRoot is a focused unit test on the containment helper.
func TestWithinRoot(t *testing.T) {
	root := "/media/tv"
	cases := []struct {
		p    string
		want bool
	}{
		{"/media/tv", true},
		{"/media/tv/Show (2020)/Season 01", true},
		{"/media/tv2", false},
		{"/media/tvx/Season 01", false},
		{"/etc/passwd", false},
	}
	for _, c := range cases {
		if got := withinRoot(root, c.p); got != c.want {
			t.Errorf("withinRoot(%q, %q) = %v, want %v", root, c.p, got, c.want)
		}
	}
}
