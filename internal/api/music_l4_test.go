package api

// L4 (Lidarr parity) endpoint coverage: add-album enrichment via
// release_group_mb_id (201 {id, warnings}), the artist refresh endpoint
// (POST /api/v1/music/artists/{id}/refresh), and the warning paths — driven
// through the kinds server with the keyless metadata adapter pointed at an
// httptest stub (each test builds its own server: the mux panics on
// double-registered routes on a shared one).

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/brandenk514/mediarr/internal/providers"
)

// newL4APIMBServer serves the group → release → CAA chain + the artist
// detail, mirroring the provider tests' fixtures.
func newL4APIMBServer(t *testing.T) *providers.MusicBrainz {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/release-group/group-uuid":
			_, _ = w.Write([]byte(`{
  "id": "group-uuid", "title": "OK Computer", "primary-type": "Album",
  "releases": [
    {"id": "rel-official", "title": "OK Computer", "status": "Official", "date": "1997-06-16"}
  ]}`))
		case "/release/rel-official":
			_, _ = w.Write([]byte(`{
  "id": "rel-official", "title": "OK Computer", "status": "Official", "date": "1997-06-16",
  "media": [{"position": 1, "tracks": [
    {"id": "tr-1", "position": 1, "title": "Airbag", "length": 237120},
    {"id": "tr-2", "position": 2, "title": "Lust", "length": 243750}
  ]}],
  "labels": [{"name": "Parlophone"}],
  "genres": [{"name": "Alternative Rock"}]}`))
		case "/release/rel-official.json": // CAA front cover
			_, _ = w.Write([]byte(`{"images": [
				{"url": "https://cover/500.jpg", "sizes": {"front": 500}, "formats": {"front": ["jpg"]}}
			]}`))
		case "/artist/artist-uuid":
			_, _ = w.Write([]byte(`{
  "id": "artist-uuid", "name": "Radiohead", "status": "continuing",
  "genres": [{"name": "Alternative Rock"}, {"name": "Art Rock"}],
  "relations": [{"target-type": "url", "type": "last.fm", "url": {"resource": "https://www.last.fm/music/Radiohead"}}]
}`))
		case "/artist/artist-uuid.json": // CAA: no artist cover
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	adapter := &providers.MusicBrainz{}
	adapter.BaseURL = srv.URL
	adapter.CAABaseURL = srv.URL
	return adapter
}

func TestL4_API_AddAlbum_Enriched(t *testing.T) {
	music, books := newKindsMusicRepo(), newKindsBooksRepo()
	f := newKindsServer(t, music, books, newL4APIMBServer(t))
	h := f.s.Handler()

	rec := f.post(t, "/api/v1/music/artists", map[string]any{"name": "Radiohead", "monitored": true})
	if rec.Code != http.StatusCreated {
		t.Fatalf("add artist status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = f.post(t, "/api/v1/music/artists/1/albums", map[string]any{
		"name": "OK Computer", "quality_profile": "FLAC", "release_group_mb_id": "group-uuid",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("add album status = %d body=%s", rec.Code, rec.Body.String())
	}
	var addResp struct {
		ID       int64    `json:"id"`
		Warnings []string `json:"warnings"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&addResp); err != nil {
		t.Fatalf("decode 201 body: %v body=%s", err, rec.Body.String())
	}
	if addResp.ID == 0 {
		t.Fatalf("201 body missing id: %s", rec.Body.String())
	}
	if len(addResp.Warnings) != 0 {
		t.Fatalf("warnings = %v, want [] on successful enrichment", addResp.Warnings)
	}

	rec = getJSON(t, h, "/api/v1/music/albums/"+strconv.FormatInt(addResp.ID, 10), f.token)
	if rec.Code != http.StatusOK {
		t.Fatalf("get album status = %d body=%s", rec.Code, rec.Body.String())
	}
	var album struct {
		MBID        string   `json:"MBID"`
		Year        int      `json:"Year"`
		ReleaseDate string   `json:"ReleaseDate"`
		Duration    int      `json:"Duration"`
		Cover       string   `json:"Cover"`
		Labels      []string `json:"Labels"`
		Genres      []string `json:"Genres"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&album); err != nil {
		t.Fatalf("decode album: %v", err)
	}
	if album.MBID != "rel-official" || album.Year != 1997 || album.ReleaseDate != "1997-06-16" {
		t.Errorf("album = %+v, want the resolved release rel-official/1997-06-16", album)
	}
	if album.Duration != 237+243 {
		t.Errorf("Duration = %d, want 480 (track sum)", album.Duration)
	}
	if album.Cover != "https://cover/500.jpg" {
		t.Errorf("Cover = %q, want the CAA 500 thumbnail", album.Cover)
	}
	if len(album.Labels) != 1 || album.Labels[0] != "Parlophone" {
		t.Errorf("Labels = %v, want [Parlophone]", album.Labels)
	}
	if len(album.Genres) != 1 || album.Genres[0] != "Alternative Rock" {
		t.Errorf("Genres = %v, want [Alternative Rock]", album.Genres)
	}

	rec = getJSON(t, h, "/api/v1/music/albums/"+strconv.FormatInt(addResp.ID, 10)+"/tracks", f.token)
	if rec.Code != http.StatusOK {
		t.Fatalf("list tracks status = %d", rec.Code)
	}
	var tracks []map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&tracks); err != nil {
		t.Fatalf("decode tracks: %v", err)
	}
	if len(tracks) != 2 {
		t.Errorf("tracks = %v, want 2 (one per release track)", tracks)
	}
}

