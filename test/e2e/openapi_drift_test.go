// OpenAPI drift guard (M8 #78).
//
// TestOpenAPIDrift boots the full API server against real Postgres + Redis
// (testcontainers), walks every /api/v1 operation, and validates both the
// request we send and the response the server actually returns against
// openapi.yaml with kin-openapi. A renamed Go struct field, a missing or
// extra property, a changed envelope shape, or an undeclared status code
// breaks the validation and fails this test — the Go-side drift guard that
// catches wire changes before any generated type is regenerated.
//
// It requires a running Docker daemon, like the other integration tests:
//
//	go test ./test/e2e/...
package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	legacyrouter "github.com/getkin/kin-openapi/routers/legacy"
	"github.com/gorilla/websocket"

	"github.com/brandenk514/mediarr/internal/api"
	"github.com/brandenk514/mediarr/internal/auth"
	"github.com/brandenk514/mediarr/internal/health"
	"github.com/brandenk514/mediarr/internal/indexers"
	"github.com/brandenk514/mediarr/internal/metrics"
	"github.com/brandenk514/mediarr/internal/postgres"
	"github.com/brandenk514/mediarr/internal/secrets"
	booksSvc "github.com/brandenk514/mediarr/internal/services/books"
	moviesSvc "github.com/brandenk514/mediarr/internal/services/movies"
	musicSvc "github.com/brandenk514/mediarr/internal/services/music"
	tvSvc "github.com/brandenk514/mediarr/internal/services/tv"
)

// driftWorld bundles a live server, the loaded spec, and the current bearer
// token. Every drift call validates request-then-response against the spec.
type driftWorld struct {
	t      *testing.T
	ts     *httptest.Server
	doc    *openapi3.T
	router routers.Router
	opts   *openapi3filter.Options
	token  string
	client *http.Client
}

// specHost must match the openapi.yaml servers entry; the legacy router
// matches requests against it.
const specHost = "localhost:8080"

// loadSpec reads openapi.yaml from the repo root and structurally validates
// it (the same loader tools/speccheck uses, so a broken spec fails here too).
func loadSpec(t *testing.T) *openapi3.T {
	t.Helper()
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromFile("../../openapi.yaml")
	if err != nil {
		t.Fatalf("load openapi.yaml: %v", err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("openapi.yaml invalid: %v", err)
	}
	return doc
}

// newDriftServer wires the full production server in-process: real repos on
// the harness's Postgres, real services (no download client, no indexers —
// pipeline endpoints exercise their "no client" error path), the indexer
// registry/worker, and the Redis event bus.
func newDriftServer(t *testing.T, h *harness) *driftWorld {
	t.Helper()

	vault, err := secrets.NewVault(testKey)
	if err != nil {
		t.Fatalf("vault: %v", err)
	}
	movieRepo := postgres.NewMovieRepo(h.db)
	tvRepo := postgres.NewTVRepo(h.db)
	musicRepo := postgres.NewMusicRepo(h.db)
	booksRepo := postgres.NewBooksRepo(h.db)
	queueRepo := postgres.NewQueueRepo(h.db)
	dashRepo := postgres.NewDashboardRepo(h.db)
	settingsRepo := postgres.NewSettingsRepo(h.db, vault)
	authRepo := auth.NewRepo(h.db)
	indexerRepo := postgres.NewIndexersRepo(h.db, vault)

	m := metrics.New(metrics.Options{})
	movieS := moviesSvc.New(moviesSvc.Deps{Repo: movieRepo, Events: h.events, Metrics: m})
	tvS := tvSvc.New(tvSvc.Deps{Repo: tvRepo, Events: h.events, Metrics: m})
	musicS := musicSvc.New(musicSvc.Deps{Repo: musicRepo, Events: h.events, Metrics: m})
	booksS := booksSvc.New(booksSvc.Deps{Repo: booksRepo, Events: h.events, Metrics: m})

	registry := indexers.NewDefaultRegistry()
	registry.Deps = indexers.AdapterDeps{Health: indexers.NewHealthTracker()}
	worker := indexers.NewHealthWorker(indexerRepo, registry, registry.Deps, 0, slog.New(slog.DiscardHandler))
	workerCtx, stopWorker := context.WithCancel(h.ctx)
	t.Cleanup(stopWorker)
	go func() { _ = worker.Run(workerCtx) }()

	pgStore, err := postgres.New(h.dsn)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = pgStore.Close() })
	checker := health.NewChecker(pgStore, h.cache)

	srv := api.NewServer(checker)
	srv.SetMetrics(&api.MetricsDeps{M: m})
	srv.SetIndexers(&api.IndexersDeps{
		Repo: indexerRepo, Worker: worker,
		Tracker: registry.Deps.Health, Admin: registry,
	})
	srv.SetDashboard(&api.DashboardDeps{Repo: dashRepo, Tracker: registry.Deps.Health, DefaultRecent: 10})
	srv.SetQueue(&api.QueueDeps{Repo: queueRepo})
	srv.SetSettings(&api.SettingsDeps{Store: settingsRepo})
	srv.SetAuth(&api.AuthDeps{Repo: authRepo})
	srv.SetEvents(&api.EventsDeps{Repo: authRepo, Events: h.events})
	srv.SetMovies(&api.MoviesDeps{Svc: movieS})
	srv.SetTV(&api.TVDeps{Svc: tvS})
	srv.SetMusic(&api.MusicDeps{Svc: musicS})
	srv.SetBooks(&api.BooksDeps{Svc: booksS})
	// Nil providers: the search endpoints answer 503 "not configured", which
	// the spec declares.
	srv.SetSearch(&api.SearchDeps{})

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return &driftWorld{t: t, ts: ts, client: &http.Client{Timeout: 30 * time.Second}}
}

