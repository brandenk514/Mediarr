package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/brandenk514/mediarr/internal/auth"
	moviesdom "github.com/brandenk514/mediarr/internal/domains/movies"
	"github.com/brandenk514/mediarr/internal/health"
	moviesvc "github.com/brandenk514/mediarr/internal/services/movies"
)

// listMoviesRepo is an in-memory movies.Repo for the list/monitored contract
// tests. Only the read + toggle paths the endpoints exercise are implemented
// meaningfully; the wanted/queue/history methods are present so the interface
// compiles.
type listMoviesRepo struct {
	movies map[int64]moviesdom.Movie
	wanted map[int64]*moviesdom.Wanted
	nextID int64
}

func newListMoviesRepo() *listMoviesRepo {
	return &listMoviesRepo{
		movies: map[int64]moviesdom.Movie{},
		wanted: map[int64]*moviesdom.Wanted{},
		nextID: 1,
	}
}

func (r *listMoviesRepo) seed(m moviesdom.Movie) int64 {
	id := r.nextID
	r.nextID++
	m.ID = id
	if m.AddedAt.IsZero() {
		m.AddedAt = time.Now().UTC()
	}
	r.movies[id] = m
	return id
}

func (r *listMoviesRepo) seedWanted(w moviesdom.Wanted) {
	r.wanted[w.MovieID] = &w
}

func (r *listMoviesRepo) CreateMovie(ctx context.Context, m moviesdom.Movie) (int64, error) {
	id := r.seed(m)
	return id, nil
}

func (r *listMoviesRepo) GetMovie(ctx context.Context, id int64) (*moviesdom.Movie, error) {
	m, ok := r.movies[id]
	if !ok {
		return nil, moviesdom.ErrMovieNotFound
	}
	return &m, nil
}

func (r *listMoviesRepo) ListMovies(ctx context.Context) ([]moviesdom.Movie, error) {
	out := make([]moviesdom.Movie, 0, len(r.movies))
	for _, m := range r.movies {
		out = append(out, m)
	}
	return out, nil
}

func (r *listMoviesRepo) SetMovieMonitored(ctx context.Context, id int64, monitored bool) error {
	m, ok := r.movies[id]
	if !ok {
		return moviesdom.ErrMovieNotFound
	}
	m.Monitored = monitored
	r.movies[id] = m
	return nil
}

func (r *listMoviesRepo) EnsureWanted(ctx context.Context, movieID int64) error { return nil }
func (r *listMoviesRepo) GetWanted(ctx context.Context, movieID int64) (*moviesdom.Wanted, error) {
	w, ok := r.wanted[movieID]
	if !ok {
		return nil, sql.ErrNoRows
	}
	return w, nil
}
func (r *listMoviesRepo) ListWanted(ctx context.Context) ([]moviesdom.Wanted, error) {
	return nil, nil
}
func (r *listMoviesRepo) MarkWantedSatisfied(ctx context.Context, movieID int64, releaseTitle string) error {
	return nil
}
func (r *listMoviesRepo) CreateQueue(ctx context.Context, e moviesdom.QueueEntry) (int64, error) {
	return 0, nil
}
func (r *listMoviesRepo) UpdateQueue(ctx context.Context, e moviesdom.QueueEntry) error { return nil }
func (r *listMoviesRepo) GetQueue(ctx context.Context, id int64) (*moviesdom.QueueEntry, error) {
	return nil, nil
}
func (r *listMoviesRepo) ListQueue(ctx context.Context) ([]moviesdom.QueueEntry, error) {
	return nil, nil
}
func (r *listMoviesRepo) AddHistory(ctx context.Context, e moviesdom.HistoryEntry) error {
	return nil
}
func (r *listMoviesRepo) ListHistory(ctx context.Context, limit int) ([]moviesdom.HistoryEntry, error) {
	return nil, nil
}

// newListTestServer returns an authenticated movies server plus a valid bearer
// token, so the contract tests can hit the real requireAuth-wrapped routes.
func newListTestServer(t *testing.T, repo moviesdom.Repo) (*Server, string) {
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
	// Log in to obtain a token.
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
	svc := moviesvc.New(moviesvc.Deps{Repo: repo, DefaultProfile: "HD-1080p"})
	s.SetMovies(&MoviesDeps{Svc: svc})
	return s, lr.Token
}

