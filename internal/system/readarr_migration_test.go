package system

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// fakeReadarrSource is the ReadarrClient fake for the migrator tests: it
// serves the same shapes the httptest client fake serves, in memory.
type fakeReadarrSource struct {
	authors     []ReadarrAuthor
	books       []ReadarrBook
	editions    []ReadarrEdition
	bookFiles   []ReadarrBookFile
	series      []ReadarrSeries
	profiles    []ReadarrQualityProfile
	defs        []ReadarrQualityDefinition
	metadatas   []ReadarrMetadataProfile
	rootFolders []ReadarrRootFolder
	tags        []ReadarrTag
	indexers    []ReadarrIndexer
	clients     []ReadarrDownloadClient
	mediaMgmt   *ReadarrMediaManagement
}

func (f *fakeReadarrSource) GetAuthors(context.Context) ([]ReadarrAuthor, error) {
	return f.authors, nil
}
func (f *fakeReadarrSource) GetBooks(context.Context) ([]ReadarrBook, error) {
	return f.books, nil
}
func (f *fakeReadarrSource) GetEditions(context.Context) ([]ReadarrEdition, error) {
	return f.editions, nil
}
func (f *fakeReadarrSource) GetBookFiles(context.Context) ([]ReadarrBookFile, error) {
	return f.bookFiles, nil
}
func (f *fakeReadarrSource) GetSeries(context.Context) ([]ReadarrSeries, error) { return f.series, nil }
func (f *fakeReadarrSource) GetQualityProfiles(context.Context) ([]ReadarrQualityProfile, error) {
	return f.profiles, nil
}
func (f *fakeReadarrSource) GetQualityDefinitions(context.Context) ([]ReadarrQualityDefinition, error) {
	return f.defs, nil
}
func (f *fakeReadarrSource) GetMetadataProfiles(context.Context) ([]ReadarrMetadataProfile, error) {
	return f.metadatas, nil
}
func (f *fakeReadarrSource) GetRootFolders(context.Context) ([]ReadarrRootFolder, error) {
	return f.rootFolders, nil
}
func (f *fakeReadarrSource) GetTags(context.Context) ([]ReadarrTag, error) { return f.tags, nil }
func (f *fakeReadarrSource) GetIndexers(context.Context) ([]ReadarrIndexer, error) {
	return f.indexers, nil
}
func (f *fakeReadarrSource) GetDownloadClients(context.Context) ([]ReadarrDownloadClient, error) {
	return f.clients, nil
}
func (f *fakeReadarrSource) GetMediaManagement(context.Context) (*ReadarrMediaManagement, error) {
	return f.mediaMgmt, nil
}

