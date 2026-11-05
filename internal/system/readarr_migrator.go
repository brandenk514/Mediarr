package system

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Readarr B6: the Readarr drop-in migration.
//
// ReadarrMigrator is the books-side counterpart of the P6/R6/Lidarr
// migrators: it pulls a live Readarr v0.4.18.2805 instance (the final
// release before the project was archived) over the /api/v1 REST API
// (authors + books + editions + book files + series links + quality
// profiles + metadata profiles + tags + root folders (with the Calibre
// block) + indexers + download clients + a best-effort settings subset)
// and imports it into Mediarr's books domain, returning a mapping report.
// Same guarantees as the other runs:
//
//   - dry-run:  report what WOULD be imported without writing anything.
//   - idempotent: re-running converges — an already-imported entity maps
//     to action "exists" (authors by name, titles by author+name, editions
//     by title+format+isbn, book files by title+path, series by name).
//   - resumable: a partial run records which books were migrated;
//     resume=true skips those and continues. Unlike the in-memory state
//     of the other runs, the state seam (ReadarrMigrationState) is
//     Postgres-backed in the real target so a restart continues.
//
// The one-time ID swap happens here, at import: each source book is
// resolved to its OpenLibrary work key (foreignBookId when it already is
// one, else via ISBN/ASIN/title through ReadarrBookIDResolver) and the
// title is stored keyed by it (the music-domain MBID analogue).
//
// The shared MigrationInclude flags apply to Readarr as series ->
// authors+books+series, episodes -> editions+book files, and the
// quality_profiles flag covers both book quality and metadata profiles.
// ---------------------------------------------------------------------------

// ReadarrBookIDResolver resolves a source book to its OpenLibrary work key
// (the "OL8189755W" shape) — the one-time ISBN/ASIN/foreign-id ->
// OpenLibrary-id swap of the migration. method reports how the key was
// found: "source" (the source already carried an OL work key), "isbn",
// "asin", or "search" (title+author).
type ReadarrBookIDResolver interface {
	ResolveWorkKey(ctx context.Context, id ReadarrBookIdentity) (key, method string, err error)
}

// ReadarrBookIdentity is the source book's identity material for the
// resolver.
type ReadarrBookIdentity struct {
	ForeignBookID string // source foreignBookId ("", or already an OL key)
	Title         string
	Author        string
	ISBN13        string
	ASIN          string
}

// ReadarrMigrationState persists which source books a run has already
// migrated, so an interrupted run resumes across process restarts.
// The real target implements it in Postgres; tests use a map. Keys are
// (sourceURL, key): a state row belongs to the run's source instance.
type ReadarrMigrationState interface {
	IsMigrated(ctx context.Context, sourceURL, key string) (bool, error)
	MarkMigrated(ctx context.Context, sourceURL, key string) error
}

// ---------------------------------------------------------------------------
// ReadarrMigrationTarget: the swappable Mediarr import seam (books domain).
// ---------------------------------------------------------------------------

// ReadarrAuthorRef is the minimal author row the target needs to import.
// QualityProfile / MetadataProfile are resolved profile NAMES ("" = target
// default); the target must not see source profile ids.
type ReadarrAuthorRef struct {
	Name            string
	OLID            string // OpenLibrary author key ("" = unknown)
	Monitored       bool
	MonitorNewItems bool
	QualityProfile  string
	MetadataProfile string
	RootFolderPath  string
	Tags            []string
	Genres          []string
}

// ReadarrTitleRef is one book title to ensure. OLID is the OpenLibrary work
// key the title is keyed by ("" = unresolved; keyed by name only).
type ReadarrTitleRef struct {
	AuthorID  int64
	Name      string
	OLID      string
	Monitored bool
}

// ReadarrEditionRef is one edition to ensure under a title (keyed by
// format+isbn, the books domain's identity).
type ReadarrEditionRef struct {
	TitleID   int64
	Format    string
	Language  string
	ISBN13    string
	Monitored bool
}

// ReadarrBookFileRef is one imported file to ensure (keyed by title+path).
// EditionID is 0 when the file has no known edition match.
type ReadarrBookFileRef struct {
	TitleID   int64
	EditionID int64
	Path      string
	Format    string
	Size      int64
}

// ReadarrSeriesLinkRef ties an imported series to an imported title.
type ReadarrSeriesLinkRef struct {
	SeriesID int64
	TitleID  int64
}

