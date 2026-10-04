package music

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestPlanTrackImport_Layout checks the destination path follows the
// per-artist template: root/<Artist>/<Album>/<NN>-<NN> <Title>.<ext>
// (PLAN §6).
func TestPlanTrackImport_Layout(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "downloads", "nowoman.flac")

	imp, err := PlanTrackImport(root, "Bob Marley", "Legend", 1, 1, "No Woman, No Man", src)
	if err != nil {
		t.Fatalf("PlanTrackImport: %v", err)
	}

	wantName := "01-01 No Woman, No Man.flac"
	wantDir := filepath.Join(root, "Bob Marley", "Legend")
	wantDest := filepath.Join(wantDir, wantName)

	if imp.DestName != wantName {
		t.Errorf("DestName = %q, want %q", imp.DestName, wantName)
	}
	if imp.DestPath != wantDest {
		t.Errorf("DestPath = %q, want %q", imp.DestPath, wantDest)
	}
	if imp.Extension != ".flac" {
		t.Errorf("Extension = %q, want .flac", imp.Extension)
	}
	if imp.Disc != 1 || imp.Number != 1 {
		t.Errorf("Disc/Number = %d/%d, want 1/1", imp.Disc, imp.Number)
	}
}

// TestPlanTrackImport_DiscNumber checks multi-disc numbering: a disc-2 track 3
// lands as 02-03.
func TestPlanTrackImport_DiscNumber(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "x.flac")
	imp, err := PlanTrackImport(root, "Artist", "Album", 2, 3, "Title", src)
	if err != nil {
		t.Fatalf("PlanTrackImport: %v", err)
	}
	if imp.DestName != "02-03 Title.flac" {
		t.Errorf("DestName = %q, want %q", imp.DestName, "02-03 Title.flac")
	}
}

// TestPlanTrackImport_DiscZeroDefaults confirms a zero disc defaults to 1.
func TestPlanTrackImport_DiscZeroDefaults(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "x.flac")
	imp, err := PlanTrackImport(root, "Artist", "Album", 0, 1, "Title", src)
	if err != nil {
		t.Fatalf("PlanTrackImport: %v", err)
	}
	if imp.Disc != 1 {
		t.Errorf("Disc = %d, want 1 (zero defaults)", imp.Disc)
	}
}

// TestPlanTrackImport_Collision confirms that if the destination file already
// exists the planner returns ErrCollision rather than overwriting it
// (PLAN §6).
func TestPlanTrackImport_Collision(t *testing.T) {
	root := t.TempDir()
	albumDir := filepath.Join(root, "Bob Marley", "Legend")
	os.MkdirAll(albumDir, 0o755)
	dest := filepath.Join(albumDir, "01-01 No Woman, No Man.flac")
	if err := os.WriteFile(dest, []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}

	src := filepath.Join(root, "downloads", "nowoman.flac")
	_, err := PlanTrackImport(root, "Bob Marley", "Legend", 1, 1, "No Woman, No Man", src)
	if err == nil {
		t.Fatal("expected ErrCollision, got nil")
	}
	if !errors.Is(err, ErrCollision) {
		t.Errorf("expected ErrCollision, got %v", err)
	}
}

// TestPlanTrackImport_PathTraversal confirms a release/artist name containing
// "../" cannot escape the media root (PLAN §8). The planner neutralises the
// separators so the destination stays under the root.
func TestPlanTrackImport_PathTraversal(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "x.flac")

	imp, err := PlanTrackImport(root, "../../etc/passwd", "Album", 1, 1, "Title", src)
	if err != nil {
		t.Fatalf("expected a neutralised (safe) destination, got error: %v", err)
	}
	if !withinRoot(root, imp.DestPath) {
		t.Errorf("destination %q escaped media root %q", imp.DestPath, root)
	}

	imp2, err := PlanTrackImport(root, "Artist", "Album", 1, 1, "../../evil", src)
	if err != nil {
		t.Fatalf("expected a neutralised destination, got error: %v", err)
	}
	if !withinRoot(root, imp2.DestPath) {
		t.Errorf("destination %q escaped media root %q", imp2.DestPath, root)
	}
}

// TestPlanTrackImport_EmptyRoot guards the empty-root edge case.
func TestPlanTrackImport_EmptyRoot(t *testing.T) {
	if _, err := PlanTrackImport("", "Artist", "Album", 1, 1, "Title", "/tmp/x.flac"); err == nil {
		t.Fatal("expected an error for an empty media root")
	}
}

// TestPlanAlbumImport_Layout checks the whole-album destination:
// root/<Artist>/<Album>/<Album>.<ext>.
func TestPlanAlbumImport_Layout(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "downloads", "legend.flac")

	imp, err := PlanAlbumImport(root, "Bob Marley", "Legend", src)
	if err != nil {
		t.Fatalf("PlanAlbumImport: %v", err)
	}
	wantDest := filepath.Join(root, "Bob Marley", "Legend", "Legend.flac")
	if imp.DestPath != wantDest {
		t.Errorf("DestPath = %q, want %q", imp.DestPath, wantDest)
	}
	if imp.DestName != "Legend.flac" {
		t.Errorf("DestName = %q, want Legend.flac", imp.DestName)
	}
}

// TestPlanAlbumImport_Collision confirms an existing whole-album destination
// collides.
func TestPlanAlbumImport_Collision(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "Bob Marley", "Legend")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "Legend.flac"), []byte("x"), 0o644)

	src := filepath.Join(root, "downloads", "legend.flac")
	_, err := PlanAlbumImport(root, "Bob Marley", "Legend", src)
	if !errors.Is(err, ErrCollision) {
		t.Errorf("expected ErrCollision, got %v", err)
	}
}

// TestAlbumDestPath_NoCollisionCheck confirms AlbumDestPath computes the same
// destination without a collision check (for the import pipeline).
func TestAlbumDestPath_NoCollisionCheck(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "downloads", "legend.flac")
	got, err := AlbumDestPath(root, "Bob Marley", "Legend", src)
	if err != nil {
		t.Fatalf("AlbumDestPath: %v", err)
	}
	want := filepath.Join(root, "Bob Marley", "Legend", "Legend.flac")
	if got != want {
		t.Errorf("AlbumDestPath = %q, want %q", got, want)
	}
}

// TestWithinRoot is a focused unit test on the containment helper.
func TestWithinRoot(t *testing.T) {
	root := "/media/music"
	cases := []struct {
		p    string
		want bool
	}{
		{"/media/music", true},
		{"/media/music/Bob Marley/Legend", true},
		{"/media/musicx", false},
		{"/media/other", false},
		{"/etc/passwd", false},
	}
	for _, c := range cases {
		if got := withinRoot(root, c.p); got != c.want {
			t.Errorf("withinRoot(%q, %q) = %v, want %v", root, c.p, got, c.want)
		}
	}
}
