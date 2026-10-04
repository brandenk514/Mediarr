package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	tv "github.com/brandenk514/mediarr/internal/domains/tv"
	"github.com/brandenk514/mediarr/internal/downloads"
	"github.com/brandenk514/mediarr/internal/indexers"
	tvs "github.com/brandenk514/mediarr/internal/services/tv"
)

// TestTVPipeline_EndToEnd_Postgres drives the full TV pipeline end-to-end
// against a REAL Postgres repository (testcontainers) — the acceptance E2E for
// card #17. It mirrors the M1 movie pipeline test: add series → want episodes →
// fake-indexer search → quality match → mock-client download → import →
// satisfied, with all state persisted through the postgres TVRepo.
//
// Unlike the in-memory service E2E, every read/write here round-trips through
// Postgres, so this also exercises the migration, the repo SQL, and the
// service-to-repo contract together.
func TestTVPipeline_EndToEnd_Postgres(t *testing.T) {
	repo := startTVPG(t)
	ctx := context.Background()

	dlDir := t.TempDir()
	mediaRoot := t.TempDir()
	mock, err := downloads.NewMockClient("mock", dlDir)
	if err != nil {
		t.Fatalf("mock client: %v", err)
	}
	fake := indexers.NewFakeIndexer("fake")
	// Two candidates for S01E01 (1080p wins) and a multi-ep release for S01E02/E03.
	fake.AddRelease(indexers.SearchResult{Title: "Breaking.Bad.S01E01.Piloto.1080p.WEB.x264", SizeBytes: 1_900_000_000})
	fake.AddRelease(indexers.SearchResult{Title: "Breaking.Bad.S01E01.Piloto.720p.WEB.x264", SizeBytes: 1_000_000_000})
	fake.AddRelease(indexers.SearchResult{Title: "Breaking.Bad.S01E02E03.1080p.WEB.x264", SizeBytes: 3_400_000_000})

	svc := tvs.New(tvs.Deps{
		Repo:           repo,
		Indexers:       []indexers.Searcher{fake},
		Client:         mock,
		MediaRoot:      mediaRoot,
		DefaultProfile: "HD-1080p",
	})

	// 1. Add the series (monitored by default) and three monitored episodes.
	seriesID, err := svc.AddSeries(ctx, "Breaking Bad", "2008", "HD-1080p")
	if err != nil {
		t.Fatalf("add series: %v", err)
	}
	for i, title := range []string{"Piloto", "Cat in a Tree", "...And No One Lets Crying Out"} {
		if _, err := svc.AddEpisode(ctx, seriesID, 1, i+1, title, true); err != nil {
			t.Fatalf("add episode %d: %v", i+1, err)
		}
	}

	// 2. Run the pipeline. It should import all three episodes:
	//    S01E01 from the 1080p single, S01E02+S01E03 from the multi-ep release.
	imported, err := svc.RunPipeline(ctx, seriesID)
	if err != nil {
		t.Fatalf("run pipeline: %v", err)
	}
	if imported != 3 {
		t.Fatalf("imported = %d, want 3", imported)
	}

	// 3. All three files must exist on disk at the documented layout.
	for i, title := range []string{"Piloto", "Cat in a Tree", "...And No One Lets Crying Out"} {
		want := episodePath(mediaRoot, "Breaking Bad", 2008, 1, i+1, title, ".mkv")
		if _, err := os.Stat(want); err != nil {
			t.Errorf("S01E%02d (%s) not on disk: %v", i+1, title, err)
		}
	}

	// 4. All three wanted rows are satisfied (read back from Postgres).
	for ep := 1; ep <= 3; ep++ {
		w, err := repo.GetWanted(ctx, seriesID, 1, ep)
		if err != nil {
			t.Fatalf("get wanted S01E%02d: %v", ep, err)
		}
		if w.Status != tv.WantedSatisfied {
			t.Errorf("S01E%02d status = %q, want satisfied", ep, w.Status)
		}
	}
	// S01E01 was satisfied by the 1080p release (the better candidate).
	if w, _ := repo.GetWanted(ctx, seriesID, 1, 1); w.ReleaseTitle != "Breaking.Bad.S01E01.Piloto.1080p.WEB.x264" {
		t.Errorf("S01E01 satisfied by = %q, want the 1080p release", w.ReleaseTitle)
	}

	// 5. Queue has exactly two downloads (one single-ep, one multi-ep release),
	//    both complete.
	qs, err := repo.ListQueue(ctx, seriesID)
	if err != nil {
		t.Fatalf("list queue: %v", err)
	}
	if len(qs) != 2 {
		t.Fatalf("queue len = %d, want 2 (one download per distinct release)", len(qs))
	}
	for _, q := range qs {
		if q.State != tv.QueueComplete {
			t.Errorf("queue %q state = %q, want complete", q.ReleaseTitle, q.State)
		}
	}

	// 6. History is non-empty and includes the add + import events.
	hist, err := repo.ListHistory(ctx, seriesID, 100)
	if err != nil {
		t.Fatalf("list history: %v", err)
	}
	if len(hist) == 0 {
		t.Error("expected history entries")
	}

	// 7. Idempotency: no pending wanted remains, so a re-run is a no-op.
	if _, err := svc.RunPipeline(ctx, seriesID); !errors.Is(err, tvs.ErrNoWanted) {
		t.Errorf("re-run pipeline: err = %v, want ErrNoWanted (already satisfied)", err)
	}
	// Still exactly two queue entries (no duplicate download on re-run).
	qs, _ = repo.ListQueue(ctx, seriesID)
	if len(qs) != 2 {
		t.Errorf("queue len after re-run = %d, want 2", len(qs))
	}
}

