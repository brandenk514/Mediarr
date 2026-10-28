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
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	dom "github.com/brandenk514/mediarr/internal/domains/music"
	"github.com/brandenk514/mediarr/internal/downloads"
	"github.com/brandenk514/mediarr/internal/indexers"
	"github.com/brandenk514/mediarr/internal/metrics"
	"github.com/brandenk514/mediarr/internal/paging"
	"github.com/brandenk514/mediarr/internal/providers"
	"github.com/brandenk514/mediarr/internal/queue"
	"github.com/brandenk514/mediarr/internal/store"
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
	// Events, when non-nil, receives queue.state and history pub/sub events as
	// the pipeline advances (#40 live dashboard). It is optional so the
	// pipeline runs without a bus (e.g. tests).
	Events store.Events
	// Metrics, when non-nil, observes pipeline runs and imports (M7 #46).
	// Nil-safe: the pipeline runs identically without it.
	Metrics *metrics.Metrics
	// Metadata, when non-nil, enriches AddAlbum (release detail + tracks)
	// and RefreshArtist (artist detail, album re-fetch, cover art). It is
	// the keyless MusicBrainz + Cover Art Archive adapter (L4). Nil-safe:
	// enrichment is skipped with a warning.
	Metadata *providers.MusicBrainz
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

// AddArtistInput is the add-artist use-case input (Lidarr L1 grid + L2 add
// modal). Omitted optional fields keep pre-L1 behavior: an empty quality
// profile stores "none", an empty path uses the artist name (Lidarr
// creates a same-named subfolder under the root folder), an empty monitor
// policy stores "" (= all).
type AddArtistInput struct {
	Name             string
	MBID             string // MusicBrainz artist id ("" = unset, M6 #42)
	Monitored        bool
	QualityProfile   string   // "" = none
	Path             string   // "" = use the artist name
	MonitorPolicy    string   // PolicyAll/...; "" = all
	MonitorNewAlbums string   // "all_albums"; "" = all_albums
	Tags             []string // free-form tags; empty = none
}

// newArtist builds the domain artist from input, applying the L1 defaults:
// every artist we create is "continuing" (no metadata source we have —
// MusicBrainz — supplies a release status), the quality profile is kept as
// given ("" = none), and an omitted path falls back to the artist name.
// Rating/Genre/Links stay empty: the MusicBrainz search response carries
// neither (genre/urls/rating are a later-phase provider extension).
func newArtist(in AddArtistInput) dom.Artist {
	name := strings.TrimSpace(in.Name)
	artistPath := in.Path
	if artistPath == "" {
		artistPath = name
	}
	return dom.Artist{
		MBID:             in.MBID,
		Name:             name,
		Monitored:        in.Monitored,
		Status:           dom.StatusContinuing,
		QualityProfile:   in.QualityProfile,
		Path:             artistPath,
		MonitorPolicy:    in.MonitorPolicy,
		MonitorNewAlbums: in.MonitorNewAlbums,
		Tags:             in.Tags,
	}
}

// AddArtist creates an artist (if new) and records the add in history.
// Returns the artist id.
func (s *Service) AddArtist(ctx context.Context, in AddArtistInput) (int64, error) {
	if strings.TrimSpace(in.Name) == "" {
		return 0, fmt.Errorf("music: artist name is required")
	}
	a := newArtist(in)
	id, err := s.deps.Repo.CreateArtist(ctx, a)
	if err != nil {
		return 0, err
	}
	s.record(ctx, id, "added", "artist added to library")
	return id, nil
}

