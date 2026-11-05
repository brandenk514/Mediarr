package system

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeReadarrServer serves the v0.4 contract shapes for the paths the client
// pulls, and asserts the X-Api-Key header on every request.
type fakeReadarrServer struct {
	*testing.T
	apiKey string
	hits   map[string]int
}

func newFakeReadarrServer(t *testing.T, apiKey string) (*fakeReadarrServer, *httptest.Server) {
	f := &fakeReadarrServer{T: t, apiKey: apiKey, hits: map[string]int{}}
	mux := http.NewServeMux()
	fake := func(path string, payload string) {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			f.hits[path]++
			if got := r.Header.Get("X-Api-Key"); got != f.apiKey {
				f.Errorf("%s: X-Api-Key = %q, want %q", path, got, f.apiKey)
			}
			w.Header().Set("Content-Type", "application/json")
			if _, err := w.Write([]byte(payload)); err != nil {
				f.Errorf("write %s: %v", path, err)
			}
		})
	}
	fake("/api/v1/author", `[
	  {"id":7,"authorMetadataId":1,"status":"monitored","authorName":"Ursula K. Le Guin",
	   "foreignAuthorId":"OL28986A","titleSlug":"ursula-k-le-guin","path":"/books/Le Guin",
	   "qualityProfileId":3,"metadataProfileId":2,"monitored":true,"monitorNewItems":true,
	   "rootFolderPath":"/media/books","folder":true,"genres":["Fantasy"],"tags":[4,5],"added":"2024-01-02T03:04:05Z"}]`)
	fake("/api/v1/book", `[
	  {"id":21,"title":"The Dispossessed","authorId":7,"foreignBookId":"OL8189755W",
	   "foreignEditionId":"OL27992345M","monitored":true,
	   "editions":[{"id":91,"bookId":21,"foreignEditionId":"OL27992345M","isbn13":"9780060792921",
	     "asin":"B000000001","title":"The Dispossessed: An Ambiguous Utopia","language":"en",
	     "format":"EPUB","isEbook":true,"monitored":true,"manualAdd":false}]}]`)
	fake("/api/v1/edition", `[
	  {"id":91,"bookId":21,"foreignEditionId":"OL27992345M","isbn13":"9780060792921",
	   "asin":"B000000001","title":"The Dispossessed: An Ambiguous Utopia","language":"en",
	   "format":"EPUB","isEbook":true,"monitored":true,"manualAdd":false}]`)
	fake("/api/v1/bookfile", `[
	  {"id":301,"authorId":7,"bookId":21,"path":"/media/books/The Dispossessed/Dispossessed.epub",
	   "size":1048576,"dateAdded":"2024-02-02T00:00:00Z",
	   "quality":{"quality":{"id":1,"name":"Digital"}}}]`)
	fake("/api/v1/series", `[
	  {"id":55,"title":"Earthsea Cycle","links":[
	    {"id":1,"position":1,"seriesPosition":1,"seriesId":55,"bookId":21},
	    {"id":2,"position":2,"seriesPosition":2,"seriesId":55,"bookId":22}]}]`)
	fake("/api/v1/qualityprofile", `[
	  {"id":3,"name":"Digital Unlimited","upgradeAllowed":true,"cutoff":7,"cutoffQualityId":1,
	   "items":[{"id":1,"name":"Digital","quality":{"id":1,"name":"Digital"},
	     "items":[1,2,3,4,5],"allowed":true}]}]`)
	fake("/api/v1/qualitydefinition", `[
	  {"id":1,"quality":{"id":1,"name":"Digital"},"title":"Any","weight":1000,"minSize":0,"maxSize":null}]`)
	fake("/api/v1/metadataprofile", `[
	  {"id":2,"name":"Books"},{"id":3,"name":"MinPop 50"}]`)
	fake("/api/v1/rootfolder", `[
	  {"id":1,"name":"media/books","path":"/media/books",
	   "defaultMetadataProfileId":2,"defaultQualityProfileId":3,
	   "defaultMonitorOption":1,"defaultNewItemMonitorOption":1,"defaultTags":[4],
	   "isCalibreLibrary":true,"host":"192.168.0.10","port":8080,"urlBase":"",
	   "username":"admin","password":"s3cret","library":"Main",
	   "outputFormat":"EPUB","outputProfile":"default","useSsl":false}]`)
	fake("/api/v1/tag", `[{"id":4,"label":"Favorites"},{"id":5,"label":"Backlist"}]`)
	fake("/api/v1/indexer", `[
	  {"id":9,"name":"SomeNZB","fields":[
	    {"order":1,"name":"baseUrl","value":"https://indexer.example"},
	    {"order":2,"name":"apiKey","value":"idx-key"}],
	   "enableRss":true,"enableAutomaticSearch":true,"enableInteractiveSearch":true,
	   "supportsRss":true,"supportsSearch":true,"protocol":"http",
	   "priority":1,"downloadClientId":9,"tags":[4]}]`)
	fake("/api/v1/downloadclient", `[
	  {"id":9,"name":"SickChill","fields":[
	    {"order":1,"name":"host","value":"sickchill.example"},
	    {"order":2,"name":"port","value":"8081"}],
	   "protocol":"http","priority":1,"enable":true,
	   "removeCompletedDownloads":false,"removeFailedDownloads":true}]`)
	fake("/api/v1/config/mediamanagement", `{
	  "id":1,"autoUnmonitorPreviouslyDownloadedBooks":true,"deleteEmptyFolders":true,
	  "copyUsingHardlinks":false,"recycleBin":true,"recycleBinCleanupDays":14,
	  "watchLibraryForChanges":false,"rescanAfterRefresh":true,"allowFingerprinting":false,
	  "setPermissionsLinux":false,"chmodFolder":"","chownGroup":"",
	  "skipFreeSpaceCheckWhenImporting":false,"minimumFreeSpaceWhenImporting":2048,
	  "createEmptyAuthorFolders":false,"downloadPropersAndRepacks":false}`)
	return f, httptest.NewServer(mux)
}

