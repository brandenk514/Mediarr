// Package music (services) is the use-case layer for the music domain. It
// orchestrates the full download pipeline — add artist → add album/track → want
// (at album or track granularity) → search → lossless quality match → pick best
// → download → import (checksum-verified) → satisfy — against the pure domain
// (domains/music) and the swappable adapter interfaces (indexers, downloads).
// It depends on interfaces, never on Postgres/Redis concretely, so the pipeline
// is testable end-to-end with fakes and a real Postgres repository alike (the
// same pattern as services/tv and services/movies).
package music

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	dom "github.com/brandenk514/mediarr/internal/domains/music"
	"github.com/brandenk514/mediarr/internal/downloads"
	"github.com/brandenk514/mediarr/internal/indexers"
)

// ErrNoMatch is returned when no indexer release satisfies a wanted album's
// quality profile.
var ErrNoMatch = fmt.Errorf("music: no release matched the quality profile")

// ErrNoWanted is returned when an artist has no pending wanted albums/tracks to
// service (nothing to do).
var ErrNoWanted = fmt.Errorf("music: no pending wanted albums or tracks")

// ErrArtistNotFound, ErrAlbumNotFound, and ErrTrackNotFound are re-exported
// from the pure domain so API/transport layers can map them to 404 without
// importing the domain directly.
var (
	ErrArtistNotFound = dom.ErrArtistNotFound
	ErrAlbumNotFound  = dom.ErrAlbumNotFound
	ErrTrackNotFound  = dom.ErrTrackNotFound
)

// Deps bundles the Service's collaborators. Everything is an interface (or the
// pure domain), so the service is testable with a fake indexer, a mock download
// client, and either a real Postgres repo or an in-memory fake.
type Deps struct {
	// Repo persists artists, albums, tracks, wanted, queue, and history.
	Repo dom.Repo
	// Indexers are searched (fan-out) for candidate releases.
	Indexers []indexers.Searcher
	// Client is the download client that fetches the chosen release.
	Client downloads.Client
	// MediaRoot is where imported files are placed (e.g. /media/music).
	MediaRoot string
	// DefaultProfile is used when an album has no profile set.
	DefaultProfile string
}

// Service implements the music use-cases.
type Service struct {
	deps Deps
}

// New builds a Service.
func New(deps Deps) *Service {
	if deps.DefaultProfile == "" {
		deps.DefaultProfile = "Lossless"
	}
	return &Service{deps: deps}
}

// AddArtist creates an artist (if new) and records the add in history. The
// monitored flag sets the artist-level monitor toggle (PLAN §4): a monitored
// artist is wanted by default; an unmonitored artist is added quietly.
// Returns the artist id.
func (s *Service) AddArtist(ctx context.Context, name string, monitored bool) (int64, error) {
	if strings.TrimSpace(name) == "" {
		return 0, fmt.Errorf("music: artist name is required")
	}
	a := dom.Artist{
		Name:      strings.TrimSpace(name),
		Monitored: monitored,
	}
	id, err := s.deps.Repo.CreateArtist(ctx, a)
	if err != nil {
		return 0, err
	}
	s.record(ctx, id, "added", "artist added to library")
	return id, nil
}

// AddAlbum creates a known album within an artist (if new) and, when the album
// is monitored, ensures a pending whole-album wanted row for it. Returns the
// album id.
func (s *Service) AddAlbum(ctx context.Context, artistID int64, name string, year int, qualityProfile string, monitored bool) (int64, error) {
	if strings.TrimSpace(name) == "" {
		return 0, fmt.Errorf("music: album name is required")
	}
	profile := qualityProfile
	if profile == "" {
		profile = s.deps.DefaultProfile
	}
	album := dom.Album{
		ArtistID:        artistID,
		Name:            strings.TrimSpace(name),
		Year:            year,
		QualityProfile:  profile,
		Monitored:       monitored,
		VerifyChecksums: true, // default on (PLAN §15.5); callers may override
	}
	id, err := s.deps.Repo.CreateAlbum(ctx, album)
	if err != nil {
		return 0, err
	}
	if monitored {
		if err := s.deps.Repo.EnsureWantedAlbum(ctx, id); err != nil {
			return 0, err
		}
	}
	s.record(ctx, artistID, "album-added", fmt.Sprintf("%s (%d)", name, year))
	return id, nil
}

