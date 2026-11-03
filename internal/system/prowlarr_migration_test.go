package system

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/brandenk514/mediarr/internal/downloadclients"
)

// fakeProwlarrSource is the swappable ProwlarrClient for unit tests: no live
// HTTP. The fixtures exercise both the "fields" array shape (real Prowlarr)
// and the full entity set (tags, indexers, download clients, proxies).
type fakeProwlarrSource struct {
	tags     []ProwlarrTag
	indexers []ProwlarrIndexer
	clients  []ProwlarrDownloadClient
	proxies  []ProwlarrIndexerProxy
}

func (f *fakeProwlarrSource) GetTags(context.Context) ([]ProwlarrTag, error) {
	return f.tags, nil
}
func (f *fakeProwlarrSource) GetIndexers(context.Context) ([]ProwlarrIndexer, error) {
	return f.indexers, nil
}
func (f *fakeProwlarrSource) GetDownloadClients(context.Context) ([]ProwlarrDownloadClient, error) {
	return f.clients, nil
}
func (f *fakeProwlarrSource) GetIndexerProxies(context.Context) ([]ProwlarrIndexerProxy, error) {
	return f.proxies, nil
}

// testProwlarrSource is the shared fixture: two tags, four indexers (a known
// torrent definition, a known usenet definition, and two unknown names that
// must fall back to the generics), three download clients (two mappable, one
// unmappable), and one FlareSolverr proxy.
func testProwlarrSource() *fakeProwlarrSource {
	return &fakeProwlarrSource{
		tags: []ProwlarrTag{
			{ID: 1, Label: "prowlarr"},
			{ID: 2, Label: "auto"},
		},
		indexers: []ProwlarrIndexer{
			{
				ID: 10, Name: "1337x", Enable: true, IndexerID: 111,
				DefinitionName: "1337x", Priority: 25, Tags: []int{1},
				BaseURL: "https://1337x.to",
				ConfigFields: map[string]any{
					"baseurl": "https://1337x.to",
					"apikey":  "SECRETKEY1337",
				},
			},
			{
				ID: 11, Name: "NZBgeek", Enable: true, IndexerID: 222,
				DefinitionName: "nzbgeek", Priority: 5, Tags: []int{1, 2},
				BaseURL: "https://nzbgeek.info",
				ConfigFields: map[string]any{
					"baseurl": "https://nzbgeek.info",
					"apikey":  "SECRETKEYNZB",
				},
			},
			{
				ID: 12, Name: "MyPrivateTorrent", Enable: false, IndexerID: 333,
				DefinitionName: "SomePrivateSite", Tags: []int{99},
				BaseURL:      "https://my.site",
				ConfigFields: map[string]any{"apikey": "PRIVATESECRET"},
			},
			{
				ID: 13, Name: "MyUsenet", Enable: true, IndexerID: 444,
				DefinitionName: "MyUsenetCrawler",
				BaseURL:        "https://usenet.example",
				ConfigFields:   map[string]any{"apikey": "USNSECRET"},
			},
		},
		clients: []ProwlarrDownloadClient{
			{
				ID: 1, Name: "QB", Enable: true, ClientType: "qBittorrent",
				Fields: map[string]string{
					"host": "qb.local", "port": "8080",
					"username": "qbuser", "apikey": "QBSECRET",
					"completedDownloadDir": "/downloads",
				},
			},
			{
				ID: 2, Name: "SAB", Enable: false, ClientType: "SABnzbd",
				Fields: map[string]string{
					"host": "sab.local", "port": "8081",
					"apikey": "SABSECRET",
				},
			},
			{
				ID: 3, Name: "Other", Enable: true, ClientType: "DownloadFolder",
				Fields: map[string]string{},
			},
		},
		proxies: []ProwlarrIndexerProxy{
			{
				ID: 1, Name: "FlareSolverr", Enable: true,
				BaseURL: "http://localhost:8191", Tags: []int{2},
				Fields: map[string]string{},
			},
		},
	}
}

