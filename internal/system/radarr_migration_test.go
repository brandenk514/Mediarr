package system

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---- mocked Radarr v3 API (the swappable source seam) ----

type fakeRadarr struct {
	movies   []RadarrMovie
	profiles []RadarrQualityProfile
	tags     []RadarrTag
	indexers []RadarrIndexer
	dcs      []RadarrDownloadClient
}

func (f *fakeRadarr) GetMovies(context.Context) ([]RadarrMovie, error) { return f.movies, nil }
func (f *fakeRadarr) GetQualityProfiles(context.Context) ([]RadarrQualityProfile, error) {
	return f.profiles, nil
}
func (f *fakeRadarr) GetTags(context.Context) ([]RadarrTag, error)         { return f.tags, nil }
func (f *fakeRadarr) GetIndexers(context.Context) ([]RadarrIndexer, error) { return f.indexers, nil }
func (f *fakeRadarr) GetDownloadClients(context.Context) ([]RadarrDownloadClient, error) {
	return f.dcs, nil
}
func (f *fakeRadarr) GetMediaManagement(context.Context) (*RadarrMediaManagement, error) {
	return &RadarrMediaManagement{}, nil
}
func (f *fakeRadarr) GetImportExport(context.Context) (*RadarrImportExport, error) {
	return nil, nil
}

func sampleRadarr() *fakeRadarr {
	return &fakeRadarr{
		profiles: []RadarrQualityProfile{
			{ID: 1, Name: "4K Best Effort", Cutoff: 1, Items: []RadarrProfileItem{
				{Quality: RadarrQuality{ID: 1, Name: "2160p WEB"}, Approved: true},
				{Quality: RadarrQuality{ID: 2, Name: "1080p WEB"}, Approved: true},
			}},
			{ID: 2, Name: "1080p", Cutoff: 2, Items: []RadarrProfileItem{
				{Quality: RadarrQuality{ID: 2, Name: "1080p WEB"}, Approved: true},
			}},
		},
		tags: []RadarrTag{{ID: 1, Label: "4k"}, {ID: 2, Label: "kids"}},
		movies: []RadarrMovie{
			{
				ID: 10, Title: "Movie A", OriginalTitle: "Movie A", SortTitle: "movie a",
				Year: 2022, Monitored: true, QualityProfileID: 1,
				Tags: []int{1}, Status: "released",
				Editions: []RadarrEdition{
					{ID: 100, Label: "Theatrical", Monitored: true, QualityProfileID: 1, Size: 1 << 30},
					{ID: 101, Label: "IMAX", Monitored: false, QualityProfileID: 2, Size: 2 << 30},
				},
			},
			{
				ID: 11, Title: "Movie B", Year: 2021, Monitored: false, QualityProfileID: 2,
				Editions: []RadarrEdition{
					{ID: 200, Label: "Theatrical", Monitored: false, QualityProfileID: 2},
				},
			},
		},
	}
}

// ---- fake Mediarr movie import target ----

type fakeRadarrTarget struct {
	movieProfiles   map[string]int64
	createdProfiles map[string]bool
	tags            map[int]int64 // source id -> target id
	movies          map[string]int64
	editions        map[int64]map[string]bool // movieID -> labels
	seededMonitored map[int64]bool
	nextID          int64
}

func newFakeRadarrTarget() *fakeRadarrTarget {
	return &fakeRadarrTarget{
		movieProfiles:   map[string]int64{},
		createdProfiles: map[string]bool{},
		tags:            map[int]int64{},
		movies:          map[string]int64{},
		editions:        map[int64]map[string]bool{},
		seededMonitored: map[int64]bool{},
		nextID:          1,
	}
}

func (f *fakeRadarrTarget) next() int64 { id := f.nextID; f.nextID++; return id }

func (f *fakeRadarrTarget) EnsureMovieProfile(ctx context.Context, p RadarrQualityProfile) (int64, TargetAction, error) {
	if id, ok := f.movieProfiles[p.Name]; ok {
		return id, ActionExists, nil
	}
	id := f.next()
	f.movieProfiles[p.Name] = id
	f.createdProfiles[p.Name] = true
	return id, ActionCreated, nil
}
func (f *fakeRadarrTarget) EnsureTag(ctx context.Context, t RadarrTag) (int64, TargetAction, error) {
	if id, ok := f.tags[t.ID]; ok {
		return id, ActionExists, nil
	}
	id := f.next()
	f.tags[t.ID] = id
	return id, ActionCreated, nil
}
func (f *fakeRadarrTarget) TagLabels(ctx context.Context, ids []int) ([]string, error) {
	// The fake target does not keep the label alongside the id; resolve the
	// way the real target does would need the label — the migrator test
	// fixtures only assert counts, not label contents.
	out := make([]string, 0, len(ids))
	for range ids {
		out = append(out, "label")
	}
	return out, nil
}
func (f *fakeRadarrTarget) EnsureMovie(ctx context.Context, ref RadarrMovieRef) (int64, TargetAction, error) {
	key := fmt.Sprintf("movie:%s|%d", ref.Title, ref.Year)
	if id, ok := f.movies[key]; ok {
		return id, ActionExists, nil
	}
	id := f.next()
	f.movies[key] = id
	f.editions[id] = map[string]bool{}
	f.seededMonitored[id] = ref.Monitored
	return id, ActionCreated, nil
}
func (f *fakeRadarrTarget) EnsureEditions(ctx context.Context, movieID int64, eds []RadarrEditionRef) (int, error) {
	m, ok := f.editions[movieID]
	if !ok {
		m = map[string]bool{}
		f.editions[movieID] = m
	}
	created := 0
	for _, e := range eds {
		if !m[e.Label] {
			m[e.Label] = true
			created++
		}
	}
	return created, nil
}

