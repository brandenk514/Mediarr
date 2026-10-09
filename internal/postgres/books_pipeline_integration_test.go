package postgres_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	books "github.com/brandenk514/mediarr/internal/domains/books"
	"github.com/brandenk514/mediarr/internal/downloads"
	"github.com/brandenk514/mediarr/internal/indexers"
	bookssvc "github.com/brandenk514/mediarr/internal/services/books"
)

// TestBooksRepo_WantedQueueHistoryRoundTrip exercises the 0006_books_pipeline
// schema (book_wanted / book_queue / book_history) and the BooksRepo pipeline
// SQL: title-level and edition-level wanted rows, idempotent Ensure, satisfy,
// queue lifecycle, and author-grouped history.
func TestBooksRepo_WantedQueueHistoryRoundTrip(t *testing.T) {
	r, _ := startBooksPG(t)
	ctx := context.Background()

	authorID, _ := r.CreateAuthor(ctx, books.Author{Name: "Ursula K. Le Guin", Monitored: true})
	titleID, _ := r.CreateTitle(ctx, books.Title{AuthorID: authorID, Name: "The Dispossessed"})
	editionID, _ := r.CreateEdition(ctx, books.Edition{TitleID: titleID, Format: "epub", ISBN: "9780805079181"})

	// Title-level wanted (edition_id = 0) — idempotent on conflict.
	if err := r.EnsureWantedTitle(ctx, titleID); err != nil {
		t.Fatalf("ensure wanted title: %v", err)
	}
	if err := r.EnsureWantedTitle(ctx, titleID); err != nil {
		t.Fatalf("re-ensure wanted title should be idempotent: %v", err)
	}
	// Edition-level wanted.
	if err := r.EnsureWantedEdition(ctx, titleID, editionID); err != nil {
		t.Fatalf("ensure wanted edition: %v", err)
	}

	wants, err := r.ListWanted(ctx, titleID)
	if err != nil {
		t.Fatalf("list wanted: %v", err)
	}
	if len(wants) != 2 {
		t.Fatalf("wanted len = %d, want 2 (title + edition)", len(wants))
	}
	// The title-level row is edition_id 0 and pending.
	if _, err := r.GetWanted(ctx, titleID, 0); err != nil {
		t.Fatalf("get title-level wanted: %v", err)
	}
	we, err := r.GetWanted(ctx, titleID, editionID)
	if err != nil {
		t.Fatalf("get edition-level wanted: %v", err)
	}
	if we.Status != books.WantedPending {
		t.Errorf("edition wanted status = %q, want pending", we.Status)
	}

	// Satisfy the title-level wanted.
	if err := r.MarkWantedSatisfied(ctx, titleID, 0, "Ursula K. Le Guin - The Dispossessed [EPUB]"); err != nil {
		t.Fatalf("mark satisfied: %v", err)
	}
	wt, _ := r.GetWanted(ctx, titleID, 0)
	if wt.Status != books.WantedSatisfied {
		t.Errorf("title wanted status = %q, want satisfied", wt.Status)
	}
	if wt.ReleaseTitle != "Ursula K. Le Guin - The Dispossessed [EPUB]" {
		t.Errorf("release title = %q, want the EPUB release", wt.ReleaseTitle)
	}
	if wt.SatisfiedAt == nil {
		t.Error("satisfied_at should be set")
	}

	// Queue lifecycle.
	qid, err := r.CreateQueue(ctx, books.QueueEntry{
		AuthorID: authorID, TitleID: titleID, ReleaseTitle: "rel",
		Indexer: "fake", DownloadClient: "mock", State: books.QueueQueued,
	})
	if err != nil {
		t.Fatalf("create queue: %v", err)
	}
	if qid == 0 {
		t.Fatal("expected non-zero queue id")
	}
	qe, _ := r.GetQueue(ctx, qid)
	if qe.State != books.QueueQueued {
		t.Errorf("queue state = %q, want queued", qe.State)
	}
	qe.State = books.QueueComplete
	qe.Progress = 100
	if err := r.UpdateQueue(ctx, *qe); err != nil {
		t.Fatalf("update queue: %v", err)
	}
	qe, _ = r.GetQueue(ctx, qid)
	if qe.State != books.QueueComplete || qe.Progress != 100 {
		t.Errorf("queue after update = %q/%d, want complete/100", qe.State, qe.Progress)
	}
	qs, _ := r.ListQueue(ctx, authorID)
	if len(qs) != 1 {
		t.Errorf("list queue len = %d, want 1", len(qs))
	}

	// History (author-grouped).
	if err := r.AddHistory(ctx, books.HistoryEntry{AuthorID: authorID, Event: "searched", Detail: "3 candidates"}); err != nil {
		t.Fatalf("add history: %v", err)
	}
	if err := r.AddHistory(ctx, books.HistoryEntry{AuthorID: authorID, Event: "imported", Detail: "1 file"}); err != nil {
		t.Fatalf("add history 2: %v", err)
	}
	hist, err := r.ListHistory(ctx, authorID, 10)
	if err != nil {
		t.Fatalf("list history: %v", err)
	}
	if len(hist) != 2 {
		t.Errorf("history len = %d, want 2", len(hist))
	}
	// Newest first: the second event (imported) is returned first.
	if hist[0].Event != "imported" {
		t.Errorf("history[0].event = %q, want imported (newest first)", hist[0].Event)
	}
}

