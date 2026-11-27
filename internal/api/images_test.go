package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/brandenk514/mediarr/internal/health"
	"github.com/brandenk514/mediarr/internal/services/imageproxy"
	"github.com/brandenk514/mediarr/internal/settings"
)

// newImagesTestServer builds a Server with the images route wired to a
// stub TMDB upstream, plus its fake settings store.
func newImagesTestServer(t *testing.T, body []byte, status int, maxAge string) (*Server, *fakeSettingsStore, *int) {
	t.Helper()
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if maxAge != "" {
			w.Header().Set("Cache-Control", maxAge)
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	s := NewServer(health.NewChecker(nil, nil))
	store := newFakeSettingsStore()
	s.SetImages(&ImagesDeps{
		Proxy:    imageproxy.New(imageproxy.Config{CacheDir: t.TempDir(), TMDBBaseURL: srv.URL}),
		Settings: store,
	})
	return s, store, &hits
}

func TestImages_RoutePassthrough(t *testing.T) {
	s, store, hits := newImagesTestServer(t, []byte("jpeg-bytes"), http.StatusOK, "public, max-age=3600")
	_ = store

	// First request: MISS (fetched from the stub upstream).
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/images/tmdb/w342/poster.jpg", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if got := w.Header().Get("X-Cache-Status"); got != "MISS" {
		t.Fatalf("X-Cache-Status = %q, want MISS", got)
	}
	if got := w.Header().Get("Content-Type"); got != "image/jpeg" {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := w.Header().Get("Cache-Control"); got != "public, max-age=3600" {
		t.Fatalf("Cache-Control = %q", got)
	}
	if w.Body.String() != "jpeg-bytes" {
		t.Fatalf("body = %q", w.Body.String())
	}

	// Second request: HIT from the on-disk cache, no second upstream call.
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/images/tmdb/w342/poster.jpg", nil))
	if w.Code != http.StatusOK || w.Header().Get("X-Cache-Status") != "HIT" {
		t.Fatalf("second: status=%d cache=%q, want 200/HIT", w.Code, w.Header().Get("X-Cache-Status"))
	}
	if *hits != 1 {
		t.Fatalf("upstream hits = %d, want 1", *hits)
	}
}

func TestImages_CacheToggle(t *testing.T) {
	// Never-saved general document → default ON.
	s, store, hits := newImagesTestServer(t, []byte("x"), http.StatusOK, "")
	s.Handler().ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/images/tmdb/w342/a.jpg", nil))
	if *hits != 1 {
		t.Fatalf("default should be ON; hits = %d", *hits)
	}

	// Explicit cache_images: false → 404, upstream untouched.
	if err := store.PutSectionDocument(context.Background(), settings.SectionGeneral, []byte(`{"cache_images":false}`)); err != nil {
		t.Fatalf("put section: %v", err)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/images/tmdb/w342/a.jpg", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("disabled: status = %d, want 404", w.Code)
	}
	if *hits != 1 {
		t.Fatalf("disabled request reached upstream (hits=%d)", *hits)
	}

	// A saved document WITHOUT the field still means ON (absent → default).
	if err := store.PutSectionDocument(context.Background(), settings.SectionGeneral, []byte(`{"port":8080}`)); err != nil {
		t.Fatalf("put section: %v", err)
	}
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/images/tmdb/w342/b.jpg", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("absent field: status = %d, want 200", w.Code)
	}
}

func TestImages_SentinelStatusMapping(t *testing.T) {
	s, _, hits := newImagesTestServer(t, []byte("x"), http.StatusOK, "")

	cases := []struct {
		name   string
		target string
		want   int
	}{
		{"bad segment chars", "/api/v1/images/tmdb/w342/a%20b.jpg", http.StatusBadRequest},
		{"unsupported type", "/api/v1/images/posters/w342/a.jpg", http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.target, nil))
			if w.Code != tc.want {
				t.Fatalf("GET %s: status = %d, want %d", tc.target, w.Code, tc.want)
			}
		})
	}

	// The '://' smuggle and a literal '..' segment cannot survive the mux
	// (ServeMux cleans/redirects such paths), so their maps are exercised
	// through the handler with crafted path values — mirroring what a
	// decoded '%2e%2e' smuggle would deliver.
	direct := []struct {
		name string
		path string
		want int
	}{
		{"scheme smuggle", "a://b.jpg", http.StatusForbidden},
		{"dotdot segment", "..", http.StatusBadRequest},
	}
	for _, tc := range direct {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/api/v1/images/tmdb/w342/"+tc.path, nil)
			r.SetPathValue("type", "tmdb")
			r.SetPathValue("size", "w342")
			r.SetPathValue("path", tc.path)
			w := httptest.NewRecorder()
			s.GetImage(w, r)
			if w.Code != tc.want {
				t.Fatalf("path %q: status = %d, want %d", tc.path, w.Code, tc.want)
			}
		})
	}
	if *hits != 0 {
		t.Fatalf("rejected requests reached upstream (hits=%d)", *hits)
	}
}

func TestImages_UpstreamErrors(t *testing.T) {
	// 404 upstream → 404.
	s404, _, _ := newImagesTestServer(t, []byte("x"), http.StatusNotFound, "")
	w := httptest.NewRecorder()
	s404.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/images/tmdb/w342/nope.jpg", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("upstream 404: status = %d, want 404", w.Code)
	}

	// 500 upstream → 502.
	s500, _, _ := newImagesTestServer(t, []byte("x"), http.StatusInternalServerError, "")
	w = httptest.NewRecorder()
	s500.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/images/tmdb/w342/boom.jpg", nil))
	if w.Code != http.StatusBadGateway {
		t.Fatalf("upstream 500: status = %d, want 502", w.Code)
	}
}
