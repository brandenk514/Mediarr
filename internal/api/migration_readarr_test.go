package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	rfdom "github.com/brandenk514/mediarr/internal/domains/rootfolders"
	"github.com/brandenk514/mediarr/internal/indexers"
	"github.com/brandenk514/mediarr/internal/postgres"
	"github.com/brandenk514/mediarr/internal/secrets"
	"github.com/brandenk514/mediarr/internal/system"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

// ---- Readarr B6: API-level tests for the dedicated Readarr endpoints ----
//
// Dry-run / validation / generic-route tests use an in-memory target (no
// Postgres): the fake v0.4 source server asserts the X-Api-Key header and
// the write-counting target proves nothing is written. Execute (idempotency)
// and resumability tests run against a real Postgres (the same
// testcontainers harness as the postgres/api integration tests) with the
// production wiring: ReadarrPostgresTarget + Postgres state seam + the
// store adapters from main.go. A stub resolver stands in for the OpenLibrary
// resolver so the tests stay deterministic and offline.
//
// The Calibre password in the fixture must stay masked in every response:
// each test asserts the plaintext never appears in a body. (The per-root
// Calibre block is persisted by the repo per the B5 convention — the API
// masks it on read — so no plaintext is asserted against the DB either.)

// writeCountingReadarrTarget implements ReadarrMigrationTarget without
// writing; its writes counter proves the dry-run is read-only.
type writeCountingReadarrTarget struct{ writes int }

func (writeCountingReadarrTarget) EnsureBookProfile(context.Context, system.ReadarrQualityProfile, []system.ReadarrQualityDefinition) (int64, system.TargetAction, error) {
	return 1, system.ActionCreated, nil
}
func (writeCountingReadarrTarget) EnsureBookMetadataProfile(context.Context, system.ReadarrMetadataProfile) (int64, system.TargetAction, error) {
	return 1, system.ActionCreated, nil
}
func (writeCountingReadarrTarget) EnsureTag(context.Context, system.ReadarrTag) (int64, system.TargetAction, error) {
	return 1, system.ActionCreated, nil
}
func (writeCountingReadarrTarget) TagLabels(context.Context, []int) ([]string, error) {
	return []string{}, nil
}
func (writeCountingReadarrTarget) EnsureIndexer(context.Context, system.ReadarrIndexer) (int64, system.TargetAction, error) {
	return 1, system.ActionCreated, nil
}
func (writeCountingReadarrTarget) SetDownloadClient(context.Context, system.ReadarrDownloadClient) (system.TargetAction, error) {
	return system.ActionCreated, nil
}
func (writeCountingReadarrTarget) EnsureRootFolder(context.Context, system.ReadarrRootFolder, string, string) (int64, system.TargetAction, error) {
	return 1, system.ActionCreated, nil
}
func (writeCountingReadarrTarget) ApplyMediaManagement(context.Context, system.ReadarrMediaManagement) (int, error) {
	return 1, nil
}
func (writeCountingReadarrTarget) EnsureAuthor(context.Context, system.ReadarrAuthorRef) (int64, system.TargetAction, error) {
	return 1, system.ActionCreated, nil
}
func (writeCountingReadarrTarget) EnsureTitle(context.Context, system.ReadarrTitleRef) (int64, system.TargetAction, error) {
	return 1, system.ActionCreated, nil
}
func (writeCountingReadarrTarget) EnsureEdition(context.Context, system.ReadarrEditionRef) (int64, system.TargetAction, error) {
	return 1, system.ActionCreated, nil
}
func (writeCountingReadarrTarget) EnsureBookFile(context.Context, system.ReadarrBookFileRef) (int64, system.TargetAction, error) {
	return 1, system.ActionCreated, nil
}
func (writeCountingReadarrTarget) EnsureSeries(context.Context, string) (int64, system.TargetAction, error) {
	return 1, system.ActionCreated, nil
}
func (writeCountingReadarrTarget) EnsureSeriesLink(context.Context, system.ReadarrSeriesLinkRef) (system.TargetAction, error) {
	return system.ActionCreated, nil
}

// stubReadarrResolver is a deterministic stand-in for the OpenLibrary
// resolver: a book's foreignBookId is its OL work key verbatim, otherwise
// the ISBN, otherwise a slug of the title. No network.
type stubReadarrResolver struct{}