// newRequest builds a fresh (reusable) request: the validation pass and the
// live call each consume a body reader.
func (d *driftWorld) newRequest(method, path string, query url.Values, body []byte, withToken bool) *http.Request {
	d.t.Helper()
	u := d.ts.URL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, u, rd)
	if err != nil {
		d.t.Fatalf("build request %s %s: %v", method, path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if withToken {
		req.Header.Set("Authorization", "Bearer "+d.token)
	}
	return req
}

// drift performs one operation and validates both sides against the spec:
// the request (matched route, params, body) and the response (status, JSON
// body). Returns the raw response body for extraction.
func (d *driftWorld) drift(method, path string, query url.Values, pathParams map[string]string, body any, wantStatus int) []byte {
	d.t.Helper()
	var bodyBytes []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			d.t.Fatalf("marshal body for %s %s: %v", method, path, err)
		}
		bodyBytes = b
	}
	withToken := d.token != ""

	// 1. Validate the outgoing request against the spec (route match +
	//    params + body). The explicit pathParams the test passes are
	//    cross-checked against the params the spec's route template
	//    derives.
	{
		req := d.newRequest(method, path, query, bodyBytes, withToken)
		route, derived, err := d.router.FindRoute(withSpecHost(req))
		if err != nil {
			d.t.Fatalf("drift: no route in openapi.yaml for %s %s: %v", method, path, err)
		}
		for k, v := range pathParams {
			if derived[k] != v {
				d.t.Fatalf("drift: %s %s path param %s = %q, spec route derives %q", method, path, k, v, derived[k])
			}
		}
		rvi := &openapi3filter.RequestValidationInput{
			Request:    req,
			Route:      route,
			PathParams: derived,
			Options:    d.opts,
		}
		if err := openapi3filter.ValidateRequest(context.Background(), rvi); err != nil {
			d.t.Fatalf("drift: request %s %s invalid per openapi.yaml: %v", method, path, err)
		}
	}

	// 2. Make the real call.
	res, err := d.client.Do(d.newRequest(method, path, query, bodyBytes, withToken))
	if err != nil {
		d.t.Fatalf("drift: call %s %s: %v", method, path, err)
	}
	defer func() { _ = res.Body.Close() }()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		d.t.Fatalf("drift: read %s %s: %v", method, path, err)
	}
	if res.StatusCode != wantStatus {
		d.t.Fatalf("drift: %s %s -> %d, want %d\nbody: %.500s", method, path, res.StatusCode, wantStatus, data)
	}

	// 3. Validate the real response against the spec.
	route, derived, err := d.router.FindRoute(withSpecHost(res.Request))
	if err != nil {
		d.t.Fatalf("drift: no route in openapi.yaml for %s %s: %v", method, path, err)
	}
	if err := openapi3filter.ValidateResponse(context.Background(), (&openapi3filter.ResponseValidationInput{
		RequestValidationInput: &openapi3filter.RequestValidationInput{
			Request:    res.Request,
			Route:      route,
			PathParams: derived,
		},
		Options: d.opts,
		Status:  res.StatusCode,
		Header:  res.Header,
	}).SetBodyBytes(data)); err != nil {
		d.t.Fatalf("drift: response %s %s (%d) does not match openapi.yaml: %v\nbody: %.500s",
			method, path, res.StatusCode, err, data)
	}
	return data
}

