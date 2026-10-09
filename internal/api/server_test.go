package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	booksdom "github.com/brandenk514/mediarr/internal/domains/books"
	musicdom "github.com/brandenk514/mediarr/internal/domains/music"
	"github.com/brandenk514/mediarr/internal/health"
	bookssvc "github.com/brandenk514/mediarr/internal/services/books"
	musicsvc "github.com/brandenk514/mediarr/internal/services/music"
)

// newTestServer builds a Server with a healthy checker.
func newTestServer() *Server {
	return NewServer(health.NewChecker(nil, nil))
}

func TestRoutesRegistered(t *testing.T) {
	s := newTestServer()
	for _, path := range []string{"/healthz", "/readyz", "/version"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		// healthz/readyz/version should all return 200 with a nil store/cache.
		// (With nil deps, readiness will be degraded but the route must exist.)
		if rec.Code == http.StatusNotFound {
			t.Errorf("route %s not registered (404)", path)
		}
	}
}

func TestHealthz_200(t *testing.T) {
	s := newTestServer()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if _, ok := body["version"]; !ok {
		t.Error("version missing from body")
	}
}

func TestVersion_200(t *testing.T) {
	s := newTestServer()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/version", nil)
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestRequestID_HeaderSet(t *testing.T) {
	s := newTestServer()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	s.Handler().ServeHTTP(rec, req)
	if rec.Header().Get("X-Request-Id") == "" {
		t.Error("X-Request-Id header should be set")
	}
}

func TestRequestID_EchoedIfInbound(t *testing.T) {
	s := newTestServer()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("X-Request-Id", "trace-abc123")
	s.Handler().ServeHTTP(rec, req)
	if got := rec.Header().Get("X-Request-Id"); got != "trace-abc123" {
		t.Errorf("X-Request-Id = %q, want echoed trace-abc123", got)
	}
}

func TestRequestID_RejectsGarbage(t *testing.T) {
	s := newTestServer()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("X-Request-Id", "bad;id with spaces&!@#")
	s.Handler().ServeHTTP(rec, req)
	got := rec.Header().Get("X-Request-Id")
	if got == "bad;id with spaces&!@#" {
		t.Error("garbage request ID should be replaced")
	}
}

func TestRecoverPanic_Returns500(t *testing.T) {
	h := recoverPanic(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestNewRequestID_Format(t *testing.T) {
	id := newRequestID()
	if len(id) != 16 {
		t.Errorf("request ID length = %d, want 16", len(id))
	}
	for _, c := range id {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'z')) {
			t.Errorf("unexpected char %q in request ID %q", c, id)
		}
	}
}

func TestIsValidID(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"trace-abc123", true},
		{"abc", false}, // too short
		{"a", false},
		{"", false},
		{"bad;id", false},
		{"good_id.value", true},
		{string(make([]byte, 200)), false}, // too long
	}
	for _, c := range cases {
		if got := isValidID(c.in); got != c.want {
			t.Errorf("isValidID(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// TestMusicRoutesRegister wires the full music route table onto a server so
// that an ambiguous pattern in SetMusic (e.g. GET /api/v1/music/{id}/albums vs
// GET /api/v1/music/albums/{id}, which Go 1.22+'s ServeMux panics on at
// registration) fails here in the unit suite instead of crashing the process
// at startup in the Docker smoke test. Registration-time panics are
// unrecoverable from the middleware chain, so the test must survive one.
func TestMusicRoutesRegister(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("SetMusic panicked while registering routes: %v", r)
		}
	}()
	s := newTestServer()
	s.SetMusic(&MusicDeps{Svc: musicsvc.New(musicsvc.Deps{Repo: stubMusicRepo{}})})
}

// stubMusicRepo implements musicdom.Repo with no-op methods. SetMusic only
// registers routes (the deps are never called), so every method can panic
// loudly if a handler is ever accidentally invoked.
type stubMusicRepo struct{}

func (stubMusicRepo) CreateArtist(context.Context, musicdom.Artist) (int64, error) {
	panic("stubMusicRepo.CreateArtist should not be called")
}
func (stubMusicRepo) GetArtist(context.Context, int64) (*musicdom.Artist, error) {
	panic("stubMusicRepo.GetArtist should not be called")
}
func (stubMusicRepo) ListArtists(context.Context) ([]musicdom.Artist, error) {
	panic("stubMusicRepo.ListArtists should not be called")
}
func (stubMusicRepo) SetArtistMonitored(context.Context, int64, bool) error {
	panic("stubMusicRepo.SetArtistMonitored should not be called")
}
func (stubMusicRepo) CreateAlbum(context.Context, musicdom.Album) (int64, error) {
	panic("stubMusicRepo.CreateAlbum should not be called")
}
func (stubMusicRepo) GetAlbum(context.Context, int64) (*musicdom.Album, error) {
	panic("stubMusicRepo.GetAlbum should not be called")
}
func (stubMusicRepo) ListAlbums(context.Context, int64) ([]musicdom.Album, error) {
	panic("stubMusicRepo.ListAlbums should not be called")
}
func (stubMusicRepo) SetAlbumMonitored(context.Context, int64, bool) error {
	panic("stubMusicRepo.SetAlbumMonitored should not be called")
}
func (stubMusicRepo) CreateTrack(context.Context, musicdom.Track) (int64, error) {
	panic("stubMusicRepo.CreateTrack should not be called")
}
func (stubMusicRepo) GetTrack(context.Context, int64, int, int) (*musicdom.Track, error) {
	panic("stubMusicRepo.GetTrack should not be called")
}
func (stubMusicRepo) ListTracks(context.Context, int64) ([]musicdom.Track, error) {
	panic("stubMusicRepo.ListTracks should not be called")
}
func (stubMusicRepo) SetTrackMonitored(context.Context, int64, int, int, bool) error {
	panic("stubMusicRepo.SetTrackMonitored should not be called")
}
func (stubMusicRepo) EnsureWantedAlbum(context.Context, int64) error {
	panic("stubMusicRepo.EnsureWantedAlbum should not be called")
}
func (stubMusicRepo) EnsureWantedTrack(context.Context, int64, int64) error {
	panic("stubMusicRepo.EnsureWantedTrack should not be called")
}
func (stubMusicRepo) GetWanted(context.Context, int64, int64) (*musicdom.Wanted, error) {
	panic("stubMusicRepo.GetWanted should not be called")
}
func (stubMusicRepo) ListWanted(context.Context, int64) ([]musicdom.Wanted, error) {
	panic("stubMusicRepo.ListWanted should not be called")
}
func (stubMusicRepo) MarkWantedSatisfied(context.Context, int64, int64, string) error {
	panic("stubMusicRepo.MarkWantedSatisfied should not be called")
}
func (stubMusicRepo) CreateQueue(context.Context, musicdom.QueueEntry) (int64, error) {
	panic("stubMusicRepo.CreateQueue should not be called")
}
func (stubMusicRepo) UpdateQueue(context.Context, musicdom.QueueEntry) error {
	panic("stubMusicRepo.UpdateQueue should not be called")
}
func (stubMusicRepo) GetQueue(context.Context, int64) (*musicdom.QueueEntry, error) {
	panic("stubMusicRepo.GetQueue should not be called")
}
func (stubMusicRepo) ListQueue(context.Context, int64) ([]musicdom.QueueEntry, error) {
	panic("stubMusicRepo.ListQueue should not be called")
}
func (stubMusicRepo) AddHistory(context.Context, musicdom.HistoryEntry) error {
	panic("stubMusicRepo.AddHistory should not be called")
}
func (stubMusicRepo) ListHistory(context.Context, int64, int) ([]musicdom.HistoryEntry, error) {
	panic("stubMusicRepo.ListHistory should not be called")
}

// TestBooksRoutesRegister wires the full books route table onto a server so
// that an ambiguous pattern in SetBooks (e.g. a bare /api/v1/books/{id}
// colliding with a collection route, which Go 1.22+'s ServeMux panics on at
// registration) fails here in the unit suite instead of crashing the process
// at startup in the Docker smoke test. Registration-time panics are
// unrecoverable from the middleware chain, so the test must survive one.
func TestBooksRoutesRegister(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("SetBooks panicked while registering routes: %v", r)
		}
	}()
	s := newTestServer()
	s.SetBooks(&BooksDeps{Svc: bookssvc.New(bookssvc.Deps{Repo: stubBooksRepo{}})})
}

