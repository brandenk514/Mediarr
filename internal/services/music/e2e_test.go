package music

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	dom "github.com/brandenk514/mediarr/internal/domains/music"
	"github.com/brandenk514/mediarr/internal/downloads"
	"github.com/brandenk514/mediarr/internal/indexers"
)

// memRepo is an in-memory implementation of dom.Repo (the music persistence
// surface) used by the end-to-end service test so the full pipeline runs with
// no Postgres or network I/O. It mirrors the TV service test harness.
type memRepo struct {
	artists    map[int64]dom.Artist
	nextArtist int64
	albums     map[int64]dom.Album
	nextAlbum  int64
	tracks     map[int64][]dom.Track // albumID -> tracks
	nextTrack  int64
	wanted     map[wantedKey]dom.Wanted
	queue      map[int64]dom.QueueEntry
	nextQueue  int64
	history    []dom.HistoryEntry
	nextHist   int64
}

// wantedKey identifies a wanted row by its album/track specifier.
type wantedKey struct {
	album int64
	track int64
}

func newMemRepo() *memRepo {
	return &memRepo{
		artists:    map[int64]dom.Artist{},
		albums:     map[int64]dom.Album{},
		tracks:     map[int64][]dom.Track{},
		wanted:     map[wantedKey]dom.Wanted{},
		queue:      map[int64]dom.QueueEntry{},
		nextArtist: 1,
		nextAlbum:  1,
		nextTrack:  1,
		nextQueue:  1,
		nextHist:   1,
	}
}

func (r *memRepo) CreateArtist(ctx context.Context, a dom.Artist) (int64, error) {
	for _, v := range r.artists {
		if v.Name == a.Name {
			return 0, errors.New("duplicate artist")
		}
	}
	a.ID = r.nextArtist
	r.nextArtist++
	a.AddedAt = time.Now()
	r.artists[a.ID] = a
	return a.ID, nil
}

func (r *memRepo) GetArtist(ctx context.Context, id int64) (*dom.Artist, error) {
	a, ok := r.artists[id]
	if !ok {
		return nil, dom.ErrArtistNotFound
	}
	return &a, nil
}

func (r *memRepo) ListArtists(ctx context.Context) ([]dom.Artist, error) {
	var out []dom.Artist
	for _, a := range r.artists {
		out = append(out, a)
	}
	return out, nil
}

func (r *memRepo) SetArtistMonitored(ctx context.Context, id int64, monitored bool) error {
	a, ok := r.artists[id]
	if !ok {
		return dom.ErrArtistNotFound
	}
	a.Monitored = monitored
	r.artists[id] = a
	return nil
}

func (r *memRepo) CreateAlbum(ctx context.Context, a dom.Album) (int64, error) {
	if _, ok := r.albums[a.ID]; ok {
		return 0, errors.New("duplicate album")
	}
	a.ID = r.nextAlbum
	r.nextAlbum++
	a.AddedAt = time.Now()
	r.albums[a.ID] = a
	return a.ID, nil
}

func (r *memRepo) GetAlbum(ctx context.Context, id int64) (*dom.Album, error) {
	a, ok := r.albums[id]
	if !ok {
		return nil, dom.ErrAlbumNotFound
	}
	return &a, nil
}

func (r *memRepo) ListAlbums(ctx context.Context, artistID int64) ([]dom.Album, error) {
	var out []dom.Album
	for _, a := range r.albums {
		if a.ArtistID == artistID {
			out = append(out, a)
		}
	}
	return out, nil
}

func (r *memRepo) SetAlbumMonitored(ctx context.Context, id int64, monitored bool) error {
	a, ok := r.albums[id]
	if !ok {
		return dom.ErrAlbumNotFound
	}
	a.Monitored = monitored
	r.albums[id] = a
	return nil
}

func (r *memRepo) CreateTrack(ctx context.Context, t dom.Track) (int64, error) {
	for _, v := range r.tracks[t.AlbumID] {
		if v.Disc == t.Disc && v.Number == t.Number {
			return 0, errors.New("duplicate track")
		}
	}
	t.ID = r.nextTrack
	r.nextTrack++
	t.AddedAt = time.Now()
	r.tracks[t.AlbumID] = append(r.tracks[t.AlbumID], t)
	return t.ID, nil
}

