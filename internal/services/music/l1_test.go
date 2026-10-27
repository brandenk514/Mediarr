package music

// L1 (Lidarr parity phase) service behavior: the add-artist L1 defaults, the
// artist history pass-through (API 404 source), and the library summary.

import (
	"context"
	"errors"
	"testing"

	dom "github.com/brandenk514/mediarr/internal/domains/music"
)

// TestAddArtist_L1Defaults pins the L1 defaults the API contract relies on:
// omitted quality_profile/path yield "continuing" status, the same-named
// subfolder path, and no profile; rating/genre/links stay empty (the
// MusicBrainz source carries none of them).
func TestAddArtist_L1Defaults(t *testing.T) {
	svc, repo := setupService(t)
	ctx := context.Background()

	id, err := svc.AddArtist(ctx, AddArtistInput{Name: "Bob Marley", Monitored: true})
	if err != nil {
		t.Fatalf("add artist: %v", err)
	}
	a, err := repo.GetArtist(ctx, id)
	if err != nil {
		t.Fatalf("get artist: %v", err)
	}
	if a.Status != dom.StatusContinuing {
		t.Errorf("status = %q, want %q (no metadata source supplies release status)", a.Status, dom.StatusContinuing)
	}
	if a.Path != "Bob Marley" {
		t.Errorf("path = %q, want the artist name (same-named subfolder)", a.Path)
	}
	if a.QualityProfile != "" {
		t.Errorf("quality_profile = %q, want empty (none)", a.QualityProfile)
	}
	if a.Rating != 0 || a.Genre != "" || len(a.Links) != 0 {
		t.Errorf("rating/genre/links = %v/%q/%d, want empty", a.Rating, a.Genre, len(a.Links))
	}

	// An explicit per-artist profile and path are stored as given.
	id2, err := svc.AddArtist(ctx, AddArtistInput{
		Name: "Radiohead", Monitored: true, QualityProfile: "FLAC", Path: "Radiohead (1992)",
	})
	if err != nil {
		t.Fatalf("add artist 2: %v", err)
	}
	b, err := repo.GetArtist(ctx, id2)
	if err != nil {
		t.Fatalf("get artist 2: %v", err)
	}
	if b.QualityProfile != "FLAC" || b.Path != "Radiohead (1992)" {
		t.Errorf("profile/path = %q/%q, want FLAC/Radiohead (1992)", b.QualityProfile, b.Path)
	}
}

// TestGetArtistHistory covers the L1 artist-history endpoint's service path:
// a missing artist is ErrArtistNotFound (the handler maps it to 404), and a
// known artist's history comes back newest first.
func TestGetArtistHistory(t *testing.T) {
	svc, _ := setupService(t)
	ctx := context.Background()

	if _, err := svc.GetArtistHistory(ctx, 999, 100); !errors.Is(err, ErrArtistNotFound) {
		t.Errorf("missing artist err = %v, want ErrArtistNotFound", err)
	}

	id, err := svc.AddArtist(ctx, AddArtistInput{Name: "Bob Marley", Monitored: true})
	if err != nil {
		t.Fatalf("add artist: %v", err)
	}
	h, err := svc.GetArtistHistory(ctx, id, 100)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(h) != 1 || h[0].Event != "added" {
		t.Errorf("history = %+v, want the single added entry", h)
	}
}

// TestLibrarySummary drives the footer counts through the service: two
// artists (one flipped to inactive directly in the repo, as the L2 edit flow
// will), one album, two tracks — one of them simulated as file-imported (the
// file tracker is what sets Status/Size; it is out of L1 scope).
func TestLibrarySummary(t *testing.T) {
	svc, repo := setupService(t)
	ctx := context.Background()

	a1, err := svc.AddArtist(ctx, AddArtistInput{Name: "Bob Marley", Monitored: true})
	if err != nil {
		t.Fatalf("add artist: %v", err)
	}
	a2, err := svc.AddArtist(ctx, AddArtistInput{Name: "Def Leppard", Monitored: true})
	if err != nil {
		t.Fatalf("add artist 2: %v", err)
	}
	if err := repo.SetArtistMonitored(ctx, a2, false); err != nil {
		t.Fatalf("unmonitor artist 2: %v", err)
	}
	// Flip one artist to inactive (the edit path that does this lands in L2).
	other := repo.artists[a1]
	other.Status = dom.StatusInactive
	repo.artists[a1] = other

	albumID, err := svc.AddAlbum(ctx, a1, "Legend", 1977, "Lossless", boolPtr(false))
	if err != nil {
		t.Fatalf("add album: %v", err)
	}
	if alb, err := repo.GetAlbum(ctx, albumID); err != nil {
		t.Fatalf("get album: %v", err)
	} else if alb.AlbumType != "album" {
		t.Errorf("album type = %q, want album (the service normalizes the empty default)", alb.AlbumType)
	}
	if _, err := svc.AddTrack(ctx, albumID, 1, 1, "No Woman, No Man", false); err != nil {
		t.Fatalf("add track 1: %v", err)
	}
	if _, err := svc.AddTrack(ctx, albumID, 1, 2, "Waiting in Vain", false); err != nil {
		t.Fatalf("add track 2: %v", err)
	}
	// Simulate the file tracker marking track 1 as imported.
	tracks := repo.tracks[albumID]
	tracks[0].Status = "FLAC"
	tracks[0].Size = 500
	repo.tracks[albumID] = tracks

	s, err := svc.LibrarySummary(ctx)
	if err != nil {
		t.Fatalf("library summary: %v", err)
	}
	if s.Artists != 2 {
		t.Errorf("artists = %d, want 2", s.Artists)
	}
	if s.Continuing != 1 || s.Inactive != 1 {
		t.Errorf("continuing/inactive = %d/%d, want 1/1", s.Continuing, s.Inactive)
	}
	if s.Monitored != 1 || s.Unmonitored != 1 {
		t.Errorf("monitored/unmonitored = %d/%d, want 1/1", s.Monitored, s.Unmonitored)
	}
	if s.Tracks != 2 {
		t.Errorf("tracks = %d, want 2", s.Tracks)
	}
	if s.Files != 1 {
		t.Errorf("files = %d, want 1 (only the imported track)", s.Files)
	}
	if s.TotalFileSize != 500 {
		t.Errorf("total_file_size = %d, want 500", s.TotalFileSize)
	}
}