// AddAlbum creates a known album within an artist (if new) and, when the
// album is monitored, ensures a pending whole-album wanted row for it.
// Returns the album id and the enrichment warnings.
//
// monitored is nil when the caller did not request a value explicitly
// (Lidarr L2: the artist's monitor policy then decides — all/missing →
// true, none/existing → false, future → the release year is after the
// current year, first/last → it is the chronologically first/last known
// album of the artist); an explicit value always wins.
//
// releaseGroupMBID (L4) is an optional MusicBrainz release-group (or bare
// release) id. When present, the album and its tracks are populated from the
// keyless metadata adapter: year, album type, release date, total duration,
// labels, genres, the release cover, and one row per track (monitored with
// the album, with a wanted row each when monitored). A failed lookup never
// fails the add: the album is created un-enriched and the failure is
// returned in warnings. When absent, behavior is identical to L1/L2.
func (s *Service) AddAlbum(ctx context.Context, artistID int64, name string, year int, qualityProfile string, monitored *bool, releaseGroupMBID string) (int64, []string, error) {
	if strings.TrimSpace(name) == "" {
		return 0, nil, fmt.Errorf("music: album name is required")
	}
	artist, err := s.deps.Repo.GetArtist(ctx, artistID)
	if err != nil {
		return 0, nil, err
	}
	warnings := []string{}
	var detail *providers.ReleaseDetail
	if strings.TrimSpace(releaseGroupMBID) != "" {
		if s.deps.Metadata == nil {
			warnings = append(warnings, "metadata enrichment skipped: metadata provider not configured")
		} else if d, derr := s.deps.Metadata.ReleaseDetail(ctx, releaseGroupMBID); derr != nil {
			warnings = append(warnings, "metadata enrichment failed: "+derr.Error())
		} else {
			detail = d
		}
	}
	mon := s.resolveMonitored(ctx, artist, year, monitored)
	profile := qualityProfile
	if profile == "" {
		profile = s.deps.DefaultProfile
	}
	album := dom.Album{
		ArtistID:        artistID,
		Name:            strings.TrimSpace(name),
		Year:            year,
		QualityProfile:  profile,
		Monitored:       mon,
		VerifyChecksums: true,    // default on (PLAN §15.5); callers may override
		AlbumType:       "album", // L1: empty = album; the OpenAPI enum allows no empty value, so the service normalizes it (TV series_type convention)
		Labels:          []string{},
		Genres:          []string{},
	}
	if detail != nil {
		album.MBID = detail.MBID
		if detail.Year > 0 {
			album.Year = detail.Year
		}
		if detail.AlbumType != "" {
			album.AlbumType = detail.AlbumType
		}
		album.ReleaseDate = detail.ReleaseDate
		album.Duration = detail.TotalDuration()
		album.Labels = detail.Labels
		album.Genres = detail.Genre
		album.Cover = s.deps.Metadata.ReleaseCoverArt(ctx, detail.MBID)
	}
	id, err := s.deps.Repo.CreateAlbum(ctx, album)
	if err != nil {
		return 0, warnings, err
	}
	if mon {
		if err := s.deps.Repo.EnsureWantedAlbum(ctx, id); err != nil {
			return 0, warnings, err
		}
	}
	if detail != nil {
		for _, tr := range detail.Tracks {
			trackID, terr := s.deps.Repo.CreateTrack(ctx, dom.Track{
				AlbumID:   id,
				MBID:      tr.MBID,
				Disc:      tr.Disc,
				Number:    tr.Number,
				Title:     tr.Title,
				Duration:  tr.Duration,
				Monitored: mon,
			})
			if terr != nil {
				warnings = append(warnings, fmt.Sprintf("track %d:%d (%s) not created: %v", tr.Disc, tr.Number, tr.Title, terr))
				continue
			}
			if mon {
				if err := s.deps.Repo.EnsureWantedTrack(ctx, id, trackID); err != nil {
					warnings = append(warnings, fmt.Sprintf("track %d:%d (%s) not wanted: %v", tr.Disc, tr.Number, tr.Title, err))
				}
			}
		}
	}
	s.record(ctx, artistID, "album-added", fmt.Sprintf("%s (%d)", name, album.Year))
	return id, warnings, nil
}

