package wanted

import (
	"context"
	"fmt"
	"testing"
	"time"

	dom "github.com/brandenk514/mediarr/internal/domains/wanted"
)

func musicTestRows() []dom.MusicRow {
	return []dom.MusicRow{
		// Monitored, no files: missing.
		{ArtistID: 1, ArtistTitle: "Bob Marley", AlbumID: 10, AlbumTitle: "Legend", ReleaseDate: mustTime("1977-06-13"), Monitored: true, TrackCount: 2},
		// Monitored, one FLAC file, no profile: default cutoff (FLAC 24bit)
		// -> cutoff-unmet.
		{ArtistID: 1, ArtistTitle: "Bob Marley", AlbumID: 11, AlbumTitle: "Uprising", Monitored: true, FileCount: 1, ImportedQualities: []string{"FLAC"}},
		// Unmonitored file-less album: absent from every tab.
		{ArtistID: 2, ArtistTitle: "Jimi Hendrix", AlbumID: 12, AlbumTitle: "Axis", Monitored: false, TrackCount: 2},
		// Monitored track with no file: a missing track row.
		{ArtistID: 1, ArtistTitle: "Bob Marley", AlbumID: 10, AlbumTitle: "Legend", Monitored: true, Disc: 1, Number: 1, TrackID: 101, TrackTitle: "No Woman, No Man", TrackMonitored: true},
	}
}

func mustTime(s string) *time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return &t
}

func TestListMusic_Missing(t *testing.T) {
	repo := &fakeRepo{musicRows: musicTestRows()}
	svc := New(repo, "/media/music")

	p, err := svc.ListMusic(context.Background(), dom.TabMissing, "", 0, 0)
	if err != nil {
		t.Fatalf("ListMusic: %v", err)
	}
	if p.Total != 2 || len(p.Items) != 2 {
		t.Fatalf("total/len = %d/%d, want 2/2 (file-less album + file-less track)", p.Total, len(p.Items))
	}
	if p.Items[0].Kind != "music" || p.Items[0].AlbumTitle != "Legend" {
		t.Errorf("first item = %+v, want the music album row", p.Items[0])
	}
	// The enriched columns ride along.
	if p.Items[0].ReleaseDate == nil || !p.Items[0].ReleaseDate.Equal(*mustTime("1977-06-13")) {
		t.Errorf("release date = %v, want 1977-06-13", p.Items[0].ReleaseDate)
	}
}

func TestListMusic_Cutoff(t *testing.T) {
	repo := &fakeRepo{musicRows: musicTestRows()}
	svc := New(repo, "/media/music")

	p, err := svc.ListMusic(context.Background(), dom.TabCutoff, "", 0, 0)
	if err != nil {
		t.Fatalf("ListMusic: %v", err)
	}
	if p.Total != 1 || len(p.Items) != 1 {
		t.Fatalf("cutoff total/len = %d/%d, want 1/1 (Uprising below default cutoff)", p.Total, len(p.Items))
	}
	it := p.Items[0]
	if it.AlbumTitle != "Uprising" || it.Quality != "FLAC" {
		t.Errorf("cutoff row = %+v", it)
	}
	if it.Status != "cutoff:FLAC below cutoff FLAC 24bit" {
		t.Errorf("cutoff status = %q", it.Status)
	}
}

func TestListMusic_Search(t *testing.T) {
	repo := &fakeRepo{musicRows: musicTestRows()}
	svc := New(repo, "/media/music")

	p, err := svc.ListMusic(context.Background(), dom.TabMissing, "uprising", 0, 0)
	if err != nil {
		t.Fatalf("ListMusic: %v", err)
	}
	if p.Total != 0 {
		t.Fatalf("search missing 'uprising' total = %d, want 0 (Uprising is imported)", p.Total)
	}
	p, err = svc.ListMusic(context.Background(), dom.TabMissing, "bob", 0, 0)
	if err != nil {
		t.Fatalf("ListMusic: %v", err)
	}
	if p.Total != 2 {
		t.Fatalf("search artist 'bob' total = %d, want 2", p.Total)
	}
}

func TestListMusic_BadTab(t *testing.T) {
	svc := New(&fakeRepo{}, "/media/music")
	if _, err := svc.ListMusic(context.Background(), dom.Tab("nope"), "", 0, 0); err != dom.ErrUnknownTab {
		t.Fatalf("err = %v, want ErrUnknownTab", err)
	}
}

func TestSearchAllMusic(t *testing.T) {
	repo := &fakeRepo{musicRows: musicTestRows()}
	svc := New(repo, "/media/music")

	n, err := svc.SearchAllMusic(context.Background(), dom.TabMissing)
	if err != nil {
		t.Fatalf("SearchAllMusic: %v", err)
	}
	// Legend (album) + the track row: 2 items, both Bob Marley (artist 1).
	if n != 2 {
		t.Fatalf("searched = %d, want 2", n)
	}
	if len(repo.musicWanted) != 2 {
		t.Fatalf("wanted rows ensured = %d, want 2: %+v", len(repo.musicWanted), repo.musicWanted)
	}
	// The whole-album row is ensured for the album, the track row for the
	// track (Season=disk, Episode=number on the item).
	want := map[string]bool{
		"10/0/0": true,
		"10/1/1": true,
	}
	for _, r := range repo.musicWanted {
		k := refKey(r.albumID, r.disc, r.number)
		if !want[k] {
			t.Errorf("unexpected wanted ref %s", k)
		}
	}
	// One search-requested event for the affected artist (music_history,
	// the kind "music" source of the global feed).
	var musicEvents int
	for _, h := range repo.history {
		if h.kind == "music" && h.event == "search-requested" {
			musicEvents++
		}
	}
	if musicEvents != 1 {
		t.Fatalf("music history events = %d, want 1: %+v", musicEvents, repo.history)
	}
}

func refKey(albumID int64, disc, number int) string {
	return fmt.Sprintf("%d/%d/%d", albumID, disc, number)
}

func TestUnmonitorMusic(t *testing.T) {
	repo := &fakeRepo{musicOff: map[string]bool{
		"10/0/0": true,
		"10/1/1": true,
	}}
	svc := New(repo, "/media/music")

	n, err := svc.UnmonitorMusic(context.Background(), []dom.MusicRef{
		{AlbumID: 10},                     // whole album: exists -> counted
		{AlbumID: 10, Disc: 1, Number: 1}, // track: exists -> counted
		{AlbumID: 99},                     // unknown: skipped
	})
	if err != nil {
		t.Fatalf("UnmonitorMusic: %v", err)
	}
	if n != 2 {
		t.Fatalf("unmonitored = %d, want 2", n)
	}
	if repo.musicOff["10/0/0"] || repo.musicOff["10/1/1"] {
		t.Fatalf("flags not flipped: %+v", repo.musicOff)
	}
}
