package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/brandenk514/mediarr/internal/auth"
	tvdom "github.com/brandenk514/mediarr/internal/domains/tv"
	"github.com/brandenk514/mediarr/internal/health"
	"github.com/brandenk514/mediarr/internal/providers"
	moviesvc "github.com/brandenk514/mediarr/internal/services/movies"
	tvsvc "github.com/brandenk514/mediarr/internal/services/tv"
)

// addFlowServer is a fully-wired, authenticated server for the E2E add-flow
// tests (M6 #42): real search endpoints backed by httptest provider stubs,
// and real movie/TV services backed by in-memory repos.
type addFlowServer struct {
	s     *Server
	token string
}

func newAddFlowServer(t *testing.T) *addFlowServer {
	t.Helper()
	store := newFakeStore()
	s := NewServer(health.NewChecker(nil, nil))
	s.SetAuth(&AuthDeps{Repo: store})
	hash, err := auth.HashPassword("a-very-long-password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateUser(context.Background(), "admin", "", "admin", hash); err != nil {
		t.Fatal(err)
	}
	rec := postJSON(t, s.Handler(), "/api/v1/auth/login", loginRequest{
		Username: "admin", Password: "a-very-long-password",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d body=%s", rec.Code, rec.Body.String())
	}
	var lr loginResponse
	if err := json.NewDecoder(rec.Body).Decode(&lr); err != nil {
		t.Fatal(err)
	}

	// Movies: real service on the in-memory list repo.
	movieRepo := newListMoviesRepo()
	movieSvc := moviesvc.New(moviesvc.Deps{Repo: movieRepo, DefaultProfile: "HD-1080p"})
	s.SetMovies(&MoviesDeps{Svc: movieSvc})

	// TV: real service on an in-memory repo.
	tvSvc := tvsvc.New(tvsvc.Deps{Repo: newMemTVRepo(), DefaultProfile: "HD-1080p"})
	s.SetTV(&TVDeps{Svc: tvSvc})
	return &addFlowServer{s: s, token: lr.Token}
}

func (f *addFlowServer) get(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	return getJSON(t, f.s.Handler(), path, f.token)
}

func (f *addFlowServer) post(t *testing.T, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+f.token)
	rec := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(rec, req)
	return rec
}

// ---- E2E movie add flow: search → select → add → verify ----

func TestE2E_MovieAddFlow(t *testing.T) {
	// TMDB stub serving the movie search.
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"results":[{"id":603,"title":"Dune: Part Two","release_date":"2024-03-01","overview":"Fremen.","vote_average":7.8}]}`))
	}))
	defer stub.Close()

	f := newAddFlowServer(t)
	f.s.SetSearch(&SearchDeps{TMDB: providers.NewTMDB("k")})
	f.s.searchDeps.TMDB.BaseURL = stub.URL

	// 1. Search the provider (as the UI's debounced search does).
	rec := f.get(t, "/api/v1/search/movies?query=dune")
	if rec.Code != http.StatusOK {
		t.Fatalf("search status = %d body=%s", rec.Code, rec.Body.String())
	}
	var hits []searchResult
	if err := json.NewDecoder(rec.Body).Decode(&hits); err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("want 1 hit, got %d", len(hits))
	}
	selected := hits[0]
	if selected.Title != "Dune: Part Two" {
		t.Fatalf("hit = %+v", selected)
	}

	// 2. Fetch the quality profiles for the picker.
	rec = f.get(t, "/api/v1/profiles?kind=movies")
	if rec.Code != http.StatusOK {
		t.Fatalf("profiles status = %d", rec.Code)
	}
	var prof profileResponse
	if err := json.NewDecoder(rec.Body).Decode(&prof); err != nil {
		t.Fatal(err)
	}
	if prof.Default != "HD-1080p" {
		t.Fatalf("default profile = %q", prof.Default)
	}

	// 3. Add the selected result with the chosen profile.
	rec = f.post(t, "/api/v1/movies", map[string]string{
		"title":           selected.Title,
		"year":            "2024",
		"quality_profile": "UHD-2160p",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("add status = %d body=%s", rec.Code, rec.Body.String())
	}

	// 4. Verify it lands in the library list with the chosen profile.
	rec = f.get(t, "/api/v1/movies")
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d", rec.Code)
	}
	var page struct {
		Items []struct {
			Title          string `json:"Title"`
			QualityProfile string `json:"QualityProfile"`
		} `json:"Items"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].Title != "Dune: Part Two" || page.Items[0].QualityProfile != "UHD-2160p" {
		t.Fatalf("list = %+v", page.Items)
	}
}

// ---- E2E TV add flow: search (TVDB) → select → add → verify ----

