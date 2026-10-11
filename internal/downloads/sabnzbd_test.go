package downloads

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// jsonMarshal is a tiny test helper (returns "null" on the (impossible) error
// path) so the stub's JSON responses need no error plumbing.
func jsonMarshal(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

// jsonString JSON-encodes a string for the stub's responses.
func jsonString(s string) []byte {
	b, _ := json.Marshal(s)
	return b
}

// contains is a readability wrapper over strings.Contains for assertions.
func contains(s, sub string) bool { return strings.Contains(s, sub) }

// stubSAB is a recorded/stubbed SABnzbd JSON API (#37 "integration tests
// against a recorded/stubbed SABnzbd API"). It implements just enough of the
// real /api surface — addurl, queue, history, history-delete — with
// controllable item state, and asserts the API key is sent on every request.
//
// Unlike the qB stub, every request is a GET to /api with a query string, so
// the stub routes on the `mode` parameter.
type stubSAB struct {
	t *testing.T

	ts     *httptest.Server
	mu     sync.Mutex
	apiKey string // expected apikey query param on every request
	// queue holds active items keyed by name; history holds finished items.
	queue   map[string]sabSlot
	history map[string]sabSlot
	// contentBase is where the stub materialises a finished item's file
	// (<contentBase>/<name>/<name>.mkv), matching SABnzbd's output layout.
	contentBase string
	// removed records history IDs deleted via the delete action.
	removed []string
}

type sabSlot struct {
	name    string
	id      string
	status  string // "Downloading", "Completed", "Success", "Failed", ...
	MBDone  float64
	MBTotal float64
	PP      string
}

func newStubSAB(t *testing.T, contentBase string) *stubSAB {
	t.Helper()
	return &stubSAB{
		t:           t,
		queue:       map[string]sabSlot{},
		history:     map[string]sabSlot{},
		contentBase: contentBase,
	}
}

func (s *stubSAB) url() string {
	if s.ts == nil {
		s.ts = httptest.NewServer(s)
	}
	return s.ts.URL
}

// finish moves an active item into history (status Success) and writes its
// file so Status can resolve it. Test-driven and deterministic.
func (s *stubSAB) finish(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	slot, ok := s.queue[name]
	if ok {
		delete(s.queue, name)
	} else {
		slot = sabSlot{name: name, id: "id-" + name}
	}
	slot.status = "Success"
	slot.PP = "Success"
	s.history[name] = slot
	dir := filepath.Join(s.contentBase, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		s.t.Fatalf("make content dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".mkv"), []byte("fake-usenet-data"), 0o644); err != nil {
		s.t.Fatalf("write content file: %v", err)
	}
}

func (s *stubSAB) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api" {
		http.NotFound(w, r)
		return
	}
	q := r.URL.Query()
	// Assert the API key is presented on every request.
	if s.apiKey != "" && q.Get("apikey") != s.apiKey {
		s.t.Errorf("apikey = %q, want %q (mode=%s)", q.Get("apikey"), s.apiKey, q.Get("mode"))
	}
	if q.Get("output") != "json" {
		s.t.Errorf("output = %q, want json", q.Get("output"))
	}

	mode := q.Get("mode")
	s.mu.Lock()
	defer s.mu.Unlock()

	switch mode {
	case "addurl":
		link := q.Get("link")
		name := nameFromLink(link)
		s.queue[name] = sabSlot{name: name, id: "id-" + name, status: "Downloading", MBDone: 0, MBTotal: 100, PP: "Downloading"}
		resp := `{"status":"success","name":` + string(jsonString(name)) + `}`
		_, _ = w.Write([]byte(resp))

	case "queue":
		slots := make([]map[string]any, 0, len(s.queue))
		for _, it := range s.queue {
			slots = append(slots, map[string]any{
				"id": it.id, "name": it.name, "pp": it.PP, "status": it.status,
				"mbtotal": it.MBTotal, "mbdone": it.MBDone,
			})
		}
		_, _ = w.Write(jsonMarshal(map[string]any{"slots": slots, "slots_limit": len(slots)}))

	case "history":
		// Delete action: history?delete=<id>.
		if del := q.Get("delete"); del != "" {
			for name, it := range s.history {
				if it.id == del {
					delete(s.history, name)
					s.removed = append(s.removed, name)
				}
			}
			_, _ = w.Write([]byte(`{"status":"success"}`))
			return
		}
		slots := make([]map[string]any, 0, len(s.history))
		for _, it := range s.history {
			slots = append(slots, map[string]any{"id": it.id, "name": it.name, "status": it.status})
		}
		_, _ = w.Write(jsonMarshal(map[string]any{"slots": slots}))

	default:
		http.Error(w, `unknown mode `+mode, http.StatusBadRequest)
	}
}

