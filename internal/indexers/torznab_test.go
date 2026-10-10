package indexers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestTorznab_Search_ParsesFixtures drives Search against a local httptest
// server that returns canned Torznab RSS. It covers the three link shapes the
// adapter must classify (magnet, .torrent, NZB) plus the empty-feed case
// (#31: "fixtures + error mapping").
func TestTorznab_Search_ParsesFixtures(t *testing.T) {
	feed := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:torznab="https://torznab.com/schemas/spec/">
  <channel>
    <title>results</title>
    <item>
      <title>Movie.Name.2024.1080p.WEB-DL</title>
      <guid>guid-1</guid>
      <link>http://example.com/magnet-1</link>
      <enclosure url="magnet:?xt=urn:btih:0123" length="1073741824"/>
      <size>1073741824</size>
    </item>
    <item>
      <title>Movie.Name.2024.1080p.torrent</title>
      <guid>guid-2</guid>
      <link>http://example.com/dl/movie.torrent</link>
      <enclosure url="http://example.com/dl/movie.torrent" length="2048"/>
    </item>
    <item>
      <title>Usenet.Release.2024.nzb</title>
      <guid>guid-3</guid>
      <link>http://usenet.example/getnzb/guid-3</link>
      <enclosure url="http://usenet.example/getnzb/guid-3" length="512"/>
    </item>
    <item>
      <title></title>
      <guid>guid-4</guid>
    </item>
  </channel>
</rss>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("t") != "search" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if q.Get("apikey") != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(feed))
	}))
	defer srv.Close()

	tk := NewHealthTracker()
	adapter := NewTorznabIndexer(Definition{
		Name:    "test-index",
		BaseURL: srv.URL,
		APIKey:  "secret",
	}, AdapterDeps{Health: tk})

	res, err := adapter.Search(context.Background(), SearchQuery{Term: "Movie"})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(res) != 3 {
		t.Fatalf("expected 3 results (title-less item skipped), got %d", len(res))
	}

	byTitle := map[string]SearchResult{}
	for _, r := range res {
		byTitle[r.Title] = r
	}

	m := byTitle["Movie.Name.2024.1080p.WEB-DL"]
	if m.Info.URL != "magnet:?xt=urn:btih:0123" {
		t.Errorf("magnet: expected enclosure url, got %q", m.Info.URL)
	}
	if m.Info.Protocol != ProtocolTorrent {
		t.Errorf("magnet: expected ProtocolTorrent, got %q", m.Info.Protocol)
	}
	if m.SizeBytes != 1073741824 {
		t.Errorf("magnet: expected size 1073741824, got %d", m.SizeBytes)
	}
	if m.Indexer != "test-index" {
		t.Errorf("magnet: expected indexer test-index, got %q", m.Indexer)
	}

	tf := byTitle["Movie.Name.2024.1080p.torrent"]
	if tf.Info.Protocol != ProtocolTorrentFile {
		t.Errorf("torrent-file: expected ProtocolTorrentFile, got %q", tf.Info.Protocol)
	}
	if tf.SizeBytes != 2048 { // enclosure length fallback
		t.Errorf("torrent-file: expected enclosure length 2048, got %d", tf.SizeBytes)
	}

	n := byTitle["Usenet.Release.2024.nzb"]
	if n.Info.URL == "" {
		t.Error("nzb: expected a non-empty download URL")
	}
}

