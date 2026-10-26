package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	moviesdom "github.com/brandenk514/mediarr/internal/domains/movies"
)

// Tests for the Radarr R1 endpoints: GET /api/v1/movies/stats,
// PUT /api/v1/movies/{id}/editions/{editionId}, and the editions/files
// embedding in the list/detail responses. A fresh server is built per test
// (mux panics on duplicate route registration).

// editionToggleBody matches the SetMonitored contract body.
type editionToggleBody struct {
	Monitored bool `json:"monitored"`
}

// statsMovieBody is the decoded MovieStats contract.
type statsMovieBody struct {
	MovieCount       int   `json:"movieCount"`
	FileCount        int   `json:"fileCount"`
	MonitoredCount   int   `json:"monitoredCount"`
	UnmonitoredCount int   `json:"unmonitoredCount"`
	TotalSize        int64 `json:"totalSize"`
}

func TestMovies_GetStats(t *testing.T) {
	repo := newListMoviesRepo()
	repo.seed(moviesdom.Movie{Title: "Inception", Year: 2010, Monitored: true, QualityProfile: "HD-1080p"})
	repo.seed(moviesdom.Movie{Title: "Tenet", Year: 2020, Monitored: false, QualityProfile: "HD-1080p"})
	_, _ = repo.CreateMovieFile(context.TODO(), moviesdom.MovieFile{MovieID: 1, RelativePath: "Inception (2010)/Inception.mkv", Size: 2_000_000_000})
	_, _ = repo.CreateMovieFile(context.TODO(), moviesdom.MovieFile{MovieID: 1, RelativePath: "Inception (2010)/Inception.1080p.mkv", Size: 1_000_000_000})

	s, token := newListTestServer(t, repo)
	rec := getJSON(t, s.Handler(), "/api/v1/movies/stats", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var st statsMovieBody
	if err := json.NewDecoder(rec.Body).Decode(&st); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if st.MovieCount != 2 || st.MonitoredCount != 1 || st.UnmonitoredCount != 1 {
		t.Fatalf("movie counts = %+v, want 2/1/1", st)
	}
	if st.FileCount != 2 || st.TotalSize != 3_000_000_000 {
		t.Fatalf("file stats = %d/%d, want 2/3000000000", st.FileCount, st.TotalSize)
	}
}

// movieDetailBody is the decoded detail response for the embedding tests.
type movieDetailBody struct {
	Status   string `json:"status"`
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	Editions []struct {
		ID        int64  `json:"id"`
		Label     string `json:"label"`
		Monitored bool   `json:"monitored"`
		Quality   string `json:"quality"`
		Size      int64  `json:"size"`
	} `json:"editions"`
	Files []struct {
		RelativePath string   `json:"relativePath"`
		Size         int64    `json:"size"`
		VideoCodec   string   `json:"videoCodec"`
		Languages    []string `json:"languages"`
		Formats      []string `json:"formats"`
	} `json:"files"`
}

func TestMovies_GetMovie_EmbedsEditionsAndFiles(t *testing.T) {
	repo := newListMoviesRepo()
	id := repo.seed(moviesdom.Movie{Title: "Inception", Year: 2010, Monitored: true, QualityProfile: "HD-1080p"})
	_, _ = repo.CreateMovieFile(context.TODO(), moviesdom.MovieFile{
		MovieID: id, RelativePath: "Inception (2010)/Inception.mkv",
		VideoCodec: "x265", Size: 1_500_000_000, Languages: []string{"en"}, Quality: "2160p", Formats: []string{"web"},
	})

	s, token := newListTestServer(t, repo)
	rec := getJSON(t, s.Handler(), "/api/v1/movies/1", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var m movieDetailBody
	if err := json.NewDecoder(rec.Body).Decode(&m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(m.Editions) != 1 || m.Editions[0].Label != "Theatrical" || !m.Editions[0].Monitored || m.Editions[0].Quality != "HD-1080p" {
		t.Fatalf("editions = %+v, want one monitored Theatrical with HD-1080p", m.Editions)
	}
	if len(m.Files) != 1 || m.Files[0].RelativePath != "Inception (2010)/Inception.mkv" || m.Files[0].Size != 1_500_000_000 {
		t.Fatalf("files = %+v", m.Files)
	}
	if m.Path != "Inception (2010)/Inception.mkv" || m.Size != 1_500_000_000 {
		t.Fatalf("path/size = %q/%d, want derived from the single file", m.Path, m.Size)
	}
	// Monitored movie with no wanted row and no queue entry → missing.
	if m.Status != "missing:monitored" {
		t.Fatalf("status = %q, want missing:monitored", m.Status)
	}
}

func TestMovies_ListMovies_EmbedsEditionsAndStatus(t *testing.T) {
	repo := newListMoviesRepo()
	repo.seed(moviesdom.Movie{Title: "Alpha", Year: 2020, Monitored: true, QualityProfile: "HD-1080p"})
	repo.seed(moviesdom.Movie{Title: "Beta", Year: 2021, Monitored: false, QualityProfile: "UHD-2160p"})

	s, token := newListTestServer(t, repo)
	rec := getJSON(t, s.Handler(), "/api/v1/movies?pageSize=10", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var env struct {
		Items []movieDetailBody `json:"items"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(env.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(env.Items))
	}
	for i, m := range env.Items {
		if m.Editions == nil {
			t.Fatalf("item %d: editions nil (must be an empty array, not null)", i)
		}
		if len(m.Editions) != 1 {
			t.Fatalf("item %d: %d editions, want 1 (seeded Theatrical)", i, len(m.Editions))
		}
		if m.Files == nil {
			t.Fatalf("item %d: files nil (must be an empty array, not null)", i)
		}
	}
	if env.Items[0].Status != "missing:monitored" || env.Items[1].Status != "missing:unmonitored" {
		t.Fatalf("statuses = %q/%q", env.Items[0].Status, env.Items[1].Status)
	}
}

func TestMovies_SetMovieEditionMonitored(t *testing.T) {
	repo := newListMoviesRepo()
	id := repo.seed(moviesdom.Movie{Title: "Inception", Year: 2010, Monitored: true, QualityProfile: "HD-1080p"})
	edID := repo.editions[1].ID // the seeded Theatrical edition of movie 1
	_ = id

	s, token := newListTestServer(t, repo)
	h := s.Handler()

	// Toggle off.
	rec := putJSON(t, h, "/api/v1/movies/1/editions/"+itoa(int(edID)), token, editionToggleBody{Monitored: false})
	if rec.Code != http.StatusOK {
		t.Fatalf("toggle off status = %d body=%s", rec.Code, rec.Body.String())
	}
	var off struct {
		Monitored bool `json:"monitored"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&off); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if off.Monitored {
		t.Fatal("monitored = true after toggle off")
	}
	e, err := repo.GetEdition(context.TODO(), edID)
	if err != nil {
		t.Fatal(err)
	}
	if e.Monitored {
		t.Fatal("repo edition still monitored after toggle off")
	}

	// Toggle back on.
	rec = putJSON(t, h, "/api/v1/movies/1/editions/"+itoa(int(edID)), token, editionToggleBody{Monitored: true})
	if rec.Code != http.StatusOK {
		t.Fatalf("toggle on status = %d body=%s", rec.Code, rec.Body.String())
	}

	// Unknown edition on an existing movie → 404.
	rec = putJSON(t, h, "/api/v1/movies/1/editions/999", token, editionToggleBody{Monitored: true})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown edition status = %d body=%s", rec.Code, rec.Body.String())
	}

	// Edition that belongs to another movie → 404.
	repo.seed(moviesdom.Movie{Title: "Tenet", Year: 2020, Monitored: true, QualityProfile: "HD-1080p"})
	otherEd := repo.editions[2].ID
	rec = putJSON(t, h, "/api/v1/movies/1/editions/"+itoa(int(otherEd)), token, editionToggleBody{Monitored: true})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-movie edition status = %d body=%s", rec.Code, rec.Body.String())
	}

	// Malformed edition id → 400.
	rec = putJSON(t, h, "/api/v1/movies/1/editions/abc", token, editionToggleBody{Monitored: true})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad edition id status = %d body=%s", rec.Code, rec.Body.String())
	}
}

