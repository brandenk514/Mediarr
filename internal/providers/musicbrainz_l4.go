package providers

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// MusicBrainz L4 (Lidarr parity, phase L4): keyless release-detail metadata
// plus Cover Art Archive cover art.
//
// OpenLibrary decision: OpenLibrary has no meaningful music API, so it stays
// the books source of truth (B phases). All music metadata in Mediarr comes
// from MusicBrainz — releases/labels/genres and the Cover Art Archive
// (the CAA is part of the MusicBrainz project and is keyless, so L4 needs no
// new credentials).

const defaultCoverArtArchiveBase = "https://coverartarchive.org"

// ReleaseTrack is one track of a release, normalized for the music domain.
type ReleaseTrack struct {
	Disc     int // 1-based disc position
	Number   int // 1-based track position within the disc
	Title    string
	Duration int // seconds (0 = unknown)
	MBID     string
}

// ReleaseDetail is the normalized L4 release metadata: what AddAlbum /
// RefreshArtist need to populate an album and its tracks. All list fields
// are non-nil (empty = none/unknown).
type ReleaseDetail struct {
	MBID        string // the resolved release id (not the release-group id)
	Name        string
	ReleaseDate string // "YYYY-MM-DD"; "" when only a partial date is known
	Year        int    // 0 if unknown
	AlbumType   string // vocabulary from normalizeReleaseGroupType; "album" default
	Status      string
	Labels      []string
	Genre       []string
	Tracks      []ReleaseTrack
}

// mbGroupRelease is one release entry of a release-group lookup.
type mbGroupRelease struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
	Date   string `json:"date"`
}

// mbReleaseGroupReleases is a release-group lookup (inc=releases).
type mbReleaseGroupReleases struct {
	ID          string           `json:"id"`
	Title       string           `json:"title"`
	PrimaryType string           `json:"primary-type"`
	Releases    []mbGroupRelease `json:"releases"`
}