// AddTrack creates a known track within an album (if new) and, when the track
// is monitored, ensures a pending track-level wanted row for it. Returns the
// track id.
func (s *Service) AddTrack(ctx context.Context, albumID int64, disc, number int, title string, monitored bool) (int64, error) {
	t := dom.Track{
		AlbumID:   albumID,
		Disc:      disc,
		Number:    number,
		Title:     strings.TrimSpace(title),
		Monitored: monitored,
	}
	id, err := s.deps.Repo.CreateTrack(ctx, t)
	if err != nil {
		return 0, err
	}
	if monitored {
		if err := s.deps.Repo.EnsureWantedTrack(ctx, albumID, id); err != nil {
			return 0, err
		}
	}
	return id, nil
}

// ListArtists returns all artists (used by GET /api/v1/music).
func (s *Service) ListArtists(ctx context.Context) ([]dom.Artist, error) {
	return s.deps.Repo.ListArtists(ctx)
}

// GetArtist loads an artist (used by GET /api/v1/music/{id}).
func (s *Service) GetArtist(ctx context.Context, id int64) (*dom.Artist, error) {
	return s.deps.Repo.GetArtist(ctx, id)
}

// ListAlbums returns an artist's known albums (used by
// GET /api/v1/music/{id}/albums).
func (s *Service) ListAlbums(ctx context.Context, artistID int64) ([]dom.Album, error) {
	return s.deps.Repo.ListAlbums(ctx, artistID)
}

// GetAlbum loads an album (used by GET /api/v1/music/albums/{id}).
func (s *Service) GetAlbum(ctx context.Context, id int64) (*dom.Album, error) {
	return s.deps.Repo.GetAlbum(ctx, id)
}

// ListTracks returns an album's known tracks (used by
// GET /api/v1/music/albums/{id}/tracks).
func (s *Service) ListTracks(ctx context.Context, albumID int64) ([]dom.Track, error) {
	return s.deps.Repo.ListTracks(ctx, albumID)
}

// ListWanted returns an album's wanted entries (album-level and track-level)
// with their status (used by GET /api/v1/music/albums/{id}/wanted).
func (s *Service) ListWanted(ctx context.Context, albumID int64) ([]dom.Wanted, error) {
	return s.deps.Repo.ListWanted(ctx, albumID)
}

// SetArtistMonitored toggles the artist-level monitor (PLAN §4) and propagates
// it to every known album of the artist, ensuring a pending whole-album wanted
// row for each on toggle-on. The wanted rows are the pipeline's work queue.
func (s *Service) SetArtistMonitored(ctx context.Context, artistID int64, monitored bool) error {
	if err := s.deps.Repo.SetArtistMonitored(ctx, artistID, monitored); err != nil {
		return err
	}
	albums, err := s.deps.Repo.ListAlbums(ctx, artistID)
	if err != nil {
		return err
	}
	for _, a := range albums {
		if err := s.SetAlbumMonitored(ctx, a.ID, monitored); err != nil {
			return err
		}
	}
	event := "monitored"
	if !monitored {
		event = "unmonitored"
	}
	s.record(ctx, artistID, event, "artist-level toggle")
	return nil
}

// SetAlbumMonitored toggles a single album's monitoring; when turning on, it
// ensures a pending whole-album wanted row (so a re-monitored album is wanted
// again).
func (s *Service) SetAlbumMonitored(ctx context.Context, albumID int64, monitored bool) error {
	if err := s.deps.Repo.SetAlbumMonitored(ctx, albumID, monitored); err != nil {
		return err
	}
	if monitored {
		return s.deps.Repo.EnsureWantedAlbum(ctx, albumID)
	}
	return nil
}

// SetTrackMonitored toggles a single track's monitoring; when turning on, it
// ensures a pending track-level wanted row.
func (s *Service) SetTrackMonitored(ctx context.Context, albumID int64, disc, number int, monitored bool) error {
	if err := s.deps.Repo.SetTrackMonitored(ctx, albumID, disc, number, monitored); err != nil {
		return err
	}
	if !monitored {
		return nil
	}
	t, err := s.deps.Repo.GetTrack(ctx, albumID, disc, number)
	if err != nil {
		return err
	}
	return s.deps.Repo.EnsureWantedTrack(ctx, albumID, t.ID)
}

