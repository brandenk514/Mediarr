package postgres_test

// L1 (Lidarr parity phase) backend coverage against real Postgres with
// migration 0015: the new artist/album/track fields round-trip, the album
// TrackCount/FileCount are computed from the tracks (never stored), and the
// library summary counts are one-pass SQL.

import (
	"context"
	"testing"

	music "github.com/brandenk514/mediarr/internal/domains/music"
)

// TestMusicRepo_L1ArtistFieldsRoundTrip writes an artist with every L1 field
// populated and reads it back via GetArtist and ListArtists.
func TestMusicRepo_L1ArtistFieldsRoundTrip(t *testing.T) {
	r := startMusicPG(t)
	ctx := context.Background()

	links := []music.ArtistLink{
		{Name: "MusicBrainz", URL: "https://musicbrainz.org/artist/mbid-1"},
		{Name: "Official", URL: "https://example.com"},
	}
	id, err := r.CreateArtist(ctx, music.Artist{
		MBID:           "mbid-1",
		Name:           "Bob Marley",
		Monitored:      true,
		Status:         music.StatusEnded,
		QualityProfile: "Lossless",
		Path:           "Bob Marley (1977)",
		Rating:         8.7,
		Genre:          "Reggae",
		Links:          links,
	})
	if err != nil {
		t.Fatalf("create artist: %v", err)
	}

	assertArtistL1 := func(a *music.Artist, where string) {
		t.Helper()
		if a.Status != music.StatusEnded {
			t.Errorf("%s: status = %q, want ended", where, a.Status)
		}
		if a.QualityProfile != "Lossless" {
			t.Errorf("%s: quality_profile = %q, want Lossless", where, a.QualityProfile)
		}
		if a.Path != "Bob Marley (1977)" {
			t.Errorf("%s: path = %q, want Bob Marley (1977)", where, a.Path)
		}
		if a.Rating != 8.7 {
			t.Errorf("%s: rating = %v, want 8.7", where, a.Rating)
		}
		if a.Genre != "Reggae" {
			t.Errorf("%s: genre = %q, want Reggae", where, a.Genre)
		}
		if len(a.Links) != 2 || a.Links[0].Name != "MusicBrainz" || a.Links[0].URL != "https://musicbrainz.org/artist/mbid-1" ||
			a.Links[1].Name != "Official" || a.Links[1].URL != "https://example.com" {
			t.Errorf("%s: links = %+v, want the two seeded links", where, a.Links)
		}
	}

	a, err := r.GetArtist(ctx, id)
	if err != nil {
		t.Fatalf("get artist: %v", err)
	}
	assertArtistL1(a, "get")

	all, err := r.ListArtists(ctx)
	if err != nil {
		t.Fatalf("list artists: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("list artists len = %d, want 1", len(all))
	}
	assertArtistL1(&all[0], "list")

	// An artist created without L1 metadata round-trips the empty values
	// (the service applies the L1 defaults on the add paths; the repo is
	// pass-through).
	id2, err := r.CreateArtist(ctx, music.Artist{Name: "Queen"})
	if err != nil {
		t.Fatalf("create bare artist: %v", err)
	}
	b, err := r.GetArtist(ctx, id2)
	if err != nil {
		t.Fatalf("get bare artist: %v", err)
	}
	if b.Status != "" || b.Path != "" || b.QualityProfile != "" || b.Rating != 0 || b.Genre != "" || len(b.Links) != 0 {
		t.Errorf("bare artist L1 fields not empty: %+v", b)
	}
}

// TestMusicRepo_L1AlbumAndTrackFieldsRoundTrip writes an album with the L1
// display fields and tracks with audio-info/file-status fields, then reads
// them back — including the computed TrackCount/FileCount.
func TestMusicRepo_L1AlbumAndTrackFieldsRoundTrip(t *testing.T) {
	r := startMusicPG(t)
	ctx := context.Background()

	artistID, err := r.CreateArtist(ctx, music.Artist{Name: "Bob Marley", Monitored: true})
	if err != nil {
		t.Fatalf("create artist: %v", err)
	}
	albumID, err := r.CreateAlbum(ctx, music.Album{
		ArtistID:        artistID,
		Name:            "Legend",
		Year:            1977,
		Monitored:       true,
		VerifyChecksums: true,
		AlbumType:       "compilation",
		ReleaseDate:     "1984-09-21",
		Rating:          4.5,
		Duration:        4767,
		Size:            4200000000,
	})
	if err != nil {
		t.Fatalf("create album: %v", err)
	}

	// A fresh album has no known tracks: both counts are zero.
	a, err := r.GetAlbum(ctx, albumID)
	if err != nil {
		t.Fatalf("get album: %v", err)
	}
	if a.AlbumType != "compilation" || a.ReleaseDate != "1984-09-21" || a.Rating != 4.5 || a.Duration != 4767 || a.Size != 4200000000 {
		t.Errorf("album L1 fields = %+v", a)
	}
	if a.TrackCount != 0 || a.FileCount != 0 {
		t.Errorf("fresh album counts = %d/%d, want 0/0", a.TrackCount, a.FileCount)
	}

	// Two tracks with an imported file (status non-empty) and one missing.
	for _, tr := range []music.Track{
		{AlbumID: albumID, Disc: 1, Number: 1, Title: "No Woman, No Man", Monitored: true,
			Status: "FLAC", Duration: 240, Codec: "FLAC", Channels: 2, Kbps: 1000,
			SampleRate: 44, BitDepth: 16, Size: 300000000},
		{AlbumID: albumID, Disc: 1, Number: 2, Title: "Three Little Birds", Monitored: true,
			Status: "FLAC", Size: 280000000},
		{AlbumID: albumID, Disc: 1, Number: 3, Title: "Waiting in Vain"},
	} {
		if _, err := r.CreateTrack(ctx, tr); err != nil {
			t.Fatalf("create track %d: %v", tr.Number, err)
		}
	}

	a, err = r.GetAlbum(ctx, albumID)
	if err != nil {
		t.Fatalf("get album after tracks: %v", err)
	}
	// "3/9"-style badge: 3 known tracks, 2 with an imported file.
	if a.TrackCount != 3 || a.FileCount != 2 {
		t.Errorf("album counts = %d/%d, want 3/2", a.TrackCount, a.FileCount)
	}

	// ListAlbums computes the same counts (the grid detail uses it).
	albums, err := r.ListAlbums(ctx, artistID)
	if err != nil {
		t.Fatalf("list albums: %v", err)
	}
	if len(albums) != 1 || albums[0].TrackCount != 3 || albums[0].FileCount != 2 {
		t.Errorf("list album counts = %+v", albums)
	}

	// Track round-trip: audio info + file status survive the write.
	tk, err := r.GetTrack(ctx, albumID, 1, 1)
	if err != nil {
		t.Fatalf("get track: %v", err)
	}
	if tk.Status != "FLAC" || tk.Duration != 240 || tk.Codec != "FLAC" || tk.Channels != 2 ||
		tk.Kbps != 1000 || tk.SampleRate != 44 || tk.BitDepth != 16 || tk.Size != 300000000 {
		t.Errorf("track fields = %+v", tk)
	}
	// A missing (disc, number) is ErrTrackNotFound.
	if _, err := r.GetTrack(ctx, albumID, 1, 99); err != music.ErrTrackNotFound {
		t.Errorf("get missing track: err = %v, want ErrTrackNotFound", err)
	}
}

// TestMusicRepo_L1LibrarySummary checks the footer status bar counts against
// a known library shape: 3 artists (2 continuing, 1 inactive; 2 monitored)
// and 3 tracks (2 with imported files totaling 300 bytes).
func TestMusicRepo_L1LibrarySummary(t *testing.T) {
	r := startMusicPG(t)
	ctx := context.Background()

	a1, err := r.CreateArtist(ctx, music.Artist{Name: "Bob Marley", Status: "continuing", Monitored: true})
	if err != nil {
		t.Fatalf("create artist: %v", err)
	}
	for _, a := range []music.Artist{
		{Name: "Queen", Status: "continuing"},
		{Name: "Def Leppard", Status: "inactive", Monitored: true},
	} {
		if _, err := r.CreateArtist(ctx, a); err != nil {
			t.Fatalf("create artist %s: %v", a.Name, err)
		}
	}

	albumID, err := r.CreateAlbum(ctx, music.Album{ArtistID: a1, Name: "Legend", Year: 1977, Monitored: true, VerifyChecksums: true})
	if err != nil {
		t.Fatalf("create album: %v", err)
	}
	for _, tr := range []music.Track{
		{AlbumID: albumID, Disc: 1, Number: 1, Title: "A", Status: "FLAC", Size: 100},
		{AlbumID: albumID, Disc: 1, Number: 2, Title: "B", Status: "MP3", Size: 200},
		{AlbumID: albumID, Disc: 1, Number: 3, Title: "C"},
	} {
		if _, err := r.CreateTrack(ctx, tr); err != nil {
			t.Fatalf("create track %d: %v", tr.Number, err)
		}
	}

	s, err := r.LibrarySummary(ctx)
	if err != nil {
		t.Fatalf("library summary: %v", err)
	}
	if s.Artists != 3 {
		t.Errorf("artists = %d, want 3", s.Artists)
	}
	if s.Continuing != 2 {
		t.Errorf("continuing = %d, want 2", s.Continuing)
	}
	if s.Inactive != 1 {
		t.Errorf("inactive = %d, want 1", s.Inactive)
	}
	if s.Monitored != 2 || s.Unmonitored != 1 {
		t.Errorf("monitored/unmonitored = %d/%d, want 2/1", s.Monitored, s.Unmonitored)
	}
	if s.Tracks != 3 {
		t.Errorf("tracks = %d, want 3", s.Tracks)
	}
	if s.Files != 2 {
		t.Errorf("files = %d, want 2 (tracks with an imported file)", s.Files)
	}
	if s.TotalFileSize != 300 {
		t.Errorf("total_file_size = %d, want 300 (sum of file-bearing tracks only)", s.TotalFileSize)
	}
}
