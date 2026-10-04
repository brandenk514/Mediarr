package music

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	dom "github.com/brandenk514/mediarr/internal/domains/music"
	"github.com/brandenk514/mediarr/internal/downloads"
	"github.com/brandenk514/mediarr/internal/indexers"
)

// These are fast, in-memory unit tests for the Service's individual use-cases
// and private helpers. The full pipeline is covered by e2e_test.go (and the
// Postgres e2e in internal/postgres); this file targets the remaining branches
// — the monitoring toggles (#22 "worker creates/updates wanted entries"),
// pass-through queries, no-wanted / no-client / indexer-failure /
// client-add-failure, and the pure helpers — so services/music clears the
// >=80% coverage bar in PLAN §10. It mirrors services/tv/service_unit_test.go
// (added in the M2 closeout).

// errIndexer is a stub indexer whose Search always fails, used to exercise the
// search-fanout error path (a single failing indexer must not fail the search).
type errIndexer struct{}

func (errIndexer) Name() string { return "err" }
func (errIndexer) Search(context.Context, indexers.SearchQuery) ([]indexers.SearchResult, error) {
	return nil, errors.New("indexer boom")
}

// failClient is a stub download client whose Add always fails, used to exercise
// the sendToClient -> Client.Add error path.
type failClient struct{ name string }

func (c failClient) Name() string { return c.name }
func (c failClient) Add(context.Context, downloads.Release) error {
	return errors.New("client add boom")
}
func (c failClient) Status(context.Context, string) (downloads.Status, error) {
	return downloads.Status{}, nil
}

// neverCompleteClient always reports not-complete, for the waitComplete
// timeout/cancel path.
type neverCompleteClient struct{}

func (neverCompleteClient) Name() string                                 { return "never" }
func (neverCompleteClient) Add(context.Context, downloads.Release) error { return nil }
func (neverCompleteClient) Status(context.Context, string) (downloads.Status, error) {
	return downloads.Status{Complete: false, Progress: 0}, nil
}

func setupService(t *testing.T) (*Service, *memRepo) {
	t.Helper()
	dlDir := t.TempDir()
	mock, err := downloads.NewMockClient("mock", dlDir)
	if err != nil {
		t.Fatalf("mock client: %v", err)
	}
	fake := indexers.NewFakeIndexer("fake")
	repo := newMemRepo()
	svc := New(Deps{
		Repo:           repo,
		Indexers:       []indexers.Searcher{fake},
		Client:         mock,
		MediaRoot:      t.TempDir(),
		DefaultProfile: "Lossless",
	})
	return svc, repo
}

// historyHas reports whether the artist's history contains an entry with the
// given event.
func historyHas(t *testing.T, repo *memRepo, artistID int64, event string) bool {
	t.Helper()
	hist, err := repo.ListHistory(context.Background(), artistID, 0)
	if err != nil {
		t.Fatalf("list history: %v", err)
	}
	for _, h := range hist {
		if h.Event == event {
			return true
		}
	}
	return false
}

// pendingWanted counts the pending wanted rows for one album.
func pendingWanted(t *testing.T, repo *memRepo, albumID int64) int {
	t.Helper()
	wants, err := repo.ListWanted(context.Background(), albumID)
	if err != nil {
		t.Fatalf("list wanted: %v", err)
	}
	n := 0
	for _, w := range wants {
		if w.Status == dom.WantedPending {
			n++
		}
	}
	return n
}

// TestNew_DefaultProfile exercises the default-profile assignment in New: a
// blank profile defaults to "Lossless"; a provided one is preserved.
func TestNew_DefaultProfile(t *testing.T) {
	if got := New(Deps{}).deps.DefaultProfile; got != "Lossless" {
		t.Errorf("DefaultProfile = %q, want Lossless (default)", got)
	}
	if got := New(Deps{DefaultProfile: "320K"}).deps.DefaultProfile; got != "320K" {
		t.Errorf("DefaultProfile = %q, want 320K (preserved)", got)
	}
}