// stubBooksRepo implements booksdom.Repo with no-op (panicking) methods.
// SetBooks only registers routes (the deps are never called), so every method
// can panic loudly if a handler is ever accidentally invoked.
type stubBooksRepo struct{}

func (stubBooksRepo) CreateAuthor(context.Context, booksdom.Author) (int64, error) {
	panic("stubBooksRepo.CreateAuthor should not be called")
}
func (stubBooksRepo) GetAuthor(context.Context, int64) (*booksdom.Author, error) {
	panic("stubBooksRepo.GetAuthor should not be called")
}
func (stubBooksRepo) ListAuthors(context.Context) ([]booksdom.Author, error) {
	panic("stubBooksRepo.ListAuthors should not be called")
}
func (stubBooksRepo) SetAuthorMonitored(context.Context, int64, bool) error {
	panic("stubBooksRepo.SetAuthorMonitored should not be called")
}
func (stubBooksRepo) CreateTitle(context.Context, booksdom.Title) (int64, error) {
	panic("stubBooksRepo.CreateTitle should not be called")
}
func (stubBooksRepo) GetTitle(context.Context, int64) (*booksdom.Title, error) {
	panic("stubBooksRepo.GetTitle should not be called")
}
func (stubBooksRepo) ListTitles(context.Context, int64) ([]booksdom.Title, error) {
	panic("stubBooksRepo.ListTitles should not be called")
}
func (stubBooksRepo) SetTitleMonitored(context.Context, int64, bool) error {
	panic("stubBooksRepo.SetTitleMonitored should not be called")
}
func (stubBooksRepo) CreateEdition(context.Context, booksdom.Edition) (int64, error) {
	panic("stubBooksRepo.CreateEdition should not be called")
}
func (stubBooksRepo) GetEdition(context.Context, int64) (*booksdom.Edition, error) {
	panic("stubBooksRepo.GetEdition should not be called")
}
func (stubBooksRepo) ListEditions(context.Context, int64) ([]booksdom.Edition, error) {
	panic("stubBooksRepo.ListEditions should not be called")
}
func (stubBooksRepo) SetEditionMonitored(context.Context, int64, bool) error {
	panic("stubBooksRepo.SetEditionMonitored should not be called")
}
func (stubBooksRepo) EnsureWantedTitle(context.Context, int64) error {
	panic("stubBooksRepo.EnsureWantedTitle should not be called")
}
func (stubBooksRepo) EnsureWantedEdition(context.Context, int64, int64) error {
	panic("stubBooksRepo.EnsureWantedEdition should not be called")
}
func (stubBooksRepo) GetWanted(context.Context, int64, int64) (*booksdom.Wanted, error) {
	panic("stubBooksRepo.GetWanted should not be called")
}
func (stubBooksRepo) ListWanted(context.Context, int64) ([]booksdom.Wanted, error) {
	panic("stubBooksRepo.ListWanted should not be called")
}
func (stubBooksRepo) MarkWantedSatisfied(context.Context, int64, int64, string) error {
	panic("stubBooksRepo.MarkWantedSatisfied should not be called")
}
func (stubBooksRepo) CreateQueue(context.Context, booksdom.QueueEntry) (int64, error) {
	panic("stubBooksRepo.CreateQueue should not be called")
}
func (stubBooksRepo) UpdateQueue(context.Context, booksdom.QueueEntry) error {
	panic("stubBooksRepo.UpdateQueue should not be called")
}
func (stubBooksRepo) GetQueue(context.Context, int64) (*booksdom.QueueEntry, error) {
	panic("stubBooksRepo.GetQueue should not be called")
}
func (stubBooksRepo) ListQueue(context.Context, int64) ([]booksdom.QueueEntry, error) {
	panic("stubBooksRepo.ListQueue should not be called")
}
func (stubBooksRepo) AddHistory(context.Context, booksdom.HistoryEntry) error {
	panic("stubBooksRepo.AddHistory should not be called")
}
func (stubBooksRepo) ListHistory(context.Context, int64, int) ([]booksdom.HistoryEntry, error) {
	panic("stubBooksRepo.ListHistory should not be called")
}

// Ensure the server can be built and the handler chain works end-to-end.
func TestServe_Smoke(t *testing.T) {
	s := newTestServer()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- s.Serve(ctx, "127.0.0.1:0", 1*time.Second, 1*time.Second, time.Second)
	}()

	// Wait for server to be ready or fail.
	select {
	case err := <-done:
		t.Fatalf("server exited early: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	cancel()
	// Drain shutdown.
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("server did not shut down in time")
	}
}
