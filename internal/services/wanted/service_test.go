package wanted

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	dom "github.com/brandenk514/mediarr/internal/domains/wanted"
	"github.com/brandenk514/mediarr/internal/metawrite"
)

type sxe struct {
	s  int64
	se int
	ep int
}

// fakeRepo implements wanted.Repo in memory for service tests.
type fakeRepo struct {
	rows        []dom.EpisodeRow
	profiles    map[int64]string
	wanted      map[sxe]string // (series, season, episode) -> status
	epMonitored map[sxe]bool
	series      map[int64]*dom.SeriesRef
	history     []histEvent
	satisfied   []satisfyCall
	err         error
	// L4 music
	musicRows   []dom.MusicRow
	musicWanted []musRef
	musicOff    map[string]bool // "album/disc/number" -> monitored
}

type histEvent struct {
	kind     string
	parentID int64
	event    string
	detail   string
}

type satisfyCall struct {
	seriesID int64
	season   int
	episode  int
	release  string
	quality  string
}

func (f *fakeRepo) EpisodeRows(_ context.Context) ([]dom.EpisodeRow, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.rows, nil
}

func (f *fakeRepo) SeriesProfiles(_ context.Context) (map[int64]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.profiles, nil
}

func (f *fakeRepo) EnsureWantedPending(_ context.Context, seriesID int64, season, episode int) error {
	if f.err != nil {
		return f.err
	}
	if f.wanted == nil {
		f.wanted = map[sxe]string{}
	}
	f.wanted[sxe{seriesID, season, episode}] = "pending"
	return nil
}

func (f *fakeRepo) SetEpisodeMonitoredOff(_ context.Context, seriesID int64, season, episode int) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	k := sxe{seriesID, season, episode}
	if f.epMonitored == nil {
		return false, nil
	}
	if ok, exists := f.epMonitored[k]; exists && ok {
		f.epMonitored[k] = false
		return true, nil
	}
	return false, nil
}

func (f *fakeRepo) GetSeries(_ context.Context, id int64) (*dom.SeriesRef, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.series[id], nil
}

func (f *fakeRepo) GetEpisode(_ context.Context, seriesID int64, season, episode int) (*dom.EpisodeRef, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.epMonitored == nil {
		return nil, nil
	}
	if _, exists := f.epMonitored[sxe{seriesID, season, episode}]; exists {
		return &dom.EpisodeRef{SeriesID: seriesID, Season: season, Episode: episode}, nil
	}
	return nil, nil
}

func (f *fakeRepo) AddHistory(_ context.Context, kind string, parentID int64, event, detail string) error {
	if f.err != nil {
		return f.err
	}
	f.history = append(f.history, histEvent{kind, parentID, event, detail})
	return nil
}

func (f *fakeRepo) SatisfyWanted(_ context.Context, seriesID int64, season, episode int, releaseTitle, quality string) error {
	if f.err != nil {
		return f.err
	}
	f.satisfied = append(f.satisfied, satisfyCall{seriesID, season, episode, releaseTitle, quality})
	return nil
}

// --- L4 music ---

type musRef struct {
	albumID int64
	disc    int
	number  int
}

func (f *fakeRepo) MusicRows(_ context.Context) ([]dom.MusicRow, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.musicRows, nil
}

func (f *fakeRepo) EnsureMusicWanted(_ context.Context, albumID int64, disc, number int) error {
	if f.err != nil {
		return f.err
	}
	f.musicWanted = append(f.musicWanted, musRef{albumID, disc, number})
	return nil
}

func (f *fakeRepo) SetMusicMonitoredOff(_ context.Context, albumID int64, disc, number int) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	k := fmt.Sprintf("%d/%d/%d", albumID, disc, number)
	if f.musicOff == nil {
		return false, nil
	}
	if ok, exists := f.musicOff[k]; exists && ok {
		f.musicOff[k] = false
		return true, nil
	}
	return false, nil
}

func (f *fakeRepo) AddMusicHistory(_ context.Context, artistID int64, event, detail string) error {
	if f.err != nil {
		return f.err
	}
	f.history = append(f.history, histEvent{"music", artistID, event, detail})
	return nil
}

func baseRows() []dom.EpisodeRow {
	return []dom.EpisodeRow{
		{SeriesID: 1, SeriesTitle: "Alpha", Season: 1, Episode: 1, EpisodeTitle: "Pilot", Monitored: true, HasFile: false},
		{SeriesID: 1, SeriesTitle: "Alpha", Season: 1, Episode: 2, EpisodeTitle: "Two", Monitored: true, HasFile: false},
		{SeriesID: 2, SeriesTitle: "Beta", Season: 1, Episode: 1, EpisodeTitle: "First", Monitored: true, HasFile: false},
		{SeriesID: 2, SeriesTitle: "Beta", Season: 1, Episode: 2, Monitored: false, HasFile: false},
		{SeriesID: 3, SeriesTitle: "Gamma", Season: 1, Episode: 1, Monitored: true, HasFile: true, FileQuality: "720p"},
		{SeriesID: 3, SeriesTitle: "Gamma", Season: 1, Episode: 2, Monitored: true, HasFile: true, FileQuality: "1080p"},
	}
}