func (stubReadarrResolver) ResolveWorkKey(_ context.Context, id system.ReadarrBookIdentity) (string, string, error) {
	switch {
	case id.ForeignBookID != "":
		return id.ForeignBookID, "source", nil
	case id.ISBN13 != "":
		return "OLISBN" + id.ISBN13, "isbn", nil
	default:
		return "OL" + strings.ReplaceAll(strings.ToLower(id.Title), " ", "_"), "search", nil
	}
}

// newFakeReadarrSourceServer serves a minimal Readarr v0.4.18.2805 instance
// (2 books — one with a foreignBookId, one ISBN-only; a Calibre root folder
// with a real password; one indexer; one qBittorrent-shaped client; one
// series with a dangling link) and asserts the X-Api-Key header — the
// client contract from the v0.4.18.2805 spec.
func newFakeReadarrSourceServer(t *testing.T) *httptest.Server {
	t.Helper()
	edition := func(id, bookID int, isbn, format string) map[string]any {
		return map[string]any{"id": id, "bookId": bookID, "foreignEditionId": "", "isbn13": isbn, "asin": "", "title": "", "language": "en", "format": format, "isEbook": true, "monitored": true, "manualAdd": false}
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "readarr-key-123" {
			t.Errorf("readarr client did not send X-Api-Key (got %q)", r.Header.Get("X-Api-Key"))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/api/v1/author"):
			_ = json.NewEncoder(w).Encode([]any{map[string]any{"id": 7, "authorMetadataId": 0, "status": "monitored", "authorName": "Ursula K. Le Guin", "foreignAuthorId": "OL19033A", "titleSlug": "", "path": "", "qualityProfileId": 1, "metadataProfileId": 2, "monitored": true, "monitorNewItems": false, "rootFolderPath": "", "folder": false, "genres": []string{"SF"}, "tags": []int{4}}})
		case strings.HasSuffix(r.URL.Path, "/api/v1/book"):
			_ = json.NewEncoder(w).Encode([]any{
				map[string]any{"id": 21, "title": "The Dispossessed", "authorId": 7, "foreignBookId": "OL8189755W", "foreignEditionId": "", "monitored": true, "editions": []any{edition(91, 21, "9780060792921", "EPUB")}},
				map[string]any{"id": 22, "title": "A Wizard of Earthsea", "authorId": 7, "foreignBookId": "", "foreignEditionId": "", "monitored": true, "editions": []any{edition(93, 22, "9780547747115", "AZW3")}},
			})
		case strings.HasSuffix(r.URL.Path, "/api/v1/edition"):
			_ = json.NewEncoder(w).Encode([]any{edition(91, 21, "9780060792921", "EPUB"), edition(93, 22, "9780547747115", "AZW3")})
		case strings.HasSuffix(r.URL.Path, "/api/v1/bookfile"):
			_ = json.NewEncoder(w).Encode([]any{map[string]any{"id": 55, "authorId": 7, "bookId": 21, "path": "The.Dispossessed.epub", "size": 123456, "dateAdded": "2026-01-01T00:00:00Z", "quality": map[string]any{"quality": map[string]any{"id": 1, "name": "EPUB"}}}})
		case strings.HasSuffix(r.URL.Path, "/api/v1/series"):
			_ = json.NewEncoder(w).Encode([]any{map[string]any{"id": 1, "title": "Earthsea Cycle", "links": []any{
				map[string]any{"id": 10, "position": 1, "seriesPosition": 1, "seriesId": 1, "bookId": 22},
				map[string]any{"id": 11, "position": 2, "seriesPosition": 2, "seriesId": 1, "bookId": 999}, // dangling: skipped
			}}})
		case strings.HasSuffix(r.URL.Path, "/api/v1/qualityprofile"):
			_ = json.NewEncoder(w).Encode([]any{map[string]any{"id": 1, "name": "Digital Unlimited", "upgradeAllowed": true, "cutoff": 1, "cutoffQualityId": 1, "items": []any{
				map[string]any{"id": 1, "name": "Digital", "quality": map[string]any{"id": 1, "name": "Digital"}, "items": []int{}, "allowed": true},
				map[string]any{"id": 2, "name": "Print", "quality": map[string]any{"id": 2, "name": "Print"}, "items": []int{}, "allowed": true},
			}}})
		case strings.HasSuffix(r.URL.Path, "/api/v1/qualitydefinition"):
			_ = json.NewEncoder(w).Encode([]any{
				map[string]any{"id": 1, "quality": map[string]any{"id": 1, "name": "Digital"}, "title": "Digital", "weight": 1},
				map[string]any{"id": 2, "quality": map[string]any{"id": 2, "name": "Print"}, "title": "Print", "weight": 0},
			})
		case strings.HasSuffix(r.URL.Path, "/api/v1/metadataprofile"):
			_ = json.NewEncoder(w).Encode([]any{map[string]any{"id": 2, "name": "Books"}})
		case strings.HasSuffix(r.URL.Path, "/api/v1/rootfolder"):
			_ = json.NewEncoder(w).Encode([]any{map[string]any{"id": 3, "name": "Books", "path": "/books", "defaultMetadataProfileId": 2, "defaultQualityProfileId": 1, "defaultMonitorOption": 1, "defaultNewItemMonitorOption": 1, "defaultTags": []int{}, "isCalibreLibrary": true, "host": "192.168.0.10", "port": 8082, "urlBase": "calibre-web", "username": "calibre", "password": "s3cret", "library": "Books", "outputFormat": "EPUB", "outputProfile": "default", "useSsl": false}})
		case strings.HasSuffix(r.URL.Path, "/api/v1/tag"):
			_ = json.NewEncoder(w).Encode([]any{map[string]any{"id": 4, "label": "Favorites"}})
		case strings.HasSuffix(r.URL.Path, "/api/v1/indexer"):
			_ = json.NewEncoder(w).Encode([]any{map[string]any{"id": 5, "name": "SomeNZB", "fields": []any{
				map[string]any{"name": "definition", "value": "somenzb"},
				map[string]any{"name": "host", "value": "somenzb.example"},
				map[string]any{"name": "port", "value": "443"},
				map[string]any{"name": "apikey", "value": "nz-key-456"},
			}, "enableRss": true, "enableAutomaticSearch": true, "enableInteractiveSearch": true, "supportsRss": true, "supportsSearch": true, "protocol": "nzb", "priority": 1, "downloadClientId": 6, "tags": []int{4}}})
		case strings.HasSuffix(r.URL.Path, "/api/v1/downloadclient"):
			_ = json.NewEncoder(w).Encode([]any{map[string]any{"id": 6, "name": "BT-box", "fields": []any{
				map[string]any{"name": "host", "value": "bt.local"},
				map[string]any{"name": "port", "value": "15698"},
				map[string]any{"name": "username", "value": "btuser"},
				map[string]any{"name": "password", "value": "btpass"},
			}, "protocol": "qBittorrent", "priority": 1, "enable": true, "removeCompletedDownloads": false, "removeFailedDownloads": false}})
		case strings.HasSuffix(r.URL.Path, "/api/v1/config/mediamanagement"):
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 1, "autoUnmonitorPreviouslyDownloadedBooks": true, "downloadPropersAndRepacks": false, "createEmptyAuthorFolders": true, "deleteEmptyFolders": false, "watchLibraryForChanges": false, "rescanAfterRefresh": false, "allowFingerprinting": false, "setPermissionsLinux": false, "chmodFolder": "", "chownGroup": "", "skipFreeSpaceCheckWhenImporting": false, "minimumFreeSpaceWhenImporting": 0, "copyUsingHardlinks": false, "recycleBin": false, "recycleBinCleanupDays": 0})
		default:
			t.Errorf("unexpected readarr source path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

// newReadarrTestPostgres boots a Postgres container and applies the full
// migration chain (the postgres package's test harness pattern).
func newReadarrTestPostgres(t *testing.T) *sql.DB {
	t.Helper()
	ctx := context.Background()
	c, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("mediarr"),
		tcpostgres.WithUsername("mediarr"),
		tcpostgres.WithPassword("mediarr-test"),
	)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}
	t.Cleanup(func() { _ = c.Terminate(context.Background()) })
	dsn, err := c.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("postgres dsn: %v", err)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.PingContext(ctx); err != nil {
		// The container can accept TCP before it accepts queries; retry briefly.
		for i := 0; i < 10; i++ {
			time.Sleep(500 * time.Millisecond)
			if err := db.PingContext(ctx); err == nil {
				break
			}
		}
		if err := db.PingContext(ctx); err != nil {
			t.Fatalf("ping postgres: %v", err)
		}
	}
	if err := (postgres.NewWithDB(db)).Migrate(ctx); err != nil {
		t.Fatalf("migrate postgres: %v", err)
	}
	return db
}

