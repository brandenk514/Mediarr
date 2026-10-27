package api

// L1 (Lidarr parity phase) endpoint coverage: the new artist-history and
// library-summary endpoints, and the optional add-artist fields, driven
// through the fully-wired kinds server (each test builds its own server —
// the mux panics on double-registered routes on a shared one).

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	musicdom "github.com/brandenk514/mediarr/internal/domains/music"
)

// TestL1_ArtistHistoryEndpoint drives GET /api/v1/music/artists/{id}/history:
// 200 with an (empty) array for a known artist, 404 for an unknown one.
// (The kinds repo's ListHistory is a no-op, so the response shape — an array,
// never null — is what the endpoint contract pins here; the entry content is
// covered by the service and postgres tests.)
func TestL1_ArtistHistoryEndpoint(t *testing.T) {
	music, books := newKindsMusicRepo(), newKindsBooksRepo()
	f := newKindsServer(t, music, books)
	h := f.s.Handler()

	rec := f.post(t, "/api/v1/music/artists", map[string]any{"name": "Radiohead"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("add artist status = %d body=%s", rec.Code, rec.Body.String())
	}

	rec = getJSON(t, h, "/api/v1/music/artists/1/history", f.token)
	if rec.Code != http.StatusOK {
		t.Fatalf("history status = %d body=%s", rec.Code, rec.Body.String())
	}
	if body := strings.TrimSpace(rec.Body.String()); body != "[]" {
		t.Fatalf("history body = %q, want [] (an array, never null)", body)
	}

	rec = getJSON(t, h, "/api/v1/music/artists/999/history", f.token)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing artist history status = %d, want 404", rec.Code)
	}
}

// TestL1_LibrarySummaryEndpoint drives GET /api/v1/music/library/summary
// against a seeded library: 3 artists (2 continuing, 1 inactive; 2
// monitored) and 3 tracks (2 with imported files totaling 300 bytes).
func TestL1_LibrarySummaryEndpoint(t *testing.T) {
	music, books := newKindsMusicRepo(), newKindsBooksRepo()
	f := newKindsServer(t, music, books)
	h := f.s.Handler()

	music.artists[1] = musicdom.Artist{ID: 1, Name: "Bob Marley", Monitored: true, Status: "continuing"}
	music.artists[2] = musicdom.Artist{ID: 2, Name: "Queen", Status: "continuing"}
	music.artists[3] = musicdom.Artist{ID: 3, Name: "Def Leppard", Monitored: true, Status: "inactive"}
	music.albums[10] = musicdom.Album{ID: 10, ArtistID: 1, Name: "Legend", Year: 1977}
	music.tracks[10] = []musicdom.Track{
		{ID: 100, AlbumID: 10, Disc: 1, Number: 1, Title: "A", Status: "FLAC", Size: 100},
		{ID: 101, AlbumID: 10, Disc: 1, Number: 2, Title: "B", Status: "MP3", Size: 200},
		{ID: 102, AlbumID: 10, Disc: 1, Number: 3, Title: "C"},
	}

	rec := getJSON(t, h, "/api/v1/music/library/summary", f.token)
	if rec.Code != http.StatusOK {
		t.Fatalf("summary status = %d body=%s", rec.Code, rec.Body.String())
	}
	var s musicdom.LibrarySummary
	if err := json.NewDecoder(rec.Body).Decode(&s); err != nil {
		t.Fatal(err)
	}
	if s.Artists != 3 || s.Continuing != 2 || s.Inactive != 1 ||
		s.Monitored != 2 || s.Unmonitored != 1 {
		t.Errorf("artist counts = %+v", s)
	}
	if s.Tracks != 3 || s.Files != 2 || s.TotalFileSize != 300 {
		t.Errorf("track/file counts = %+v, want 3/2/300 (files and size only over imported tracks)", s)
	}
}

// TestL1_AddArtistFields drives the optional quality_profile/path on the
// add-artist request through to the stored artist (the L2 add-modal inputs).
func TestL1_AddArtistFields(t *testing.T) {
	music, books := newKindsMusicRepo(), newKindsBooksRepo()
	f := newKindsServer(t, music, books)
	h := f.s.Handler()

	rec := f.post(t, "/api/v1/music/artists", map[string]any{
		"name": "Radiohead", "quality_profile": "FLAC", "path": "Radiohead (1992)",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("add artist status = %d body=%s", rec.Code, rec.Body.String())
	}

	rec = getJSON(t, h, "/api/v1/music/artists/1", f.token)
	if rec.Code != http.StatusOK {
		t.Fatalf("get artist status = %d body=%s", rec.Code, rec.Body.String())
	}
	var a musicdom.Artist
	if err := json.NewDecoder(rec.Body).Decode(&a); err != nil {
		t.Fatal(err)
	}
	if a.QualityProfile != "FLAC" || a.Path != "Radiohead (1992)" || a.Status != "continuing" {
		t.Errorf("artist = %+v, want profile FLAC, path Radiohead (1992), status continuing", a)
	}
}

// TestL1_ArtistCountsWire pins the L1 spec-gap fields on the wire: a fresh
// artist serializes TrackCount/FileCount/QueuedCount/TotalSize (the kinds
// repo doesn't compute them, so they read 0 here; the computed values are
// covered by the postgres integration test).
func TestL1_ArtistCountsWire(t *testing.T) {
	music, books := newKindsMusicRepo(), newKindsBooksRepo()
	f := newKindsServer(t, music, books)
	h := f.s.Handler()

	rec := f.post(t, "/api/v1/music/artists", map[string]any{"name": "Radiohead"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("add artist status = %d body=%s", rec.Code, rec.Body.String())
	}

	rec = getJSON(t, h, "/api/v1/music/artists/1", f.token)
	if rec.Code != http.StatusOK {
		t.Fatalf("get artist status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, key := range []string{`"TrackCount":`, `"FileCount":`, `"QueuedCount":`, `"TotalSize":`} {
		if !strings.Contains(body, key) {
			t.Errorf("artist JSON missing %s: %s", key, body)
		}
	}
}
