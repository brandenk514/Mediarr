package e2e_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	dom "github.com/brandenk514/mediarr/internal/domains/movies"
	"github.com/brandenk514/mediarr/internal/indexers"
)

// qbStub is an in-process stub of the qBittorrent Web API v2. It is what the
// *real* QBittorrentClient talks to: the same endpoints and request shapes,
// but the "download" is simulated by writing the file straight into the
// torrent's savepath the moment it is added, so the client's Status poll finds
// a completed download to import (largestFile under content_path).
//
// A torrent is named by the magnet's `dn` (destination name) parameter, exactly
// as real qBittorrent names the entry; the tests set `dn` to the scene release
// name so the client's Status(title) / Remove(title) match.
type qbStub struct {
	entries   map[string]qbEntry // name -> entry
	savedPath string             // fallback save dir when add omits savepath
	adds      int
	removed   []string
}

type qbEntry struct {
	name        string
	contentPath string
}

func newQBStub(savePath string) *qbStub {
	return &qbStub{entries: map[string]qbEntry{}, savedPath: savePath}
}

func (s *qbStub) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/torrents/add", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		s.adds++
		name := dnName(r.Form.Get("urls"))
		savePath := r.Form.Get("savepath")
		if savePath == "" {
			savePath = s.savedPath
		}
		// Simulate the download completing: qBittorrent would write the file
		// under the savepath; the stub does it here, under the scene name the
		// service will poll for.
		if err := writeDownload(savePath, name); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		s.entries[name] = qbEntry{name: name, contentPath: savePath}
		writeJSON(w, map[string]any{"msg": "Ok."})
	})
	mux.HandleFunc("/api/v2/torrents/info", func(w http.ResponseWriter, r *http.Request) {
		var out []map[string]any
		for _, e := range s.entries {
			out = append(out, map[string]any{
				"hash":         "stub-" + e.name,
				"name":         e.name,
				"progress":     1.0,
				"state":        "uploading",
				"content_path": e.contentPath,
			})
		}
		writeJSON(w, out)
	})
	mux.HandleFunc("/api/v2/torrents/delete", func(w http.ResponseWriter, r *http.Request) {
		// The real client sends `hashes` as a form-encoded request body on the
		// DELETE; http.Request.ParseForm does not parse bodies for non-POST
		// methods, so read the body explicitly.
		var hashes string
		if r.Body != nil {
			b, _ := io.ReadAll(r.Body)
			if f, err := url.ParseQuery(string(b)); err == nil {
				hashes = f.Get("hashes")
			}
		}
		if hashes == "" {
			hashes = r.URL.Query().Get("hashes")
		}
		for name := range s.entries {
			if strings.Contains(hashes, "stub-"+name) {
				s.removed = append(s.removed, name)
				delete(s.entries, name)
			}
		}
		writeJSON(w, map[string]any{"msg": "Ok."})
	})
	return mux
}

// dnName extracts the magnet `dn` (destination name) from a torrent URL.
func dnName(magnet string) string {
	if i := strings.Index(magnet, "dn="); i >= 0 {
		rest := magnet[i+len("dn="):]
		if j := strings.IndexAny(rest, "& "); j >= 0 {
			rest = rest[:j]
		}
		if u, err := url.QueryUnescape(rest); err == nil {
			return u
		}
		return rest
	}
	return magnet
}

// writeDownload writes the placeholder "downloaded" file into dir under the
// scene name the service polls for (fileNameFor: dots/hyphens -> spaces,
// " + ".mkv") — the same naming the real client resolves via largestFile.
func writeDownload(dir, sceneName string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	name := strings.ReplaceAll(strings.ReplaceAll(sceneName, ".", " "), "-", " ")
	name = strings.Join(strings.Fields(name), " ") + ".mkv"
	return os.WriteFile(filepath.Join(dir, name), []byte("qbittorrent-stub-download: "+sceneName+"\n"), 0o644)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// newQBStubServer starts the stub and returns its URL.
func newQBStubServer(t *testing.T, savePath string) (*qbStub, string) {
	t.Helper()
	s := newQBStub(savePath)
	srv := httptest.NewServer(s.handler())
	t.Cleanup(srv.Close)
	return s, srv.URL
}

// --- Test: full pipeline through the REAL qBittorrent client (config swap) ---

// TestE2E_RealQBittorrentPipeline drives the entire movie pipeline with the
// config-driven swap selecting the real qBittorrent client (via
// config.LoadFromOS + downloads.NewClient), talking to the in-process stub
// over HTTP, with real Postgres persistence and a real Redis pub/sub queue.
// It asserts: queue watch (pub/sub state transitions), import into the media
// root, wanted satisfied by the best release, and removal after import.
func TestE2E_RealQBittorrentPipeline(t *testing.T) {
	h := startHarness(t)
	dlDir := t.TempDir()
	mediaRoot := t.TempDir()
	const title = "Dune.Part.Two.2024.1080p.WEB.x264"

	// The real qBittorrent Web API, stubbed in-process.
	qb, qbURL := newQBStubServer(t, dlDir)

	// Config-driven swap: this env makes config.LoadFromOS() select the
	// qBittorrent client and point it at the stub.
	h.setMediaEnv(t, "qbittorrent", qbURL, "test-api-key", dlDir, mediaRoot)
	cfg := loadConfig(t)
	if cfg.ClientType != "qbittorrent" {
		t.Fatalf("config-driven swap: ClientType = %q, want qbittorrent", cfg.ClientType)
	}

	// The config-driven factory must build the real qBittorrent client.
	if name, err := clientNameFor(t, cfg); err != nil {
		t.Fatalf("config-driven client build: %v", err)
	} else if name != "qBittorrent" {
		t.Fatalf("download client = %q, want qBittorrent", name)
	}

	fake := indexers.NewFakeIndexer("fake")
	fake.AddRelease(indexers.SearchResult{
		Title: "Dune.Part.Two.2024.1080p.WEB.x264", SizeBytes: 5_100_000_000,
		Info: indexers.ReleaseInfo{
			Protocol: indexers.ProtocolTorrent,
			URL:      "magnet:?xt=urn:btih:stubdune1080&dn=" + url.QueryEscape(title),
		},
	})
	fake.AddRelease(indexers.SearchResult{
		Title: "Dune.Part.Two.2024.720p.WEB.x264", SizeBytes: 2_500_000_000,
		Info: indexers.ReleaseInfo{
			Protocol: indexers.ProtocolTorrent,
			URL:      "magnet:?xt=urn:btih:stubdune720&dn=" + url.QueryEscape("Dune.Part.Two.2024.720p.WEB.x264"),
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

	// Queue is complete and attributes the download to the qBittorrent client.
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
	if qs[0].DownloadClient != "qBittorrent" {
		t.Errorf("queue client = %q, want qBittorrent", qs[0].DownloadClient)
	}
	if qs[0].ReleaseTitle != title {
		t.Errorf("queue release = %q, want %q", qs[0].ReleaseTitle, title)
	}

	// The stub actually received the add, and the release was removed from the
	// client after import (the Remover capability).
	if qb.adds != 1 {
		t.Errorf("qb add calls = %d, want 1", qb.adds)
	}
	if len(qb.removed) != 1 {
		t.Errorf("qb removed = %v, want the imported release removed", qb.removed)
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