// fakeProwlarrTarget is the in-memory ProwlarrMigrationTarget: it records
// every ref it is asked to write (so tests can assert on the resolved
// payloads) and simulates existing entities on a second pass.
type fakeProwlarrTarget struct {
	tags           map[int]int64
	indexersByName map[string]ProwlarrIndexerRef
	indexerIDs     map[string]int64
	dcsByName      map[string]ProwlarrDCRef
	proxiesByName  map[string]ProwlarrProxyRef
	writes         int
	failIndexersAt int // EnsureIndexer fails once the count reaches this (0 = never)
}

func newFakeProwlarrTarget() *fakeProwlarrTarget {
	return &fakeProwlarrTarget{
		tags:           map[int]int64{},
		indexersByName: map[string]ProwlarrIndexerRef{},
		indexerIDs:     map[string]int64{},
		dcsByName:      map[string]ProwlarrDCRef{},
		proxiesByName:  map[string]ProwlarrProxyRef{},
	}
}

func (f *fakeProwlarrTarget) nextID() int64 {
	return int64(len(f.indexersByName) + len(f.dcsByName) + len(f.proxiesByName) + len(f.tags) + 1)
}

func (f *fakeProwlarrTarget) EnsureTag(ctx context.Context, t ProwlarrTag) (int64, TargetAction, error) {
	f.writes++
	if id, ok := f.tags[t.ID]; ok {
		return id, ActionExists, nil
	}
	id := f.nextID()
	f.tags[t.ID] = id
	return id, ActionCreated, nil
}

func (f *fakeProwlarrTarget) EnsureIndexer(ctx context.Context, ref ProwlarrIndexerRef) (int64, TargetAction, error) {
	f.writes++
	if id, ok := f.indexerIDs[ref.Name]; ok {
		return id, ActionExists, nil
	}
	if f.failIndexersAt > 0 && len(f.indexersByName) >= f.failIndexersAt {
		return 0, TargetAction(""), fmt.Errorf("target: simulated failure on indexer %q", ref.Name)
	}
	id := f.nextID()
	f.indexersByName[ref.Name] = ref
	f.indexerIDs[ref.Name] = id
	return id, ActionCreated, nil
}

func (f *fakeProwlarrTarget) EnsureDownloadClient(ctx context.Context, ref ProwlarrDCRef) (TargetAction, error) {
	f.writes++
	if _, ok := f.dcsByName[ref.Name]; ok {
		return ActionExists, nil
	}
	f.dcsByName[ref.Name] = ref
	return ActionCreated, nil
}

func (f *fakeProwlarrTarget) EnsureIndexerProxy(ctx context.Context, ref ProwlarrProxyRef) (int64, TargetAction, error) {
	f.writes++
	if _, ok := f.proxiesByName[ref.Name]; ok {
		return f.nextID(), ActionExists, nil
	}
	id := f.nextID()
	f.proxiesByName[ref.Name] = ref
	return id, ActionCreated, nil
}

// newTestProwlarrMigrator wires the fake source and target into a migrator.
func newTestProwlarrMigrator(src *fakeProwlarrSource, tgt *fakeProwlarrTarget) *ProwlarrMigrator {
	m := NewProwlarrMigrator(tgt)
	m.NewSource = func(baseURL, apiKey string) ProwlarrClient { return src }
	return m
}

func prowlarrTestRequest() MigrationRequest {
	return MigrationRequest{
		Source:    SourceProwlarr,
		SourceURL: "http://prowlarr.test:9696",
		APIKey:    "prowlarr-api-key",
		Include:   &MigrationInclude{},
	}
}

func itemAction(t *testing.T, res *MigrationResult, entity, title string) string {
	t.Helper()
	for _, it := range res.Items {
		if it.Entity == entity && it.SourceTitle == title {
			return it.Action
		}
	}
	t.Fatalf("no %s item for %q in %+v", entity, title, res.Items)
	return ""
}

// TestProwlarrMigrationDryRunWritesNothing: a dry-run produces the full
// mapping report but performs zero target writes.
func TestProwlarrMigrationDryRunWritesNothing(t *testing.T) {
	tgt := newFakeProwlarrTarget()
	m := newTestProwlarrMigrator(testProwlarrSource(), tgt)

	req := prowlarrTestRequest()
	req.DryRun = true
	res, err := m.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if tgt.writes != 0 {
		t.Fatalf("dry run performed %d target writes, want 0", tgt.writes)
	}
	if res.DryRun != true {
		t.Fatal("dry-run result not flagged")
	}
	for _, want := range []string{"tag", "indexer", "download_client", "indexer_proxy", "apps"} {
		found := false
		for _, it := range res.Items {
			if it.Entity == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("dry-run report missing entity %q", want)
		}
	}
	// The apps note must state explicitly that apps are not imported.
	var appsMsg string
	for _, it := range res.Items {
		if it.Entity == "apps" {
			appsMsg = it.Message
		}
	}
	if !strings.Contains(appsMsg, "not imported") {
		t.Errorf("apps item does not state that apps are not imported: %q", appsMsg)
	}
}

