package music

import (
	"context"
	"sync"
	"testing"

	"github.com/brandenk514/mediarr/internal/downloads"
	"github.com/brandenk514/mediarr/internal/indexers"
	"github.com/brandenk514/mediarr/internal/store"
)

// recordingEvents is a store.Events that records every published event so the
// pipeline's live-dashboard emission (queue.updated / history.appended, #40)
// can be asserted.
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

// TestPipeline_EmitsEvents verifies that when an event bus is configured, the
// music pipeline publishes queue.updated events as the queue advances and
// history.appended events as the pipeline records activity (#40 live
// dashboard). A bus-less pipeline (Events == nil) must run without
// panicking (covered by the existing E2E tests, which pass no bus).
func TestPipeline_EmitsEvents(t *testing.T) {
	dlDir := t.TempDir()
	mediaRoot := t.TempDir()
	mock, err := downloads.NewMockClient("mock", dlDir)
	if err != nil {
		t.Fatalf("mock client: %v", err)
	}
	fake := indexers.NewFakeIndexer("fake")
	fake.AddRelease(indexers.SearchResult{Title: "Bob Marley - Legend [FLAC Lossless]", SizeBytes: 4_200_000_000})

	bus := &recordingEvents{}
	repo := newMemRepo()
	svc := New(Deps{
		Repo:           repo,
		Indexers:       []indexers.Searcher{fake},
		Client:         mock,
		MediaRoot:      mediaRoot,
		DefaultProfile: "Lossless",
		Events:         bus,
	})
	ctx := context.Background()

	artistID, err := svc.AddArtist(ctx, AddArtistInput{Name: "Bob Marley", Monitored: true})
	if err != nil {
		t.Fatalf("add artist: %v", err)
	}
	if _, err := svc.AddAlbum(ctx, artistID, "Legend", 1977, "Lossless", true); err != nil {
		t.Fatalf("add album: %v", err)
	}
	if _, err := svc.RunPipeline(ctx, artistID); err != nil {
		t.Fatalf("run pipeline: %v", err)
	}

	var queueUpdated, historyAppended int
	for _, ty := range bus.types() {
		switch ty {
		case "queue.updated":
			queueUpdated++
		case "history.appended":
			historyAppended++
		default:
			t.Errorf("unexpected event type %q (only queue.updated / history.appended expected)", ty)
		}
	}
	if queueUpdated < 2 {
		t.Errorf("expected >=2 queue.updated events (queued + complete), got %d", queueUpdated)
	}
	if historyAppended < 1 {
		t.Errorf("expected >=1 history.appended event (e.g. added/searched/imported), got %d", historyAppended)
	}
}
