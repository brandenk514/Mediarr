package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/brandenk514/mediarr/internal/auth"
	"github.com/brandenk514/mediarr/internal/health"
	"github.com/brandenk514/mediarr/internal/providers"
	"github.com/brandenk514/mediarr/internal/settings"
	tvdom "github.com/brandenk514/mediarr/internal/domains/tv"
	tvsvc "github.com/brandenk514/mediarr/internal/services/tv"
	moviesdom "github.com/brandenk514/mediarr/internal/domains/movies"
)

// fakeTVLib is an in-memory TVImporter for the import-list endpoint tests.
type fakeTVLib struct {
	series  []tvdom.Series
	added   []string
	addErr  error
}

func (f *fakeTVLib) AddSeries(_ context.Context, req tvsvc.AddSeriesRequest) (int64, error) {
	if f.addErr != nil {
		return 0, f.addErr
	}
	f.added = append(f.added, req.Title)
	return int64(len(f.added)), nil
}

func (f *fakeTVLib) ListSeries(context.Context) ([]tvdom.Series, error) {
	return f.series, nil
}

// fakeMoviesLib is an in-memory MoviesImporter for the import-list tests.
type fakeMoviesLib struct {
	movies []moviesdom.Movie
	added  []string
}

func (f *fakeMoviesLib) AddMovie(_ context.Context, title, _, _ string) (int64, error) {
	f.added = append(f.added, title)
	return int64(len(f.added)), nil
}

func (f *fakeMoviesLib) ListMovies(context.Context) ([]moviesdom.Movie, error) {
	return f.movies, nil
}

// newP5TestServer builds a fresh authenticated server for the P5
// wave-2/3 endpoints over an in-memory settings store. Each test calls
// SetSettingsP5 exactly once (mux double-registration panics).
func newP5TestServer(t *testing.T) (*Server, *fakeSettingsStore, string) {
	t.Helper()
	store := newFakeStore()
	u, _ := store.CreateUser(context.Background(), "admin", "", "admin", "hash")
	pair, err := auth.NewToken("ma_live_")
	if err != nil {
		t.Fatalf("new token: %v", err)
	}
	_, _ = store.CreateToken(context.Background(), u, "test", pair, nil)

	settingsStore := newFakeSettingsStore()
	s := NewServer(health.NewChecker(nil, nil))
	s.SetAuth(&AuthDeps{Repo: store})
	return s, settingsStore, pair.Raw
}

func postP5JSON(t *testing.T, s *Server, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

// TestSettingsP5_RequiresAuth: all four P5 routes are 401 without a token.
func TestSettingsP5_RequiresAuth(t *testing.T) {
	s, store, _ := newP5TestServer(t)
	s.SetSettingsP5(&SettingsP5Deps{Store: store})
	for _, path := range []string{
		"/api/v1/settings/custom-formats/score",
		"/api/v1/settings/connect/test",
		"/api/v1/settings/metadata-source/search",
		"/api/v1/settings/import-lists/import",
	} {
		rec := doReq(t, s.Handler(), http.MethodPost, path, "")
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("POST %s without token: got %d, want 401", path, rec.Code)
		}
	}
}