// TestProwlarrMigrationImportsEntities: a real run imports every entity with
// the correct resolved payloads (definition fallbacks, kind mapping, tag
// labels, download-client mapping, proxy endpoint).
func TestProwlarrMigrationImportsEntities(t *testing.T) {
	tgt := newFakeProwlarrTarget()
	m := newTestProwlarrMigrator(testProwlarrSource(), tgt)

	res, err := m.Run(context.Background(), prowlarrTestRequest())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Resumable {
		t.Fatalf("unexpected partial run: %+v", res)
	}

	// Tags: both imported.
	if len(tgt.tags) != 2 {
		t.Fatalf("want 2 tags, got %d", len(tgt.tags))
	}

	// Indexers: resolved refs carry the right kind / definition / credentials.
	if len(tgt.indexersByName) != 4 {
		t.Fatalf("want 4 indexers, got %d: %v", len(tgt.indexersByName), keys(tgt.indexersByName))
	}
	ix := tgt.indexersByName["1337x"]
	if ix.Kind != "torznab" || ix.DefinitionName != "1337x" {
		t.Errorf("1337x resolved to %s/%s, want torznab/1337x", ix.Kind, ix.DefinitionName)
	}
	if ix.APIKey != "SECRETKEY1337" {
		t.Errorf("1337x apikey = %q, want the source plaintext key (the store re-encrypts it)", ix.APIKey)
	}
	if len(ix.Tags) != 1 || ix.Tags[0] != "prowlarr" {
		t.Errorf("1337x tags = %v, want [prowlarr]", ix.Tags)
	}
	nzb := tgt.indexersByName["NZBgeek"]
	if nzb.Kind != "nzb" || nzb.DefinitionName != "nzbgeek" {
		t.Errorf("NZBgeek resolved to %s/%s, want nzb/nzbgeek", nzb.Kind, nzb.DefinitionName)
	}
	priv := tgt.indexersByName["MyPrivateTorrent"]
	if priv.DefinitionName != "generic-torznab" || priv.Kind != "torznab" {
		t.Errorf("unknown torrent definition fell back to %s/%s, want generic-torznab/torznab", priv.DefinitionName, priv.Kind)
	}
	if priv.APIKey != "PRIVATESECRET" || priv.ConfigFields["apikey"] != "PRIVATESECRET" {
		t.Errorf("fallback indexer must carry the stored config fields, got %+v", priv.ConfigFields)
	}
	usr := tgt.indexersByName["MyUsenet"]
	if usr.DefinitionName != "generic-newznab" || usr.Kind != "nzb" {
		t.Errorf("unknown usenet definition fell back to %s/%s, want generic-newznab/nzb", usr.DefinitionName, usr.Kind)
	}

	// Download clients: the two mappable ones are ensured, the third is skipped.
	if len(tgt.dcsByName) != 2 {
		t.Fatalf("want 2 download clients, got %d: %v", len(tgt.dcsByName), keysDC(tgt.dcsByName))
	}
	qb := tgt.dcsByName["qBittorrent"]
	if qb.Kind != downloadclients.KindQB || qb.BaseURL != "qb.local:8080" || qb.APIKey != "QBSECRET" || qb.CompleteDir != "/downloads" || !qb.Enabled {
		t.Errorf("qBittorrent ref = %+v", qb)
	}
	sab := tgt.dcsByName["SABnzbd"]
	if sab.Kind != downloadclients.KindSAB || sab.BaseURL != "sab.local:8081" || sab.APIKey != "SABSECRET" || sab.Enabled {
		t.Errorf("SABnzbd ref = %+v", sab)
	}
	if action := itemAction(t, res, "download_client", "Other"); action != "skipped" {
		t.Errorf("DownloadFolder client action = %q, want skipped", action)
	}

	// Proxy: the FlareSolverr endpoint with its tag label.
	proxy := tgt.proxiesByName["FlareSolverr"]
	if proxy.URL != "http://localhost:8191" || len(proxy.Tags) != 1 || proxy.Tags[0] != "auto" {
		t.Errorf("proxy ref = %+v", proxy)
	}
}