func (r *memRepo) GetTrack(ctx context.Context, albumID int64, disc, number int) (*dom.Track, error) {
	for _, t := range r.tracks[albumID] {
		if t.Disc == disc && t.Number == number {
			return &t, nil
		}
	}
	return nil, dom.ErrTrackNotFound
}

func (r *memRepo) ListTracks(ctx context.Context, albumID int64) ([]dom.Track, error) {
	return r.tracks[albumID], nil
}

func (r *memRepo) SetTrackMonitored(ctx context.Context, albumID int64, disc, number int, monitored bool) error {
	for i, t := range r.tracks[albumID] {
		if t.Disc == disc && t.Number == number {
			t.Monitored = monitored
			r.tracks[albumID][i] = t
			return nil
		}
	}
	return dom.ErrTrackNotFound
}

func (r *memRepo) EnsureWantedAlbum(ctx context.Context, albumID int64) error {
	k := wantedKey{album: albumID, track: 0}
	if _, ok := r.wanted[k]; !ok {
		r.wanted[k] = dom.Wanted{AlbumID: albumID, Status: dom.WantedPending, CreatedAt: time.Now()}
	}
	return nil
}

func (r *memRepo) EnsureWantedTrack(ctx context.Context, albumID, trackID int64) error {
	k := wantedKey{album: albumID, track: trackID}
	if _, ok := r.wanted[k]; !ok {
		r.wanted[k] = dom.Wanted{AlbumID: albumID, TrackID: trackID, Status: dom.WantedPending, CreatedAt: time.Now()}
	}
	return nil
}

func (r *memRepo) GetWanted(ctx context.Context, albumID, trackID int64) (*dom.Wanted, error) {
	w, ok := r.wanted[wantedKey{albumID, trackID}]
	if !ok {
		return nil, dom.ErrAlbumNotFound
	}
	return &w, nil
}

func (r *memRepo) ListWanted(ctx context.Context, albumID int64) ([]dom.Wanted, error) {
	var out []dom.Wanted
	for _, w := range r.wanted {
		if w.AlbumID == albumID {
			out = append(out, w)
		}
	}
	return out, nil
}

func (r *memRepo) MarkWantedSatisfied(ctx context.Context, albumID, trackID int64, releaseTitle string) error {
	k := wantedKey{albumID, trackID}
	w, ok := r.wanted[k]
	if !ok {
		return dom.ErrAlbumNotFound
	}
	now := time.Now()
	w.Status = dom.WantedSatisfied
	w.ReleaseTitle = releaseTitle
	w.SatisfiedAt = &now
	r.wanted[k] = w
	return nil
}

func (r *memRepo) CreateQueue(ctx context.Context, e dom.QueueEntry) (int64, error) {
	e.ID = r.nextQueue
	r.nextQueue++
	now := time.Now()
	e.CreatedAt = now
	e.UpdatedAt = now
	r.queue[e.ID] = e
	return e.ID, nil
}

func (r *memRepo) UpdateQueue(ctx context.Context, e dom.QueueEntry) error {
	q, ok := r.queue[e.ID]
	if !ok {
		return dom.ErrArtistNotFound
	}
	q.State = e.State
	q.Progress = e.Progress
	q.UpdatedAt = time.Now()
	r.queue[e.ID] = q
	return nil
}

func (r *memRepo) GetQueue(ctx context.Context, id int64) (*dom.QueueEntry, error) {
	e, ok := r.queue[id]
	if !ok {
		return nil, dom.ErrArtistNotFound
	}
	return &e, nil
}

func (r *memRepo) ListQueue(ctx context.Context, artistID int64) ([]dom.QueueEntry, error) {
	var out []dom.QueueEntry
	for _, e := range r.queue {
		if e.ArtistID == artistID {
			out = append(out, e)
		}
	}
	return out, nil
}

func (r *memRepo) AddHistory(ctx context.Context, e dom.HistoryEntry) error {
	e.ID = r.nextHist
	r.nextHist++
	e.At = time.Now()
	r.history = append(r.history, e)
	return nil
}