// RunPipeline drives the full pipeline for one artist: it services every
// pending wanted album/track by searching indexers, matching the best lossless
// release, downloading it, importing it under the media root (checksum-verified
// per the per-library opt-out), and marking the wanted entries satisfied.
//
// A whole-album release satisfies the album-level wanted and every track-level
// wanted within that album (the album file contains each track). The pipeline
// is idempotent: wanted entries already satisfied are skipped, and an artist
// with no pending wanted entries returns ErrNoWanted without searching.
func (s *Service) RunPipeline(ctx context.Context, artistID int64) (int, error) {
	artist, err := s.deps.Repo.GetArtist(ctx, artistID)
	if err != nil {
		return 0, err
	}

	// Collect the pending wanted per album.
	albums, err := s.deps.Repo.ListAlbums(ctx, artistID)
	if err != nil {
		return 0, err
	}
	type albumWork struct {
		album   dom.Album
		pending []dom.Wanted
	}
	var work []albumWork
	for _, a := range albums {
		wants, err := s.deps.Repo.ListWanted(ctx, a.ID)
		if err != nil {
			return 0, err
		}
		var pending []dom.Wanted
		for _, w := range wants {
			if w.Status == dom.WantedPending {
				pending = append(pending, w)
			}
		}
		if len(pending) > 0 {
			work = append(work, albumWork{album: a, pending: pending})
		}
	}
	if len(work) == 0 {
		return 0, ErrNoWanted
	}

	if s.deps.Client == nil {
		s.record(ctx, artistID, "no-client", "no download client configured")
		return 0, fmt.Errorf("music: no download client configured")
	}

	// 1. Search (once for the artist).
	results, err := s.searchAll(ctx, artist.Name, 0)
	if err != nil {
		s.record(ctx, artistID, "search", "search failed: "+err.Error())
		return 0, err
	}
	s.record(ctx, artistID, "searched", fmt.Sprintf("%d candidate release(s)", len(results)))

	// 2. Service each album that has pending wanted entries.
	imported := 0
	for _, w := range work {
		profile := w.album.QualityProfile
		if profile == "" {
			profile = s.deps.DefaultProfile
		}
		cands := s.buildCandidates(artist, w.album, profile, results)
		best, ok := dom.BestForAlbum(cands, artist.Name, w.album.Name)
		if !ok {
			s.record(ctx, artistID, "no-match",
				fmt.Sprintf("no release satisfied %q (%s)", w.album.Name, profile))
			continue
		}
		n, err := s.serviceAlbum(ctx, artist, w.album, best, w.pending)
		if err != nil {
			s.record(ctx, artistID, "import-failed", err.Error())
			continue
		}
		imported += n
	}
	if imported == 0 {
		return 0, ErrNoMatch
	}
	return imported, nil
}

// searchAll fans out to every indexer concurrently (via the shared M5 Fanout),
// merging and deduping by release title (first indexer wins). The domain pickers
// re-score and re-order the candidates, so the merged order does not affect which
// release is chosen.
func (s *Service) searchAll(ctx context.Context, term string, year int) ([]indexers.SearchResult, error) {
	q := indexers.SearchQuery{Term: term, Year: year}
	f := indexers.NewFanout(s.deps.Indexers...)
	return f.Search(ctx, q)
}

// buildCandidates parses and lossless-matches every indexer result against the
// album's profile, dropping releases that do not parse or satisfy it.
func (s *Service) buildCandidates(artist *dom.Artist, album dom.Album, profile string, results []indexers.SearchResult) []dom.CandidateRelease {
	var cands []dom.CandidateRelease
	for _, r := range results {
		c, ok := dom.BuildCandidate(r.Title, r.Indexer, profile)
		if !ok {
			continue
		}
		cands = append(cands, c)
	}
	return cands
}

// serviceAlbum downloads the best whole-album release once, imports it under
// the media root (checksum-verified when the album opts in), and satisfies
// every pending wanted entry the release delivers (the album level plus any
// track-level wanted within the album). Returns the number of wanted entries
// satisfied.
func (s *Service) serviceAlbum(ctx context.Context, artist *dom.Artist, album dom.Album, rel dom.CandidateRelease, pending []dom.Wanted) (int, error) {
	// Download once.
	queueID, err := s.sendToClient(ctx, artist, album, rel)
	if err != nil {
		return 0, err
	}

	// Import the whole-album file (checksum-verified).
	if err := s.importAlbum(ctx, artist, album, rel, queueID); err != nil {
		return 0, err
	}

	// Satisfy every pending wanted the release delivers.
	satisfied := 0
	for _, w := range pending {
		if err := s.deps.Repo.MarkWantedSatisfied(ctx, w.AlbumID, w.TrackID, rel.Title); err != nil {
			s.record(ctx, artist.ID, "satisfy-failed", err.Error())
			continue
		}
		satisfied++
	}
	s.record(ctx, artist.ID, "imported",
		fmt.Sprintf("imported %q satisfying %d wanted", rel.Title, satisfied))
	return satisfied, nil
}

