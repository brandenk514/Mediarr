package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/brandenk514/mediarr/internal/auth"
	wdom "github.com/brandenk514/mediarr/internal/domains/wanted"
	"github.com/brandenk514/mediarr/internal/health"
	wsvc "github.com/brandenk514/mediarr/internal/services/wanted"
)

// newWantedMusicTestServer builds a fresh server (the mux panics on double
// registration, so one server per endpoint group) whose /api/v1/wanted
// serves kind=music over the seeded memWantedRepo music rows.
func newWantedMusicTestServer(t *testing.T) (*Server, *memWantedRepo, string) {
	t.Helper()
	rel := time.Date(1977, 6, 13, 0, 0, 0, 0, time.UTC)
	repo := &memWantedRepo{
		musicRows: []wdom.MusicRow{
			// Monitored, file-less: a missing album row (default cutoff
			// applies, but file-less albums are missing, not cutoff).
			{ArtistID: 1, ArtistTitle: "Bob Marley", AlbumID: 10, AlbumTitle: "Legend", AlbumType: "album", ReleaseDate: &rel, Monitored: true, TrackCount: 2},
			// Monitored, imported in FLAC, no profile: the default cutoff
			// (FLAC 24bit) is unmet.
			{ArtistID: 1, ArtistTitle: "Bob Marley", AlbumID: 11, AlbumTitle: "Uprising", Monitored: true, FileCount: 1, ImportedQualities: []string{"FLAC"}},
			// Unmonitored: absent from every tab.
			{ArtistID: 2, ArtistTitle: "Jimi Hendrix", AlbumID: 12, AlbumTitle: "Axis", Monitored: false, TrackCount: 2},
			// Monitored file-less track: a missing track row.
			{ArtistID: 1, ArtistTitle: "Bob Marley", AlbumID: 10, AlbumTitle: "Legend", Monitored: true, Disc: 1, Number: 1, TrackID: 101, TrackTitle: "No Woman, No Man", TrackMonitored: true},
		},
		musicMonitored: map[string]bool{
			"10/0/0": true,
			"10/1/1": true,
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
	s.SetWanted(&WantedDeps{Svc: wsvc.New(repo, os.TempDir())})
	return s, repo, pair.Raw
}

func TestWantedMusic_ListMissing(t *testing.T) {
	s, _, token := newWantedMusicTestServer(t)
	rec := doReq(t, s.Handler(), http.MethodGet, "/api/v1/wanted?kind=music&tab=missing", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d; body %s", rec.Code, rec.Body.String())
	}
	var env struct {
		Items []wdom.Item `json:"items"`
		Total int         `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// Legend (file-less album) + its file-less track: 2 rows.
	if env.Total != 2 || len(env.Items) != 2 {
		t.Fatalf("total/len = %d/%d, want 2/2; %+v", env.Total, len(env.Items), env.Items)
	}
	it := env.Items[0]
	if it.Kind != "music" || it.AlbumTitle != "Legend" || it.ArtistTitle != "Bob Marley" ||
		it.AlbumType != "album" || it.ReleaseDate == nil {
		t.Errorf("album row = %+v, want kind=music with the enriched columns", it)
	}
	if it.Status != "missing:no file" {
		t.Errorf("status = %q", it.Status)
	}
	// The track row carries the track in the episode-shaped fields.
	tr := env.Items[1]
	if tr.Season != 1 || tr.Episode != 1 || tr.EpisodeTitle != "No Woman, No Man" {
		t.Errorf("track row = %+v", tr)
	}
}

func TestWantedMusic_ListCutoff(t *testing.T) {
	s, _, token := newWantedMusicTestServer(t)
	rec := doReq(t, s.Handler(), http.MethodGet, "/api/v1/wanted?kind=music&tab=cutoff", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d; body %s", rec.Code, rec.Body.String())
	}
	var env struct {
		Items []wdom.Item `json:"items"`
		Total int         `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// Only Uprising (FLAC below the default FLAC 24bit cutoff).
	if env.Total != 1 || len(env.Items) != 1 {
		t.Fatalf("cutoff total/len = %d/%d, want 1/1; %+v", env.Total, len(env.Items), env.Items)
	}
	it := env.Items[0]
	if it.AlbumTitle != "Uprising" || it.Quality != "FLAC" {
		t.Errorf("cutoff row = %+v", it)
	}
}

func TestWantedMusic_SearchAll(t *testing.T) {
	s, repo, token := newWantedMusicTestServer(t)
	rec := doJSONReq(t, s.Handler(), http.MethodPost, "/api/v1/wanted/search", token,
		`{"tab":"missing","kind":"music"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d; body %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Searchd int `json:"searched"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	// Legend (whole album) + the track row.
	if out.Searchd != 2 {
		t.Fatalf("searched = %d, want 2", out.Searchd)
	}
	if len(repo.musicWanted) != 2 {
		t.Fatalf("wanted rows ensured = %d, want 2: %+v", len(repo.musicWanted), repo.musicWanted)
	}
	// An unknown kind is a 400.
	rec = doJSONReq(t, s.Handler(), http.MethodPost, "/api/v1/wanted/search", token,
		`{"tab":"missing","kind":"books"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad kind: got %d, want 400", rec.Code)
	}
}

func TestWantedMusic_Unmonitor(t *testing.T) {
	s, repo, token := newWantedMusicTestServer(t)
	body := `{"kind":"music","music":[{"album_id":10},{"album_id":10,"disc":1,"number":1},{"album_id":99}]}`
	rec := doJSONReq(t, s.Handler(), http.MethodPost, "/api/v1/wanted/unmonitor", token, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d; body %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Unmonitored int `json:"unmonitored"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	// The two known refs flip; the unknown one is skipped.
	if out.Unmonitored != 2 {
		t.Fatalf("unmonitored = %d, want 2", out.Unmonitored)
	}
	if repo.musicMonitored["10/0/0"] || repo.musicMonitored["10/1/1"] {
		t.Fatalf("flags not flipped: %+v", repo.musicMonitored)
	}
	// An unknown kind is a 400.
	rec = doJSONReq(t, s.Handler(), http.MethodPost, "/api/v1/wanted/unmonitor", token,
		`{"kind":"books","music":[{"album_id":10}]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad kind: got %d, want 400", rec.Code)
	}
}