// withSpecHost rewrites a request's host to the spec's servers entry so
// the legacy router can match it (the test server is 127.0.0.1:<random>).
func withSpecHost(req *http.Request) *http.Request {
	r := *req
	u := *r.URL
	u.Host = specHost
	r.URL = &u
	return &r
}

// driftNoSecurity is for the unauthenticated negative cases: the request
// deliberately lacks the bearer token, so only the response (the 401 Error
// document) is validated.
func (d *driftWorld) driftNoSecurity(method, path string, wantStatus int) {
	d.t.Helper()
	res, err := d.client.Do(d.newRequest(method, path, nil, nil, false))
	if err != nil {
		d.t.Fatalf("drift: call %s %s: %v", method, path, err)
	}
	defer func() { _ = res.Body.Close() }()
	data, _ := io.ReadAll(res.Body)
	if res.StatusCode != wantStatus {
		d.t.Fatalf("drift: %s %s -> %d, want %d\nbody: %.500s", method, path, res.StatusCode, wantStatus, data)
	}
	route, derived, err := d.router.FindRoute(withSpecHost(res.Request))
	if err != nil {
		d.t.Fatalf("drift: no route in openapi.yaml for %s %s: %v", method, path, err)
	}
	if err := openapi3filter.ValidateResponse(context.Background(), (&openapi3filter.ResponseValidationInput{
		RequestValidationInput: &openapi3filter.RequestValidationInput{
			Request:    res.Request,
			Route:      route,
			PathParams: derived,
		},
		Options: d.opts,
		Status:  res.StatusCode,
		Header:  res.Header,
	}).SetBodyBytes(data)); err != nil {
		d.t.Fatalf("drift: 401 response for %s %s does not match openapi.yaml: %v\nbody: %.500s", method, path, err, data)
	}
}

// id extracts {"id": N} from an AddedId-style response.
func id(t *testing.T, body []byte) int64 {
	t.Helper()
	var v struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("extract id from %s: %v", body, err)
	}
	return v.ID
}

// driftWebSocket connects to /api/v1/events with the bearer token and
// validates the hello frame against the EventFrame schema; a bad token must
// be rejected (401) before the upgrade.
func (d *driftWorld) driftWebSocket(t *testing.T) {
	t.Helper()
	scheme := "ws"
	if strings.HasPrefix(d.ts.URL, "https") {
		scheme = "wss"
	}
	dial := func(token string) (*websocket.Conn, *http.Response, error) {
		return websocket.DefaultDialer.Dial(
			fmt.Sprintf("%s://%s/api/v1/events?token=%s", scheme, strings.TrimPrefix(d.ts.URL, "http://"), url.QueryEscape(token)),
			nil)
	}
	// Invalid token: rejected before the upgrade.
	if _, resp, err := dial("ma_invalid"); err == nil || (resp != nil && resp.StatusCode != http.StatusUnauthorized) {
		t.Fatalf("ws: expected 401 for invalid token, got err=%v status=%v", err, statusOf(resp))
	}
	conn, resp, err := dial(d.token)
	if err != nil {
		t.Fatalf("ws dial: %v (status %v)", err, statusOf(resp))
	}
	defer func() { _ = conn.Close() }()

	var frame map[string]any
	if err := conn.ReadJSON(&frame); err != nil {
		t.Fatalf("ws read hello frame: %v", err)
	}
	if err := d.doc.ValidateSchemaJSON(d.doc.Components.Schemas["EventFrame"].Value, frame); err != nil {
		t.Fatalf("ws: hello frame does not match EventFrame schema: %v\nframe: %v", err, frame)
	}
}