func TestAddArtist(t *testing.T) {
	svc, repo := setupService(t)
	ctx := context.Background()

	// Blank name is rejected.
	if _, err := svc.AddArtist(ctx, "   ", true); err == nil {
		t.Error("expected error for blank artist name, got nil")
	}

	// Valid add: name trimmed, monitored flag stored, history recorded.
	id, err := svc.AddArtist(ctx, "  Bob Marley  ", true)
	if err != nil {
		t.Fatalf("add artist: %v", err)
	}
	a, _ := repo.GetArtist(ctx, id)
	if a.Name != "Bob Marley" {
		t.Errorf("name = %q, want trimmed 'Bob Marley'", a.Name)
	}
	if !a.Monitored {
		t.Error("artist should be monitored")
	}
	if !historyHas(t, repo, id, "added") {
		t.Error("expected an 'added' history entry")
	}
}

func TestAddAlbum(t *testing.T) {
	svc, repo := setupService(t)
	ctx := context.Background()
	artistID, _ := svc.AddArtist(ctx, "Bob Marley", true)

	// Blank name is rejected.
	if _, err := svc.AddAlbum(ctx, artistID, "  ", 1977, "Lossless", true); err == nil {
		t.Error("expected error for blank album name, got nil")
	}

	// A blank profile falls back to the default; a monitored album gets a
	// pending whole-album wanted row; an unmonitored one does not.
	monID, err := svc.AddAlbum(ctx, artistID, "Legend", 1977, "", true)
	if err != nil {
		t.Fatalf("add album: %v", err)
	}
	mon, _ := repo.GetAlbum(ctx, monID)
	if mon.QualityProfile != "Lossless" {
		t.Errorf("profile = %q, want default Lossless", mon.QualityProfile)
	}
	if mon.VerifyChecksums != true {
		t.Error("VerifyChecksums should default on (PLAN §15.5)")
	}
	if pendingWanted(t, repo, monID) != 1 {
		t.Errorf("monitored album pending wanted = %d, want 1", pendingWanted(t, repo, monID))
	}

	unmonID, _ := svc.AddAlbum(ctx, artistID, "Uprising", 1980, "320K", false)
	if pendingWanted(t, repo, unmonID) != 0 {
		t.Errorf("unmonitored album pending wanted = %d, want 0", pendingWanted(t, repo, unmonID))
	}
}

// TestSetArtistMonitored verifies the artist-level toggle (PLAN §4) fans out to
// every known album and ensures a pending whole-album wanted row on toggle-on.
func TestSetArtistMonitored(t *testing.T) {
	svc, repo := setupService(t)
	ctx := context.Background()

	// Artist added unmonitored; two known unmonitored albums.
	artistID, _ := svc.AddArtist(ctx, "Bob Marley", false)
	if _, err := svc.AddAlbum(ctx, artistID, "Legend", 1977, "Lossless", false); err != nil {
		t.Fatalf("add album 1: %v", err)
	}
	if _, err := svc.AddAlbum(ctx, artistID, "Uprising", 1980, "Lossless", false); err != nil {
		t.Fatalf("add album 2: %v", err)
	}
	albums, _ := repo.ListAlbums(ctx, artistID)
	legendID, uprisingID := albums[0].ID, albums[1].ID

	// Turning the artist on must fan out to both albums and ensure a wanted row
	// for each.
	if err := svc.SetArtistMonitored(ctx, artistID, true); err != nil {
		t.Fatalf("set artist monitored on: %v", err)
	}
	a, _ := repo.GetArtist(ctx, artistID)
	if !a.Monitored {
		t.Error("artist should be monitored after toggle-on")
	}
	for _, id := range []int64{legendID, uprisingID} {
		al, _ := repo.GetAlbum(ctx, id)
		if !al.Monitored {
			t.Errorf("album %d should be monitored after artist toggle-on", id)
		}
		if pendingWanted(t, repo, id) != 1 {
			t.Errorf("album %d pending wanted = %d, want 1 (fan-out)", id, pendingWanted(t, repo, id))
		}
	}
	if !historyHas(t, repo, artistID, "monitored") {
		t.Error("expected a 'monitored' history entry")
	}

	// Turning the artist off flips every album unmonitored. (Per PLAN §4 the
	// wanted rows are the pipeline's work queue and are intentionally left in
	// place, matching services/tv; re-monitoring is idempotent via EnsureWanted.)
	if err := svc.SetArtistMonitored(ctx, artistID, false); err != nil {
		t.Fatalf("set artist monitored off: %v", err)
	}
	a, _ = repo.GetArtist(ctx, artistID)
	if a.Monitored {
		t.Error("artist should be unmonitored after toggle-off")
	}
	for _, id := range []int64{legendID, uprisingID} {
		al, _ := repo.GetAlbum(ctx, id)
		if al.Monitored {
			t.Errorf("album %d should be unmonitored after artist toggle-off", id)
		}
	}
	if !historyHas(t, repo, artistID, "unmonitored") {
		t.Error("expected an 'unmonitored' history entry")
	}

	// A missing artist surfaces the repo error.
	if err := svc.SetArtistMonitored(ctx, 999, true); err == nil {
		t.Error("expected error for missing artist")
	}
}