// newRealReadarrTarget builds the production target from the test DB using
// the same store adapters as cmd/mediarr/main.go.
func newRealReadarrTarget(t *testing.T, db *sql.DB) *system.ReadarrPostgresTarget {
	t.Helper()
	vault, err := secrets.NewVaultFromBytes(make([]byte, 32))
	if err != nil {
		t.Fatalf("vault: %v", err)
	}
	return system.NewReadarrPostgresTarget(
		postgres.NewSettingsRepo(db, vault),
		postgres.NewBooksRepo(db),
		&testReadarrRootStore{repo: postgres.NewRootFoldersRepo(db)},
		&testReadarrIndexerStore{repo: postgres.NewIndexersRepo(db, vault)},
		postgres.NewDownloadClientsRepo(db, vault),
	)
}

// testReadarrRootStore adapts RootFoldersRepo to the migrator's root-folder
// store (same shape as main.go's migrationReadarrRootFolderStore).
type testReadarrRootStore struct {
	repo *postgres.RootFoldersRepo
}

func (s *testReadarrRootStore) FindRootFolder(ctx context.Context, path string) (rfdom.RootFolder, bool, error) {
	rf, err := s.repo.FindByPath(ctx, path)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return rfdom.RootFolder{}, false, nil
		}
		return rfdom.RootFolder{}, false, err
	}
	return *rf, true, nil
}