func baseProfiles() map[int64]string {
	return map[int64]string{1: "HD-1080p", 2: "HD-1080p", 3: "HD-1080p"}
}

func TestListMissingPaging(t *testing.T) {
	s := New(&fakeRepo{rows: baseRows(), profiles: baseProfiles()}, "")
	p, err := s.List(context.Background(), dom.TabMissing, "", 1, 2)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	// 3 missing episodes (Alpha 1x1, 1x2, Beta 1x1); page 1 of size 2 -> 2 items.
	if p.Total != 3 || len(p.Items) != 2 {
		t.Fatalf("total/len = %d/%d, want 3/2", p.Total, len(p.Items))
	}
	if p.Page != 1 || p.PageSize != 2 {
		t.Fatalf("page/size = %d/%d", p.Page, p.PageSize)
	}
	if p.Items[0].Status != "missing:no file" {
		t.Fatalf("status = %q", p.Items[0].Status)
	}
}

func TestListMissingPageBounds(t *testing.T) {
	s := New(&fakeRepo{rows: baseRows(), profiles: baseProfiles()}, "")
	// Page past the end -> empty items but correct total.
	p, err := s.List(context.Background(), dom.TabMissing, "", 9, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if p.Total != 3 || len(p.Items) != 0 {
		t.Fatalf("past end: total/len = %d/%d, want 3/0", p.Total, len(p.Items))
	}
	// Defaults: no page/size -> page 1 with the default page size (25).
	p, _ = s.List(context.Background(), dom.TabMissing, "", 0, 0)
	if p.Page != 1 || p.PageSize != 25 {
		t.Fatalf("defaults: page/size = %d/%d", p.Page, p.PageSize)
	}
}

func TestListCutoffTab(t *testing.T) {
	// Series 3 uses UHD-2160p (items 2160p, 1080p), whose P3 default cutoff
	// is the lowest item = 1080p. So Gamma 1x1 (720p) is below cutoff, while
	// 1x2 (1080p) is exactly at it and not flagged.
	profiles := map[int64]string{1: "HD-1080p", 2: "HD-1080p", 3: "UHD-2160p"}
	s := New(&fakeRepo{rows: baseRows(), profiles: profiles}, "")
	p, err := s.List(context.Background(), dom.TabCutoff, "", 1, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if p.Total != 1 {
		t.Fatalf("cutoff total = %d, want 1; items %+v", p.Total, p.Items)
	}
	if p.Items[0].SeriesTitle != "Gamma" || p.Items[0].Episode != 1 {
		t.Fatalf("cutoff row = %+v", p.Items[0])
	}
	if p.Items[0].Status != "cutoff:720p below cutoff 1080p" {
		t.Fatalf("cutoff status = %q", p.Items[0].Status)
	}
}

func TestListUnknownTab(t *testing.T) {
	s := New(&fakeRepo{}, "")
	_, err := s.List(context.Background(), dom.Tab("bogus"), "", 1, 10)
	if !errors.Is(err, dom.ErrUnknownTab) {
		t.Fatalf("err = %v, want ErrUnknownTab", err)
	}
}

func TestSearchAll(t *testing.T) {
	repo := &fakeRepo{rows: baseRows(), profiles: baseProfiles()}
	s := New(repo, "")
	n, err := s.SearchAll(context.Background(), dom.TabMissing)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if n != 3 {
		t.Fatalf("searched = %d, want 3", n)
	}
	// One pending wanted row per missing episode.
	if len(repo.wanted) != 3 {
		t.Fatalf("wanted rows = %d, want 3", len(repo.wanted))
	}
	// One search-requested history event per affected series (Alpha + Beta = 2).
	var searchEvents int
	for _, h := range repo.history {
		if h.event == "search-requested" {
			searchEvents++
		}
	}
	if searchEvents != 2 {
		t.Fatalf("search-requested events = %d, want 2", searchEvents)
	}
}

func TestUnmonitor(t *testing.T) {
	repo := &fakeRepo{
		rows:     baseRows(),
		profiles: baseProfiles(),
		epMonitored: map[sxe]bool{
			(sxe{1, 1, 1}): true,
			(sxe{1, 1, 2}): true,
			(sxe{2, 1, 1}): true,
		},
	}
	s := New(repo, "")
	n, err := s.Unmonitor(context.Background(), []dom.EpisodeRef{
		{SeriesID: 1, Season: 1, Episode: 1},
		{SeriesID: 9, Season: 1, Episode: 1}, // unknown -> not counted
		{SeriesID: 2, Season: 1, Episode: 1},
	})
	if err != nil {
		t.Fatalf("unmonitor: %v", err)
	}
	if n != 2 {
		t.Fatalf("unmonitored = %d, want 2", n)
	}
	if repo.epMonitored[sxe{1, 1, 1}] {
		t.Fatal("1x1 should be unmonitored")
	}
}

func TestManualImportHappyPath(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "Gamma.S01E01.1080p.WEB.x264.mkv")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo := &fakeRepo{
		rows: baseRows(), profiles: baseProfiles(),
		epMonitored: map[sxe]bool{(sxe{3, 1, 1}): true},
		series:      map[int64]*dom.SeriesRef{3: {ID: 3, Title: "Gamma", QualityProfile: "HD-1080p"}},
	}
	s := New(repo, dir)
	rep, err := s.ManualImport(context.Background(), dom.ManualImport{
		Kind: "tv", SeriesID: 3, Season: 1, Episode: 1, FilePath: file,
	})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if !rep.Imported {
		t.Fatalf("report = %+v, want imported", rep)
	}
	if len(repo.satisfied) != 1 || repo.satisfied[0].release != "Gamma.S01E01.1080p.WEB.x264.mkv" {
		t.Fatalf("satisfied = %+v", repo.satisfied)
	}
	found := false
	for _, h := range repo.history {
		if h.event == "manual-import" && h.parentID == 3 {
			found = true
		}
	}
	if !found {
		t.Fatalf("history = %+v, want manual-import for series 3", repo.history)
	}
}

func TestManualImportValidation(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "x.mkv")
	_ = os.WriteFile(file, []byte("x"), 0o644)

	repo := &fakeRepo{
		rows: baseRows(), profiles: baseProfiles(),
		epMonitored: map[sxe]bool{(sxe{3, 1, 1}): true},
		series:      map[int64]*dom.SeriesRef{3: {ID: 3, Title: "Gamma"}},
	}
	s := New(repo, dir)

	cases := []struct {
		name string
		req  dom.ManualImport
		want error
	}{
		{"non-tv kind", dom.ManualImport{Kind: "movie", SeriesID: 3, Season: 1, Episode: 1, FilePath: file}, dom.ErrUnsupportedKind},
		{"bad episode", dom.ManualImport{Kind: "tv", SeriesID: 3, Season: 0, Episode: 1, FilePath: file}, dom.ErrInvalidEpisode},
		{"empty path", dom.ManualImport{Kind: "tv", SeriesID: 3, Season: 1, Episode: 1}, dom.ErrEmptyPath},
		{"missing file", dom.ManualImport{Kind: "tv", SeriesID: 3, Season: 1, Episode: 1, FilePath: filepath.Join(dir, "nope.mkv")}, dom.ErrFileNotFound},
		{"outside root", dom.ManualImport{Kind: "tv", SeriesID: 3, Season: 1, Episode: 1, FilePath: "/etc/passwd"}, dom.ErrPathOutsideRoot},
		{"traversal escape", dom.ManualImport{Kind: "tv", SeriesID: 3, Season: 1, Episode: 1, FilePath: filepath.Join(dir, "..", "..", "escape.mkv")}, dom.ErrPathOutsideRoot},
		{"unknown series", dom.ManualImport{Kind: "tv", SeriesID: 99, Season: 1, Episode: 1, FilePath: file}, dom.ErrSeriesNotFound},
		{"unknown episode", dom.ManualImport{Kind: "tv", SeriesID: 3, Season: 2, Episode: 9, FilePath: file}, dom.ErrEpisodeNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := s.ManualImport(context.Background(), tc.req); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestManualImportWritesNFOSidecar(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "Gamma.S01E01.mkv")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo := &fakeRepo{
		rows: baseRows(), profiles: baseProfiles(),
		epMonitored: map[sxe]bool{(sxe{3, 1, 1}): true},
		series:      map[int64]*dom.SeriesRef{3: {ID: 3, Title: "Gamma"}},
	}
	s := New(repo, dir)
	s.SetNFOWriter(&metawrite.Writer{Enabled: true, Language: "en"})

	_, err := s.ManualImport(context.Background(), dom.ManualImport{
		Kind: "tv", SeriesID: 3, Season: 1, Episode: 1, FilePath: file,
	})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	data, err := os.ReadFile(file + ".nfo")
	if err != nil {
		t.Fatalf("nfo sidecar missing: %v", err)
	}
	if !strings.Contains(string(data), "<episodedetails>") || !strings.Contains(string(data), "Gamma") {
		t.Fatalf("nfo content = %q", data)
	}
}