func TestReadarrBaseNormalization(t *testing.T) {
	for base, want := range map[string]string{
		"http://readarr:8888":              "http://readarr:8888/api/v1",
		"http://readarr:8888/":             "http://readarr:8888/api/v1",
		"http://readarr:8888/api":          "http://readarr:8888/api/v1",
		"http://readarr:8888/api/":         "http://readarr:8888/api/v1",
		"http://readarr:8888/api/v1":       "http://readarr:8888/api/v1",
		"https://host.example/sub/api/v1/": "https://host.example/sub/api/v1",
	} {
		if got := readarrBase(base); got != want {
			t.Errorf("readarrBase(%q) = %q, want %q", base, got, want)
		}
	}
}

func TestReadarrClientPullsEveryResource(t *testing.T) {
	f, srv := newFakeReadarrServer(t, "readarr-key-123")
	defer srv.Close()
	c := NewReadarrClient(srv.URL, "readarr-key-123")
	ctx := context.Background()

	authors, err := c.GetAuthors(ctx)
	if err != nil {
		t.Fatalf("GetAuthors: %v", err)
	}
	if len(authors) != 1 || authors[0].AuthorName != "Ursula K. Le Guin" ||
		!authors[0].Monitored || !authors[0].MonitorNewItems ||
		authors[0].QualityProfileID != 3 || authors[0].MetadataProfileID != 2 ||
		authors[0].ForeignAuthorID != "OL28986A" || len(authors[0].Tags) != 2 {
		t.Fatalf("author decode = %+v", authors[0])
	}

	books, err := c.GetBooks(ctx)
	if err != nil {
		t.Fatalf("GetBooks: %v", err)
	}
	if len(books) != 1 || len(books[0].Editions) != 1 ||
		books[0].ForeignBookID != "OL8189755W" ||
		books[0].Editions[0].ISBN13 != "9780060792921" ||
		books[0].Editions[0].Format != "EPUB" || books[0].Editions[0].Language != "en" {
		t.Fatalf("book decode = %+v", books[0])
	}

	editions, err := c.GetEditions(ctx)
	if err != nil {
		t.Fatalf("GetEditions: %v", err)
	}
	if len(editions) != 1 || editions[0].BookID != 21 || editions[0].ASIN != "B000000001" {
		t.Fatalf("edition decode = %+v", editions)
	}

	files, err := c.GetBookFiles(ctx)
	if err != nil {
		t.Fatalf("GetBookFiles: %v", err)
	}
	if len(files) != 1 || files[0].Quality.Quality.Name != "Digital" ||
		files[0].Size != 1048576 || files[0].BookID != 21 {
		t.Fatalf("bookfile decode = %+v", files[0])
	}

	series, err := c.GetSeries(ctx)
	if err != nil {
		t.Fatalf("GetSeries: %v", err)
	}
	if len(series) != 1 || len(series[0].Links) != 2 ||
		series[0].Links[0].BookID != 21 || series[0].Links[1].SeriesID != 55 {
		t.Fatalf("series decode = %+v", series[0])
	}

	qp, err := c.GetQualityProfiles(ctx)
	if err != nil {
		t.Fatalf("GetQualityProfiles: %v", err)
	}
	if len(qp) != 1 || qp[0].Name != "Digital Unlimited" ||
		qp[0].CutoffQualityID != 1 || len(qp[0].Items) != 1 || !qp[0].Items[0].Allowed {
		t.Fatalf("quality profile decode = %+v", qp[0])
	}

	qd, err := c.GetQualityDefinitions(ctx)
	if err != nil {
		t.Fatalf("GetQualityDefinitions: %v", err)
	}
	if len(qd) != 1 || qd[0].Quality.Name != "Digital" || qd[0].Title != "Any" {
		t.Fatalf("quality definition decode = %+v", qd[0])
	}

	mp, err := c.GetMetadataProfiles(ctx)
	if err != nil {
		t.Fatalf("GetMetadataProfiles: %v", err)
	}
	if len(mp) != 2 || mp[0].Name != "Books" || mp[1].Name != "MinPop 50" {
		t.Fatalf("metadata profile decode = %+v", mp)
	}

	rootFolders, err := c.GetRootFolders(ctx)
	if err != nil {
		t.Fatalf("GetRootFolders: %v", err)
	}
	if len(rootFolders) != 1 || !rootFolders[0].IsCalibreLibrary ||
		rootFolders[0].Host != "192.168.0.10" || rootFolders[0].Port != 8080 ||
		rootFolders[0].Password != "s3cret" || rootFolders[0].OutputProfile != "default" ||
		rootFolders[0].OutputFormat != "EPUB" {
		t.Fatalf("rootfolder decode = %+v", rootFolders[0])
	}

	tags, err := c.GetTags(ctx)
	if err != nil {
		t.Fatalf("GetTags: %v", err)
	}
	if len(tags) != 2 || tags[1].Label != "Backlist" {
		t.Fatalf("tag decode = %+v", tags)
	}

	indexers, err := c.GetIndexers(ctx)
	if err != nil {
		t.Fatalf("GetIndexers: %v", err)
	}
	if len(indexers) != 1 || indexers[0].Name != "SomeNZB" {
		t.Fatalf("indexer decode = %+v", indexers[0])
	}
	baseURL, ok := indexerFieldValue(indexers[0].Fields, "baseUrl")
	if !ok || baseURL != "https://indexer.example" {
		t.Fatalf("indexer baseUrl field = %q, %v", baseURL, ok)
	}
	key, _ := indexerFieldValue(indexers[0].Fields, "apiKey")
	if key != "idx-key" {
		t.Fatalf("indexer apiKey field = %q", key)
	}

	clients, err := c.GetDownloadClients(ctx)
	if err != nil {
		t.Fatalf("GetDownloadClients: %v", err)
	}
	if len(clients) != 1 || !clients[0].Enable || clients[0].Protocol != "http" {
		t.Fatalf("downloadclient decode = %+v", clients[0])
	}

	mm, err := c.GetMediaManagement(ctx)
	if err != nil {
		t.Fatalf("GetMediaManagement: %v", err)
	}
	if !mm.AutoUnmonitorPreviouslyDownloadedBooks || !mm.DeleteEmptyFolders ||
		!mm.RecycleBin || mm.RecycleBinCleanupDays != 14 || mm.MinimumFreeSpaceWhenImporting != 2048 {
		t.Fatalf("mediamanagement decode = %+v", mm)
	}

	// Every endpoint hit exactly once.
	for _, path := range []string{
		"/api/v1/author", "/api/v1/book", "/api/v1/edition", "/api/v1/bookfile",
		"/api/v1/series", "/api/v1/qualityprofile", "/api/v1/qualitydefinition",
		"/api/v1/metadataprofile", "/api/v1/rootfolder", "/api/v1/tag",
		"/api/v1/indexer", "/api/v1/downloadclient", "/api/v1/config/mediamanagement",
	} {
		if f.hits[path] != 1 {
			t.Errorf("%s hit %d times, want 1", path, f.hits[path])
		}
	}
}

