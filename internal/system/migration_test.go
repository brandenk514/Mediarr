package system

import (
	"context"
	"fmt"
	"testing"
)

// ---- mocked Sonarr API (the swappable source seam) ----

type fakeSonarr struct {
	series   []SonarrSeries
	episodes []SonarrEpisode
	profiles []SonarrQualityProfile
	tags     []SonarrTag
	indexers []SonarrIndexer
	dcs      []SonarrDownloadClient
}

func (f *fakeSonarr) GetSeries(context.Context) ([]SonarrSeries, error)    { return f.series, nil }
func (f *fakeSonarr) GetEpisodes(context.Context) ([]SonarrEpisode, error) { return f.episodes, nil }
func (f *fakeSonarr) GetQualityProfiles(context.Context) ([]SonarrQualityProfile, error) {
	return f.profiles, nil
}
func (f *fakeSonarr) GetTags(context.Context) ([]SonarrTag, error)         { return f.tags, nil }
func (f *fakeSonarr) GetIndexers(context.Context) ([]SonarrIndexer, error) { return f.indexers, nil }
func (f *fakeSonarr) GetDownloadClients(context.Context) ([]SonarrDownloadClient, error) {
	return f.dcs, nil
}

func sampleSonarr() *fakeSonarr {
	return &fakeSonarr{
		profiles: []SonarrQualityProfile{{ID: 1, Name: "Default", Cutoff: 6, Items: []SonarrProfileItem{{Quality: SonarrQuality{ID: 6, Name: "1080p"}, Approved: true}}}},
		tags:     []SonarrTag{{ID: 1, Label: "4k"}, {ID: 2, Label: "kids"}},
		indexers: []SonarrIndexer{{ID: 1, Name: "RAR", Enable: true, EnableAutomatic: true, IndexerType: "indexer", APIKey: "k", BaseURL: "https://rar.example"}},
		dcs:      []SonarrDownloadClient{{ID: 1, Name: "qbit", Enable: true, ClientKey: "qBittorrent"}},
		series: []SonarrSeries{
			{ID: 10, SeriesID: 100, Title: "Show A", Year: 2020, Status: "continuing", QualityProfileID: 1, Monitored: true, Tags: []int{1}},
			{ID: 11, SeriesID: 200, Title: "Show B", Year: 2021, Status: "ended", QualityProfileID: 1, Monitored: true, Tags: []int{2}},
		},
		episodes: []SonarrEpisode{
			{ID: 1001, SeriesID: 100, SeasonNumber: 1, EpisodeNumber: 1, Title: "Pilot"},
			{ID: 1002, SeriesID: 100, SeasonNumber: 1, EpisodeNumber: 2, Title: "Two"},
			{ID: 2001, SeriesID: 200, SeasonNumber: 1, EpisodeNumber: 1, Title: "B1"},
		},
	}
}

// ---- fake Mediarr import target ----

type fakeTarget struct {
	profiles   map[string]int64
	tags       map[string]int64
	indexers   map[string]int64
	dcSet      bool
	series     map[string]int64
	episodes   map[int64]map[int]bool // seriesID -> episodeID
	nextID     int64
	failSeries int // if >0, EnsureSeries fails once this many series have been created
}

func newFakeTarget() *fakeTarget {
	return &fakeTarget{
		profiles: map[string]int64{}, tags: map[string]int64{}, indexers: map[string]int64{},
		series: map[string]int64{}, episodes: map[int64]map[int]bool{}, nextID: 1,
	}
}

func (f *fakeTarget) next() int64 { id := f.nextID; f.nextID++; return id }

