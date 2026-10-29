// Package wanted is the pure domain for the global Wanted view (P3): the
// Missing and Cutoff Unmet tabs that list, across all tracked series, the
// episodes the libraries still want. It owns the classification of an
// episode row into a tab, the P3-default cutoff resolution (the full
// profile-driven cutoff configuration lands in P5), and the shapes the
// Search All / Unmonitor / Manual Import actions operate on.
//
// A wanted row is always TV in this phase: the other kinds surface their own
// wanted state through their per-kind pages; the global table is the
// episode-shaped one (IA §2: series title · SxE · episode title · air date
// · status). No I/O lives here.
package wanted

import (
	"context"
	"errors"
	"strings"
	"time"

	tv "github.com/brandenk514/mediarr/internal/domains/tv"
)

// Errors surfaced by the wanted service.
var (
	// ErrUnknownTab is a tab other than missing/cutoff.
	ErrUnknownTab = errors.New("wanted: unknown tab")
	// ErrUnsupportedKind is a manual import for a non-TV kind (P3 is TV-only).
	ErrUnsupportedKind = errors.New("wanted: manual import supports tv only")
	// ErrSeriesNotFound is a manual import for an unknown series.
	ErrSeriesNotFound = errors.New("wanted: series not found")
	// ErrEpisodeNotFound is a manual import for an unknown episode.
	ErrEpisodeNotFound = errors.New("wanted: episode not found")
	// ErrFileNotFound is a manual import whose file does not exist.
	ErrFileNotFound = errors.New("wanted: file not found")
	// ErrPathOutsideRoot is a manual import whose file path is outside the
	// media root.
	ErrPathOutsideRoot = errors.New("wanted: file path is outside the media root")
	// ErrInvalidEpisode is a manual import with a non-positive season/episode.
	ErrInvalidEpisode = errors.New("wanted: season and episode must be positive")
	// ErrEmptyPath is a manual import with no file path.
	ErrEmptyPath = errors.New("wanted: file path is required")
)

// Tab names the two wanted sets.
type Tab string

const (
	// TabMissing is monitored episodes that have no file yet.
	TabMissing Tab = "missing"
	// TabCutoff is episodes whose current file quality is below the
	// series' cutoff.
	TabCutoff Tab = "cutoff"
)

// Valid reports whether t is a known tab.
func (t Tab) Valid() bool { return t == TabMissing || t == TabCutoff }

// Normalize returns the tab to use, defaulting an empty tab to missing.
func (t Tab) Normalize() Tab {
	if t == "" {
		return TabMissing
	}
	return t
}

// EpisodeRow is one candidate episode, as the repository reads it: the
// episode's own fields plus its series' title, its monitoring state, and its
// file state (HasFile + FileQuality, where FileQuality is the quality label
// of the satisfying release, "" when unknown or absent).
type EpisodeRow struct {
	SeriesID     int64
	SeriesTitle  string
	Season       int
	Episode      int
	EpisodeTitle string
	AirDate      *time.Time
	Monitored    bool
	HasFile      bool
	FileQuality  string
}

// Item is one row in the global wanted table.
type Item struct {
	Kind         string     `json:"kind"`
	SeriesID     int64      `json:"series_id"`
	SeriesTitle  string     `json:"series_title"`
	Season       int        `json:"season"`
	Episode      int        `json:"episode"`
	EpisodeTitle string     `json:"episode_title"`
	AirDate      *time.Time `json:"air_date"`
	Status       string     `json:"status"`

	// L4 music extras (kind=music; omitempty keeps the TV wire shape
	// byte-compatible). SeriesID/SeriesTitle carry the album id/title and
	// season/episode/episode_title carry the track (0/0/"" for album rows);
	// Quality is the imported quality string (cutoff-unmet rows).
	ArtistID    int64      `json:"artist_id,omitempty"`
	ArtistTitle string     `json:"artist_title,omitempty"`
	AlbumID     int64      `json:"album_id,omitempty"`
	AlbumTitle  string     `json:"album_title,omitempty"`
	AlbumType   string     `json:"album_type,omitempty"`
	ReleaseDate *time.Time `json:"release_date,omitempty"`
	Quality     string     `json:"quality,omitempty"`
}