func TestReadarrClientContractError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "k" {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"status":"403","error":"Forbidden","message":"Invalid API key"}`))
			return
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()
	c := NewReadarrClient(srv.URL, "wrong")
	if _, err := c.GetAuthors(context.Background()); err == nil || !strings.Contains(err.Error(), "Invalid API key") {
		t.Fatalf("want contract error surfaced, got %v", err)
	}
}

func TestReadarrFieldString(t *testing.T) {
	stringField := ReadarrField{Name: "host"}
	if err := json.Unmarshal([]byte(`"sickchill.example"`), &stringField.Value); err != nil {
		t.Fatalf("unmarshal string: %v", err)
	}
	if got := stringField.String(); got != "sickchill.example" {
		t.Fatalf("string field = %q", got)
	}
	numField := ReadarrField{Name: "port"}
	if err := json.Unmarshal([]byte(`8081`), &numField.Value); err != nil {
		t.Fatalf("unmarshal number: %v", err)
	}
	if got := numField.String(); got != "8081" {
		t.Fatalf("number field = %q", got)
	}
	nullField := ReadarrField{Name: "password"}
	if err := json.Unmarshal([]byte(`null`), &nullField.Value); err != nil {
		t.Fatalf("unmarshal null: %v", err)
	}
	if got := nullField.String(); got != "" {
		t.Fatalf("null field = %q", got)
	}
}
