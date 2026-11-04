package books

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	dom "github.com/brandenk514/mediarr/internal/domains/books"
	"github.com/brandenk514/mediarr/internal/downloads"
	"github.com/brandenk514/mediarr/internal/indexers"
)

// TestService_ReadMethods exercises the read/lookup pass-throughs against the
// in-memory repo so the service's accessor surface is covered.
func TestService_ReadMethods(t *testing.T) {
	repo := newMemRepo()
	svc := New(Deps{Repo: repo, DefaultProfile: "Best"})
	ctx := context.Background()

	authorID, _ := svc.AddAuthor(ctx, AddAuthorInput{Name: "Ursula K. Le Guin", Monitored: true})
	titleID, _ := svc.AddTitle(ctx, authorID, "The Dispossessed", true)
	editionID, _ := svc.AddEdition(ctx, titleID, dom.FormatEPUB, "en", "9780805079181", "Astounding", 1974, 505, true)

	// ListAuthors / GetAuthor.
	authors, err := svc.ListAuthors(ctx)
	if err != nil {
		t.Fatalf("ListAuthors: %v", err)
	}
	if len(authors) != 1 || authors[0].Name != "Ursula K. Le Guin" {
		t.Errorf("ListAuthors = %+v, want one Ursula K. Le Guin", authors)
	}
	ga, err := svc.GetAuthor(ctx, authorID)
	if err != nil || ga.ID != authorID {
		t.Errorf("GetAuthor = (%+v, %v), want the author", ga, err)
	}
	if _, err := svc.GetAuthor(ctx, 9999); !errors.Is(err, ErrAuthorNotFound) {
		t.Errorf("GetAuthor(missing) err = %v, want ErrAuthorNotFound", err)
	}

	// ListTitles / GetTitle.
	titles, err := svc.ListTitles(ctx, authorID)
	if err != nil {
		t.Fatalf("ListTitles: %v", err)
	}
	if len(titles) != 1 || titles[0].Name != "The Dispossessed" {
		t.Errorf("ListTitles = %+v, want one title", titles)
	}
	gt, err := svc.GetTitle(ctx, titleID)
	if err != nil || gt.ID != titleID {
		t.Errorf("GetTitle = (%+v, %v), want the title", gt, err)
	}
	if _, err := svc.GetTitle(ctx, 9999); !errors.Is(err, ErrTitleNotFound) {
		t.Errorf("GetTitle(missing) err = %v, want ErrTitleNotFound", err)
	}

	// ListEditions / GetEdition.
	editions, err := svc.ListEditions(ctx, titleID)
	if err != nil {
		t.Fatalf("ListEditions: %v", err)
	}
	if len(editions) != 1 || editions[0].Format != dom.FormatEPUB {
		t.Errorf("ListEditions = %+v, want one epub edition", editions)
	}
	ge, err := svc.GetEdition(ctx, editionID)
	if err != nil || ge.ID != editionID {
		t.Errorf("GetEdition = (%+v, %v), want the edition", ge, err)
	}
	if _, err := svc.GetEdition(ctx, 9999); !errors.Is(err, ErrEditionNotFound) {
		t.Errorf("GetEdition(missing) err = %v, want ErrEditionNotFound", err)
	}

	// ListWanted (title + edition rows exist because both were monitored).
	wants, err := svc.ListWanted(ctx, titleID)
	if err != nil {
		t.Fatalf("ListWanted: %v", err)
	}
	if len(wants) != 2 {
		t.Errorf("ListWanted = %d rows, want 2 (title + edition)", len(wants))
	}
}

