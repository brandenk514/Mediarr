package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brandenk514/mediarr/internal/auth"
	musicdom "github.com/brandenk514/mediarr/internal/domains/music"
	"github.com/brandenk514/mediarr/internal/health"
	musicsvc "github.com/brandenk514/mediarr/internal/services/music"
)

// unmappedRepo is an in-memory music.Repo for the Unmapped Files endpoint
// test. It overrides only ListArtists (the one method the unmapped flow
// touches) with a configurable artist list; every other method panics
// through the embedded stub if a handler ever accidentally invokes it.
type unmappedRepo struct {
	stubMusicRepo
	artists []musicdom.Artist
}

func (r *unmappedRepo) ListArtists(context.Context) ([]musicdom.Artist, error) {
	return r.artists, nil
}

func newUnmappedTestServer(t *testing.T, repo *unmappedRepo, mediaRoot string) (*Server, string) {
	t.Helper()
	s := NewServer(health.NewChecker(nil, nil))
	store := newFakeStore()
	u, _ := store.CreateUser(context.Background(), "admin", "", "admin", "hash")
	pair, err := auth.NewToken("ma_live_")
	if err != nil {
		t.Fatalf("new token: %v", err)
	}
	_, _ = store.CreateToken(context.Background(), u, "test", pair, nil)
	s.SetAuth(&AuthDeps{Repo: store})
	s.SetMusic(&MusicDeps{Svc: musicsvc.New(musicsvc.Deps{Repo: repo, MediaRoot: mediaRoot})})
	return s, pair.Raw
}

// TestMusicUnmappedFiles is the L5 "Unmapped Files" endpoint contract: a
// read-only walk of the media root that lists audio files NOT mapped to a
// monitored artist, with the Lidarr columns (Path/Size/Date Added/Quality).
// Files under a folder named after a monitored artist are mapped (excluded);
// files in other folders, or loose directly under the root, are unmapped.
func TestMusicUnmappedFiles(t *testing.T) {
	root := t.TempDir()
	// Mapped: a folder named after a monitored artist (excluded from the list).
	mappedDir := filepath.Join(root, "Radiohead", "OK Computer")
	if err := os.MkdirAll(mappedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mappedDir, "01-Airbag.flac"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Unmapped: a folder that is NOT a monitored artist.
	unmappedDir := filepath.Join(root, "Unknown Artist", "Misc")
	if err := os.MkdirAll(unmappedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(unmappedDir, "01-Song.mp3"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Unmapped: a loose file directly under the root (no top-level folder).
	if err := os.WriteFile(filepath.Join(root, "loose.flac"), []byte("z"), 0o644); err != nil {
		t.Fatal(err)
	}

	repo := &unmappedRepo{artists: []musicdom.Artist{{ID: 1, Name: "Radiohead", Monitored: true}}}
	s, tok := newUnmappedTestServer(t, repo, root)

	rec := doJSONReq(t, s.Handler(), http.MethodGet, "/api/v1/music/unmapped", tok, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET unmapped: got %d, want 200: %s", rec.Code, rec.Body.String())
	}

	var files []struct {
		Path      string `json:"path"`
		Size      int64  `json:"size"`
		DateAdded string `json:"date_added"`
		Quality   string `json:"quality"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &files); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("want 2 unmapped files, got %d: %+v", len(files), files)
	}

	byBase := map[string]string{}
	for _, f := range files {
		if strings.Contains(f.Path, "Radiohead") {
			t.Errorf("mapped file (under monitored artist) leaked into unmapped list: %s", f.Path)
		}
		byBase[filepath.Base(f.Path)] = f.Quality
		if f.Size == 0 {
			t.Errorf("file %s: size should be non-zero", f.Path)
		}
	}
	if q, ok := byBase["01-Song.mp3"]; !ok || q != "MP3" {
		t.Errorf("unmapped mp3: want quality MP3, got %q (present=%v)", q, ok)
	}
	if q, ok := byBase["loose.flac"]; !ok || q != "FLAC" {
		t.Errorf("unmapped loose flac: want quality FLAC, got %q (present=%v)", q, ok)
	}
}

// TestMusicUnmappedFiles_RequiresAuth is the auth contract: the unmapped
// endpoint is 401 without a bearer token.
func TestMusicUnmappedFiles_RequiresAuth(t *testing.T) {
	repo := &unmappedRepo{artists: nil}
	s, _ := newUnmappedTestServer(t, repo, t.TempDir())
	rec := doJSONReq(t, s.Handler(), http.MethodGet, "/api/v1/music/unmapped", "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("GET unmapped without token: got %d, want 401", rec.Code)
	}
}