// sendToClient queues the release, sends it to the download client, and records
// the queue entry. Returns the queue entry id.
func (s *Service) sendToClient(ctx context.Context, artist *dom.Artist, album dom.Album, rel dom.CandidateRelease) (int64, error) {
	qe := dom.QueueEntry{
		ArtistID:       artist.ID,
		AlbumID:        album.ID,
		ReleaseTitle:   rel.Title,
		Indexer:        rel.Indexer,
		DownloadClient: s.deps.Client.Name(),
		State:          dom.QueueQueued,
		Progress:       0,
	}
	id, err := s.deps.Repo.CreateQueue(ctx, qe)
	if err != nil {
		return 0, err
	}
	qe.ID = id
	qe.State = dom.QueueDownloading
	_ = s.deps.Repo.UpdateQueue(ctx, qe)

	if err := s.deps.Client.Add(ctx, downloads.Release{
		Title:    rel.Title,
		FileName: fileNameFor(album, rel),
	}); err != nil {
		qe.State = dom.QueueFailed
		_ = s.deps.Repo.UpdateQueue(ctx, qe)
		return 0, err
	}
	return id, nil
}

// importAlbum polls the client until the release is complete, imports the
// whole-album file under the media root, verifies its checksum when the album
// opts in (PLAN §15.5), and marks the queue complete. A checksum mismatch
// removes the bad file, marks the queue failed, and is surfaced as an error so
// the wanted entries are NOT satisfied.
func (s *Service) importAlbum(ctx context.Context, artist *dom.Artist, album dom.Album, rel dom.CandidateRelease, queueID int64) error {
	file, err := s.waitComplete(ctx, rel.Title)
	if err != nil {
		return err
	}
	if file == "" {
		return fmt.Errorf("music: download did not produce a file for %q", rel.Title)
	}

	plan, err := dom.PlanAlbumImport(s.deps.MediaRoot, artist.Name, album.Name, file)
	if err != nil {
		if !errors.Is(err, dom.ErrCollision) {
			return err
		}
		// The album file already exists; reuse its destination in place.
		dest, derr := dom.AlbumDestPath(s.deps.MediaRoot, artist.Name, album.Name, file)
		if derr != nil {
			return derr
		}
		plan = &dom.Import{DestPath: dest}
	}

	// Copy the file into place unless it is already there.
	if _, statErr := os.Stat(plan.DestPath); os.IsNotExist(statErr) {
		if err := copyFile(plan.SourcePath, plan.DestPath); err != nil {
			return err
		}
	}

	// Checksum verification (on by default, opt-out per library).
	if album.VerifyChecksums && strings.TrimSpace(album.Checksum) != "" {
		if err := dom.VerifyChecksum(plan.DestPath, album.Checksum); err != nil {
			_ = os.Remove(plan.DestPath) // don't keep a file we can't trust
			qe := dom.QueueEntry{ID: queueID, State: dom.QueueFailed}
			_ = s.deps.Repo.UpdateQueue(ctx, qe)
			return fmt.Errorf("music: import: %w", err)
		}
	}

	// Remove the source file now that it has been imported.
	_ = os.Remove(file)

	qe := dom.QueueEntry{ID: queueID, State: dom.QueueComplete, Progress: 100}
	if err := s.deps.Repo.UpdateQueue(ctx, qe); err != nil {
		return err
	}
	return nil
}

// waitComplete polls the download client until the release reports complete
// (bounded by a deadline) and returns the file path.
func (s *Service) waitComplete(ctx context.Context, title string) (string, error) {
	deadline := time.Now().Add(30 * time.Second)
	for {
		st, err := s.deps.Client.Status(ctx, title)
		if err != nil {
			return "", err
		}
		if st.Complete && st.File != "" {
			return st.File, nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("music: timed out waiting for download of %q", title)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// record appends a history entry, ignoring errors (history is best-effort).
func (s *Service) record(ctx context.Context, artistID int64, event, detail string) {
	_ = s.deps.Repo.AddHistory(ctx, dom.HistoryEntry{
		ArtistID: artistID, Event: event, Detail: detail,
	})
}

// fileNameFor derives a safe download-client file name from an album and a
// parsed release: a cleaned album name plus an audio extension inferred from
// the release's format.
func fileNameFor(album dom.Album, rel dom.CandidateRelease) string {
	name := album.Name
	if name == "" {
		name = rel.Title
	}
	if name == "" {
		name = "album"
	}
	name = strings.ReplaceAll(name, ".", " ")
	name = strings.ReplaceAll(name, "-", " ")
	name = strings.Join(strings.Fields(name), " ")
	return strings.ToLower(name) + extForFormat(rel.Quality.Format)
}

func extForFormat(format string) string {
	switch format {
	case dom.FormatFLAC:
		return ".flac"
	case dom.FormatAPE:
		return ".ape"
	case dom.FormatWAV:
		return ".wav"
	case dom.FormatM4A:
		return ".m4a"
	case dom.FormatOpus:
		return ".opus"
	default:
		return ".mp3"
	}
}

// copyFile copies src to dst, creating dst's directory as needed.
func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}