// sampleReadarrSource builds a two-author / three-book source: one Calibre
// root folder, one indexer, one download client, tags, and profiles.
func sampleReadarrSource() *fakeReadarrSource {
	return &fakeReadarrSource{
		authors: []ReadarrAuthor{
			{ID: 7, AuthorName: "Ursula K. Le Guin", ForeignAuthorID: "OL28986A",
				QualityProfileID: 3, MetadataProfileID: 2, Monitored: true, MonitorNewItems: true,
				RootFolderPath: "/media/books", Tags: []int{4}, Genres: []string{"Fantasy"}},
			{ID: 8, AuthorName: "Aldous Huxley", ForeignAuthorID: "OL28238A",
				QualityProfileID: 99, Monitored: false, RootFolderPath: "/media/books"},
		},
		books: []ReadarrBook{
			{ID: 21, Title: "The Dispossessed", AuthorID: 7, ForeignBookID: "OL8189755W", Monitored: true,
				Editions: []ReadarrEdition{
					{ID: 91, BookID: 21, ISBN13: "9780060792921", ASIN: "B000000001", Format: "EPUB", Language: "en", Monitored: true},
					{ID: 92, BookID: 21, ISBN13: "9780060533069", Format: "MOBI", Language: "en", Monitored: false},
				}},
			{ID: 22, Title: "A Wizard of Earthsea", AuthorID: 7, Monitored: true,
				Editions: []ReadarrEdition{
					{ID: 93, BookID: 22, ISBN13: "9780547747115", Format: "AZW3", Language: "en"},
				}},
			{ID: 23, Title: "Brave New World", AuthorID: 8, Monitored: false,
				Editions: []ReadarrEdition{
					{ID: 94, BookID: 23, ISBN13: "9780060850524", Format: "EPUB", Language: "en"},
				}},
		},
		bookFiles: []ReadarrBookFile{
			{ID: 301, AuthorID: 7, BookID: 21, Path: "/media/books/Le Guin/The Dispossessed.epub", Size: 1048576,
				Quality: ReadarrBookFileQuality{Quality: ReadarrQuality{Name: "Digital"}}},
		},
		series: []ReadarrSeries{
			{ID: 55, Title: "Earthsea Cycle", Links: []ReadarrSeriesLink{
				{ID: 1, SeriesID: 55, BookID: 21},
				{ID: 2, SeriesID: 55, BookID: 999}, // book not in the source book list
			}},
		},
		profiles: []ReadarrQualityProfile{
			{ID: 3, Name: "Digital Unlimited", CutoffQualityID: 1,
				Items: []ReadarrProfileQualityItem{{ID: 1, Name: "Digital", Quality: ReadarrQuality{ID: 1, Name: "Digital"}, Allowed: true}}},
		},
		defs: []ReadarrQualityDefinition{
			{ID: 1, Quality: ReadarrQuality{ID: 1, Name: "Digital"}},
		},
		metadatas: []ReadarrMetadataProfile{{ID: 2, Name: "Books"}},
		rootFolders: []ReadarrRootFolder{
			{ID: 1, Name: "media/books", Path: "/media/books",
				DefaultQualityProfileID: 3, DefaultMetadataProfileID: 2, DefaultTags: []int{4},
				IsCalibreLibrary: true, Host: "192.168.0.10", Port: 8080, Username: "admin",
				Password: "s3cret", Library: "Main", OutputFormat: "EPUB", OutputProfile: "kindle", UseSSL: false},
		},
		tags: []ReadarrTag{{ID: 4, Label: "Favorites"}},
		indexers: []ReadarrIndexer{
			{ID: 9, Name: "SomeNZB", Fields: nil, EnableRSS: true, EnableAutomaticSearch: true, EnableInteractiveSearch: true,
				SupportsRSS: true, SupportsSearch: true, Protocol: "http", Priority: 1, Tags: []int{4}},
		},
		clients: []ReadarrDownloadClient{
			{ID: 9, Name: "SickChill", Protocol: "http", Priority: 1, Enable: true},
		},
		mediaMgmt: &ReadarrMediaManagement{ID: 1, AutoUnmonitorPreviouslyDownloadedBooks: true, DeleteEmptyFolders: true,
			RecycleBin: true, RecycleBinCleanupDays: 14, MinimumFreeSpaceWhenImporting: 2048},
	}
}

// fakeReadarrTarget records every write call and emulates idempotency by
// identity (the books domain's identities).
type fakeReadarrTarget struct {
	writes int

	profiles     map[string]int64 // name -> id
	metadatas    map[string]int64
	tags         map[int]TagRef   // source id -> label
	indexers     map[string]int64 // name -> id
	download     map[string]int64 // name -> id
	rootFolders  map[string]int64 // path -> id
	authors      map[string]int64 // name -> id
	titles       map[string]int64 // authorName|title -> id
	titlesOLID   map[int64]string // title id -> OL key
	editions     map[string]int64 // titleID|format|isbn -> id
	bookFiles    map[string]int64 // titleID|path -> id
	series       map[string]int64 // name -> id
	seriesLinks  map[string]bool  // seriesID|titleID -> linked
	mediaApplied int
	mediaCalls   int
}

type TagRef struct {
	ID    int
	Label string
}

func newFakeReadarrTarget() *fakeReadarrTarget {
	return &fakeReadarrTarget{
		profiles:    map[string]int64{},
		metadatas:   map[string]int64{},
		tags:        map[int]TagRef{},
		indexers:    map[string]int64{},
		download:    map[string]int64{},
		rootFolders: map[string]int64{},
		authors:     map[string]int64{},
		titles:      map[string]int64{},
		titlesOLID:  map[int64]string{},
		editions:    map[string]int64{},
		bookFiles:   map[string]int64{},
		series:      map[string]int64{},
		seriesLinks: map[string]bool{},
	}
}

