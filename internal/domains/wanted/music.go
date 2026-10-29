// music.go carries the L4 (Lidarr) music wanted shapes: the row the
// repository reads for the music Missing / Cutoff Unmet tabs, the L3
// profile-driven cutoff resolution, and the shapes the music actions
// (Search All / Unmonitor Selected / Manual Import) operate on.
//
// The cutoff rule (L3, per the parity plan): an album's effective cutoff is
// the cutoff level of the album's QualityProfile name resolved against the
// seeded music profiles (settings.DefaultMusicProfiles), falling back to the
// DefaultMusicQualityProfile profile when the name is empty or unknown. An
// imported track is cutoff-unmet when its quality level is BELOW the cutoff
// level in the seeded ladder's best-first order; an unknown/"quality" is
// below every level.
package wanted

import (
	"strings"
	"time"

	musdom "github.com/brandenk514/mediarr/internal/domains/music"
	"github.com/brandenk514/mediarr/internal/settings"
)

// MusicRow is one candidate music row, as the repository reads it: an
// effectively-monitored album (one album-level row per album) or one of its
// monitored tracks (track-level rows), plus the file state and the L3
// cutoff-resolution input.
type MusicRow struct {
	ArtistID    int64
	ArtistTitle string
	AlbumID     int64
	AlbumTitle  string
	AlbumType   string // album / ep / single / ...; "" = album
	Year        int
	// ReleaseDate is the enriched catalog release date; nil when unknown.
	ReleaseDate *time.Time
	// ProfileName is the album's effective quality profile name; "" = none
	// (resolves to the default profile per the L3 rule).
	ProfileName string
	// Monitored is the effective monitor flag (artist AND album monitored).
	Monitored bool
	// Track-level row identity: Disc/Number/TrackTitle are 0/0/"" on
	// album-level rows; a track row carries the track (TrackID is the DB id
	// the search-all wanted row uses).
	Disc           int
	Number         int
	TrackID        int64
	TrackTitle     string
	TrackMonitored bool
	// Album-level file state (computed, mirroring the library's badges).
	TrackCount int // known tracks in the album
	FileCount  int // tracks with an imported file
	// ImportedQualities are the imported tracks' quality strings (codec
	// names, e.g. "FLAC"); the album is cutoff-unmet when any is below the
	// effective cutoff. Track rows use TrackStatus instead.
	ImportedQualities []string
	// TrackStatus is the track row's imported quality string; "" = no file.
	TrackStatus string
}

// MusicItem is not a separate wire shape: the music row reuses Item with the
// music extras (below) and the episode-shaped fields carrying the track
// (Season = disc, Episode = number, EpisodeTitle = track title) so the
// table's existing columns line up with Lidarr's track rows.

// MusicRef identifies an album (Disc/Number zero = whole album) or a track
// within one — the unit of Unmonitor Selected for kind=music.
type MusicRef struct {
	AlbumID int64 `json:"album_id"`
	Disc    int   `json:"disc,omitempty"`
	Number  int   `json:"number,omitempty"`
}

// MusicCutoffLevel resolves the effective cutoff level ID (the seeded
// ladder's space) for a music quality profile name (L3 rule): the seeded
// profile's cutoff level; empty or unknown names fall back to the
// DefaultMusicQualityProfile profile. 0 = not evaluable.
func MusicCutoffLevel(profileName string) int {
	name := strings.TrimSpace(profileName)
	for _, p := range settings.DefaultMusicProfiles() {
		if p.Name == name {
			return p.Cutoff
		}
	}
	for _, p := range settings.DefaultMusicProfiles() {
		if p.Name == settings.DefaultMusicQualityProfile {
			return p.Cutoff
		}
	}
	return 0
}

// MusicLevelName is the display name of a seeded music level (for the
// cutoff-unmet status text); "unknown" for an absent id.
func MusicLevelName(levelID int) string {
	for _, l := range settings.DefaultMusicLevels() {
		if l.ID == levelID {
			return l.Name
		}
	}
	return "unknown"
}

