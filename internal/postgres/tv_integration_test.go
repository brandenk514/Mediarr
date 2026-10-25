package postgres_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	tv "github.com/brandenk514/mediarr/internal/domains/tv"
	"github.com/brandenk514/mediarr/internal/postgres"
	_ "github.com/jackc/pgx/v5/stdlib"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

// startTVPG spins up a Postgres container, opens a pool, runs migrations, and
// returns a *TVRepo. Mirrors the movies/auth integration-test harness.
func startTVPG(t *testing.T) *postgres.TVRepo {
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
	return postgres.NewTVRepo(db)
}

func TestTVRepo_SeriesLifecycle(t *testing.T) {
	r := startTVPG(t)
	ctx := context.Background()

	id, err := r.CreateSeries(ctx, tv.Series{
		Title: "Breaking Bad", Year: 2008, QualityProfile: "HD-1080p", Monitored: true,
	})
	if err != nil {
		t.Fatalf("create series: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero id")
	}

	s, err := r.GetSeries(ctx, id)
	if err != nil {
		t.Fatalf("get series: %v", err)
	}
	if s.Title != "Breaking Bad" || s.Year != 2008 || s.QualityProfile != "HD-1080p" {
		t.Errorf("series = %+v, want Breaking Bad 2008 HD-1080p", *s)
	}
	if s.AddedAt.IsZero() {
		t.Error("added_at should be set (defaults to now())")
	}

	// Unknown id → ErrSeriesNotFound.
	if _, err := r.GetSeries(ctx, 9999); err != tv.ErrSeriesNotFound {
		t.Errorf("get missing: err = %v, want ErrSeriesNotFound", err)
	}

	// Re-creating the same (title,year) is idempotent (M6 #42 find-or-create):
	// it returns the existing id instead of a UNIQUE-constraint error.
	dupID, err := r.CreateSeries(ctx, tv.Series{Title: "Breaking Bad", Year: 2008})
	if err != nil {
		t.Errorf("idempotent create: %v", err)
	} else if dupID != id {
		t.Errorf("idempotent create id = %d, want existing %d", dupID, id)
	}

	all, err := r.ListSeries(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("list len = %d, want 1", len(all))
	}

	// Toggle monitoring.
	if err := r.SetSeriesMonitored(ctx, id, false); err != nil {
		t.Fatalf("set monitored: %v", err)
	}
	s, _ = r.GetSeries(ctx, id)
	if s.Monitored {
		t.Error("series should be unmonitored after SetSeriesMonitored(false)")
	}
}

func TestTVRepo_EpisodeLifecycle(t *testing.T) {
	r := startTVPG(t)
	ctx := context.Background()

	seriesID, err := r.CreateSeries(ctx, tv.Series{Title: "The Wire", Year: 2002, Monitored: true})
	if err != nil {
		t.Fatalf("create series: %v", err)
	}

	// Create two episodes.
	if _, err := r.CreateEpisode(ctx, tv.Episode{
		SeriesID: seriesID, Season: 1, Episode: 1, Title: "The Details", Monitored: true,
	}); err != nil {
		t.Fatalf("create episode 1: %v", err)
	}
	if _, err := r.CreateEpisode(ctx, tv.Episode{
		SeriesID: seriesID, Season: 1, Episode: 2, Title: "The Target", Monitored: true,
	}); err != nil {
		t.Fatalf("create episode 2: %v", err)
	}

	// Get a single episode.
	e, err := r.GetEpisode(ctx, seriesID, 1, 1)
	if err != nil {
		t.Fatalf("get episode: %v", err)
	}
	if e.Season != 1 || e.Episode != 1 || e.Title != "The Details" {
		t.Errorf("episode = %+v, want S01E01 The Details", *e)
	}

	// Unknown episode → ErrEpisodeNotFound.
	if _, err := r.GetEpisode(ctx, seriesID, 1, 99); err != tv.ErrEpisodeNotFound {
		t.Errorf("get missing: err = %v, want ErrEpisodeNotFound", err)
	}

	// List episodes (ordered by season, episode).
	eps, err := r.ListEpisodes(ctx, seriesID)
	if err != nil {
		t.Fatalf("list episodes: %v", err)
	}
	if len(eps) != 2 {
		t.Fatalf("episodes len = %d, want 2", len(eps))
	}
	if eps[0].Episode != 1 || eps[1].Episode != 2 {
		t.Errorf("episode order = %d,%d, want 1,2", eps[0].Episode, eps[1].Episode)
	}

	// Toggle a single episode's monitoring.
	if err := r.SetEpisodeMonitored(ctx, seriesID, 1, 2, false); err != nil {
		t.Fatalf("set episode monitored: %v", err)
	}
	e2, _ := r.GetEpisode(ctx, seriesID, 1, 2)
	if e2.Monitored {
		t.Error("episode S01E02 should be unmonitored after SetEpisodeMonitored(false)")
	}
}

