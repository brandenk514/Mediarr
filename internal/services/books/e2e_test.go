package books

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	dom "github.com/brandenk514/mediarr/internal/domains/books"
	"github.com/brandenk514/mediarr/internal/downloads"
	"github.com/brandenk514/mediarr/internal/indexers"
)

// memRepo is an in-memory implementation of dom.Repo (the books persistence
// surface) used by the end-to-end service test so the full pipeline runs with
// no Postgres or network I/O. It mirrors the music/TV service test harness.
type memRepo struct {
	authors     map[int64]dom.Author
	nextAuthor  int64
	titles      map[int64]dom.Title
	nextTitle   int64
	editions    map[int64][]dom.Edition // titleID -> editions
	nextEdition int64
	wanted      map[wantedKey]dom.Wanted
	queue       map[int64]dom.QueueEntry
	nextQueue   int64
	history     []dom.HistoryEntry
	nextHist    int64
}

// wantedKey identifies a wanted row by its title/edition specifier.
type wantedKey struct {
	title   int64
	edition int64
}

func newMemRepo() *memRepo {
	return &memRepo{
		authors:     map[int64]dom.Author{},
		titles:      map[int64]dom.Title{},
		editions:    map[int64][]dom.Edition{},
		wanted:      map[wantedKey]dom.Wanted{},
		queue:       map[int64]dom.QueueEntry{},
		nextAuthor:  1,
		nextTitle:   1,
		nextEdition: 1,
		nextQueue:   1,
		nextHist:    1,
	}
}

func (r *memRepo) CreateAuthor(ctx context.Context, a dom.Author) (int64, error) {
	for _, v := range r.authors {
		if v.Name == a.Name {
			return 0, errors.New("duplicate author")
		}
	}
	a.ID = r.nextAuthor
	r.nextAuthor++
	a.AddedAt = time.Now()
	r.authors[a.ID] = a
	return a.ID, nil
}

func (r *memRepo) GetAuthor(ctx context.Context, id int64) (*dom.Author, error) {
	a, ok := r.authors[id]
	if !ok {
		return nil, dom.ErrAuthorNotFound
	}
	return &a, nil
}

