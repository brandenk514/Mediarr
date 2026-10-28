package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/brandenk514/mediarr/internal/auth"
	musicdom "github.com/brandenk514/mediarr/internal/domains/music"
	wdom "github.com/brandenk514/mediarr/internal/domains/wanted"
	"github.com/brandenk514/mediarr/internal/health"
	musicsvc "github.com/brandenk514/mediarr/internal/services/music"
	wsvc "github.com/brandenk514/mediarr/internal/services/wanted"
)

// importKey identifies a wanted row (album-level when track is 0).
type importKey struct {
	album int64
	track int64
}

// importMusicRepo is an in-memory music.Repo for the kind=music contract
// tests on POST /api/v1/import: it overrides the methods the import flow
// touches; every other method panics through the embedded stub if a handler
// is ever accidentally invoked.
type importMusicRepo struct {
	stubMusicRepo
	artists map[int64]musicdom.Artist
	albums  map[int64]musicdom.Album
	tracks  map[int64][]musicdom.Track
	wanted  map[importKey]musicdom.Wanted
	history int
}

func (r *importMusicRepo) GetArtist(_ context.Context, id int64) (*musicdom.Artist, error) {
	a, ok := r.artists[id]
	if !ok {
		return nil, musicdom.ErrArtistNotFound
	}
	return &a, nil
}
func (r *importMusicRepo) GetAlbum(_ context.Context, id int64) (*musicdom.Album, error) {
	a, ok := r.albums[id]
	if !ok {
		return nil, musicdom.ErrAlbumNotFound
	}
	return &a, nil
}
func (r *importMusicRepo) ListTracks(_ context.Context, albumID int64) ([]musicdom.Track, error) {
	return r.tracks[albumID], nil
}
func (r *importMusicRepo) SetTrackStatus(_ context.Context, albumID int64, disc, number int, status string) (bool, error) {
	for i, t := range r.tracks[albumID] {
		if t.Disc == disc && t.Number == number {
			t.Status = status
			r.tracks[albumID][i] = t
			return true, nil
		}
	}
	return false, nil
}
func (r *importMusicRepo) GetWanted(_ context.Context, albumID, trackID int64) (*musicdom.Wanted, error) {
	w, ok := r.wanted[importKey{albumID, trackID}]
	if !ok {
		return nil, musicdom.ErrAlbumNotFound
	}
	return &w, nil
}
func (r *importMusicRepo) MarkWantedSatisfied(_ context.Context, albumID, trackID int64, release string) error {
	k := importKey{albumID, trackID}
	w, ok := r.wanted[k]
	if !ok {
		return musicdom.ErrAlbumNotFound
	}
	w.Status = musicdom.WantedSatisfied
	w.ReleaseTitle = release
	r.wanted[k] = w
	return nil
}
func (r *importMusicRepo) AddHistory(_ context.Context, _ musicdom.HistoryEntry) error {
	r.history++
	return nil
}

// newImportTestServer wires a server whose /api/v1/import serves both kinds:
// the TV flow on the shared memWantedRepo, the music flow on the seeded
// importMusicRepo under mediaRoot.
func newImportTestServer(t *testing.T, mediaRoot string) (*Server, *importMusicRepo, string) {
	t.Helper()
	music := &importMusicRepo{
		artists: map[int64]musicdom.Artist{1: {ID: 1, Name: "Bob Marley", Monitored: true}},
		albums:  map[int64]musicdom.Album{1: {ID: 1, ArtistID: 1, Name: "Legend", Monitored: true}},
		tracks: map[int64][]musicdom.Track{
			1: {
				{ID: 10, AlbumID: 1, Disc: 1, Number: 1, Title: "No Woman, No Man"},
				{ID: 11, AlbumID: 1, Disc: 1, Number: 2, Title: "Three Little Birds"},
			},
		},
		wanted: map[importKey]musicdom.Wanted{
			{album: 1, track: 11}: {AlbumID: 1, TrackID: 11, Status: musicdom.WantedPending},
		},
	}
	store := newFakeStore()
	u, _ := store.CreateUser(context.Background(), "admin", "", "admin", "hash")
	pair, err := auth.NewToken("ma_live_")
	if err != nil {
		t.Fatalf("new token: %v", err)
	}
	_, _ = store.CreateToken(context.Background(), u, "test", pair, nil)

	s := NewServer(health.NewChecker(nil, nil))
	s.SetAuth(&AuthDeps{Repo: store})
	s.SetImport(&ImportDeps{
		Wanted: wsvc.New(&memWantedRepo{}, os.TempDir()),
		Music:  musicsvc.New(musicsvc.Deps{Repo: music, MediaRoot: mediaRoot}),
	})
	return s, music, pair.Raw
}