func TestScoreCustomFormats(t *testing.T) {
	s, store, token := newP5TestServer(t)
	s.SetSettingsP5(&SettingsP5Deps{Store: store})
	// Save a custom-formats document: one matching format (title contains
	// "1080p", +2), one unmatched (quality equals "2160p", -3).
	doc := settings.CustomFormatsSettings{Formats: []settings.CustomFormat{
		{Name: "1080p bonus", Score: 2, Enabled: true, Rules: []settings.CFRule{
			{Field: "title", Operator: "contains", Value: "1080p"},
		}},
		{Name: "4k penalty", Score: -3, Enabled: true, Rules: []settings.CFRule{
			{Field: "quality", Operator: "equals", Value: "2160p"},
		}},
		{Name: "disabled", Score: 100, Enabled: false, Rules: []settings.CFRule{
			{Field: "title", Operator: "contains", Value: "1080p"},
		}},
	}}
	if err := store.PutSectionDocument(context.Background(), settings.SectionCustomFormats, mustJSON(t, doc)); err != nil {
		t.Fatal(err)
	}
	rec := postP5JSON(t, s, "/api/v1/settings/custom-formats/score", token,
		`{"title":"Show.1080p.WEB","quality":"1080p"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Score   int `json:"score"`
		Matches []struct {
			Name  string `json:"name"`
			Score int    `json:"score"`
		} `json:"matches"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Score != 2 || len(out.Matches) != 1 || out.Matches[0].Name != "1080p bonus" {
		t.Fatalf("unexpected result: %+v", out)
	}
}

func TestTestConnectNotification_InlineWebhook(t *testing.T) {
	var gotPath, gotAuth, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		b := make([]byte, 1024)
		n, _ := r.Body.Read(b)
		gotBody = string(b[:n])
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	s, store, token := newP5TestServer(t)
	s.SetSettingsP5(&SettingsP5Deps{Store: store, HTTP: srv.Client()})
	rec := postP5JSON(t, s, "/api/v1/settings/connect/test", token,
		`{"kind":"webhook","options":{"url":"`+srv.URL+`","token":"sekrit"},"event":"test"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		OK      bool   `json:"ok"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !out.OK {
		t.Fatalf("want ok=true, got %+v", out)
	}
	if gotAuth != "Bearer sekrit" || gotPath != "/" {
		t.Fatalf("stub saw path=%q auth=%q", gotPath, gotAuth)
	}
	if !strings.Contains(gotBody, `"type":"test"`) {
		t.Fatalf("stub body missing event type: %s", gotBody)
	}
}

func TestTestConnectNotification_SavedNotification(t *testing.T) {
	var gotToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("X-Plex-Token")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	s, store, token := newP5TestServer(t)
	s.SetSettingsP5(&SettingsP5Deps{Store: store, HTTP: srv.Client()})
	// Save the notification with a stored (plaintext-in-doc) token.
	doc := settings.ConnectSettings{Notifications: []settings.Notification{
		{ID: 1, Name: "Plex refresh", Kind: "plex", Enabled: true,
			Options: settings.NotificationOptions{URL: srv.URL, SectionID: "1", Token: "stored-token"}},
	}}
	if err := store.PutSectionDocument(context.Background(), settings.SectionConnect, mustJSON(t, doc)); err != nil {
		t.Fatal(err)
	}
	rec := postP5JSON(t, s, "/api/v1/settings/connect/test", token, `{"id":1}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}
	if gotToken != "stored-token" {
		t.Fatalf("sink did not use the stored token, got %q", gotToken)
	}
}

func TestTestConnectNotification_DeliveryFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	t.Cleanup(srv.Close)

	s, store, token := newP5TestServer(t)
	s.SetSettingsP5(&SettingsP5Deps{Store: store, HTTP: srv.Client()})
	rec := postP5JSON(t, s, "/api/v1/settings/connect/test", token,
		`{"kind":"webhook","options":{"url":"`+srv.URL+`"}}`)
	// The endpoint itself answers 200; the delivery failure is in the body.
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rec.Code)
	}
	var out struct {
		OK      bool   `json:"ok"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.OK {
		t.Fatalf("want ok=false on non-2xx delivery: %+v", out)
	}
}

func TestTestConnectNotification_UnknownKind(t *testing.T) {
	s, store, token := newP5TestServer(t)
	s.SetSettingsP5(&SettingsP5Deps{Store: store})
	rec := postP5JSON(t, s, "/api/v1/settings/connect/test", token, `{"kind":"carrier-pigeon"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rec.Code)
	}
}

func TestSearchMetadataSource_FallbackToKeyless(t *testing.T) {
	// tvdb unconfigured (nil adapter) -> tvmaze stub answers.
	stubURL := newStubServer(t, `{
		"shows": [{"score": 1, "show": {"id": 7, "name": "Fool U", "premiered": "2020-01-01"}}]
	}`)
	s, store, token := newP5TestServer(t)
	s.SetSettingsP5(&SettingsP5Deps{
		Store:  store,
		TVmaze: providers.NewTVmaze(),
		HTTP:   nil,
	})
	s.settingsP5Deps.TVmaze.BaseURL = stubURL
	// The section document pins the tv chain to [tvdb, tvmaze].
	src := settings.MetadataSourceSettings{Sources: map[string][]string{
		"tv": {"tvdb", "tvmaze"},
	}}
	if err := store.PutSectionDocument(context.Background(), settings.SectionMetadataSource, mustJSON(t, src)); err != nil {
		t.Fatal(err)
	}
	rec := postP5JSON(t, s, "/api/v1/settings/metadata-source/search", token, `{"kind":"tv","query":"fool u"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Source   string `json:"source"`
		Results  []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"results"`
		Attempts []struct {
			Source string `json:"source"`
			OK     bool   `json:"ok"`
			Error  string `json:"error"`
		} `json:"attempts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Source != "tvmaze" || len(out.Results) != 1 || out.Results[0].Title != "Fool U" {
		t.Fatalf("unexpected result: %+v", out)
	}
	if len(out.Attempts) != 2 || out.Attempts[0].OK || out.Attempts[1].OK != true {
		t.Fatalf("attempts: %+v", out.Attempts)
	}
}

func TestSearchMetadataSource_AllFail503(t *testing.T) {
	// Both sources unconfigured -> 503 with the attempt log.
	s, store, token := newP5TestServer(t)
	s.SetSettingsP5(&SettingsP5Deps{Store: store})
	src := settings.MetadataSourceSettings{Sources: map[string][]string{
		"tv": {"tvdb", "tvmaze"},
	}}
	if err := store.PutSectionDocument(context.Background(), settings.SectionMetadataSource, mustJSON(t, src)); err != nil {
		t.Fatal(err)
	}
	rec := postP5JSON(t, s, "/api/v1/settings/metadata-source/search", token, `{"kind":"tv","query":"x"}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want 503: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Source   string `json:"source"`
		Attempts []struct {
			Source string `json:"source"`
		} `json:"attempts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Source != "" || len(out.Attempts) != 2 {
		t.Fatalf("unexpected 503 body: %+v", out)
	}
}

func TestSearchMetadataSource_BadKind(t *testing.T) {
	s, store, token := newP5TestServer(t)
	s.SetSettingsP5(&SettingsP5Deps{Store: store})
	rec := postP5JSON(t, s, "/api/v1/settings/metadata-source/search", token, `{"kind":"cartoon","query":"x"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rec.Code)
	}
}

func TestImportLists_TraktDryRunAndReal(t *testing.T) {
	// Trakt stub returns one show + one movie. The list URL keeps the
	// real trakt.tv host (parsed for user/slug); the API host itself is
	// overridden below with the stub.
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/u/lists/slug/items" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"type":"show","id":11,"show":{"title":"Alpha Show","year":2021}},
			{"type":"movie","id":22,"movie":{"title":"Beta Movie","year":2022}},
			{"type":"person","id":33}
		]`))
	}))
	t.Cleanup(stub.Close)
	oldTrakt := traktAPIBase
	traktAPIBase = stub.URL
	t.Cleanup(func() { traktAPIBase = oldTrakt })

	tv := &fakeTVLib{series: []tvdom.Series{{Title: "Alpha Show"}}} // pre-existing
	movies := &fakeMoviesLib{}

	s, store, token := newP5TestServer(t)
	s.SetSettingsP5(&SettingsP5Deps{Store: store, TV: tv, Movies: movies, HTTP: stub.Client()})
	lists := settings.ImportListsSettings{Lists: []settings.ImportList{
		{ID: 1, Name: "My list", Kind: "trakt", URL: "https://trakt.tv/users/u/lists/slug", Enabled: true, APIKey: "client-id"},
	}}
	if err := store.PutSectionDocument(context.Background(), settings.SectionImportLists, mustJSON(t, lists)); err != nil {
		t.Fatal(err)
	}

	// Dry run: nothing added, existing item reported exists.
	rec := postP5JSON(t, s, "/api/v1/settings/import-lists/import", token, `{"id":1,"dry_run":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("dry run got %d: %s", rec.Code, rec.Body.String())
	}
	var dry struct {
		Total int `json:"total"`
		Items []struct {
			Title  string `json:"title"`
			Status string `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &dry); err != nil {
		t.Fatal(err)
	}
	if dry.Total != 2 {
		t.Fatalf("dry run total = %d, want 2: %+v", dry.Total, dry)
	}
	statusOf := map[string]string{}
	for _, it := range dry.Items {
		statusOf[it.Title] = it.Status
	}
	if statusOf["Alpha Show"] != "exists" || statusOf["Beta Movie"] != "skipped" {
		t.Fatalf("dry run statuses: %+v", statusOf)
	}
	if len(tv.added) != 0 || len(movies.added) != 0 {
		t.Fatalf("dry run must not add: %+v %+v", tv.added, movies.added)
	}

	// Real run: the new movie is added; the existing show is not.
	rec = postP5JSON(t, s, "/api/v1/settings/import-lists/import", token, `{"id":1,"dry_run":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("real run got %d: %s", rec.Code, rec.Body.String())
	}
	var real struct {
		Items []struct {
			Title  string `json:"title"`
			Status string `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &real); err != nil {
		t.Fatal(err)
	}
	statusOf = map[string]string{}
	for _, it := range real.Items {
		statusOf[it.Title] = it.Status
	}
	if statusOf["Alpha Show"] != "exists" || statusOf["Beta Movie"] != "imported" {
		t.Fatalf("real run statuses: %+v", statusOf)
	}
	if len(movies.added) != 1 || movies.added[0] != "Beta Movie" {
		t.Fatalf("movies added: %+v", movies.added)
	}
}

