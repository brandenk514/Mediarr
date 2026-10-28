package postgres_test

// L4 (Lidarr parity phase) backend coverage against real Postgres with
// migration 0018: the new artist/album metadata columns round-trip,
// UpdateAlbum replaces the enrichment fields in place, and DeleteTracks
// clears an album's tracks for the refresh flow.

import (
	"context"
	"testing"

	music "github.com/brandenk514/mediarr/internal/domains/music"
)

// TestMusicRepo_L4ArtistCoverRoundTrip writes an artist with the L4 cover
// column and reads it back; a bare artist round-trips "".
func TestMusicRepo_L4ArtistCoverRoundTrip(t *testing.T) {
	r := startMusicPG(t)
	ctx := context.Background()

	id, err := r.CreateArtist(ctx, music.Artist{
		Name:      "Radiohead",
		Monitored: true,
		Genre:     "Alternative Rock",
		Links:     []music.ArtistLink{{Name: "MusicBrainz", URL: "https://musicbrainz.org/artist/mbid-r"}},
		Cover:     "https://coverartarchive.org/artist/mbid-r/500/abc.jpg",
	})
	if err != nil {
		t.Fatalf("create artist: %v", err)
	}
	a, err := r.GetArtist(ctx, id)
	if err != nil {
		t.Fatalf("get artist: %v", err)
	}
	if a.Cover != "https://coverartarchive.org/artist/mbid-r/500/abc.jpg" {
		t.Errorf("cover = %q, want the CAA URL", a.Cover)
	}

	// UpdateArtist persists the L4 enrichment fields in place.
	a.Rating = 9.1
	a.Genre = "Art Rock"
	a.Links = nil
	a.Cover = "https://coverartarchive.org/artist/mbid-r/500/def.jpg"
	if err := r.UpdateArtist(ctx, *a); err != nil {
		t.Fatalf("update artist: %v", err)
	}
	got, err := r.GetArtist(ctx, id)
	if err != nil {
		t.Fatalf("get artist after update: %v", err)
	}
	if got.Rating != 9.1 || got.Genre != "Art Rock" || len(got.Links) != 0 || got.Cover != "https://coverartarchive.org/artist/mbid-r/500/def.jpg" {
		t.Errorf("artist after UpdateArtist = %+v", got)
	}

	// A bare artist created without L4 fields round-trips empty values.
	bare, err := r.CreateArtist(ctx, music.Artist{Name: "Queen"})
	if err != nil {
		t.Fatalf("create bare artist: %v", err)
	}
	b, err := r.GetArtist(ctx, bare)
	if err != nil {
		t.Fatalf("get bare artist: %v", err)
	}
	if b.Cover != "" {
		t.Errorf("bare artist cover = %q, want \"\"", b.Cover)
	}
}

// TestMusicRepo_L4AlbumMetadataRoundTrip writes an album with the L4 metadata
// fields and reads it back; an un-enriched album round-trips the empty
// defaults.
func TestMusicRepo_L4AlbumMetadataRoundTrip(t *testing.T) {
	r := startMusicPG(t)
	ctx := context.Background()

	artistID, err := r.CreateArtist(ctx, music.Artist{Name: "Radiohead"})
	if err != nil {
		t.Fatalf("create artist: %v", err)
	}
	albumID, err := r.CreateAlbum(ctx, music.Album{
		ArtistID: artistID, Name: "OK Computer", Year: 1997, Monitored: true,
		MBID:        "rel-1",
		AlbumType:   "album",
		ReleaseDate: "1997-06-16",
		Duration:    5792,
		Cover:       "https://coverartarchive.org/release/rel-1/500/abc.jpg",
		Labels:      []string{"Parlophone"},
		Genres:      []string{"Alternative Rock", "Art Rock"},
	})
	if err != nil {
		t.Fatalf("create album: %v", err)
	}

	a, err := r.GetAlbum(ctx, albumID)
	if err != nil {
		t.Fatalf("get album: %v", err)
	}
	if a.Cover != "https://coverartarchive.org/release/rel-1/500/abc.jpg" {
		t.Errorf("cover = %q, want the CAA URL", a.Cover)
	}
	if len(a.Labels) != 1 || a.Labels[0] != "Parlophone" {
		t.Errorf("labels = %v, want [Parlophone]", a.Labels)
	}
	if len(a.Genres) != 2 || a.Genres[0] != "Alternative Rock" || a.Genres[1] != "Art Rock" {
		t.Errorf("genres = %v, want [Alternative Rock Art Rock]", a.Genres)
	}

	// ListAlbums returns the same values (the grid uses it).
	albums, err := r.ListAlbums(ctx, artistID)
	if err != nil {
		t.Fatalf("list albums: %v", err)
	}
	if len(albums) != 1 || albums[0].Cover == "" || len(albums[0].Labels) != 1 || len(albums[0].Genres) != 2 {
		t.Errorf("listed album = %+v", albums[0])
	}

	// An un-enriched album round-trips empty (non-nil) lists and "".
	plain, err := r.CreateAlbum(ctx, music.Album{ArtistID: artistID, Name: "Plain", Year: 2000})
	if err != nil {
		t.Fatalf("create plain album: %v", err)
	}
	p, err := r.GetAlbum(ctx, plain)
	if err != nil {
		t.Fatalf("get plain album: %v", err)
	}
	if p.Cover != "" || p.Labels == nil || len(p.Labels) != 0 || p.Genres == nil || len(p.Genres) != 0 {
		t.Errorf("plain album L4 fields = %+v, want empty (non-nil) values", p)
	}
}

