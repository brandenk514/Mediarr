// Package calendar is the P4/R4 domain: it shapes upcoming monitored TV air
// dates (P4) and movie release dates (R4) into the calendar page's rows and
// the iCal feed.
//
// The only I/O boundary is the Repo interface (implemented by the Postgres
// store); everything else here is pure and unit-testable without a database.
//
// Air dates: the TV domain stores a date-only air date (tv_episodes.air_date
// is a DATE column sourced from provider metadata). The calendar therefore
// derives a full time range from each date: start is the date at
// DefaultAirTime (UTC) and end is start + DefaultRuntime. The feed and the
// page both read from these derived values.
//
// Release dates (R4): movies carry a date-only release date
// (movies.release_date, "YYYY-MM-DD", persisted by the TMDB enricher). The
// movie calendar derives an all-day range per release (midnight .. 23:59
// UTC) and reuses the Entry wire shape — SeriesID/SeriesTitle carry the
// movie id/title and the Year/Status/QualityProfile extras hold the movie
// variant's identifying fields.
package calendar

import (
	"context"
	"fmt"
	"strings"
	"time"

	moviesdom "github.com/brandenk514/mediarr/internal/domains/movies"
)

// Kind is which feed a calendar window is for.
type Kind string

const (
	// KindTV is the P4 TV air-date calendar (the default).
	KindTV Kind = "tv"
	// KindMovie is the R4 movie release-date calendar.
	KindMovie Kind = "movie"
	// KindMusic is the L4 music release-date calendar (upcoming releases of
	// monitored albums).
	KindMusic Kind = "music"
)

// Entry is one upcoming air date, in the wire shape returned by
// GET /api/v1/calendar (snake_case, matching the OpenAPI CalendarEntry).
// The same shape carries movie entries (kind=movie, R4): SeriesID is the
// movie id, SeriesTitle the movie title, season/episode/sxe are zero/"" and
// the Year/Status/QualityProfile fields hold the movie's identifying extras.
type Entry struct {
	Kind         string     `json:"kind"`
	SeriesID     int64      `json:"series_id"`
	SeriesTitle  string     `json:"series_title"`
	Season       int        `json:"season"`
	Episode      int        `json:"episode"`
	EpisodeTitle string     `json:"episode_title"`
	SXE          string     `json:"sxe"`
	AirDate      *time.Time `json:"air_date"`
	AirStart     time.Time  `json:"air_start"`
	AirEnd       time.Time  `json:"air_end"`

	// R4 movie-calendar extras. Zero/empty on TV entries; the omitempty
	// tags keep the TV wire shape byte-compatible.
	Year           int    `json:"year,omitempty"`
	Status         string `json:"status,omitempty"`
	QualityProfile string `json:"quality_profile,omitempty"`

	// L4 music-calendar extras. Zero/empty on TV/movie entries; the
	// omitempty tags keep the TV/movie wire shape byte-compatible.
	// SeriesID/SeriesTitle carry the album id/title; these carry the artist.
	ArtistID    int64  `json:"artist_id,omitempty"`
	ArtistTitle string `json:"artist_title,omitempty"`
	AlbumID     int64  `json:"album_id,omitempty"`
	AlbumTitle  string `json:"album_title,omitempty"`
	AlbumType   string `json:"album_type,omitempty"`
}

// EpisodeRow is the repository shape: an episode with a known air date, plus
// its series title and monitor flag, as selected by the store.
type EpisodeRow struct {
	SeriesID     int64
	SeriesTitle  string
	Season       int
	Episode      int
	EpisodeTitle string
	AirDate      *time.Time
	Monitored    bool
}

// MovieRow is the R4 repository shape: a movie with a known release date, its
// monitor flag, and the state the status derivation needs (wanted
// satisfaction + active queue entry), as selected by the store.
type MovieRow struct {
	ID    int64
	Title string
	Year  int
	// ReleaseDate is nil when unknown or unparseable (such rows are
	// excluded by BuildMovies).
	ReleaseDate *time.Time
	Monitored   bool
	// WantedSatisfied/InQueue feed DeriveMovieStatus so the calendar agrees
	// with the library's status chips.
	WantedSatisfied bool
	InQueue         bool
	QualityProfile  string
}

