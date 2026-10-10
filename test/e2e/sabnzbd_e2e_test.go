package e2e_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path"
	"strings"
	"testing"

	dom "github.com/brandenk514/mediarr/internal/domains/movies"
	"github.com/brandenk514/mediarr/internal/indexers"
)

// sabStub is an in-process stub of the SABnzbd JSON API. It is what the *real*
// SABnzbdClient talks to: the same endpoint (/api?mode=...) and the same
// response shapes, but the "download" is simulated by writing the file straight
// into the complete directory under the NZB-name folder the moment the NZB is
// added, so the client's Status poll finds a completed history entry to import.
//
// Like real SABnzbd, a finished NZB lives in *history* (not the active queue)
// and is named after the NZB name. The stub derives that name from the added
// link's file name (the test sets it to the scene release name) so the client's
// Status(title) / Remove(title) match.
type sabStub struct {
	history  []sabHistorySlot // finished items
	complete string           // complete directory
	adds     int
	removed  []string
	sawKey   bool
}

type sabHistorySlot struct {
	id     string
	name   string
	status string // "Success" / "Failed"
}

func newSABStub(completeDir, _ string) *sabStub {
	return &sabStub{complete: completeDir}
}

func (s *sabStub) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api" {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		if q.Get("apikey") != "" {
			s.sawKey = true
		}
		mode := q.Get("mode")
		switch mode {
		case "addurl":
			s.handleAdd(w, q)
		case "queue":
			// Finished NZBs are in history, not the active queue.
			writeJSON(w, map[string]any{"slots": []any{}})
		case "history":
			s.handleHistory(w, q)
		default:
			writeJSON(w, map[string]any{"status": "success"})
		}
	})
}

// handleAdd registers the NZB as a finished history item and materialises the
// downloaded file under <completeDir>/<nzbname>/, matching real SABnzbd, which
// names output folders after the NZB name and post-processes into them.
func (s *sabStub) handleAdd(w http.ResponseWriter, q url.Values) {
	link := q.Get("link")
	name := nzbName(link)
	s.adds++
	s.history = append(s.history, sabHistorySlot{id: "nzb-" + name, name: name, status: "Success"})
	// Simulate the download + post-processing completing: write the file the
	// way the service's fileNameFor names it, under the NZB-name folder.
	if err := writeDownload(path.Join(s.complete, name), name); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, map[string]any{"status": "success", "id": "nzb-" + name})
}

func (s *sabStub) handleHistory(w http.ResponseWriter, q url.Values) {
	if del := q.Get("delete"); del != "" {
		var kept []sabHistorySlot
		for _, h := range s.history {
			if h.id == del {
				s.removed = append(s.removed, h.name)
				continue
			}
			kept = append(kept, h)
		}
		s.history = kept
		writeJSON(w, map[string]any{"status": "success"})
		return
	}
	slots := make([]map[string]any, 0, len(s.history))
	for _, h := range s.history {
		slots = append(slots, map[string]any{"id": h.id, "name": h.name, "status": h.status})
	}
	writeJSON(w, map[string]any{"slots": slots})
}

// nzbName derives the NZB name from a download link: the unescaped base file
// name with any ".nzb" extension stripped — exactly how real SABnzbd names a
// queue/history slot from the added file.
func nzbName(link string) string {
	u, err := url.Parse(link)
	if err != nil {
		return link
	}
	base := path.Base(u.Path)
	if u, err := url.PathUnescape(base); err == nil {
		base = u
	}
	return strings.TrimSuffix(base, ".nzb")
}

// newSABStubServer starts the stub and returns its URL.
func newSABStubServer(t *testing.T, completeDir string) (*sabStub, string) {
	t.Helper()
	s := newSABStub(completeDir, "")
	srv := httptest.NewServer(s.handler())
	t.Cleanup(srv.Close)
	return s, srv.URL
}

// --- Test: full pipeline through the REAL SABnzbd client (config swap) ---