// ReadarrMigrationTarget is the write surface for importing a Readarr
// instance into Mediarr. Each Ensure* is idempotent: it creates the entity
// when absent (returns ActionCreated) and reports ActionExists when already
// present (never duplicating). The Postgres-backed target implements this;
// tests use a fake.
type ReadarrMigrationTarget interface {
	// Config + registries.
	EnsureBookProfile(ctx context.Context, p ReadarrQualityProfile, defs []ReadarrQualityDefinition) (id int64, action TargetAction, err error)
	EnsureBookMetadataProfile(ctx context.Context, mp ReadarrMetadataProfile) (id int64, action TargetAction, err error)
	EnsureTag(ctx context.Context, t ReadarrTag) (id int64, action TargetAction, err error)
	TagLabels(ctx context.Context, ids []int) ([]string, error)
	EnsureIndexer(ctx context.Context, ix ReadarrIndexer) (id int64, action TargetAction, err error)
	SetDownloadClient(ctx context.Context, c ReadarrDownloadClient) (action TargetAction, err error)
	EnsureRootFolder(ctx context.Context, rf ReadarrRootFolder, defaultQuality, defaultMetadata string) (id int64, action TargetAction, err error)
	// ApplyMediaManagement maps the best-effort media-management subset and
	// returns the number of fields written.
	ApplyMediaManagement(ctx context.Context, mm ReadarrMediaManagement) (int, error)
	// Books.
	EnsureAuthor(ctx context.Context, a ReadarrAuthorRef) (id int64, action TargetAction, err error)
	EnsureTitle(ctx context.Context, t ReadarrTitleRef) (id int64, action TargetAction, err error)
	EnsureEdition(ctx context.Context, e ReadarrEditionRef) (id int64, action TargetAction, err error)
	EnsureBookFile(ctx context.Context, f ReadarrBookFileRef) (id int64, action TargetAction, err error)
	EnsureSeries(ctx context.Context, name string) (id int64, action TargetAction, err error)
	EnsureSeriesLink(ctx context.Context, l ReadarrSeriesLinkRef) (action TargetAction, err error)
}

// ---------------------------------------------------------------------------
// ReadarrMigrator.
// ---------------------------------------------------------------------------

// ReadarrMigrator runs Readarr migrations. NewSource is the swappable client
// factory (tests inject a mock); Target is the Mediarr books import surface;
// Resolver performs the OpenLibrary id swap; State persists book migration
// keys for resume (nil = in-memory fallback).
type ReadarrMigrator struct {
	NewSource func(baseURL, apiKey string) ReadarrClient
	Target    ReadarrMigrationTarget
	Resolver  ReadarrBookIDResolver
	State     ReadarrMigrationState

	mu         sync.Mutex
	status     string
	running    bool
	startedAt  *time.Time
	finishedAt *time.Time
	done       int
	total      int
	lastResult *MigrationResult
	// migrated remembers migrated book keys across runs for resume (the
	// in-memory twin of State, used when State is nil).
	migrated map[string]bool
}

// NewReadarrMigrator builds a ReadarrMigrator with the default HTTP source
// factory and the given books target + OpenLibrary resolver.
func NewReadarrMigrator(target ReadarrMigrationTarget, resolver ReadarrBookIDResolver) *ReadarrMigrator {
	return &ReadarrMigrator{
		NewSource: NewReadarrClient,
		Target:    target,
		Resolver:  resolver,
		status:    "idle",
		migrated:  map[string]bool{},
	}
}

// Status returns a snapshot of the current Readarr migration status.
func (m *ReadarrMigrator) Status() MigrationStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	var last json.RawMessage
	if m.lastResult != nil {
		b, _ := json.Marshal(m.lastResult)
		last = b
	}
	return MigrationStatus{
		Status:     m.status,
		Running:    m.running,
		StartedAt:  m.startedAt,
		FinishedAt: m.finishedAt,
		Done:       m.done,
		Total:      m.total,
		LastResult: last,
	}
}

// Run executes a Readarr migration. On a target/source error mid-run it
// records a partial (resumable) status and returns the mapping report for
// what completed.
func (m *ReadarrMigrator) Run(ctx context.Context, req MigrationRequest) (*MigrationResult, error) {
	if req.SourceURL == "" {
		return nil, fmt.Errorf("migration: source_url is required")
	}
	if u, err := url.Parse(req.SourceURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("migration: source_url must be an http/https URL")
	}
	if m.Target == nil {
		return nil, fmt.Errorf("migration: the readarr migration is not wired")
	}

	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return nil, fmt.Errorf("migration: a migration is already running")
	}
	now := time.Now()
	m.running = true
	m.status = "running"
	m.startedAt = &now
	m.finishedAt = nil
	m.done = 0
	m.mu.Unlock()

	result, runErr := m.runLocked(ctx, req)

	m.mu.Lock()
	m.running = false
	finished := time.Now()
	m.finishedAt = &finished
	if result != nil && result.Resumable {
		m.status = "partial"
	} else if runErr != nil {
		m.status = "failed"
	} else {
		m.status = "completed"
	}
	m.lastResult = result
	m.mu.Unlock()

	return result, runErr
}

