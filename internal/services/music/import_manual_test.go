package music

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	dom "github.com/brandenk514/mediarr/internal/domains/music"
	wdom "github.com/brandenk514/mediarr/internal/domains/wanted"
)

// importFixture builds a monitored artist + monitored album with two known
// tracks and returns the service + ids. The album file destination layout is
// the documented one: root/<Artist>/<Album>/...
func importFixture(t *testing.T, mediaRoot string) (*Service, *memRepo, int64, int64, int64, int64) {
	t.Helper()
	repo := newMemRepo()
	svc := New(Deps{Repo: repo, MediaRoot: mediaRoot, DefaultProfile: "Lossless"})
	ctx := context.Background()

	artistID, err := repo.CreateArtist(ctx, dom.Artist{Name: "Bob Marley", Monitored: true})
	if err != nil {
		t.Fatalf("create artist: %v", err)
	}
	albumID, err := repo.CreateAlbum(ctx, dom.Album{ArtistID: artistID, Name: "Legend", Monitored: true})
	if err != nil {
		t.Fatalf("create album: %v", err)
	}
	t1, err := repo.CreateTrack(ctx, dom.Track{AlbumID: albumID, Disc: 1, Number: 1, Title: "No Woman, No Man"})
	if err != nil {
		t.Fatalf("create track 1: %v", err)
	}
	t2, err := repo.CreateTrack(ctx, dom.Track{AlbumID: albumID, Disc: 1, Number: 2, Title: "Three Little Birds"})
	if err != nil {
		t.Fatalf("create track 2: %v", err)
	}
	return svc, repo, artistID, albumID, t1, t2
}