var _ RadarrMigrationTarget = (*fakeRadarrTarget)(nil)
var _ RadarrTagStore = (*fakeRadarrTarget)(nil)

// ---- W1 tests: profiles / tags / movies / editions ----

func TestRadarrMigrationDryRunReportsWithoutWriting(t *testing.T) {
	src := sampleRadarr()
	target := newFakeRadarrTarget()
	m := &RadarrMigrator{NewSource: func(b, k string) RadarrClient { return src }, Target: target}

	res, err := m.Run(context.Background(), MigrationRequest{SourceURL: "https://radarr.local:7878", DryRun: true, Source: SourceRadarr})
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	// Nothing written to the target.
	if len(target.movies) != 0 || len(target.movieProfiles) != 0 || len(target.tags) != 0 {
		t.Fatalf("dry-run must not write to the target: movies=%d profiles=%d tags=%d",
			len(target.movies), len(target.movieProfiles), len(target.tags))
	}
	// Every item is a dry-run action; all four entity classes present.
	if len(res.Items) == 0 {
		t.Fatal("expected mapping items")
	}
	entities := map[string]bool{}
	for _, it := range res.Items {
		if it.Action != "dry-run" {
			t.Errorf("item %s/%s action = %q, want dry-run", it.Entity, it.ExternalID, it.Action)
		}
		entities[it.Entity] = true
	}
	for _, want := range []string{"quality_profile", "tag", "movie", "edition"} {
		if !entities[want] {
			t.Errorf("mapping report missing entity class %q", want)
		}
	}
	// Counts: 2 profiles + 2 tags + 2 movies + 3 editions = 9.
	if res.Counts.Total != 9 {
		t.Errorf("total = %d, want 9", res.Counts.Total)
	}
	if !res.DryRun {
		t.Error("expected dry_run=true")
	}
}

func TestRadarrMigrationHappyPath(t *testing.T) {
	src := sampleRadarr()
	target := newFakeRadarrTarget()
	m := &RadarrMigrator{NewSource: func(b, k string) RadarrClient { return src }, Target: target}

	res, err := m.Run(context.Background(), MigrationRequest{SourceURL: "https://radarr.local:7878", Source: SourceRadarr})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	// Profiles: both source profiles created in the target.
	if len(target.movieProfiles) != 2 {
		t.Errorf("profiles = %d, want 2", len(target.movieProfiles))
	}
	// Tags: both source tags imported.
	if len(target.tags) != 2 {
		t.Errorf("tags = %d, want 2", len(target.tags))
	}
	// Movies: both movies created (title+year keying).
	if len(target.movies) != 2 {
		t.Fatalf("movies = %d, want 2: %v", len(target.movies), target.movies)
	}
	// Editions: Movie A has two, Movie B one.
	if len(target.editions) != 2 {
		t.Fatalf("editions for %d movies, want 2", len(target.editions))
	}
	var movieA, movieB int64
	for k, id := range target.movies {
		if strings.Contains(k, "Movie A") {
			movieA = id
		}
		if strings.Contains(k, "Movie B") {
			movieB = id
		}
	}
	if len(target.editions[movieA]) != 2 {
		t.Errorf("movie A editions = %d, want 2", len(target.editions[movieA]))
	}
	if len(target.editions[movieB]) != 1 {
		t.Errorf("movie B editions = %d, want 1", len(target.editions[movieB]))
	}
	// Report counts: 2 profiles + 2 tags + 2 movies + 3 editions = 9, all created.
	if res.Counts.Total != 9 {
		t.Errorf("total = %d, want 9", res.Counts.Total)
	}
	if res.Counts.Created != 9 {
		t.Errorf("created = %d, want 9", res.Counts.Created)
	}
	// Status reflects the run.
	if st := m.Status(); st.Status != "completed" || st.Total != 2 || st.Done != 2 {
		t.Errorf("status = %+v, want completed 2/2", st)
	}
}