func (t *fakeReadarrTarget) nextID() int64 { return int64(t.writes + 1000) }
func (t *fakeReadarrTarget) touch()        { t.writes++ }

func (t *fakeReadarrTarget) EnsureBookProfile(ctx context.Context, p ReadarrQualityProfile, defs []ReadarrQualityDefinition) (int64, TargetAction, error) {
	if id, ok := t.profiles[p.Name]; ok {
		return id, ActionExists, nil
	}
	t.touch()
	t.profiles[p.Name] = t.nextID()
	return t.profiles[p.Name], ActionCreated, nil
}

func (t *fakeReadarrTarget) EnsureBookMetadataProfile(ctx context.Context, mp ReadarrMetadataProfile) (int64, TargetAction, error) {
	if id, ok := t.metadatas[mp.Name]; ok {
		return id, ActionExists, nil
	}
	t.touch()
	t.metadatas[mp.Name] = t.nextID()
	return t.metadatas[mp.Name], ActionCreated, nil
}

func (t *fakeReadarrTarget) EnsureTag(ctx context.Context, tag ReadarrTag) (int64, TargetAction, error) {
	if ref, ok := t.tags[tag.ID]; ok && ref.Label == tag.Label {
		return int64(tag.ID), ActionExists, nil
	}
	t.touch()
	t.tags[tag.ID] = TagRef{tag.ID, tag.Label}
	return int64(tag.ID), ActionCreated, nil
}

func (t *fakeReadarrTarget) TagLabels(ctx context.Context, ids []int) ([]string, error) {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if ref, ok := t.tags[id]; ok {
			out = append(out, ref.Label)
		}
	}
	return out, nil
}

func (t *fakeReadarrTarget) EnsureIndexer(ctx context.Context, ix ReadarrIndexer) (int64, TargetAction, error) {
	if id, ok := t.indexers[ix.Name]; ok {
		return id, ActionExists, nil
	}
	t.touch()
	t.indexers[ix.Name] = t.nextID()
	return t.indexers[ix.Name], ActionCreated, nil
}

func (t *fakeReadarrTarget) SetDownloadClient(ctx context.Context, c ReadarrDownloadClient) (TargetAction, error) {
	if _, ok := t.download[c.Name]; ok {
		return ActionExists, nil
	}
	t.touch()
	t.download[c.Name] = t.nextID()
	return ActionCreated, nil
}

func (t *fakeReadarrTarget) EnsureRootFolder(ctx context.Context, rf ReadarrRootFolder) (int64, TargetAction, error) {
	if id, ok := t.rootFolders[rf.Path]; ok {
		return id, ActionExists, nil
	}
	t.touch()
	t.rootFolders[rf.Path] = t.nextID()
	return t.rootFolders[rf.Path], ActionCreated, nil
}

func (t *fakeReadarrTarget) ApplyMediaManagement(ctx context.Context, mm ReadarrMediaManagement) (int, error) {
	t.mediaCalls++
	t.mediaApplied = 3
	return 3, nil
}

func (t *fakeReadarrTarget) EnsureAuthor(ctx context.Context, a ReadarrAuthorRef) (int64, TargetAction, error) {
	if id, ok := t.authors[a.Name]; ok {
		return id, ActionExists, nil
	}
	t.touch()
	t.authors[a.Name] = t.nextID()
	return t.authors[a.Name], ActionCreated, nil
}

func (t *fakeReadarrTarget) EnsureTitle(ctx context.Context, ref ReadarrTitleRef) (int64, TargetAction, error) {
	// Find the author name for the composite key.
	authorName := ""
	for name, id := range t.authors {
		if id == ref.AuthorID {
			authorName = name
			break
		}
	}
	key := authorName + "|" + ref.Name
	if id, ok := t.titles[key]; ok {
		return id, ActionExists, nil
	}
	t.touch()
	t.titles[key] = t.nextID()
	t.titlesOLID[t.titles[key]] = ref.OLID
	return t.titles[key], ActionCreated, nil
}

