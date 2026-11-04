package system

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	books "github.com/brandenk514/mediarr/internal/domains/books"
	rfdom "github.com/brandenk514/mediarr/internal/domains/rootfolders"
	"github.com/brandenk514/mediarr/internal/downloadclients"
	"github.com/brandenk514/mediarr/internal/settings"
)

// ---------------------------------------------------------------------------
// Readarr B6 (drop-in migration) — Postgres target.
//
// ReadarrPostgresTarget implements ReadarrMigrationTarget against Mediarr's
// Postgres state: the books domain (authors/titles/editions/files/series),
// the per-root Calibre block (B5), the settings documents (book quality +
// metadata profiles, tags, media management), the indexer definition store,
// and the saved download-client list. Every Ensure* is idempotent: it
// creates the entity when absent (ActionCreated) and reports ActionExists
// when it is already present, matching on the books domain's stable
// identities (author name; author+title name; (title, format, isbn);
// (title, path); series name).
//
// The Calibre password travels from the source (the v0.4 API exposes it in
// plaintext) straight into the root-folder row — the B5 convention: the
// repo persists it, the API masks it on read and keeps it on a blank write.
// It is never echoed into the migration report (the migrator's
// calibreReportNote handles that) and never logged here.
// ---------------------------------------------------------------------------

// ReadarrBooksStore is the books-domain surface the Readarr target writes
// through. main.go wires *postgres.BooksRepo, which implements it.
type ReadarrBooksStore interface {
	// Find* return (id, found, err) keyed on the domain's stable identity.
	FindAuthor(ctx context.Context, name string) (int64, bool, error)
	FindTitle(ctx context.Context, authorID int64, name string) (int64, bool, error)
	FindEdition(ctx context.Context, titleID int64, format, isbn string) (int64, bool, error)
	FindBookFile(ctx context.Context, titleID int64, path string) (int64, bool, error)
	FindSeries(ctx context.Context, name string) (int64, bool, error)
	IsSeriesMember(ctx context.Context, seriesID, titleID int64) (bool, error)
	// Create* store a new row and return its id.
	CreateAuthor(ctx context.Context, a books.Author) (int64, error)
	CreateTitle(ctx context.Context, t books.Title) (int64, error)
	CreateEdition(ctx context.Context, e books.Edition) (int64, error)
	CreateBookFile(ctx context.Context, f books.BookFile) (int64, error)
	CreateSeries(ctx context.Context, s books.Series) (int64, error)
	// LinkTitleToSeries links a title to a series (idempotent).
	LinkTitleToSeries(ctx context.Context, titleID, seriesID int64) error
}

// ReadarrRootFolderStore is the root-folder registry surface: find by path
// (the migration's identity) and create with the B5 Calibre block.
type ReadarrRootFolderStore interface {
	FindRootFolder(ctx context.Context, path string) (rfdom.RootFolder, bool, error)
	CreateRootFolder(ctx context.Context, rf rfdom.RootFolder) (int64, error)
}

// ReadarrPostgresTarget is the Postgres-backed ReadarrMigrationTarget.
// Every field is required (nil is a run error, not a silent skip), so the
// wiring gap fails loudly.
type ReadarrPostgresTarget struct {
	Settings    SettingsStore
	Books       ReadarrBooksStore
	RootFolders ReadarrRootFolderStore
	Indexers    RadarrIndexerStore
	DCRepo      downloadclients.Repo

	// tagIDsBySource caches the source-tag-id -> target-label map built as
	// tags are ensured, so TagLabels (called later for author references)
	// resolves without a second document read.
	tagIDsBySource map[int]string
}

// NewReadarrPostgresTarget builds the target over the given stores.
func NewReadarrPostgresTarget(
	settings SettingsStore,
	books ReadarrBooksStore,
	roots ReadarrRootFolderStore,
	indexers RadarrIndexerStore,
	dcRepo downloadclients.Repo,
) *ReadarrPostgresTarget {
	return &ReadarrPostgresTarget{
		Settings:       settings,
		Books:          books,
		RootFolders:    roots,
		Indexers:       indexers,
		DCRepo:         dcRepo,
		tagIDsBySource: map[int]string{},
	}
}

