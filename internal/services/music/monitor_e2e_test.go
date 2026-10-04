package music

import (
	"context"
	"os"
	"testing"

	dom "github.com/brandenk514/mediarr/internal/domains/music"
	"github.com/brandenk514/mediarr/internal/downloads"
	"github.com/brandenk514/mediarr/internal/indexers"
)

// This file holds the #22 monitoring-acceptance E2Es: the "add (unmonitored) →
// monitor → pipeline satisfies exactly that" paths, driven through the
// Set*Monitored toggles. It mirrors TestPipeline_SeasonMonitor_Acceptance in
// services/tv (added in the M2 closeout) — the toggle, not the add-time flag,
// is what creates the wanted rows the pipeline services.

// TestMonitor_Acceptance_Album is the #22 album-granularity acceptance E2E:
//
//	add artist (unmonitored) → add unmonitored albums → monitor one → pipeline
//	satisfies exactly that album
//
// The unmonitored artist starts with no wanted rows. Toggling one album on
// creates a pending whole-album wanted row; the other album stays unmonitored.
// A distractor release for the unmonitored album is present in the indexer but
// is never wanted, so it must NOT be imported — this proves album-level
// monitoring controls what the pipeline services.
func TestMonitor_Acceptance_Album(t *testing.T) {
	dlDir := t.TempDir()
	mediaRoot := t.TempDir()
	mock, _ := downloads.NewMockClient("mock", dlDir)
	fake := indexers.NewFakeIndexer("fake")
	// A release for the monitored album...
	fake.AddRelease(indexers.SearchResult{Title: "Bob Marley - Legend [FLAC Lossless]", SizeBytes: 4_200_000_000})
	// ...and a distractor for the UNmonitored album that must be left alone.
	fake.AddRelease(indexers.SearchResult{Title: "Bob Marley - Uprising [FLAC Lossless]", SizeBytes: 4_200_000_000})

	repo := newMemRepo()
	svc := New(Deps{
		Repo:           repo,
		Indexers:       []indexers.Searcher{fake},
		Client:         mock,
		MediaRoot:      mediaRoot,
		DefaultProfile: "Lossless",
	})
	ctx := context.Background()

	// 1. Add the artist unmonitored (no wanted rows yet).
	artistID, err := svc.AddArtist(ctx, "Bob Marley", false)
	if err != nil {
		t.Fatalf("add artist: %v", err)
	}
	a, _ := repo.GetArtist(ctx, artistID)
	if a.Monitored {
		t.Error("artist Monitored = true, want false (added unmonitored)")
	}

	// 2. Add two known albums, both unmonitored (no wanted rows yet).
	if _, err := svc.AddAlbum(ctx, artistID, "Legend", 1977, "Lossless", false); err != nil {
		t.Fatalf("add Legend: %v", err)
	}
	if _, err := svc.AddAlbum(ctx, artistID, "Uprising", 1980, "Lossless", false); err != nil {
		t.Fatalf("add Uprising: %v", err)
	}
	albums, _ := repo.ListAlbums(ctx, artistID)
	legendID, uprisingID := albums[0].ID, albums[1].ID
	if pendingWanted(t, repo, legendID) != 0 || pendingWanted(t, repo, uprisingID) != 0 {
		t.Fatal("wanted rows exist before monitoring; want 0 (added unmonitored)")
	}

	// 3. Monitor exactly the Legend album (Uprising stays unmonitored).
	if err := svc.SetAlbumMonitored(ctx, legendID, true); err != nil {
		t.Fatalf("monitor Legend: %v", err)
	}
	if pendingWanted(t, repo, legendID) != 1 {
		t.Errorf("Legend pending wanted after toggle = %d, want 1", pendingWanted(t, repo, legendID))
	}
	if pendingWanted(t, repo, uprisingID) != 0 {
		t.Errorf("Uprising pending wanted after toggle = %d, want 0", pendingWanted(t, repo, uprisingID))
	}

	// 4. Run the pipeline: it must import exactly the monitored Legend album.
	imported, err := svc.RunPipeline(ctx, artistID)
	if err != nil {
		t.Fatalf("run pipeline: %v", err)
	}
	if imported != 1 {
		t.Fatalf("imported = %d, want exactly 1 (only the monitored album)", imported)
	}

	// Legend is satisfied and on disk...
	w, _ := repo.GetWanted(ctx, legendID, 0)
	if w.Status != dom.WantedSatisfied {
		t.Errorf("Legend status = %q, want satisfied", w.Status)
	}
	if _, err := os.Stat(albumDestPath(mediaRoot, "Bob Marley", "Legend", ".flac")); err != nil {
		t.Errorf("imported file Legend not on disk: %v", err)
	}
	// ...and the unmonitored Uprising was left alone (no file, no wanted row).
	if _, err := os.Stat(albumDestPath(mediaRoot, "Bob Marley", "Uprising", ".flac")); err == nil {
		t.Errorf("unmonitored Uprising was imported; it should have been left alone")
	}
	if _, err := repo.GetWanted(ctx, uprisingID, 0); err == nil {
		t.Errorf("Uprising unexpectedly has a wanted row (should stay unmonitored)")
	}
}

