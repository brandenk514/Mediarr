package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Tests for the Radarr R2 add-movie endpoint: the edition-aware monitor
// variants, the persisted add-flow fields, enum validation, and the
// best-effort start-search. A fresh server is built per test (mux panics on
// duplicate route registration).

// r2MovieBody is the decoded add-flow detail response (wire fields use the
// Go exported names; the domain struct has no JSON tags).
type r2MovieBody struct {
	Monitored           bool     `json:"Monitored"`
	Tags                []string `json:"Tags"`
	RootFolder          string   `json:"RootFolder"`
	MinimumAvailability string   `json:"MinimumAvailability"`
	Editions            []struct {
		Label     string `json:"Label"`
		Monitored bool   `json:"Monitored"`
	} `json:"Editions"`
}

// r2AddBody is the R2 add request shape.
type r2AddBody struct {
	Title               string   `json:"title"`
	Year                string   `json:"year"`
	QualityProfile      string   `json:"quality_profile"`
	Monitor             string   `json:"monitor"`
	MinimumAvailability string   `json:"minimum_availability"`
	Tags                []string `json:"tags"`
	RootFolder          string   `json:"root_folder"`
	StartSearch         bool     `json:"start_search"`
}

// postR2Movie posts an R2 add body to /api/v1/movies with auth.
func postR2Movie(t *testing.T, h http.Handler, token string, body r2AddBody) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/movies", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestMovies_AddMovieMonitorVariants(t *testing.T) {
	for _, tc := range []struct {
		name          string
		monitor       string
		wantMonitored bool
		wantEdition   bool
	}{
		{name: "default (all)", monitor: "", wantMonitored: true, wantEdition: true},
		{name: "all", monitor: "all", wantMonitored: true, wantEdition: true},
		{name: "movie_only", monitor: "movie_only", wantMonitored: true, wantEdition: false},
		{name: "unmonitored", monitor: "unmonitored", wantMonitored: false, wantEdition: false},
		{name: "all_except_3d", monitor: "all_except_3d", wantMonitored: true, wantEdition: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, token := newListTestServer(t, newListMoviesRepo())
			rec := postR2Movie(t, s.Handler(), token, r2AddBody{
				Title: "Dune Part Two", Year: "2024", Monitor: tc.monitor,
			})
			if rec.Code != http.StatusCreated {
				t.Fatalf("add status = %d body=%s", rec.Code, rec.Body.String())
			}
			var added struct {
				ID int64 `json:"id"`
			}
			if err := json.NewDecoder(rec.Body).Decode(&added); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			detail := getJSON(t, s.Handler(), "/api/v1/movies/"+itoa(int(added.ID)), token)
			if detail.Code != http.StatusOK {
				t.Fatalf("detail status = %d body=%s", detail.Code, detail.Body.String())
			}
			var m r2MovieBody
			if err := json.NewDecoder(detail.Body).Decode(&m); err != nil {
				t.Fatalf("unmarshal detail: %v", err)
			}
			if m.Monitored != tc.wantMonitored {
				t.Errorf("Monitored = %v, want %v", m.Monitored, tc.wantMonitored)
			}
			if len(m.Editions) != 1 {
				t.Fatalf("editions = %+v, want one seeded", m.Editions)
			}
			if m.Editions[0].Monitored != tc.wantEdition {
				t.Errorf("edition Monitored = %v, want %v", m.Editions[0].Monitored, tc.wantEdition)
			}
		})
	}
}

func TestMovies_AddMoviePersistsR2Fields(t *testing.T) {
	s, token := newListTestServer(t, newListMoviesRepo())
	rec := postR2Movie(t, s.Handler(), token, r2AddBody{
		Title:               "Dune Part Two",
		Year:                "2024",
		Monitor:             "movie_only",
		MinimumAvailability: "in_production",
		Tags:                []string{"sci-fi", "watchlist"},
		RootFolder:          "/media/movies",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("add status = %d body=%s", rec.Code, rec.Body.String())
	}
	var added struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&added); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	detail := getJSON(t, s.Handler(), "/api/v1/movies/"+itoa(int(added.ID)), token)
	if detail.Code != http.StatusOK {
		t.Fatalf("detail status = %d body=%s", detail.Code, detail.Body.String())
	}
	var m r2MovieBody
	if err := json.NewDecoder(detail.Body).Decode(&m); err != nil {
		t.Fatalf("unmarshal detail: %v", err)
	}
	if len(m.Tags) != 2 || m.Tags[0] != "sci-fi" || m.Tags[1] != "watchlist" {
		t.Errorf("Tags = %v, want [sci-fi watchlist]", m.Tags)
	}
	if m.RootFolder != "/media/movies" {
		t.Errorf("RootFolder = %q, want /media/movies", m.RootFolder)
	}
	if m.MinimumAvailability != "in_production" {
		t.Errorf("MinimumAvailability = %q, want in_production", m.MinimumAvailability)
	}
}

func TestMovies_AddMovieInvalidMonitor(t *testing.T) {
	s, token := newListTestServer(t, newListMoviesRepo())
	rec := postR2Movie(t, s.Handler(), token, r2AddBody{Title: "X", Year: "2024", Monitor: "bogus"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestMovies_AddMovieInvalidMinimumAvailability(t *testing.T) {
	s, token := newListTestServer(t, newListMoviesRepo())
	rec := postR2Movie(t, s.Handler(), token, r2AddBody{
		Title: "X", Year: "2024", MinimumAvailability: "previews",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body=%s)", rec.Code, rec.Body.String())
	}
}

// TestMovies_AddMovieStartSearch asserts the endpoint accepts start_search
// and that the best-effort pipeline failure (no download client in this test
// wiring) does not fail the add.
func TestMovies_AddMovieStartSearch(t *testing.T) {
	s, token := newListTestServer(t, newListMoviesRepo())
	rec := postR2Movie(t, s.Handler(), token, r2AddBody{
		Title: "Dune Part Two", Year: "2024", StartSearch: true,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("add with start_search status = %d, want 201 (body=%s)", rec.Code, rec.Body.String())
	}
	var added struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&added); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if added.ID == 0 {
		t.Fatal("expected non-zero id")
	}
}
