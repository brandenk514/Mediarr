package tv

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	dom "github.com/brandenk514/mediarr/internal/domains/tv"
	"github.com/brandenk514/mediarr/internal/downloads"
	"github.com/brandenk514/mediarr/internal/indexers"
)

// memRepo is an in-memory implementation of dom.Repo (the TV persistence
// surface) used by the end-to-end service test so the full pipeline runs with
// no Postgres or network I/O. It mirrors the movies service test harness.
type memRepo struct {
	series     map[int64]dom.Series
	nextSeries int64
	episodes   map[int64][]dom.Episode // seriesID -> episodes
	nextEp     int64
	wanted     map[wantedKey]dom.Wanted
	queue      map[int64]dom.QueueEntry
	nextQueue  int64
	history    []dom.HistoryEntry
	nextHist   int64
}

// wantedKey identifies a wanted row by its episode specifier.
type wantedKey struct {
	series  int64
	season  int
	episode int
}

func newMemRepo() *memRepo {
	return &memRepo{
		series:     map[int64]dom.Series{},
		episodes:   map[int64][]dom.Episode{},
		wanted:     map[wantedKey]dom.Wanted{},
		queue:      map[int64]dom.QueueEntry{},
		nextSeries: 1,
		nextEp:     1,
		nextQueue:  1,
		nextHist:   1,
	}
}

func (r *memRepo) CreateSeries(ctx context.Context, s dom.Series) (int64, error) {
	for _, v := range r.series {
		if v.Title == s.Title && v.Year == s.Year {
			return 0, errors.New("duplicate series")
		}
	}
	s.ID = r.nextSeries
	r.nextSeries++
	s.AddedAt = time.Now()
	r.series[s.ID] = s
	return s.ID, nil
}

func (r *memRepo) GetSeries(ctx context.Context, id int64) (*dom.Series, error) {
	s, ok := r.series[id]
	if !ok {
		return nil, dom.ErrSeriesNotFound
	}
	return &s, nil
}

func (r *memRepo) ListSeries(ctx context.Context) ([]dom.Series, error) {
	var out []dom.Series
	for _, s := range r.series {
		out = append(out, s)
	}
	return out, nil
}

func (r *memRepo) SetSeriesMonitored(ctx context.Context, id int64, monitored bool) error {
	s, ok := r.series[id]
	if !ok {
		return dom.ErrSeriesNotFound
	}
	s.Monitored = monitored
	r.series[id] = s
	return nil
}

func (r *memRepo) CreateEpisode(ctx context.Context, e dom.Episode) (int64, error) {
	for _, v := range r.episodes[e.SeriesID] {
		if v.Season == e.Season && v.Episode == e.Episode {
			return 0, errors.New("duplicate episode")
		}
	}
	e.ID = r.nextEp
	r.nextEp++
	e.AddedAt = time.Now()
	r.episodes[e.SeriesID] = append(r.episodes[e.SeriesID], e)
	return e.ID, nil
}

func (r *memRepo) GetEpisode(ctx context.Context, seriesID int64, season, episode int) (*dom.Episode, error) {
	for _, e := range r.episodes[seriesID] {
		if e.Season == season && e.Episode == episode {
			return &e, nil
		}
	}
	return nil, dom.ErrEpisodeNotFound
}

func (r *memRepo) ListEpisodes(ctx context.Context, seriesID int64) ([]dom.Episode, error) {
	return r.episodes[seriesID], nil
}

func (r *memRepo) SetEpisodeMonitored(ctx context.Context, seriesID int64, season, episode int, monitored bool) error {
	for i, e := range r.episodes[seriesID] {
		if e.Season == season && e.Episode == episode {
			e.Monitored = monitored
			r.episodes[seriesID][i] = e
			return nil
		}
	}
	return dom.ErrEpisodeNotFound
}

func (r *memRepo) EnsureWanted(ctx context.Context, seriesID int64, season, episode int) error {
	k := wantedKey{seriesID, season, episode}
	if _, ok := r.wanted[k]; !ok {
		r.wanted[k] = dom.Wanted{SeriesID: seriesID, Season: season, Episode: episode, Status: dom.WantedPending, CreatedAt: time.Now()}
	}
	return nil
}

func (r *memRepo) GetWanted(ctx context.Context, seriesID int64, season, episode int) (*dom.Wanted, error) {
	k := wantedKey{seriesID, season, episode}
	w, ok := r.wanted[k]
	if !ok {
		return nil, dom.ErrEpisodeNotFound
	}
	return &w, nil
}