// EpisodeRef identifies an episode within its series.
type EpisodeRef struct {
	SeriesID int64 `json:"series_id"`
	Season   int   `json:"season"`
	Episode  int   `json:"episode"`
}

// ManualImport is a manual import request for a wanted episode: import this
// local file as the episode's release.
type ManualImport struct {
	Kind         string `json:"kind"`
	SeriesID     int64  `json:"series_id"`
	Season       int    `json:"season"`
	Episode      int    `json:"episode"`
	FilePath     string `json:"file_path"`
	ReleaseTitle string `json:"release_title"`
	// L4 (kind=music): the album the file belongs to. 0 for TV requests;
	// FilePath is a single audio file (whole-album import) or a directory
	// of per-track files.
	AlbumID int64 `json:"album_id,omitempty"`
}

// ManualImportReport is the result of a manual import.
type ManualImportReport struct {
	Imported bool   `json:"imported"`
	Message  string `json:"message"`
}

// Filter narrows the classified rows before paging. Q is a
// case-insensitive substring match on the series title.
type Filter struct {
	Tab Tab
	Q   string
}

// CutoffItem resolves a series' effective cutoff quality item (P3 default):
// the profile's explicit cutoff item when the profile enables one, otherwise
// the profile's lowest item (items are ordered best-first) — the minimum
// quality we will accept. A nil result means "not evaluable" (no profile or
// no items): such a series never appears in the Cutoff Unmet tab. P5
// replaces this default with the full profile-driven cutoff configuration.
func CutoffItem(p tv.Profile) *tv.ProfileItem {
	if len(p.Items) == 0 {
		return nil
	}
	if p.UseCutoff {
		for i := range p.Items {
			if p.Items[i].ID == p.CutoffID {
				return &p.Items[i]
			}
		}
	}
	return &p.Items[len(p.Items)-1]
}

// resolutionRank mirrors tv.qualityRank (best-first: higher is better) so
// the wanted domain can compare quality labels without importing tv's
// internals. "any", "" and unknown labels rank 0 (not evaluable).
func resolutionRank(name string) int {
	for i := range tv.Resolutions {
		if tv.Resolutions[i].Name == name {
			return len(tv.Resolutions) - i
		}
	}
	return 0
}

// BelowCutoff reports whether a file of quality fileQuality is strictly
// below the cutoff item. Unparseable qualities on either side ("any", "",
// unknown) make the comparison unevaluable and return false — a legacy file
// with an unknown quality is not flagged as cutoff-unmet.
func BelowCutoff(fileQuality string, cut *tv.ProfileItem) bool {
	if cut == nil || cut.Quality == tv.CodecAny {
		return false
	}
	fr := resolutionRank(fileQuality)
	cr := resolutionRank(cut.Quality)
	if fr == 0 || cr == 0 {
		return false
	}
	return fr < cr
}