func getJSON(t *testing.T, h http.Handler, path string, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func putJSON(t *testing.T, h http.Handler, path string, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPut, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestListMovies_Envelope(t *testing.T) {
	repo := newListMoviesRepo()
	repo.seed(moviesdom.Movie{Title: "Dune", Year: 2021, Monitored: true, QualityProfile: "HD-1080p"})
	repo.seed(moviesdom.Movie{Title: "dune part two", Year: 2024, Monitored: false, QualityProfile: "HD-1080p"})
	repo.seed(moviesdom.Movie{Title: "Inception", Year: 2010, Monitored: true, QualityProfile: "HD-1080p"})
	s, token := newListTestServer(t, repo)

	// No filters: all three, page metadata correct.
	rec := getJSON(t, s.Handler(), "/api/v1/movies", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var page struct {
		Items    []moviesdom.Movie `json:"items"`
		Total    int               `json:"total"`
		Page     int               `json:"page"`
		PageSize int               `json:"page_size"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 3 || len(page.Items) != 3 {
		t.Errorf("total=%d len=%d, want 3/3", page.Total, len(page.Items))
	}
	if page.Page != 1 || page.PageSize != 25 {
		t.Errorf("page/size = %d/%d, want 1/25", page.Page, page.PageSize)
	}
}

func TestListMovies_FilterByQ(t *testing.T) {
	repo := newListMoviesRepo()
	repo.seed(moviesdom.Movie{Title: "Dune", Monitored: true})
	repo.seed(moviesdom.Movie{Title: "dune part two", Monitored: true})
	repo.seed(moviesdom.Movie{Title: "Inception", Monitored: true})
	s, token := newListTestServer(t, repo)

	rec := getJSON(t, s.Handler(), "/api/v1/movies?q=DUNE", token)
	var page struct {
		Items []moviesdom.Movie `json:"items"`
		Total int               `json:"total"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 {
		t.Errorf("total = %d, want 2 (both dune titles, case-insensitive)", page.Total)
	}
}

func TestListMovies_FilterByMonitored(t *testing.T) {
	repo := newListMoviesRepo()
	repo.seed(moviesdom.Movie{Title: "A", Monitored: true})
	repo.seed(moviesdom.Movie{Title: "B", Monitored: false})
	repo.seed(moviesdom.Movie{Title: "C", Monitored: true})
	s, token := newListTestServer(t, repo)

	rec := getJSON(t, s.Handler(), "/api/v1/movies?monitored=true", token)
	var page struct {
		Items []moviesdom.Movie `json:"items"`
		Total int               `json:"total"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 {
		t.Errorf("monitored=true total = %d, want 2", page.Total)
	}
}

func TestListMovies_Pagination(t *testing.T) {
	repo := newListMoviesRepo()
	for i := 1; i <= 5; i++ {
		repo.seed(moviesdom.Movie{Title: string(rune('A' + i - 1)), Monitored: true})
	}
	s, token := newListTestServer(t, repo)

	rec := getJSON(t, s.Handler(), "/api/v1/movies?"+url.Values{
		"page":      {"2"},
		"page_size": {"2"},
	}.Encode(), token)
	var page struct {
		Items    []moviesdom.Movie `json:"items"`
		Total    int               `json:"total"`
		Page     int               `json:"page"`
		PageSize int               `json:"page_size"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 5 || page.Page != 2 || page.PageSize != 2 {
		t.Errorf("total/page/size = %d/%d/%d, want 5/2/2", page.Total, page.Page, page.PageSize)
	}
	if len(page.Items) != 2 {
		t.Errorf("len(Items) = %d, want 2", len(page.Items))
	}
}

func TestSetMovieMonitored(t *testing.T) {
	repo := newListMoviesRepo()
	id := repo.seed(moviesdom.Movie{Title: "Dune", Monitored: true})
	s, token := newListTestServer(t, repo)

	rec := putJSON(t, s.Handler(), "/api/v1/movies/1", token, map[string]bool{"monitored": false})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	got, err := repo.GetMovie(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Monitored {
		t.Error("movie should be unmonitored after toggle off")
	}
}

func TestSetMovieMonitored_404Unknown(t *testing.T) {
	repo := newListMoviesRepo()
	s, token := newListTestServer(t, repo)

	rec := putJSON(t, s.Handler(), "/api/v1/movies/999", token, map[string]bool{"monitored": true})
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for unknown movie", rec.Code)
	}
}

// TestGetMovieWanted covers the P7 detail-page endpoint: 200 with the wanted
// row, 204 when the movie has no wanted row yet, 404 for an unknown movie.
func TestGetMovieWanted(t *testing.T) {
	repo := newListMoviesRepo()
	duneID := repo.seed(moviesdom.Movie{Title: "Dune", Year: 2021, Monitored: true})
	repo.seedWanted(moviesdom.Wanted{
		MovieID: duneID, Status: moviesdom.WantedSatisfied,
		ReleaseTitle: "Dune.2021.1080p.BluRay.x264-GROUP",
	})
	plainID := repo.seed(moviesdom.Movie{Title: "Inception", Year: 2010, Monitored: true})
	s, token := newListTestServer(t, repo)

	// Satisfied wanted row → 200 with the JSON body.
	rec := getJSON(t, s.Handler(), fmt.Sprintf("/api/v1/movies/%d/wanted", duneID), token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", rec.Code, rec.Body.String())
	}
	var wd struct {
		MovieID      int64  `json:"MovieID"`
		Status       string `json:"Status"`
		ReleaseTitle string `json:"ReleaseTitle"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&wd); err != nil {
		t.Fatal(err)
	}
	if wd.MovieID != duneID || wd.Status != "satisfied" || wd.ReleaseTitle == "" {
		t.Errorf("got %+v, want movie %d satisfied with a release title", wd, duneID)
	}

	// Movie exists but no wanted row → 204.
	rec = getJSON(t, s.Handler(), fmt.Sprintf("/api/v1/movies/%d/wanted", plainID), token)
	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d body=%s, want 204", rec.Code, rec.Body.String())
	}

	// Unknown movie → 404.
	rec = getJSON(t, s.Handler(), "/api/v1/movies/999/wanted", token)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestListMovies_401WithoutToken(t *testing.T) {
	repo := newListMoviesRepo()
	s, _ := newListTestServer(t, repo)
	rec := getJSON(t, s.Handler(), "/api/v1/movies", "")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 with no token", rec.Code)
	}
}
