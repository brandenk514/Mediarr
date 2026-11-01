package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/brandenk514/mediarr/internal/downloadclients"
	"github.com/brandenk514/mediarr/internal/indexers"
)

// fakeQBServer is a qBittorrent Web API stub that counts add-torrent calls,
// so the PR5 grab tests can assert WHICH saved client a release was routed
// to (Prowlarr PR5 plural routing).
func fakeQBServer(t *testing.T) (*httptest.Server, *int) {
	t.Helper()
	var adds int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v2/torrents/add" {
			adds++
		}
		_, _ = w.Write([]byte(`{"msg":"Ok."}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &adds
}

// newPR5GrabServer wires the plural routing path (Clients + DownloadsDir, no
// legacy single client) so resolveGrabClient is the only grab path.
func newPR5GrabServer(t *testing.T, repo *fakeIdxRepo, dc *fakeDCRepo) (*Server, string, *fakeActivityRepo) {
	t.Helper()
	s, token := newAuthServer(t)
	activity := &fakeActivityRepo{}
	s.SetIndexerSearch(&IndexerSearchDeps{
		Indexers:     repo,
		Registry:     testSearchRegistry(t),
		Activity:     activity,
		Clients:      dc,
		DownloadsDir: t.TempDir(),
	})
	return s, token, activity
}

func pr5Grab(t *testing.T, s *Server, token, indexer, protocol string) *httptest.ResponseRecorder {
	t.Helper()
	return doJSON(t, s.Handler(), http.MethodPost, "/api/v1/search/grab", token, map[string]any{
		"title": "Sample.2026.1080p", "indexer": indexer, "protocol": protocol,
		"url": "magnet:?xt=urn:btih:pr5stub",
	})
}

// TestPR5_Grab_BindingHonored: two enabled qBittorrent clients, indexer bound
// to the second — the grab must hit the second, not the first.
func TestPR5_Grab_BindingHonored(t *testing.T) {
	aSrv, aAdds := fakeQBServer(t)
	bSrv, bAdds := fakeQBServer(t)
	dc := newFakeDCRepo()
	_, _, _ = dc.Create(context.Background(), downloadclients.Record{
		Name: "a", Kind: downloadclients.KindQB, Enabled: true, Position: 1,
		BaseURL: aSrv.URL, APIKey: "***",
	})
	b, _, _ := dc.Create(context.Background(), downloadclients.Record{
		Name: "b", Kind: downloadclients.KindQB, Enabled: true, Position: 2,
		BaseURL: bSrv.URL, APIKey: "***",
	})
	repo := newFakeIdxRepo()
	_, _ = repo.Create(context.Background(), indexers.Definition{
		Name: "good-ix", Kind: "good", Enabled: true, DownloadClientID: &b,
	})
	s, token, activity := newPR5GrabServer(t, repo, dc)

	rec := pr5Grab(t, s, token, "good-ix", indexers.ProtocolTorrent)
	if rec.Code != http.StatusOK {
		t.Fatalf("grab: got %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if *aAdds != 0 || *bAdds != 1 {
		t.Errorf("adds a=%d b=%d, want 0/1 (binding honored)", *aAdds, *bAdds)
	}
	if _, g, _ := activity.Totals(context.Background()); g != 1 {
		t.Errorf("recorded grabs %d, want 1", g)
	}
}

// TestPR5_Grab_DisabledBindingFallsThrough: the bound client is disabled, so
// the grab routes to the first enabled client (position order).
func TestPR5_Grab_DisabledBindingFallsThrough(t *testing.T) {
	aSrv, aAdds := fakeQBServer(t)
	bSrv, bAdds := fakeQBServer(t)
	dc := newFakeDCRepo()
	_, _, _ = dc.Create(context.Background(), downloadclients.Record{
		Name: "a", Kind: downloadclients.KindQB, Enabled: true, Position: 1,
		BaseURL: aSrv.URL, APIKey: "***",
	})
	b, _, _ := dc.Create(context.Background(), downloadclients.Record{
		Name: "b", Kind: downloadclients.KindQB, Enabled: false, Position: 2,
		BaseURL: bSrv.URL, APIKey: "***",
	})
	repo := newFakeIdxRepo()
	_, _ = repo.Create(context.Background(), indexers.Definition{
		Name: "good-ix", Kind: "good", Enabled: true, DownloadClientID: &b,
	})
	s, token, _ := newPR5GrabServer(t, repo, dc)

	rec := pr5Grab(t, s, token, "good-ix", indexers.ProtocolTorrent)
	if rec.Code != http.StatusOK {
		t.Fatalf("grab: got %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if *aAdds != 1 || *bAdds != 0 {
		t.Errorf("adds a=%d b=%d, want 1/0 (disabled binding falls through)", *aAdds, *bAdds)
	}
}

// TestPR5_Grab_ProtocolMismatchFallsThrough: the bound client is SABnzbd,
// which cannot take a torrent — the grab routes to the first enabled
// torrent-capable client.
func TestPR5_Grab_ProtocolMismatchFallsThrough(t *testing.T) {
	aSrv, aAdds := fakeQBServer(t)
	dc := newFakeDCRepo()
	a, _, _ := dc.Create(context.Background(), downloadclients.Record{
		Name: "sab", Kind: downloadclients.KindSAB, Enabled: true, Position: 1,
		BaseURL: aSrv.URL, APIKey: "***",
	})
	bSrv, bAdds := fakeQBServer(t)
	_, _, _ = dc.Create(context.Background(), downloadclients.Record{
		Name: "qb", Kind: downloadclients.KindQB, Enabled: true, Position: 2,
		BaseURL: bSrv.URL, APIKey: "***",
	})
	repo := newFakeIdxRepo()
	_, _ = repo.Create(context.Background(), indexers.Definition{
		Name: "good-ix", Kind: "good", Enabled: true, DownloadClientID: &a,
	})
	s, token, _ := newPR5GrabServer(t, repo, dc)

	rec := pr5Grab(t, s, token, "good-ix", indexers.ProtocolTorrent)
	if rec.Code != http.StatusOK {
		t.Fatalf("grab: got %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if *aAdds != 0 || *bAdds != 1 {
		t.Errorf("adds a=%d b=%d, want 0/1 (protocol mismatch falls through)", *aAdds, *bAdds)
	}
}

// TestPR5_Grab_UnknownBindingFallsThrough: the bound id does not exist in the
// store, so the grab routes to the first enabled client.
func TestPR5_Grab_UnknownBindingFallsThrough(t *testing.T) {
	aSrv, aAdds := fakeQBServer(t)
	bSrv, bAdds := fakeQBServer(t)
	dc := newFakeDCRepo()
	_, _, _ = dc.Create(context.Background(), downloadclients.Record{
		Name: "a", Kind: downloadclients.KindQB, Enabled: true, Position: 1,
		BaseURL: aSrv.URL, APIKey: "***",
	})
	_, _, _ = dc.Create(context.Background(), downloadclients.Record{
		Name: "b", Kind: downloadclients.KindQB, Enabled: true, Position: 2,
		BaseURL: bSrv.URL, APIKey: "***",
	})
	gone := int64(999)
	repo := newFakeIdxRepo()
	_, _ = repo.Create(context.Background(), indexers.Definition{
		Name: "good-ix", Kind: "good", Enabled: true, DownloadClientID: &gone,
	})
	s, token, _ := newPR5GrabServer(t, repo, dc)

	rec := pr5Grab(t, s, token, "good-ix", indexers.ProtocolTorrent)
	if rec.Code != http.StatusOK {
		t.Fatalf("grab: got %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if *aAdds != 1 || *bAdds != 0 {
		t.Errorf("adds a=%d b=%d, want 1/0 (unknown binding falls through)", *aAdds, *bAdds)
	}
}

// TestPR5_Grab_TransmissionBindingFallsThrough: a transmission binding cannot
// be built (no adapter yet), so the grab routes to the first enabled client
// that can handle the protocol.
func TestPR5_Grab_TransmissionBindingFallsThrough(t *testing.T) {
	qbSrv, qbAdds := fakeQBServer(t)
	dc := newFakeDCRepo()
	tID, _, _ := dc.Create(context.Background(), downloadclients.Record{
		Name: "trans", Kind: downloadclients.KindTransmission, Enabled: true,
		Position: 1, BaseURL: "http://127.0.0.1:1",
	})
	_, _, _ = dc.Create(context.Background(), downloadclients.Record{
		Name: "qb", Kind: downloadclients.KindQB, Enabled: true, Position: 2,
		BaseURL: qbSrv.URL, APIKey: "***",
	})
	repo := newFakeIdxRepo()
	_, _ = repo.Create(context.Background(), indexers.Definition{
		Name: "good-ix", Kind: "good", Enabled: true, DownloadClientID: &tID,
	})
	s, token, _ := newPR5GrabServer(t, repo, dc)

	rec := pr5Grab(t, s, token, "good-ix", indexers.ProtocolTorrent)
	if rec.Code != http.StatusOK {
		t.Fatalf("grab: got %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if *qbAdds != 1 {
		t.Errorf("qb adds %d, want 1 (unbuildable binding falls through)", *qbAdds)
	}
}

// TestPR5_Grab_NoClients503: with no saved clients at all, a grab is a 503.
func TestPR5_Grab_NoClients503(t *testing.T) {
	repo := newFakeIdxRepo()
	s, token, _ := newPR5GrabServer(t, repo, newFakeDCRepo())

	rec := pr5Grab(t, s, token, "good-ix", indexers.ProtocolTorrent)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("grab: got %d, want 503: %s", rec.Code, rec.Body.String())
	}
}