func keys(m map[string]ProwlarrIndexerRef) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func keysDC(m map[string]ProwlarrDCRef) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestProwlarrMigrationSecondRunIdempotent: a second run over the same
// source converges — every previously-created entity reports "exists".
func TestProwlarrMigrationSecondRunIdempotent(t *testing.T) {
	tgt := newFakeProwlarrTarget()
	m := newTestProwlarrMigrator(testProwlarrSource(), tgt)
	first, err := m.Run(context.Background(), prowlarrTestRequest())
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	if first.Counts.Created == 0 {
		t.Fatalf("first run created nothing: %+v", first.Counts)
	}

	second, err := m.Run(context.Background(), prowlarrTestRequest())
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	for _, it := range second.Items {
		if it.Action != "exists" && it.Action != "skipped" {
			t.Errorf("second run item %s/%s action = %q, want exists or skipped", it.Entity, it.SourceTitle, it.Action)
		}
	}
	if second.Counts.Created != 0 {
		t.Errorf("second run created %d entities, want 0: %+v", second.Counts.Created, second.Counts)
	}
}

// TestProwlarrMigrationPartialAndResume: a target failure mid-run marks the
// result resumable; a resumed run skips what the first run wrote.
func TestProwlarrMigrationPartialAndResume(t *testing.T) {
	tgt := newFakeProwlarrTarget()
	tgt.failIndexersAt = 1 // the first indexer imports, the second fails
	m := newTestProwlarrMigrator(testProwlarrSource(), tgt)

	req := prowlarrTestRequest()
	partial, err := m.Run(context.Background(), req)
	if err == nil {
		t.Fatal("expected a partial-run error")
	}
	if partial == nil || !partial.Resumable {
		t.Fatalf("partial result = %+v, want Resumable", partial)
	}
	if len(tgt.indexersByName) != 1 {
		t.Fatalf("want 1 indexer imported before failure, got %d", len(tgt.indexersByName))
	}

	// Clear the simulated failure: it models a transient target error, and
	// the resume run must see a healthy store.
	tgt.failIndexersAt = 0
	req.Resume = true
	resumed, err := m.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("resume run: %v", err)
	}
	if resumed.Resumable {
		t.Fatalf("resume run still resumable: %+v", resumed)
	}
	skipped := 0
	for _, it := range resumed.Items {
		if it.Entity == "indexer" && it.Action == "skipped" {
			skipped++
		}
	}
	if skipped != 1 {
		t.Errorf("resume run skipped %d indexers, want 1", skipped)
	}
	// The resume still imports the remaining indexers.
	if len(tgt.indexersByName) != 4 {
		t.Errorf("after resume want 4 indexers, got %d", len(tgt.indexersByName))
	}
}

// TestProwlarrCredentialValuesNotInReport: the plaintext credential values
// flow into the target payload (for store-side re-encryption) but never into
// the mapping report items.
func TestProwlarrCredentialValuesNotInReport(t *testing.T) {
	tgt := newFakeProwlarrTarget()
	m := newTestProwlarrMigrator(testProwlarrSource(), tgt)
	res, err := m.Run(context.Background(), prowlarrTestRequest())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	secrets := []string{"SECRETKEY1337", "SECRETKEYNZB", "PRIVATESECRET", "USNSECRET", "QBSECRET", "SABSECRET"}
	for _, it := range res.Items {
		joined := it.Entity + " " + it.ExternalID + " " + it.SourceTitle + " " + it.Action + " " + it.Message
		for _, s := range secrets {
			if strings.Contains(joined, s) {
				t.Errorf("report item %q leaks credential value %q", joined, s)
			}
		}
	}
	// ...while the target payload carries the plaintext for the store.
	if tgt.indexersByName["1337x"].APIKey != "SECRETKEY1337" {
		t.Error("target ref must carry the plaintext key for store-side re-encryption")
	}
}

