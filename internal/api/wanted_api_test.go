package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/brandenk514/mediarr/internal/auth"
	wdom "github.com/brandenk514/mediarr/internal/domains/wanted"
	"github.com/brandenk514/mediarr/internal/health"
	wsvc "github.com/brandenk514/mediarr/internal/services/wanted"
)

type sxe struct {
	s  int64
	se int
	ep int
}

// memWantedRepo is an in-memory wanted.Repo for the endpoint contract tests.
type memWantedRepo struct {
	rows      []wdom.EpisodeRow
	profiles  map[int64]string
	series    map[int64]*wdom.SeriesRef
	episodes  map[sxe]bool // present episode keys
	wanted    map[sxe]string
	history   int
	satisfied int
	// L4 music
	musicRows      []wdom.MusicRow
	musicWanted    []string // "album/disc/number"
	musicMonitored map[string]bool
}

func (m *memWantedRepo) EpisodeRows(_ context.Context) ([]wdom.EpisodeRow, error) { return m.rows, nil }
func (m *memWantedRepo) SeriesProfiles(_ context.Context) (map[int64]string, error) {
	return m.profiles, nil
}
func (m *memWantedRepo) EnsureWantedPending(_ context.Context, s int64, se, ep int) error {
	if m.wanted == nil {
		m.wanted = map[sxe]string{}
	}
	m.wanted[sxe{s, se, ep}] = "pending"
	return nil
}
func (m *memWantedRepo) SetEpisodeMonitoredOff(_ context.Context, s int64, se, ep int) (bool, error) {
	k := sxe{s, se, ep}
	if ok, exists := m.episodes[k]; exists && ok {
		m.episodes[k] = false
		return true, nil
	}
	return false, nil
}
func (m *memWantedRepo) GetSeries(_ context.Context, id int64) (*wdom.SeriesRef, error) {
	return m.series[id], nil
}
func (m *memWantedRepo) GetEpisode(_ context.Context, s int64, se, ep int) (*wdom.EpisodeRef, error) {
	if _, exists := m.episodes[sxe{s, se, ep}]; exists {
		return &wdom.EpisodeRef{SeriesID: s, Season: se, Episode: ep}, nil
	}
	return nil, nil
}
func (m *memWantedRepo) AddHistory(_ context.Context, _ string, _ int64, _, _ string) error {
	m.history++
	return nil
}
func (m *memWantedRepo) SatisfyWanted(_ context.Context, _ int64, _, _ int, _, _ string) error {
	m.satisfied++
	return nil
}

// --- L4 music ---

func (m *memWantedRepo) MusicRows(_ context.Context) ([]wdom.MusicRow, error) {
	return m.musicRows, nil
}
func (m *memWantedRepo) EnsureMusicWanted(_ context.Context, albumID int64, disc, number int) error {
	m.musicWanted = append(m.musicWanted, fmt.Sprintf("%d/%d/%d", albumID, disc, number))
	return nil
}
func (m *memWantedRepo) SetMusicMonitoredOff(_ context.Context, albumID int64, disc, number int) (bool, error) {
	k := fmt.Sprintf("%d/%d/%d", albumID, disc, number)
	if ok, exists := m.musicMonitored[k]; exists && ok {
		m.musicMonitored[k] = false
		return true, nil
	}
	return false, nil
}
func (m *memWantedRepo) AddMusicHistory(_ context.Context, _ int64, _, _ string) error {
	m.history++
	return nil
}

func newWantedTestServer(t *testing.T) (*Server, *memWantedRepo, string) {
	t.Helper()
	air := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	repo := &memWantedRepo{
		// Series 2 uses UHD-2160p (cutoff = lowest item = 1080p) so its 720p
		// file is below cutoff; the cutoff tab tests rely on this.
		profiles: map[int64]string{1: "HD-1080p", 2: "UHD-2160p"},
		rows: []wdom.EpisodeRow{
			{SeriesID: 1, SeriesTitle: "Alpha", Season: 1, Episode: 1, EpisodeTitle: "Pilot", AirDate: &air, Monitored: true, HasFile: false},
			{SeriesID: 1, SeriesTitle: "Alpha", Season: 1, Episode: 2, EpisodeTitle: "Two", AirDate: &air, Monitored: true, HasFile: false},
			{SeriesID: 2, SeriesTitle: "Beta", Season: 1, Episode: 1, EpisodeTitle: "First", AirDate: &air, Monitored: true, HasFile: false},
			{SeriesID: 2, SeriesTitle: "Beta", Season: 1, Episode: 2, Monitored: false, HasFile: false},
			{SeriesID: 2, SeriesTitle: "Beta", Season: 2, Episode: 1, Monitored: true, HasFile: true, FileQuality: "720p"},
		},
		series:   map[int64]*wdom.SeriesRef{2: {ID: 2, Title: "Beta", QualityProfile: "HD-1080p"}},
		episodes: map[sxe]bool{(sxe{1, 1, 1}): true, (sxe{1, 1, 2}): true, (sxe{2, 1, 1}): true, (sxe{2, 1, 2}): false, (sxe{2, 2, 1}): true},
	}
	store := newFakeStore()
	u, _ := store.CreateUser(context.Background(), "admin", "", "admin", "hash")
	pair, err := auth.NewToken("ma_live_")
	if err != nil {
		t.Fatalf("new token: %v", err)
	}
	_, _ = store.CreateToken(context.Background(), u, "test", pair, nil)

	s := NewServer(health.NewChecker(nil, nil))
	s.SetAuth(&AuthDeps{Repo: store})
	svc := wsvc.New(repo, os.TempDir())
	s.SetWanted(&WantedDeps{Svc: svc})
	s.SetImport(&ImportDeps{Wanted: svc})
	return s, repo, pair.Raw
}