// TestMusicRepo_L4UpdateAlbumAndDeleteTracks exercises the refresh-flow
// primitives: UpdateAlbum replaces the enrichment fields without touching
// monitored, and DeleteTracks clears the track set.
func TestMusicRepo_L4UpdateAlbumAndDeleteTracks(t *testing.T) {
	r := startMusicPG(t)
	ctx := context.Background()

	artistID, err := r.CreateArtist(ctx, music.Artist{Name: "Radiohead"})
	if err != nil {
		t.Fatalf("create artist: %v", err)
	}
	albumID, err := r.CreateAlbum(ctx, music.Album{ArtistID: artistID, Name: "OK Computer", Monitored: true})
	if err != nil {
		t.Fatalf("create album: %v", err)
	}
	for _, tr := range []music.Track{
		{AlbumID: albumID, Disc: 1, Number: 1, Title: "Airbag", Monitored: true, Duration: 237},
		{AlbumID: albumID, Disc: 1, Number: 2, Title: "Lust", Monitored: false, Duration: 243},
	} {
		if _, err := r.CreateTrack(ctx, tr); err != nil {
			t.Fatalf("create track: %v", err)
		}
	}

	// UpdateAlbum replaces the metadata fields; monitored is untouched.
	a, err := r.GetAlbum(ctx, albumID)
	if err != nil {
		t.Fatalf("get album: %v", err)
	}
	a.MBID = "rel-1"
	a.Year = 1997
	a.AlbumType = "album"
	a.ReleaseDate = "1997-06-16"
	a.Duration = 5792
	a.Cover = "https://coverartarchive.org/release/rel-1/500/abc.jpg"
	a.Labels = []string{"Parlophone"}
	a.Genres = []string{"Alternative Rock"}
	if err := r.UpdateAlbum(ctx, *a); err != nil {
		t.Fatalf("update album: %v", err)
	}
	got, err := r.GetAlbum(ctx, albumID)
	if err != nil {
		t.Fatalf("get album after update: %v", err)
	}
	if !got.Monitored {
		t.Error("monitored was clobbered by UpdateAlbum, want true")
	}
	if got.MBID != "rel-1" || got.Year != 1997 || got.ReleaseDate != "1997-06-16" || got.Duration != 5792 ||
		got.Cover != "https://coverartarchive.org/release/rel-1/500/abc.jpg" ||
		len(got.Labels) != 1 || len(got.Genres) != 1 {
		t.Errorf("album after UpdateAlbum = %+v", got)
	}

	// DeleteTracks clears the whole track set (refresh replaces it).
	if err := r.DeleteTracks(ctx, albumID); err != nil {
		t.Fatalf("delete tracks: %v", err)
	}
	tracks, err := r.ListTracks(ctx, albumID)
	if err != nil {
		t.Fatalf("list tracks: %v", err)
	}
	if len(tracks) != 0 {
		t.Errorf("tracks after DeleteTracks = %+v, want none", tracks)
	}

	// UpdateAlbum on an unknown album is ErrAlbumNotFound.
	if err := r.UpdateAlbum(ctx, music.Album{ID: 999999}); err != music.ErrAlbumNotFound {
		t.Errorf("update unknown album: err = %v, want ErrAlbumNotFound", err)
	}
}