// TestBooksPipeline_EndToEnd_Postgres drives the full books pipeline end-to-end
// against a REAL Postgres repository (testcontainers) — the acceptance E2E for
// cards #26/#27/#28. It mirrors TestMusicPipeline_EndToEnd_Postgres: add author
// → want title (title-level + edition-level) → fake-indexer search → e-book
// format match → mock-client download → import → satisfied, with all state
// persisted through the postgres BooksRepo.
func TestBooksPipeline_EndToEnd_Postgres(t *testing.T) {
	repo, _ := startBooksPG(t)
	ctx := context.Background()

	dlDir := t.TempDir()
	mediaRoot := t.TempDir()
	mock, err := downloads.NewMockClient("mock", dlDir)
	if err != nil {
		t.Fatalf("mock client: %v", err)
	}
	fake := indexers.NewFakeIndexer("fake")
	// Two candidates for The Dispossessed (EPUB wins under "Best") and a
	// distractor for an unmonitored title.
	fake.AddRelease(indexers.SearchResult{Title: "Ursula K. Le Guin - The Dispossessed [MOBI]", SizeBytes: 1_200_000})
	fake.AddRelease(indexers.SearchResult{Title: "Ursula K. Le Guin - The Dispossessed [EPUB]", SizeBytes: 1_100_000})
	fake.AddRelease(indexers.SearchResult{Title: "Ursula K. Le Guin - The Left Hand of Darkness [EPUB]", SizeBytes: 1_000_000})

	svc := bookssvc.New(bookssvc.Deps{
		Repo:           repo,
		Indexers:       []indexers.Searcher{fake},
		Client:         mock,
		MediaRoot:      mediaRoot,
		DefaultProfile: "Best",
	})

	// 1. Add the author (monitored) and a monitored title (whole-title wanted).
	authorID, err := svc.AddAuthor(ctx, "Ursula K. Le Guin", true)
	if err != nil {
		t.Fatalf("add author: %v", err)
	}
	dispossessedID, err := svc.AddTitle(ctx, authorID, "The Dispossessed", true)
	if err != nil {
		t.Fatalf("add title: %v", err)
	}
	// A monitored edition of the same title (edition-level wanted too).
	if _, err := svc.AddEdition(ctx, dispossessedID, books.FormatEPUB, "9780805079181", "Astounding", 1974, 505, true); err != nil {
		t.Fatalf("add edition: %v", err)
	}
	// An unmonitored second title (its release is a distractor, not imported).
	if _, err := svc.AddTitle(ctx, authorID, "The Left Hand of Darkness", false); err != nil {
		t.Fatalf("add title 2: %v", err)
	}

	// 2. Run the pipeline. It should import exactly the monitored title.
	imported, err := svc.RunPipeline(ctx, authorID)
	if err != nil {
		t.Fatalf("run pipeline: %v", err)
	}
	if imported != 2 {
		t.Fatalf("imported = %d, want 2 (title-level + edition-level wanted both satisfied)", imported)
	}

	// 3. The e-book file must exist on disk at the documented layout (.epub win).
	wantPath := filepath.Join(mediaRoot, "Ursula K. Le Guin", "The Dispossessed", "The Dispossessed.epub")
	if _, err := os.Stat(wantPath); err != nil {
		t.Errorf("imported file not on disk: %v", err)
	}

	// 4. Both the title-level and the edition-level wanted are satisfied.
	wt, err := repo.GetWanted(ctx, dispossessedID, 0)
	if err != nil {
		t.Fatalf("get title wanted: %v", err)
	}
	if wt.Status != books.WantedSatisfied {
		t.Errorf("title wanted status = %q, want satisfied", wt.Status)
	}
	if wt.ReleaseTitle != "Ursula K. Le Guin - The Dispossessed [EPUB]" {
		t.Errorf("satisfied by = %q, want the EPUB release", wt.ReleaseTitle)
	}

	// 5. The queue is complete and the history is non-empty (author-grouped).
	qs, _ := repo.ListQueue(ctx, authorID)
	if len(qs) != 1 {
		t.Fatalf("queue len = %d, want 1", len(qs))
	}
	if qs[0].State != books.QueueComplete {
		t.Errorf("queue state = %q, want complete", qs[0].State)
	}
	hist, _ := repo.ListHistory(ctx, authorID, 100)
	if len(hist) == 0 {
		t.Error("expected non-empty history")
	}

	// 6. Idempotency: no pending wanted remains, so a re-run is a no-op.
	if _, err := svc.RunPipeline(ctx, authorID); !errors.Is(err, bookssvc.ErrNoWanted) {
		t.Errorf("re-run pipeline: err = %v, want ErrNoWanted (already satisfied)", err)
	}
}