// TestTVPipeline_NoMatch_Postgres asserts the NoMatch path against real Postgres:
// a 480p-only pool cannot satisfy an HD-1080p profile, so nothing is imported
// and the episode stays pending.
func TestTVPipeline_NoMatch_Postgres(t *testing.T) {
	repo := startTVPG(t)
	ctx := context.Background()

	dlDir := t.TempDir()
	mediaRoot := t.TempDir()
	mock, _ := downloads.NewMockClient("mock", dlDir)
	fake := indexers.NewFakeIndexer("fake")
	fake.AddRelease(indexers.SearchResult{Title: "Fringe.S01E01.480p.WEB.x264", SizeBytes: 1_000_000_000})

	svc := tvs.New(tvs.Deps{
		Repo:           repo,
		Indexers:       []indexers.Searcher{fake},
		Client:         mock,
		MediaRoot:      mediaRoot,
		DefaultProfile: "HD-1080p",
	})

	seriesID, err := svc.AddSeries(ctx, "Fringe", "2008", "HD-1080p")
	if err != nil {
		t.Fatalf("add series: %v", err)
	}
	if _, err := svc.AddEpisode(ctx, seriesID, 1, 1, "Pilot", true); err != nil {
		t.Fatalf("add episode: %v", err)
	}

	if _, err := svc.RunPipeline(ctx, seriesID); !errors.Is(err, tvs.ErrNoMatch) {
		t.Fatalf("run pipeline: err = %v, want ErrNoMatch", err)
	}

	// Still pending, and nothing imported.
	w, err := repo.GetWanted(ctx, seriesID, 1, 1)
	if err != nil {
		t.Fatalf("get wanted: %v", err)
	}
	if w.Status != tv.WantedPending {
		t.Errorf("wanted status = %q, want pending after no-match", w.Status)
	}
	entries, _ := filepath.Glob(filepath.Join(mediaRoot, "*"))
	if len(entries) != 0 {
		t.Errorf("expected nothing imported, found %d entries", len(entries))
	}
}

// episodePath builds the documented TV import destination for an episode so the
// tests can assert on the real layout:
//
//	root/<Series (Year)>/Season <NN>/<Series> S<xx>E<yy>[ <EpisodeTitle>].<ext>
func episodePath(root, series string, year, season, episode int, epTitle, ext string) string {
	name := series
	seriesDir := name
	if year > 0 {
		seriesDir = fmt.Sprintf("%s (%d)", name, year)
	}
	fileName := fmt.Sprintf("%s S%02dE%02d", name, season, episode)
	if epTitle != "" {
		fileName += " " + epTitle
	}
	return filepath.Join(root, seriesDir, fmt.Sprintf("Season %02d", season), fileName+ext)
}
