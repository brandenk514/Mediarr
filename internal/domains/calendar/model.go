// Package calendar is the P4 domain: it shapes upcoming monitored TV air
// dates into the calendar page's rows and the iCal feed.
//
// The only I/O boundary is the Repo interface (implemented by the Postgres
// store); everything else here is pure and unit-testable without a database.
//
// Air dates: the TV domain stores a date-only air date (tv_episodes.air_date
// is a DATE column sourced from provider metadata). The calendar therefore
// derives a full time range from each date: start is the date at
// DefaultAirTime (UTC) and end is start + DefaultRuntime. The feed and the
// page both read from these derived values.
package calendar

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Entry is one upcoming air date, in the wire shape returned by
// GET /api/v1/calendar (snake_case, matching the OpenAPI CalendarEntry).
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

// Repo provides the episodes the calendar renders over.
type Repo interface {
	// Episodes returns every episode with a known air date, joined with its
	// series title and monitor flag, ordered by air date then show, season,
	// episode.
	Episodes(ctx context.Context) ([]EpisodeRow, error)
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

// icsUID returns a stable, unique identifier for an episode so calendar
// clients de-duplicate it across feed refreshes.
func icsUID(e Entry) string {
	return fmt.Sprintf("mediarr-tv-%d-s%02d-e%02d@mediarr", e.SeriesID, e.Season, e.Episode)
}

// RenderICal renders the entries as an RFC 5545 iCal document: a VCALENDAR
// wrapper (VERSION + PRODID) with one VEVENT per entry. Each event carries a
// stable UID, a DTSTAMP (now), DTSTART/DTEND (the air range), and a SUMMARY of
// "Show - Title". `now` is injected for a deterministic DTSTAMP in tests. The
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
		b.WriteString("DTSTART:" + icsStamp(e.AirStart) + "\r\n")
		b.WriteString("DTEND:" + icsStamp(e.AirEnd) + "\r\n")
		b.WriteString("SUMMARY:" + icsText(e.SeriesTitle+" - "+e.EpisodeTitle) + "\r\n")
		b.WriteString("DESCRIPTION:" + icsText(e.SeriesTitle+" "+e.SXE) + "\r\n")
		b.WriteString("END:VEVENT\r\n")
	}
	b.WriteString("END:VCALENDAR\r\n")
	return b.String()
}
