package movies

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	dom "github.com/brandenk514/mediarr/internal/domains/movies"
	"github.com/brandenk514/mediarr/internal/downloads"
	"github.com/brandenk514/mediarr/internal/indexers"
)

// memRepo is an in-memory implementation of dom.Repo used by the end-to-end
// service test so the full pipeline runs with no Postgres or network I/O.
type memRepo struct {
	movies    map[int64]dom.Movie
	nextMovie int64
	wanted    map[int64]dom.Wanted
	queue     map[int64]dom.QueueEntry
	nextQueue int64
	history   []dom.HistoryEntry
	nextHist  int64
}

func newMemRepo() *memRepo {
	return &memRepo{
		movies:    map[int64]dom.Movie{},
		wanted:    map[int64]dom.Wanted{},
		queue:     map[int64]dom.QueueEntry{},
		nextMovie: 1,
		nextQueue: 1,
		nextHist:  1,
	}
}

func (r *memRepo) CreateMovie(ctx context.Context, m dom.Movie) (int64, error) {
	for _, v := range r.movies {
		if v.Title == m.Title && v.Year == m.Year {
			return 0, errors.New("duplicate")
		}
	}
	m.ID = r.nextMovie
	r.nextMovie++
	m.AddedAt = time.Now()
	r.movies[m.ID] = m
	return m.ID, nil
}

func (r *memRepo) GetMovie(ctx context.Context, id int64) (*dom.Movie, error) {
	m, ok := r.movies[id]
	if !ok {
		return nil, dom.ErrMovieNotFound
	}
	return &m, nil
}

func (r *memRepo) ListMovies(ctx context.Context) ([]dom.Movie, error) {
	var out []dom.Movie
	for _, m := range r.movies {
		out = append(out, m)
	}
	return out, nil
}

func (r *memRepo) EnsureWanted(ctx context.Context, movieID int64) error {
	if _, ok := r.wanted[movieID]; !ok {
		r.wanted[movieID] = dom.Wanted{MovieID: movieID, Status: dom.WantedPending, CreatedAt: time.Now()}
	}
	return nil
}

func (r *memRepo) GetWanted(ctx context.Context, movieID int64) (*dom.Wanted, error) {
	w, ok := r.wanted[movieID]
	if !ok {
		return nil, dom.ErrMovieNotFound
	}
	return &w, nil
}

func (r *memRepo) ListWanted(ctx context.Context) ([]dom.Wanted, error) {
	var out []dom.Wanted
	for _, w := range r.wanted {
		out = append(out, w)
	}
	return out, nil
}

func (r *memRepo) MarkWantedSatisfied(ctx context.Context, movieID int64, releaseTitle string) error {
	w, ok := r.wanted[movieID]
	if !ok {
		return dom.ErrMovieNotFound
	}
	now := time.Now()
	w.Status = dom.WantedSatisfied
	w.ReleaseTitle = releaseTitle
	w.SatisfiedAt = &now
	r.wanted[movieID] = w
	return nil
}

func (r *memRepo) CreateQueue(ctx context.Context, e dom.QueueEntry) (int64, error) {
	e.ID = r.nextQueue
	r.nextQueue++
	now := time.Now()
	e.CreatedAt = now
	e.UpdatedAt = now
	r.queue[e.ID] = e
	return e.ID, nil
}

func (r *memRepo) UpdateQueue(ctx context.Context, e dom.QueueEntry) error {
	q, ok := r.queue[e.ID]
	if !ok {
		return dom.ErrMovieNotFound
	}
	q.State = e.State
	q.Progress = e.Progress
	q.UpdatedAt = time.Now()
	r.queue[e.ID] = q
	return nil
}

func (r *memRepo) GetQueue(ctx context.Context, id int64) (*dom.QueueEntry, error) {
	e, ok := r.queue[id]
	if !ok {
		return nil, dom.ErrMovieNotFound
	}
	return &e, nil
}