// RefreshReport summarizes a RefreshArtist run.
type RefreshReport struct {
	AlbumsRefreshed int `json:"albums_refreshed"`
	AlbumsFailed    int `json:"albums_failed"`
}

// RefreshArtist re-fetches the artist's metadata from the keyless adapter —
// status, genres, links, and the artist cover art — and refreshes every
// known album that carries a release MBID: the album's metadata (year,
// album type, release date, duration, labels, genres, release cover) is
// replaced and its tracks are rebuilt from the release. Surviving tracks
// (matched by MBID, falling back to disc/number) keep their monitored flag;
// new monitored tracks get a wanted row. Best effort: a failed artist
// lookup leaves the artist unchanged, a failed album lookup is counted in
// the report, and a nil Metadata adapter (not configured) or an artist
// without an MBID returns the artist unchanged with a zero report.
func (s *Service) RefreshArtist(ctx context.Context, artistID int64) (*dom.Artist, *RefreshReport, error) {
	artist, err := s.deps.Repo.GetArtist(ctx, artistID)
	if err != nil {
		return nil, nil, err
	}
	report := &RefreshReport{}
	if s.deps.Metadata == nil || artist.MBID == "" {
		return artist, report, nil
	}

	// Artist metadata: status / genres / links / cover. A failed lookup
	// leaves the stored artist untouched (best effort, like L2 enrichment).
	if d, derr := s.deps.Metadata.GetArtistDetail(ctx, artist.MBID); derr == nil {
		if d.Status != "" {
			artist.Status = d.Status
		}
		if len(d.Genres) > 0 {
			artist.Genre = strings.Join(d.Genres, ", ")
		}
		if len(d.Links) > 0 {
			links := make([]dom.ArtistLink, 0, len(d.Links))
			for _, u := range d.Links {
				links = append(links, dom.ArtistLink{Name: "External", URL: u})
			}
			artist.Links = links
		}
		artist.Cover = s.deps.Metadata.ArtistCoverArt(ctx, artist.MBID)
		if err := s.deps.Repo.UpdateArtist(ctx, *artist); err != nil {
			return nil, report, err
		}
	}

	albums, err := s.deps.Repo.ListAlbums(ctx, artistID)
	if err != nil {
		return nil, report, err
	}
	for _, al := range albums {
		if al.MBID == "" {
			continue // no release id → nothing to re-fetch
		}
		d, derr := s.deps.Metadata.ReleaseDetail(ctx, al.MBID)
		if derr != nil {
			report.AlbumsFailed++
			continue
		}
		al.MBID = d.MBID
		if d.Year > 0 {
			al.Year = d.Year
		}
		if d.AlbumType != "" {
			al.AlbumType = d.AlbumType
		}
		al.ReleaseDate = d.ReleaseDate
		al.Duration = d.TotalDuration()
		al.Labels = d.Labels
		al.Genres = d.Genre
		al.Cover = s.deps.Metadata.ReleaseCoverArt(ctx, d.MBID)
		if err := s.deps.Repo.UpdateAlbum(ctx, al); err != nil {
			report.AlbumsFailed++
			continue
		}

		// Replace the tracks, preserving the monitored flag on survivors.
		oldTracks, err := s.deps.Repo.ListTracks(ctx, al.ID)
		if err != nil {
			report.AlbumsFailed++
			continue
		}
		monitored := map[string]bool{}
		for _, t := range oldTracks {
			if t.MBID != "" {
				monitored["mb:"+t.MBID] = t.Monitored
			}
			monitored[fmt.Sprintf("pos:%d:%d", t.Disc, t.Number)] = t.Monitored
		}
		if err := s.deps.Repo.DeleteTracks(ctx, al.ID); err != nil {
			report.AlbumsFailed++
			continue
		}
		rebuilt := true
		for _, tr := range d.Tracks {
			monT := al.Monitored
			if m, ok := monitored["mb:"+tr.MBID]; ok {
				monT = m
			} else if m, ok := monitored[fmt.Sprintf("pos:%d:%d", tr.Disc, tr.Number)]; ok {
				monT = m
			}
			trackID, terr := s.deps.Repo.CreateTrack(ctx, dom.Track{
				AlbumID:   al.ID,
				MBID:      tr.MBID,
				Disc:      tr.Disc,
				Number:    tr.Number,
				Title:     tr.Title,
				Duration:  tr.Duration,
				Monitored: monT,
			})
			if terr != nil {
				rebuilt = false
				break
			}
			if monT {
				_ = s.deps.Repo.EnsureWantedTrack(ctx, al.ID, trackID)
			}
		}
		if !rebuilt {
			report.AlbumsFailed++
			continue
		}
		report.AlbumsRefreshed++
	}

	fresh, err := s.deps.Repo.GetArtist(ctx, artistID)
	if err != nil {
		fresh = artist
	}
	s.record(ctx, artistID, "refreshed", fmt.Sprintf("artist + %d album(s) refreshed", report.AlbumsRefreshed))
	return fresh, report, nil
}