// TestSetAlbumMonitored verifies the album-level toggle ensures a pending
// whole-album wanted row on toggle-on (idempotent: a re-monitored album keeps
// its existing wanted status) and does not touch wanted on toggle-off.
func TestSetAlbumMonitored(t *testing.T) {
	svc, repo := setupService(t)
	ctx := context.Background()

	artistID, _ := svc.AddArtist(ctx, "Bob Marley", true)
	albumID, _ := svc.AddAlbum(ctx, artistID, "Legend", 1977, "Lossless", true)
	// The monitored add already created a pending wanted row; satisfy it so that
	// re-monitoring must preserve (not clobber) the existing status.
	if err := repo.MarkWantedSatisfied(ctx, albumID, 0, "x"); err != nil {
		t.Fatalf("mark satisfied: %v", err)
	}

	// Toggle off, then on again: the wanted row must still exist and keep its
	// satisfied status (EnsureWanted is a no-op if one already exists).
	if err := svc.SetAlbumMonitored(ctx, albumID, false); err != nil {
		t.Fatalf("set album monitored off: %v", err)
	}
	al, _ := repo.GetAlbum(ctx, albumID)
	if al.Monitored {
		t.Error("album should be unmonitored after toggle-off")
	}
	if err := svc.SetAlbumMonitored(ctx, albumID, true); err != nil {
		t.Fatalf("set album monitored on: %v", err)
	}
	al, _ = repo.GetAlbum(ctx, albumID)
	if !al.Monitored {
		t.Error("album should be monitored after toggle-on")
	}
	w, err := repo.GetWanted(ctx, albumID, 0)
	if err != nil {
		t.Fatalf("expected a whole-album wanted row after re-monitoring: %v", err)
	}
	if w.Status != dom.WantedSatisfied {
		t.Errorf("wanted status = %q, want satisfied (re-monitor must not reset it)", w.Status)
	}

	// A missing album surfaces the repo error.
	if err := svc.SetAlbumMonitored(ctx, 999, true); err == nil {
		t.Error("expected error for missing album")
	}
}

// TestSetTrackMonitored verifies the track-level toggle ensures a pending
// track-level wanted row on toggle-on and does not touch wanted on toggle-off.
func TestSetTrackMonitored(t *testing.T) {
	svc, repo := setupService(t)
	ctx := context.Background()

	artistID, _ := svc.AddArtist(ctx, "Bob Marley", true)
	// The album is added unmonitored so no whole-album wanted row exists yet; the
	// track toggle below must be the only source of a wanted row.
	albumID, _ := svc.AddAlbum(ctx, artistID, "Legend", 1977, "Lossless", false)
	trackID, _ := svc.AddTrack(ctx, albumID, 1, 1, "No Woman, No Cry", false)
	if pendingWanted(t, repo, albumID) != 0 {
		t.Fatalf("pending wanted before toggle = %d, want 0", pendingWanted(t, repo, albumID))
	}

	// Toggle the track on: it must be flagged monitored and get a track-level
	// wanted row (TrackID = the track's id, not 0).
	if err := svc.SetTrackMonitored(ctx, albumID, 1, 1, true); err != nil {
		t.Fatalf("set track monitored on: %v", err)
	}
	tr, _ := repo.GetTrack(ctx, albumID, 1, 1)
	if !tr.Monitored {
		t.Error("track should be monitored after toggle-on")
	}
	w, err := repo.GetWanted(ctx, albumID, trackID)
	if err != nil {
		t.Fatalf("expected a track-level wanted row: %v", err)
	}
	if w.Status != dom.WantedPending {
		t.Errorf("track wanted status = %q, want pending", w.Status)
	}

	// Toggle off: unmonitored, and it must not error.
	if err := svc.SetTrackMonitored(ctx, albumID, 1, 1, false); err != nil {
		t.Fatalf("set track monitored off: %v", err)
	}
	tr, _ = repo.GetTrack(ctx, albumID, 1, 1)
	if tr.Monitored {
		t.Error("track should be unmonitored after toggle-off")
	}

	// A missing track surfaces the repo error.
	if err := svc.SetTrackMonitored(ctx, albumID, 1, 99, true); err == nil {
		t.Error("expected error for missing track")
	}
}

