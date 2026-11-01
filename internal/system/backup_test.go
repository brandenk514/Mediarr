package system

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/brandenk514/mediarr/internal/downloadclients"
	"github.com/brandenk514/mediarr/internal/settings"
)

// fakeBackupStore is an in-memory BackupStore for the round-trip tests.
type fakeBackupStore struct {
	libs     map[settings.LibraryKind]*settings.LibraryOverride
	sections map[settings.Section][]byte
}

func newFakeBackupStore() *fakeBackupStore {
	return &fakeBackupStore{
		libs:     map[settings.LibraryKind]*settings.LibraryOverride{},
		sections: map[settings.Section][]byte{},
	}
}

func (f *fakeBackupStore) GetLibrary(ctx context.Context, k settings.LibraryKind) (*settings.LibraryOverride, error) {
	if o, ok := f.libs[k]; ok {
		return o, nil
	}
	return &settings.LibraryOverride{}, nil
}
func (f *fakeBackupStore) PutLibrary(ctx context.Context, k settings.LibraryKind, o *settings.LibraryOverride) error {
	f.libs[k] = o
	return nil
}
func (f *fakeBackupStore) GetSectionDocument(ctx context.Context, s settings.Section) ([]byte, error) {
	return f.sections[s], nil
}
func (f *fakeBackupStore) PutSectionDocument(ctx context.Context, s settings.Section, doc []byte) error {
	f.sections[s] = doc
	return nil
}

var fakeNow = func() time.Time { return time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC) }

// fakeDCRepo is an in-memory downloadclients.Repo for the backup tests.
type fakeDCRepo struct {
	nextID int64
	recs   []downloadclients.Record
}

func (f *fakeDCRepo) Create(ctx context.Context, r downloadclients.Record) (int64, time.Time, error) {
	f.nextID++
	r.ID = f.nextID
	now := time.Now().UTC()
	r.CreatedAt, r.UpdatedAt = now, now
	f.recs = append(f.recs, r)
	return r.ID, now, nil
}
func (f *fakeDCRepo) Get(ctx context.Context, id int64) (*downloadclients.Record, error) {
	for i := range f.recs {
		if f.recs[i].ID == id {
			r := f.recs[i]
			return &r, nil
		}
	}
	return nil, sql.ErrNoRows
}
func (f *fakeDCRepo) List(ctx context.Context) ([]downloadclients.Record, error) {
	return f.recs, nil
}
func (f *fakeDCRepo) GetByName(ctx context.Context, name string) (*downloadclients.Record, error) {
	for i := range f.recs {
		if f.recs[i].Name == name {
			r := f.recs[i]
			return &r, nil
		}
	}
	return nil, sql.ErrNoRows
}
func (f *fakeDCRepo) Update(ctx context.Context, r downloadclients.Record) error {
	for i := range f.recs {
		if f.recs[i].ID == r.ID {
			f.recs[i] = r
			return nil
		}
	}
	return sql.ErrNoRows
}
func (f *fakeDCRepo) Delete(ctx context.Context, id int64) error {
	for i := range f.recs {
		if f.recs[i].ID == id {
			f.recs = append(f.recs[:i], f.recs[i+1:]...)
			return nil
		}
	}
	return sql.ErrNoRows
}