var _ ReadarrMigrationTarget = (*ReadarrPostgresTarget)(nil)

// ---------------------------------------------------------------------------
// Profiles + tags.
// ---------------------------------------------------------------------------

// profilesDoc loads (or defaults) the profiles settings document.
func (t *ReadarrPostgresTarget) profilesDoc(ctx context.Context) (*settings.ProfilesSettings, error) {
	raw, err := t.Settings.GetSectionDocument(ctx, settings.SectionProfiles)
	if err != nil {
		return nil, err
	}
	if raw == nil {
		d := settings.DefaultProfiles()
		return &d, nil
	}
	var doc settings.ProfilesSettings
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("decode profiles section: %w", err)
	}
	return &doc, nil
}

func (t *ReadarrPostgresTarget) putProfiles(ctx context.Context, doc *settings.ProfilesSettings) error {
	raw, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	return t.Settings.PutSectionDocument(ctx, settings.SectionProfiles, raw)
}

// EnsureBookProfile maps a Readarr book quality profile onto Mediarr's book
// quality-profile document. Source book qualities match by name: "Digital"
// approves the three e-book format levels (EPUB/AZW3/MOBI); "Print" has no
// Mediarr book-level equivalent (paper) and is not approved. The cutoff is
// the highest approved level.
func (t *ReadarrPostgresTarget) EnsureBookProfile(ctx context.Context, p ReadarrQualityProfile, defs []ReadarrQualityDefinition) (int64, TargetAction, error) {
	if t.Settings == nil {
		return 0, "", fmt.Errorf("migration: settings store not wired")
	}
	if p.Name == "" {
		return 0, "", fmt.Errorf("migration: book quality profile has no name")
	}
	doc, err := t.profilesDoc(ctx)
	if err != nil {
		return 0, "", err
	}
	for i := range doc.BookProfiles {
		if doc.BookProfiles[i].Name == p.Name {
			return int64(doc.BookProfiles[i].ID), ActionExists, nil
		}
	}
	items := mapReadarrBookProfileItems(p)
	if len(items) == 0 {
		return 0, "", fmt.Errorf("migration: book quality profile %q has no mappable qualities", p.Name)
	}
	cutoff := 0
	for _, it := range items {
		if it.Approved && it.Quality > cutoff {
			cutoff = it.Quality
		}
	}
	id := nextBookProfileID(doc)
	doc.BookProfiles = append(doc.BookProfiles, settings.QualityProfile{
		ID:     id,
		Name:   p.Name,
		Items:  items,
		Cutoff: cutoff,
	})
	if err := t.putProfiles(ctx, doc); err != nil {
		return 0, "", err
	}
	return int64(id), ActionCreated, nil
}

// nextBookProfileID is the next free profile id (max id + 1 across the book
// profile array — book ids live in their own 300+ document space).
func nextBookProfileID(doc *settings.ProfilesSettings) int {
	max := 0
	for _, p := range doc.BookProfiles {
		if p.ID > max {
			max = p.ID
		}
	}
	return max + 1
}

// mapReadarrBookProfileItems maps the source profile's approval set onto
// Mediarr's book levels: every approved "Digital" item approves all three
// e-book levels; other source qualities (e.g. "Print") have no book level
// and are dropped.
func mapReadarrBookProfileItems(p ReadarrQualityProfile) []settings.QualityProfileItem {
	approvedEBook := false
	for _, it := range p.Items {
		if !it.Allowed {
			continue
		}
		q := strings.ToLower(strings.TrimSpace(it.Quality.Name))
		if q == "" {
			q = strings.ToLower(strings.TrimSpace(it.Name))
		}
		if q == "digital" {
			approvedEBook = true
		}
	}
	levels := settings.DefaultBookLevels()
	out := make([]settings.QualityProfileItem, 0, len(levels))
	for _, l := range levels {
		approved := l.Group == "E-book" && approvedEBook
		out = append(out, settings.QualityProfileItem{Quality: l.ID, Approved: approved})
	}
	return out
}