func TestTVRepo_WantedEpisodeGranularity(t *testing.T) {
	r := startTVPG(t)
	ctx := context.Background()

	seriesID, err := r.CreateSeries(ctx, tv.Series{Title: "Fringe", Year: 2008})
	if err != nil {
		t.Fatalf("create series: %v", err)
	}

	// Two distinct episodes must yield two distinct wanted rows (episode
	// granularity, not one-per-series as with movies).
	if err := r.EnsureWanted(ctx, seriesID, 1, 1); err != nil {
		t.Fatalf("ensure wanted S01E01: %v", err)
	}
	if err := r.EnsureWanted(ctx, seriesID, 1, 2); err != nil {
		t.Fatalf("ensure wanted S01E02: %v", err)
	}
	// Idempotent: re-adding S01E01 must not create a second row.
	if err := r.EnsureWanted(ctx, seriesID, 1, 1); err != nil {
		t.Fatalf("ensure wanted S01E01 again: %v", err)
	}

	wants, err := r.ListWanted(ctx, seriesID)
	if err != nil {
		t.Fatalf("list wanted: %v", err)
	}
	if len(wants) != 2 {
		t.Fatalf("wanted len = %d, want 2 (one per episode)", len(wants))
	}
	if wants[0].Status != tv.WantedPending {
		t.Errorf("status = %q, want pending", wants[0].Status)
	}

	// Satisfy one episode only.
	if err := r.MarkWantedSatisfied(ctx, seriesID, 1, 1, "Fringe.S01E01.1080p.WEB.x264", "HDTV-1080p"); err != nil {
		t.Fatalf("satisfy S01E01: %v", err)
	}
	w, err := r.GetWanted(ctx, seriesID, 1, 1)
	if err != nil {
		t.Fatalf("get wanted S01E01: %v", err)
	}
	if w.Status != tv.WantedSatisfied || w.ReleaseTitle != "Fringe.S01E01.1080p.WEB.x264" {
		t.Errorf("wanted S01E01 = %+v, want satisfied + release", *w)
	}
	if w.SatisfiedAt == nil {
		t.Error("satisfied_at should be set")
	}

	// The other episode must remain pending.
	w2, err := r.GetWanted(ctx, seriesID, 1, 2)
	if err != nil {
		t.Fatalf("get wanted S01E02: %v", err)
	}
	if w2.Status != tv.WantedPending {
		t.Errorf("wanted S01E02 = %q, want pending (only S01E01 was satisfied)", w2.Status)
	}
}

func TestTVRepo_QueueAndHistory(t *testing.T) {
	r := startTVPG(t)
	ctx := context.Background()

	seriesID, err := r.CreateSeries(ctx, tv.Series{Title: "Westworld", Year: 2016})
	if err != nil {
		t.Fatalf("create series: %v", err)
	}

	qid, err := r.CreateQueue(ctx, tv.QueueEntry{
		SeriesID: seriesID, Season: 1, Episode: 1,
		ReleaseTitle: "Westworld.S01E01.1080p.WEB.x264",
		Indexer:      "fake", DownloadClient: "mock", State: tv.QueueQueued,
	})
	if err != nil {
		t.Fatalf("create queue: %v", err)
	}

	// Advance state.
	qe, _ := r.GetQueue(ctx, qid)
	qe.State = tv.QueueComplete
	qe.Progress = 100
	if err := r.UpdateQueue(ctx, *qe); err != nil {
		t.Fatalf("update queue: %v", err)
	}
	qe, _ = r.GetQueue(ctx, qid)
	if qe.State != tv.QueueComplete || qe.Progress != 100 {
		t.Errorf("queue = state %q prog %d, want complete/100", qe.State, qe.Progress)
	}

	// Per-series history is append-only.
	if err := r.AddHistory(ctx, tv.HistoryEntry{SeriesID: seriesID, Event: "added"}); err != nil {
		t.Fatalf("add history: %v", err)
	}
	if err := r.AddHistory(ctx, tv.HistoryEntry{SeriesID: seriesID, Event: "download", Detail: "Westworld.S01E01"}); err != nil {
		t.Fatalf("add history 2: %v", err)
	}
	h, err := r.ListHistory(ctx, seriesID, 10)
	if err != nil {
		t.Fatalf("list history: %v", err)
	}
	if len(h) != 2 {
		t.Fatalf("history len = %d, want 2", len(h))
	}
}