func TestBackupExportMasksSecretsAndIncludesConfig(t *testing.T) {
	src := newFakeBackupStore()
	// A qbittorrent client with a secret that must be masked in the export.
	dc := &fakeDCRepo{}
	_, _, _ = dc.Create(context.Background(), downloadclients.Record{
		Name:     "qBittorrent",
		Kind:     downloadclients.KindQB,
		Enabled:  true,
		Position: 1,
		BaseURL:  "http://qb:8080",
		Username: "admin",
		APIKey:   "qbtest...0",
	})
	on := true
	src.libs[settings.LibraryTV] = &settings.LibraryOverride{Path: "/media/tv", QualityProfile: "Default", ChecksumVerify: &on}

	// One saved section; the rest export as defaults.
	tagDoc, _ := json.Marshal(settings.TagsSettings{Tags: []settings.TagInfo{{ID: 1, Label: "4k"}}})
	src.sections[settings.SectionTags] = tagDoc

	// Optional snapshots.
	qsnap := json.RawMessage(`[{"id":1,"kind":"tv"}]`)
	snap := json.RawMessage(`[{"id":1,"title":"The Show"}]`)
	svc := &BackupService{
		Store:      src,
		Version:    "0.6.0",
		Now:        fakeNow,
		DCRepo:     dc,
		QueueSnap:  func(ctx context.Context) (json.RawMessage, error) { return qsnap, nil },
		SeriesSnap: func(ctx context.Context) (json.RawMessage, error) { return snap, nil },
	}

	doc, err := svc.Export(context.Background(), true, true)
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	// Secrets masked to presence.
	if len(doc.Settings.DownloadClients) != 1 {
		t.Fatalf("expected 1 download client, got %d", len(doc.Settings.DownloadClients))
	}
	bc := doc.Settings.DownloadClients[0]
	if !bc.APIKeySet {
		t.Errorf("expected api_key_set true, got %+v", bc)
	}
	if jsonContains(doc, "super-secret-key") {
		t.Errorf("backup document leaked a secret: %v", doc)
	}
	// Non-secret fields carried.
	if bc.Name != "qBittorrent" || bc.Kind != "qbittorrent" || bc.BaseURL != "http://qb:8080" || bc.Username != "admin" {
		t.Errorf("expected non-secret DC fields, got %+v", bc)
	}
	// Four libraries present (tv set, others empty).
	if len(doc.Settings.Libraries) != 4 {
		t.Errorf("expected 4 libraries, got %d", len(doc.Settings.Libraries))
	}
	if doc.Settings.Libraries["tv"].Path != "/media/tv" || doc.Settings.Libraries["tv"].ChecksumVerify == nil {
		t.Errorf("tv library not exported correctly: %+v", doc.Settings.Libraries["tv"])
	}
	// All 11 sections present, tags carries the saved doc.
	if len(doc.Settings.Sections) != len(settings.AllSections) {
		t.Errorf("expected %d sections, got %d", len(settings.AllSections), len(doc.Settings.Sections))
	}
	foundTags := false
	for _, s := range doc.Settings.Sections {
		if s.Section == "tags" && s.Document == string(tagDoc) {
			foundTags = true
		}
	}
	if !foundTags {
		t.Errorf("tags section not exported with its saved document")
	}
	// Snapshots attached.
	if len(doc.Queue) == 0 || len(doc.Series) == 0 {
		t.Errorf("expected queue and series snapshots, got %v / %v", doc.Queue, doc.Series)
	}
	if doc.Version != "0.6.0" || !doc.ExportedAt.Equal(fakeNow()) {
		t.Errorf("version/exported_at wrong: %q %v", doc.Version, doc.ExportedAt)
	}
}