func (r *memRepo) ListHistory(ctx context.Context, artistID int64, limit int) ([]dom.HistoryEntry, error) {
	var out []dom.HistoryEntry
	for _, e := range r.history {
		if e.ArtistID == artistID {
			out = append(out, e)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

var _ dom.Repo = (*memRepo)(nil)

// albumDestPath builds the documented whole-album import destination so the
// tests can assert on the real layout:
//
//	root/<Artist>/<Album>/<Album>.<ext>
func albumDestPath(root, artist, album, ext string) string {
	return filepath.Join(root, artist, album, album+ext)
}

// TestPipeline_EndToEnd drives the full music pipeline: add artist → add
// monitored album (whole-album wanted) → search → lossless match → download →
// import (checksum-verified) → satisfied.
func TestPipeline_EndToEnd(t *testing.T) {
	dlDir := t.TempDir()
	mediaRoot := t.TempDir()
	mock, err := downloads.NewMockClient("mock", dlDir)
	if err != nil {
		t.Fatalf("mock client: %v", err)
	}
	fake := indexers.NewFakeIndexer("fake")
	// Two releases of the same album at different qualities; the lossless one
	// must win.
	fake.AddRelease(indexers.SearchResult{Title: "Bob Marley - Legend [FLAC Lossless]", SizeBytes: 4_200_000_000})
	fake.AddRelease(indexers.SearchResult{Title: "Bob Marley - Legend [MP3 320kbps]", SizeBytes: 1_100_000_000})

	repo := newMemRepo()
	svc := New(Deps{
		Repo:           repo,
		Indexers:       []indexers.Searcher{fake},
		Client:         mock,
		MediaRoot:      mediaRoot,
		DefaultProfile: "Lossless",
	})
	ctx := context.Background()

	// 1. Add a monitored artist + a monitored album (whole-album wanted row).
	artistID, err := svc.AddArtist(ctx, "Bob Marley", true)
	if err != nil {
		t.Fatalf("add artist: %v", err)
	}
	albumID, err := svc.AddAlbum(ctx, artistID, "Legend", 1977, "Lossless", true)
	if err != nil {
		t.Fatalf("add album: %v", err)
	}

	w, err := repo.GetWanted(ctx, albumID, 0)
	if err != nil {
		t.Fatalf("get wanted: %v", err)
	}
	if w.Status != dom.WantedPending {
		t.Errorf("wanted status = %q, want pending", w.Status)
	}

	// 2. Run the pipeline.
	imported, err := svc.RunPipeline(ctx, artistID)
	if err != nil {
		t.Fatalf("run pipeline: %v", err)
	}
	if imported != 1 {
		t.Fatalf("imported = %d, want 1", imported)
	}

	// 3. The whole-album file must exist at the documented layout (.flac for the
	// lossless win).
	wantPath := albumDestPath(mediaRoot, "Bob Marley", "Legend", ".flac")
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("imported file not on disk: %v", err)
	}

	// 4. The whole-album wanted is satisfied by the FLAC release.
	w, _ = repo.GetWanted(ctx, albumID, 0)
	if w.Status != dom.WantedSatisfied {
		t.Errorf("wanted status = %q, want satisfied", w.Status)
	}
	if w.ReleaseTitle != "Bob Marley - Legend [FLAC Lossless]" {
		t.Errorf("satisfied by = %q, want the FLAC release", w.ReleaseTitle)
	}

	// 5. Queue is complete.
	qs, _ := repo.ListQueue(ctx, artistID)
	if len(qs) != 1 {
		t.Fatalf("queue len = %d, want 1", len(qs))
	}
	if qs[0].State != dom.QueueComplete {
		t.Errorf("queue state = %q, want complete", qs[0].State)
	}

	// 6. Idempotency: no pending wanted remains, so a re-run is a no-op.
	if _, err := svc.RunPipeline(ctx, artistID); !errors.Is(err, ErrNoWanted) {
		t.Errorf("re-run pipeline: err = %v, want ErrNoWanted (already satisfied)", err)
	}
}

// TestPipeline_TrackWanted_SatisfiedByAlbum verifies a track-level wanted is
// satisfied when the whole album (which contains the track) is imported.
func TestPipeline_TrackWanted_SatisfiedByAlbum(t *testing.T) {
	dlDir := t.TempDir()
	mediaRoot := t.TempDir()
	mock, _ := downloads.NewMockClient("mock", dlDir)
	fake := indexers.NewFakeIndexer("fake")
	fake.AddRelease(indexers.SearchResult{Title: "Bob Marley - Legend [FLAC Lossless]", SizeBytes: 4_200_000_000})

	repo := newMemRepo()
	svc := New(Deps{
		Repo:           repo,
		Indexers:       []indexers.Searcher{fake},
		Client:         mock,
		MediaRoot:      mediaRoot,
		DefaultProfile: "Lossless",
	})
	ctx := context.Background()

	artistID, _ := svc.AddArtist(ctx, "Bob Marley", true)
	albumID, _ := svc.AddAlbum(ctx, artistID, "Legend", 1977, "Lossless", false)
	// Add a known track and monitor it (track-level wanted, not album-level).
	trackID, err := svc.AddTrack(ctx, albumID, 1, 1, "No Woman, No Man", true)
	if err != nil {
		t.Fatalf("add track: %v", err)
	}
	if trackID == 0 {
		t.Fatal("track id not assigned")
	}
	if _, err := repo.GetWanted(ctx, albumID, trackID); err != nil {
		t.Fatalf("track wanted row: %v", err)
	}

	imported, err := svc.RunPipeline(ctx, artistID)
	if err != nil {
		t.Fatalf("run pipeline: %v", err)
	}
	if imported != 1 {
		t.Fatalf("imported = %d, want 1 (album import satisfies the track want)", imported)
	}
	// The track-level wanted must now be satisfied by the album release.
	w, _ := repo.GetWanted(ctx, albumID, trackID)
	if w.Status != dom.WantedSatisfied {
		t.Errorf("track wanted status = %q, want satisfied", w.Status)
	}
}

// TestPipeline_AlbumMonitor_Acceptance is the album-granularity acceptance
// test:
//
//	add artist (monitored) → add two albums (one monitored, one not) → the
//	pipeline services exactly the monitored album
//
// The unmonitored album has no wanted row, so its (otherwise present) release
// must NOT be imported.
func TestPipeline_AlbumMonitor_Acceptance(t *testing.T) {
	dlDir := t.TempDir()
	mediaRoot := t.TempDir()
	mock, _ := downloads.NewMockClient("mock", dlDir)
	fake := indexers.NewFakeIndexer("fake")
	// A release for the monitored album...
	fake.AddRelease(indexers.SearchResult{Title: "Bob Marley - Legend [FLAC Lossless]", SizeBytes: 4_200_000_000})
	// ...and a distractor for the UNmonitored album that must be left alone.
	fake.AddRelease(indexers.SearchResult{Title: "Bob Marley - Uprising [FLAC Lossless]", SizeBytes: 4_200_000_000})

	repo := newMemRepo()
	svc := New(Deps{
		Repo:           repo,
		Indexers:       []indexers.Searcher{fake},
		Client:         mock,
		MediaRoot:      mediaRoot,
		DefaultProfile: "Lossless",
	})
	ctx := context.Background()

	artistID, _ := svc.AddArtist(ctx, "Bob Marley", true)
	legendID, _ := svc.AddAlbum(ctx, artistID, "Legend", 1977, "Lossless", true)
	uprisingID, _ := svc.AddAlbum(ctx, artistID, "Uprising", 1980, "Lossless", false)
	_ = uprisingID

	// Only the monitored album has a wanted row.
	if wants, _ := repo.ListWanted(ctx, legendID); len(wants) != 1 {
		t.Fatalf("Legend wanted len = %d, want 1", len(wants))
	}
	if wants, _ := repo.ListWanted(ctx, uprisingID); len(wants) != 0 {
		t.Fatalf("Uprising wanted len = %d, want 0 (unmonitored)", len(wants))
	}

	imported, err := svc.RunPipeline(ctx, artistID)
	if err != nil {
		t.Fatalf("run pipeline: %v", err)
	}
	if imported != 1 {
		t.Fatalf("imported = %d, want exactly 1 (only the monitored album)", imported)
	}

	// Legend is satisfied and its file is on disk.
	w, _ := repo.GetWanted(ctx, legendID, 0)
	if w.Status != dom.WantedSatisfied {
		t.Errorf("Legend status = %q, want satisfied", w.Status)
	}
	if _, err := os.Stat(albumDestPath(mediaRoot, "Bob Marley", "Legend", ".flac")); err != nil {
		t.Errorf("imported file Legend not on disk: %v", err)
	}
	// The unmonitored Uprising must NOT have been imported.
	if _, err := os.Stat(albumDestPath(mediaRoot, "Bob Marley", "Uprising", ".flac")); err == nil {
		t.Errorf("unmonitored Uprising was imported; it should have been left alone")
	}
}

// TestPipeline_ChecksumMismatch verifies that a bad checksum aborts the import:
// the file is removed, the queue entry is failed, and the wanted stays pending
// (not satisfied) so a future run can retry.
func TestPipeline_ChecksumMismatch(t *testing.T) {
	dlDir := t.TempDir()
	mediaRoot := t.TempDir()
	mock, _ := downloads.NewMockClient("mock", dlDir)
	fake := indexers.NewFakeIndexer("fake")
	fake.AddRelease(indexers.SearchResult{Title: "Bob Marley - Legend [FLAC Lossless]", SizeBytes: 4_200_000_000})

	repo := newMemRepo()
	svc := New(Deps{
		Repo:           repo,
		Indexers:       []indexers.Searcher{fake},
		Client:         mock,
		MediaRoot:      mediaRoot,
		DefaultProfile: "Lossless",
	})
	ctx := context.Background()

	artistID, _ := svc.AddArtist(ctx, "Bob Marley", true)
	albumID, _ := svc.AddAlbum(ctx, artistID, "Legend", 1977, "Lossless", true)
	// Set an (intentionally wrong) expected checksum on the album.
	album, _ := repo.GetAlbum(ctx, albumID)
	album.Checksum = "deadbeef"
	repo.albums[albumID] = *album

	if _, err := svc.RunPipeline(ctx, artistID); err == nil {
		t.Fatal("expected an error when the checksum mismatches")
	}
	// The file must not remain on disk (removed after a failed verification).
	if _, err := os.Stat(albumDestPath(mediaRoot, "Bob Marley", "Legend", ".flac")); err == nil {
		t.Errorf("imported file should have been removed after a checksum mismatch")
	}
	// The wanted must still be pending (retryable), not satisfied.
	w, _ := repo.GetWanted(ctx, albumID, 0)
	if w.Status != dom.WantedPending {
		t.Errorf("wanted status = %q after checksum mismatch, want pending", w.Status)
	}
	// The queue entry must be failed.
	qs, _ := repo.ListQueue(ctx, artistID)
	if len(qs) != 1 {
		t.Fatalf("queue len = %d, want 1", len(qs))
	}
	if qs[0].State != dom.QueueFailed {
		t.Errorf("queue state = %q, want failed", qs[0].State)
	}
}

// TestPipeline_NoMatch verifies that when no release satisfies the profile, the
// pipeline returns ErrNoMatch without importing or satisfying.
func TestPipeline_NoMatch(t *testing.T) {
	dlDir := t.TempDir()
	mediaRoot := t.TempDir()
	mock, _ := downloads.NewMockClient("mock", dlDir)
	fake := indexers.NewFakeIndexer("fake")
	// Only an MP3 release — a "Lossless" album requires a lossless format.
	fake.AddRelease(indexers.SearchResult{Title: "Bob Marley - Legend [MP3 128kbps]", SizeBytes: 800_000_000})

	repo := newMemRepo()
	svc := New(Deps{
		Repo:           repo,
		Indexers:       []indexers.Searcher{fake},
		Client:         mock,
		MediaRoot:      mediaRoot,
		DefaultProfile: "Lossless",
	})
	ctx := context.Background()

	artistID, _ := svc.AddArtist(ctx, "Bob Marley", true)
	albumID, _ := svc.AddAlbum(ctx, artistID, "Legend", 1977, "Lossless", true)

	_, err := svc.RunPipeline(ctx, artistID)
	if !errors.Is(err, ErrNoMatch) {
		t.Fatalf("run pipeline: err = %v, want ErrNoMatch", err)
	}
	w, _ := repo.GetWanted(ctx, albumID, 0)
	if w.Status != dom.WantedPending {
		t.Errorf("wanted status = %q, want pending after no-match", w.Status)
	}
	entries, _ := filepath.Glob(filepath.Join(mediaRoot, "*"))
	if len(entries) != 0 {
		t.Errorf("expected nothing imported, found %d entries", len(entries))
	}
}