// ScanMusicLibrary (L4, Lidarr import phase 1) scans a root folder's
// immediate subfolders as artist candidates and matches them against the
// tracked artists. A folder with no subfolders and only audio files
// directly in it (a single-artist flat layout) is still a candidate: its
// files are counted. Phase 1 ends at the report — no file moves, no artist
// creation (the UI adds selected new artists through the add-artist flow).
func (s *Service) ScanMusicLibrary(ctx context.Context, root string) (*dom.MusicImportReport, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("music: import: root folder path is required")
	}
	clean := filepath.Clean(root)
	entries, err := os.ReadDir(clean)
	if err != nil {
		return nil, fmt.Errorf("music: import: read root folder %q: %w", root, err)
	}
	known, err := s.deps.Repo.ListArtists(ctx)
	if err != nil {
		return nil, err
	}
	report := &dom.MusicImportReport{Root: clean, Candidates: []dom.MusicImportCandidate{}}
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		title, _ := dom.ParseFolderName(ent.Name())
		cand := dom.MusicImportCandidate{
			Path:  filepath.Join(clean, ent.Name()),
			Title: title,
		}
		cand.Files, cand.Size = scanArtistDir(cand.Path)
		for _, ka := range known {
			if dom.CandidateMatchesArtist(cand.Title, ka) {
				cand.Matched = true
				cand.ArtistID = ka.ID
				break
			}
		}
		report.Scanned++
		report.Candidates = append(report.Candidates, cand)
	}
	return report, nil
}

// scanArtistDir counts the audio files (and their total size) under dir,
// recursively: the flat layout keeps files directly in the folder, the
// standard layout nests albums in subfolders.
func scanArtistDir(dir string) (files int, size int64) {
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if dom.IsAudioFile(d.Name()) {
			files++
			if info, ierr := d.Info(); ierr == nil {
				size += info.Size()
			}
		}
		return nil
	})
	return files, size
}

// resolveMonitored resolves the album's monitored flag: an explicit
// request wins; otherwise the artist's monitor policy (L2) decides.
// first/last compare the release year against the artist's known albums
// (the album being added included); a year of 0 is chronologically
// unknowable, so first/last fall back to unmonitored.
func (s *Service) resolveMonitored(ctx context.Context, artist *dom.Artist, year int, explicit *bool) bool {
	if explicit != nil {
		return *explicit
	}
	switch artist.MonitorPolicy {
	case dom.PolicyAll, dom.PolicyMissing, "":
		return true
	case dom.PolicyNone, dom.PolicyExisting:
		return false
	case dom.PolicyFuture:
		return year > time.Now().UTC().Year()
	case dom.PolicyFirst, dom.PolicyLast:
		if year <= 0 {
			return false
		}
		albums, err := s.deps.Repo.ListAlbums(ctx, artist.ID)
		if err != nil {
			return false
		}
		minY, maxY := year, year
		for _, a := range albums {
			if a.Year <= 0 {
				continue // chronologically unknowable
			}
			if a.Year < minY {
				minY = a.Year
			}
			if a.Year > maxY {
				maxY = a.Year
			}
		}
		if artist.MonitorPolicy == dom.PolicyFirst {
			return year == minY
		}
		return year == maxY
	default:
		return true
	}
}