func (r *memRepo) ListWanted(ctx context.Context, seriesID int64) ([]dom.Wanted, error) {
	var out []dom.Wanted
	for _, w := range r.wanted {
		if w.SeriesID == seriesID {
			out = append(out, w)
		}
	}
	return out, nil
}

func (r *memRepo) MarkWantedSatisfied(ctx context.Context, seriesID int64, season, episode int, releaseTitle string) error {
	k := wantedKey{seriesID, season, episode}
	w, ok := r.wanted[k]
	if !ok {
		return dom.ErrEpisodeNotFound
	}
	now := time.Now()
	w.Status = dom.WantedSatisfied
	w.ReleaseTitle = releaseTitle
	w.SatisfiedAt = &now
	r.wanted[k] = w
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
		return dom.ErrSeriesNotFound
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
		return nil, dom.ErrSeriesNotFound
	}
	return &e, nil
}

func (r *memRepo) ListQueue(ctx context.Context, seriesID int64) ([]dom.QueueEntry, error) {
	var out []dom.QueueEntry
	for _, e := range r.queue {
		if e.SeriesID == seriesID {
			out = append(out, e)
		}
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

func (r *memRepo) ListHistory(ctx context.Context, seriesID int64, limit int) ([]dom.HistoryEntry, error) {
	var out []dom.HistoryEntry
	for _, e := range r.history {
		if e.SeriesID == seriesID {
			out = append(out, e)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

var _ dom.Repo = (*memRepo)(nil)

// episodePath builds the documented import destination for an episode so the
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

// TestPipeline_EndToEnd drives the full TV pipeline: add series → add
// episode (monitored) → search → match → download → import → satisfied.
func TestPipeline_EndToEnd(t *testing.T) {
	dlDir := t.TempDir()
	mediaRoot := t.TempDir()
	mock, err := downloads.NewMockClient("mock", dlDir)
	if err != nil {
		t.Fatalf("mock client: %v", err)
	}
	fake := indexers.NewFakeIndexer("fake")
	fake.AddRelease(indexers.SearchResult{Title: "Breaking.Bad.S01E01.Piloto.1080p.WEB.x264", SizeBytes: 1_900_000_000})
	fake.AddRelease(indexers.SearchResult{Title: "Breaking.Bad.S01E01.Piloto.720p.WEB.x264", SizeBytes: 1_000_000_000})

	repo := newMemRepo()
	svc := New(Deps{
		Repo:           repo,
		Indexers:       []indexers.Searcher{fake},
		Client:         mock,
		MediaRoot:      mediaRoot,
		DefaultProfile: "HD-1080p",
	})
	ctx := context.Background()

	// 1. Add series + a monitored episode (which ensures a pending wanted row).
	seriesID, err := svc.AddSeries(ctx, "Breaking Bad", "2008", "HD-1080p", true)
	if err != nil {
		t.Fatalf("add series: %v", err)
	}
	if _, err := svc.AddEpisode(ctx, seriesID, 1, 1, "Piloto", true); err != nil {
		t.Fatalf("add episode: %v", err)
	}
	w, err := repo.GetWanted(ctx, seriesID, 1, 1)
	if err != nil {
		t.Fatalf("get wanted: %v", err)
	}
	if w.Status != dom.WantedPending {
		t.Errorf("wanted status = %q, want pending", w.Status)
	}

	// 2. Run the pipeline.
	imported, err := svc.RunPipeline(ctx, seriesID)
	if err != nil {
		t.Fatalf("run pipeline: %v", err)
	}
	if imported != 1 {
		t.Fatalf("imported = %d, want 1", imported)
	}

	// 3. The file must exist at the documented layout.
	wantPath := episodePath(mediaRoot, "Breaking Bad", 2008, 1, 1, "Piloto", ".mkv")
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("imported file not on disk: %v", err)
	}

	// 4. Wanted is now satisfied by the 1080p release (the better one).
	w, _ = repo.GetWanted(ctx, seriesID, 1, 1)
	if w.Status != dom.WantedSatisfied {
		t.Errorf("wanted status = %q, want satisfied", w.Status)
	}
	if w.ReleaseTitle != "Breaking.Bad.S01E01.Piloto.1080p.WEB.x264" {
		t.Errorf("satisfied by = %q, want the 1080p release", w.ReleaseTitle)
	}

	// 5. Queue is complete.
	qs, _ := repo.ListQueue(ctx, seriesID)
	if len(qs) != 1 {
		t.Fatalf("queue len = %d, want 1", len(qs))
	}
	if qs[0].State != dom.QueueComplete {
		t.Errorf("queue state = %q, want complete", qs[0].State)
	}

	// 6. Idempotency: no pending wanted remains, so a re-run is a no-op.
	if _, err := svc.RunPipeline(ctx, seriesID); !errors.Is(err, ErrNoWanted) {
		t.Errorf("re-run pipeline: err = %v, want ErrNoWanted (already satisfied)", err)
	}
}

// TestPipeline_MultiEpisode verifies a single multi-episode release satisfies
// several wanted episodes with one download.
func TestPipeline_MultiEpisode(t *testing.T) {
	dlDir := t.TempDir()
	mediaRoot := t.TempDir()
	mock, _ := downloads.NewMockClient("mock", dlDir)
	fake := indexers.NewFakeIndexer("fake")
	fake.AddRelease(indexers.SearchResult{Title: "The.Wire.S01E01E02.1080p.WEB.x264", SizeBytes: 3_300_000_000})

	repo := newMemRepo()
	svc := New(Deps{
		Repo:           repo,
		Indexers:       []indexers.Searcher{fake},
		Client:         mock,
		MediaRoot:      mediaRoot,
		DefaultProfile: "HD-1080p",
	})
	ctx := context.Background()

	seriesID, _ := svc.AddSeries(ctx, "The Wire", "2002", "HD-1080p", true)
	if _, err := svc.AddEpisode(ctx, seriesID, 1, 1, "The Details", true); err != nil {
		t.Fatalf("add e1: %v", err)
	}
	if _, err := svc.AddEpisode(ctx, seriesID, 1, 2, "The Target", true); err != nil {
		t.Fatalf("add e2: %v", err)
	}

	imported, err := svc.RunPipeline(ctx, seriesID)
	if err != nil {
		t.Fatalf("run pipeline: %v", err)
	}
	if imported != 2 {
		t.Fatalf("imported = %d, want 2 (both episodes from one release)", imported)
	}

	// Both wanted rows are satisfied.
	for _, ep := range []int{1, 2} {
		w, _ := repo.GetWanted(ctx, seriesID, 1, ep)
		if w.Status != dom.WantedSatisfied {
			t.Errorf("S01E%02d status = %q, want satisfied", ep, w.Status)
		}
	}

	// One physical download (queue entry) for the multi-ep release.
	qs, _ := repo.ListQueue(ctx, seriesID)
	if len(qs) != 1 {
		t.Errorf("queue len = %d, want 1 (one download for the multi-ep release)", len(qs))
	}

	// Both episode files exist on disk at their per-episode names.
	if _, err := os.Stat(episodePath(mediaRoot, "The Wire", 2002, 1, 1, "The Details", ".mkv")); err != nil {
		t.Errorf("episode file S01E01 not on disk: %v", err)
	}
	if _, err := os.Stat(episodePath(mediaRoot, "The Wire", 2002, 1, 2, "The Target", ".mkv")); err != nil {
		t.Errorf("episode file S01E02 not on disk: %v", err)
	}
}

// TestPipeline_SeasonMonitor_Acceptance is the #16 acceptance E2E:
//
//	add series (unmonitored) → monitor S1E1-2 → pipeline satisfies exactly those
//
// The unmonitored series starts with no wanted rows; the two episodes that are
// explicitly monitored each get a pending wanted row, and the pipeline services
// exactly those. A distractor release for the unmonitored S1E3 is present in the
// indexer but is never wanted, so it must NOT be imported — this proves
// monitoring at episode granularity controls what the pipeline services.
func TestPipeline_SeasonMonitor_Acceptance(t *testing.T) {
	dlDir := t.TempDir()
	mediaRoot := t.TempDir()
	mock, _ := downloads.NewMockClient("mock", dlDir)
	fake := indexers.NewFakeIndexer("fake")
	// Releases for the two monitored episodes...
	fake.AddRelease(indexers.SearchResult{Title: "Fringe.S01E01.Pilot.1080p.WEB.x264", SizeBytes: 1_900_000_000})
	fake.AddRelease(indexers.SearchResult{Title: "Fringe.S01E02.Over-Looked.1080p.WEB.x264", SizeBytes: 1_900_000_000})
	// ...and a distractor for the UNmonitored S1E3 that must be left alone.
	fake.AddRelease(indexers.SearchResult{Title: "Fringe.S01E03.Bright.Lights.1080p.WEB.x264", SizeBytes: 1_900_000_000})

	repo := newMemRepo()
	svc := New(Deps{
		Repo:           repo,
		Indexers:       []indexers.Searcher{fake},
		Client:         mock,
		MediaRoot:      mediaRoot,
		DefaultProfile: "HD-1080p",
	})
	ctx := context.Background()

	// 1. Add the series unmonitored (no wanted rows yet).
	seriesID, err := svc.AddSeries(ctx, "Fringe", "2008", "HD-1080p", false)
	if err != nil {
		t.Fatalf("add series: %v", err)
	}
	s, _ := repo.GetSeries(ctx, seriesID)
	if s.Monitored {
		t.Errorf("series Monitored = true, want false (added unmonitored)")
	}

	// 2. Add three known episodes; the unmonitored E3 is present but unwanted.
	for ep, title := range map[int]string{1: "Pilot", 2: "Over-Looked", 3: "Bright Lights"} {
		if _, err := svc.AddEpisode(ctx, seriesID, 1, ep, title, false); err != nil {
			t.Fatalf("add episode %d: %v", ep, err)
		}
	}
	// No wanted rows exist yet.
	if wants, _ := repo.ListWanted(ctx, seriesID); len(wants) != 0 {
		t.Fatalf("wanted len = %d before monitoring, want 0", len(wants))
	}

	// 3. Monitor exactly S1E1 and S1E2 (S1E3 stays unmonitored).
	if err := svc.SetEpisodeMonitored(ctx, seriesID, 1, 1, true); err != nil {
		t.Fatalf("monitor E1: %v", err)
	}
	if err := svc.SetEpisodeMonitored(ctx, seriesID, 1, 2, true); err != nil {
		t.Fatalf("monitor E2: %v", err)
	}
	wants, _ := repo.ListWanted(ctx, seriesID)
	if len(wants) != 2 {
		t.Fatalf("wanted len = %d after monitoring E1-E2, want 2", len(wants))
	}

	// 4. Run the pipeline: it must satisfy exactly E1 + E2, and ignore E3.
	imported, err := svc.RunPipeline(ctx, seriesID)
	if err != nil {
		t.Fatalf("run pipeline: %v", err)
	}
	if imported != 2 {
		t.Fatalf("imported = %d, want exactly 2 (E1 + E2)", imported)
	}

	// E1 and E2 satisfied...
	for _, ep := range []int{1, 2} {
		w, _ := repo.GetWanted(ctx, seriesID, 1, ep)
		if w == nil {
			t.Fatalf("no wanted row for S1E%02d", ep)
		}
		if w.Status != dom.WantedSatisfied {
			t.Errorf("S1E%02d status = %q, want satisfied", ep, w.Status)
		}
	}
	// ...and E3 has no wanted row at all (never wanted).
	if _, err := repo.GetWanted(ctx, seriesID, 1, 3); err == nil {
		t.Errorf("S1E03 unexpectedly has a wanted row (should stay unmonitored)")
	}

	// The two monitored episode files exist on disk at the documented layout.
	for ep, title := range map[int]string{1: "Pilot", 2: "Over-Looked"} {
		if _, err := os.Stat(episodePath(mediaRoot, "Fringe", 2008, 1, ep, title, ".mkv")); err != nil {
			t.Errorf("imported file S1E%02d not on disk: %v", ep, err)
		}
	}
	// The unmonitored E3 must NOT have been imported (its file is absent).
	if _, err := os.Stat(episodePath(mediaRoot, "Fringe", 2008, 1, 3, "Bright Lights", ".mkv")); err == nil {
		t.Errorf("unmonitored S1E03 was imported; it should have been left alone")
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
	fake.AddRelease(indexers.SearchResult{Title: "Fringe.S01E01.480p.WEB.x264", SizeBytes: 1_000_000_000})

	repo := newMemRepo()
	svc := New(Deps{
		Repo:           repo,
		Indexers:       []indexers.Searcher{fake},
		Client:         mock,
		MediaRoot:      mediaRoot,
		DefaultProfile: "HD-1080p",
	})
	ctx := context.Background()

	seriesID, _ := svc.AddSeries(ctx, "Fringe", "2008", "HD-1080p", true)
	_, _ = svc.AddEpisode(ctx, seriesID, 1, 1, "Pilot", true)

	_, err := svc.RunPipeline(ctx, seriesID)
	if !errors.Is(err, ErrNoMatch) {
		t.Fatalf("run pipeline: err = %v, want ErrNoMatch", err)
	}
	w, _ := repo.GetWanted(ctx, seriesID, 1, 1)
	if w.Status != dom.WantedPending {
		t.Errorf("wanted status = %q, want pending after no-match", w.Status)
	}
	entries, _ := filepath.Glob(filepath.Join(mediaRoot, "*"))
	if len(entries) != 0 {
		t.Errorf("expected nothing imported, found %d entries", len(entries))
	}
}