// TestTorznab_Search_EmptyFeed is a legal "no matches" (nil, nil), not an
// error.
func TestTorznab_Search_EmptyFeed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<?xml version="1.0"?><rss><channel></channel></rss>`))
	}))
	defer srv.Close()

	adapter := NewTorznabIndexer(Definition{Name: "empty", BaseURL: srv.URL, APIKey: "k"}, AdapterDeps{})
	res, err := adapter.Search(context.Background(), SearchQuery{Term: "nothing"})
	if err != nil {
		t.Fatalf("expected nil error for empty feed, got %v", err)
	}
	if res != nil {
		t.Fatalf("expected nil results for empty feed, got %v", res)
	}
}

// TestTorznab_Search_MalformedXML maps a corrupt feed to ErrBadResponse (the
// adapter must not surface a fatal parse error that would take down a fan-out).
func TestTorznab_Search_MalformedXML(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`this is not xml at all <<<`))
	}))
	defer srv.Close()

	adapter := NewTorznabIndexer(Definition{Name: "bad", BaseURL: srv.URL, APIKey: "k"}, AdapterDeps{})
	_, err := adapter.Search(context.Background(), SearchQuery{Term: "x"})
	if err == nil {
		t.Fatal("expected an error for malformed XML, got nil")
	}
	if !errors.Is(err, ErrBadResponse) {
		t.Fatalf("expected ErrBadResponse, got %v (%T)", err, err)
	}
}

// TestTorznab_ErrorMapping verifies each non-2xx status maps to its sentinel
// (#31: "error mapping: 4xx/5xx → typed errors"). The adapter returns the
// typed error from its shared httpGet, so we assert with errors.Is.
func TestTorznab_ErrorMapping(t *testing.T) {
	cases := []struct {
		status int
		want   error
	}{
		{401, ErrUnauthorized},
		{403, ErrUnauthorized},
		{404, ErrNotFound},
		{400, ErrBadRequest},
		{429, ErrRateLimited},
		{500, ErrServerError},
		{503, ErrServerError},
	}
	for _, tc := range cases {
		t.Run(string(rune(tc.status)), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
			}))
			defer srv.Close()
			adapter := NewTorznabIndexer(Definition{Name: "e", BaseURL: srv.URL, APIKey: "k"}, AdapterDeps{})
			_, err := adapter.Search(context.Background(), SearchQuery{Term: "x"})
			if err == nil {
				t.Fatalf("expected error for status %d, got nil", tc.status)
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("status %d: expected %v, got %v", tc.status, tc.want, err)
			}
		})
	}
}

// TestTorznab_Test_CapsProbe confirms Test() issues a t=caps request and
// succeeds on 2xx.
func TestTorznab_Test_CapsProbe(t *testing.T) {
	var sawT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawT = r.URL.Query().Get("t")
		_, _ = w.Write([]byte(`<rss><channel></channel></rss>`))
	}))
	defer srv.Close()

	adapter := NewTorznabIndexer(Definition{Name: "t", BaseURL: srv.URL, APIKey: "k"}, AdapterDeps{})
	if err := adapter.Test(context.Background()); err != nil {
		t.Fatalf("Test returned error: %v", err)
	}
	if sawT != "caps" {
		t.Fatalf("expected t=caps probe, got t=%q", sawT)
	}
}

// TestTorznab_Test_Unauthorized confirms a bad key surfaces as ErrUnauthorized
// (distinct from a network fault), so a wrong key is actionable.
func TestTorznab_Test_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	adapter := NewTorznabIndexer(Definition{Name: "t", BaseURL: srv.URL, APIKey: "bad"}, AdapterDeps{})
	err := adapter.Test(context.Background())
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}
}

// TestTorznab_RecordsHealth confirms the adapter records each call to the
// shared HealthTracker (the seam #34/#35 depend on).
func TestTorznab_RecordsHealth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<?xml version="1.0"?><rss><channel><item><title>x</title><link>magnet:?xt=a</link></item></channel></rss>`))
	}))
	defer srv.Close()

	tk := NewHealthTracker()
	adapter := NewTorznabIndexer(Definition{Name: "h", BaseURL: srv.URL, APIKey: "k"}, AdapterDeps{Health: tk})
	_, _ = adapter.Search(context.Background(), SearchQuery{Term: "x"})

	st := tk.Stats("h")
	if st.Successes != 1 {
		t.Errorf("expected 1 success, got %+v", st)
	}
	if st.Calls != 1 {
		t.Errorf("expected 1 call, got %+v", st)
	}
}

// TestClassifyError covers the status→sentinel table directly, including the
// 2xx/3xx → nil boundary.
func TestClassifyError(t *testing.T) {
	if e := ClassifyError(200); e != nil {
		t.Errorf("200 → expected nil, got %v", e)
	}
	if e := ClassifyError(301); e != nil {
		t.Errorf("301 → expected nil, got %v", e)
	}
	if !errors.Is(ClassifyError(401), ErrUnauthorized) {
		t.Error("401 → expected ErrUnauthorized")
	}
	if !errors.Is(ClassifyError(500), ErrServerError) {
		t.Error("500 → expected ErrServerError")
	}
}

// TestError_MessageShape confirms the composed error string includes op +
// sentinel + detail and stays credential-free.
func TestError_MessageShape(t *testing.T) {
	e := newError("torznab search", ErrUnauthorized, "status 401")
	if !strings.Contains(e.Error(), "torznab search") {
		t.Errorf("expected op in message: %q", e.Error())
	}
	if !strings.Contains(e.Error(), "status 401") {
		t.Errorf("expected detail in message: %q", e.Error())
	}
	if !errors.Is(e, ErrUnauthorized) {
		t.Error("expected errors.Is to match the sentinel")
	}
}

// TestTorznab_Search_SendsSeasonEpisode confirms the season/episode query
// fields are threaded into the Torznab request (#31 S/E plumbing).
func TestTorznab_Search_SendsSeasonEpisode(t *testing.T) {
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		got = map[string]string{
			"q":      q.Get("q"),
			"year":   q.Get("year"),
			"season": q.Get("season"),
			"ep":     q.Get("ep"),
		}
		_, _ = w.Write([]byte(`<?xml version="1.0"?><rss><channel></channel></rss>`))
	}))
	defer srv.Close()

	adapter := NewTorznabIndexer(Definition{Name: "s", BaseURL: srv.URL, APIKey: "k"}, AdapterDeps{})
	_, _ = adapter.Search(context.Background(), SearchQuery{Term: "show", Year: 2020, Season: 3, Episode: 7})
	if got["q"] != "show" || got["year"] != "2020" || got["season"] != "3" || got["ep"] != "7" {
		t.Fatalf("expected q=show year=2020 season=3 ep=7, got %v", got)
	}
}