// EnsureBookMetadataProfile mirrors a Readarr metadata profile into the
// book metadata profile array (id + name are what the migration resolves by;
// the v0.4 filter rules have no book-domain equivalent, so the row is
// created unfiltered).
func (t *ReadarrPostgresTarget) EnsureBookMetadataProfile(ctx context.Context, mp ReadarrMetadataProfile) (int64, TargetAction, error) {
	if t.Settings == nil {
		return 0, "", fmt.Errorf("migration: settings store not wired")
	}
	if mp.Name == "" {
		return 0, "", fmt.Errorf("migration: metadata profile has no name")
	}
	doc, err := t.profilesDoc(ctx)
	if err != nil {
		return 0, "", err
	}
	for i := range doc.BookMetadataProfiles {
		if doc.BookMetadataProfiles[i].Name == mp.Name {
			return int64(doc.BookMetadataProfiles[i].ID), ActionExists, nil
		}
	}
	id := nextBookMetadataProfileID(doc)
	doc.BookMetadataProfiles = append(doc.BookMetadataProfiles, settings.BookMetadataProfile{
		ID:          id,
		Name:        mp.Name,
		Description: "Imported from Readarr",
	})
	if err := t.putProfiles(ctx, doc); err != nil {
		return 0, "", err
	}
	return int64(id), ActionCreated, nil
}

func nextBookMetadataProfileID(doc *settings.ProfilesSettings) int {
	max := 0
	for _, p := range doc.BookMetadataProfiles {
		if p.ID > max {
			max = p.ID
		}
	}
	return max + 1
}

// tagsDoc loads (or defaults) the tags settings document.
func (t *ReadarrPostgresTarget) tagsDoc(ctx context.Context) (*settings.TagsSettings, error) {
	raw, err := t.Settings.GetSectionDocument(ctx, settings.SectionTags)
	if err != nil {
		return nil, err
	}
	if raw == nil {
		d := settings.DefaultTags()
		return &d, nil
	}
	var doc settings.TagsSettings
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("decode tags section: %w", err)
	}
	return &doc, nil
}

func (t *ReadarrPostgresTarget) putTags(ctx context.Context, doc *settings.TagsSettings) error {
	raw, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	return t.Settings.PutSectionDocument(ctx, settings.SectionTags, raw)
}

// EnsureTag adds the tag label to the tags document when absent (labels are
// the identity; a same-label tag reuses the existing row).
func (t *ReadarrPostgresTarget) EnsureTag(ctx context.Context, tag ReadarrTag) (int64, TargetAction, error) {
	if t.Settings == nil {
		return 0, "", fmt.Errorf("migration: settings store not wired")
	}
	if strings.TrimSpace(tag.Label) == "" {
		return 0, "", fmt.Errorf("migration: tag has no label")
	}
	doc, err := t.tagsDoc(ctx)
	if err != nil {
		return 0, "", err
	}
	for i := range doc.Tags {
		if doc.Tags[i].Label == tag.Label {
			if t.tagIDsBySource != nil {
				t.tagIDsBySource[tag.ID] = doc.Tags[i].Label
			}
			return int64(doc.Tags[i].ID), ActionExists, nil
		}
	}
	id := 1
	for _, tg := range doc.Tags {
		if tg.ID >= id {
			id = tg.ID + 1
		}
	}
	doc.Tags = append(doc.Tags, settings.TagInfo{ID: id, Label: tag.Label})
	if err := t.putTags(ctx, doc); err != nil {
		return 0, "", err
	}
	if t.tagIDsBySource != nil {
		t.tagIDsBySource[tag.ID] = tag.Label
	}
	return int64(id), ActionCreated, nil
}