// TestRunPipeline_NoWanted verifies the pipeline short-circuits when the artist
// has no pending wanted entries (before any client / search work).
func TestRunPipeline_NoWanted(t *testing.T) {
	svc, _ := setupService(t)
	ctx := context.Background()
	artistID, _ := svc.AddArtist(ctx, "Bob Marley", true)
	// No albums, so no pending wanted.
	if _, err := svc.RunPipeline(ctx, artistID); !errors.Is(err, ErrNoWanted) {
		t.Errorf("RunPipeline = %v, want ErrNoWanted", err)
	}
	// A missing artist surfaces the repo error.
	if _, err := svc.RunPipeline(ctx, 999); err == nil {
		t.Error("expected error for missing artist")
	}
}

// TestRunPipeline_NoClient verifies the pipeline records "no-client" and errors
// when no download client is configured but there is pending work.
func TestRunPipeline_NoClient(t *testing.T) {
	dlDir := t.TempDir()
	_ = dlDir
	fake := indexers.NewFakeIndexer("fake")
	fake.AddRelease(indexers.SearchResult{Title: "Bob Marley - Legend [FLAC Lossless]", SizeBytes: 4_200_000_000})
	repo := newMemRepo()
	// Client is nil, but there is a pending wanted album.
	svc := New(Deps{
		Repo:           repo,
		Indexers:       []indexers.Searcher{fake},
		Client:         nil,
		MediaRoot:      t.TempDir(),
		DefaultProfile: "Lossless",
	})
	ctx := context.Background()

	artistID, _ := svc.AddArtist(ctx, "Bob Marley", true)
	if _, err := svc.AddAlbum(ctx, artistID, "Legend", 1977, "Lossless", true); err != nil {
		t.Fatalf("add album: %v", err)
	}

	_, err := svc.RunPipeline(ctx, artistID)
	if err == nil || !strings.Contains(err.Error(), "no download client") {
		t.Fatalf("RunPipeline = %v, want a no-client error", err)
	}
	if !historyHas(t, repo, artistID, "no-client") {
		t.Error("expected a 'no-client' history entry")
	}
}

// TestRunPipeline_IndexerFailureDoesNotFailSearch verifies a single failing
// indexer is skipped and the good indexer's release still matches and imports.
func TestRunPipeline_IndexerFailureDoesNotFailSearch(t *testing.T) {
	dlDir := t.TempDir()
	mock, _ := downloads.NewMockClient("mock", dlDir)
	// One failing indexer and one good indexer returning a lossless release.
	good := indexers.NewFakeIndexer("good")
	good.AddRelease(indexers.SearchResult{Title: "Bob Marley - Legend [FLAC Lossless]", SizeBytes: 4_200_000_000})
	repo := newMemRepo()
	svc := New(Deps{
		Repo:           repo,
		Indexers:       []indexers.Searcher{errIndexer{}, good},
		Client:         mock,
		MediaRoot:      t.TempDir(),
		DefaultProfile: "Lossless",
	})
	ctx := context.Background()

	artistID, _ := svc.AddArtist(ctx, "Bob Marley", true)
	_, _ = svc.AddAlbum(ctx, artistID, "Legend", 1977, "Lossless", true)

	// The failing indexer is skipped; the good one still yields a match, so the
	// pipeline should import rather than fail.
	imported, err := svc.RunPipeline(ctx, artistID)
	if err != nil {
		t.Fatalf("run pipeline: %v (expected the good indexer to be used)", err)
	}
	if imported != 1 {
		t.Errorf("imported = %d, want 1", imported)
	}
}

