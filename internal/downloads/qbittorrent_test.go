package downloads

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// stubQB is a recorded/stubbed qBittorrent Web API (#36 "integration tests
// against a recorded/stubbed qBittorrent API"). It implements just enough of
// the real v2 surface — add, info, delete, login — with controllable torrent
// state, and asserts that credentials are presented correctly (API key header
// or SID cookie) but never echoed back.
type stubQB struct {
	t *testing.T

	ts       *httptest.Server
	mu       sync.Mutex
	apiKey   string // expected X-Api-Key, when key auth is used
	user     string // expected login credentials, when cookie auth is used
	pass     string
	logins   int
	added    map[string]string // name -> requested savepath
	torrents map[string]qbTorrent
	removed  []string
	// progressBump: on each /info call, advance every incomplete torrent by
	// this much (simulates an in-flight download finishing over polls).
	progressBump float64
	// contentBase is where the stub writes the "downloaded" file for a
	// completed torrent (its content_path).
	contentBase string
	// wantKeyAuth: when set, assert the X-Api-Key header is present and
	// correct on every request (key-auth mode).
	wantKeyAuth bool
}

func newStubQB(t *testing.T, contentBase string) *stubQB {
	t.Helper()
	return &stubQB{
		t:           t,
		added:       map[string]string{},
		torrents:    map[string]qbTorrent{},
		contentBase: contentBase,
	}
}

// url starts the stub server (idempotent) and returns its base URL.
func (s *stubQB) url() string {
	if s.ts == nil {
		s.ts = httptest.NewServer(s)
	}
	return s.ts.URL
}

func (s *stubQB) setTorrent(name, state string, progress float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.putTorrent(name, state, progress)
}

