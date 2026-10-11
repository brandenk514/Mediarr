package movies

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	dom "github.com/brandenk514/mediarr/internal/domains/movies"
	"github.com/brandenk514/mediarr/internal/downloads"
	"github.com/brandenk514/mediarr/internal/indexers"
	"github.com/brandenk514/mediarr/internal/store"
)

// recordingClient is a download client that implements both Client and
// Remover. It materialises the release file locally (like the mock) and
// records which releases were Add-ed and Removed, so the pipeline's
// removal-after-import behaviour can be asserted (#36/#37).
type recordingClient struct {
	mu      sync.Mutex
	added   map[string]bool
	removed map[string]int
	base    string
}

func newRecordingClient(dir string) *recordingClient {
	return &recordingClient{added: map[string]bool{}, removed: map[string]int{}, base: dir}
}

func (c *recordingClient) Name() string { return "recorder" }

func (c *recordingClient) Add(_ context.Context, r downloads.Release) error {
	dir := r.PathDir
	if dir == "" {
		dir = c.base
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f := filepath.Join(dir, r.FileName)
	if err := os.WriteFile(f, []byte("recorder: "+r.Title+"\n"), 0o644); err != nil {
		return err
	}
	c.mu.Lock()
	c.added[r.Title] = true
	c.mu.Unlock()
	return nil
}

func (c *recordingClient) Status(_ context.Context, title string) (downloads.Status, error) {
	c.mu.Lock()
	added := c.added[title]
	c.mu.Unlock()
	if !added {
		return downloads.Status{Complete: false, Progress: 0}, nil
	}
	// Find the file (the recorder writes under base, one file per title).
	entries, _ := os.ReadDir(c.base)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(c.base, e.Name()))
		if err != nil {
			continue
		}
		if strings.Contains(string(b), title) {
			return downloads.Status{Complete: true, Progress: 100, File: filepath.Join(c.base, e.Name())}, nil
		}
	}
	return downloads.Status{Complete: false, Progress: 0}, nil
}

func (c *recordingClient) Remove(_ context.Context, title string) error {
	c.mu.Lock()
	c.removed[title]++
	c.mu.Unlock()
	return nil
}

var (
	_ downloads.Client  = (*recordingClient)(nil)
	_ downloads.Remover = (*recordingClient)(nil)
)

// recordingEvents is a store.Events that records every published event so the
// pipeline's queue.updated emission can be asserted (#36 pub/sub).
type recordingEvents struct {
	mu     sync.Mutex
	events []store.Event
}

func (e *recordingEvents) Publish(_ context.Context, ev store.Event) error {
	e.mu.Lock()
	e.events = append(e.events, ev)
	e.mu.Unlock()
	return nil
}

func (e *recordingEvents) Subscribe(context.Context, ...string) (<-chan store.Event, error) {
	ch := make(chan store.Event)
	close(ch)
	return ch, nil
}

func (e *recordingEvents) Ping(context.Context) error { return nil }

func (e *recordingEvents) types() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]string, 0, len(e.events))
	for _, ev := range e.events {
		out = append(out, ev.Type)
	}
	return out
}

var _ store.Events = (*recordingEvents)(nil)

// TestPipeline_RemovalAfterImport verifies that when the download client
// implements Remover, the pipeline drops the release from the client's queue
// after the file has been imported (PLAN §6, #36/#37).
func TestPipeline_RemovalAfterImport(t *testing.T) {
	dlDir := t.TempDir()
	mediaRoot := t.TempDir()
	rec := newRecordingClient(dlDir)
	fake := indexers.NewFakeIndexer("fake")
	title := "Dune.Part.Two.2024.1080p.WEB.x265.10bit"
	fake.AddRelease(indexers.SearchResult{Title: title, SizeBytes: 5_000_000_000})

	repo := newMemRepo()
	svc := New(Deps{
		Repo:           repo,
		Indexers:       []indexers.Searcher{fake},
		Client:         rec,
		MediaRoot:      mediaRoot,
		DefaultProfile: "HD-1080p",
		DownloadsDir:   dlDir,
	})
	ctx := context.Background()

	id, err := svc.AddMovie(ctx, "Dune Part Two", "2024", "HD-1080p")
	if err != nil {
		t.Fatalf("add movie: %v", err)
	}
	if _, err := svc.RunPipeline(ctx, id); err != nil {
		t.Fatalf("run pipeline: %v", err)
	}

	rec.mu.Lock()
	added := rec.added[title]
	removed := rec.removed[title]
	rec.mu.Unlock()
	if !added {
		t.Fatalf("release %q was never added to the client", title)
	}
	if removed != 1 {
		t.Fatalf("Remove called %d times, want exactly 1 (removal after import)", removed)
	}
}