func TestE2E_TVAddFlow(t *testing.T) {
	// TVDB stub: two-step login + search, like the real v4 API.
	tvdb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			_, _ = w.Write([]byte(`{"token":"jwt-xyz"}`))
		case "/search/series":
			_, _ = w.Write([]byte(`{"data":[{"id":82350,"seriesName":"Breaking Bad","firstAired":"2008-01-20","overview":"Chem."}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer tvdb.Close()

	f := newAddFlowServer(t)
	f.s.SetSearch(&SearchDeps{TVDB: providers.NewTVDB("k")})
	f.s.searchDeps.TVDB.BaseURL = tvdb.URL

	// 1. Search.
	rec := f.get(t, "/api/v1/search/tv?query=breaking")
	if rec.Code != http.StatusOK {
		t.Fatalf("search status = %d body=%s", rec.Code, rec.Body.String())
	}
	var hits []searchResult
	if err := json.NewDecoder(rec.Body).Decode(&hits); err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].ID != "82350" || hits[0].Title != "Breaking Bad" {
		t.Fatalf("hits = %+v", hits)
	}

	// 2. Add the selected series with its tvdb_id and a profile.
	rec = f.post(t, "/api/v1/tv", map[string]string{
		"title":           "Breaking Bad",
		"year":            "2008",
		"quality_profile": "HD-1080p",
		"tvdb_id":         hits[0].ID,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("add status = %d body=%s", rec.Code, rec.Body.String())
	}

	// 3. Verify it lands in the series list with the provider id recorded.
	rec = f.get(t, "/api/v1/tv")
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d", rec.Code)
	}
	var page struct {
		Items []struct {
			Title  string `json:"Title"`
			TvdbID *int   `json:"TVDBID"`
		} `json:"Items"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].Title != "Breaking Bad" {
		t.Fatalf("list = %+v", page.Items)
	}
	if page.Items[0].TvdbID == nil || *page.Items[0].TvdbID != 82350 {
		t.Fatalf("tvdb_id = %v, want 82350", page.Items[0].TvdbID)
	}
}

// ---- In-memory TV repo for the E2E test ----

type memTVRepo struct {
	series  map[int64]tvdom.Series
	nextID  int64
	history []tvdom.HistoryEntry
}

func newMemTVRepo() *memTVRepo {
	return &memTVRepo{series: map[int64]tvdom.Series{}, nextID: 1}
}

func (r *memTVRepo) CreateSeries(ctx context.Context, s tvdom.Series) (int64, error) {
	id := r.nextID
	r.nextID++
	s.ID = id
	if s.AddedAt.IsZero() {
		s.AddedAt = time.Now().UTC()
	}
	r.series[id] = s
	return id, nil
}

func (r *memTVRepo) GetSeries(ctx context.Context, id int64) (*tvdom.Series, error) {
	s, ok := r.series[id]
	if !ok {
		return nil, tvdom.ErrSeriesNotFound
	}
	return &s, nil
}

func (r *memTVRepo) ListSeries(ctx context.Context) ([]tvdom.Series, error) {
	out := make([]tvdom.Series, 0, len(r.series))
	for _, s := range r.series {
		out = append(out, s)
	}
	return out, nil
}

func (r *memTVRepo) SetSeriesMonitored(ctx context.Context, id int64, monitored bool) error {
	if s, ok := r.series[id]; ok {
		s.Monitored = monitored
		r.series[id] = s
	}
	return nil
}

// UpdateSeries applies a partial series update (P2).
func (r *memTVRepo) UpdateSeries(ctx context.Context, id int64, p tvdom.SeriesPatch) error {
	s, ok := r.series[id]
	if !ok {
		return tvdom.ErrSeriesNotFound
	}
	if p.Monitored != nil {
		s.Monitored = *p.Monitored
	}
	if p.QualityProfile != nil {
		s.QualityProfile = *p.QualityProfile
	}
	if p.SeriesType != nil {
		s.SeriesType = *p.SeriesType
	}
	if p.SeasonFolder != nil {
		s.SeasonFolder = *p.SeasonFolder
	}
	if p.Tags != nil {
		s.Tags = *p.Tags
	}
	r.series[id] = s
	return nil
}

func (r *memTVRepo) CreateEpisode(ctx context.Context, e tvdom.Episode) (int64, error) {
	return 0, nil
}

func (r *memTVRepo) GetEpisode(ctx context.Context, seriesID int64, season, episode int) (*tvdom.Episode, error) {
	return nil, tvdom.ErrEpisodeNotFound
}

func (r *memTVRepo) ListEpisodes(ctx context.Context, seriesID int64) ([]tvdom.Episode, error) {
	return nil, nil
}

func (r *memTVRepo) SetEpisodeMonitored(ctx context.Context, seriesID int64, season, episode int, monitored bool) error {
	return nil
}

func (r *memTVRepo) EnsureWanted(ctx context.Context, seriesID int64, season, episode int) error {
	return nil
}

func (r *memTVRepo) GetWanted(ctx context.Context, seriesID int64, season, episode int) (*tvdom.Wanted, error) {
	return nil, nil
}

func (r *memTVRepo) ListWanted(ctx context.Context, seriesID int64) ([]tvdom.Wanted, error) {
	return nil, nil
}

func (r *memTVRepo) MarkWantedSatisfied(ctx context.Context, seriesID int64, season, episode int, releaseTitle, quality string) error {
	return nil
}

func (r *memTVRepo) CreateQueue(ctx context.Context, e tvdom.QueueEntry) (int64, error) {
	return 0, nil
}

func (r *memTVRepo) UpdateQueue(ctx context.Context, e tvdom.QueueEntry) error {
	return nil
}

func (r *memTVRepo) GetQueue(ctx context.Context, id int64) (*tvdom.QueueEntry, error) {
	return nil, nil
}

func (r *memTVRepo) ListQueue(ctx context.Context, seriesID int64) ([]tvdom.QueueEntry, error) {
	return nil, nil
}

func (r *memTVRepo) AddHistory(ctx context.Context, e tvdom.HistoryEntry) error {
	r.history = append(r.history, e)
	return nil
}

func (r *memTVRepo) ListHistory(ctx context.Context, seriesID int64, limit int) ([]tvdom.HistoryEntry, error) {
	return r.history, nil
}