func (r *memRepo) ListQueue(ctx context.Context) ([]dom.QueueEntry, error) {
	var out []dom.QueueEntry
	for _, e := range r.queue {
		out = append(out, e)
	}
	return out, nil
}

func (r *memRepo) AddHistory(ctx context.Context, e dom.HistoryEntry) error {
	e.ID = r.nextHist
	r.nextHist++
	e.At = time.Now()
	r.history = append(r.history, e)
	return nil
}

func (r *memRepo) ListHistory(ctx context.Context, limit int) ([]dom.HistoryEntry, error) {
	if limit <= 0 {
		limit = len(r.history)
	}
	n := limit
	if n > len(r.history) {
		n = len(r.history)
	}
	// Most recent `n` entries, newest first (mirror ORDER BY at DESC).
	start := len(r.history) - n
	recent := make([]dom.HistoryEntry, 0, n)
	for i := len(r.history) - 1; i >= start; i-- {
		recent = append(recent, r.history[i])
	}
	return recent, nil
}

var _ dom.Repo = (*memRepo)(nil)

// TestPipeline_EndToEnd drives the full movie pipeline with an in-memory repo,
// a fake indexer, and the mock download client: add → want → search → match →
// download → import → satisfied.
func TestPipeline_EndToEnd(t *testing.T) {
	dlDir := t.TempDir()
	mediaRoot := t.TempDir()
	mock, err := downloads.NewMockClient("mock", dlDir)
	if err != nil {
		t.Fatalf("mock client: %v", err)
	}
	fake := indexers.NewFakeIndexer("fake")
	fake.AddRelease(indexers.SearchResult{
		Title:     "Dune.Part.Two.2024.1080p.WEB.x265.10bit",
		SizeBytes: 5_000_000_000,
	})
	fake.AddRelease(indexers.SearchResult{
		Title:     "Dune.Part.Two.2024.720p.WEB.x264",
		SizeBytes: 3_000_000_000,
	})

	repo := newMemRepo()
	svc := New(Deps{
		Repo:           repo,
		Indexers:       []indexers.Searcher{fake},
		Client:         mock,
		MediaRoot:      mediaRoot,
		DefaultProfile: "HD-1080p",
	})

	ctx := context.Background()

	// 1. Add the movie.
	id, err := svc.AddMovie(ctx, "Dune Part Two", "2024", "HD-1080p")
	if err != nil {
		t.Fatalf("add movie: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero movie id")
	}

	// A pending wanted row must exist.
	w, err := repo.GetWanted(ctx, id)
	if err != nil {
		t.Fatalf("get wanted: %v", err)
	}
	if w.Status != dom.WantedPending {
		t.Errorf("wanted status = %q, want pending", w.Status)
	}

	// 2. Run the pipeline.
	imported, err := svc.RunPipeline(ctx, id)
	if err != nil {
		t.Fatalf("run pipeline: %v", err)
	}
	if imported == "" {
		t.Fatal("expected a non-empty import path")
	}
	// The file must exist on disk under the media root.
	if _, err := os.Stat(imported); err != nil {
		t.Fatalf("imported file not on disk: %v", err)
	}
	// It must land at <root>/<Title (Year)>/<Title (Year)>.mkv.
	want := filepath.Join(mediaRoot, "Dune Part Two (2024)", "Dune Part Two (2024).mkv")
	if imported != want {
		t.Errorf("imported = %q, want %q", imported, want)
	}

	// 3. Wanted is now satisfied, recording the 1080p release (the better one).
	w, _ = repo.GetWanted(ctx, id)
	if w.Status != dom.WantedSatisfied {
		t.Errorf("wanted status = %q, want satisfied", w.Status)
	}
	if w.ReleaseTitle != "Dune.Part.Two.2024.1080p.WEB.x265.10bit" {
		t.Errorf("satisfied by = %q, want the 1080p release", w.ReleaseTitle)
	}

	// 4. Queue is complete.
	qs, _ := repo.ListQueue(ctx)
	if len(qs) != 1 {
		t.Fatalf("queue len = %d, want 1", len(qs))
	}
	if qs[0].State != dom.QueueComplete {
		t.Errorf("queue state = %q, want complete", qs[0].State)
	}

	// 5. History is non-empty.
	hist, _ := repo.ListHistory(ctx, 100)
	if len(hist) == 0 {
		t.Error("expected history entries")
	}

	// 6. Idempotency: running the pipeline again must not fail or re-import.
	if _, err := svc.RunPipeline(ctx, id); err != nil {
		t.Errorf("re-run pipeline: %v", err)
	}
	// Only one queue entry should exist (no duplicate download).
	qs, _ = repo.ListQueue(ctx)
	if len(qs) != 1 {
		t.Errorf("queue len after re-run = %d, want 1", len(qs))
	}
}