// TestSAB_Add_SendsLinkAndKey verifies Add issues mode=addurl with the release
// URL and the API key.
func TestSAB_Add_SendsLinkAndKey(t *testing.T) {
	s := newStubSAB(t, t.TempDir())
	s.apiKey = "sab-key"
	c, err := NewSABnzbdClient(SABnzbdConfig{Base: s.url(), APIKey: "sab-key", CompleteDir: t.TempDir()})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	ctx := context.Background()
	name := "Movie.2020.1080p"
	if err := c.Add(ctx, Release{Title: name, URL: "https://nzb.example/" + name + ".nzb"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	// SABnzbd names items after the NZB's internal release title, so the
	// stub (and the client's poll-by-title) line up on the release title.
	s.mu.Lock()
	_, inQueue := s.queue[name]
	s.mu.Unlock()
	if !inQueue {
		t.Fatalf("item %q not queued", name)
	}
}

// TestSAB_NoURL_Fails ensures a URL-less release is rejected (the mock is the
// only client that accepts them).
func TestSAB_NoURL_Fails(t *testing.T) {
	c, _ := NewSABnzbdClient(SABnzbdConfig{Base: "http://localhost:1", APIKey: "k"})
	err := c.Add(context.Background(), Release{Title: "no-url"})
	if err == nil || !contains(err.Error(), "no download URL") {
		t.Fatalf("Add(no URL) = %v, want 'no download URL'", err)
	}
}

// TestSAB_Status_Progression drives the polling loop: in the active queue
// (downloading) -> finished (moved to history, file written) -> complete, then
// removal from history.
func TestSAB_Status_Progression(t *testing.T) {
	content := t.TempDir()
	s := newStubSAB(t, content)
	s.apiKey = "sab-key"
	c, _ := NewSABnzbdClient(SABnzbdConfig{Base: s.url(), APIKey: "sab-key", CompleteDir: content})
	ctx := context.Background()
	name := "Movie.2020.1080p"

	if err := c.Add(ctx, Release{Title: name, URL: "https://nzb.example/" + name + ".nzb"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Poll 1: active queue, downloading, not complete.
	st, err := c.Status(ctx, name)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.Complete {
		t.Fatal("status should not be complete while downloading")
	}

	// Finish the item: move to history + write the file.
	s.finish(name)

	// Poll 2: found in history, file resolved, complete.
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

	// Removal: delete the history entry; a second Remove is a no-op.
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

// TestSAB_Status_Failed maps a failed item to an error so the pipeline fails
// the queue entry instead of polling forever.
func TestSAB_Status_Failed(t *testing.T) {
	s := newStubSAB(t, t.TempDir())
	s.apiKey = "k"
	c, _ := NewSABnzbdClient(SABnzbdConfig{Base: s.url(), APIKey: "k", CompleteDir: t.TempDir()})

	s.mu.Lock()
	s.queue["bad"] = sabSlot{name: "bad", id: "id-bad", status: "Failed", PP: "failed-post-processing", MBDone: 20, MBTotal: 100}
	s.mu.Unlock()

	_, err := c.Status(context.Background(), "bad")
	if err == nil || !contains(err.Error(), "failed") {
		t.Fatalf("Status(failed) = %v, want failed error", err)
	}
}

// TestSAB_BadBase_Fails fast on a missing key or non-http base.
func TestSAB_BadBase_Fails(t *testing.T) {
	if _, err := NewSABnzbdClient(SABnzbdConfig{Base: "ftp://x", APIKey: "k"}); err == nil {
		t.Fatal("expected error for non-http base")
	}
	if _, err := NewSABnzbdClient(SABnzbdConfig{Base: "", APIKey: "k"}); err == nil {
		t.Fatal("expected error for empty base")
	}
	if _, err := NewSABnzbdClient(SABnzbdConfig{Base: "http://x"}); err == nil {
		t.Fatal("expected error for missing API key")
	}
}