func (m *ReadarrMigrator) runLocked(ctx context.Context, req MigrationRequest) (*MigrationResult, error) {
	inc := req.Include
	if inc == nil {
		inc = &MigrationInclude{}
	}
	src := m.NewSource(req.SourceURL, req.APIKey)
	if m.migrated == nil {
		m.migrated = map[string]bool{}
	}
	result := &MigrationResult{
		DryRun:    req.DryRun,
		Resume:    req.Resume,
		SourceURL: req.SourceURL,
		Items:     []MigrationItem{},
	}

	// 1. Book quality profiles. The source quality definitions (the v0.4
	// /qualitydefinition endpoint) supply the source quality NAMES; the
	// target maps a profile's approved set onto Mediarr's book level
	// space. id -> name keeps author qualityProfileId references resolvable.
	qualityDefs, _ := src.GetQualityDefinitions(ctx) // best-effort: cutoffs degrade to the "Any" profile
	profileNamesByID := map[int]string{}
	if inc.wantProfiles() {
		profiles, err := src.GetQualityProfiles(ctx)
		if err != nil {
			return result, err
		}
		for _, p := range profiles {
			profileNamesByID[p.ID] = p.Name
			if req.DryRun {
				result.Items = append(result.Items, MigrationItem{
					Entity: "quality_profile", ExternalID: fmt.Sprint(p.ID), SourceTitle: p.Name, Action: "dry-run",
				})
				result.Counts.Total++
				continue
			}
			id, action, err := m.Target.EnsureBookProfile(ctx, p, qualityDefs)
			if err != nil {
				result.Counts.Total++
				result.Counts.Errors++
				result.Items = append(result.Items, MigrationItem{Entity: "quality_profile", ExternalID: fmt.Sprint(p.ID), SourceTitle: p.Name, Action: "error", Message: err.Error()})
				return result, err
			}
			it := MigrationItem{Entity: "quality_profile", ExternalID: fmt.Sprint(p.ID), SourceTitle: p.Name, Action: string(action), TargetID: id}
			addMigrated(result, it)
		}
	}

	// 2. Book metadata profiles (id -> name for author references).
	metadataNamesByID := map[int]string{}
	if inc.wantProfiles() {
		metadatas, err := src.GetMetadataProfiles(ctx)
		if err != nil {
			return result, err
		}
		for _, mp := range metadatas {
			metadataNamesByID[mp.ID] = mp.Name
			if req.DryRun {
				result.Items = append(result.Items, MigrationItem{
					Entity: "metadata_profile", ExternalID: fmt.Sprint(mp.ID), SourceTitle: mp.Name, Action: "dry-run",
				})
				result.Counts.Total++
				continue
			}
			id, action, err := m.Target.EnsureBookMetadataProfile(ctx, mp)
			if err != nil {
				result.Counts.Total++
				result.Counts.Errors++
				result.Items = append(result.Items, MigrationItem{Entity: "metadata_profile", ExternalID: fmt.Sprint(mp.ID), SourceTitle: mp.Name, Action: "error", Message: err.Error()})
				return result, err
			}
			addMigrated(result, MigrationItem{Entity: "metadata_profile", ExternalID: fmt.Sprint(mp.ID), SourceTitle: mp.Name, Action: string(action), TargetID: id})
		}
	}

	// 3. Tags (source ids preserved for author references).
	if inc.wantTags() {
		tags, err := src.GetTags(ctx)
		if err != nil {
			return result, err
		}
		for _, t := range tags {
			if req.DryRun {
				result.Items = append(result.Items, MigrationItem{Entity: "tag", ExternalID: fmt.Sprint(t.ID), SourceTitle: t.Label, Action: "dry-run"})
				result.Counts.Total++
				continue
			}
			id, action, err := m.Target.EnsureTag(ctx, t)
			if err != nil {
				result.Counts.Total++
				result.Counts.Errors++
				result.Items = append(result.Items, MigrationItem{Entity: "tag", ExternalID: fmt.Sprint(t.ID), SourceTitle: t.Label, Action: "error", Message: err.Error()})
				return result, err
			}
			addMigrated(result, MigrationItem{Entity: "tag", ExternalID: fmt.Sprint(t.ID), SourceTitle: t.Label, Action: string(action), TargetID: id})
		}
	}

	// 4. Indexers. The v0.4 contract has no per-indexer enable flag, so
	// imported indexers land enabled (the report notes it per entity).
	if inc.wantIndexers() {
		indexers, err := src.GetIndexers(ctx)
		if err != nil {
			return result, err
		}
		for _, ix := range indexers {
			if req.DryRun {
				result.Items = append(result.Items, MigrationItem{Entity: "indexer", ExternalID: fmt.Sprint(ix.ID), SourceTitle: ix.Name, Action: "dry-run"})
				result.Counts.Total++
				continue
			}
			id, action, err := m.Target.EnsureIndexer(ctx, ix)
			if err != nil {
				result.Counts.Total++
				result.Counts.Errors++
				result.Items = append(result.Items, MigrationItem{Entity: "indexer", ExternalID: fmt.Sprint(ix.ID), SourceTitle: ix.Name, Action: "error", Message: err.Error()})
				return result, err
			}
			it := MigrationItem{Entity: "indexer", ExternalID: fmt.Sprint(ix.ID), SourceTitle: ix.Name, Action: string(action), TargetID: id,
				Message: "v0.4 exposes no per-indexer enable flag; imported enabled"}
			addMigrated(result, it)
		}
	}

	// 5. Download clients (Readarr supports several; each is upserted by
	// name, like the Prowlarr PR5 saved-client store).
	if inc.wantDownloadClient() {
		clients, err := src.GetDownloadClients(ctx)
		if err != nil {
			return result, err
		}
		for _, dc := range clients {
			if req.DryRun {
				result.Items = append(result.Items, MigrationItem{Entity: "download_client", ExternalID: fmt.Sprint(dc.ID), SourceTitle: dc.Name, Action: "dry-run"})
				result.Counts.Total++
				continue
			}
			action, err := m.Target.SetDownloadClient(ctx, dc)
			if err != nil {
				result.Counts.Total++
				result.Counts.Errors++
				result.Items = append(result.Items, MigrationItem{Entity: "download_client", ExternalID: fmt.Sprint(dc.ID), SourceTitle: dc.Name, Action: "error", Message: err.Error()})
				return result, err
			}
			addMigrated(result, MigrationItem{Entity: "download_client", ExternalID: fmt.Sprint(dc.ID), SourceTitle: dc.Name, Action: string(action)})
		}
	}

	// 6. Root folders (with the Calibre block where enabled). The source
	// path is the identity: a folder at the same local path is reused.
	rootFolderPathSet := map[string]bool{}
	if inc.wantRootFolders() {
		rootFolders, err := src.GetRootFolders(ctx)
		if err != nil {
			return result, err
		}
		for _, rf := range rootFolders {
			if rf.Path != "" {
				rootFolderPathSet[rf.Path] = true
			}
			if req.DryRun {
				msg := ""
				if rf.IsCalibreLibrary {
					msg = calibreReportNote(rf)
				}
				result.Items = append(result.Items, MigrationItem{Entity: "root_folder", ExternalID: fmt.Sprint(rf.ID), SourceTitle: rf.Path, Action: "dry-run", Message: msg})
				result.Counts.Total++
				continue
			}
			id, action, err := m.Target.EnsureRootFolder(ctx, rf,
				profileNamesByID[rf.DefaultQualityProfileID],
				metadataNamesByID[rf.DefaultMetadataProfileID])
			if err != nil {
				result.Counts.Total++
				result.Counts.Errors++
				result.Items = append(result.Items, MigrationItem{Entity: "root_folder", ExternalID: fmt.Sprint(rf.ID), SourceTitle: rf.Path, Action: "error", Message: err.Error()})
				return result, err
			}
			it := MigrationItem{Entity: "root_folder", ExternalID: fmt.Sprint(rf.ID), SourceTitle: rf.Path, Action: string(action), TargetID: id}
			if rf.IsCalibreLibrary {
				it.Message = calibreReportNote(rf)
			}
			addMigrated(result, it)
		}
	}

	// 7. Settings (best-effort media-management subset; a failure degrades
	// to a report note and never fails the run).
	if req.DryRun {
		result.Items = append(result.Items, MigrationItem{Entity: "settings", Action: "dry-run"})
		result.Counts.Total++
	} else {
		notes := make([]string, 0, 1)
		action := TargetAction("exists")
		if mm, err := src.GetMediaManagement(ctx); err != nil {
			notes = append(notes, "media management unavailable: "+err.Error())
		} else if n, err := m.Target.ApplyMediaManagement(ctx, *mm); err != nil {
			notes = append(notes, "media management failed: "+err.Error())
			result.Counts.Errors++
		} else {
			if n > 0 {
				action = ActionUpdated
			}
			notes = append(notes, fmt.Sprintf("media management: %d fields mapped", n))
		}
		addMigrated(result, MigrationItem{Entity: "settings", Action: string(action), Message: strings.Join(notes, "; ")})
	}

	// 8. Authors (include.series).
	authorIDs := map[int]int64{}
	authorNameByID := map[int]string{}
	if inc.wantSeries() {
		authors, err := src.GetAuthors(ctx)
		if err != nil {
			return result, err
		}
		for _, a := range authors {
			authorNameByID[a.ID] = a.AuthorName
			if req.DryRun {
				result.Items = append(result.Items, MigrationItem{Entity: "author", ExternalID: fmt.Sprint(a.ID), SourceTitle: a.AuthorName, Action: "dry-run"})
				result.Counts.Total++
				continue
			}
			profName, profNote := resolveBookProfile(profileNamesByID, a.QualityProfileID)
			metaName, metaNote := resolveBookProfile(metadataNamesByID, a.MetadataProfileID)
			labels, err := m.Target.TagLabels(ctx, a.Tags)
			if err != nil {
				return result, err
			}
			ref := ReadarrAuthorRef{
				Name:            a.AuthorName,
				OLID:            a.ForeignAuthorID,
				Monitored:       a.Monitored,
				MonitorNewItems: a.MonitorNewItems,
				QualityProfile:  profName,
				MetadataProfile: metaName,
				RootFolderPath:  a.RootFolderPath,
				Tags:            labels,
				Genres:          a.Genres,
			}
			if a.RootFolderPath != "" && !rootFolderPathSet[a.RootFolderPath] {
				ref.RootFolderPath = ""
				notes := "author root folder not in the imported set; using the server default"
				if profNote != "" {
					notes = profNote + "; " + notes
				}
				profNote = notes
			}
			id, action, err := m.Target.EnsureAuthor(ctx, ref)
			if err != nil {
				result.Counts.Total++
				result.Counts.Errors++
				it := MigrationItem{Entity: "author", ExternalID: fmt.Sprint(a.ID), SourceTitle: a.AuthorName, Action: "error", Message: err.Error()}
				if profNote != "" || metaNote != "" {
					it.Message = joinNotes(profNote, metaNote, it.Message)
				}
				result.Items = append(result.Items, it)
				result.Resumable = true
				return result, err
			}
			authorIDs[a.ID] = id
			it := MigrationItem{Entity: "author", ExternalID: fmt.Sprint(a.ID), SourceTitle: a.AuthorName, Action: string(action), TargetID: id}
			if profNote != "" || metaNote != "" {
				it.Message = joinNotes(profNote, metaNote)
			}
			addMigrated(result, it)
		}
	}

	// 9. Books + editions + book files (include.series -> books,
	// include.episodes -> editions/files). The one-time ID swap happens
	// here: each book is resolved to its OpenLibrary work key before the
	// title is ensured.
	titleIDsByBook := map[int]int64{} // source book id -> imported title id (series links resolve through it)
	if inc.wantSeries() {
		books, err := src.GetBooks(ctx)
		if err != nil {
			return result, err
		}
		// Books without nested editions fall back to the /edition list.
		editionsByBook := map[int][]ReadarrEdition{}
		if inc.wantEpisodes() {
			if all, err := src.GetEditions(ctx); err != nil {
				return result, err
			} else {
				for _, e := range all {
					editionsByBook[e.BookID] = append(editionsByBook[e.BookID], e)
				}
			}
		}
		filesByBook := map[int][]ReadarrBookFile{}
		if inc.wantEpisodes() {
			if all, err := src.GetBookFiles(ctx); err != nil {
				return result, err
			} else {
				for _, f := range all {
					filesByBook[f.BookID] = append(filesByBook[f.BookID], f)
				}
			}
		}

		m.mu.Lock()
		m.total = len(books)
		m.mu.Unlock()

		for _, b := range books {
			// Resolve the OpenLibrary work key (the id swap). A resolver
			// error degrades to the source id + a note — the book is still
			// imported, keyed by name.
			key, note := m.resolveBookKey(ctx, b, authorNameByID[b.AuthorID], editionsByBook)
			bookExternal := fmt.Sprintf("book:%s", key)

			m.mu.Lock()
			migrated := m.migrated[key]
			if m.State != nil {
				if ok, err := m.State.IsMigrated(ctx, req.SourceURL, key); err == nil {
					migrated = ok
				}
			}
			resumeSkip := req.Resume && migrated
			m.done++
			m.mu.Unlock()

			if resumeSkip {
				result.Items = append(result.Items, MigrationItem{Entity: "book", ExternalID: bookExternal, SourceTitle: b.Title, Action: "skipped", Message: "already migrated (resume)"})
				result.Counts.Total++
				result.Counts.Skipped++
				continue
			}

			if req.DryRun {
				it := MigrationItem{Entity: "book", ExternalID: bookExternal, SourceTitle: b.Title, Action: "dry-run"}
				if note != "" {
					it.Message = note
				}
				result.Items = append(result.Items, it)
				result.Counts.Total++
				if inc.wantEpisodes() {
					for _, e := range bookEditions(b, editionsByBook) {
						result.Items = append(result.Items, MigrationItem{Entity: "edition", ExternalID: editionExternalKey(e), SourceTitle: b.Title + " " + e.Format, Action: "dry-run"})
						result.Counts.Total++
					}
					for _, f := range filesByBook[b.ID] {
						result.Items = append(result.Items, MigrationItem{Entity: "book_file", ExternalID: fileExternalKey(f), SourceTitle: b.Title, Action: "dry-run"})
						result.Counts.Total++
					}
				}
				continue
			}

			titleID, found := authorIDs[b.AuthorID]
			if !found {
				// The author was not imported (include.series off, or a
				// failed run): the book cannot be placed.
				result.Counts.Total++
				result.Counts.Errors++
				result.Items = append(result.Items, MigrationItem{Entity: "book", ExternalID: bookExternal, SourceTitle: b.Title, Action: "error", Message: "author not in the imported set"})
				result.Resumable = true
				return result, fmt.Errorf("migration: book %q: author %d not in the imported set", b.Title, b.AuthorID)
			}

			title, _, err := m.ensureTitle(ctx, b, titleID, key, note, result)
			if err != nil {
				return result, err
			}
			titleIDsByBook[b.ID] = title

			if inc.wantEpisodes() {
				if err := m.ensureBookExtras(ctx, b, title, editionsByBook, filesByBook, result); err != nil {
					return result, err
				}
			}

			if err := m.markBookMigrated(ctx, req.SourceURL, key); err != nil {
				return result, err
			}
		}
	}

	// 10. Series + book links (include.series).
	if inc.wantSeries() {
		seriesList, err := src.GetSeries(ctx)
		if err != nil {
			return result, err
		}
		for _, s := range seriesList {
			if req.DryRun {
				result.Items = append(result.Items, MigrationItem{Entity: "series", ExternalID: fmt.Sprint(s.ID), SourceTitle: s.Title, Action: "dry-run"})
				result.Counts.Total++
				for _, l := range s.Links {
					result.Items = append(result.Items, MigrationItem{Entity: "series_link", ExternalID: fmt.Sprint(l.ID), SourceTitle: fmt.Sprintf("%s -> book %d", s.Title, l.BookID), Action: "dry-run"})
					result.Counts.Total++
				}
				continue
			}
			seriesID, action, err := m.Target.EnsureSeries(ctx, s.Title)
			if err != nil {
				result.Counts.Total++
				result.Counts.Errors++
				result.Items = append(result.Items, MigrationItem{Entity: "series", ExternalID: fmt.Sprint(s.ID), SourceTitle: s.Title, Action: "error", Message: err.Error()})
				return result, err
			}
			addMigrated(result, MigrationItem{Entity: "series", ExternalID: fmt.Sprint(s.ID), SourceTitle: s.Title, Action: string(action), TargetID: seriesID})

			for _, l := range s.Links {
				titleID, ok := titleIDsByBook[l.BookID]
				if !ok {
					result.Items = append(result.Items, MigrationItem{Entity: "series_link", ExternalID: fmt.Sprint(l.ID), SourceTitle: fmt.Sprintf("%s -> book %d", s.Title, l.BookID), Action: "skipped", Message: "book not in the imported set"})
					result.Counts.Total++
					result.Counts.Skipped++
					continue
				}
				linkAction, err := m.Target.EnsureSeriesLink(ctx, ReadarrSeriesLinkRef{SeriesID: seriesID, TitleID: titleID})
				if err != nil {
					result.Counts.Total++
					result.Counts.Errors++
					result.Items = append(result.Items, MigrationItem{Entity: "series_link", ExternalID: fmt.Sprint(l.ID), SourceTitle: fmt.Sprintf("%s -> book %d", s.Title, l.BookID), Action: "error", Message: err.Error()})
					return result, err
				}
				addMigrated(result, MigrationItem{Entity: "series_link", ExternalID: fmt.Sprint(l.ID), SourceTitle: fmt.Sprintf("%s -> book %d", s.Title, l.BookID), Action: string(linkAction)})
			}
		}
	}

	return result, nil
}

