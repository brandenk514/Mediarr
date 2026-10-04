package tv

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	dom "github.com/brandenk514/mediarr/internal/domains/tv"
	"github.com/brandenk514/mediarr/internal/downloads"
	"github.com/brandenk514/mediarr/internal/indexers"
)

// These are fast, in-memory unit tests for the Service's individual use-cases
// and private helpers. The end-to-end pipeline is covered by e2e_test.go; this
// file targets the remaining branches (pass-through queries, season packs,
// indexer/client failures, no-client, collisions, timeouts, and the pure
// helpers) so services/tv clears the >=80% coverage bar in PLAN §10.

// errIndexer is a stub indexer whose Search always fails, used to exercise the
// search-fanout error path (a single failing indexer must not fail the search).
type errIndexer struct{}

func (errIndexer) Name() string { return "err" }
func (errIndexer) Search(context.Context, indexers.SearchQuery) ([]indexers.SearchResult, error) {
	return nil, errors.New("indexer boom")
}

// failClient is a stub download client whose Add always fails, used to exercise
// the sendToClient -> Client.Add error path.
type failClient struct{ name string }

func (c failClient) Name() string { return c.name }
func (c failClient) Add(context.Context, downloads.Release) error {
	return errors.New("client add boom")
}
func (c failClient) Status(context.Context, string) (downloads.Status, error) {
	return downloads.Status{}, nil
}

func setupService(t *testing.T) (*Service, *memRepo) {
	t.Helper()
	dlDir := t.TempDir()
	mock, err := downloads.NewMockClient("mock", dlDir)
	if err != nil {
		t.Fatalf("mock client: %v", err)
	}
	fake := indexers.NewFakeIndexer("fake")
	repo := newMemRepo()
	svc := New(Deps{
		Repo:           repo,
		Indexers:       []indexers.Searcher{fake},
		Client:         mock,
		MediaRoot:      t.TempDir(),
		DefaultProfile: "HD-1080p",
	})
	return svc, repo
}

// TestNew_DefaultProfile exercises the default-profile assignment in New.
func TestNew_DefaultProfile(t *testing.T) {
	svc := New(Deps{})
	if svc.deps.DefaultProfile != "HD-1080p" {
		t.Errorf("DefaultProfile = %q, want HD-1080p (default)", svc.deps.DefaultProfile)
	}
}

func TestAddSeries(t *testing.T) {
	svc, repo := setupService(t)
	ctx := context.Background()

	// Blank title is rejected.
	if _, err := svc.AddSeries(ctx, "   ", "2008", ""); err == nil {
		t.Error("expected error for blank title, got nil")
	}

	// Valid add: year parsed, default profile applied, history recorded.
	id, err := svc.AddSeries(ctx, "  Severance  ", "2022", "")
	if err != nil {
		t.Fatalf("add series: %v", err)
	}
	s, _ := repo.GetSeries(ctx, id)
	if s.Title != "Severance" {
		t.Errorf("title = %q, want trimmed 'Severance'", s.Title)
	}
	if s.Year != 2022 {
		t.Errorf("year = %d, want 2022", s.Year)
	}
	if s.QualityProfile != "HD-1080p" {
		t.Errorf("quality profile = %q, want default HD-1080p", s.QualityProfile)
	}
	hist, _ := repo.ListHistory(ctx, id, 0)
	found := false
	for _, h := range hist {
		if h.Event == "added" {
			found = true
		}
	}
	if !found {
		t.Error("expected an 'added' history entry")
	}

	// Invalid year string maps to 0.
	if _, err := svc.AddSeries(ctx, "No Year", "soon", ""); err != nil {
		t.Fatalf("add series (bad year): %v", err)
	}
}

func TestAddEpisode(t *testing.T) {
	svc, repo := setupService(t)
	ctx := context.Background()

	seriesID, _ := svc.AddSeries(ctx, "Severance", "2022", "HD-1080p")

	// Monitored episode creates a pending wanted row.
	if _, err := svc.AddEpisode(ctx, seriesID, 1, 1, "Goodbye", true); err != nil {
		t.Fatalf("add episode (monitored): %v", err)
	}
	w, _ := repo.GetWanted(ctx, seriesID, 1, 1)
	if w.Status != dom.WantedPending {
		t.Errorf("monitored episode wanted status = %q, want pending", w.Status)
	}

	// Non-monitored episode creates no wanted row.
	if _, err := svc.AddEpisode(ctx, seriesID, 1, 2, "Looping", false); err != nil {
		t.Fatalf("add episode (unmonitored): %v", err)
	}
	if _, err := repo.GetWanted(ctx, seriesID, 1, 2); err == nil {
		t.Error("expected no wanted row for unmonitored episode")
	}

	// Creating a duplicate episode surfaces the repo error.
	if _, err := svc.AddEpisode(ctx, seriesID, 1, 1, "x", true); err == nil {
		t.Error("expected error adding a duplicate episode")
	}
}