// UpdateArtistInput is the Edit Artist modal input (Lidarr L2). Pointer
// fields mean "apply this value"; nil means "keep the current value" — the
// L1 monitored toggle sends only Monitored and must not clobber the other
// fields. When Monitored is applied and differs from the current state it
// fans out to the artist's known albums (the L1 artist-level toggle).
type UpdateArtistInput struct {
	Monitored        *bool
	MonitorPolicy    *string
	MonitorNewAlbums *string
	QualityProfile   *string
	Path             *string
	Tags             *[]string
}

// UpdateArtist applies the Edit Artist modal fields to one artist and, when
// the monitored state changed, propagates it to every known album (the L1
// artist-level fan-out). It returns the updated artist (the API responds
// with its monitored state).
func (s *Service) UpdateArtist(ctx context.Context, id int64, in UpdateArtistInput) (*dom.Artist, error) {
	cur, err := s.deps.Repo.GetArtist(ctx, id)
	if err != nil {
		return nil, err
	}
	a := *cur
	if in.Monitored != nil {
		a.Monitored = *in.Monitored
	}
	if in.MonitorPolicy != nil {
		a.MonitorPolicy = *in.MonitorPolicy
	}
	if in.MonitorNewAlbums != nil {
		a.MonitorNewAlbums = *in.MonitorNewAlbums
	}
	if in.QualityProfile != nil {
		a.QualityProfile = *in.QualityProfile
	}
	if in.Path != nil {
		a.Path = *in.Path
	}
	if in.Tags != nil {
		a.Tags = *in.Tags
	}
	if err := s.deps.Repo.UpdateArtist(ctx, a); err != nil {
		return nil, err
	}
	if in.Monitored != nil && *in.Monitored != cur.Monitored {
		albums, err := s.deps.Repo.ListAlbums(ctx, id)
		if err != nil {
			return nil, err
		}
		for _, al := range albums {
			if err := s.SetAlbumMonitored(ctx, al.ID, *in.Monitored); err != nil {
				return nil, err
			}
		}
		event := "monitored"
		if !*in.Monitored {
			event = "unmonitored"
		}
		s.record(ctx, id, event, "artist-level toggle")
	}
	return &a, nil
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

// ListArtistsQuery returns a server-side filtered and paginated page of
// artists (M6 #41). The music list view expands an artist to its albums and
// tracks lazily; the page envelope applies to the artist level.
func (s *Service) ListArtistsQuery(ctx context.Context, q paging.Query) (paging.Page[dom.Artist], error) {
	all, err := s.deps.Repo.ListArtists(ctx)
	if err != nil {
		return paging.Page[dom.Artist]{}, err
	}
	return paging.Apply(all, q, func(a dom.Artist) (string, bool) {
		return a.Name, a.Monitored
	}), nil
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

// GetArtistHistory returns the artist's history entries (newest first), for
// GET /api/v1/music/artists/{id}/history. A missing artist is surfaced via
// ErrArtistNotFound (the handler maps it to 404, like the other per-artist
// music endpoints).
func (s *Service) GetArtistHistory(ctx context.Context, artistID int64, limit int) ([]dom.HistoryEntry, error) {
	if _, err := s.deps.Repo.GetArtist(ctx, artistID); err != nil {
		return nil, err
	}
	return s.deps.Repo.ListHistory(ctx, artistID, limit)
}

// LibrarySummary returns the library-wide footer counts (L1 status bar).
func (s *Service) LibrarySummary(ctx context.Context) (dom.LibrarySummary, error) {
	return s.deps.Repo.LibrarySummary(ctx)
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
func (s *Service) RunPipeline(ctx context.Context, artistID int64) (imported int, err error) {
	start := time.Now()
	defer func() {
		result := metrics.ResultOK
		if err != nil {
			result = metrics.ResultError
		}
		s.deps.Metrics.RecordPipeline(string(queue.KindMusic), result, time.Since(start), imported)
	}()

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
		// L4 Activity display columns (Lidarr queue row). Whole-album row:
		// quality/formats carry the matched format; TrackTitle is "".
		AlbumTitle: album.Name,
		Quality:    dom.DisplayQuality(rel.Quality),
		Formats:    dom.DisplayFormats(rel.Quality.Format),
		TrackTitle: "",
	}
	id, err := s.deps.Repo.CreateQueue(ctx, qe)
	if err != nil {
		return 0, err
	}
	qe.ID = id
	s.emitQueue(ctx, qe)
	qe.State = dom.QueueDownloading
	_ = s.deps.Repo.UpdateQueue(ctx, qe)
	s.emitQueue(ctx, qe)

	if err := s.deps.Client.Add(ctx, downloads.Release{
		Title:    rel.Title,
		FileName: fileNameFor(album, rel),
	}); err != nil {
		qe.State = dom.QueueFailed
		qe.Progress = 0
		_ = s.deps.Repo.UpdateQueue(ctx, qe)
		s.emitQueue(ctx, qe)
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
			s.emitQueue(ctx, qe)
			return fmt.Errorf("music: import: %w", err)
		}
	}

	// Remove the source file now that it has been imported.
	_ = os.Remove(file)

	qe := dom.QueueEntry{ID: queueID, State: dom.QueueComplete, Progress: 100}
	if err := s.deps.Repo.UpdateQueue(ctx, qe); err != nil {
		return err
	}
	s.emitQueue(ctx, qe)
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

// emitQueue publishes a "queue.updated" pub/sub event for a queue entry. It is
// a no-op when no bus is configured and never fails the pipeline: a dead Redis
// must not stop a download, so publish errors are deliberately ignored here
// (the queue table is the source of truth; the bus is a best-effort push).
func (s *Service) emitQueue(ctx context.Context, qe dom.QueueEntry) {
	if s.deps.Events == nil {
		return
	}
	payload, err := json.Marshal(map[string]any{
		"artist_id":     qe.ArtistID,
		"album_id":      qe.AlbumID,
		"queue_id":      qe.ID,
		"release_title": qe.ReleaseTitle,
		"indexer":       qe.Indexer,
		"client":        qe.DownloadClient,
		"state":         qe.State,
		"progress":      qe.Progress,
	})
	if err != nil {
		return
	}
	_ = s.deps.Events.Publish(ctx, store.Event{Type: "queue.updated", Payload: payload})
}

// record appends a history entry, ignoring errors (history is best-effort),
// and emits a "history.appended" pub/sub event so the live dashboard's history
// feed refreshes (the queue is the source of truth; the bus is best-effort).
func (s *Service) record(ctx context.Context, artistID int64, event, detail string) {
	_ = s.deps.Repo.AddHistory(ctx, dom.HistoryEntry{
		ArtistID: artistID, Event: event, Detail: detail,
	})
	if s.deps.Events == nil {
		return
	}
	payload, err := json.Marshal(map[string]any{
		"artist_id": artistID,
		"event":     event,
		"detail":    detail,
	})
	if err != nil {
		return
	}
	_ = s.deps.Events.Publish(ctx, store.Event{Type: "history.appended", Payload: payload})
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
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil { //nolint:gosec // media tree dirs must stay group/world-readable for media-server users (Plex, etc.)
		return err
	}
	data, err := os.ReadFile(src) //nolint:gosec // src is the download client's output path, built by the pipeline
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644) //nolint:gosec // media files must be world-readable for media-server users
}