func (t *fakeReadarrTarget) EnsureEdition(ctx context.Context, e ReadarrEditionRef) (int64, TargetAction, error) {
	key := fmt.Sprintf("%d|%s|%s", e.TitleID, e.Format, e.ISBN13)
	if id, ok := t.editions[key]; ok {
		return id, ActionExists, nil
	}
	t.touch()
	t.editions[key] = t.nextID()
	return t.editions[key], ActionCreated, nil
}

func (t *fakeReadarrTarget) EnsureBookFile(ctx context.Context, f ReadarrBookFileRef) (int64, TargetAction, error) {
	key := fmt.Sprintf("%d|%s", f.TitleID, f.Path)
	if id, ok := t.bookFiles[key]; ok {
		return id, ActionExists, nil
	}
	t.touch()
	t.bookFiles[key] = t.nextID()
	return t.bookFiles[key], ActionCreated, nil
}

func (t *fakeReadarrTarget) EnsureSeries(ctx context.Context, name string) (int64, TargetAction, error) {
	if id, ok := t.series[name]; ok {
		return id, ActionExists, nil
	}
	t.touch()
	t.series[name] = t.nextID()
	return t.series[name], ActionCreated, nil
}

func (t *fakeReadarrTarget) EnsureSeriesLink(ctx context.Context, l ReadarrSeriesLinkRef) (TargetAction, error) {
	key := fmt.Sprintf("%d|%d", l.SeriesID, l.TitleID)
	if t.seriesLinks[key] {
		return ActionExists, nil
	}
	t.touch()
	t.seriesLinks[key] = true
	return ActionCreated, nil
}

// memReadarrState is the in-memory ReadarrMigrationState fake.
type memReadarrState struct {
	keys map[string]bool
}

func newMemReadarrState() *memReadarrState { return &memReadarrState{keys: map[string]bool{}} }
func (s *memReadarrState) IsMigrated(_ context.Context, key string) (bool, error) {
	return s.keys[key], nil
}
func (s *memReadarrState) MarkMigrated(_ context.Context, key string) error {
	s.keys[key] = true
	return nil
}
func (s *memReadarrState) migrated(key string) bool { return s.keys[key] }

// fakeOLResolver resolves by explicit rule: foreignBookID wins when it
// looks like an OL key, else ISBN/ASIN/title through a lookup table.
type fakeOLResolver struct {
	calls int
}

func (r *fakeOLResolver) ResolveWorkKey(_ context.Context, id ReadarrBookIdentity) (string, string, error) {
	r.calls++
	if strings.HasPrefix(id.ForeignBookID, "OL") {
		return id.ForeignBookID, "source", nil
	}
	if id.ISBN13 != "" {
		return "OLFROMISBN" + id.ISBN13, "isbn", nil
	}
	if id.ASIN != "" {
		return "OLFROMASIN" + id.ASIN, "asin", nil
	}
	if id.Title != "" {
		return "OLSEARCHED", "search", nil
	}
	return "", "search", nil
}

func newReadarrTestMigrator(src *fakeReadarrSource, target *fakeReadarrTarget, resolver ReadarrBookIDResolver, state *memReadarrState) *ReadarrMigrator {
	m := NewReadarrMigrator(target, resolver)
	m.NewSource = func(baseURL, apiKey string) ReadarrClient { return src }
	m.State = state
	return m
}

// TestReadarrDryRunMakesNoWrites: the dry-run pulls, maps, and reports
// without a single target write.
func TestReadarrDryRunMakesNoWrites(t *testing.T) {
	src := sampleReadarrSource()
	target := newFakeReadarrTarget()
	m := newReadarrTestMigrator(src, target, &fakeOLResolver{}, newMemReadarrState())

	result, err := m.Run(context.Background(), MigrationRequest{SourceURL: "http://readarr:8888", Source: SourceReadarr, APIKey: "k", DryRun: true})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if target.writes != 0 {
		t.Fatalf("dry-run performed %d writes", target.writes)
	}
	if result.Counts.Total == 0 {
		t.Fatal("dry-run reported no items")
	}
	for _, it := range result.Items {
		if it.Action != "dry-run" {
			t.Fatalf("item %s/%s action = %q, want dry-run", it.Entity, it.SourceTitle, it.Action)
		}
	}
	// The Calibre root folder is reported with the password masked out.
	for _, it := range result.Items {
		if it.Entity == "root_folder" {
			if strings.Contains(it.Message, "s3cret") {
				t.Fatal("report leaked the Calibre password")
			}
			if !strings.Contains(it.Message, "calibre library") {
				t.Fatalf("calibre note missing: %q", it.Message)
			}
		}
	}
	if status := m.Status(); status.Status != "completed" || status.LastResult == nil {
		t.Fatalf("status after dry-run = %+v", status)
	}
}