// MusicRow is the L4 repository shape: a monitored album with a known
// release date, its artist identity, and the effective quality profile,
// as selected by the store.
type MusicRow struct {
	ArtistID    int64
	ArtistTitle string
	AlbumID     int64
	AlbumTitle  string
	AlbumType   string
	Year        int
	// ReleaseDate is nil when unknown or unparseable (such rows are
	// excluded by BuildMusic).
	ReleaseDate *time.Time
	// Monitored is true when BOTH the artist and the album are monitored.
	Monitored bool
	// QualityProfile is the album's effective profile name (album-level
	// profile, falling back to the artist's, falling back to the default).
	QualityProfile string
}

// Repo provides the episodes, movies, and music the calendar renders over.
type Repo interface {
	// Episodes returns every episode with a known air date, joined with its
	// series title and monitor flag, ordered by air date then show, season,
	// episode.
	Episodes(ctx context.Context) ([]EpisodeRow, error)
	// MovieRows returns every movie with a known (non-empty) release date,
	// with its monitor flag and the status-derivation state, ordered by
	// release date then title (R4 movie calendar).
	MovieRows(ctx context.Context) ([]MovieRow, error)
	// MusicRows returns every monitored album (artist + album monitored)
	// with a known (non-empty) release date, ordered by release date then
	// album title (L4 music calendar).
	MusicRows(ctx context.Context) ([]MusicRow, error)
}

var (
	// DefaultAirTime is the time of day (UTC) an episode is assumed to air
	// when only a date is stored.
	DefaultAirTime = time.Date(2000, 1, 1, 21, 0, 0, 0, time.UTC)
	// DefaultRuntime is the assumed duration of an episode for the calendar
	// time range when only a date is known.
	DefaultRuntime = 42 * time.Minute
)

// SxE formats the absolute season/episode as a zero-padded "S1E05".
func SxE(season, episode int) string {
	return fmt.Sprintf("S%02dE%02d", season, episode)
}

// AirRange derives the air start + end from a date-only air date: start is the
// date at DefaultAirTime (UTC); end is start + DefaultRuntime.
func AirRange(airDate time.Time) (start, end time.Time) {
	y, m, d := airDate.Date()
	start = time.Date(y, m, d, DefaultAirTime.Hour(), DefaultAirTime.Minute(), DefaultAirTime.Second(), 0, time.UTC)
	return start, start.Add(DefaultRuntime)
}

// ReleaseRange derives the calendar time range from a movie's release date
// (R4): start is the release day at midnight (UTC) and end is 23:59 on that
// day — the all-day span the UI shows as 00:00–23:59.
func ReleaseRange(releaseDate time.Time) (start, end time.Time) {
	y, m, d := releaseDate.Date()
	start = time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	return start, time.Date(y, m, d, 23, 59, 0, 0, time.UTC)
}