func (f *fakeTarget) EnsureQualityProfile(ctx context.Context, p SonarrQualityProfile) (int64, TargetAction, error) {
	if id, ok := f.profiles[p.Name]; ok {
		return id, ActionExists, nil
	}
	id := f.next()
	f.profiles[p.Name] = id
	return id, ActionCreated, nil
}
func (f *fakeTarget) EnsureTag(ctx context.Context, t SonarrTag) (int64, TargetAction, error) {
	if id, ok := f.tags[t.Label]; ok {
		return id, ActionExists, nil
	}
	id := f.next()
	f.tags[t.Label] = id
	return id, ActionCreated, nil
}
func (f *fakeTarget) EnsureIndexer(ctx context.Context, ix SonarrIndexer) (int64, TargetAction, error) {
	if id, ok := f.indexers[ix.Name]; ok {
		return id, ActionExists, nil
	}
	id := f.next()
	f.indexers[ix.Name] = id
	return id, ActionCreated, nil
}
func (f *fakeTarget) SetDownloadClient(ctx context.Context, c SonarrDownloadClient) (TargetAction, error) {
	if f.dcSet {
		return ActionExists, nil
	}
	f.dcSet = true
	return ActionCreated, nil
}
func (f *fakeTarget) EnsureSeries(ctx context.Context, s SonarrSeries) (int64, TargetAction, error) {
	key := seriesKey(s)
	if id, ok := f.series[key]; ok {
		return id, ActionExists, nil
	}
	if f.failSeries > 0 && len(f.series) >= f.failSeries {
		return 0, "", fmt.Errorf("target: simulated failure on series %q", s.Title)
	}
	id := f.next()
	f.series[key] = id
	f.episodes[id] = map[int]bool{}
	return id, ActionCreated, nil
}
func (f *fakeTarget) EnsureEpisodes(ctx context.Context, seriesID int64, eps []SonarrEpisode) (int, error) {
	m, ok := f.episodes[seriesID]
	if !ok {
		m = map[int]bool{}
		f.episodes[seriesID] = m
	}
	created := 0
	for _, e := range eps {
		if !m[e.ID] {
			m[e.ID] = true
			created++
		}
	}
	return created, nil
}

// ---- tests ----

func TestMigrationDryRunReportsWithoutWriting(t *testing.T) {
	src := sampleSonarr()
	target := newFakeTarget()
	m := &Migrator{NewSource: func(b, k string) SonarrClient { return src }, Target: target}

	res, err := m.Run(context.Background(), MigrationRequest{SourceURL: "http://sonarr.local:8989", DryRun: true})
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	// Nothing written to the target.
	if len(target.series) != 0 || len(target.profiles) != 0 || len(target.tags) != 0 || len(target.indexers) != 0 || target.dcSet {
		t.Fatalf("dry-run must not write to the target: series=%d profiles=%d tags=%d indexers=%d dc=%v",
			len(target.series), len(target.profiles), len(target.tags), len(target.indexers), target.dcSet)
	}
	// Every item is a dry-run action.
	if len(res.Items) == 0 {
		t.Fatal("expected mapping items")
	}
	for _, it := range res.Items {
		if it.Action != "dry-run" {
			t.Errorf("item %s/%s action = %q, want dry-run", it.Entity, it.ExternalID, it.Action)
		}
	}
	// Counts: 1 profile + 2 tags + 1 indexer + 1 dc + 2 series + 3 episodes = 10.
	if res.Counts.Total != 10 {
		t.Errorf("total = %d, want 10", res.Counts.Total)
	}
	if res.DryRun != true {
		t.Error("expected dry_run=true")
	}
	// The mapping report covers all six entity classes.
	entities := map[string]bool{}
	for _, it := range res.Items {
		entities[it.Entity] = true
	}
	for _, want := range []string{"quality_profile", "tag", "indexer", "download_client", "series", "episode"} {
		if !entities[want] {
			t.Errorf("mapping report missing entity class %q", want)
		}
	}
}