func TestImport_Music_SingleTrack(t *testing.T) {
	mediaRoot := t.TempDir()
	s, music, token := newImportTestServer(t, mediaRoot)

	src := filepath.Join(mediaRoot, "downloads/01-02 Three Little Birds.flac")
	if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}

	body := `{"kind":"music","album_id":1,"file_path":"` + src + `"}`
	rec := doJSONReq(t, s.Handler(), http.MethodPost, "/api/v1/import", token, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d; body %s", rec.Code, rec.Body.String())
	}
	var rep wdom.ManualImportReport
	if err := json.Unmarshal(rec.Body.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}
	if !rep.Imported {
		t.Fatalf("report = %+v, want imported", rep)
	}
	// The track is marked imported with the detected codec and its wanted
	// row is satisfied.
	if music.tracks[1][1].Status != "FLAC" {
		t.Errorf("track status = %q, want FLAC", music.tracks[1][1].Status)
	}
	if music.tracks[1][0].Status != "" {
		t.Errorf("uncovered track status = %q, want empty", music.tracks[1][0].Status)
	}
	if w := music.wanted[importKey{1, 11}]; w.Status != musicdom.WantedSatisfied {
		t.Errorf("wanted status = %q, want satisfied", w.Status)
	}
	// The file is placed under the album folder.
	dest := filepath.Join(mediaRoot, "Bob Marley", "Legend", "01-02 Three Little Birds.flac")
	if _, err := os.Stat(dest); err != nil {
		t.Errorf("placed file missing: %v", err)
	}
}

func TestImport_Music_Validation(t *testing.T) {
	mediaRoot := t.TempDir()
	s, _, token := newImportTestServer(t, mediaRoot)

	src := filepath.Join(mediaRoot, "01-01 No Woman, No Man.flac")
	if err := os.WriteFile(src, []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}

	// kind=music without an album id -> 400.
	rec := doJSONReq(t, s.Handler(), http.MethodPost, "/api/v1/import", token,
		`{"kind":"music","file_path":"`+src+`"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("no album_id: got %d, want 400", rec.Code)
	}
	// Unknown album -> 404.
	rec = doJSONReq(t, s.Handler(), http.MethodPost, "/api/v1/import", token,
		`{"kind":"music","album_id":99,"file_path":"`+src+`"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown album: got %d, want 404", rec.Code)
	}
	// Empty file path -> 400.
	rec = doJSONReq(t, s.Handler(), http.MethodPost, "/api/v1/import", token,
		`{"kind":"music","album_id":1}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty path: got %d, want 400", rec.Code)
	}
	// A path outside the media root -> 400.
	outside := t.TempDir()
	rec = doJSONReq(t, s.Handler(), http.MethodPost, "/api/v1/import", token,
		`{"kind":"music","album_id":1,"file_path":"`+filepath.Join(outside, "x.flac")+`"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("outside root: got %d, want 400", rec.Code)
	}
	// A missing file -> 400.
	rec = doJSONReq(t, s.Handler(), http.MethodPost, "/api/v1/import", token,
		`{"kind":"music","album_id":1,"file_path":"`+filepath.Join(mediaRoot, "nope.flac")+`"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing file: got %d, want 400", rec.Code)
	}
	// An unsupported file type -> 400 (phase 1: audio files / directories).
	txt := filepath.Join(mediaRoot, "notes.txt")
	if err := os.WriteFile(txt, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec = doJSONReq(t, s.Handler(), http.MethodPost, "/api/v1/import", token,
		`{"kind":"music","album_id":1,"file_path":"`+txt+`"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unsupported type: got %d, want 400", rec.Code)
	}
	// An unknown kind -> 400.
	rec = doJSONReq(t, s.Handler(), http.MethodPost, "/api/v1/import", token,
		`{"kind":"book","album_id":1,"file_path":"`+src+`"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown kind: got %d, want 400", rec.Code)
	}
}