// utcDate truncates t to its UTC calendar date (midnight), so date-only
// comparisons are independent of the input's time-of-day or location.
func utcDate(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// Build filters the store rows into calendar entries: monitored episodes whose
// air date is within [start, end] (inclusive, date-based) and in the future
// (on or after now's date). `now` is injected so the future cutoff is
// deterministic in tests. The input order is preserved. The returned slice is
// never nil (an empty window yields []), matching the API contract.
func Build(rows []EpisodeRow, start, end, now time.Time) []Entry {
	sd, ed, nd := utcDate(start), utcDate(end), utcDate(now)
	out := make([]Entry, 0, len(rows))
	for _, r := range rows {
		if !r.Monitored || r.AirDate == nil {
			continue
		}
		ad := utcDate(*r.AirDate)
		if ad.Before(sd) || ad.After(ed) {
			continue
		}
		if ad.Before(nd) {
			continue // already aired
		}
		as, ae := AirRange(*r.AirDate)
		out = append(out, Entry{
			Kind:         "tv",
			SeriesID:     r.SeriesID,
			SeriesTitle:  r.SeriesTitle,
			Season:       r.Season,
			Episode:      r.Episode,
			EpisodeTitle: r.EpisodeTitle,
			SXE:          SxE(r.Season, r.Episode),
			AirDate:      r.AirDate,
			AirStart:     as,
			AirEnd:       ae,
		})
	}
	return out
}

// BuildMovies filters the store rows into movie calendar entries (R4):
// monitored movies whose release date is within [start, end] (inclusive,
// date-based) and not in the past (on or after now's date — the same future
// rule as TV). The entry's identifying fields carry the movie: SeriesID is
// the movie id, SeriesTitle the title, and Year/Status/QualityProfile hold
// the movie variant's extras. The status uses the movies domain's shared
// derivation so the calendar agrees with the library's status chips. The
// input order is preserved. The returned slice is never nil.
func BuildMovies(rows []MovieRow, start, end, now time.Time) []Entry {
	sd, ed, nd := utcDate(start), utcDate(end), utcDate(now)
	out := make([]Entry, 0, len(rows))
	for _, r := range rows {
		if !r.Monitored || r.ReleaseDate == nil {
			continue
		}
		rd := utcDate(*r.ReleaseDate)
		if rd.Before(sd) || rd.After(ed) {
			continue
		}
		if rd.Before(nd) {
			continue // already released
		}
		as, ae := ReleaseRange(*r.ReleaseDate)
		status := moviesdom.DeriveMovieStatus(r.WantedSatisfied, r.InQueue, r.Monitored, r.ReleaseDate, now)
		out = append(out, Entry{
			Kind:           string(KindMovie),
			SeriesID:       r.ID,
			SeriesTitle:    r.Title,
			AirDate:        r.ReleaseDate,
			AirStart:       as,
			AirEnd:         ae,
			Year:           r.Year,
			Status:         string(status),
			QualityProfile: r.QualityProfile,
		})
	}
	return out
}

// BuildMusic filters the store rows into music calendar entries (L4):
// monitored albums whose release date is within [start, end] (inclusive,
// date-based) and not in the past (on or after now's date — the same future
// rule as TV). The entry's identifying fields carry the album: SeriesID is
// the album id, SeriesTitle the album title; the music extras carry the
// artist and album type. Every entry the calendar shows is a future release,
// so the status is "unreleased". The input order is preserved. The returned
// slice is never nil.
func BuildMusic(rows []MusicRow, start, end, now time.Time) []Entry {
	sd, ed, nd := utcDate(start), utcDate(end), utcDate(now)
	out := make([]Entry, 0, len(rows))
	for _, r := range rows {
		if !r.Monitored || r.ReleaseDate == nil {
			continue
		}
		rd := utcDate(*r.ReleaseDate)
		if rd.Before(sd) || rd.After(ed) {
			continue
		}
		if rd.Before(nd) {
			continue // already released
		}
		as, ae := ReleaseRange(*r.ReleaseDate)
		out = append(out, Entry{
			Kind:           string(KindMusic),
			SeriesID:       r.AlbumID,
			SeriesTitle:    r.AlbumTitle,
			AirDate:        r.ReleaseDate,
			AirStart:       as,
			AirEnd:         ae,
			Year:           r.Year,
			Status:         "unreleased",
			QualityProfile: r.QualityProfile,
			ArtistID:       r.ArtistID,
			ArtistTitle:    r.ArtistTitle,
			AlbumID:        r.AlbumID,
			AlbumTitle:     r.AlbumTitle,
			AlbumType:      r.AlbumType,
		})
	}
	return out
}

// icsStamp renders a time as an RFC 5545 UTC date-time (20060102T150405Z).
func icsStamp(t time.Time) string {
	return t.UTC().Format("20060102T150405Z")
}

// icsText escapes a string for use in an RFC 5545 TEXT value.
func icsText(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "\r\n", `\n`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, ";", `\;`)
	s = strings.ReplaceAll(s, ",", `\,`)
	return s
}