func TestWanted_ListMissing(t *testing.T) {
	s, _, token := newWantedTestServer(t)
	rec := doReq(t, s.Handler(), http.MethodGet, "/api/v1/wanted?tab=missing", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d; body %s", rec.Code, rec.Body.String())
	}
	var env struct {
		Items    []wdom.Item `json:"items"`
		Total    int         `json:"total"`
		Page     int         `json:"page"`
		PageSize int         `json:"pageSize"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Total != 3 {
		t.Fatalf("total = %d, want 3 (3 monitored file-less)", env.Total)
	}
	if env.PageSize != 25 || env.Page != 1 {
		t.Fatalf("page/size = %d/%d, want 1/25", env.Page, env.PageSize)
	}
	if len(env.Items) != 3 || env.Items[0].Status != "missing:no file" {
		t.Fatalf("items = %+v", env.Items)
	}
}

func TestWanted_ListCutoffPaging(t *testing.T) {
	s, _, token := newWantedTestServer(t)
	// Beta 2x1 (720p) is below the UHD-2160p cutoff (1080p).
	rec := doReq(t, s.Handler(), http.MethodGet, "/api/v1/wanted?tab=cutoff", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	var env struct {
		Items []wdom.Item `json:"items"`
		Total int         `json:"total"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	if env.Total != 1 || len(env.Items) != 1 {
		t.Fatalf("cutoff: total/len = %d/%d, want 1/1; %+v", env.Total, len(env.Items), env.Items)
	}
	if env.Items[0].SeriesTitle != "Beta" || env.Items[0].Episode != 1 {
		t.Fatalf("cutoff row = %+v", env.Items[0])
	}
	// Unknown tab is a 400.
	rec = doReq(t, s.Handler(), http.MethodGet, "/api/v1/wanted?tab=bogus", token)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad tab: got %d, want 400", rec.Code)
	}
}

func TestWanted_SearchAll(t *testing.T) {
	s, repo, token := newWantedTestServer(t)
	rec := doJSONReq(t, s.Handler(), http.MethodPost, "/api/v1/wanted/search", token, `{"tab":"missing"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	var out struct {
		Searchd int `json:"searched"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out.Searchd != 3 {
		t.Fatalf("searched = %d, want 3", out.Searchd)
	}
	if len(repo.wanted) != 3 {
		t.Fatalf("wanted rows = %d, want 3", len(repo.wanted))
	}
	// Bad tab is a 400.
	rec = doJSONReq(t, s.Handler(), http.MethodPost, "/api/v1/wanted/search", token, `{"tab":"nope"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad tab search: got %d, want 400", rec.Code)
	}
}

func TestWanted_Unmonitor(t *testing.T) {
	s, repo, token := newWantedTestServer(t)
	body := `{"episodes":[{"series_id":1,"season":1,"episode":1},{"series_id":1,"season":1,"episode":2},{"series_id":9,"season":1,"episode":1}]}`
	rec := doJSONReq(t, s.Handler(), http.MethodPost, "/api/v1/wanted/unmonitor", token, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	var out struct {
		Unmonitored int `json:"unmonitored"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out.Unmonitored != 2 {
		t.Fatalf("unmonitored = %d, want 2 (unknown series skipped)", out.Unmonitored)
	}
	if repo.episodes[sxe{1, 1, 1}] {
		t.Fatal("1x1 should be unmonitored")
	}
}

func TestImport_ManualHappyPath(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "Beta.S02E01.1080p.WEB.x264.mkv")
	_ = os.WriteFile(file, []byte("x"), 0o644)

	s, repo, token := newWantedTestServer(t)
	body := `{"kind":"tv","series_id":2,"season":2,"episode":1,"file_path":"` + file + `"}`
	rec := doJSONReq(t, s.Handler(), http.MethodPost, "/api/v1/import", token, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d; body %s", rec.Code, rec.Body.String())
	}
	var rep wdom.ManualImportReport
	_ = json.Unmarshal(rec.Body.Bytes(), &rep)
	if !rep.Imported {
		t.Fatalf("report = %+v, want imported", rep)
	}
	if repo.satisfied != 1 || repo.history != 1 {
		t.Fatalf("satisfied/history = %d/%d, want 1/1", repo.satisfied, repo.history)
	}
}

func TestImport_ManualValidation(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "x.mkv")
	_ = os.WriteFile(file, []byte("x"), 0o644)

	s, _, token := newWantedTestServer(t)

	// Non-TV kind -> 400.
	rec := doJSONReq(t, s.Handler(), http.MethodPost, "/api/v1/import", token,
		`{"kind":"movie","series_id":2,"season":1,"episode":1,"file_path":"`+file+`"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("movie kind: got %d, want 400", rec.Code)
	}
	// Unknown series -> 404.
	rec = doJSONReq(t, s.Handler(), http.MethodPost, "/api/v1/import", token,
		`{"kind":"tv","series_id":99,"season":1,"episode":1,"file_path":"`+file+`"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown series: got %d, want 404", rec.Code)
	}
	// Unknown episode -> 404.
	rec = doJSONReq(t, s.Handler(), http.MethodPost, "/api/v1/import", token,
		`{"kind":"tv","series_id":2,"season":9,"episode":9,"file_path":"`+file+`"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown episode: got %d, want 404", rec.Code)
	}
	// Missing file -> 400.
	rec = doJSONReq(t, s.Handler(), http.MethodPost, "/api/v1/import", token,
		`{"kind":"tv","series_id":2,"season":2,"episode":1,"file_path":"`+filepath.Join(dir, "nope.mkv")+`"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing file: got %d, want 400", rec.Code)
	}
}