func (s *testReadarrRootStore) CreateRootFolder(ctx context.Context, rf rfdom.RootFolder) (int64, error) {
	return s.repo.Create(rf)
}

// testReadarrIndexerStore adapts IndexersRepo to the migrator's indexer
// store (same shape as main.go's migrationReadarrIndexers).
type testReadarrIndexerStore struct {
	repo *postgres.IndexersRepo
}

func (s *testReadarrIndexerStore) FindIndexer(ctx context.Context, name string) (int64, bool) {
	def, err := s.repo.GetByName(ctx, name)
	if err != nil {
		return 0, false
	}
	return def.ID, true
}

func (s *testReadarrIndexerStore) CreateIndexer(ctx context.Context, name, kind, baseURL, apiKey string, enabled, enableRss, enableAutomatic, enableInteractive bool) (int64, error) {
	def := indexers.Definition{
		Name: name, Kind: kind, BaseURL: baseURL, APIKey: apiKey, Enabled: enabled,
		SettingsJSON: indexers.WithIndexerToggles("", indexers.IndexerSettings{
			RSS: enableRss, AutoSearch: enableAutomatic, InteractiveSearch: enableInteractive,
		}),
	}
	return s.repo.Create(ctx, def)
}

// interruptibleReadarrTarget wraps the real target and makes the Nth
// EnsureTitle call fail — simulating a source crash mid-import for the
// resumability test.
type interruptibleReadarrTarget struct {
	*system.ReadarrPostgresTarget
	failAfter  int
	titleCalls int
}

func (t *interruptibleReadarrTarget) EnsureTitle(ctx context.Context, ref system.ReadarrTitleRef) (int64, system.TargetAction, error) {
	t.titleCalls++
	if t.titleCalls == t.failAfter {
		return 0, "", errors.New("simulated interruption (source crash)")
	}
	return t.ReadarrPostgresTarget.EnsureTitle(ctx, ref)
}

// newReadarrAPIServer wires a fresh server with a migration service that has
// ONLY the readarr runner (one server per test, per the mux
// double-registration quirk).
func newReadarrAPIServer(t *testing.T, migrator *system.ReadarrMigrator) (*Server, string) {
	t.Helper()
	s, token := newSystemTestServer(t)
	s.SetSystem(&SystemDeps{Migration: system.NewMigrationService(nil, nil, nil, nil, migrator)})
	return s, token
}