func statusOf(resp *http.Response) int {
	if resp == nil {
		return -1
	}
	return resp.StatusCode
}

func TestOpenAPIDrift(t *testing.T) {
	h := startHarness(t)
	d := newDriftServer(t, h)
	d.doc = loadSpec(t)
	router, err := legacyrouter.NewRouter(d.doc)
	if err != nil {
		t.Fatalf("build spec router: %v", err)
	}
	d.router = router
	d.opts = &openapi3filter.Options{
		// The spec's bearerAuth is the only security scheme; accept exactly
		// the token this test minted (kin-openapi requires an
		// AuthenticationFunc or it fails the security requirement).
		AuthenticationFunc: func(_ context.Context, in *openapi3filter.AuthenticationInput) error {
			got := in.RequestValidationInput.Request.Header.Get("Authorization")
			if got != "Bearer "+d.token {
				return errors.New("expected the minted bearer token")
			}
			return nil
		},
	}

	// ---- liveness / version (no auth) ----
	d.drift("GET", "/healthz", nil, nil, nil, http.StatusOK)
	d.drift("GET", "/readyz", nil, nil, nil, http.StatusOK)
	d.drift("GET", "/version", nil, nil, nil, http.StatusOK)

	// ---- auth gate (401 before login), then first-boot login + me ----
	d.driftNoSecurity("GET", "/api/v1/movies", http.StatusUnauthorized)
	d.driftNoSecurity("GET", "/api/v1/auth/me", http.StatusUnauthorized)
	login := d.drift("POST", "/api/v1/auth/login", nil, nil, map[string]any{
		"username": "admin", "password": "drift-guard-password",
	}, http.StatusOK)
	var loginResp struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(login, &loginResp); err != nil || loginResp.Token == "" {
		t.Fatalf("login: no token in %s", login)
	}
	d.token = loginResp.Token
	d.drift("GET", "/api/v1/auth/me", nil, nil, nil, http.StatusOK)

	// ---- movies ----
	movieID := id(t, d.drift("POST", "/api/v1/movies", nil, nil, map[string]any{
		"title": "Dr. Strangelove", "year": "1964",
	}, http.StatusCreated))
	d.drift("GET", "/api/v1/movies", nil, nil, nil, http.StatusOK)
	d.drift("GET", "/api/v1/movies", url.Values{"q": []string{"strange"}}, nil, nil, http.StatusOK)
	d.drift("GET", fmt.Sprintf("/api/v1/movies/%d", movieID), nil, map[string]string{"id": fmt.Sprint(movieID)}, nil, http.StatusOK)
	d.drift("PUT", fmt.Sprintf("/api/v1/movies/%d", movieID), nil, map[string]string{"id": fmt.Sprint(movieID)},
		map[string]any{"monitored": false}, http.StatusOK)
	d.drift("PUT", fmt.Sprintf("/api/v1/movies/%d", movieID), nil, map[string]string{"id": fmt.Sprint(movieID)},
		map[string]any{"monitored": true}, http.StatusOK)
	// No download client is wired: the pipeline answers its "no client" 500,
	// which the spec declares.
	d.drift("POST", fmt.Sprintf("/api/v1/movies/%d/pipeline", movieID), nil, map[string]string{"id": fmt.Sprint(movieID)}, nil, http.StatusInternalServerError)
	d.drift("GET", "/api/v1/movies/999999", nil, map[string]string{"id": "999999"}, nil, http.StatusNotFound)

	// ---- tv ----
	seriesID := id(t, d.drift("POST", "/api/v1/tv", nil, nil, map[string]any{
		"title": "The Expanse", "year": "2017", "monitored": true,
	}, http.StatusCreated))
	d.drift("GET", "/api/v1/tv", nil, nil, nil, http.StatusOK)
	d.drift("GET", fmt.Sprintf("/api/v1/tv/%d", seriesID), nil, map[string]string{"id": fmt.Sprint(seriesID)}, nil, http.StatusOK)
	id(t, d.drift("POST", fmt.Sprintf("/api/v1/tv/%d/episodes", seriesID), nil, map[string]string{"id": fmt.Sprint(seriesID)},
		map[string]any{"season": 1, "episode": 1, "title": "Cain's Break"}, http.StatusCreated))
	d.drift("GET", fmt.Sprintf("/api/v1/tv/%d/episodes", seriesID), nil, map[string]string{"id": fmt.Sprint(seriesID)}, nil, http.StatusOK)
	d.drift("PUT", fmt.Sprintf("/api/v1/tv/%d", seriesID), nil, map[string]string{"id": fmt.Sprint(seriesID)},
		map[string]any{"monitored": false}, http.StatusOK)
	d.drift("PUT", fmt.Sprintf("/api/v1/tv/%d/seasons/1", seriesID), nil, map[string]string{"id": fmt.Sprint(seriesID), "season": "1"},
		map[string]any{"monitored": true}, http.StatusOK)
	d.drift("PUT", fmt.Sprintf("/api/v1/tv/%d/episodes/1/1", seriesID), nil,
		map[string]string{"id": fmt.Sprint(seriesID), "season": "1", "episode": "1"},
		map[string]any{"monitored": false}, http.StatusOK)
	d.drift("GET", fmt.Sprintf("/api/v1/tv/%d/wanted", seriesID), nil, map[string]string{"id": fmt.Sprint(seriesID)}, nil, http.StatusOK)
	d.drift("POST", fmt.Sprintf("/api/v1/tv/%d/pipeline", seriesID), nil, map[string]string{"id": fmt.Sprint(seriesID)}, nil, http.StatusInternalServerError)
	d.drift("GET", "/api/v1/tv/999999", nil, map[string]string{"id": "999999"}, nil, http.StatusNotFound)

	// ---- music ----
	artistID := id(t, d.drift("POST", "/api/v1/music/artists", nil, nil, map[string]any{
		"name": "Pink Floyd", "monitored": true,
	}, http.StatusCreated))
	d.drift("GET", "/api/v1/music/artists", nil, nil, nil, http.StatusOK)
	d.drift("GET", fmt.Sprintf("/api/v1/music/artists/%d", artistID), nil, map[string]string{"id": fmt.Sprint(artistID)}, nil, http.StatusOK)
	d.drift("PUT", fmt.Sprintf("/api/v1/music/artists/%d", artistID), nil, map[string]string{"id": fmt.Sprint(artistID)},
		map[string]any{"monitored": false}, http.StatusOK)
	albumID := id(t, d.drift("POST", fmt.Sprintf("/api/v1/music/artists/%d/albums", artistID), nil, map[string]string{"id": fmt.Sprint(artistID)},
		map[string]any{"name": "The Dark Side of the Moon", "year": 1973}, http.StatusCreated))
	d.drift("GET", fmt.Sprintf("/api/v1/music/artists/%d/albums", artistID), nil, map[string]string{"id": fmt.Sprint(artistID)}, nil, http.StatusOK)
	d.drift("GET", fmt.Sprintf("/api/v1/music/albums/%d", albumID), nil, map[string]string{"id": fmt.Sprint(albumID)}, nil, http.StatusOK)
	d.drift("PUT", fmt.Sprintf("/api/v1/music/albums/%d", albumID), nil, map[string]string{"id": fmt.Sprint(albumID)},
		map[string]any{"monitored": false}, http.StatusOK)
	id(t, d.drift("POST", fmt.Sprintf("/api/v1/music/albums/%d/tracks", albumID), nil, map[string]string{"id": fmt.Sprint(albumID)},
		map[string]any{"disc": 1, "number": 1, "title": "Speak to Me"}, http.StatusCreated))
	d.drift("GET", fmt.Sprintf("/api/v1/music/albums/%d/tracks", albumID), nil, map[string]string{"id": fmt.Sprint(albumID)}, nil, http.StatusOK)
	d.drift("PUT", fmt.Sprintf("/api/v1/music/albums/%d/tracks/1/1", albumID), nil,
		map[string]string{"id": fmt.Sprint(albumID), "disc": "1", "number": "1"},
		map[string]any{"monitored": false}, http.StatusOK)
	d.drift("GET", fmt.Sprintf("/api/v1/music/albums/%d/wanted", albumID), nil, map[string]string{"id": fmt.Sprint(albumID)}, nil, http.StatusOK)
	d.drift("POST", fmt.Sprintf("/api/v1/music/artists/%d/pipeline", artistID), nil, map[string]string{"id": fmt.Sprint(artistID)}, nil, http.StatusInternalServerError)
	d.drift("GET", "/api/v1/music/artists/999999", nil, map[string]string{"id": "999999"}, nil, http.StatusNotFound)

	// ---- books ----
	authorID := id(t, d.drift("POST", "/api/v1/books/authors", nil, nil, map[string]any{
		"name": "Ursula K. Le Guin", "monitored": true,
	}, http.StatusCreated))
	d.drift("GET", "/api/v1/books/authors", nil, nil, nil, http.StatusOK)
	d.drift("GET", fmt.Sprintf("/api/v1/books/authors/%d", authorID), nil, map[string]string{"id": fmt.Sprint(authorID)}, nil, http.StatusOK)
	d.drift("PUT", fmt.Sprintf("/api/v1/books/authors/%d", authorID), nil, map[string]string{"id": fmt.Sprint(authorID)},
		map[string]any{"monitored": false}, http.StatusOK)
	titleID := id(t, d.drift("POST", fmt.Sprintf("/api/v1/books/authors/%d/titles", authorID), nil, map[string]string{"id": fmt.Sprint(authorID)},
		map[string]any{"name": "The Dispossessed"}, http.StatusCreated))
	d.drift("GET", fmt.Sprintf("/api/v1/books/authors/%d/titles", authorID), nil, map[string]string{"id": fmt.Sprint(authorID)}, nil, http.StatusOK)
	d.drift("GET", fmt.Sprintf("/api/v1/books/titles/%d", titleID), nil, map[string]string{"id": fmt.Sprint(titleID)}, nil, http.StatusOK)
	d.drift("PUT", fmt.Sprintf("/api/v1/books/titles/%d", titleID), nil, map[string]string{"id": fmt.Sprint(titleID)},
		map[string]any{"monitored": false}, http.StatusOK)
	editionID := id(t, d.drift("POST", fmt.Sprintf("/api/v1/books/titles/%d/editions", titleID), nil, map[string]string{"id": fmt.Sprint(titleID)},
		map[string]any{"format": "epub", "isbn": "978-0-06-051275-0"}, http.StatusCreated))
	d.drift("GET", fmt.Sprintf("/api/v1/books/titles/%d/editions", titleID), nil, map[string]string{"id": fmt.Sprint(titleID)}, nil, http.StatusOK)
	d.drift("GET", fmt.Sprintf("/api/v1/books/editions/%d", editionID), nil, map[string]string{"id": fmt.Sprint(editionID)}, nil, http.StatusOK)
	d.drift("PUT", fmt.Sprintf("/api/v1/books/editions/%d", editionID), nil, map[string]string{"id": fmt.Sprint(editionID)},
		map[string]any{"monitored": false}, http.StatusOK)
	d.drift("GET", fmt.Sprintf("/api/v1/books/titles/%d/wanted", titleID), nil, map[string]string{"id": fmt.Sprint(titleID)}, nil, http.StatusOK)
	d.drift("POST", fmt.Sprintf("/api/v1/books/authors/%d/pipeline", authorID), nil, map[string]string{"id": fmt.Sprint(authorID)}, nil, http.StatusInternalServerError)
	d.drift("GET", "/api/v1/books/authors/999999", nil, map[string]string{"id": "999999"}, nil, http.StatusNotFound)

	// ---- indexers (fake kind: no network, always healthy) ----
	d.drift("GET", "/api/v1/indexers", nil, nil, nil, http.StatusOK)
	d.drift("POST", "/api/v1/indexers", nil, nil, map[string]any{
		"name": "drift-fake", "kind": "fake",
	}, http.StatusCreated)
	driftBody := d.drift("GET", "/api/v1/indexers", nil, nil, nil, http.StatusOK)
	// Extract the id from the list (the list shape is the Indexer schema).
	var indexersList []map[string]any
	if err := json.Unmarshal(driftBody, &indexersList); err != nil || len(indexersList) != 1 {
		t.Fatalf("expected exactly one indexer in %s: %v", driftBody, err)
	}
	idxRaw, _ := indexersList[0]["id"].(float64)
	indexerID := int64(idxRaw)
	d.drift("GET", fmt.Sprintf("/api/v1/indexers/%d/stats", indexerID), nil, map[string]string{"id": fmt.Sprint(indexerID)}, nil, http.StatusOK)
	d.drift("POST", fmt.Sprintf("/api/v1/indexers/%d/test", indexerID), nil, map[string]string{"id": fmt.Sprint(indexerID)}, nil, http.StatusOK)
	d.drift("PATCH", fmt.Sprintf("/api/v1/indexers/%d/enabled", indexerID), nil, map[string]string{"id": fmt.Sprint(indexerID)},
		map[string]any{"enabled": false}, http.StatusOK)
	d.drift("PUT", fmt.Sprintf("/api/v1/indexers/%d", indexerID), nil, map[string]string{"id": fmt.Sprint(indexerID)},
		map[string]any{"name": "drift-fake-renamed", "kind": "fake"}, http.StatusOK)
	d.drift("DELETE", fmt.Sprintf("/api/v1/indexers/%d", indexerID), nil, map[string]string{"id": fmt.Sprint(indexerID)}, nil, http.StatusNoContent)
	d.drift("GET", "/api/v1/indexers/999999/stats", nil, map[string]string{"id": "999999"}, nil, http.StatusNotFound)

	// ---- dashboard / queue / settings ----
	d.drift("GET", "/api/v1/dashboard", nil, nil, nil, http.StatusOK)
	d.drift("GET", "/api/v1/dashboard", url.Values{"recent": []string{"5"}}, nil, nil, http.StatusOK)
	d.drift("GET", "/api/v1/queue", nil, nil, nil, http.StatusOK)
	d.drift("GET", "/api/v1/queue", url.Values{"kind": []string{"movie"}, "state": []string{"queued"}}, nil, nil, http.StatusOK)
	d.drift("GET", fmt.Sprintf("/api/v1/queue/movie/%d/history", movieID), nil, map[string]string{"kind": "movie", "parent": fmt.Sprint(movieID)}, nil, http.StatusOK)
	d.drift("GET", "/api/v1/settings/download-client", nil, nil, nil, http.StatusOK)
	d.drift("PUT", "/api/v1/settings/download-client", nil, nil, map[string]any{"kind": "mock"}, http.StatusOK)
	d.drift("GET", "/api/v1/settings/library", nil, nil, nil, http.StatusOK)
	for _, kind := range []string{"movie", "tv", "music", "book"} {
		d.drift("PUT", "/api/v1/settings/library/"+kind, nil, map[string]string{"kind": kind},
			map[string]any{"path": "/media/" + kind, "quality_profile": "HD-1080p"}, http.StatusOK)
	}

	// ---- search (nil providers -> 503 "not configured") ----
	for _, search := range []string{
		"/api/v1/search/movies?query=dune",
		"/api/v1/search/tv?query=expanse",
		"/api/v1/search/tv-tmdb?query=expanse",
		"/api/v1/search/artists?query=pink",
		"/api/v1/search/books/authors?query=leguin",
		"/api/v1/search/books/works?query=dispossessed&author=leguin",
	} {
		path, query, _ := strings.Cut(search, "?")
		vals, _ := url.ParseQuery(query)
		d.drift("GET", path, vals, nil, nil, http.StatusServiceUnavailable)
	}

	// ---- profiles ----
	for _, kind := range []string{"movies", "tv", "music", "books"} {
		d.drift("GET", "/api/v1/profiles", url.Values{"kind": []string{kind}}, nil, nil, http.StatusOK)
	}

	// ---- metrics (Prometheus text exposition) ----
	d.drift("GET", "/metrics", nil, nil, nil, http.StatusOK)

	// ---- events (WebSocket: 401 on bad token, hello frame on the good one) ----
	d.driftWebSocket(t)
}
