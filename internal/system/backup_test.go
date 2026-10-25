package system

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/brandenk514/mediarr/internal/settings"
)

// fakeBackupStore is an in-memory BackupStore for the round-trip tests.
type fakeBackupStore struct {
	dc       *settings.DownloadClient
	libs     map[settings.LibraryKind]*settings.LibraryOverride
	sections map[settings.Section][]byte
}

func newFakeBackupStore() *fakeBackupStore {
	return &fakeBackupStore{
		dc:       &settings.DownloadClient{},
		libs:     map[settings.LibraryKind]*settings.LibraryOverride{},
		sections: map[settings.Section][]byte{},
	}
}

func (f *fakeBackupStore) GetDownloadClient(ctx context.Context) (*settings.DownloadClient, error) {
	if f.dc == nil {
		return &settings.DownloadClient{}, nil
	}
	return f.dc, nil
}
func (f *fakeBackupStore) PutDownloadClient(ctx context.Context, dc *settings.DownloadClient) error {
	f.dc = dc
	return nil
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

func TestBackupExportMasksSecretsAndIncludesConfig(t *testing.T) {
	src := newFakeBackupStore()
	// A qbittorrent client with secrets that must be masked in the export.
	src.dc = &settings.DownloadClient{
		Kind:       "qbittorrent",
		QBBase:     "http://qb:8080",
		QBUsername: "admin",
		QBAPIKey:   "super-secret-key",
		QBPassword: "hunter2",
	}
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
		QueueSnap:  func(ctx context.Context) (json.RawMessage, error) { return qsnap, nil },
		SeriesSnap: func(ctx context.Context) (json.RawMessage, error) { return snap, nil },
	}

	doc, err := svc.Export(context.Background(), true, true)
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	// Secrets masked to presence.
	if doc.Settings.DownloadClient.QBAPIKeySet != true || doc.Settings.DownloadClient.QBPasswordSet != true {
		t.Errorf("expected presence booleans true, got %+v", doc.Settings.DownloadClient)
	}
	if jsonContains(doc, "super-secret-key") || jsonContains(doc, "hunter2") {
		t.Errorf("backup document leaked a secret: %v", doc)
	}
	// Non-secret fields carried.
	if doc.Settings.DownloadClient.QBBase != "http://qb:8080" || doc.Settings.DownloadClient.QBUsername != "admin" {
		t.Errorf("expected non-secret DC fields, got %+v", doc.Settings.DownloadClient)
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
	src.dc = &settings.DownloadClient{Kind: "sabnzbd", SABBase: "http://sab:1234", SABAPIKey: "sab-key"}
	src.libs[settings.LibraryMovie] = &settings.LibraryOverride{Path: "/media/movie"}
	tagDoc, _ := json.Marshal(settings.TagsSettings{Tags: []settings.TagInfo{{ID: 1, Label: "kids"}}})
	src.sections[settings.SectionTags] = tagDoc

	svc := &BackupService{Store: src, Version: "0.6.0", Now: fakeNow}
	doc, err := svc.Export(context.Background(), false, false)
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	// Import into a fresh store.
	dst := newFakeBackupStore()
	dstSvc := &BackupService{Store: dst, Version: "0.6.0", Now: fakeNow}
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

	// The config converged: DC non-secret fields, libraries, and the tags doc.
	if dst.dc.Kind != "sabnzbd" || dst.dc.SABBase != "http://sab:1234" {
		t.Errorf("DC not restored: %+v", dst.dc)
	}
	if dst.dc.SABAPIKey != "" {
		t.Errorf("DC secret should not be restored from the masked view, got %q", dst.dc.SABAPIKey)
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
}

func TestBackupImportInvalidDocument(t *testing.T) {
	dst := newFakeBackupStore()
	svc := &BackupService{Store: dst, Version: "0.6.0", Now: fakeNow}
	// A tags document with a missing label -> Validate fails.
	bad, _ := json.Marshal(settings.TagsSettings{Tags: []settings.TagInfo{{ID: 1, Label: ""}}})
	doc := &BackupDocument{
		Version: "0.6.0",
		Settings: BackupSettings{
			DownloadClient: DownloadClientView{Kind: "mock"},
			Libraries:      map[string]BackupLibraryOverride{},
			Sections:       []BackupSection{{Section: "tags", Document: string(bad)}},
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
		Settings: BackupSettings{DownloadClient: DownloadClientView{Kind: "mock"}, Libraries: map[string]BackupLibraryOverride{}, Sections: []BackupSection{}},
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