func TestTVRepo_MultiEpisodeAndSeasonPack(t *testing.T) {
	r := startTVPG(t)
	ctx := context.Background()

	seriesID, err := r.CreateSeries(ctx, tv.Series{Title: "Game of Thrones", Year: 2011})
	if err != nil {
		t.Fatalf("create series: %v", err)
	}

	// A season-pack queue entry has episode = 0.
	qid, err := r.CreateQueue(ctx, tv.QueueEntry{
		SeriesID: seriesID, Season: 1, Episode: 0,
		ReleaseTitle: "Game.of.Thrones.S01.COMPLETE.1080p.WEB.x264",
		Indexer:      "fake", DownloadClient: "mock", State: tv.QueueQueued,
	})
	if err != nil {
		t.Fatalf("create season-pack queue: %v", err)
	}
	qe, _ := r.GetQueue(ctx, qid)
	if qe.Episode != 0 {
		t.Errorf("season-pack episode = %d, want 0", qe.Episode)
	}
}

// TestTVRepo_SeriesP2Fields verifies the P2 parity fields (series_type,
// season_folder, tags) survive a postgres round-trip and that UpdateSeries
// applies partial patches without touching unset fields.
func TestTVRepo_SeriesP2Fields(t *testing.T) {
	r := startTVPG(t)
	ctx := context.Background()

	tags := []string{"drama", "scifi"}
	id, err := r.CreateSeries(ctx, tv.Series{
		Title: "Severance", Year: 2022, QualityProfile: "HD-1080p", Monitored: true,
		SeriesType: "standard", SeasonFolder: true, Tags: tags,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	s, err := r.GetSeries(ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if s.SeriesType != "standard" || !s.SeasonFolder {
		t.Errorf("series = %+v, want standard + season folder", *s)
	}
	if len(s.Tags) != 2 || s.Tags[0] != "drama" || s.Tags[1] != "scifi" {
		t.Errorf("tags = %v, want [drama scifi]", s.Tags)
	}

	// Defaults for a series created without P2 fields.
	id2, err := r.CreateSeries(ctx, tv.Series{Title: "Fringe", Year: 2008, Monitored: false})
	if err != nil {
		t.Fatalf("create 2: %v", err)
	}
	s2, _ := r.GetSeries(ctx, id2)
	if s2.SeriesType != "standard" || s2.SeasonFolder || len(s2.Tags) != 0 {
		t.Errorf("defaults = %+v, want standard/false/[]", *s2)
	}

	// Partial update: change profile + tags, keep season folder.
	newProfile := "UHD-2160p"
	newTags := []string{"retro"}
	if err := r.UpdateSeries(ctx, id, tv.SeriesPatch{QualityProfile: &newProfile, Tags: &newTags}); err != nil {
		t.Fatalf("update: %v", err)
	}
	s3, _ := r.GetSeries(ctx, id)
	if s3.QualityProfile != "UHD-2160p" {
		t.Errorf("profile = %q, want UHD-2160p", s3.QualityProfile)
	}
	if len(s3.Tags) != 1 || s3.Tags[0] != "retro" {
		t.Errorf("tags = %v, want [retro]", s3.Tags)
	}
	if !s3.SeasonFolder || s3.Monitored != true {
		t.Errorf("untouched fields changed: %+v", *s3)
	}

	// Empty patch is a no-op.
	if err := r.UpdateSeries(ctx, id, tv.SeriesPatch{}); err != nil {
		t.Fatalf("empty patch: %v", err)
	}
	// Unknown id.
	if err := r.UpdateSeries(ctx, 999, tv.SeriesPatch{}); err != tv.ErrSeriesNotFound {
		t.Errorf("update missing err = %v, want ErrSeriesNotFound", err)
	}
}