// TestE2E_RealSABnzbdPipeline drives the entire movie pipeline with the
// config-driven swap selecting the real SABnzbd client (via
// config.LoadFromOS + downloads.NewClient), talking to the in-process stub
// over HTTP, with real Postgres persistence and a real Redis pub/sub queue.
// It asserts: queue watch (pub/sub state transitions), import into the media
// root, wanted satisfied by the best release, the API key being sent, and
// removal after import.
func TestE2E_RealSABnzbdPipeline(t *testing.T) {
	h := startHarness(t)
	dlDir := t.TempDir()
	mediaRoot := t.TempDir()
	const title = "Dune.Part.Two.2024.1080p.WEB.x264"

	// The real SABnzbd JSON API, stubbed in-process.
	sab, sabURL := newSABStubServer(t, dlDir)

	// Config-driven swap: this env makes config.LoadFromOS() select the
	// SABnzbd client and point it at the stub.
	h.setMediaEnv(t, "sabnzbd", sabURL, "sab-test-api-key", dlDir, mediaRoot)
	cfg := loadConfig(t)
	if cfg.ClientType != "sabnzbd" {
		t.Fatalf("config-driven swap: ClientType = %q, want sabnzbd", cfg.ClientType)
	}

	// The config-driven factory must build the real SABnzbd client.
	if name, err := clientNameFor(t, cfg); err != nil {
		t.Fatalf("config-driven client build: %v", err)
	} else if name != "SABnzbd" {
		t.Fatalf("download client = %q, want SABnzbd", name)
	}

	fake := indexers.NewFakeIndexer("fake")
	// Usenet releases carry an NZB link; the stub names the history slot from
	// the link's file name, so we set it to the scene release name.
	fake.AddRelease(indexers.SearchResult{
		Title: "Dune.Part.Two.2024.1080p.WEB.x264", SizeBytes: 5_100_000_000,
		Info: indexers.ReleaseInfo{
			Protocol: indexers.ProtocolNZB,
			URL:      "http://stub.nzb/dl/" + url.PathEscape(title) + ".nzb",
		},
	})
	fake.AddRelease(indexers.SearchResult{
		Title: "Dune.Part.Two.2024.720p.WEB.x264", SizeBytes: 2_500_000_000,
		Info: indexers.ReleaseInfo{
			Protocol: indexers.ProtocolNZB,
			URL:      "http://stub.nzb/dl/" + url.PathEscape("Dune.Part.Two.2024.720p.WEB.x264") + ".nzb",
		},
	})

	svc := h.buildMovieService(t, cfg, fake)

	// Subscribe to the queue pub/sub bus before running the pipeline so no
	// state-transition event is missed.
	col := h.startQueueCollector(t)

	id, imported := h.runPipeline(t, svc, "Dune Part Two", "2024")
	assertImported(t, imported, mediaRoot, "Dune Part Two", 2024)

	// Wanted is satisfied by the best (1080p) release.
	w, err := h.movieRepo.GetWanted(h.ctx, id)
	if err != nil {
		t.Fatalf("get wanted: %v", err)
	}
	if w.Status != dom.WantedSatisfied {
		t.Errorf("wanted status = %q, want satisfied", w.Status)
	}
	if w.ReleaseTitle != title {
		t.Errorf("satisfied by = %q, want %q (1080p, best match)", w.ReleaseTitle, title)
	}

	// Queue is complete and attributes the download to the SABnzbd client.
	qs, err := h.movieRepo.ListQueue(h.ctx)
	if err != nil {
		t.Fatalf("list queue: %v", err)
	}
	if len(qs) != 1 {
		t.Fatalf("queue len = %d, want 1", len(qs))
	}
	if qs[0].State != dom.QueueComplete {
		t.Errorf("queue state = %q, want complete", qs[0].State)
	}
	if qs[0].Progress != 100 {
		t.Errorf("queue progress = %d, want 100", qs[0].Progress)
	}
	if qs[0].DownloadClient != "SABnzbd" {
		t.Errorf("queue client = %q, want SABnzbd", qs[0].DownloadClient)
	}
	if qs[0].ReleaseTitle != title {
		t.Errorf("queue release = %q, want %q", qs[0].ReleaseTitle, title)
	}

	// The stub actually received the add (with the API key), and the release
	// was removed from the client's history after import (the Remover).
	if sab.adds != 1 {
		t.Errorf("sab add calls = %d, want 1", sab.adds)
	}
	if !sab.sawKey {
		t.Error("sab api key not observed on requests")
	}
	if len(sab.removed) != 1 {
		t.Errorf("sab removed = %v, want the imported release removed", sab.removed)
	}

	// Queue state transitions were fed to the pub/sub bus (queue watch).
	col.collect()
	seq := col.stateSequence()
	if !containsInOrder(seq, []string{string(dom.QueueQueued), string(dom.QueueDownloading), string(dom.QueueComplete)}) {
		t.Errorf("queue event states = %v, want to contain [queued downloading complete] in order", seq)
	}

	// History was recorded on Postgres.
	assertHistory(t, h, id)
}