// TestReadarrImportMapsEverything: an execute run lands every entity with
// the right identity (OL keys, lowercase formats, tag labels, series links).
func TestReadarrImportMapsEverything(t *testing.T) {
	src := sampleReadarrSource()
	target := newFakeReadarrTarget()
	resolver := &fakeOLResolver{}
	m := newReadarrTestMigrator(src, target, resolver, newMemReadarrState())

	result, err := m.Run(context.Background(), MigrationRequest{SourceURL: "http://readarr:8888", Source: SourceReadarr, APIKey: "k"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.Counts.Created == 0 {
		t.Fatalf("expected creations, got %+v", result.Counts)
	}

	// Profiles by name (the books profile document is name-keyed).
	if _, ok := target.profiles["Digital Unlimited"]; !ok {
		t.Fatalf("quality profile not imported: %v", target.profiles)
	}
	if _, ok := target.metadatas["Books"]; !ok {
		t.Fatalf("metadata profile not imported: %v", target.metadatas)
	}
	if target.tags[4].Label != "Favorites" {
		t.Fatalf("tag not imported: %v", target.tags)
	}
	if _, ok := target.indexers["SomeNZB"]; !ok {
		t.Fatalf("indexer not imported: %v", target.indexers)
	}
	if _, ok := target.download["SickChill"]; !ok {
		t.Fatalf("download client not imported: %v", target.download)
	}
	if _, ok := target.rootFolders["/media/books"]; !ok {
		t.Fatalf("root folder not imported: %v", target.rootFolders)
	}
	if target.mediaCalls == 0 {
		t.Fatal("media management not applied")
	}
	if len(target.authors) != 2 {
		t.Fatalf("authors = %v", target.authors)
	}
	// Books keyed by OpenLibrary work keys: the first book already carries
	// one (source), the others are swapped in via the resolver.
	got := map[string]string{}
	for name, id := range target.titles {
		got[name] = target.titlesOLID[id]
	}
	if got["Ursula K. Le Guin|The Dispossessed"] != "OL8189755W" {
		t.Fatalf("Dispossessed OL key = %q, want OL8189755W (source id carried through)", got["Ursula K. Le Guin|The Dispossessed"])
	}
	if got["Ursula K. Le Guin|A Wizard of Earthsea"] != "OLFROMISBN9780547747115" {
		t.Fatalf("Earthsea OL key = %q, want the ISBN-swapped key", got["Ursula K. Le Guin|A Wizard of Earthsea"])
	}
	if len(target.titles) != 3 {
		t.Fatalf("titles = %v", got)
	}
	// Editions lower-cased + isbn-keyed.
	if len(target.editions) != 4 {
		t.Fatalf("editions = %v", target.editions)
	}
	if len(target.bookFiles) != 1 {
		t.Fatalf("book files = %v", target.bookFiles)
	}
	// Series: one link created, the dangling one skipped.
	if _, ok := target.series["Earthsea Cycle"]; !ok {
		t.Fatalf("series not imported: %v", target.series)
	}
	if len(target.seriesLinks) != 1 {
		t.Fatalf("series links = %v (want 1 created, 1 skipped)", target.seriesLinks)
	}
	skipped := 0
	for _, it := range result.Items {
		if it.Action == "skipped" {
			skipped++
		}
	}
	if skipped != 1 {
		t.Fatalf("skipped items = %d, want 1 (the dangling series link)", skipped)
	}
}

// TestReadarrImportIdempotentSecondRun: a second run over the same source is
// a no-op — everything reports exists, nothing is created.
func TestReadarrImportIdempotentSecondRun(t *testing.T) {
	src := sampleReadarrSource()
	target := newFakeReadarrTarget()
	m := newReadarrTestMigrator(src, target, &fakeOLResolver{}, newMemReadarrState())
	req := MigrationRequest{SourceURL: "http://readarr:8888", Source: SourceReadarr, APIKey: "k"}

	if _, err := m.Run(context.Background(), req); err != nil {
		t.Fatalf("first run: %v", err)
	}
	firstWrites := target.writes
	if _, err := m.Run(context.Background(), req); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if target.writes != firstWrites {
		t.Fatalf("second run wrote %d more entities", target.writes-firstWrites)
	}
	// Re-run the migrator fresh (as a restart would) on the same target.
	m2 := newReadarrTestMigrator(src, target, &fakeOLResolver{}, newMemReadarrState())
	result, err := m2.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("third run: %v", err)
	}
	if result.Counts.Created != 0 || result.Counts.Existing == 0 {
		t.Fatalf("re-run counts = %+v, want all existing", result.Counts)
	}
}

// TestReadarrResumeSkipsMigratedBooks: after a partial run, resume=true
// skips the books the state says are done and continues the rest.
func TestReadarrResumeSkipsMigratedBooks(t *testing.T) {
	src := sampleReadarrSource()
	target := newFakeReadarrTarget()
	state := newMemReadarrState()
	// Simulate a prior partial run that migrated only the first book.
	state.keys["OL8189755W"] = true
	m := newReadarrTestMigrator(src, target, &fakeOLResolver{}, state)

	result, err := m.Run(context.Background(), MigrationRequest{
		SourceURL: "http://readarr:8888", Source: SourceReadarr, APIKey: "k", Resume: true,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var dispossessed *MigrationItem
	for i := range result.Items {
		if result.Items[i].Entity == "book" && result.Items[i].SourceTitle == "The Dispossessed" {
			dispossessed = &result.Items[i]
		}
	}
	if dispossessed == nil {
		t.Fatal("no Dispossessed book item")
	}
	if dispossessed.Action != "skipped" {
		t.Fatalf("Dispossessed action = %q, want skipped (resume)", dispossessed.Action)
	}
	if result.Counts.Skipped < 1 {
		t.Fatalf("counts = %+v, want >= 1 skipped", result.Counts)
	}
}

// TestReadarrConflictNotes: dangling profile ids, the missing-per-indexer
// enable flag, and an unknown Calibre profile all surface as report notes;
// the Calibre password never appears in the report.
func TestReadarrConflictNotes(t *testing.T) {
	src := sampleReadarrSource()
	target := newFakeReadarrTarget()
	// Huxley references profile id 99, which is not in the source list.
	m := newReadarrTestMigrator(src, target, &fakeOLResolver{}, newMemReadarrState())

	result, err := m.Run(context.Background(), MigrationRequest{SourceURL: "http://readarr:8888", Source: SourceReadarr, APIKey: "k"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var huxleyNote, indexerNote, calibreNote string
	for _, it := range result.Items {
		switch {
		case it.Entity == "author" && it.SourceTitle == "Aldous Huxley":
			huxleyNote = it.Message
		case it.Entity == "indexer":
			indexerNote = it.Message
		case it.Entity == "root_folder":
			calibreNote = it.Message
		}
	}
	if !strings.Contains(huxleyNote, "profile id 99") {
		t.Fatalf("huxley note = %q, want the dangling-profile note", huxleyNote)
	}
	if !strings.Contains(indexerNote, "no per-indexer enable flag") {
		t.Fatalf("indexer note = %q, want the v0.4 enable-flag note", indexerNote)
	}
	for _, note := range []string{huxleyNote, indexerNote, calibreNote} {
		if strings.Contains(note, "s3cret") {
			t.Fatal("report leaked the Calibre password")
		}
	}
	if !strings.Contains(calibreNote, "output profile") {
		t.Fatalf("calibre note = %q, want the output-profile mapping", calibreNote)
	}
}

// TestReadarrBookIDSwapRules: the resolver is consulted per book; a
// resolver failure degrades to the source id with a note (the book is
// still imported).
func TestReadarrBookIDSwapRules(t *testing.T) {
	src := sampleReadarrSource()
	target := newFakeReadarrTarget()

	failing := &failingResolver{}
	m := newReadarrTestMigrator(src, target, failing, newMemReadarrState())
	result, err := m.Run(context.Background(), MigrationRequest{SourceURL: "http://readarr:8888", Source: SourceReadarr, APIKey: "k"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	found := false
	for _, it := range result.Items {
		if it.Entity == "book" && strings.Contains(it.Message, "openlibrary resolution failed") {
			found = true
			if !strings.Contains(it.ExternalID, "local:") {
				t.Fatalf("unresolved book external id = %q, want local:<id>", it.ExternalID)
			}
		}
	}
	if !found {
		t.Fatal("no book with a resolver-failure note")
	}
	if len(target.titles) != 3 {
		t.Fatalf("titles = %d, want 3 (unresolved books still import)", len(target.titles))
	}
}

type failingResolver struct{}

func (failingResolver) ResolveWorkKey(context.Context, ReadarrBookIdentity) (string, string, error) {
	return "", "", errors.New("boom")
}

// TestCalibreProfileIndex: the v0.4 slug -> enum-index mapping, with the
// unknown-slug fallback.
func TestCalibreProfileIndex(t *testing.T) {
	if idx, ok := calibreProfileIndex(""); !ok || idx != 0 {
		t.Fatalf("empty slug = %d,%v, want 0,true", idx, ok)
	}
	if idx, ok := calibreProfileIndex("default"); !ok || idx != 0 {
		t.Fatalf("default = %d,%v, want 0,true", idx, ok)
	}
	if idx, ok := calibreProfileIndex("Kindle"); !ok || idx == 0 {
		t.Fatalf("kindle = %d,%v, want a non-zero index", idx, ok)
	}
	if idx, ok := calibreProfileIndex("not-a-profile"); ok {
		t.Fatalf("unknown slug = %d,%v, want ok=false", idx, ok)
	}
}

// TestReadarrMigratorRequiresURL: validation of the request.
func TestReadarrMigratorRequiresURL(t *testing.T) {
	m := NewReadarrMigrator(newFakeReadarrTarget(), nil)
	if _, err := m.Run(context.Background(), MigrationRequest{Source: SourceReadarr}); err == nil {
		t.Fatal("expected an error for a missing source_url")
	}
	if _, err := m.Run(context.Background(), MigrationRequest{SourceURL: "ftp://nope", Source: SourceReadarr}); err == nil {
		t.Fatal("expected an error for a non-http source_url")
	}
}

// TestReadarrServiceRouting: the shared router dispatches 'readarr'.
func TestReadarrServiceRouting(t *testing.T) {
	ran := false
	runner := &readarrRoutingRunner{run: func() *MigrationResult {
		ran = true
		return &MigrationResult{}
	}}
	svc := NewMigrationService(nil, nil, nil, nil, runner)
	if _, err := svc.Run(context.Background(), MigrationRequest{SourceURL: "http://r", Source: SourceReadarr}); err != nil {
		t.Fatalf("readarr routing: %v", err)
	}
	if !ran {
		t.Fatal("readarr runner not invoked")
	}
}

type readarrRoutingRunner struct {
	run     func() *MigrationResult
	status  string
	lastRes *MigrationResult
}

func (r *readarrRoutingRunner) Run(ctx context.Context, req MigrationRequest) (*MigrationResult, error) {
	res := r.run()
	r.lastRes = res
	return res, nil
}

func (r *readarrRoutingRunner) Status() MigrationStatus {
	if r.lastRes != nil {
		return MigrationStatus{Status: "completed"}
	}
	return MigrationStatus{Status: "idle"}
}

// TestReadarrUnwiredSource: a nil readarr runner answers a validation error.
func TestReadarrUnwiredSource(t *testing.T) {
	svc := NewMigrationService(&readarrRoutingRunner{}, nil, nil, nil, nil)
	if _, err := svc.Run(context.Background(), MigrationRequest{SourceURL: "http://r", Source: SourceReadarr}); err == nil {
		t.Fatal("expected an error for an unwired readarr source")
	}
}