// resolveBookKey resolves a source book to its stable migration key: the
// OpenLibrary work key when resolvable (the id swap), else the source book
// id. It returns (key, note) — a resolver failure yields
// ("local:<id>", note).
func (m *ReadarrMigrator) resolveBookKey(ctx context.Context, b ReadarrBook, authorName string, editionsByBook map[int][]ReadarrEdition) (key, note string) {
	if m.Resolver == nil {
		return fmt.Sprintf("local:%d", b.ID), ""
	}
	ident := ReadarrBookIdentity{
		ForeignBookID: b.ForeignBookID,
		Title:         b.Title,
		Author:        authorName,
	}
	if eds := bookEditions(b, editionsByBook); len(eds) > 0 {
		ident.ISBN13 = eds[0].ISBN13
		ident.ASIN = eds[0].ASIN
	}
	k, _, err := m.Resolver.ResolveWorkKey(ctx, ident)
	if err != nil {
		return fmt.Sprintf("local:%d", b.ID), "openlibrary resolution failed (" + err.Error() + "); keyed by source id"
	}
	if k == "" {
		return fmt.Sprintf("local:%d", b.ID), "no openlibrary id found; keyed by source id"
	}
	return k, ""
}

// ensureTitle creates the title (with the resolved OL work key) and appends
// the book item; it returns the target title id. A failure is partial +
// resumable (the run stops, what completed is kept).
func (m *ReadarrMigrator) ensureTitle(ctx context.Context, b ReadarrBook, authorID int64, key, note string, result *MigrationResult) (int64, TargetAction, error) {
	ref := ReadarrTitleRef{
		AuthorID:  authorID,
		Name:      b.Title,
		OLID:      olKeyFromMigrationKey(key),
		Monitored: b.Monitored,
	}
	id, action, err := m.Target.EnsureTitle(ctx, ref)
	if err != nil {
		result.Counts.Total++
		result.Counts.Errors++
		it := MigrationItem{Entity: "book", ExternalID: "book:" + key, SourceTitle: b.Title, Action: "error", Message: err.Error()}
		if note != "" {
			it.Message = note + "; " + err.Error()
		}
		result.Items = append(result.Items, it)
		result.Resumable = true
		return 0, "", err
	}
	it := MigrationItem{Entity: "book", ExternalID: "book:" + key, SourceTitle: b.Title, Action: string(action), TargetID: id}
	if note != "" {
		it.Message = note
	} else if ref.OLID != "" {
		it.Message = "keyed by OpenLibrary work key " + ref.OLID
	}
	addMigrated(result, it)
	return id, action, nil
}