// musicLevelRank is the best-first rank of a seeded music level (higher is
// better; 0 = unknown/absent).
func musicLevelRank(levelID int) int {
	ladder := settings.DefaultMusicLevels()
	for i := range ladder {
		if ladder[i].ID == levelID {
			return len(ladder) - i
		}
	}
	return 0
}

// BelowMusicCutoff reports whether an imported file of quality fileQuality
// (a codec name such as "FLAC") is strictly below the cutoff level. An
// unrankable cutoff makes the comparison unevaluable (false); an unrankable
// file quality ("", unknown codec) is below every cutoff — the L3 rule's
// "Unknown = below all".
func BelowMusicCutoff(fileQuality string, cutoffLevelID int) bool {
	cr := musicLevelRank(cutoffLevelID)
	if cr == 0 {
		return false
	}
	fr := musicLevelRank(musdom.QualityLevelID(fileQuality))
	if fr == 0 {
		return true // unknown quality is below every cutoff
	}
	return fr < cr
}

// MusicFilter narrows the music rows before paging. Q is a case-insensitive
// substring match on the album title or the artist title.
type MusicFilter struct {
	Tab Tab
	Q   string
}

// Apply classifies the music rows for the filter's tab and returns the
// matching items (input order preserved; the repository returns
// artist/album, disc, number order). Cutoffs maps album id to the resolved
// cutoff level ID (0/absent = not evaluable). The returned slice is always
// non-nil.
func (f MusicFilter) Apply(rows []MusicRow, cutoffs map[int64]int) []Item {
	t := f.Tab.Normalize()
	needle := strings.ToLower(strings.TrimSpace(f.Q))

	out := make([]Item, 0, len(rows))
	for _, r := range rows {
		if !r.Monitored {
			continue
		}
		if needle != "" && !strings.Contains(strings.ToLower(r.AlbumTitle), needle) &&
			!strings.Contains(strings.ToLower(r.ArtistTitle), needle) {
			continue
		}
		item := Item{
			Kind:        "music",
			ArtistID:    r.ArtistID,
			ArtistTitle: r.ArtistTitle,
			AlbumID:     r.AlbumID,
			AlbumTitle:  r.AlbumTitle,
			AlbumType:   r.AlbumType,
			ReleaseDate: r.ReleaseDate,
			SeriesID:    r.AlbumID, // the drill-down parent is the album
			SeriesTitle: r.AlbumTitle,
		}
		isTrack := r.Disc > 0 || r.Number > 0
		if isTrack {
			item.Season = r.Disc
			item.Episode = r.Number
			item.EpisodeTitle = r.TrackTitle
		}
		switch t {
		case TabMissing:
			if isTrack {
				// A monitored track with no imported file.
				if !r.TrackMonitored || r.TrackStatus != "" {
					continue
				}
			} else {
				// An effectively-monitored album with no imported file.
				if r.FileCount > 0 {
					continue
				}
			}
			item.Status = "missing:no file"
		case TabCutoff:
			// Album granularity only (Lidarr's cutoff tab lists albums,
			// flagged by their below-cutoff files): track rows drop out.
			if isTrack {
				continue
			}
			cut, ok := cutoffs[r.AlbumID]
			if !ok || cut == 0 {
				continue // not evaluable
			}
			// The album row: unmet when any imported track is below the
			// cutoff (an album with no files is a missing row, not a
			// cutoff row).
			if r.FileCount == 0 {
				continue
			}
			var worst string
			unmet := false
			for _, q := range r.ImportedQualities {
				if BelowMusicCutoff(q, cut) {
					unmet = true
					if worst == "" {
						worst = q
					}
				}
			}
			if !unmet {
				continue
			}
			item.Quality = worst
			item.Status = "cutoff:" + worst + " below cutoff " + MusicLevelName(cut)
		default:
			return []Item{}
		}
		out = append(out, item)
	}
	return out
}