func TestRadarrMigrationUnknownProfileCreatedAndNoted(t *testing.T) {
	src := sampleRadarr()
	src.profiles = []RadarrQualityProfile{{ID: 9, Name: "My Custom 8K", Cutoff: 1, Items: nil}}
	src.movies[0].QualityProfileID = 9
	target := newFakeRadarrTarget()
	m := &RadarrMigrator{NewSource: func(b, k string) RadarrClient { return src }, Target: target}

	res, err := m.Run(context.Background(), MigrationRequest{SourceURL: "https://radarr.local:7878", Source: SourceRadarr})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	// The unknown source profile is created in the target (document).
	if !target.createdProfiles["My Custom 8K"] {
		t.Error("unknown source profile not created in the target")
	}
	// And the mapping report notes it.
	noted := false
	for _, it := range res.Items {
		if it.Entity == "quality_profile" && strings.Contains(it.Message, "created") {
			noted = true
		}
	}
	if !noted {
		t.Errorf("expected a 'created in the document' note in the report: %+v", res.Items)
	}
	// A movie referencing a profile id absent from the source list falls
	// back to the target default (with a note), and still imports.
	src2 := sampleRadarr()
	src2.movies[0].QualityProfileID = 4242
	target2 := newFakeRadarrTarget()
	m2 := &RadarrMigrator{NewSource: func(b, k string) RadarrClient { return src2 }, Target: target2}
	res2, err := m2.Run(context.Background(), MigrationRequest{SourceURL: "https://radarr.local:7878", Source: SourceRadarr})
	if err != nil {
		t.Fatalf("fallback run: %v", err)
	}
	if len(target2.movies) != 2 {
		t.Errorf("fallback: movies = %d, want 2", len(target2.movies))
	}
	noteFound := false
	for _, it := range res2.Items {
		if it.Entity == "movie" && strings.Contains(it.Message, "not in source profiles") {
			noteFound = true
		}
	}
	if !noteFound {
		t.Error("expected a profile-fallback note on the movie item")
	}
}

func TestRadarrMigrationSourceFailure(t *testing.T) {
	// A source 404/500 surfaces as a run error carrying the reason (the API
	// maps it to a 502 with the source name).
	for status, body := range map[int]string{
		http.StatusNotFound:            `{"error":"not found"}`,
		http.StatusInternalServerError: `{"error":"boom"}`,
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Api-Key") != "secret" {
				t.Errorf("missing X-Api-Key header")
			}
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		}))
		m := NewRadarrMigrator(newFakeRadarrTarget()) // default HTTP source factory
		_, err := m.Run(context.Background(), MigrationRequest{SourceURL: srv.URL, Source: SourceRadarr, APIKey: "secret"})
		srv.Close()
		if err == nil {
			t.Fatalf("status %d: expected an error", status)
		}
		if !strings.Contains(err.Error(), fmt.Sprintf("returned %d", status)) {
			t.Errorf("status %d: error %q should carry the HTTP reason", status, err.Error())
		}
	}
}

func TestRadarrMigrationRequiresValidSourceURL(t *testing.T) {
	m := &RadarrMigrator{NewSource: func(b, k string) RadarrClient { return &fakeRadarr{} }, Target: newFakeRadarrTarget()}
	if _, err := m.Run(context.Background(), MigrationRequest{SourceURL: ""}); err == nil {
		t.Error("expected error for empty source_url")
	}
	if _, err := m.Run(context.Background(), MigrationRequest{SourceURL: "not a url"}); err == nil {
		t.Error("expected error for non-http source_url")
	}
}

// ---- MigrationService routing (the R6 source toggle) ----

type recordingRunner struct {
	name   string
	result *MigrationResult
	err    error
}

func (r *recordingRunner) Run(ctx context.Context, req MigrationRequest) (*MigrationResult, error) {
	r.result = &MigrationResult{SourceURL: req.SourceURL}
	return r.result, r.err
}
func (r *recordingRunner) Status() MigrationStatus { return MigrationStatus{Status: "completed"} }

func TestMigrationServiceRoutesBySource(t *testing.T) {
	sonarr := &recordingRunner{name: "sonarr"}
	radarr := &recordingRunner{name: "radarr"}
	svc := NewMigrationService(sonarr, radarr)

	// Explicit radarr routes to the Radarr runner.
	if _, err := svc.Run(context.Background(), MigrationRequest{SourceURL: "http://r", Source: "radarr"}); err != nil {
		t.Fatalf("radarr run: %v", err)
	}
	if radarr.result == nil {
		t.Fatal("radarr runner not invoked")
	}
	// Empty source defaults to Sonarr (existing Sonarr requests unchanged).
	if _, err := svc.Run(context.Background(), MigrationRequest{SourceURL: "http://s"}); err != nil {
		t.Fatalf("sonarr run: %v", err)
	}
	if sonarr.result == nil {
		t.Fatal("sonarr runner not invoked")
	}
	// An unknown source is a validation error (400 at the API).
	_, err := svc.Run(context.Background(), MigrationRequest{SourceURL: "http://x", Source: "lidarr"})
	if err == nil {
		t.Fatal("expected an error for an unknown source")
	}
	if !strings.HasPrefix(err.Error(), "migration:") {
		t.Errorf("unknown-source error %q should be a validation error", err.Error())
	}
	// A nil runner answers a validation error, not a panic.
	onlySonarr := NewMigrationService(sonarr, nil)
	if _, err := onlySonarr.Run(context.Background(), MigrationRequest{SourceURL: "http://r", Source: "radarr"}); err == nil {
		t.Fatal("expected an error for an unwired source")
	}
}