// ensureBookExtras imports a title's editions and book files. A file is tied
// to the edition whose format+isbn it matches ("" = no known match).
func (m *ReadarrMigrator) ensureBookExtras(ctx context.Context, b ReadarrBook, titleID int64, editionsByBook map[int][]ReadarrEdition, filesByBook map[int][]ReadarrBookFile, result *MigrationResult) error {
	editionIDs := map[int]int64{} // source edition id -> target edition id
	for _, e := range bookEditions(b, editionsByBook) {
		ref := ReadarrEditionRef{
			TitleID:   titleID,
			Format:    normalizeBookFormat(e.Format),
			Language:  e.Language,
			ISBN13:    e.ISBN13,
			Monitored: e.Monitored,
		}
		id, action, err := m.Target.EnsureEdition(ctx, ref)
		if err != nil {
			result.Counts.Total++
			result.Counts.Errors++
			result.Items = append(result.Items, MigrationItem{Entity: "edition", ExternalID: editionExternalKey(e), SourceTitle: b.Title + " " + e.Format, Action: "error", Message: err.Error()})
			result.Resumable = true
			return err
		}
		editionIDs[e.ID] = id
		addMigrated(result, MigrationItem{Entity: "edition", ExternalID: editionExternalKey(e), SourceTitle: b.Title + " " + e.Format, Action: string(action), TargetID: id})
	}

	for _, f := range filesByBook[b.ID] {
		var editionID int64
		for _, e := range bookEditions(b, editionsByBook) {
			if editionMatch(e, f) {
				editionID = editionIDs[e.ID]
				break
			}
		}
		ref := ReadarrBookFileRef{
			TitleID:   titleID,
			EditionID: editionID,
			Path:      f.Path,
			Format:    normalizeBookFormat(bookFileFormat(f)),
			Size:      f.Size,
		}
		id, action, err := m.Target.EnsureBookFile(ctx, ref)
		if err != nil {
			result.Counts.Total++
			result.Counts.Errors++
			result.Items = append(result.Items, MigrationItem{Entity: "book_file", ExternalID: fileExternalKey(f), SourceTitle: b.Title, Action: "error", Message: err.Error()})
			result.Resumable = true
			return err
		}
		addMigrated(result, MigrationItem{Entity: "book_file", ExternalID: fileExternalKey(f), SourceTitle: b.Title, Action: string(action), TargetID: id})
	}
	return nil
}