// Apply classifies the rows for the filter's tab, narrows by Q, and returns
// the matching items (input order preserved; the repository returns
// series-title, season, episode order). Cutoffs maps series id to that
// series' resolved cutoff item (nil/absent = not evaluable). The returned
// slice is always non-nil.
func (f Filter) Apply(rows []EpisodeRow, cutoffs map[int64]*tv.ProfileItem) []Item {
	t := f.Tab.Normalize()
	needle := strings.ToLower(strings.TrimSpace(f.Q))

	out := make([]Item, 0, len(rows))
	for _, r := range rows {
		if needle != "" && !strings.Contains(strings.ToLower(r.SeriesTitle), needle) {
			continue
		}
		item := Item{
			Kind:         "tv",
			SeriesID:     r.SeriesID,
			SeriesTitle:  r.SeriesTitle,
			Season:       r.Season,
			Episode:      r.Episode,
			EpisodeTitle: r.EpisodeTitle,
			AirDate:      r.AirDate,
		}
		switch t {
		case TabMissing:
			// Monitored and file-less. (Unmonitored episodes drop out of
			// the wanted set; episodes with a file belong to the
			// cutoff tab at most.)
			if !r.Monitored || r.HasFile {
				continue
			}
			item.Status = "missing:no file"
		case TabCutoff:
			// Monitored, has a file, and the file quality is below the
			// series' cutoff.
			if !r.Monitored || !r.HasFile {
				continue
			}
			cut, ok := cutoffs[r.SeriesID]
			if !ok || cut == nil || !BelowCutoff(r.FileQuality, cut) {
				continue
			}
			item.Status = "cutoff:" + r.FileQuality + " below cutoff " + cut.Quality
		default:
			return []Item{}
		}
		out = append(out, item)
	}
	return out
}

// Repo is the wanted view's persistence surface. The Postgres adapter
// implements it over the tv_* tables (episodes, series, wanted) plus the
// global media_history.
type Repo interface {
	// EpisodeRows returns every episode of every series, ordered by series
	// title, season, episode, each with its file state.
	EpisodeRows(ctx context.Context) ([]EpisodeRow, error)
	// SeriesProfiles maps series id to the series' quality-profile name.
	SeriesProfiles(ctx context.Context) (map[int64]string, error)
	// EnsureWantedPending inserts a pending wanted row for the episode, or
	// resets an existing satisfied row back to pending (re-requesting the
	// search). Pending rows are left untouched.
	EnsureWantedPending(ctx context.Context, seriesID int64, season, episode int) error
	// SetEpisodeMonitoredOff turns monitoring off for one episode and
	// reports whether a row was updated (false = unknown episode).
	SetEpisodeMonitoredOff(ctx context.Context, seriesID int64, season, episode int) (bool, error)
	// GetSeries loads one series (nil when it does not exist).
	GetSeries(ctx context.Context, id int64) (*SeriesRef, error)
	// GetEpisode loads one episode (nil when it does not exist).
	GetEpisode(ctx context.Context, seriesID int64, season, episode int) (*EpisodeRef, error)
	// AddHistory appends a global media-history event.
	AddHistory(ctx context.Context, kind string, parentID int64, event, detail string) error
	// SatisfyWanted marks the episode's wanted row satisfied by the given
	// release + quality. It is a no-op when no row exists.
	SatisfyWanted(ctx context.Context, seriesID int64, season, episode int, releaseTitle, quality string) error

	// L4 music (kind=music). The rows come from the music_* tables.
	// MusicRows returns the music wanted rows: one album-level row per
	// effectively-monitored album (artist AND album monitored) plus one
	// row per monitored track of such an album, ordered by artist name,
	// album (year, name), disc, number.
	MusicRows(ctx context.Context) ([]MusicRow, error)
	// EnsureMusicWanted inserts a pending music_wanted row for the ref
	// (the whole album when disc/number are zero, else the track), or
	// resets an existing satisfied row back to pending (re-requesting the
	// search). Pending rows are left untouched.
	EnsureMusicWanted(ctx context.Context, albumID int64, disc, number int) error
	// SetMusicMonitoredOff turns monitoring off for the music ref (whole
	// album when disc/number are zero, else the one track) and reports
	// whether a row was updated (false = unknown ref).
	SetMusicMonitoredOff(ctx context.Context, albumID int64, disc, number int) (bool, error)
	// AddMusicHistory appends a music_history event (kind "music" in the
	// global Activity feed) for the artist.
	AddMusicHistory(ctx context.Context, artistID int64, event, detail string) error
}

// SeriesRef is the series shape the wanted service needs (id, title, and the
// quality-profile name the cutoff is resolved from).
type SeriesRef struct {
	ID             int64
	Title          string
	QualityProfile string
}