func TestImportLists_SonarrBridge(t *testing.T) {
	// A Sonarr instance stub: the drop-in bridge.
	sonarr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/series" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("X-Api-Key") != "bridge-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"title":"Bridged Series","year":2019,"seriesId":55},
			{"title":"","seriesId":56}
		]`))
	}))
	t.Cleanup(sonarr.Close)

	tv := &fakeTVLib{}
	s, store, token := newP5TestServer(t)
	s.SetSettingsP5(&SettingsP5Deps{Store: store, TV: tv, HTTP: sonarr.Client()})
	lists := settings.ImportListsSettings{Lists: []settings.ImportList{
		{ID: 2, Name: "Other Sonarr", Kind: "sonarr", URL: sonarr.URL, Enabled: true, APIKey: "bridge-key"},
	}}
	if err := store.PutSectionDocument(context.Background(), settings.SectionImportLists, mustJSON(t, lists)); err != nil {
		t.Fatal(err)
	}
	rec := postP5JSON(t, s, "/api/v1/settings/import-lists/import", token, `{"id":2,"dry_run":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}
	if len(tv.added) != 1 || tv.added[0] != "Bridged Series" {
		t.Fatalf("tv added: %+v", tv.added)
	}
}

func TestImportLists_Errors(t *testing.T) {
	s, store, token := newP5TestServer(t)
	s.SetSettingsP5(&SettingsP5Deps{Store: store})
	// Unknown id -> 404.
	rec := postP5JSON(t, s, "/api/v1/settings/import-lists/import", token, `{"id":99}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown id: got %d, want 404", rec.Code)
	}
	// Disabled list -> 400.
	lists := settings.ImportListsSettings{Lists: []settings.ImportList{
		{ID: 1, Name: "off", Kind: "sonarr", URL: "http://x.example", Enabled: false},
	}}
	if err := store.PutSectionDocument(context.Background(), settings.SectionImportLists, mustJSON(t, lists)); err != nil {
		t.Fatal(err)
	}
	rec = postP5JSON(t, s, "/api/v1/settings/import-lists/import", token, `{"id":1}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("disabled list: got %d, want 400", rec.Code)
	}
	// Fetch failure -> 502.
	lists.Lists[0].Enabled = true
	if err := store.PutSectionDocument(context.Background(), settings.SectionImportLists, mustJSON(t, lists)); err != nil {
		t.Fatal(err)
	}
	rec = postP5JSON(t, s, "/api/v1/settings/import-lists/import", token, `{"id":1}`)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("fetch failure: got %d, want 502", rec.Code)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// newStubServer is a tiny JSON-replay stub for provider search endpoints.
func newStubServer(t *testing.T, body string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}