// TagLabels resolves source tag ids to labels: the in-run cache first, then
// the document (so ids created by an earlier run still resolve). Unknown
// ids are dropped.
func (t *ReadarrPostgresTarget) TagLabels(ctx context.Context, ids []int) ([]string, error) {
	if len(ids) == 0 {
		return []string{}, nil
	}
	labelByID := map[int]string{}
	if t.Settings != nil {
		doc, err := t.tagsDoc(ctx)
		if err != nil {
			return nil, err
		}
		for _, tg := range doc.Tags {
			labelByID[tg.ID] = tg.Label
		}
	}
	if t.tagIDsBySource != nil {
		for id, lbl := range t.tagIDsBySource {
			if _, ok := labelByID[id]; !ok {
				labelByID[id] = lbl
			}
		}
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if lbl, ok := labelByID[id]; ok {
			out = append(out, lbl)
		}
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Indexers + download clients.
// ---------------------------------------------------------------------------

// EnsureIndexer upserts a Readarr indexer into the indexer definition store.
// The v0.4 indexer resource carries no enabled flag (a present indexer is
// enabled); the per-search booleans map onto the R5a indexer settings the
// same way as the Sonarr/Radarr/Lidarr runs.
func (t *ReadarrPostgresTarget) EnsureIndexer(ctx context.Context, ix ReadarrIndexer) (int64, TargetAction, error) {
	if t.Indexers == nil {
		return 0, "", fmt.Errorf("migration: indexer store not wired")
	}
	if id, found := t.Indexers.FindIndexer(ctx, ix.Name); found {
		return id, ActionExists, nil
	}
	kind := readarrFieldString(ix.Fields, "definition")
	if kind == "" {
		kind = "indexer"
	}
	baseURL := readarrURLField(ix.Fields)
	apiKey := readarrFieldString(ix.Fields, "apikey", "apiKey")
	id, err := t.Indexers.CreateIndexer(ctx, ix.Name, kind, baseURL, apiKey, true,
		ix.EnableRSS, ix.EnableAutomaticSearch, ix.EnableInteractiveSearch)
	if err != nil {
		return 0, "", err
	}
	return id, ActionCreated, nil
}

// SetDownloadClient maps a Readarr download client into the Mediarr
// saved-client list (Prowlarr PR5: a named row upserted by name; the same
// mapping as the Sonarr and Radarr runs). Readarr v0.4 supports qBittorrent
// and SABnzbd; anything else is unmapped (ActionExists, a no-op) — the
// shared upsert treats a nil seed that way.
func (t *ReadarrPostgresTarget) SetDownloadClient(ctx context.Context, c ReadarrDownloadClient) (TargetAction, error) {
	rec := seedReadarrDownloadClient(c)
	if rec == nil {
		return ActionExists, nil
	}
	if t.DCRepo == nil {
		return "", fmt.Errorf("migration: download client store not wired")
	}
	return upsertDownloadClient(ctx, t.DCRepo, rec)
}

// seedReadarrDownloadClient maps the v0.4 download-client fields onto a
// Mediarr record. Kind detection is by field shape: SABnzbd exposes an
// "apikey" field; qBittorrent exposes "username" (+ "password"/"apikey").
// Returns nil for a shape neither matches.
func seedReadarrDownloadClient(c ReadarrDownloadClient) *downloadclients.Record {
	names := map[string]bool{}
	for _, f := range c.Fields {
		names[strings.ToLower(f.Name)] = true
	}
	var name string
	var kind downloadclients.Kind
	var base, username, apiKey, completeDir string
	switch {
	case names["apikey"]:
		name, kind = "SABnzbd", downloadclients.KindSAB
		base = readarrURLField(c.Fields)
		apiKey = readarrFieldString(c.Fields, "apikey")
		completeDir = readarrFieldString(c.Fields, "category")
	case names["username"] || names["password"]:
		name, kind = "qBittorrent", downloadclients.KindQB
		base = readarrURLField(c.Fields)
		username = readarrFieldString(c.Fields, "username")
		apiKey = readarrFieldString(c.Fields, "apikey", "password")
	default:
		return nil
	}
	if base == "" {
		return nil
	}
	return &downloadclients.Record{
		Name:        name,
		Kind:        kind,
		Enabled:     c.Enable,
		Position:    1,
		BaseURL:     base,
		Username:    username,
		APIKey:      apiKey,
		CompleteDir: completeDir,
	}
}

// readarrFieldString returns the first named field's string value (the v0.4
// Field schema values are strings, nulls, or numbers).
func readarrFieldString(fields []ReadarrField, want ...string) string {
	for _, f := range fields {
		for _, w := range want {
			if strings.EqualFold(f.Name, w) {
				return f.String()
			}
		}
	}
	return ""
}

// readarrURLField builds a "scheme://host:port" base URL from host/port
// fields (a pre-built url/baseUrl wins when present).
func readarrURLField(fields []ReadarrField) string {
	if base := readarrFieldString(fields, "url", "baseUrl"); base != "" {
		return base
	}
	host := readarrFieldString(fields, "host", "serverhost")
	if host == "" {
		return ""
	}
	if port := readarrFieldString(fields, "port"); port != "" {
		return "http://" + host + ":" + port
	}
	return "http://" + host
}

// ---------------------------------------------------------------------------
// Root folders (+ the B5 Calibre block).
// ---------------------------------------------------------------------------

// EnsureRootFolder registers a root folder (path is the identity) with its
// resolved default profile names and, where the source is a Calibre library,
// the per-root Calibre config. defaultQuality/defaultMetadata are profile
// NAMES the migrator resolved ("\" = app default).
func (t *ReadarrPostgresTarget) EnsureRootFolder(ctx context.Context, rf ReadarrRootFolder, defaultQuality, defaultMetadata string) (int64, TargetAction, error) {
	if t.RootFolders == nil {
		return 0, "", fmt.Errorf("migration: root folder store not wired")
	}
	path := filepath.Clean(rf.Path)
	if path == "" {
		return 0, "", fmt.Errorf("migration: root folder has no path")
	}
	if existing, found, err := t.RootFolders.FindRootFolder(ctx, path); err != nil {
		return 0, "", err
	} else if found {
		return existing.ID, ActionExists, nil
	}
	name := rf.Name
	if name == "" {
		name = filepath.Base(path)
	}
	target := rfdom.RootFolder{
		Path:                   path,
		Name:                   name,
		DefaultQualityProfile:  defaultQuality,
		DefaultMetadataProfile: defaultMetadata,
		Calibre:                toCalibreSettings(rf),
	}
	id, err := t.RootFolders.CreateRootFolder(ctx, target)
	if err != nil {
		return 0, "", err
	}
	return id, ActionCreated, nil
}

// toCalibreSettings maps the v0.4 root-folder Calibre block onto the B5
// per-root Calibre model. A non-Calibre root (and a Calibre root with no
// credentials at all) maps to nil = not configured. The password is carried
// through as the source sent it — it is persisted by the repo and masked by
// the API, never logged or reported.
func toCalibreSettings(rf ReadarrRootFolder) *settings.CalibreSettings {
	if !rf.IsCalibreLibrary && rf.Host == "" && rf.Library == "" {
		return nil
	}
	outputProfile := 0
	if rf.OutputProfile != "" {
		if n, err := strconv.Atoi(rf.OutputProfile); err == nil {
			outputProfile = n
		}
	}
	return &settings.CalibreSettings{
		Enabled:       rf.IsCalibreLibrary,
		Host:          rf.Host,
		Port:          rf.Port,
		URLBase:       rf.URLBase,
		Username:      rf.Username,
		Password:      rf.Password,
		PasswordSet:   rf.Password != "",
		Library:       rf.Library,
		UseSSL:        rf.UseSSL,
		OutputFormat:  rf.OutputFormat,
		OutputProfile: outputProfile,
	}
}

// ---------------------------------------------------------------------------
// Media management.
// ---------------------------------------------------------------------------

// ApplyMediaManagement maps the Readarr media-management config onto the
// Mediarr book media-management document (the best-effort subset: the fields
// the document can hold) and returns the number of fields written.
func (t *ReadarrPostgresTarget) ApplyMediaManagement(ctx context.Context, mm ReadarrMediaManagement) (int, error) {
	if t.Settings == nil {
		return 0, fmt.Errorf("migration: settings store not wired")
	}
	raw, err := t.Settings.GetSectionDocument(ctx, settings.SectionMediaManagement)
	if err != nil {
		return 0, err
	}
	var doc settings.MediaManagementSettings
	if raw == nil {
		doc = settings.DefaultMediaManagement()
	} else if err := json.Unmarshal(raw, &doc); err != nil {
		return 0, fmt.Errorf("decode media management: %w", err)
	}
	b := &doc.BookManagement
	applied := 0

	b.AutoUnmonitorDownloaded = mm.AutoUnmonitorPreviouslyDownloadedBooks
	applied++
	if mm.DownloadPropersAndRepacks {
		b.DownloadPropersAndRepacks = "include"
	} else {
		b.DownloadPropersAndRepacks = "ignore"
	}
	applied++
	b.CreateEmptyAuthorFolders = mm.CreateEmptyAuthorFolders
	applied++
	b.DeleteEmptyFolders = mm.DeleteEmptyFolders
	applied++
	b.WatchLibraryForChanges = mm.WatchLibraryForChanges
	applied++
	if mm.RescanAfterRefresh {
		b.RescanAfterRefresh = "changed"
	} else {
		b.RescanAfterRefresh = "no"
	}
	applied++
	if mm.AllowFingerprinting {
		b.AllowFingerprinting = "yes"
	} else {
		b.AllowFingerprinting = "no"
	}
	applied++
	b.SetPermissionsLinux = mm.SetPermissionsLinux
	applied++
	if mm.SetPermissionsLinux {
		if mm.ChmodFolder != "" {
			b.ChmodFolder = mm.ChmodFolder
			applied++
		}
		if mm.ChownGroup != "" {
			b.ChownGroup = mm.ChownGroup
			applied++
		}
	}
	b.SkipFreeSpaceCheckWhenImporting = mm.SkipFreeSpaceCheckWhenImporting
	applied++
	b.MinimumFreeSpaceWhenImportingGB = mm.MinimumFreeSpaceWhenImporting
	applied++
	b.CopyUsingHardlinks = mm.CopyUsingHardlinks
	applied++
	// Readarr's recycleBin is a boolean; the Mediarr document keys the
	// toggle on the path, so only the cleanup days are mapped.
	if mm.RecycleBin && mm.RecycleBinCleanupDays > 0 {
		b.RecycleBinCleanupDays = mm.RecycleBinCleanupDays
		applied++
	}

	out, err := json.Marshal(doc)
	if err != nil {
		return 0, err
	}
	if err := t.Settings.PutSectionDocument(ctx, settings.SectionMediaManagement, out); err != nil {
		return 0, err
	}
	return applied, nil
}

// ---------------------------------------------------------------------------
// Books.
// ---------------------------------------------------------------------------

// EnsureAuthor creates (or finds) the author. The resolved profile names and
// tag labels are carried on the domain Author; MonitorPolicy/MonitorNewItems
// map onto the author's monitor fields.
func (t *ReadarrPostgresTarget) EnsureAuthor(ctx context.Context, a ReadarrAuthorRef) (int64, TargetAction, error) {
	if t.Books == nil {
		return 0, "", fmt.Errorf("migration: books store not wired")
	}
	if a.Name == "" {
		return 0, "", fmt.Errorf("migration: author has no name")
	}
	if id, found, err := t.Books.FindAuthor(ctx, a.Name); err != nil {
		return 0, "", err
	} else if found {
		return id, ActionExists, nil
	}
	author := books.Author{
		Name:            a.Name,
		Monitored:       a.Monitored || a.MonitorNewItems,
		QualityProfile:  a.QualityProfile,
		MetadataProfile: a.MetadataProfile,
		RootFolder:      a.RootFolderPath,
		Tags:            a.Tags,
	}
	id, err := t.Books.CreateAuthor(ctx, author)
	if err != nil {
		return 0, "", err
	}
	return id, ActionCreated, nil
}

// EnsureTitle creates (or finds) the book title under its author.
func (t *ReadarrPostgresTarget) EnsureTitle(ctx context.Context, tr ReadarrTitleRef) (int64, TargetAction, error) {
	if t.Books == nil {
		return 0, "", fmt.Errorf("migration: books store not wired")
	}
	if tr.Name == "" {
		return 0, "", fmt.Errorf("migration: title has no name")
	}
	if id, found, err := t.Books.FindTitle(ctx, tr.AuthorID, tr.Name); err != nil {
		return 0, "", err
	} else if found {
		return id, ActionExists, nil
	}
	id, err := t.Books.CreateTitle(ctx, books.Title{
		AuthorID:  tr.AuthorID,
		Name:      tr.Name,
		Monitored: tr.Monitored,
	})
	if err != nil {
		return 0, "", err
	}
	return id, ActionCreated, nil
}

// EnsureEdition creates (or finds) the (format, isbn) edition under a title.
func (t *ReadarrPostgresTarget) EnsureEdition(ctx context.Context, e ReadarrEditionRef) (int64, TargetAction, error) {
	if t.Books == nil {
		return 0, "", fmt.Errorf("migration: books store not wired")
	}
	if e.Format == "" {
		return 0, "", fmt.Errorf("migration: edition has no format")
	}
	if id, found, err := t.Books.FindEdition(ctx, e.TitleID, e.Format, e.ISBN13); err != nil {
		return 0, "", err
	} else if found {
		return id, ActionExists, nil
	}
	id, err := t.Books.CreateEdition(ctx, books.Edition{
		TitleID:   e.TitleID,
		Format:    e.Format,
		Language:  e.Language,
		ISBN:      e.ISBN13,
		Monitored: e.Monitored,
	})
	if err != nil {
		return 0, "", err
	}
	return id, ActionCreated, nil
}

// EnsureBookFile records (or finds) the imported file under its title.
func (t *ReadarrPostgresTarget) EnsureBookFile(ctx context.Context, f ReadarrBookFileRef) (int64, TargetAction, error) {
	if t.Books == nil {
		return 0, "", fmt.Errorf("migration: books store not wired")
	}
	if f.Path == "" {
		return 0, "", fmt.Errorf("migration: book file has no path")
	}
	if id, found, err := t.Books.FindBookFile(ctx, f.TitleID, f.Path); err != nil {
		return 0, "", err
	} else if found {
		return id, ActionExists, nil
	}
	id, err := t.Books.CreateBookFile(ctx, books.BookFile{
		TitleID:   f.TitleID,
		EditionID: f.EditionID,
		Path:      f.Path,
		Format:    f.Format,
		Size:      f.Size,
	})
	if err != nil {
		return 0, "", err
	}
	return id, ActionCreated, nil
}

// EnsureSeries creates (or finds) the named series.
func (t *ReadarrPostgresTarget) EnsureSeries(ctx context.Context, name string) (int64, TargetAction, error) {
	if t.Books == nil {
		return 0, "", fmt.Errorf("migration: books store not wired")
	}
	if strings.TrimSpace(name) == "" {
		return 0, "", fmt.Errorf("migration: series has no name")
	}
	if id, found, err := t.Books.FindSeries(ctx, name); err != nil {
		return 0, "", err
	} else if found {
		return id, ActionExists, nil
	}
	id, err := t.Books.CreateSeries(ctx, books.Series{Name: name})
	if err != nil {
		return 0, "", err
	}
	return id, ActionCreated, nil
}

// EnsureSeriesLink links a title to a series (idempotent).
func (t *ReadarrPostgresTarget) EnsureSeriesLink(ctx context.Context, l ReadarrSeriesLinkRef) (TargetAction, error) {
	if t.Books == nil {
		return "", fmt.Errorf("migration: books store not wired")
	}
	if ok, err := t.Books.IsSeriesMember(ctx, l.SeriesID, l.TitleID); err != nil {
		return "", err
	} else if ok {
		return ActionExists, nil
	}
	if err := t.Books.LinkTitleToSeries(ctx, l.TitleID, l.SeriesID); err != nil {
		return "", err
	}
	return ActionCreated, nil
}