func TestL4_API_AddAlbum_NotConfigured(t *testing.T) {
	// No metadata adapter: the add still succeeds; the warning says why the
	// enrichment was skipped.
	music, books := newKindsMusicRepo(), newKindsBooksRepo()
	f := newKindsServer(t, music, books)
	h := f.s.Handler()

	rec := f.post(t, "/api/v1/music/artists", map[string]any{"name": "Radiohead"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("add artist status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = f.post(t, "/api/v1/music/artists/1/albums", map[string]any{
		"name": "OK Computer", "release_group_mb_id": "group-uuid",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("add album status = %d body=%s (the add must still succeed)", rec.Code, rec.Body.String())
	}
	var addResp struct {
		ID       int64    `json:"id"`
		Warnings []string `json:"warnings"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&addResp); err != nil {
		t.Fatalf("decode 201 body: %v", err)
	}
	if len(addResp.Warnings) != 1 || addResp.Warnings[0] != "metadata enrichment skipped: metadata provider not configured" {
		t.Fatalf("warnings = %v, want the not-configured warning", addResp.Warnings)
	}
	// And the album exists, un-enriched.
	rec = getJSON(t, h, "/api/v1/music/albums/"+strconv.FormatInt(addResp.ID, 10), f.token)
	if rec.Code != http.StatusOK {
		t.Fatalf("get album status = %d", rec.Code)
	}
}

func TestL4_API_RefreshArtist(t *testing.T) {
	music, books := newKindsMusicRepo(), newKindsBooksRepo()
	f := newKindsServer(t, music, books, newL4APIMBServer(t))

	rec := f.post(t, "/api/v1/music/artists", map[string]any{"name": "Radiohead", "mbid": "artist-uuid", "monitored": true})
	if rec.Code != http.StatusCreated {
		t.Fatalf("add artist status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = f.post(t, "/api/v1/music/artists/1/albums", map[string]any{
		"name": "OK Computer", "monitored": true, "release_group_mb_id": "group-uuid",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("add album status = %d body=%s", rec.Code, rec.Body.String())
	}

	// POST with no body.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/music/artists/1/refresh", nil)
	req.Header.Set("Authorization", "Bearer "+f.token)
	rr := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("refresh status = %d body=%s", rr.Code, rr.Body.String())
	}
	var out struct {
		Artist struct {
			Genre string `json:"Genre"`
			Cover string `json:"Cover"`
			Links []struct {
				Name string `json:"Name"`
				URL  string `json:"URL"`
			} `json:"Links"`
		} `json:"artist"`
		Report struct {
			AlbumsRefreshed int `json:"albums_refreshed"`
			AlbumsFailed    int `json:"albums_failed"`
		} `json:"report"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&out); err != nil {
		t.Fatalf("decode refresh body: %v body=%s", err, rr.Body.String())
	}
	if out.Report.AlbumsRefreshed != 1 || out.Report.AlbumsFailed != 0 {
		t.Errorf("report = %+v, want {1, 0}", out.Report)
	}
	if out.Artist.Genre != "Alternative Rock, Art Rock" {
		t.Errorf("artist.Genre = %q, want the joined genres", out.Artist.Genre)
	}
	if len(out.Artist.Links) != 1 || out.Artist.Links[0].URL != "https://www.last.fm/music/Radiohead" {
		t.Errorf("artist.Links = %+v, want the one last.fm URL", out.Artist.Links)
	}
	if out.Artist.Cover != "" {
		t.Errorf("artist.Cover = %q, want \"\" (CAA 404)", out.Artist.Cover)
	}
}

func TestL4_API_RefreshArtist_Unknown(t *testing.T) {
	music, books := newKindsMusicRepo(), newKindsBooksRepo()
	f := newKindsServer(t, music, books, newL4APIMBServer(t))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/music/artists/999/refresh", nil)
	req.Header.Set("Authorization", "Bearer "+f.token)
	rr := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("refresh unknown artist status = %d, want 404 body=%s", rr.Code, rr.Body.String())
	}
}