// markBookMigrated records the book key in the state seam (Postgres in the
// real target) and the in-memory twin.
func (m *ReadarrMigrator) markBookMigrated(ctx context.Context, sourceURL, key string) error {
	if m.State != nil {
		if err := m.State.MarkMigrated(ctx, sourceURL, key); err != nil {
			return fmt.Errorf("migration: record migrated book %q: %w", key, err)
		}
	}
	m.mu.Lock()
	m.migrated[key] = true
	m.mu.Unlock()
	return nil
}

// bookEditions returns a book's editions: the nested ones when present,
// else the /edition list entries for the book id.
func bookEditions(b ReadarrBook, byBook map[int][]ReadarrEdition) []ReadarrEdition {
	if len(b.Editions) > 0 {
		return b.Editions
	}
	return byBook[b.ID]
}

// editionMatch reports whether a file matches an edition by format+isbn
// (the file's own format only; Readarr files carry no isbn of their own).
func editionMatch(e ReadarrEdition, f ReadarrBookFile) bool {
	return e.ISBN13 != "" && (normalizeBookFormat(e.Format) == normalizeBookFormat(bookFileFormat(f)))
}

// bookFileFormat derives the e-book format from the file's extension.
func bookFileFormat(f ReadarrBookFile) string {
	i := strings.LastIndex(f.Path, ".")
	if i < 0 || i == len(f.Path)-1 {
		return ""
	}
	return strings.ToLower(f.Path[i+1:])
}