func writeAudioFile(t *testing.T, dir, name, codec string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestManualImport_SingleTrackFile(t *testing.T) {
	mediaRoot := t.TempDir()
	svc, repo, artistID, albumID, _, t2 := importFixture(t, mediaRoot)
	// The track's wanted row is pending.
	if err := repo.EnsureWantedTrack(context.Background(), albumID, t2); err != nil {
		t.Fatal(err)
	}
	src := writeAudioFile(t, mediaRoot, "downloads/01-02 Three Little Birds.flac", "FLAC")

	rep, err := svc.ManualImport(context.Background(), wdom.ManualImport{Kind: "music", AlbumID: albumID, FilePath: src})
	if err != nil {
		t.Fatalf("ManualImport: %v", err)
	}
	if !rep.Imported {
		t.Fatalf("report = %+v, want imported", rep)
	}
	// The file is placed at the documented per-track layout.
	dest := filepath.Join(mediaRoot, "Bob Marley", "Legend", "01-02 Three Little Birds.flac")
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("placed file missing: %v", err)
	}
	// The covered track is marked imported with the detected codec; the
	// other track is untouched.
	tr, err := repo.GetTrack(context.Background(), albumID, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if tr.Status != "FLAC" {
		t.Errorf("track status = %q, want FLAC", tr.Status)
	}
	tr1, err := repo.GetTrack(context.Background(), albumID, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if tr1.Status != "" {
		t.Errorf("other track status = %q, want empty", tr1.Status)
	}
	// The covered track's wanted row is satisfied.
	w, err := repo.GetWanted(context.Background(), albumID, t2)
	if err != nil {
		t.Fatal(err)
	}
	if w.Status != dom.WantedSatisfied {
		t.Errorf("wanted status = %q, want satisfied", w.Status)
	}
	// A manual-import event is recorded in the artist history.
	hist, err := repo.ListHistory(context.Background(), artistID, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, h := range hist {
		if h.Event == "manual-import" {
			found = true
			if !strings.Contains(h.Detail, "covering 1 track(s)") {
				t.Errorf("detail = %q, want the 1-track coverage", h.Detail)
			}
		}
	}
	if !found {
		t.Errorf("no manual-import history event: %+v", hist)
	}
}

func TestManualImport_WholeAlbumFile(t *testing.T) {
	mediaRoot := t.TempDir()
	svc, repo, _, albumID, t1, t2 := importFixture(t, mediaRoot)
	ctx := context.Background()
	if err := repo.EnsureWantedAlbum(ctx, albumID); err != nil {
		t.Fatal(err)
	}
	src := writeAudioFile(t, mediaRoot, "downloads/Legend.mp3", "MP3")

	rep, err := svc.ManualImport(ctx, wdom.ManualImport{Kind: "music", AlbumID: albumID, FilePath: src})
	if err != nil {
		t.Fatalf("ManualImport: %v", err)
	}
	if !rep.Imported {
		t.Fatalf("report = %+v", rep)
	}
	// Whole-album file at the documented layout.
	dest := filepath.Join(mediaRoot, "Bob Marley", "Legend", "Legend.mp3")
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("placed file missing: %v", err)
	}
	// Both tracks are marked imported; the whole-album wanted is satisfied.
	for _, n := range []int{1, 2} {
		tr, _ := repo.GetTrack(ctx, albumID, 1, n)
		if tr.Status != "MP3" {
			t.Errorf("track %d status = %q, want MP3", n, tr.Status)
		}
	}
	w, err := repo.GetWanted(ctx, albumID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if w.Status != dom.WantedSatisfied {
		t.Errorf("album wanted status = %q, want satisfied", w.Status)
	}
	_ = t1
	_ = t2
}

func TestManualImport_Directory(t *testing.T) {
	mediaRoot := t.TempDir()
	svc, repo, _, albumID, t1, t2 := importFixture(t, mediaRoot)
	ctx := context.Background()
	if err := repo.EnsureWantedAlbum(ctx, albumID); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(mediaRoot, "downloads/album")
	writeAudioFile(t, dir, "01-01 No Woman, No Man.flac", "FLAC")
	writeAudioFile(t, dir, "01-02 Three Little Birds.mp3", "MP3")
	// An audio file that matches no known track: ignored (counted).
	writeAudioFile(t, dir, "99-99 Extra.mp3", "MP3")
	// A non-audio file: left alone entirely.
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	rep, err := svc.ManualImport(ctx, wdom.ManualImport{Kind: "music", AlbumID: albumID, FilePath: dir})
	if err != nil {
		t.Fatalf("ManualImport: %v", err)
	}
	if !rep.Imported {
		t.Fatalf("report = %+v", rep)
	}
	if !strings.Contains(rep.Message, "1 unmatched file(s) ignored") {
		t.Errorf("message = %q, want the unmatched count", rep.Message)
	}
	tr1, _ := repo.GetTrack(ctx, albumID, 1, 1)
	tr2, _ := repo.GetTrack(ctx, albumID, 1, 2)
	if tr1.Status != "FLAC" {
		t.Errorf("track 1 status = %q, want FLAC", tr1.Status)
	}
	if tr2.Status != "MP3" {
		t.Errorf("track 2 status = %q, want MP3", tr2.Status)
	}
	// Whole-album coverage: the album wanted row is satisfied.
	w, _ := repo.GetWanted(ctx, albumID, 0)
	if w.Status != dom.WantedSatisfied {
		t.Errorf("album wanted status = %q, want satisfied", w.Status)
	}
	_ = t1
	_ = t2
}

func TestManualImport_Errors(t *testing.T) {
	mediaRoot := t.TempDir()
	svc, repo, _, albumID, _, _ := importFixture(t, mediaRoot)
	ctx := context.Background()

	// Missing album id.
	if _, err := svc.ManualImport(ctx, wdom.ManualImport{Kind: "music", FilePath: "x.flac"}); !errors.Is(err, dom.ErrAlbumNotFound) {
		t.Errorf("no album id: err = %v, want ErrAlbumNotFound", err)
	}
	// Unknown album.
	if _, err := svc.ManualImport(ctx, wdom.ManualImport{Kind: "music", AlbumID: 99, FilePath: "x.flac"}); !errors.Is(err, dom.ErrAlbumNotFound) {
		t.Errorf("unknown album: err = %v, want ErrAlbumNotFound", err)
	}
	// Empty file path.
	if _, err := svc.ManualImport(ctx, wdom.ManualImport{Kind: "music", AlbumID: albumID}); !errors.Is(err, wdom.ErrEmptyPath) {
		t.Errorf("empty path: err = %v, want ErrEmptyPath", err)
	}
	// A path outside the media root is rejected.
	outside := t.TempDir()
	if _, err := svc.ManualImport(ctx, wdom.ManualImport{Kind: "music", AlbumID: albumID, FilePath: filepath.Join(outside, "x.flac")}); !errors.Is(err, wdom.ErrPathOutsideRoot) {
		t.Errorf("outside root: err = %v, want ErrPathOutsideRoot", err)
	}
	// A missing file.
	if _, err := svc.ManualImport(ctx, wdom.ManualImport{Kind: "music", AlbumID: albumID, FilePath: filepath.Join(mediaRoot, "nope.flac")}); !errors.Is(err, wdom.ErrFileNotFound) {
		t.Errorf("missing file: err = %v, want ErrFileNotFound", err)
	}
	// An unsupported file type (phase 1: audio files / directories only).
	txt := filepath.Join(mediaRoot, "notes.txt")
	if err := os.WriteFile(txt, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ManualImport(ctx, wdom.ManualImport{Kind: "music", AlbumID: albumID, FilePath: txt}); !errors.Is(err, dom.ErrUnsupportedFile) {
		t.Errorf("unsupported type: err = %v, want ErrUnsupportedFile", err)
	}

	// A directory with no recognizable track file covers nothing.
	emptyDir := filepath.Join(mediaRoot, "empty")
	if err := os.MkdirAll(emptyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ManualImport(ctx, wdom.ManualImport{Kind: "music", AlbumID: albumID, FilePath: emptyDir}); !errors.Is(err, dom.ErrUnsupportedFile) {
		t.Errorf("empty dir: err = %v, want ErrUnsupportedFile (no coverage)", err)
	}
	_ = repo
}