// TestRunPipeline_ClientAddFails verifies a failing download client marks the
// queue failed, records the failure, leaves the wanted pending, and returns
// ErrNoMatch (nothing imported).
func TestRunPipeline_ClientAddFails(t *testing.T) {
	dlDir := t.TempDir()
	_ = dlDir
	fake := indexers.NewFakeIndexer("fake")
	fake.AddRelease(indexers.SearchResult{Title: "Bob Marley - Legend [FLAC Lossless]", SizeBytes: 4_200_000_000})
	repo := newMemRepo()
	svc := New(Deps{
		Repo:           repo,
		Indexers:       []indexers.Searcher{fake},
		Client:         failClient{name: "fail"},
		MediaRoot:      t.TempDir(),
		DefaultProfile: "Lossless",
	})
	ctx := context.Background()

	artistID, _ := svc.AddArtist(ctx, "Bob Marley", true)
	albumID, _ := svc.AddAlbum(ctx, artistID, "Legend", 1977, "Lossless", true)

	// Client.Add fails -> import-failed recorded, nothing imported, no match.
	imported, err := svc.RunPipeline(ctx, artistID)
	if !errors.Is(err, ErrNoMatch) {
		t.Fatalf("run pipeline: err = %v, want ErrNoMatch (download failed)", err)
	}
	if imported != 0 {
		t.Errorf("imported = %d, want 0", imported)
	}
	w, _ := repo.GetWanted(ctx, albumID, 0)
	if w.Status != dom.WantedPending {
		t.Errorf("wanted status = %q, want pending (download failed)", w.Status)
	}
	if !historyHas(t, repo, artistID, "import-failed") {
		t.Error("expected an 'import-failed' history entry")
	}
}

// TestWaitComplete_CtxCancel verifies waitComplete returns promptly on a
// cancelled context rather than blocking.
func TestWaitComplete_CtxCancel(t *testing.T) {
	dlDir := t.TempDir()
	mock, _ := downloads.NewMockClient("mock", dlDir)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately
	svc := New(Deps{Repo: newMemRepo(), Client: mock, MediaRoot: t.TempDir()})
	if _, err := svc.waitComplete(ctx, "never"); err == nil {
		t.Fatal("expected error from waitComplete on cancelled context")
	}
}

// TestWaitComplete_NeverComplete verifies the timeout branch is reached when the
// client never reports completion. We bound it with a short context deadline so
// the test fails fast if the loop stops honoring the context.
func TestWaitComplete_NeverComplete(t *testing.T) {
	svc := New(Deps{Repo: newMemRepo(), Client: neverCompleteClient{}, MediaRoot: t.TempDir()})
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if _, err := svc.waitComplete(ctx, "never"); err == nil {
		t.Fatal("expected error from waitComplete when download never completes")
	}
}

func TestFileNameFor(t *testing.T) {
	cases := []struct {
		name  string
		album dom.Album
		rel   dom.CandidateRelease
		want  string
	}{
		{
			name:  "album name wins, flac ext",
			album: dom.Album{Name: "Legend"},
			rel:   dom.CandidateRelease{Quality: dom.ProfileMatch{Format: dom.FormatFLAC}},
			want:  "legend.flac",
		},
		{
			name:  "dashes and dots become spaces, lowercased",
			album: dom.Album{Name: "The White Album"},
			rel:   dom.CandidateRelease{Quality: dom.ProfileMatch{Format: dom.FormatAPE}},
			want:  "the white album.ape",
		},
		{
			name:  "empty album falls back to release title",
			album: dom.Album{},
			rel:   dom.CandidateRelease{Title: "Bob Marley - Legend", Quality: dom.ProfileMatch{Format: dom.FormatWAV}},
			want:  "bob marley legend.wav",
		},
		{
			name:  "empty album + empty title -> 'album'",
			album: dom.Album{},
			rel:   dom.CandidateRelease{Quality: dom.ProfileMatch{Format: dom.FormatMP3}},
			want:  "album.mp3",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := fileNameFor(tc.album, tc.rel); got != tc.want {
				t.Errorf("fileNameFor = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestExtForFormat(t *testing.T) {
	cases := map[string]string{
		dom.FormatFLAC: ".flac",
		dom.FormatAPE:  ".ape",
		dom.FormatWAV:  ".wav",
		dom.FormatM4A:  ".m4a",
		dom.FormatOpus: ".opus",
		dom.FormatMP3:  ".mp3",
		dom.FormatAAC:  ".mp3", // default
		dom.FormatAny:  ".mp3", // default
		"":             ".mp3", // default
	}
	for in, want := range cases {
		if got := extForFormat(in); got != want {
			t.Errorf("extForFormat(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCopyFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.flac")
	if err := os.WriteFile(src, []byte("data"), 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}
	// Dest is in a not-yet-created subdirectory; copyFile must create it.
	dst := filepath.Join(dir, "a", "b", "dst.flac")
	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if string(got) != "data" {
		t.Errorf("dest content = %q, want 'data'", string(got))
	}
}