func editionExternalKey(e ReadarrEdition) string {
	if e.ID > 0 {
		return fmt.Sprintf("edition:%d", e.ID)
	}
	return fmt.Sprintf("edition:%s|%s", e.ISBN13, e.Format)
}

func fileExternalKey(f ReadarrBookFile) string {
	if f.ID > 0 {
		return fmt.Sprintf("bookfile:%d", f.ID)
	}
	return "bookfile:" + f.Path
}

// olKeyFromMigrationKey extracts the OpenLibrary work key from a migration
// key ("local:42" -> "").
func olKeyFromMigrationKey(key string) string {
	if strings.HasPrefix(key, "local:") {
		return ""
	}
	return key
}

// normalizeBookFormat folds a source format (Readarr spells it "EPUB",
// "AZW3", ...) into the books domain's lower-case format code.
func normalizeBookFormat(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// resolveBookProfile maps a source profile id to its name; an id not in the
// source list (or 0) resolves to "" (the target default) + a note for the
// dangling-id case.
func resolveBookProfile(namesByID map[int]string, sourceID int) (name, note string) {
	if n, ok := namesByID[sourceID]; ok {
		return n, ""
	}
	if sourceID > 0 {
		return "", fmt.Sprintf("profile id %d not in the source set; using the target default", sourceID)
	}
	return "", ""
}

func joinNotes(notes ...string) string {
	parts := make([]string, 0, len(notes))
	for _, n := range notes {
		if n != "" {
			parts = append(parts, n)
		}
	}
	return strings.Join(parts, "; ")
}

// Calibre output-profile slugs, per Readarr v0.4's CalibreProfile enum
// (default first, then the device slugs in the order Readarr's own
// frontend/src/Helpers/Props/calibreProfiles.js ships them). The v0.4 API
// returns the profile as the slug string; the index is what Mediarr's
// per-root CalibreSettings.OutputProfile stores.
var calibreProfileNames = []string{
	"default", "cybookg3", "cybook_opus", "generic_eink", "generic_eink_hd",
	"generic_eink_large", "hanlinv3", "hanlinv5", "illiad", "ipad", "ipad3",
	"irexdr1000", "irexdr800", "jetbook5", "kindle", "kindle_dx",
	"kindle_fire", "kindle_oasis", "kindle_pw", "kindle_pw3", "kindle_voyage",
	"kobo", "msreader", "mobipocket", "nook", "nook_color", "nook_hd_plus",
	"pocketbook_900", "pocketbook_pro_912", "galaxy", "sony", "sony300",
	"sony900", "sony_landscape", "sonyt3", "tablet",
}

// calibreProfileIndex maps a v0.4 output-profile slug to its enum index;
// unknown slugs (a future Readarr) report ok=false so the caller can fall
// back to the default profile with a report note.
func calibreProfileIndex(slug string) (int, bool) {
	s := strings.ToLower(strings.TrimSpace(slug))
	if s == "" {
		return 0, true
	}
	for i, name := range calibreProfileNames {
		if name == s {
			return i, true
		}
	}
	return 0, false
}

// calibreReportNote describes the Calibre mapping for a root folder report
// item WITHOUT exposing the password (the v0.4 contract returns it in
// plaintext; the report only says whether one was carried).
func calibreReportNote(rf ReadarrRootFolder) string {
	notes := []string{"calibre library"}
	if rf.Host != "" {
		notes = append(notes, fmt.Sprintf("host %s:%d", rf.Host, rf.Port))
	}
	if idx, ok := calibreProfileIndex(rf.OutputProfile); ok {
		if rf.OutputProfile != "" && !strings.EqualFold(rf.OutputProfile, "default") {
			notes = append(notes, fmt.Sprintf("output profile %q -> index %d", rf.OutputProfile, idx))
		}
	} else if rf.OutputProfile != "" {
		notes = append(notes, fmt.Sprintf("output profile %q not in the v0.4 set; using default", rf.OutputProfile))
	}
	if rf.Password != "" {
		notes = append(notes, "password present (migrated as a masked secret; never logged)")
	}
	return strings.Join(notes, ", ")
}