// TestMonitor_Acceptance_Track is the #22 track-granularity acceptance E2E:
//
//	add artist (unmonitored) + unmonitored album → monitor a single track →
//	pipeline satisfies exactly that track
//
// The album is unmonitored, so the only wanted row is the track-level one
// created by the track toggle. A whole-album release that contains the track is
// imported and satisfies the track-level want. The unmonitored sibling track is
// never wanted, so it is never serviced on its own.
func TestMonitor_Acceptance_Track(t *testing.T) {
	dlDir := t.TempDir()
	mediaRoot := t.TempDir()
	mock, _ := downloads.NewMockClient("mock", dlDir)
	fake := indexers.NewFakeIndexer("fake")
	fake.AddRelease(indexers.SearchResult{Title: "Bob Marley - Legend [FLAC Lossless]", SizeBytes: 4_200_000_000})

	repo := newMemRepo()
	svc := New(Deps{
		Repo:           repo,
		Indexers:       []indexers.Searcher{fake},
		Client:         mock,
		MediaRoot:      mediaRoot,
		DefaultProfile: "Lossless",
	})
	ctx := context.Background()

	// 1. Add the artist unmonitored, then an unmonitored album (no wanted rows).
	artistID, _ := svc.AddArtist(ctx, "Bob Marley", false)
	albumID, _ := svc.AddAlbum(ctx, artistID, "Legend", 1977, "Lossless", false)
	if pendingWanted(t, repo, albumID) != 0 {
		t.Fatal("wanted rows exist before monitoring; want 0 (added unmonitored)")
	}

	// 2. Add two known tracks, both unmonitored.
	if _, err := svc.AddTrack(ctx, albumID, 1, 1, "No Woman, No Cry", false); err != nil {
		t.Fatalf("add track 1: %v", err)
	}
	if _, err := svc.AddTrack(ctx, albumID, 1, 2, "Could Be Lonely", false); err != nil {
		t.Fatalf("add track 2: %v", err)
	}

	// 3. Monitor exactly track 1 (track 2 stays unmonitored).
	t1, _ := repo.GetTrack(ctx, albumID, 1, 1)
	if err := svc.SetTrackMonitored(ctx, albumID, 1, 1, true); err != nil {
		t.Fatalf("monitor track 1: %v", err)
	}
	// A track-level wanted row now exists for track 1 (and only track 1).
	w1, err := repo.GetWanted(ctx, albumID, t1.ID)
	if err != nil {
		t.Fatalf("expected a track-level wanted row for track 1: %v", err)
	}
	if w1.Status != dom.WantedPending {
		t.Errorf("track 1 wanted status = %q, want pending", w1.Status)
	}

	// 4. Run the pipeline: the whole-album release satisfies the track want.
	imported, err := svc.RunPipeline(ctx, artistID)
	if err != nil {
		t.Fatalf("run pipeline: %v", err)
	}
	if imported != 1 {
		t.Fatalf("imported = %d, want 1 (the monitored track, satisfied by the album)", imported)
	}
	// The track-level want is now satisfied...
	w1, _ = repo.GetWanted(ctx, albumID, t1.ID)
	if w1.Status != dom.WantedSatisfied {
		t.Errorf("track 1 status = %q, want satisfied", w1.Status)
	}
	// ...and the unmonitored track 2 never got a wanted row of its own.
	t2, _ := repo.GetTrack(ctx, albumID, 1, 2)
	if _, err := repo.GetWanted(ctx, albumID, t2.ID); err == nil {
		t.Errorf("track 2 unexpectedly has a wanted row (should stay unmonitored)")
	}
}