// mbReleaseDetail is a release lookup (inc=media+labels+genres).
type mbReleaseDetail struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
	Date   string `json:"date"`
	Media  []struct {
		Position int `json:"position"`
		Tracks   []struct {
			ID       string `json:"id"`
			Position int    `json:"position"`
			Title    string `json:"title"`
			Length   int    `json:"length"` // milliseconds
		} `json:"tracks"`
	} `json:"media"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
	Genres []struct {
		Name string `json:"name"`
	} `json:"genres"`
}

// caaImagesResponse is a Cover Art Archive images lookup.
type caaImagesResponse struct {
	Images []struct {
		URL   string `json:"url"`
		Sizes struct {
			Front int `json:"front"`
		} `json:"sizes"`
		Formats struct {
			Front []string `json:"front"`
		} `json:"formats"`
	} `json:"images"`
}

// caaBaseURL is the Cover Art Archive base URL (test-overridable).
func (m *MusicBrainz) caaBaseURL() string {
	if m.CAABaseURL != "" {
		return m.CAABaseURL
	}
	return defaultCoverArtArchiveBase
}

// ReleaseDetail resolves a MusicBrainz release-group id (preferred) or a
// release id to one concrete release with media, labels, and genres.
//
// Resolution: the id is first treated as a release-group id (inc=releases);
// the group's releases are scanned and a release is picked — prefer an
// "Official" status release, else the earliest dated one. The chosen release
// is then fetched (inc=media+labels+genres) for the detail. When the id does
// not resolve as a group (unknown id → 404, or a bare release id), the same
// id is retried directly as a release id.
//
// Keyless, and bounded: at most two MusicBrainz lookups per call, so a
// refresh over a large library stays within polite rate limits.
func (m *MusicBrainz) ReleaseDetail(ctx context.Context, mbID string) (*ReleaseDetail, error) {
	id := strings.TrimSpace(mbID)
	if id == "" {
		return nil, fmt.Errorf("providers: release detail requires a release-group or release id")
	}

	m.Base = m.baseURL()
	var group mbReleaseGroupReleases
	if err := m.getJSON(ctx, "/release-group/"+id, url.Values{"fmt": {"json"}, "inc": {"releases"}}, &group); err == nil && len(group.Releases) > 0 {
		rel := pickRelease(group.Releases)
		if rel.ID == "" {
			return nil, fmt.Errorf("providers: release group %s has no usable releases", id)
		}
		return m.fetchReleaseDetail(ctx, rel.ID, group.PrimaryType)
	}
	// Not a group (or an empty group): the id is a bare release id.
	return m.fetchReleaseDetail(ctx, id, "")
}

// fetchReleaseDetail fetches one release (media + labels + genres) and
// normalizes it. groupType (the release group's primary-type, when known)
// drives the AlbumType mapping; without it the type defaults to "album".
func (m *MusicBrainz) fetchReleaseDetail(ctx context.Context, releaseID, groupType string) (*ReleaseDetail, error) {
	var rd mbReleaseDetail
	if err := m.getJSON(ctx, "/release/"+releaseID, url.Values{"fmt": {"json"}, "inc": {"media+labels+genres"}}, &rd); err != nil {
		return nil, fmt.Errorf("providers: fetch release %s: %w", releaseID, err)
	}

	detail := &ReleaseDetail{
		MBID:      rd.ID,
		Name:      rd.Title,
		Status:    rd.Status,
		AlbumType: "album",
		Labels:    []string{},
		Genre:     []string{},
		Tracks:    []ReleaseTrack{},
	}
	detail.ReleaseDate, detail.Year = normalizeReleaseDate(rd.Date)
	if groupType != "" {
		detail.AlbumType = normalizeReleaseGroupType(groupType)
	}
	for _, md := range rd.Media {
		disc := md.Position
		if disc < 1 {
			disc = 1
		}
		for _, tr := range md.Tracks {
			number := tr.Position
			if number < 1 {
				number = 1
			}
			detail.Tracks = append(detail.Tracks, ReleaseTrack{
				Disc:     disc,
				Number:   number,
				Title:    tr.Title,
				Duration: tr.Length / 1000,
				MBID:     tr.ID,
			})
		}
	}
	for _, l := range rd.Labels {
		detail.Labels = appendUnique(detail.Labels, l.Name)
	}
	for _, g := range rd.Genres {
		detail.Genre = appendUnique(detail.Genre, g.Name)
	}
	return detail, nil
}

// ReleaseCoverArt returns the URL of the release's Cover Art Archive front
// cover — the 500px thumbnail when one exists, else the smallest available
// front image. "" on any error, 404, or when the release has no cover.
func (m *MusicBrainz) ReleaseCoverArt(ctx context.Context, releaseID string) string {
	id := strings.TrimSpace(releaseID)
	if id == "" {
		return ""
	}
	return m.coverFront(ctx, "release", id)
}

// ArtistCoverArt is ReleaseCoverArt for artist ids.
func (m *MusicBrainz) ArtistCoverArt(ctx context.Context, artistID string) string {
	id := strings.TrimSpace(artistID)
	if id == "" {
		return ""
	}
	return m.coverFront(ctx, "artist", id)
}

// coverFront fetches {kind}/{id}.json from the Cover Art Archive and picks
// the front image: a native-500 front image (its URL is the 500 thumbnail)
// wins; otherwise the smallest front image. Best effort: "" on any failure.
func (m *MusicBrainz) coverFront(ctx context.Context, kind, id string) string {
	m.Base = m.caaBaseURL()
	var resp caaImagesResponse
	if err := m.getJSON(ctx, "/"+kind+"/"+id+".json", nil, &resp); err != nil {
		return ""
	}
	best := ""
	bestSize := 0
	for _, img := range resp.Images {
		if len(img.Formats.Front) == 0 || img.URL == "" {
			continue
		}
		if img.Sizes.Front == 500 {
			return img.URL
		}
		if best == "" || (img.Sizes.Front > 0 && (bestSize == 0 || img.Sizes.Front < bestSize)) {
			best, bestSize = img.URL, img.Sizes.Front
		}
	}
	return best
}

// pickRelease picks the release to detail from a group's releases: prefer an
// "Official" status release, else the earliest dated release, else the first.
func pickRelease(rels []mbGroupRelease) mbGroupRelease {
	var official, earliest mbGroupRelease
	hasEarliest := false
	for _, r := range rels {
		if r.ID == "" {
			continue
		}
		if r.Status == "Official" && official.ID == "" {
			official = r
		}
		if !hasEarliest || (r.Date != "" && (earliest.Date == "" || r.Date < earliest.Date)) {
			earliest, hasEarliest = r, true
		}
	}
	if official.ID != "" {
		return official
	}
	if hasEarliest {
		return earliest
	}
	return mbGroupRelease{}
}

// normalizeReleaseDate maps a MusicBrainz release date — a full "YYYY-MM-DD",
// a partial "YYYY-MM"/"YYYY" prefix, or "" — to (ReleaseDate, Year):
// ReleaseDate is set only for full calendar dates; Year from the leading
// four digits (0 when absent).
func normalizeReleaseDate(date string) (string, int) {
	s := strings.TrimSpace(date)
	year := 0
	if len(s) >= 4 {
		year, _ = strconv.Atoi(s[:4])
	}
	if len(s) < 10 || s[4] != '-' || s[7] != '-' {
		return "", year
	}
	return s[:10], year
}

// appendUnique appends s to list unless it is empty or already present.
func appendUnique(list []string, s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return list
	}
	for _, existing := range list {
		if existing == s {
			return list
		}
	}
	return append(list, s)
}