func TestMigrationIdempotentNoDupes(t *testing.T) {
	src := sampleSonarr()
	target := newFakeTarget()
	m := &Migrator{NewSource: func(b, k string) SonarrClient { return src }, Target: target}
	req := MigrationRequest{SourceURL: "http://sonarr.local:8989"}

	// First run: everything created.
	res1, err := m.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	firstCreated := res1.Counts.Created
	if firstCreated == 0 {
		t.Fatal("expected created > 0 on first run")
	}
	seriesCountAfter1 := len(target.series)
	profileCountAfter1 := len(target.profiles)

	// Second run (same source, same target): everything exists, nothing new.
	res2, err := m.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if res2.Counts.Created != 0 {
		t.Errorf("second run created = %d, want 0 (idempotent)", res2.Counts.Created)
	}
	if res2.Counts.Existing != firstCreated {
		t.Errorf("second run existing = %d, want %d (all from first run)", res2.Counts.Existing, firstCreated)
	}
	if len(target.series) != seriesCountAfter1 {
		t.Errorf("series count changed: %d -> %d (dupes)", seriesCountAfter1, len(target.series))
	}
	if len(target.profiles) != profileCountAfter1 {
		t.Errorf("profile count changed: %d -> %d (dupes)", profileCountAfter1, len(target.profiles))
	}
	// Spot-check: a series item is "exists" on the second run.
	for _, it := range res2.Items {
		if it.Entity == "series" && it.Action != "exists" {
			t.Errorf("series %s action = %q, want exists", it.ExternalID, it.Action)
		}
	}
}

func TestMigrationResumablePartialThenResume(t *testing.T) {
	src := sampleSonarr()
	target := newFakeTarget()
	target.failSeries = 1 // fail after the first series is created

	m := &Migrator{NewSource: func(b, k string) SonarrClient { return src }, Target: target}
	req := MigrationRequest{SourceURL: "http://sonarr.local:8989"}

	// First run: partial (series 1 created, series 2 fails).
	res1, err := m.Run(context.Background(), req)
	if err == nil {
		t.Fatal("expected an error from the partial run")
	}
	if res1 == nil || !res1.Resumable {
		t.Fatalf("expected a resumable partial result, got %+v", res1)
	}
	st := m.Status()
	if st.Status != "partial" {
		t.Errorf("status = %q, want partial", st.Status)
	}
	if len(target.series) != 1 {
		t.Fatalf("expected 1 series created before failure, got %d", len(target.series))
	}

	// Clear the failure and resume: series 1 is skipped, series 2 is created.
	target.failSeries = 0
	res2, err := m.Run(context.Background(), MigrationRequest{SourceURL: "http://sonarr.local:8989", Resume: true})
	if err != nil {
		t.Fatalf("resume run: %v", err)
	}
	if res2 == nil {
		t.Fatal("resume returned no result")
	}
	// One series skipped (already migrated), one created.
	skipped, created := 0, 0
	for _, it := range res2.Items {
		if it.Entity != "series" {
			continue
		}
		switch it.Action {
		case "skipped":
			skipped++
		case "created":
			created++
		}
	}
	if skipped != 1 || created != 1 {
		t.Errorf("resume: skipped=%d created=%d, want 1/1", skipped, created)
	}
	if len(target.series) != 2 {
		t.Errorf("after resume, series = %d, want 2", len(target.series))
	}
	if m.Status().Status != "completed" {
		t.Errorf("final status = %q, want completed", m.Status().Status)
	}
}

func TestMigrationRequiresValidSourceURL(t *testing.T) {
	m := &Migrator{NewSource: func(b, k string) SonarrClient { return &fakeSonarr{} }, Target: newFakeTarget()}
	if _, err := m.Run(context.Background(), MigrationRequest{SourceURL: ""}); err == nil {
		t.Error("expected error for empty source_url")
	}
	if _, err := m.Run(context.Background(), MigrationRequest{SourceURL: "not a url"}); err == nil {
		t.Error("expected error for non-http source_url")
	}
}

func TestMigrationStatusTracksProgressAndLastResult(t *testing.T) {
	src := sampleSonarr()
	target := newFakeTarget()
	m := &Migrator{NewSource: func(b, k string) SonarrClient { return src }, Target: target}
	if _, err := m.Run(context.Background(), MigrationRequest{SourceURL: "http://sonarr.local:8989"}); err != nil {
		t.Fatalf("run: %v", err)
	}
	st := m.Status()
	if st.Status != "completed" || st.Running {
		t.Errorf("status = %+v, want completed/not running", st)
	}
	if st.Total != 2 || st.Done != 2 {
		t.Errorf("progress = %d/%d, want 2/2", st.Done, st.Total)
	}
	if len(st.LastResult) == 0 {
		t.Error("expected a last_result to be stored")
	}
}