func (r *memRepo) ListAuthors(ctx context.Context) ([]dom.Author, error) {
	var out []dom.Author
	for _, a := range r.authors {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (r *memRepo) SetAuthorMonitored(ctx context.Context, id int64, monitored bool) error {
	a, ok := r.authors[id]
	if !ok {
		return dom.ErrAuthorNotFound
	}
	a.Monitored = monitored
	r.authors[id] = a
	return nil
}

func (r *memRepo) CreateTitle(ctx context.Context, t dom.Title) (int64, error) {
	for _, v := range r.titles {
		if v.AuthorID == t.AuthorID && v.Name == t.Name {
			return v.ID, nil
		}
	}
	t.ID = r.nextTitle
	r.nextTitle++
	t.AddedAt = time.Now()
	r.titles[t.ID] = t
	return t.ID, nil
}

func (r *memRepo) GetTitle(ctx context.Context, id int64) (*dom.Title, error) {
	a, ok := r.titles[id]
	if !ok {
		return nil, dom.ErrTitleNotFound
	}
	return &a, nil
}

func (r *memRepo) ListTitles(ctx context.Context, authorID int64) ([]dom.Title, error) {
	var out []dom.Title
	for _, a := range r.titles {
		if a.AuthorID == authorID {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (r *memRepo) SetTitleMonitored(ctx context.Context, id int64, monitored bool) error {
	a, ok := r.titles[id]
	if !ok {
		return dom.ErrTitleNotFound
	}
	a.Monitored = monitored
	r.titles[id] = a
	return nil
}

func (r *memRepo) CreateEdition(ctx context.Context, e dom.Edition) (int64, error) {
	for _, v := range r.editions[e.TitleID] {
		if v.ISBN == e.ISBN && v.Format == e.Format {
			return v.ID, nil
		}
	}
	e.ID = r.nextEdition
	r.nextEdition++
	e.AddedAt = time.Now()
	r.editions[e.TitleID] = append(r.editions[e.TitleID], e)
	return e.ID, nil
}

func (r *memRepo) GetEdition(ctx context.Context, id int64) (*dom.Edition, error) {
	for _, list := range r.editions {
		for _, e := range list {
			if e.ID == id {
				return &e, nil
			}
		}
	}
	return nil, dom.ErrEditionNotFound
}

func (r *memRepo) ListEditions(ctx context.Context, titleID int64) ([]dom.Edition, error) {
	return r.editions[titleID], nil
}

func (r *memRepo) SetEditionMonitored(ctx context.Context, id int64, monitored bool) error {
	for titleID, list := range r.editions {
		for i, e := range list {
			if e.ID == id {
				e.Monitored = monitored
				r.editions[titleID][i] = e
				return nil
			}
		}
	}
	return dom.ErrEditionNotFound
}

func (r *memRepo) EnsureWantedTitle(ctx context.Context, titleID int64) error {
	k := wantedKey{title: titleID, edition: 0}
	if _, ok := r.wanted[k]; !ok {
		r.wanted[k] = dom.Wanted{TitleID: titleID, Status: dom.WantedPending, CreatedAt: time.Now()}
	}
	return nil
}

func (r *memRepo) EnsureWantedEdition(ctx context.Context, titleID, editionID int64) error {
	k := wantedKey{title: titleID, edition: editionID}
	if _, ok := r.wanted[k]; !ok {
		r.wanted[k] = dom.Wanted{TitleID: titleID, EditionID: editionID, Status: dom.WantedPending, CreatedAt: time.Now()}
	}
	return nil
}

func (r *memRepo) GetWanted(ctx context.Context, titleID, editionID int64) (*dom.Wanted, error) {
	w, ok := r.wanted[wantedKey{titleID, editionID}]
	if !ok {
		return nil, dom.ErrTitleNotFound
	}
	return &w, nil
}

func (r *memRepo) ListWanted(ctx context.Context, titleID int64) ([]dom.Wanted, error) {
	var out []dom.Wanted
	for _, w := range r.wanted {
		if w.TitleID == titleID {
			out = append(out, w)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].EditionID < out[j].EditionID })
	return out, nil
}

func (r *memRepo) MarkWantedSatisfied(ctx context.Context, titleID, editionID int64, releaseTitle string) error {
	k := wantedKey{titleID, editionID}
	w, ok := r.wanted[k]
	if !ok {
		return dom.ErrTitleNotFound
	}
	now := time.Now()
	w.Status = dom.WantedSatisfied
	w.ReleaseTitle = releaseTitle
	w.SatisfiedAt = &now
	r.wanted[k] = w
	return nil
}

func (r *memRepo) CreateQueue(ctx context.Context, e dom.QueueEntry) (int64, error) {
	e.ID = r.nextQueue
	r.nextQueue++
	now := time.Now()
	e.CreatedAt = now
	e.UpdatedAt = now
	r.queue[e.ID] = e
	return e.ID, nil
}

func (r *memRepo) UpdateQueue(ctx context.Context, e dom.QueueEntry) error {
	q, ok := r.queue[e.ID]
	if !ok {
		return dom.ErrAuthorNotFound
	}
	q.State = e.State
	q.Progress = e.Progress
	q.UpdatedAt = time.Now()
	r.queue[e.ID] = q
	return nil
}

func (r *memRepo) GetQueue(ctx context.Context, id int64) (*dom.QueueEntry, error) {
	e, ok := r.queue[id]
	if !ok {
		return nil, dom.ErrAuthorNotFound
	}
	return &e, nil
}

func (r *memRepo) ListQueue(ctx context.Context, authorID int64) ([]dom.QueueEntry, error) {
	var out []dom.QueueEntry
	for _, e := range r.queue {
		if e.AuthorID == authorID {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (r *memRepo) AddHistory(ctx context.Context, e dom.HistoryEntry) error {
	e.ID = r.nextHist
	r.nextHist++
	e.At = time.Now()
	r.history = append(r.history, e)
	return nil
}

func (r *memRepo) ListHistory(ctx context.Context, authorID int64, limit int) ([]dom.HistoryEntry, error) {
	var out []dom.HistoryEntry
	for _, e := range r.history {
		if e.AuthorID == authorID {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

var _ dom.Repo = (*memRepo)(nil)

// bookDestPath builds the documented import destination so the tests can assert
// on the real layout:
//
//	root/<Author>/<Title>/<Title>.<ext>
func bookDestPath(root, author, title, ext string) string {
	return filepath.Join(root, author, title, title+ext)
}

// TestPipeline_EndToEnd drives the full books pipeline: add author → add
// monitored title (whole-title wanted) → search → e-book format match →
// download → import → satisfied.
func TestPipeline_EndToEnd(t *testing.T) {
	dlDir := t.TempDir()
	mediaRoot := t.TempDir()
	mock, err := downloads.NewMockClient("mock", dlDir)
	if err != nil {
		t.Fatalf("mock client: %v", err)
	}
	fake := indexers.NewFakeIndexer("fake")
	// Two releases of the same title at different formats; under "Best" the
	// EPUB must win (most open / forward-compatible).
	fake.AddRelease(indexers.SearchResult{Title: "Ursula K. Le Guin - The Dispossessed [MOBI]", SizeBytes: 1_200_000})
	fake.AddRelease(indexers.SearchResult{Title: "Ursula K. Le Guin - The Dispossessed [EPUB]", SizeBytes: 1_100_000})

	repo := newMemRepo()
	svc := New(Deps{
		Repo:           repo,
		Indexers:       []indexers.Searcher{fake},
		Client:         mock,
		MediaRoot:      mediaRoot,
		DefaultProfile: "Best",
	})
	ctx := context.Background()

	// 1. Add a monitored author + a monitored title (whole-title wanted row).
	authorID, err := svc.AddAuthor(ctx, "Ursula K. Le Guin", true)
	if err != nil {
		t.Fatalf("add author: %v", err)
	}
	titleID, err := svc.AddTitle(ctx, authorID, "The Dispossessed", true)
	if err != nil {
		t.Fatalf("add title: %v", err)
	}

	w, err := repo.GetWanted(ctx, titleID, 0)
	if err != nil {
		t.Fatalf("get wanted: %v", err)
	}
	if w.Status != dom.WantedPending {
		t.Errorf("wanted status = %q, want pending", w.Status)
	}

	// 2. Run the pipeline.
	imported, err := svc.RunPipeline(ctx, authorID)
	if err != nil {
		t.Fatalf("run pipeline: %v", err)
	}
	if imported != 1 {
		t.Fatalf("imported = %d, want 1", imported)
	}

	// 3. The e-book file must exist at the documented layout (.epub for the
	// EPUB win).
	wantPath := bookDestPath(mediaRoot, "Ursula K. Le Guin", "The Dispossessed", ".epub")
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("imported file not on disk: %v", err)
	}

	// 4. The whole-title wanted is satisfied by the EPUB release.
	w, _ = repo.GetWanted(ctx, titleID, 0)
	if w.Status != dom.WantedSatisfied {
		t.Errorf("wanted status = %q, want satisfied", w.Status)
	}
	if w.ReleaseTitle != "Ursula K. Le Guin - The Dispossessed [EPUB]" {
		t.Errorf("satisfied by = %q, want the EPUB release", w.ReleaseTitle)
	}

	// 5. Queue is complete.
	qs, _ := repo.ListQueue(ctx, authorID)
	if len(qs) != 1 {
		t.Fatalf("queue len = %d, want 1", len(qs))
	}
	if qs[0].State != dom.QueueComplete {
		t.Errorf("queue state = %q, want complete", qs[0].State)
	}

	// 6. Idempotency: no pending wanted remains, so a re-run is a no-op.
	if _, err := svc.RunPipeline(ctx, authorID); !errors.Is(err, ErrNoWanted) {
		t.Errorf("re-run pipeline: err = %v, want ErrNoWanted (already satisfied)", err)
	}
}

// TestPipeline_EditionWanted_SatisfiedByTitle verifies an edition-level wanted
// is satisfied when the whole title (which contains that edition) is imported.
func TestPipeline_EditionWanted_SatisfiedByTitle(t *testing.T) {
	dlDir := t.TempDir()
	mediaRoot := t.TempDir()
	mock, _ := downloads.NewMockClient("mock", dlDir)
	fake := indexers.NewFakeIndexer("fake")
	fake.AddRelease(indexers.SearchResult{Title: "Ursula K. Le Guin - The Dispossessed [EPUB]", SizeBytes: 1_100_000})

	repo := newMemRepo()
	svc := New(Deps{
		Repo:           repo,
		Indexers:       []indexers.Searcher{fake},
		Client:         mock,
		MediaRoot:      mediaRoot,
		DefaultProfile: "Best",
	})
	ctx := context.Background()

	authorID, _ := svc.AddAuthor(ctx, "Ursula K. Le Guin", true)
	titleID, _ := svc.AddTitle(ctx, authorID, "The Dispossessed", false)
	// Add a known edition and monitor it (edition-level wanted, not title-level).
	editionID, err := svc.AddEdition(ctx, titleID, dom.FormatEPUB, "9780805079181", "Astounding", 1974, 505, true)
	if err != nil {
		t.Fatalf("add edition: %v", err)
	}
	if editionID == 0 {
		t.Fatal("edition id not assigned")
	}
	if _, err := repo.GetWanted(ctx, titleID, editionID); err != nil {
		t.Fatalf("edition wanted row: %v", err)
	}

	imported, err := svc.RunPipeline(ctx, authorID)
	if err != nil {
		t.Fatalf("run pipeline: %v", err)
	}
	if imported != 1 {
		t.Fatalf("imported = %d, want 1 (title import satisfies the edition want)", imported)
	}
	// The edition-level wanted must now be satisfied by the title release.
	w, _ := repo.GetWanted(ctx, titleID, editionID)
	if w.Status != dom.WantedSatisfied {
		t.Errorf("edition wanted status = %q, want satisfied", w.Status)
	}
}

// TestPipeline_TitleMonitor_Acceptance is the title-granularity acceptance
// test:
//
//	add author (monitored) → add two titles (one monitored, one not) → the
//	pipeline services exactly the monitored title
//
// The unmonitored title has no wanted row, so its (otherwise present) release
// must NOT be imported.
func TestPipeline_TitleMonitor_Acceptance(t *testing.T) {
	dlDir := t.TempDir()
	mediaRoot := t.TempDir()
	mock, _ := downloads.NewMockClient("mock", dlDir)
	fake := indexers.NewFakeIndexer("fake")
	// A release for the monitored title...
	fake.AddRelease(indexers.SearchResult{Title: "Bob - Legend [EPUB]", SizeBytes: 1_000_000})
	// ...and a distractor for the UNmonitored title that must be left alone.
	fake.AddRelease(indexers.SearchResult{Title: "Bob - Uprising [EPUB]", SizeBytes: 1_000_000})

	repo := newMemRepo()
	svc := New(Deps{
		Repo:           repo,
		Indexers:       []indexers.Searcher{fake},
		Client:         mock,
		MediaRoot:      mediaRoot,
		DefaultProfile: "Best",
	})
	ctx := context.Background()

	authorID, _ := svc.AddAuthor(ctx, "Bob", true)
	legendID, _ := svc.AddTitle(ctx, authorID, "Legend", true)
	uprisingID, _ := svc.AddTitle(ctx, authorID, "Uprising", false)

	// Only the monitored title has a wanted row.
	if wants, _ := repo.ListWanted(ctx, legendID); len(wants) != 1 {
		t.Fatalf("Legend wanted len = %d, want 1", len(wants))
	}
	if wants, _ := repo.ListWanted(ctx, uprisingID); len(wants) != 0 {
		t.Fatalf("Uprising wanted len = %d, want 0 (unmonitored)", len(wants))
	}

	imported, err := svc.RunPipeline(ctx, authorID)
	if err != nil {
		t.Fatalf("run pipeline: %v", err)
	}
	if imported != 1 {
		t.Fatalf("imported = %d, want exactly 1 (only the monitored title)", imported)
	}

	// Legend is satisfied and its file is on disk.
	w, _ := repo.GetWanted(ctx, legendID, 0)
	if w.Status != dom.WantedSatisfied {
		t.Errorf("Legend status = %q, want satisfied", w.Status)
	}
	if _, err := os.Stat(bookDestPath(mediaRoot, "Bob", "Legend", ".epub")); err != nil {
		t.Errorf("imported file Legend not on disk: %v", err)
	}
	// The unmonitored Uprising must NOT have been imported.
	if _, err := os.Stat(bookDestPath(mediaRoot, "Bob", "Uprising", ".epub")); err == nil {
		t.Errorf("unmonitored Uprising was imported; it should have been left alone")
	}
}

// TestPipeline_NoMatch verifies that when no release satisfies the profile, the
// pipeline returns ErrNoMatch without importing or satisfying.
func TestPipeline_NoMatch(t *testing.T) {
	dlDir := t.TempDir()
	mediaRoot := t.TempDir()
	mock, _ := downloads.NewMockClient("mock", dlDir)
	fake := indexers.NewFakeIndexer("fake")
	// Only an unsupported PDF release — a "Best" title requires an e-book format.
	fake.AddRelease(indexers.SearchResult{Title: "Bob - Legend [PDF]", SizeBytes: 800_000})

	repo := newMemRepo()
	svc := New(Deps{
		Repo:           repo,
		Indexers:       []indexers.Searcher{fake},
		Client:         mock,
		MediaRoot:      mediaRoot,
		DefaultProfile: "Best",
	})
	ctx := context.Background()

	authorID, _ := svc.AddAuthor(ctx, "Bob", true)
	titleID, _ := svc.AddTitle(ctx, authorID, "Legend", true)

	_, err := svc.RunPipeline(ctx, authorID)
	if !errors.Is(err, ErrNoMatch) {
		t.Fatalf("run pipeline: err = %v, want ErrNoMatch", err)
	}
	w, _ := repo.GetWanted(ctx, titleID, 0)
	if w.Status != dom.WantedPending {
		t.Errorf("wanted status = %q, want pending after no-match", w.Status)
	}
	entries, _ := filepath.Glob(filepath.Join(mediaRoot, "*"))
	if len(entries) != 0 {
		t.Errorf("expected nothing imported, found %d entries", len(entries))
	}
}