// TestPipeline_NoMatch verifies that when no release satisfies the profile, the
// pipeline returns ErrNoMatch without importing or satisfying.
func TestPipeline_NoMatch(t *testing.T) {
	dlDir := t.TempDir()
	mediaRoot := t.TempDir()
	mock, _ := downloads.NewMockClient("mock", dlDir)
	fake := indexers.NewFakeIndexer("fake")
	// Only a 480p release — below HD-1080p's 720p floor.
	fake.AddRelease(indexers.SearchResult{
		Title:     "Dune.Part.Two.2024.480p.WEB.x264",
		SizeBytes: 1_000_000_000,
	})

	repo := newMemRepo()
	svc := New(Deps{
		Repo:      repo,
		Indexers:  []indexers.Searcher{fake},
		Client:    mock,
		MediaRoot: mediaRoot,
	})
	ctx := context.Background()

	id, err := svc.AddMovie(ctx, "Dune Part Two", "2024", "HD-1080p")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	_, err = svc.RunPipeline(ctx, id)
	if err != ErrNoMatch {
		t.Fatalf("run pipeline: err = %v, want ErrNoMatch", err)
	}
	// Wanted must still be pending (not satisfied).
	w, _ := repo.GetWanted(ctx, id)
	if w.Status != dom.WantedPending {
		t.Errorf("wanted status = %q, want pending after no-match", w.Status)
	}
	// Nothing should have been imported.
	entries, _ := filepath.Glob(filepath.Join(mediaRoot, "*"))
	if len(entries) != 0 {
		t.Errorf("expected nothing imported, found %d entries", len(entries))
	}
}

// TestPipeline_PicksBestRelease verifies the scorer picks the higher resolution
// (1080p) over 720p among candidates.
func TestPipeline_PicksBestRelease(t *testing.T) {
	dlDir := t.TempDir()
	mediaRoot := t.TempDir()
	mock, _ := downloads.NewMockClient("mock", dlDir)
	fake := indexers.NewFakeIndexer("fake")
	fake.AddRelease(indexers.SearchResult{Title: "Dune.Part.Two.2024.720p.WEB.x264", SizeBytes: 1})
	fake.AddRelease(indexers.SearchResult{Title: "Dune.Part.Two.2024.1080p.WEB.x265", SizeBytes: 2})

	repo := newMemRepo()
	svc := New(Deps{
		Repo:      repo,
		Indexers:  []indexers.Searcher{fake},
		Client:    mock,
		MediaRoot: mediaRoot,
	})
	ctx := context.Background()
	id, _ := svc.AddMovie(ctx, "Dune Part Two", "2024", "HD-1080p")
	if _, err := svc.RunPipeline(ctx, id); err != nil {
		t.Fatalf("run pipeline: %v", err)
	}
	w, _ := repo.GetWanted(ctx, id)
	if w.ReleaseTitle != "Dune.Part.Two.2024.1080p.WEB.x265" {
		t.Errorf("satisfied by = %q, want the 1080p release", w.ReleaseTitle)
	}
}