// TestProwlarrClientParsesFieldsArray: the real client normalizes the
// Prowlarr "fields" array (and "configFields" object) shapes.
func TestProwlarrClientParsesFieldsArray(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/indexer":
			w.Write([]byte(`[{
				"id": 10, "name": "1337x", "enable": true, "indexerId": 111,
				"definitionName": "1337x", "priority": 25, "tags": [1],
				"baseUrl": "https://1337x.to",
				"fields": [
					{"name": "baseurl", "value": "https://1337x.to"},
					{"name": "apikey", "value": "FIELDKEY"}
				]
			}]`))
		case "/api/v1/tag":
			w.Write([]byte(`[{"id": 1, "label": "prowlarr"}]`))
		case "/api/v1/downloadclient":
			w.Write([]byte(`[{
				"id": 1, "name": "QB", "enable": true, "clientType": "qBittorrent",
				"fields": [
					{"name": "host", "value": "qb.local"},
					{"name": "port", "value": "8080"},
					{"name": "apikey", "value": "QBSECRET"}
				]
			}]`))
		case "/api/v1/indexerproxy":
			w.Write([]byte(`[{
				"id": 1, "name": "FlareSolverr", "enable": true,
				"baseUrl": "http://localhost:8191",
				"fields": [{"name": "username", "value": "fs"}, {"name": "password", "value": "fspot"}]
			}]`))
		default:
			http.Error(w, "nope", http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := NewProwlarrClient(srv.URL, "test-key")
	ctx := context.Background()
	ix, err := c.GetIndexers(ctx)
	if err != nil {
		t.Fatalf("GetIndexers: %v", err)
	}
	if len(ix) != 1 || ix[0].ConfigFields["apikey"] != "FIELDKEY" || ix[0].ConfigFields["baseurl"] != "https://1337x.to" {
		t.Fatalf("indexer fields not normalized: %+v", ix)
	}
	dcs, err := c.GetDownloadClients(ctx)
	if err != nil {
		t.Fatalf("GetDownloadClients: %v", err)
	}
	if len(dcs) != 1 || dcs[0].Fields["apikey"] != "QBSECRET" || dcs[0].Fields["port"] != "8080" {
		t.Fatalf("downloadclient fields not normalized: %+v", dcs)
	}
	proxies, err := c.GetIndexerProxies(ctx)
	if err != nil {
		t.Fatalf("GetIndexerProxies: %v", err)
	}
	if len(proxies) != 1 || proxies[0].BaseURL != "http://localhost:8191" || proxies[0].Fields["password"] != "fspot" {
		t.Fatalf("indexerproxy fields not normalized: %+v", proxies)
	}
	tags, err := c.GetTags(ctx)
	if err != nil {
		t.Fatalf("GetTags: %v", err)
	}
	if len(tags) != 1 || tags[0].Label != "prowlarr" {
		t.Fatalf("tags not parsed: %+v", tags)
	}
}

// TestProwlarrClientErrorDoesNotLeakBody: a non-200 response (whose body may
// echo credential data) surfaces as a status-code-only error.
func TestProwlarrClientErrorDoesNotLeakBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "k" {
			t.Errorf("X-Api-Key header not sent")
		}
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("Unauthorized: leaked-secret-token"))
	}))
	defer srv.Close()

	c := NewProwlarrClient(srv.URL, "k")
	_, err := c.GetIndexers(context.Background())
	if err == nil {
		t.Fatal("expected an error for a 401")
	}
	if strings.Contains(err.Error(), "leaked-secret-token") {
		t.Errorf("client error leaks response body: %v", err)
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("client error should carry the status code: %v", err)
	}
}