func TestBackupImportRoundTripAndIdempotency(t *testing.T) {
	src := newFakeBackupStore()
	srcDC := &fakeDCRepo{}
	_, _, _ = srcDC.Create(context.Background(), downloadclients.Record{
		Name:    "SABnzbd",
		Kind:    downloadclients.KindSAB,
		Enabled: true,
		BaseURL: "http://sab:1234",
		APIKey:  "sabtes...ey",
	})
	src.libs[settings.LibraryMovie] = &settings.LibraryOverride{Path: "/media/movie"}
	tagDoc, _ := json.Marshal(settings.TagsSettings{Tags: []settings.TagInfo{{ID: 1, Label: "kids"}}})
	src.sections[settings.SectionTags] = tagDoc

	svc := &BackupService{Store: src, Version: "0.6.0", Now: fakeNow, DCRepo: srcDC}
	doc, err := svc.Export(context.Background(), false, false)
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	// Import into a fresh store.
	dst := newFakeBackupStore()
	dstDC := &fakeDCRepo{}
	dstSvc := &BackupService{Store: dst, Version: "0.6.0", Now: fakeNow, DCRepo: dstDC}
	res, err := dstSvc.Import(context.Background(), doc, false, false)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if !res.Imported {
		t.Fatalf("expected imported=true")
	}
	if res.SettingsImported != len(settings.AllSections) {
		t.Errorf("expected %d settings imported, got %d", len(settings.AllSections), res.SettingsImported)
	}

	// The config converged: the client's non-secret fields, libraries, and
	// the tags doc. Keys are never carried: the restored client has none.
	if len(dstDC.recs) != 1 {
		t.Fatalf("expected 1 restored client, got %d", len(dstDC.recs))
	}
	if dstDC.recs[0].Kind != downloadclients.KindSAB || dstDC.recs[0].BaseURL != "http://sab:1234" {
		t.Errorf("client not restored: %+v", dstDC.recs[0])
	}
	if dstDC.recs[0].APIKey != "" {
		t.Errorf("client secret should not be restored from the masked view, got %q", dstDC.recs[0].APIKey)
	}
	if dst.libs[settings.LibraryMovie].Path != "/media/movie" {
		t.Errorf("movie library not restored: %+v", dst.libs[settings.LibraryMovie])
	}
	if string(dst.sections[settings.SectionTags]) != string(tagDoc) {
		t.Errorf("tags section not restored")
	}

	// Idempotency: re-import the same document; state is unchanged and the
	// section count is stable (no duplicates).
	res2, err := dstSvc.Import(context.Background(), doc, false, false)
	if err != nil {
		t.Fatalf("re-import: %v", err)
	}
	if res2.SettingsImported != len(settings.AllSections) {
		t.Errorf("re-import section count changed: %d", res2.SettingsImported)
	}
	if string(dst.sections[settings.SectionTags]) != string(tagDoc) {
		t.Errorf("re-import changed the tags document")
	}
	if len(dst.sections) != len(settings.AllSections) {
		t.Errorf("re-import changed the section count to %d", len(dst.sections))
	}
	if len(dstDC.recs) != 1 {
		t.Errorf("re-import changed the client count to %d", len(dstDC.recs))
	}
}

func TestBackupImportInvalidDocument(t *testing.T) {
	dst := newFakeBackupStore()
	svc := &BackupService{Store: dst, Version: "0.6.0", Now: fakeNow}
	// A tags document with a missing label -> Validate fails.
	bad, _ := json.Marshal(settings.TagsSettings{Tags: []settings.TagInfo{{ID: 1, Label: ""}}})
	doc := &BackupDocument{
		Version: "0.6.0",
		Settings: BackupSettings{
			DownloadClients: []BackupDownloadClient{},
			Libraries:       map[string]BackupLibraryOverride{},
			Sections:        []BackupSection{{Section: "tags", Document: string(bad)}},
		},
	}
	_, err := svc.Import(context.Background(), doc, false, false)
	if !errors.Is(err, ErrInvalidBackup) {
		t.Fatalf("expected ErrInvalidBackup, got %v", err)
	}
}

func TestBackupImportQueueAndSeries(t *testing.T) {
	dst := newFakeBackupStore()
	var gotQueue, gotSeries json.RawMessage
	svc := &BackupService{
		Store:        dst,
		Version:      "0.6.0",
		Now:          fakeNow,
		QueueImport:  func(ctx context.Context, raw json.RawMessage) (int, error) { gotQueue = raw; return 2, nil },
		SeriesImport: func(ctx context.Context, raw json.RawMessage) (int, error) { gotSeries = raw; return 3, nil },
	}
	doc := &BackupDocument{
		Version:  "0.6.0",
		Settings: BackupSettings{DownloadClients: []BackupDownloadClient{}, Libraries: map[string]BackupLibraryOverride{}, Sections: []BackupSection{}},
		Queue:    json.RawMessage(`[{"id":1},{"id":2}]`),
		Series:   json.RawMessage(`[{"id":1},{"id":2},{"id":3}]`),
	}
	res, err := svc.Import(context.Background(), doc, true, true)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.QueueImported != 2 || res.SeriesImported != 3 {
		t.Errorf("expected 2 queue / 3 series imported, got %d / %d", res.QueueImported, res.SeriesImported)
	}
	if len(gotQueue) == 0 || len(gotSeries) == 0 {
		t.Errorf("snapshots not passed to import sinks")
	}
}

// jsonContains reports whether the document's JSON encoding contains sub.
func jsonContains(s *BackupDocument, sub string) bool {
	b, _ := json.Marshal(s)
	return strings.Contains(string(b), sub)
}