// putTorrent records a torrent and materialises its content file once complete.
// Callers must hold s.mu.
func (s *stubQB) putTorrent(name, state string, progress float64) {
	content := filepath.Join(s.contentBase, name)
	if progress >= 1.0 {
		if err := os.MkdirAll(content, 0o755); err != nil {
			s.t.Fatalf("make content dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(content, name+".mkv"), []byte("fake-movie-data"), 0o644); err != nil {
			s.t.Fatalf("write content file: %v", err)
		}
	}
	s.torrents[name] = qbTorrent{
		Hash:        "hash-" + name,
		Name:        name,
		State:       state,
		Progress:    progress,
		ContentPath: content,
	}
}

func (s *stubQB) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Auth checks.
	if s.wantKeyAuth {
		got := r.Header.Get("X-Api-Key")
		if got != s.apiKey {
			s.t.Errorf("X-Api-Key = %q, want %q (path %s)", got, s.apiKey, r.URL.Path)
		}
	}
	if s.user != "" && r.URL.Path != "/api/v2/auth/login" {
		var sid string
		if c, err := r.Cookie("SID"); err == nil {
			sid = c.Value
		}
		if sid == "" {
			s.t.Errorf("missing SID cookie on %s (cookie auth not established)", r.URL.Path)
		}
	}

	switch r.URL.Path {
	case "/api/v2/auth/login":
		_ = r.ParseForm()
		if r.FormValue("username") != s.user || r.FormValue("password") != s.pass {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"wrong credentials"}`))
			return
		}
		s.logins++
		http.SetCookie(w, &http.Cookie{Name: "SID", Value: "test-sid-123", Path: "/"})
		_, _ = w.Write([]byte(`{"status":"Ok."}`))

	case "/api/v2/torrents/add":
		_ = r.ParseForm()
		link := r.FormValue("urls")
		save := r.FormValue("savepath")
		name := nameFromLink(link)
		s.added[name] = save
		// ServeHTTP holds s.mu, so use the lock-free putTorrent.
		s.putTorrent(name, "uploading", 0.0)
		_, _ = w.Write([]byte(`{"msg":"Ok."}`))

	case "/api/v2/torrents/info":
		for _, t := range s.torrents {
			if s.progressBump > 0 && t.Progress < 1.0 {
				t.Progress += s.progressBump
				if t.Progress >= 1.0 {
					t.Progress = 1.0
					t.State = "uploading"
				}
				// Re-put so a torrent crossing 1.0 materialises its file.
				s.putTorrent(t.Name, t.State, t.Progress)
			}
		}
		_, _ = w.Write(s.torrentsAsJSON())

	case "/api/v2/torrents/delete":
		// Go's FormValue does not parse the body for DELETE, so read it
		// directly (the client sends hashes as the urlencoded body).
		db, _ := io.ReadAll(r.Body)
		dv, _ := url.ParseQuery(string(db))
		hash := dv.Get("hashes")
		for name, t := range s.torrents {
			if t.Hash == hash {
				delete(s.torrents, name)
				s.removed = append(s.removed, name)
			}
		}
		_, _ = w.Write([]byte(`{"msg":"Ok."}`))

	default:
		http.NotFound(w, r)
	}
}

// torrentsAsJSON serialises the current torrent set for /info.
func (s *stubQB) torrentsAsJSON() []byte {
	list := make([]qbTorrent, 0, len(s.torrents))
	for _, t := range s.torrents {
		list = append(list, t)
	}
	b, _ := json.Marshal(list)
	return b
}

// nameFromLink derives a stable torrent name from a download URL: the magnet
// dn= (download name) parameter, or the URL basename for http(s) links. qB
// names a torrent after its content, so the stub uses the same source to make
// Add and Status line up on the release title.
func nameFromLink(link string) string {
	if strings.HasPrefix(link, "magnet:?") {
		for _, part := range strings.Split(link, "&") {
			if strings.HasPrefix(part, "dn=") {
				return part[len("dn="):]
			}
		}
		for _, part := range strings.Split(link, "&") {
			if strings.HasPrefix(part, "xt=urn:btih:") {
				return part[len("xt=urn:btih:"):]
			}
		}
		return "magnet"
	}
	base := link
	if i := strings.LastIndexByte(base, '/'); i >= 0 {
		base = base[i+1:]
	}
	if i := strings.IndexByte(base, '?'); i >= 0 {
		base = base[:i]
	}
	// A .nzb is a container extension, not part of the release name: SABnzbd
	// names items after the NZB's internal release title, so strip it.
	base = strings.TrimSuffix(base, ".nzb")
	return base
}

func (s *stubQB) host() string { return s.url() }

// TestQB_Add_SendsURLAndSavepath verifies Add POSTs the magnet URL and the
// release's PathDir as savepath to the Web API.
func TestQB_Add_SendsURLAndSavepath(t *testing.T) {
	s := newStubQB(t, t.TempDir())
	s.ts = httptest.NewServer(s)
	s.apiKey = "k3y"
	s.wantKeyAuth = true

	c, err := NewQBittorrentClient(QBittorrentConfig{Base: s.url(), APIKey: "k3y"})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	ctx := context.Background()
	if err := c.Add(ctx, Release{Title: "Movie.2020.1080p", URL: "magnet:?xt=urn:btih:abc123&dn=Movie.2020.1080p", PathDir: "/downloads"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	s.mu.Lock()
	_, has := s.added["Movie.2020.1080p"]
	save := s.added["Movie.2020.1080p"]
	s.mu.Unlock()
	if !has {
		t.Fatal("torrent was not added")
	}
	if save != "/downloads" {
		t.Errorf("savepath = %q, want /downloads", save)
	}
}

// TestQB_NoURL_Fails ensures a release without a URL is rejected by the real
// client (the mock is the only client that accepts URL-less releases).
func TestQB_NoURL_Fails(t *testing.T) {
	c, _ := NewQBittorrentClient(QBittorrentConfig{Base: "http://localhost:1", APIKey: "k"})
	err := c.Add(context.Background(), Release{Title: "no-url"})
	if err == nil || !strings.Contains(err.Error(), "no download URL") {
		t.Fatalf("Add(no URL) err = %v, want 'no download URL'", err)
	}
}

// TestQB_Status_Progression drives the polling loop: in-progress -> complete
// (resolving the downloaded file) -> removal.
func TestQB_Status_Progression(t *testing.T) {
	content := t.TempDir()
	s := newStubQB(t, content)
	s.ts = httptest.NewServer(s)
	s.apiKey = "k3y"
	s.wantKeyAuth = true

	c, _ := NewQBittorrentClient(QBittorrentConfig{Base: s.url(), APIKey: "k3y"})
	ctx := context.Background()

	name := "Movie.2020.1080p"
	if err := c.Add(ctx, Release{Title: name, URL: "magnet:?xt=urn:btih:abc123&dn=" + name}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// The stub advances the torrent by 0.5 on each /info poll: poll 1 sees
	// 50% (in progress), poll 2 sees 100% (complete, file materialised).
	s.progressBump = 0.5

	// Poll 1: in progress.
	st, err := c.Status(ctx, name)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.Complete {
		t.Fatal("status should not be complete on first poll")
	}

	// Poll 2: complete, with a resolved file under the content path.
	st, err = c.Status(ctx, name)
	if err != nil {
		t.Fatalf("Status(2): %v", err)
	}
	if !st.Complete || st.Progress != 100 || st.File == "" {
		t.Fatalf("Status(2) = %+v, want complete with file", st)
	}
	if _, err := os.Stat(st.File); err != nil {
		t.Errorf("resolved file does not exist: %v", err)
	}

	// Removal: drop the torrent, then Remove again is a no-op.
	if err := c.Remove(ctx, name); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if err := c.Remove(ctx, name); err != nil {
		t.Fatalf("Remove(unknown) = %v, want nil", err)
	}
	s.mu.Lock()
	n := len(s.removed)
	s.mu.Unlock()
	if n != 1 {
		t.Errorf("removed = %d, want 1", n)
	}
}

// TestQB_Status_ErrorState maps a failed torrent to an error so the pipeline
// fails the queue entry instead of polling forever.
func TestQB_Status_ErrorState(t *testing.T) {
	s := newStubQB(t, t.TempDir())
	s.ts = httptest.NewServer(s)
	s.apiKey = "k"
	s.wantKeyAuth = true
	c, _ := NewQBittorrentClient(QBittorrentConfig{Base: s.url(), APIKey: "k"})

	s.setTorrent("bad", "error", 0.3)
	_, err := c.Status(context.Background(), "bad")
	if err == nil || !strings.Contains(err.Error(), "state") {
		t.Fatalf("Status(err state) = %v, want state error", err)
	}
}

// TestQB_CookieAuth uses the username/password login path (SID cookie), not
// the API-key header.
func TestQB_CookieAuth(t *testing.T) {
	s := newStubQB(t, t.TempDir())
	s.ts = httptest.NewServer(s)
	s.user, s.pass = "admin", "s3cret"

	c, err := NewQBittorrentClient(QBittorrentConfig{Base: s.url(), Username: "admin", Password: "s3cret"})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	ctx := context.Background()
	name := "Movie.2020.1080p"
	if err := c.Add(ctx, Release{Title: name, URL: "magnet:?xt=urn:btih:abc123&dn=" + name}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := c.Status(ctx, name); err != nil {
		t.Fatalf("Status: %v", err)
	}
	s.mu.Lock()
	logins := s.logins
	s.mu.Unlock()
	if logins != 1 {
		t.Errorf("logins = %d, want 1 (cookie reuse, no re-login)", logins)
	}
}

// TestQB_BadBase_Fails fast on a non-http base.
func TestQB_BadBase_Fails(t *testing.T) {
	if _, err := NewQBittorrentClient(QBittorrentConfig{Base: "ftp://x", APIKey: "k"}); err == nil {
		t.Fatal("expected error for non-http base")
	}
	if _, err := NewQBittorrentClient(QBittorrentConfig{Base: "", APIKey: "k"}); err == nil {
		t.Fatal("expected error for empty base")
	}
	if _, err := NewQBittorrentClient(QBittorrentConfig{Base: "http://x"}); err == nil {
		t.Fatal("expected error for no credentials")
	}
}