// TestService_B1ReadSurface exercises the Readarr B1 service additions: the
// flat Books index, book files, series (add/idempotency/linking/membership)
// and per-author history.
func TestService_B1ReadSurface(t *testing.T) {
	repo := newMemRepo()
	svc := New(Deps{Repo: repo, DefaultProfile: "Best"})
	ctx := context.Background()

	authorID, _ := svc.AddAuthor(ctx, AddAuthorInput{Name: "Ursula K. Le Guin", Monitored: true})
	titleID, _ := svc.AddTitle(ctx, authorID, "The Dispossessed", true)

	// Flat Books index carries the author name.
	books, err := svc.ListBooks(ctx)
	if err != nil {
		t.Fatalf("ListBooks: %v", err)
	}
	if len(books) != 1 || books[0].Name != "The Dispossessed" ||
		books[0].AuthorName != "Ursula K. Le Guin" {
		t.Fatalf("ListBooks = %+v, want one Dispossessed by Le Guin", books)
	}

	// Book files: guard on unknown title, empty for an un-imported title.
	if _, err := svc.GetBookFiles(ctx, 9999); !errors.Is(err, ErrTitleNotFound) {
		t.Errorf("GetBookFiles(missing) err = %v, want ErrTitleNotFound", err)
	}
	files, err := svc.GetBookFiles(ctx, titleID)
	if err != nil {
		t.Fatalf("GetBookFiles: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("GetBookFiles = %+v, want none (never imported)", files)
	}

	// Series: add is idempotent by name; unknown series is an error.
	seriesID, err := svc.AddSeries(ctx, "The Dispossessed Saga")
	if err != nil {
		t.Fatalf("AddSeries: %v", err)
	}
	again, err := svc.AddSeries(ctx, "The Dispossessed Saga")
	if err != nil {
		t.Fatalf("AddSeries (again): %v", err)
	}
	if again != seriesID {
		t.Errorf("AddSeries idempotent: got %d, want %d", again, seriesID)
	}
	if _, err := svc.GetSeries(ctx, 9999); !errors.Is(err, ErrSeriesNotFound) {
		t.Errorf("GetSeries(missing) err = %v, want ErrSeriesNotFound", err)
	}

	// Linking: unknown title or series fails; a valid link is idempotent.
	if err := svc.LinkTitleToSeries(ctx, 9999, seriesID); !errors.Is(err, ErrTitleNotFound) {
		t.Errorf("LinkTitleToSeries(missing title) err = %v, want ErrTitleNotFound", err)
	}
	if err := svc.LinkTitleToSeries(ctx, titleID, 9999); !errors.Is(err, ErrSeriesNotFound) {
		t.Errorf("LinkTitleToSeries(missing series) err = %v, want ErrSeriesNotFound", err)
	}
	if err := svc.LinkTitleToSeries(ctx, titleID, seriesID); err != nil {
		t.Fatalf("LinkTitleToSeries: %v", err)
	}
	if err := svc.LinkTitleToSeries(ctx, titleID, seriesID); err != nil {
		t.Fatalf("LinkTitleToSeries (again, idempotent): %v", err)
	}

	// Membership from both directions.
	titleSeries, err := svc.ListTitleSeries(ctx, titleID)
	if err != nil {
		t.Fatalf("ListTitleSeries: %v", err)
	}
	if len(titleSeries) != 1 || titleSeries[0].Name != "The Dispossessed Saga" {
		t.Errorf("ListTitleSeries = %+v, want the saga", titleSeries)
	}
	seriesBooks, err := svc.ListSeriesBooks(ctx, seriesID)
	if err != nil {
		t.Fatalf("ListSeriesBooks: %v", err)
	}
	if len(seriesBooks) != 1 || seriesBooks[0].Name != "The Dispossessed" ||
		seriesBooks[0].AuthorName != "Ursula K. Le Guin" {
		t.Errorf("ListSeriesBooks = %+v, want the Dispossessed", seriesBooks)
	}
	authorSeries, err := svc.ListAuthorSeries(ctx, authorID)
	if err != nil {
		t.Fatalf("ListAuthorSeries: %v", err)
	}
	if len(authorSeries) != 1 || len(authorSeries[0].Titles) != 1 {
		t.Errorf("ListAuthorSeries = %+v, want the saga with one title", authorSeries)
	}

	// Per-author history: the linking event is recorded; unknown author 404s.
	hist, err := svc.GetAuthorHistory(ctx, authorID, 0)
	if err != nil {
		t.Fatalf("GetAuthorHistory: %v", err)
	}
	found := false
	for _, h := range hist {
		if h.Event == "series-linked" {
			found = true
		}
	}
	if !found {
		t.Errorf("history = %+v, want a series-linked event", hist)
	}
	if _, err := svc.GetAuthorHistory(ctx, 9999, 0); !errors.Is(err, ErrAuthorNotFound) {
		t.Errorf("GetAuthorHistory(missing) err = %v, want ErrAuthorNotFound", err)
	}
}

// TestService_MonitorToggles exercises the author/title/edition monitor
// toggles: turning on must (re)create the wanted row, turning off must not.
func TestService_MonitorToggles(t *testing.T) {
	repo := newMemRepo()
	svc := New(Deps{Repo: repo, DefaultProfile: "Best"})
	ctx := context.Background()

	authorID, _ := svc.AddAuthor(ctx, AddAuthorInput{Name: "A Author", Monitored: false})
	titleID, _ := svc.AddTitle(ctx, authorID, "Title", false)
	editionID, _ := svc.AddEdition(ctx, titleID, dom.FormatEPUB, "en", "", "", 0, 0, false)

	// Turning a title on ensures a pending whole-title wanted row.
	if err := svc.SetTitleMonitored(ctx, titleID, true); err != nil {
		t.Fatalf("SetTitleMonitored(true): %v", err)
	}
	w, err := repo.GetWanted(ctx, titleID, 0)
	if err != nil || w.Status != dom.WantedPending {
		t.Errorf("after SetTitleMonitored(true): wanted = (%+v, %v), want pending", w, err)
	}
	// Turning it off does not create a new wanted row (the existing one stays).
	if err := svc.SetTitleMonitored(ctx, titleID, false); err != nil {
		t.Fatalf("SetTitleMonitored(false): %v", err)
	}

	// Turning an edition on ensures a pending edition-level wanted row.
	if err := svc.SetEditionMonitored(ctx, titleID, editionID, true); err != nil {
		t.Fatalf("SetEditionMonitored(true): %v", err)
	}
	we, err := repo.GetWanted(ctx, titleID, editionID)
	if err != nil || we.Status != dom.WantedPending {
		t.Errorf("after SetEditionMonitored(true): wanted = (%+v, %v), want pending", we, err)
	}
	// Turning an edition off is a no-op on wanted rows.
	if err := svc.SetEditionMonitored(ctx, titleID, editionID, false); err != nil {
		t.Fatalf("SetEditionMonitored(false): %v", err)
	}

	// Author-level toggle fans out to the author's titles.
	if err := svc.SetAuthorMonitored(ctx, authorID, true); err != nil {
		t.Fatalf("SetAuthorMonitored(true): %v", err)
	}
	ti, _ := repo.GetTitle(ctx, titleID)
	if !ti.Monitored {
		t.Error("title should be monitored after author-level toggle")
	}
	if err := svc.SetAuthorMonitored(ctx, authorID, false); err != nil {
		t.Fatalf("SetAuthorMonitored(false): %v", err)
	}
	ti, _ = repo.GetTitle(ctx, titleID)
	if ti.Monitored {
		t.Error("title should be unmonitored after author-level toggle off")
	}
}

// TestService_InputGuards exercises the input-validation error paths that are
// cheap to hit without a pipeline: empty names and an unsupported format.
func TestService_InputGuards(t *testing.T) {
	svc := New(Deps{Repo: newMemRepo()})
	ctx := context.Background()

	if _, err := svc.AddAuthor(ctx, AddAuthorInput{Name: "  ", Monitored: true}); err == nil {
		t.Error("AddAuthor with blank name should fail")
	}
	if _, err := svc.AddTitle(ctx, 1, "\t", true); err == nil {
		t.Error("AddTitle with blank name should fail")
	}
	if _, err := svc.AddEdition(ctx, 1, "pdf", "en", "", "", 0, 0, true); err == nil {
		t.Error("AddEdition with unsupported format should fail")
	}
}

// TestService_New_DefaultsProfile asserts a service built without a profile
// defaults to "Best" (so an empty config still picks the best e-book format).
func TestService_New_DefaultsProfile(t *testing.T) {
	svc := New(Deps{Repo: newMemRepo()})
	if svc.deps.DefaultProfile != "Best" {
		t.Errorf("DefaultProfile = %q, want Best (default)", svc.deps.DefaultProfile)
	}
}

// TestService_RunPipeline_NoClient exercises the branch where a download client
// is not configured: a pending wanted is present, but no client → error.
func TestService_RunPipeline_NoClient(t *testing.T) {
	repo := newMemRepo()
	svc := New(Deps{Repo: repo, DefaultProfile: "Best"}) // no Client
	ctx := context.Background()
	authorID, _ := svc.AddAuthor(ctx, AddAuthorInput{Name: "A Author", Monitored: true})
	if _, err := svc.AddTitle(ctx, authorID, "Title", true); err != nil {
		t.Fatalf("add title: %v", err)
	}

	_, err := svc.RunPipeline(ctx, authorID)
	if err == nil {
		t.Fatal("RunPipeline with no client should fail")
	}
}

// TestService_RunPipeline_NoMatch exercises the branch where a pending wanted
// title has no matching release: the pipeline imports nothing and returns
// ErrNoMatch.
func TestService_RunPipeline_NoMatch(t *testing.T) {
	dlDir := t.TempDir()
	mock, _ := downloads.NewMockClient("mock", dlDir)
	fake := indexers.NewFakeIndexer("fake")
	// A release for a different title: no candidate matches "Title".
	fake.AddRelease(indexers.SearchResult{Title: "Someone Else - Unrelated [EPUB]", SizeBytes: 1000})

	repo := newMemRepo()
	svc := New(Deps{
		Repo:           repo,
		Indexers:       []indexers.Searcher{fake},
		Client:         mock,
		MediaRoot:      t.TempDir(),
		DefaultProfile: "Best",
	})
	ctx := context.Background()
	authorID, _ := svc.AddAuthor(ctx, AddAuthorInput{Name: "A Author", Monitored: true})
	if _, err := svc.AddTitle(ctx, authorID, "Title", true); err != nil {
		t.Fatalf("add title: %v", err)
	}

	_, err := svc.RunPipeline(ctx, authorID)
	if !errors.Is(err, ErrNoMatch) {
		t.Errorf("RunPipeline with no matching release: err = %v, want ErrNoMatch", err)
	}
}

// TestService_RunPipeline_NoWanted exercises the branch where the author has no
// pending wanted entries: the pipeline returns ErrNoWanted without searching.
func TestService_RunPipeline_NoWanted(t *testing.T) {
	svc := New(Deps{Repo: newMemRepo(), DefaultProfile: "Best"})
	ctx := context.Background()
	// An unmonitored author → no wanted rows.
	authorID, _ := svc.AddAuthor(ctx, AddAuthorInput{Name: "A Author", Monitored: false})

	_, err := svc.RunPipeline(ctx, authorID)
	if !errors.Is(err, ErrNoWanted) {
		t.Errorf("RunPipeline with no wanted: err = %v, want ErrNoWanted", err)
	}
}

// TestService_FileHelpers exercises the unexported file-naming and copy
// helpers that the import path relies on.
func TestService_FileHelpers(t *testing.T) {
	rel := dom.CandidateRelease{Title: "A Author - The Dispossessed [EPUB]", Format: dom.ProfileMatch{Format: dom.FormatEPUB}}
	if got := extForFormat(dom.FormatEPUB); got != ".epub" {
		t.Errorf("extForFormat(epub) = %q, want .epub", got)
	}
	if got := extForFormat(dom.FormatAZW3); got != ".azw3" {
		t.Errorf("extForFormat(azw3) = %q, want .azw3", got)
	}
	if got := extForFormat(dom.FormatMOBI); got != ".mobi" {
		t.Errorf("extForFormat(mobi) = %q, want .mobi", got)
	}
	if got := extForFormat("unknown"); got != ".epub" {
		t.Errorf("extForFormat(unknown) = %q, want .epub (default)", got)
	}
	fn := fileNameFor(dom.Title{Name: "The Dispossessed"}, rel)
	if fn != "the dispossessed.epub" {
		t.Errorf("fileNameFor = %q, want the dispossessed.epub", fn)
	}

	// copyFile creates the destination tree and copies the bytes.
	dir := t.TempDir()
	src := filepath.Join(dir, "src.epub")
	if err := os.WriteFile(src, []byte("book-bytes"), 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}
	dst := filepath.Join(dir, "A", "B", "dest.epub")
	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile: %v", err)
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if string(data) != "book-bytes" {
		t.Errorf("dst = %q, want book-bytes", string(data))
	}
	// Copying a missing source must fail.
	if err := copyFile(filepath.Join(dir, "missing.epub"), filepath.Join(dir, "x.epub")); err == nil {
		t.Error("copyFile of a missing source should fail")
	}
}
