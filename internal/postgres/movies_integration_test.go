package postgres_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	movies "github.com/brandenk514/mediarr/internal/domains/movies"
	"github.com/brandenk514/mediarr/internal/postgres"
	_ "github.com/jackc/pgx/v5/stdlib"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

// startPG spins up a Postgres container, opens a pool, runs migrations, and
// returns a *MovieRepo. Mirrors the auth integration-test harness.
func startPG(t *testing.T) *postgres.MovieRepo {
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
	return postgres.NewMovieRepo(db)
}

func TestMovieRepo_MovieLifecycle(t *testing.T) {
	r := startPG(t)
	ctx := context.Background()

	id, err := r.CreateMovie(ctx, movies.Movie{
		Title: "Dune Part Two", Year: 2024, QualityProfile: "HD-1080p", Monitored: true,
	})
	if err != nil {
		t.Fatalf("create movie: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero id")
	}

	m, err := r.GetMovie(ctx, id)
	if err != nil {
		t.Fatalf("get movie: %v", err)
	}
	if m.Title != "Dune Part Two" || m.Year != 2024 || m.QualityProfile != "HD-1080p" {
		t.Errorf("movie = %+v, want Dune Part Two 2024 HD-1080p", *m)
	}
	if m.AddedAt.IsZero() {
		t.Error("added_at should be set (defaults to now())")
	}

	// Unknown id → ErrMovieNotFound.
	if _, err := r.GetMovie(ctx, 9999); err != movies.ErrMovieNotFound {
		t.Errorf("get missing: err = %v, want ErrMovieNotFound", err)
	}

	// Duplicate (title,year) must be rejected by the UNIQUE constraint.
	if _, err := r.CreateMovie(ctx, movies.Movie{Title: "Dune Part Two", Year: 2024}); err == nil {
		t.Error("duplicate (title,year) should fail")
	}

	all, err := r.ListMovies(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("list len = %d, want 1", len(all))
	}
}

func TestMovieRepo_WantedLifecycle(t *testing.T) {
	r := startPG(t)
	ctx := context.Background()

	id, err := r.CreateMovie(ctx, movies.Movie{Title: "Inception", Year: 2010, QualityProfile: "HD-1080p"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Idempotent: two EnsureWanted calls → exactly one row.
	if err := r.EnsureWanted(ctx, id); err != nil {
		t.Fatalf("ensure wanted: %v", err)
	}
	if err := r.EnsureWanted(ctx, id); err != nil {
		t.Fatalf("ensure wanted (again): %v", err)
	}
	wants, err := r.ListWanted(ctx)
	if err != nil {
		t.Fatalf("list wanted: %v", err)
	}
	if len(wants) != 1 {
		t.Fatalf("wanted len = %d, want 1 (EnsureWanted must be idempotent)", len(wants))
	}
	if wants[0].Status != movies.WantedPending {
		t.Errorf("status = %q, want pending", wants[0].Status)
	}

	// Satisfy it.
	if err := r.MarkWantedSatisfied(ctx, id, "Inception.2010.1080p.WEB.x264"); err != nil {
		t.Fatalf("satisfy: %v", err)
	}
	w, err := r.GetWanted(ctx, id)
	if err != nil {
		t.Fatalf("get wanted: %v", err)
	}
	if w.Status != movies.WantedSatisfied {
		t.Errorf("status = %q, want satisfied", w.Status)
	}
	if w.ReleaseTitle != "Inception.2010.1080p.WEB.x264" {
		t.Errorf("release_title = %q, want the satisfied release", w.ReleaseTitle)
	}
	if w.SatisfiedAt == nil {
		t.Error("satisfied_at should be set")
	}
}

func TestMovieRepo_QueueAndHistory(t *testing.T) {
	r := startPG(t)
	ctx := context.Background()

	id, err := r.CreateMovie(ctx, movies.Movie{Title: "Interstellar", Year: 2014})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	qid, err := r.CreateQueue(ctx, movies.QueueEntry{
		MovieID: id, ReleaseTitle: "Interstellar.2014.1080p.BluRay.x264",
		Indexer: "fake", DownloadClient: "mock", State: movies.QueueQueued,
	})
	if err != nil {
		t.Fatalf("create queue: %v", err)
	}

	// Advance state.
	qe, _ := r.GetQueue(ctx, qid)
	qe.State = movies.QueueComplete
	qe.Progress = 100
	if err := r.UpdateQueue(ctx, *qe); err != nil {
		t.Fatalf("update queue: %v", err)
	}
	qe, _ = r.GetQueue(ctx, qid)
	if qe.State != movies.QueueComplete || qe.Progress != 100 {
		t.Errorf("queue = state %q prog %d, want complete/100", qe.State, qe.Progress)
	}

	// History is append-only.
	if err := r.AddHistory(ctx, movies.HistoryEntry{MovieID: id, Event: "added"}); err != nil {
		t.Fatalf("add history: %v", err)
	}
	if err := r.AddHistory(ctx, movies.HistoryEntry{MovieID: id, Event: "imported"}); err != nil {
		t.Fatalf("add history 2: %v", err)
	}
	h, err := r.ListHistory(ctx, 10)
	if err != nil {
		t.Fatalf("list history: %v", err)
	}
	if len(h) != 2 {
		t.Fatalf("history len = %d, want 2", len(h))
	}
}