func TestListAndGetSeries(t *testing.T) {
	svc, _ := setupService(t)
	ctx := context.Background()

	seriesID, _ := svc.AddSeries(ctx, "Severance", "2022", "HD-1080p")
	if _, err := svc.AddSeries(ctx, "Halt", "2020", "HD-1080p"); err != nil {
		t.Fatalf("add second series: %v", err)
	}

	all, err := svc.ListSeries(ctx)
	if err != nil {
		t.Fatalf("list series: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("ListSeries len = %d, want 2", len(all))
	}

	got, err := svc.GetSeries(ctx, seriesID)
	if err != nil {
		t.Fatalf("get series: %v", err)
	}
	if got.Title != "Severance" {
		t.Errorf("GetSeries title = %q, want Severance", got.Title)
	}

	// GetSeries for a missing id surfaces the not-found error.
	if _, err := svc.GetSeries(ctx, 999); err == nil {
		t.Error("expected error for missing series id")
	}
}

func TestListEpisodesAndWanted(t *testing.T) {
	svc, _ := setupService(t)
	ctx := context.Background()

	seriesID, _ := svc.AddSeries(ctx, "Severance", "2022", "HD-1080p")
	_, _ = svc.AddEpisode(ctx, seriesID, 1, 1, "Goodbye", true)
	_, _ = svc.AddEpisode(ctx, seriesID, 1, 2, "Looping", true)

	eps, err := svc.ListEpisodes(ctx, seriesID)
	if err != nil {
		t.Fatalf("list episodes: %v", err)
	}
	if len(eps) != 2 {
		t.Errorf("ListEpisodes len = %d, want 2", len(eps))
	}

	wants, err := svc.ListWanted(ctx, seriesID)
	if err != nil {
		t.Fatalf("list wanted: %v", err)
	}
	if len(wants) != 2 {
		t.Errorf("ListWanted len = %d, want 2", len(wants))
	}
}

func TestSetEpisodeMonitored(t *testing.T) {
	svc, repo := setupService(t)
	ctx := context.Background()

	seriesID, _ := svc.AddSeries(ctx, "Severance", "2022", "HD-1080p")
	_, _ = svc.AddEpisode(ctx, seriesID, 1, 1, "Goodbye", true)
	// The monitored add already created a pending wanted row; mark it satisfied
	// so that re-monitoring must create/refresh a pending row.
	if err := repo.MarkWantedSatisfied(ctx, seriesID, 1, 1, "x"); err != nil {
		t.Fatalf("mark satisfied: %v", err)
	}

	// Turning monitoring on again must ensure a wanted row exists (EnsureWanted
	// is a no-op if one already exists, so the prior status is preserved).
	if err := svc.SetEpisodeMonitored(ctx, seriesID, 1, 1, true); err != nil {
		t.Fatalf("set monitored on: %v", err)
	}
	if _, err := repo.GetWanted(ctx, seriesID, 1, 1); err != nil {
		t.Errorf("expected a wanted row after re-monitoring: %v", err)
	}
	e, _ := repo.GetEpisode(ctx, seriesID, 1, 1)
	if !e.Monitored {
		t.Error("episode should be monitored after set on")
	}

	// Turning monitoring off must not touch wanted and must not error.
	if err := svc.SetEpisodeMonitored(ctx, seriesID, 1, 1, false); err != nil {
		t.Fatalf("set monitored off: %v", err)
	}
	e, _ = repo.GetEpisode(ctx, seriesID, 1, 1)
	if e.Monitored {
		t.Error("episode should be unmonitored after set off")
	}

	// Setting monitoring for a missing episode surfaces the repo error.
	if err := svc.SetEpisodeMonitored(ctx, seriesID, 9, 9, true); err == nil {
		t.Error("expected error for missing episode")
	}
}

func TestRunPipeline_NoClient(t *testing.T) {
	fake := indexers.NewFakeIndexer("fake")
	fake.AddRelease(indexers.SearchResult{Title: "Fringe.S01E01.1080p.WEB.x264", SizeBytes: 1_900_000_000})
	repo := newMemRepo()
	// Client is nil.
	svc := New(Deps{
		Repo:           repo,
		Indexers:       []indexers.Searcher{fake},
		Client:         nil,
		MediaRoot:      t.TempDir(),
		DefaultProfile: "HD-1080p",
	})
	ctx := context.Background()

	seriesID, _ := svc.AddSeries(ctx, "Fringe", "2008", "HD-1080p")
	_, _ = svc.AddEpisode(ctx, seriesID, 1, 1, "Pilot", true)

	if _, err := svc.RunPipeline(ctx, seriesID); err == nil {
		t.Fatal("expected error when no download client is configured")
	}
	// A "no-client" history entry should be recorded.
	hist, _ := repo.ListHistory(ctx, seriesID, 0)
	found := false
	for _, h := range hist {
		if h.Event == "no-client" {
			found = true
		}
	}
	if !found {
		t.Error("expected a 'no-client' history entry")
	}
}

func TestRunPipeline_IndexerFailureDoesNotFailSearch(t *testing.T) {
	dlDir := t.TempDir()
	mock, _ := downloads.NewMockClient("mock", dlDir)
	// One failing indexer and one good indexer returning a 1080p release.
	good := indexers.NewFakeIndexer("good")
	good.AddRelease(indexers.SearchResult{Title: "Fringe.S01E01.1080p.WEB.x264", SizeBytes: 1_900_000_000})
	repo := newMemRepo()
	svc := New(Deps{
		Repo:           repo,
		Indexers:       []indexers.Searcher{errIndexer{}, good},
		Client:         mock,
		MediaRoot:      t.TempDir(),
		DefaultProfile: "HD-1080p",
	})
	ctx := context.Background()

	seriesID, _ := svc.AddSeries(ctx, "Fringe", "2008", "HD-1080p")
	_, _ = svc.AddEpisode(ctx, seriesID, 1, 1, "Pilot", true)

	// The failing indexer is skipped; the good one still yields a match, so the
	// pipeline should import rather than fail.
	imported, err := svc.RunPipeline(ctx, seriesID)
	if err != nil {
		t.Fatalf("run pipeline: %v (expected the good indexer to be used)", err)
	}
	if imported != 1 {
		t.Errorf("imported = %d, want 1", imported)
	}
}

func TestRunPipeline_ClientAddFails(t *testing.T) {
	dlDir := t.TempDir()
	_ = dlDir
	fake := indexers.NewFakeIndexer("fake")
	fake.AddRelease(indexers.SearchResult{Title: "Fringe.S01E01.1080p.WEB.x264", SizeBytes: 1_900_000_000})
	repo := newMemRepo()
	svc := New(Deps{
		Repo:           repo,
		Indexers:       []indexers.Searcher{fake},
		Client:         failClient{name: "fail"},
		MediaRoot:      t.TempDir(),
		DefaultProfile: "HD-1080p",
	})
	ctx := context.Background()

	seriesID, _ := svc.AddSeries(ctx, "Fringe", "2008", "HD-1080p")
	_, _ = svc.AddEpisode(ctx, seriesID, 1, 1, "Pilot", true)

	// Client.Add fails -> download-failed recorded, nothing imported, no match.
	imported, err := svc.RunPipeline(ctx, seriesID)
	if !errors.Is(err, ErrNoMatch) {
		t.Fatalf("run pipeline: err = %v, want ErrNoMatch (download failed)", err)
	}
	if imported != 0 {
		t.Errorf("imported = %d, want 0", imported)
	}
	w, _ := repo.GetWanted(ctx, seriesID, 1, 1)
	if w.Status != dom.WantedPending {
		t.Errorf("wanted status = %q, want pending (download failed)", w.Status)
	}
}

func TestCoveredWanted_SeasonPack(t *testing.T) {
	parsed := dom.ParsedRelease{Title: "S", Season: 2, SeasonPack: true}
	pending := []dom.Wanted{
		{SeriesID: 1, Season: 2, Episode: 1, Status: dom.WantedPending},
		{SeriesID: 1, Season: 2, Episode: 3, Status: dom.WantedPending},
		{SeriesID: 1, Season: 1, Episode: 1, Status: dom.WantedPending},
	}
	got := coveredWanted(parsed, pending)
	if len(got) != 2 {
		t.Fatalf("coveredWanted(season pack) = %d, want 2 (both S2 episodes)", len(got))
	}
	for _, w := range got {
		if w.Season != 2 {
			t.Errorf("covered episode is S%02dE%02d, want season 2 only", w.Season, w.Episode)
		}
	}
}

func TestCoveredWanted_DiscreteAndNone(t *testing.T) {
	// Discrete: only the listed (season, episode) is covered.
	parsed := dom.ParsedRelease{Title: "S", Season: 1, Episodes: []int{2}}
	pending := []dom.Wanted{
		{SeriesID: 1, Season: 1, Episode: 1, Status: dom.WantedPending},
		{SeriesID: 1, Season: 1, Episode: 2, Status: dom.WantedPending},
	}
	got := coveredWanted(parsed, pending)
	if len(got) != 1 || got[0].Episode != 2 {
		t.Fatalf("coveredWanted(discrete) = %+v, want exactly S01E02", got)
	}

	// Neither discrete nor a season pack -> nothing covered.
	none := dom.ParsedRelease{Title: "S", Season: 1}
	if got := coveredWanted(none, pending); got != nil {
		t.Errorf("coveredWanted(neither) = %+v, want nil", got)
	}
}

func TestFileNameFor(t *testing.T) {
	cases := []struct {
		name   string
		parsed dom.ParsedRelease
		series dom.Series
		want   string
	}{
		{
			name:   "series title, single episode",
			parsed: dom.ParsedRelease{Season: 1},
			series: dom.Series{Title: "Breaking Bad"},
			want:   "breaking bad S01.mkv",
		},
		{
			name:   "fall back to parsed title when series title empty",
			parsed: dom.ParsedRelease{Title: "Fallback.Release", Season: 2},
			series: dom.Series{},
			want:   "fallback release S02.mkv",
		},
		{
			name:   "no series, no parsed title, no season",
			parsed: dom.ParsedRelease{},
			series: dom.Series{},
			want:   "release.mkv",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := fileNameFor(tc.parsed, &tc.series); got != tc.want {
				t.Errorf("fileNameFor = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseYear(t *testing.T) {
	cases := map[string]int{
		"2008":   2008,
		"  2008": 2008,
		"":       0,
		"soon":   0,
		"20xx":   20, // Sscanf stops at the first non-digit
	}
	for in, want := range cases {
		if got := parseYear(in); got != want {
			t.Errorf("parseYear(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestCopyFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.mkv")
	if err := os.WriteFile(src, []byte("data"), 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}
	// Dest is in a not-yet-created subdirectory; copyFile must create it.
	dst := filepath.Join(dir, "a", "b", "dst.mkv")
	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if string(got) != "data" {
		t.Errorf("dest content = %q, want 'data'", string(got))
	}
}

// TestWaitComplete_Timeout uses a client that reports never-complete to drive
// the deadline path in waitComplete. The 30s deadline is long; we instead use
// context cancellation (below) for a quick failure, and here just assert the
// function returns promptly on a cancelled context rather than blocking.
func TestWaitComplete_CtxCancel(t *testing.T) {
	dlDir := t.TempDir()
	mock, _ := downloads.NewMockClient("mock", dlDir)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately
	svc := New(Deps{Repo: newMemRepo(), Client: mock, MediaRoot: t.TempDir()})
	if _, err := svc.waitComplete(ctx, "never"); err == nil {
		t.Fatal("expected error from waitComplete on cancelled context")
	}
}

// TestWaitComplete_NeverComplete verifies the timeout branch is reached when the
// client never reports completion. We bound it with a short context deadline so
// the test fails fast if the loop stops honoring the context.
func TestWaitComplete_NeverComplete(t *testing.T) {
	// A client that always reports not-complete.
	never := neverCompleteClient{}
	svc := New(Deps{Repo: newMemRepo(), Client: never, MediaRoot: t.TempDir()})
	ctx, cancel := context.WithTimeout(context.Background(), 500*1000*1000) // 0.5s
	defer cancel()
	if _, err := svc.waitComplete(ctx, "never"); err == nil {
		t.Fatal("expected error from waitComplete when download never completes")
	}
}

// neverCompleteClient always reports not-complete, for the waitComplete
// timeout/cancel path.
type neverCompleteClient struct{}

func (neverCompleteClient) Name() string                                 { return "never" }
func (neverCompleteClient) Add(context.Context, downloads.Release) error { return nil }
func (neverCompleteClient) Status(context.Context, string) (downloads.Status, error) {
	return downloads.Status{Complete: false, Progress: 0}, nil
}
