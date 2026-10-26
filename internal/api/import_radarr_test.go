package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	moviesdom "github.com/brandenk514/mediarr/internal/domains/movies"
	moviesvc "github.com/brandenk514/mediarr/internal/services/movies"
	"github.com/brandenk514/mediarr/internal/settings"
)

// radarrMoviesLib is an in-memory MoviesImporter that captures the full add
// request (the R5a bridge must honor the source monitored state).
type radarrMoviesLib struct {
	movies []moviesdom.Movie
	added  []moviesvc.AddMovieRequest
}

func (f *radarrMoviesLib) AddMovie(_ context.Context, req moviesvc.AddMovieRequest) (int64, error) {
	f.added = append(f.added, req)
	return int64(len(f.added)), nil
}

func (f *radarrMoviesLib) ListMovies(context.Context) ([]moviesdom.Movie, error) {
	return f.movies, nil
}

// stubHostPort splits an httptest server URL into host + port for the
// radarr ImportList (host/port, not URL, is the radarr list shape).
func stubHostPort(t *testing.T, raw string) (string, int) {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	return u.Hostname(), port
}

// R5a drop-in bridge: fetch a Radarr instance's movies and add them
// idempotently (existing title+year → exists, not duplicated), honoring
// the source monitored state.
func TestImportLists_RadarrBridge(t *testing.T) {
	// Fake Radarr: one new monitored movie, one pre-existing movie, one
	// new unmonitored movie, and one untitled entry (dropped by the fetch).
	var gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/movies" {
			http.NotFound(w, r)
			return
		}
		gotKey = r.URL.Query().Get("api_key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"id":1,"title":"Dune","year":2021,"tmdbId":414906,"monitored":true},
			{"id":2,"title":"Existing","year":2019,"tmdbId":603,"monitored":false},
			{"id":3,"title":"Unmonitored","year":2018,"tmdbId":700,"monitored":false},
			{"id":4,"year":2020,"tmdbId":0}
		]`))
	}))
	t.Cleanup(srv.Close)
	host, port := stubHostPort(t, srv.URL)

	movies := &radarrMoviesLib{movies: []moviesdom.Movie{{Title: "Existing", Year: 2019}}}
	s, store, token := newP5TestServer(t)
	s.SetSettingsP5(&SettingsP5Deps{Store: store, Movies: movies, HTTP: srv.Client()})
	lists := settings.ImportListsSettings{Lists: []settings.ImportList{{
		ID: 1, Name: "My Radarr", Kind: "radarr", Host: host, Port: port, Enabled: true, APIKey: "radarr-key",
	}}}
	if err := store.PutSectionDocument(context.Background(), settings.SectionImportLists, mustJSON(t, lists)); err != nil {
		t.Fatal(err)
	}

	rec := postP5JSON(t, s, "/api/v1/settings/import-lists/import", token, `{"id":1,"dry_run":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}
	if gotKey != "radarr-key" {
		t.Errorf("api_key query param = %q, want radarr-key", gotKey)
	}
	var out struct {
		Total int `json:"total"`
		Items []struct {
			ExternalID string `json:"external_id"`
			Title      string `json:"title"`
			Status     string `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Total != 3 { // the untitled entry is dropped by the fetch
		t.Fatalf("total = %d, want 3: %+v", out.Total, out)
	}
	statusOf := map[string]string{}
	externalOf := map[string]string{}
	for _, it := range out.Items {
		statusOf[it.Title] = it.Status
		externalOf[it.Title] = it.ExternalID
	}
	if statusOf["Dune"] != "imported" || statusOf["Existing"] != "exists" || statusOf["Unmonitored"] != "imported" {
		t.Fatalf("statuses: %+v", statusOf)
	}
	if externalOf["Dune"] != "414906" {
		t.Errorf("Dune external_id = %q, want the source tmdb id", externalOf["Dune"])
	}

	if len(movies.added) != 2 {
		t.Fatalf("added = %+v, want Dune + Unmonitored", movies.added)
	}
	monOf := map[string]string{}
	for _, a := range movies.added {
		monOf[a.Title] = a.Monitor
	}
	if monOf["Dune"] != "" {
		t.Errorf("Dune monitor = %q, want the default", monOf["Dune"])
	}
	if monOf["Unmonitored"] != "unmonitored" {
		t.Errorf("Unmonitored monitor = %q, want unmonitored", monOf["Unmonitored"])
	}
}

// R5a: a Radarr instance that rejects the key (401) or has no such endpoint
// (404) is a caller configuration problem → 400 with the reason, not a 502.
func TestImportLists_RadarrNon2xxIs400(t *testing.T) {
	for status, wantReason := range map[int]string{
		http.StatusUnauthorized: "target returned 401",
		http.StatusNotFound:     "target returned 404",
	} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
			}))
			t.Cleanup(srv.Close)
			host, port := stubHostPort(t, srv.URL)

			s, store, token := newP5TestServer(t)
			s.SetSettingsP5(&SettingsP5Deps{Store: store})
			lists := settings.ImportListsSettings{Lists: []settings.ImportList{{
				ID: 1, Name: "My Radarr", Kind: "radarr", Host: host, Port: port, Enabled: true,
			}}}
			if err := store.PutSectionDocument(context.Background(), settings.SectionImportLists, mustJSON(t, lists)); err != nil {
				t.Fatal(err)
			}
			rec := postP5JSON(t, s, "/api/v1/settings/import-lists/import", token, `{"id":1}`)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("%d from Radarr: got %d, want 400: %s", status, rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), wantReason) {
				t.Fatalf("%d from Radarr: body %q must contain %q", status, rec.Body.String(), wantReason)
			}
		})
	}
}