// icsUID returns a stable, unique identifier for an entry so calendar
// clients de-duplicate it across feed refreshes.
func icsUID(e Entry) string {
	switch e.Kind {
	case string(KindMovie):
		return fmt.Sprintf("mediarr-movie-%d@mediarr", e.SeriesID)
	case string(KindMusic):
		return fmt.Sprintf("mediarr-music-%d@mediarr", e.AlbumID)
	}
	return fmt.Sprintf("mediarr-tv-%d-s%02d-e%02d@mediarr", e.SeriesID, e.Season, e.Episode)
}

// movieSummary is the iCal SUMMARY for a movie entry: "Title (Year)".
func movieSummary(e Entry) string {
	return fmt.Sprintf("%s (%d)", e.SeriesTitle, e.Year)
}

// musicSummary is the iCal SUMMARY for a music entry: "Artist - Album (Year)"
// (the Lidarr release naming the UI uses elsewhere).
func musicSummary(e Entry) string {
	if e.ArtistTitle == "" {
		return fmt.Sprintf("%s (%d)", e.AlbumTitle, e.Year)
	}
	return fmt.Sprintf("%s - %s (%d)", e.ArtistTitle, e.AlbumTitle, e.Year)
}

// RenderICal renders the entries as an RFC 5545 iCal document: a VCALENDAR
// wrapper (VERSION + PRODID) with one VEVENT per entry. TV entries carry a
// stable UID, a DTSTAMP (now), DTSTART/DTEND (the air range) and a SUMMARY
// of "Show - Title". Movie entries (R4) are all-day events on the release
// date (DATE values, DTEND the exclusive day after) with SUMMARY "Title
// (Year)". `now` is injected for a deterministic DTSTAMP in tests. The
// output uses CRLF line endings and is directly importable by Google/Apple
// Calendar.
func RenderICal(entries []Entry, now time.Time) string {
	var b strings.Builder
	b.WriteString("BEGIN:VCALENDAR\r\n")
	b.WriteString("VERSION:2.0\r\n")
	b.WriteString("PRODID:-//Mediarr//Calendar 1.0//EN\r\n")
	b.WriteString("CALSCALE:GREGORIAN\r\n")
	b.WriteString("METHOD:PUBLISH\r\n")
	for _, e := range entries {
		b.WriteString("BEGIN:VEVENT\r\n")
		b.WriteString("UID:" + icsUID(e) + "\r\n")
		b.WriteString("DTSTAMP:" + icsStamp(now) + "\r\n")
		if e.Kind == string(KindMovie) || e.Kind == string(KindMusic) {
			// All-day event on the release date. RFC 5545: DATE values;
			// DTEND is the exclusive day after, per the all-day rule.
			summary := movieSummary(e)
			if e.Kind == string(KindMusic) {
				summary = musicSummary(e)
			}
			b.WriteString("DTSTART;VALUE=DATE:" + e.AirStart.UTC().Format("20060102") + "\r\n")
			b.WriteString("DTEND;VALUE=DATE:" + e.AirStart.UTC().AddDate(0, 0, 1).Format("20060102") + "\r\n")
			b.WriteString("SUMMARY:" + icsText(summary) + "\r\n")
			b.WriteString("DESCRIPTION:" + icsText(summary) + "\r\n")
		} else {
			b.WriteString("DTSTART:" + icsStamp(e.AirStart) + "\r\n")
			b.WriteString("DTEND:" + icsStamp(e.AirEnd) + "\r\n")
			b.WriteString("SUMMARY:" + icsText(e.SeriesTitle+" - "+e.EpisodeTitle) + "\r\n")
			b.WriteString("DESCRIPTION:" + icsText(e.SeriesTitle+" "+e.SXE) + "\r\n")
		}
		b.WriteString("END:VEVENT\r\n")
	}
	b.WriteString("END:VCALENDAR\r\n")
	return b.String()
}