// TestProwlarrMigrationUnknownClientSkippedWithReason: an unmappable client
// type is reported as skipped with a reason (no target write).
func TestProwlarrMigrationUnknownClientSkippedWithReason(t *testing.T) {
	src := testProwlarrSource()
	src.clients = src.clients[:1:1]
	src.clients = append(src.clients, ProwlarrDownloadClient{
		ID: 9, Name: "NZBGet", Enable: true, ClientType: "nzbget", Fields: map[string]string{},
	})
	tgt := newFakeProwlarrTarget()
	m := newTestProwlarrMigrator(src, tgt)
	res, err := m.Run(context.Background(), prowlarrTestRequest())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(tgt.dcsByName) != 1 {
		t.Fatalf("want 1 download client, got %d", len(tgt.dcsByName))
	}
	var found bool
	for _, it := range res.Items {
		if it.Entity == "download_client" && it.SourceTitle == "NZBGet" {
			found = true
			if it.Action != "skipped" || !strings.Contains(it.Message, "not imported") {
				t.Errorf("nzbget item = %+v, want skipped with a reason", it)
			}
		}
	}
	if !found {
		t.Error("no report item for the unmappable nzbget client")
	}
}

// TestProwlarrIndexerResolutionFallback: the definition-name fallback and
// tag-resolution notes (pure function, table driven).
func TestProwlarrIndexerResolutionFallback(t *testing.T) {
	tagsByID := map[int]string{1: "prowlarr", 2: "auto"}

	t.Run("unknown torrent definition falls back to generic torznab", func(t *testing.T) {
		ref, notes := resolveProwlarrIndexer(ProwlarrIndexer{
			ID: 1, Name: "X", Enable: true, DefinitionName: "Bogus",
			BaseURL: "https://x.test", ConfigFields: map[string]any{"apikey": "k"},
		}, tagsByID)
		if ref.DefinitionName != "generic-torznab" || ref.Kind != "torznab" {
			t.Fatalf("fallback = %s/%s", ref.DefinitionName, ref.Kind)
		}
		if !containsNote(notes, "not in the Mediarr definition library") {
			t.Errorf("notes should say the definition is not in the library: %v", notes)
		}
	})

	t.Run("unknown usenet-named definition falls back to generic newznab", func(t *testing.T) {
		ref, _ := resolveProwlarrIndexer(ProwlarrIndexer{
			ID: 1, Name: "Y", Enable: true, DefinitionName: "MyNewznabFeed",
			BaseURL: "https://y.test", ConfigFields: map[string]any{},
		}, tagsByID)
		if ref.DefinitionName != "generic-newznab" || ref.Kind != "nzb" {
			t.Fatalf("fallback = %s/%s", ref.DefinitionName, ref.Kind)
		}
	})

	t.Run("known definition keeps its library id", func(t *testing.T) {
		ref, notes := resolveProwlarrIndexer(ProwlarrIndexer{
			ID: 1, Name: "Z", Enable: true, DefinitionName: "nzbgeek",
			BaseURL: "https://z.test", ConfigFields: map[string]any{"apikey": "k"},
		}, tagsByID)
		if ref.DefinitionName != "nzbgeek" || ref.Kind != "nzb" {
			t.Fatalf("resolved = %s/%s", ref.DefinitionName, ref.Kind)
		}
		if containsNote(notes, "not in the Mediarr definition library") {
			t.Errorf("known definition must not carry a fallback note: %v", notes)
		}
	})

	t.Run("unknown tag ids are dropped with a note", func(t *testing.T) {
		ref, notes := resolveProwlarrIndexer(ProwlarrIndexer{
			ID: 1, Name: "W", Enable: true, DefinitionName: "generic-torznab",
			Tags: []int{1, 99}, ConfigFields: map[string]any{},
		}, tagsByID)
		if len(ref.Tags) != 1 || ref.Tags[0] != "prowlarr" {
			t.Fatalf("tags = %v, want [prowlarr]", ref.Tags)
		}
		if !containsNote(notes, "tag id 99") {
			t.Errorf("notes should mention the dropped tag: %v", notes)
		}
	})

	t.Run("defaults for empty priority/privacy/sync", func(t *testing.T) {
		ref, _ := resolveProwlarrIndexer(ProwlarrIndexer{
			ID: 1, Name: "D", Enable: true, DefinitionName: "generic-torznab",
			ConfigFields: map[string]any{},
		}, tagsByID)
		if ref.Priority != 25 || ref.Privacy != "public" || ref.SyncLevel != "standard" {
			t.Fatalf("defaults = %d/%s/%s", ref.Priority, ref.Privacy, ref.SyncLevel)
		}
	})
}

func containsNote(notes []string, sub string) bool {
	for _, n := range notes {
		if strings.Contains(n, sub) {
			return true
		}
	}
	return false
}