// doReadarrReq runs a request through the server with a JSON body.
func doReadarrReq(t *testing.T, h http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func readarrBody(t *testing.T, sourceURL string, extra map[string]any) string {
	t.Helper()
	payload := map[string]any{"base_url": sourceURL, "api_key": "readarr-key-123"}
	for k, v := range extra {
		payload[k] = v
	}
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	return string(b)
}

// countTable returns the row count of a table.
func countTable(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// TestReadarrMigrationDryRunEndpoint: POST /system/migration/readarr is
// read-only — the mapping report is produced, the write-counting target
// records zero writes, and the Calibre password never appears.
func TestReadarrMigrationDryRunEndpoint(t *testing.T) {
	target := &writeCountingReadarrTarget{}
	migrator := system.NewReadarrMigrator(target, stubReadarrResolver{})
	src := newFakeReadarrSourceServer(t)
	t.Cleanup(src.Close)
	s, token := newReadarrAPIServer(t, migrator)

	rec := doReadarrReq(t, s.Handler(), http.MethodPost, "/api/v1/system/migration/readarr", token, readarrBody(t, src.URL, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("dry-run status = %d, body %s", rec.Code, rec.Body.String())
	}
	if target.writes != 0 {
		t.Errorf("dry-run performed %d writes; must be 0", target.writes)
	}
	if strings.Contains(rec.Body.String(), "s3cret") {
		t.Errorf("dry-run report leaks the Calibre password")
	}
	var res system.MigrationResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if !res.DryRun {
		t.Errorf("dry_run = false, want true")
	}
	if res.Counts.Created != 0 || res.Counts.Existing != 0 || res.Counts.Skipped != 0 || res.Counts.Errors != 0 {
		t.Errorf("dry-run counts = %+v, want all zero", res.Counts)
	}
	// 16 entities: 1 quality profile + 1 metadata profile + 1 tag + 1 indexer
	// + 1 download client + 1 root folder + 1 settings + 1 author + 2 books +
	// 2 editions + 1 book file + 1 series + 2 series links.
	if res.Counts.Total != 16 {
		t.Errorf("dry-run total = %d, want 16 (%+v)", res.Counts.Total, res.Counts)
	}
	// The book ids come from the stub resolver (OL work keys).
	var dispossessedFound bool
	for _, it := range res.Items {
		if it.Entity == "book" && it.ExternalID == "book:OL8189755W" && it.Action == "dry-run" {
			dispossessedFound = true
		}
	}
	if !dispossessedFound {
		t.Errorf("expected a dry-run book item for book:OL8189755W; items: %+v", res.Items)
	}
	// The Calibre block surfaces as a root_folder item with a masked note.
	var calibreNote string
	for _, it := range res.Items {
		if it.Entity == "root_folder" {
			calibreNote = it.Message
		}
	}
	if !strings.Contains(calibreNote, "calibre library") {
		t.Errorf("root_folder item message = %q, want the calibre library note", calibreNote)
	}
	if strings.Contains(calibreNote, "s3cret") {
		t.Errorf("calibre note %q leaks the password", calibreNote)
	}
}

// TestReadarrMigrationDryRunRepeatable: the dry-run is safe to repeat — a
// second run returns a report with zero writes.
func TestReadarrMigrationDryRunRepeatable(t *testing.T) {
	target := &writeCountingReadarrTarget{}
	migrator := system.NewReadarrMigrator(target, stubReadarrResolver{})
	src := newFakeReadarrSourceServer(t)
	t.Cleanup(src.Close)
	s, token := newReadarrAPIServer(t, migrator)
	body := readarrBody(t, src.URL, nil)

	for i := 1; i <= 2; i++ {
		rec := doReadarrReq(t, s.Handler(), http.MethodPost, "/api/v1/system/migration/readarr", token, body)
		if rec.Code != http.StatusOK {
			t.Fatalf("run %d status = %d, body %s", i, rec.Code, rec.Body.String())
		}
	}
	if target.writes != 0 {
		t.Errorf("repeat dry-runs performed %d writes; must be 0", target.writes)
	}
}

// TestReadarrMigrationDryRunValidation: 400 for a missing base_url or a bad
// body, 401 without auth, and an unreachable source degrades to the
// cross-source 200-partial contract (an empty report; the run status then
// reports failed).
func TestReadarrMigrationDryRunValidation(t *testing.T) {
	migrator := system.NewReadarrMigrator(&writeCountingReadarrTarget{}, stubReadarrResolver{})
	s, token := newReadarrAPIServer(t, migrator)

	rec := doReadarrReq(t, s.Handler(), http.MethodPost, "/api/v1/system/migration/readarr", token, `{"api_key":"k"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("missing base_url: status = %d, want 400 (%s)", rec.Code, rec.Body.String())
	}
	rec = doReadarrReq(t, s.Handler(), http.MethodPost, "/api/v1/system/migration/readarr", token, `{not json`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("malformed body: status = %d, want 400", rec.Code)
	}
	rec = doReadarrReq(t, s.Handler(), http.MethodPost, "/api/v1/system/migration/readarr", token, `{"base_url":"http://127.0.0.1:9","api_key":"k"}`)
	if rec.Code != http.StatusOK {
		t.Errorf("unreachable source: status = %d, want 200 with the partial (empty) report", rec.Code)
	}
	var empty system.MigrationResult
	if err := json.Unmarshal(rec.Body.Bytes(), &empty); err != nil {
		t.Fatalf("decode empty report: %v", err)
	}
	if empty.Counts.Total != 0 || len(empty.Items) != 0 {
		t.Errorf("unreachable source report = %+v, want empty", empty)
	}
	st := migrator.Status()
	if st.Status != "failed" {
		t.Errorf("unreachable source status = %q, want failed", st.Status)
	}
	rec = doReadarrReq(t, s.Handler(), http.MethodPost, "/api/v1/system/migration/readarr", "", readarrBody(t, "http://127.0.0.1:9", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated: status = %d, want 401", rec.Code)
	}
}

// TestReadarrMigrationGenericRouteReadarr: the generic endpoint keeps its
// semantics — source "readarr" is routed through the same migrator.
func TestReadarrMigrationGenericRouteReadarr(t *testing.T) {
	target := &writeCountingReadarrTarget{}
	migrator := system.NewReadarrMigrator(target, stubReadarrResolver{})
	src := newFakeReadarrSourceServer(t)
	t.Cleanup(src.Close)
	s, token := newReadarrAPIServer(t, migrator)

	generic := `{"source":"readarr","source_url":"` + src.URL + `","api_key":"readarr-key-123","dry_run":true}`
	rec := doReadarrReq(t, s.Handler(), http.MethodPost, "/api/v1/system/migration", token, generic)
	if rec.Code != http.StatusOK {
		t.Fatalf("generic readarr run status = %d, body %s", rec.Code, rec.Body.String())
	}
	if target.writes != 0 {
		t.Errorf("generic dry-run performed %d writes; must be 0", target.writes)
	}
	var res system.MigrationResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !res.DryRun || res.Counts.Total == 0 {
		t.Errorf("generic readarr run = %+v, want a dry-run report with items", res)
	}
	// The generic status endpoint reflects the most recently run source
	// (readarr here).
	rec = doReq(t, s.Handler(), http.MethodGet, "/api/v1/system/migration/status", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("generic status = %d, body %s", rec.Code, rec.Body.String())
	}
	var st system.MigrationStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if st.Status != "completed" {
		t.Errorf("generic status after the readarr dry-run = %q, want completed", st.Status)
	}
}

// TestReadarrMigrationExecuteIdempotent: two execute runs over the same
// source. The first imports; the second reports the same entities as
// existing — no duplicates in Postgres, the state table stays at one row
// per book, and the Calibre password stays masked in both responses.
func TestReadarrMigrationExecuteIdempotent(t *testing.T) {
	db := newReadarrTestPostgres(t)
	src := newFakeReadarrSourceServer(t)
	t.Cleanup(src.Close)
	migrator := system.NewReadarrMigrator(newRealReadarrTarget(t, db), stubReadarrResolver{})
	migrator.State = postgres.NewReadarrMigrationStateRepo(db)
	s, token := newReadarrAPIServer(t, migrator)
	body := readarrBody(t, src.URL, nil)

	rec := doReadarrReq(t, s.Handler(), http.MethodPost, "/api/v1/system/migration/readarr/execute", token, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("execute #1 status = %d, body %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "s3cret") {
		t.Errorf("execute #1 response leaks the Calibre password")
	}
	var first system.MigrationResult
	if err := json.Unmarshal(rec.Body.Bytes(), &first); err != nil {
		t.Fatalf("decode #1: %v", err)
	}
	if first.DryRun || first.Resumable || first.Counts.Errors != 0 {
		t.Errorf("execute #1 = %+v, want a clean import", first)
	}
	if first.Counts.Created == 0 {
		t.Errorf("execute #1 counts = %+v, want created>0", first.Counts)
	}

	// The import landed.
	for table, want := range map[string]int{
		"book_authors":            1,
		"book_titles":             2,
		"book_editions":           2,
		"book_files":              1,
		"book_series":             1,
		"book_series_members":     1, // the dangling series link (book 999) is skipped
		"indexers":                1,
		"download_clients":        1, // the qBittorrent-shaped client
		"root_folders":            1,
		"readarr_migration_state": 2,
	} {
		if got := countTable(t, db, table); got != want {
			t.Errorf("after execute #1: %s = %d, want %d", table, got, want)
		}
	}

	// The Calibre block persisted (B5 hand-off): the host is stored; the
	// password travels through the repo per the B5 convention and the
	// migration responses above already proved it is masked there.
	var calibre string
	if err := db.QueryRow(`SELECT calibre::text FROM root_folders WHERE path = '/books'`).Scan(&calibre); err != nil {
		t.Fatalf("root folder calibre: %v", err)
	}
	if !strings.Contains(calibre, "192.168.0.10") {
		t.Errorf("persisted calibre block %q missing the host", calibre)
	}

	// Run #2: everything already exists.
	rec = doReadarrReq(t, s.Handler(), http.MethodPost, "/api/v1/system/migration/readarr/execute", token, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("execute #2 status = %d, body %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "s3cret") {
		t.Errorf("execute #2 response leaks the Calibre password")
	}
	var second system.MigrationResult
	if err := json.Unmarshal(rec.Body.Bytes(), &second); err != nil {
		t.Fatalf("decode #2: %v", err)
	}
	if second.Counts.Created != 0 || second.Counts.Errors != 0 {
		t.Errorf("execute #2 counts = %+v, want created=0 errors=0 (idempotent)", second.Counts)
	}
	if second.Counts.Existing < 2 {
		t.Errorf("execute #2 counts = %+v, want the books reported as existing", second.Counts)
	}
	for table, want := range map[string]int{
		"book_titles":             2,
		"book_editions":           2,
		"book_files":              1,
		"book_series_members":     1,
		"indexers":                1,
		"download_clients":        1,
		"root_folders":            1,
		"readarr_migration_state": 2,
	} {
		if got := countTable(t, db, table); got != want {
			t.Errorf("after execute #2: %s = %d, want %d (no duplicates)", table, got, want)
		}
	}
}

// TestReadarrMigrationExecuteResumable: a run interrupted mid-import returns
// a 200 partial report (the resumable contract); a resume run skips the
// finished books and completes the rest without duplicating anything.
func TestReadarrMigrationExecuteResumable(t *testing.T) {
	db := newReadarrTestPostgres(t)
	src := newFakeReadarrSourceServer(t)
	t.Cleanup(src.Close)
	inner := newRealReadarrTarget(t, db)
	target := &interruptibleReadarrTarget{ReadarrPostgresTarget: inner, failAfter: 2}
	migrator := system.NewReadarrMigrator(target, stubReadarrResolver{})
	migrator.State = postgres.NewReadarrMigrationStateRepo(db)
	s, token := newReadarrAPIServer(t, migrator)
	body := readarrBody(t, src.URL, nil)

	// Run #1: the second book's EnsureTitle fails mid-import.
	rec := doReadarrReq(t, s.Handler(), http.MethodPost, "/api/v1/system/migration/readarr/execute", token, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("partial run status = %d, body %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "s3cret") {
		t.Errorf("partial run response leaks the Calibre password")
	}
	var partial system.MigrationResult
	if err := json.Unmarshal(rec.Body.Bytes(), &partial); err != nil {
		t.Fatalf("decode partial: %v", err)
	}
	if !partial.Resumable {
		t.Errorf("partial run: resumable = false, want true (%+v)", partial)
	}
	if partial.Counts.Errors == 0 {
		t.Errorf("partial run: errors = 0, want >0 (%+v)", partial.Counts)
	}
	if got := countTable(t, db, "book_titles"); got != 1 {
		t.Fatalf("partial run: book_titles = %d, want 1", got)
	}
	if got := countTable(t, db, "book_editions"); got != 1 {
		t.Fatalf("partial run: book_editions = %d, want 1", got)
	}
	if got := countTable(t, db, "readarr_migration_state"); got != 1 {
		t.Fatalf("partial run: state rows = %d, want 1 (only the finished book)", got)
	}

	// Run #2: resume skips the finished book and imports the rest.
	resume := readarrBody(t, src.URL, map[string]any{"resume": true})
	rec = doReadarrReq(t, s.Handler(), http.MethodPost, "/api/v1/system/migration/readarr/execute", token, resume)
	if rec.Code != http.StatusOK {
		t.Fatalf("resume run status = %d, body %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "s3cret") {
		t.Errorf("resume run response leaks the Calibre password")
	}
	var done system.MigrationResult
	if err := json.Unmarshal(rec.Body.Bytes(), &done); err != nil {
		t.Fatalf("decode resume: %v", err)
	}
	if done.Resumable || done.Counts.Errors != 0 {
		t.Errorf("resume run: %+v, want a completed, clean import", done)
	}
	var skipped int
	for _, it := range done.Items {
		if it.Entity == "book" && it.Action == "skipped" {
			skipped++
		}
	}
	if skipped != 1 {
		t.Errorf("resume run: skipped books = %d, want 1 (%+v)", skipped, done.Items)
	}
	for table, want := range map[string]int{
		"book_titles":             2, // no duplicate of the finished book
		"book_editions":           2,
		"book_files":              1,
		"book_series":             1,
		"book_series_members":     1,
		"readarr_migration_state": 2,
	} {
		if got := countTable(t, db, table); got != want {
			t.Errorf("resume run: %s = %d, want %d", table, got, want)
		}
	}
}

// TestReadarrMigrationStatusEndpoint: the source-scoped status is idle on a
// fresh stack, completed with the last report after an execute, and never
// leaks the Calibre password.
func TestReadarrMigrationStatusEndpoint(t *testing.T) {
	db := newReadarrTestPostgres(t)
	src := newFakeReadarrSourceServer(t)
	t.Cleanup(src.Close)
	migrator := system.NewReadarrMigrator(newRealReadarrTarget(t, db), stubReadarrResolver{})
	migrator.State = postgres.NewReadarrMigrationStateRepo(db)
	s, token := newReadarrAPIServer(t, migrator)

	rec := doReq(t, s.Handler(), http.MethodGet, "/api/v1/system/migration/readarr/status", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("idle status = %d, body %s", rec.Code, rec.Body.String())
	}
	var idle system.MigrationStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &idle); err != nil {
		t.Fatalf("decode idle: %v", err)
	}
	if idle.Status != "idle" {
		t.Errorf("idle status = %q", idle.Status)
	}

	rec = doReadarrReq(t, s.Handler(), http.MethodPost, "/api/v1/system/migration/readarr/execute", token, readarrBody(t, src.URL, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("execute status = %d, body %s", rec.Code, rec.Body.String())
	}
	rec = doReq(t, s.Handler(), http.MethodGet, "/api/v1/system/migration/readarr/status", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("post-run status = %d, body %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "s3cret") {
		t.Errorf("status response leaks the Calibre password")
	}
	var done system.MigrationStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &done); err != nil {
		t.Fatalf("decode post-run: %v", err)
	}
	if done.Status != "completed" || done.Running {
		t.Errorf("post-run status = %+v, want completed and not running", done)
	}
	if len(done.LastResult) == 0 {
		t.Fatalf("post-run last_result is empty, want the import report")
	}
	var last system.MigrationResult
	if err := json.Unmarshal(done.LastResult, &last); err != nil {
		t.Fatalf("decode last_result: %v", err)
	}
	if last.Counts.Created == 0 {
		t.Errorf("last_result counts = %+v, want created>0", last.Counts)
	}
}

// TestReadarrMigrationUnwired: when the system deps are wired but the
// migration service is not (the other ARR integrations may ship without the
// Readarr drop-in), the dedicated endpoints report 501, not a panic.
// (A server that never gets system deps at all 404s — the system routes are
// registered by SetSystem.)
func TestReadarrMigrationUnwired(t *testing.T) {
	s, token := newSystemTestServer(t)
	s.SetSystem(&SystemDeps{})
	for _, tc := range []struct {
		method, path, body string
	}{
		{http.MethodPost, "/api/v1/system/migration/readarr", `{"base_url":"http://x"}`},
		{http.MethodPost, "/api/v1/system/migration/readarr/execute", `{"base_url":"http://x"}`},
		{http.MethodGet, "/api/v1/system/migration/readarr/status", ""},
	} {
		rec := doReadarrReq(t, s.Handler(), tc.method, tc.path, token, tc.body)
		if rec.Code != http.StatusNotImplemented {
			t.Errorf("%s %s (unwired): status = %d, want 501", tc.method, tc.path, rec.Code)
		}
	}
}