// TestPipeline_NoRemovalForNonRemover verifies the pipeline does NOT call
// Remove for clients that don't implement Remover (e.g. a shared/seeding
// client or the mock) — the type assertion must skip removal, not fail.
func TestPipeline_NoRemovalForNonRemover(t *testing.T) {
	dlDir := t.TempDir()
	mediaRoot := t.TempDir()
	// The mock client does not implement Remover.
	mock, err := downloads.NewMockClient("mock", dlDir)
	if err != nil {
		t.Fatalf("mock: %v", err)
	}
	fake := indexers.NewFakeIndexer("fake")
	title := "Dune.Part.Two.2024.1080p.WEB.x265.10bit"
	fake.AddRelease(indexers.SearchResult{Title: title, SizeBytes: 5_000_000_000})

	repo := newMemRepo()
	svc := New(Deps{
		Repo:           repo,
		Indexers:       []indexers.Searcher{fake},
		Client:         mock,
		MediaRoot:      mediaRoot,
		DefaultProfile: "HD-1080p",
		DownloadsDir:   dlDir,
	})
	ctx := context.Background()

	id, err := svc.AddMovie(ctx, "Dune Part Two", "2024", "HD-1080p")
	if err != nil {
		t.Fatalf("add movie: %v", err)
	}
	if _, err := svc.RunPipeline(ctx, id); err != nil {
		t.Fatalf("run pipeline (non-remover) must still succeed: %v", err)
	}
	// The pipeline must have imported even without removal.
	qs, _ := repo.ListQueue(ctx)
	if len(qs) != 1 || qs[0].State != dom.QueueComplete {
		t.Fatalf("queue = %+v, want one complete entry", qs)
	}
}

// TestPipeline_EmitsQueueEvents verifies that when an event bus is configured,
// the pipeline publishes queue.updated events as the queue advances
// (queued → downloading → complete). A bus-less pipeline (Events == nil) must
// run without panicking.
func TestPipeline_EmitsQueueEvents(t *testing.T) {
	dlDir := t.TempDir()
	mediaRoot := t.TempDir()
	rec := newRecordingClient(dlDir)
	bus := &recordingEvents{}
	fake := indexers.NewFakeIndexer("fake")
	fake.AddRelease(indexers.SearchResult{Title: "Dune.Part.Two.2024.1080p.WEB.x265.10bit", SizeBytes: 5_000_000_000})

	repo := newMemRepo()
	svc := New(Deps{
		Repo:           repo,
		Indexers:       []indexers.Searcher{fake},
		Client:         rec,
		MediaRoot:      mediaRoot,
		DefaultProfile: "HD-1080p",
		DownloadsDir:   dlDir,
		Events:         bus,
	})
	ctx := context.Background()

	id, err := svc.AddMovie(ctx, "Dune Part Two", "2024", "HD-1080p")
	if err != nil {
		t.Fatalf("add movie: %v", err)
	}
	if _, err := svc.RunPipeline(ctx, id); err != nil {
		t.Fatalf("run pipeline: %v", err)
	}

	types := bus.types()
	if len(types) == 0 {
		t.Fatal("expected queue.updated events, got none")
	}
	for _, ty := range types {
		if ty != "queue.updated" {
			t.Errorf("unexpected event type %q (only queue.updated expected)", ty)
		}
	}
	// We should see at least the initial queued and the final complete states.
	// (States ride in the payload; the count is a coarse but sufficient signal
	// that the bus was driven through the state machine.)
	if len(types) < 2 {
		t.Errorf("expected >=2 queue.updated events (queued + complete), got %d", len(types))
	}
}