// TestBooksPipeline_CascadeWithPipelineTables verifies that deleting an author
// cascades through the 0006 pipeline tables (book_wanted / book_queue /
// book_history) as well as the 0005 identity tables, leaving no orphans.
func TestBooksPipeline_CascadeWithPipelineTables(t *testing.T) {
	r, db := startBooksPG(t)
	ctx := context.Background()

	authorID, _ := r.CreateAuthor(ctx, books.Author{Name: "Ted Chiang"})
	titleID, _ := r.CreateTitle(ctx, books.Title{AuthorID: authorID, Name: "Stories of Your Life"})
	r.EnsureWantedTitle(ctx, titleID)
	r.CreateQueue(ctx, books.QueueEntry{
		AuthorID: authorID, TitleID: titleID, ReleaseTitle: "rel",
		Indexer: "fake", DownloadClient: "mock", State: books.QueueComplete,
	})
	r.AddHistory(ctx, books.HistoryEntry{AuthorID: authorID, Event: "added", Detail: "x"})

	if _, err := db.ExecContext(ctx, `DELETE FROM book_authors WHERE id = $1`, authorID); err != nil {
		t.Fatalf("delete author: %v", err)
	}

	var wantedCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM book_wanted WHERE title_id = $1`, titleID).Scan(&wantedCount); err != nil {
		t.Fatalf("count book_wanted: %v", err)
	}
	if wantedCount != 0 {
		t.Errorf("book_wanted rows after cascade = %d, want 0", wantedCount)
	}
	var queueCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM book_queue WHERE author_id = $1`, authorID).Scan(&queueCount); err != nil {
		t.Fatalf("count book_queue: %v", err)
	}
	if queueCount != 0 {
		t.Errorf("book_queue rows after cascade = %d, want 0", queueCount)
	}
	var histCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM book_history WHERE author_id = $1`, authorID).Scan(&histCount); err != nil {
		t.Fatalf("count book_history: %v", err)
	}
	if histCount != 0 {
		t.Errorf("book_history rows after cascade = %d, want 0", histCount)
	}
}
