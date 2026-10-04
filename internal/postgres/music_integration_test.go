package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	music "github.com/brandenk514/mediarr/internal/domains/music"
	"github.com/brandenk514/mediarr/internal/downloads"
	"github.com/brandenk514/mediarr/internal/indexers"
	"github.com/brandenk514/mediarr/internal/postgres"
	musics "github.com/brandenk514/mediarr/internal/services/music"
	_ "github.com/jackc/pgx/v5/stdlib"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

// startMusicPG spins up a Postgres container, opens a pool, runs migrations,
// and returns a *MusicRepo. Mirrors the TV/auth integration-test harness.
func startMusicPG(t *testing.T) *postgres.MusicRepo {
	t.Helper()
	ctx := context.Background()

	c, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("mediarr"),
		tcpostgres.WithUsername("mediarr"),
		tcpostgres.WithPassword("mediarr-test"),
	)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}
	t.Cleanup(func() { _ = c.Terminate(context.Background()) })

	dsn, err := c.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	deadline := time.Now().Add(30 * time.Second)
	for {
		if err := db.PingContext(ctx); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("ping: %v", err)
		}
		time.Sleep(500 * time.Millisecond)
	}

	st := postgres.NewWithDB(db)
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return postgres.NewMusicRepo(db)
}

// TestMusicRepo_ArtistAlbumLifecycle exercises the artist/album CRUD round-trip
// against real Postgres (the 0004_music schema + MusicRepo SQL).
func TestMusicRepo_ArtistAlbumLifecycle(t *testing.T) {
	r := startMusicPG(t)
	ctx := context.Background()

	artistID, err := r.CreateArtist(ctx, music.Artist{Name: "Bob Marley", Monitored: true})
	if err != nil {
		t.Fatalf("create artist: %v", err)
	}
	if artistID == 0 {
		t.Fatal("expected non-zero artist id")
	}

	a, err := r.GetArtist(ctx, artistID)
	if err != nil {
		t.Fatalf("get artist: %v", err)
	}
	if a.Name != "Bob Marley" {
		t.Errorf("artist = %q, want Bob Marley", a.Name)
	}
	if !a.Monitored {
		t.Error("artist should be monitored")
	}
	if a.AddedAt.IsZero() {
		t.Error("added_at should be set (defaults to now())")
	}

	// Unknown id → ErrArtistNotFound.
	if _, err := r.GetArtist(ctx, 9999); err != music.ErrArtistNotFound {
		t.Errorf("get missing artist: err = %v, want ErrArtistNotFound", err)
	}

	// Duplicate artist name must be rejected by the UNIQUE constraint.
	if _, err := r.CreateArtist(ctx, music.Artist{Name: "Bob Marley"}); err == nil {
		t.Error("duplicate artist name should fail")
	}

	all, err := r.ListArtists(ctx)
	if err != nil {
		t.Fatalf("list artists: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("list artists len = %d, want 1", len(all))
	}

	// Albums: (name, year) identity within an artist.
	albumID, err := r.CreateAlbum(ctx, music.Album{
		ArtistID: artistID, Name: "Legend", Year: 1977,
		QualityProfile: "Lossless", Monitored: true, VerifyChecksums: true,
	})
	if err != nil {
		t.Fatalf("create album: %v", err)
	}
	al, err := r.GetAlbum(ctx, albumID)
	if err != nil {
		t.Fatalf("get album: %v", err)
	}
	if al.Name != "Legend" || al.Year != 1977 || al.QualityProfile != "Lossless" {
		t.Errorf("album = %+v, want Legend 1977 Lossless", *al)
	}

	// Duplicate (artist, name, year) must be rejected.
	if _, err := r.CreateAlbum(ctx, music.Album{ArtistID: artistID, Name: "Legend", Year: 1977}); err == nil {
		t.Error("duplicate (artist,name,year) album should fail")
	}

	albums, err := r.ListAlbums(ctx, artistID)
	if err != nil {
		t.Fatalf("list albums: %v", err)
	}
	if len(albums) != 1 {
		t.Errorf("albums len = %d, want 1", len(albums))
	}

	// Toggle album monitoring.
	if err := r.SetAlbumMonitored(ctx, albumID, false); err != nil {
		t.Fatalf("set album monitored: %v", err)
	}
	al, _ = r.GetAlbum(ctx, albumID)
	if al.Monitored {
		t.Error("album should be unmonitored after SetAlbumMonitored(false)")
	}
}

// TestMusicRepo_TrackLifecycle exercises track CRUD + (disc, number) identity.
func TestMusicRepo_TrackLifecycle(t *testing.T) {
	r := startMusicPG(t)
	ctx := context.Background()

	artistID, _ := r.CreateArtist(ctx, music.Artist{Name: "The Beatles"})
	albumID, _ := r.CreateAlbum(ctx, music.Album{ArtistID: artistID, Name: "The White Album", Year: 1968})

	if _, err := r.CreateTrack(ctx, music.Track{AlbumID: albumID, Disc: 1, Number: 1, Title: "Back in the U.S.S.R.", Monitored: true}); err != nil {
		t.Fatalf("create track 1: %v", err)
	}
	if _, err := r.CreateTrack(ctx, music.Track{AlbumID: albumID, Disc: 1, Number: 2, Title: "Dear Prudence"}); err != nil {
		t.Fatalf("create track 2: %v", err)
	}

	tr, err := r.GetTrack(ctx, albumID, 1, 1)
	if err != nil {
		t.Fatalf("get track: %v", err)
	}
	if tr.Number != 1 || tr.Title != "Back in the U.S.S.R." {
		t.Errorf("track = %+v, want 1 Back in the U.S.S.R.", *tr)
	}

	// Unknown track → ErrTrackNotFound.
	if _, err := r.GetTrack(ctx, albumID, 1, 99); err != music.ErrTrackNotFound {
		t.Errorf("get missing track: err = %v, want ErrTrackNotFound", err)
	}

	// Duplicate (disc, number) must be rejected.
	if _, err := r.CreateTrack(ctx, music.Track{AlbumID: albumID, Disc: 1, Number: 1, Title: "dup"}); err == nil {
		t.Error("duplicate (disc,number) track should fail")
	}

	tracks, err := r.ListTracks(ctx, albumID)
	if err != nil {
		t.Fatalf("list tracks: %v", err)
	}
	if len(tracks) != 2 {
		t.Fatalf("tracks len = %d, want 2", len(tracks))
	}
	if tracks[0].Number != 1 || tracks[1].Number != 2 {
		t.Errorf("track order = %d,%d, want 1,2", tracks[0].Number, tracks[1].Number)
	}

	// Toggle a single track's monitoring.
	if err := r.SetTrackMonitored(ctx, albumID, 1, 2, true); err != nil {
		t.Fatalf("set track monitored: %v", err)
	}
	t2, _ := r.GetTrack(ctx, albumID, 1, 2)
	if !t2.Monitored {
		t.Error("track 2 should be monitored after SetTrackMonitored(true)")
	}
}

// TestMusicRepo_WantedGranularity verifies wanted rows at both album and track
// granularity are distinct, idempotent to ensure, and satisfiable.
func TestMusicRepo_WantedGranularity(t *testing.T) {
	r := startMusicPG(t)
	ctx := context.Background()

	artistID, _ := r.CreateArtist(ctx, music.Artist{Name: "Bob Marley"})
	albumID, _ := r.CreateAlbum(ctx, music.Album{ArtistID: artistID, Name: "Uprising", Year: 1980})
	trackID, _ := r.CreateTrack(ctx, music.Track{AlbumID: albumID, Disc: 1, Number: 1, Title: "Redemption Song"})

	// A whole-album want (trackID 0) and a track-level want are distinct rows.
	if err := r.EnsureWantedAlbum(ctx, albumID); err != nil {
		t.Fatalf("ensure album wanted: %v", err)
	}
	if err := r.EnsureWantedTrack(ctx, albumID, trackID); err != nil {
		t.Fatalf("ensure track wanted: %v", err)
	}
	// Idempotent: re-ensuring the album want must not create a second row.
	if err := r.EnsureWantedAlbum(ctx, albumID); err != nil {
		t.Fatalf("re-ensure album wanted: %v", err)
	}

	wants, err := r.ListWanted(ctx, albumID)
	if err != nil {
		t.Fatalf("list wanted: %v", err)
	}
	if len(wants) != 2 {
		t.Fatalf("wanted len = %d, want 2 (album + track)", len(wants))
	}

	// Satisfy only the track-level want.
	if err := r.MarkWantedSatisfied(ctx, albumID, trackID, "Bob Marley - Uprising [FLAC Lossless]"); err != nil {
		t.Fatalf("satisfy track: %v", err)
	}
	wt, err := r.GetWanted(ctx, albumID, trackID)
	if err != nil {
		t.Fatalf("get track wanted: %v", err)
	}
	if wt.Status != music.WantedSatisfied || wt.ReleaseTitle == "" {
		t.Errorf("track wanted = %+v, want satisfied + release", *wt)
	}
	if wt.SatisfiedAt == nil {
		t.Error("satisfied_at should be set")
	}
	// The album-level want must remain pending.
	wa, err := r.GetWanted(ctx, albumID, 0)
	if err != nil {
		t.Fatalf("get album wanted: %v", err)
	}
	if wa.Status != music.WantedPending {
		t.Errorf("album wanted = %q, want pending (only the track was satisfied)", wa.Status)
	}
}

// TestMusicRepo_QueueAndHistory exercises queue + history round-trips.
func TestMusicRepo_QueueAndHistory(t *testing.T) {
	r := startMusicPG(t)
	ctx := context.Background()

	artistID, _ := r.CreateArtist(ctx, music.Artist{Name: "Bob Marley"})
	albumID, _ := r.CreateAlbum(ctx, music.Album{ArtistID: artistID, Name: "Legend", Year: 1977})

	qid, err := r.CreateQueue(ctx, music.QueueEntry{
		ArtistID: artistID, AlbumID: albumID,
		ReleaseTitle: "Bob Marley - Legend [FLAC Lossless]",
		Indexer:      "fake", DownloadClient: "mock", State: music.QueueQueued,
	})
	if err != nil {
		t.Fatalf("create queue: %v", err)
	}

	qe, err := r.GetQueue(ctx, qid)
	if err != nil {
		t.Fatalf("get queue: %v", err)
	}
	qe.State = music.QueueComplete
	qe.Progress = 100
	if err := r.UpdateQueue(ctx, *qe); err != nil {
		t.Fatalf("update queue: %v", err)
	}
	qe, _ = r.GetQueue(ctx, qid)
	if qe.State != music.QueueComplete || qe.Progress != 100 {
		t.Errorf("queue = state %q prog %d, want complete/100", qe.State, qe.Progress)
	}

	qs, err := r.ListQueue(ctx, artistID)
	if err != nil {
		t.Fatalf("list queue: %v", err)
	}
	if len(qs) != 1 {
		t.Errorf("list queue len = %d, want 1", len(qs))
	}

	// Per-artist history is append-only.
	if err := r.AddHistory(ctx, music.HistoryEntry{ArtistID: artistID, Event: "added"}); err != nil {
		t.Fatalf("add history: %v", err)
	}
	if err := r.AddHistory(ctx, music.HistoryEntry{ArtistID: artistID, Event: "imported", Detail: "Legend"}); err != nil {
		t.Fatalf("add history 2: %v", err)
	}
	h, err := r.ListHistory(ctx, artistID, 10)
	if err != nil {
		t.Fatalf("list history: %v", err)
	}
	if len(h) != 2 {
		t.Fatalf("history len = %d, want 2", len(h))
	}
}

// musicAlbumPath builds the documented whole-album import destination so the
// tests can assert on the real layout:
//
//	root/<Artist>/<Album>/<Album>.<ext>
func musicAlbumPath(root, artist, album, ext string) string {
	return filepath.Join(root, artist, album, album+ext)
}

// TestMusicPipeline_EndToEnd_Postgres drives the full music pipeline end-to-end
// against a REAL Postgres repository (testcontainers) — the acceptance E2E for
// card #18. It mirrors the M1 movie / M2 TV pipeline tests: add artist → want
// album → fake-indexer search → lossless quality match → mock-client download →
// import → satisfied, with all state persisted through the postgres MusicRepo.
//
// Unlike the in-memory service E2E, every read/write here round-trips through
// Postgres, so this also exercises the 0004_music migration, the MusicRepo SQL,
// and the service-to-repo contract together.
func TestMusicPipeline_EndToEnd_Postgres(t *testing.T) {
	repo := startMusicPG(t)
	ctx := context.Background()

	dlDir := t.TempDir()
	mediaRoot := t.TempDir()
	mock, err := downloads.NewMockClient("mock", dlDir)
	if err != nil {
		t.Fatalf("mock client: %v", err)
	}
	fake := indexers.NewFakeIndexer("fake")
	// Two candidates for Legend (FLAC wins) and a distractor for Uprising.
	fake.AddRelease(indexers.SearchResult{Title: "Bob Marley - Legend [FLAC Lossless]", SizeBytes: 4_200_000_000})
	fake.AddRelease(indexers.SearchResult{Title: "Bob Marley - Legend [MP3 320kbps]", SizeBytes: 1_100_000_000})
	fake.AddRelease(indexers.SearchResult{Title: "Bob Marley - Uprising [FLAC Lossless]", SizeBytes: 4_200_000_000})

	svc := musics.New(musics.Deps{
		Repo:           repo,
		Indexers:       []indexers.Searcher{fake},
		Client:         mock,
		MediaRoot:      mediaRoot,
		DefaultProfile: "Lossless",
	})

	// 1. Add the artist (monitored) and a monitored album (whole-album wanted).
	artistID, err := svc.AddArtist(ctx, "Bob Marley", true)
	if err != nil {
		t.Fatalf("add artist: %v", err)
	}
	legendID, err := svc.AddAlbum(ctx, artistID, "Legend", 1977, "Lossless", true)
	if err != nil {
		t.Fatalf("add album: %v", err)
	}
	// An unmonitored second album (its release is a distractor, not imported).
	if _, err := svc.AddAlbum(ctx, artistID, "Uprising", 1980, "Lossless", false); err != nil {
		t.Fatalf("add album 2: %v", err)
	}

	// 2. Run the pipeline. It should import exactly the monitored Legend album.
	imported, err := svc.RunPipeline(ctx, artistID)
	if err != nil {
		t.Fatalf("run pipeline: %v", err)
	}
	if imported != 1 {
		t.Fatalf("imported = %d, want 1 (only the monitored album)", imported)
	}

	// 3. The whole-album file must exist on disk at the documented layout.
	if _, err := os.Stat(musicAlbumPath(mediaRoot, "Bob Marley", "Legend", ".flac")); err != nil {
		t.Errorf("imported file Legend not on disk: %v", err)
	}
	// The unmonitored Uprising must NOT have been imported.
	if _, err := os.Stat(musicAlbumPath(mediaRoot, "Bob Marley", "Uprising", ".flac")); err == nil {
		t.Errorf("unmonitored Uprising was imported; it should have been left alone")
	}

	// 4. The Legend wanted row is satisfied (read back from Postgres) by FLAC.
	w, err := repo.GetWanted(ctx, legendID, 0)
	if err != nil {
		t.Fatalf("get wanted: %v", err)
	}
	if w.Status != music.WantedSatisfied {
		t.Errorf("wanted status = %q, want satisfied", w.Status)
	}
	if w.ReleaseTitle != "Bob Marley - Legend [FLAC Lossless]" {
		t.Errorf("satisfied by = %q, want the FLAC release", w.ReleaseTitle)
	}

	// 5. Queue has exactly one download, complete.
	qs, err := repo.ListQueue(ctx, artistID)
	if err != nil {
		t.Fatalf("list queue: %v", err)
	}
	if len(qs) != 1 {
		t.Fatalf("queue len = %d, want 1 (one download for the monitored album)", len(qs))
	}
	if qs[0].State != music.QueueComplete {
		t.Errorf("queue state = %q, want complete", qs[0].State)
	}

	// 6. History is non-empty.
	hist, err := repo.ListHistory(ctx, artistID, 100)
	if err != nil {
		t.Fatalf("list history: %v", err)
	}
	if len(hist) == 0 {
		t.Error("expected history entries")
	}

	// 7. Idempotency: no pending wanted remains, so a re-run is a no-op.
	if _, err := svc.RunPipeline(ctx, artistID); !errors.Is(err, musics.ErrNoWanted) {
		t.Errorf("re-run pipeline: err = %v, want ErrNoWanted (already satisfied)", err)
	}
	// Still exactly one queue entry (no duplicate download on re-run).
	qs, _ = repo.ListQueue(ctx, artistID)
	if len(qs) != 1 {
		t.Errorf("queue len after re-run = %d, want 1", len(qs))
	}
}
