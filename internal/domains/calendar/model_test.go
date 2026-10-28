package calendar

import (
	"strings"
	"testing"
	"time"
)

// now is the fixed "current" time used by the Build tests.
var testNow = time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)

func testWindow() (start, end time.Time) {
	// Window straddles "now" so range and future exclusions can be told apart.
	return time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC), time.Date(2026, 5, 31, 23, 59, 59, 0, time.UTC)
}

func ad(t time.Time) *time.Time { return &t }

func row(title string, season, episode int, air time.Time, monitored bool) EpisodeRow {
	r := EpisodeRow{
		SeriesID:     int64(100 + len(title)),
		SeriesTitle:  title,
		Season:       season,
		Episode:      episode,
		EpisodeTitle: "Ep " + title,
		Monitored:    monitored,
	}
	if !air.IsZero() {
		r.AirDate = ad(air)
	}
	return r
}

func TestSxE(t *testing.T) {
	cases := map[[2]int]string{
		{1, 5}:   "S01E05",
		{12, 3}:  "S12E03",
		{100, 1}: "S100E01",
	}
	for in, want := range cases {
		if got := SxE(in[0], in[1]); got != want {
			t.Errorf("SxE(%d,%d) = %q, want %q", in[0], in[1], got, want)
		}
	}
}

func TestAirRange(t *testing.T) {
	start, end := AirRange(time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC))
	wantStart := time.Date(2026, 5, 15, 21, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2026, 5, 15, 21, 42, 0, 0, time.UTC)
	if !start.Equal(wantStart) {
		t.Errorf("start = %v, want %v", start, wantStart)
	}
	if !end.Equal(wantEnd) {
		t.Errorf("end = %v, want %v", end, wantEnd)
	}
	// The date-only input's time-of-day must not leak into the range.
	start2, _ := AirRange(time.Date(2026, 5, 15, 3, 17, 45, 0, time.UTC))
	if !start2.Equal(wantStart) {
		t.Errorf("start leaked the input time: %v", start2)
	}
}

// includedTitles is a tiny helper to assert the exact set of rows that survive
// Build.
func includedTitles(t *testing.T, rows []EpisodeRow, start, end, now time.Time) map[string]bool {
	t.Helper()
	got := Build(rows, start, end, now)
	m := map[string]bool{}
	for _, e := range got {
		m[e.SeriesTitle] = true
	}
	return m
}

func TestBuild_MonitoredOnly(t *testing.T) {
	start, end := testWindow()
	rows := []EpisodeRow{
		row("Monitored", 1, 1, time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC), true),
		row("Unmonitored", 1, 1, time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC), false),
		row("NoDate", 1, 1, time.Time{}, true),
	}
	got := includedTitles(t, rows, start, end, testNow)
	if !got["Monitored"] {
		t.Errorf("expected Monitored in results; got %v", got)
	}
	if got["Unmonitored"] || got["NoDate"] {
		t.Errorf("Unmonitored/NoDate must be excluded; got %v", got)
	}
}

func TestBuild_RangeExclusion(t *testing.T) {
	start, end := testWindow()
	// now = 2026-05-15; window = [05-10, 05-31].
	rows := []EpisodeRow{
		row("BeforeWindow", 1, 1, time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC), true), // < start
		row("AfterWindow", 1, 1, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), true),  // > end
		row("InWindow", 1, 1, time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC), true),    // inside
	}
	got := includedTitles(t, rows, start, end, testNow)
	if got["BeforeWindow"] || got["AfterWindow"] {
		t.Errorf("out-of-range rows must be excluded; got %v", got)
	}
	if !got["InWindow"] {
		t.Errorf("in-range row must be included; got %v", got)
	}
}

func TestBuild_WindowBoundariesInclusive(t *testing.T) {
	start, end := testWindow()
	// A row sitting exactly on the (future) window end must be included.
	rows := []EpisodeRow{
		row("AtEnd", 1, 1, time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC), true),
	}
	got := includedTitles(t, rows, start, end, testNow)
	if !got["AtEnd"] {
		t.Errorf("window-end date must be inclusive; got %v", got)
	}
}

func TestBuild_FutureOnly(t *testing.T) {
	start, end := testWindow()
	// In the window but already aired (before now) -> excluded by future filter.
	rows := []EpisodeRow{
		row("InWindowPast", 1, 1, time.Date(2026, 5, 12, 0, 0, 0, 0, time.UTC), true),
		row("InWindowFuture", 1, 1, time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC), true),
	}
	got := includedTitles(t, rows, start, end, testNow)
	if got["InWindowPast"] {
		t.Errorf("in-window but past must be excluded; got %v", got)
	}
	if !got["InWindowFuture"] {
		t.Errorf("in-window future must be included; got %v", got)
	}
}

func TestBuild_TodayCountsAsFuture(t *testing.T) {
	// A row airing on "today" (== now's date) is still upcoming.
	rows := []EpisodeRow{
		row("Today", 1, 1, time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC), true),
		row("Yesterday", 1, 1, time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC), true),
	}
	got := includedTitles(t, rows, testNow, testNow.Add(7*24*time.Hour), testNow)
	if !got["Today"] {
		t.Errorf("today must be included; got %v", got)
	}
	if got["Yesterday"] {
		t.Errorf("yesterday must be excluded; got %v", got)
	}
}

func TestBuild_PreservesOrderAndShapesSXE(t *testing.T) {
	start, end := testWindow()
	// Both air dates are in the future (after now = 05-15) and inside the
	// window, but the *input* order is NOT sorted by air date (Zebra 05-20
	// before Alpha 05-18). Build must preserve the input (repo) order.
	rows := []EpisodeRow{
		row("Zebra", 2, 3, time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC), true),
		row("Alpha", 1, 5, time.Date(2026, 5, 18, 0, 0, 0, 0, time.UTC), true),
	}
	got := Build(rows, start, end, testNow)
	if len(got) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(got))
	}
	if got[0].SeriesTitle != "Zebra" || got[1].SeriesTitle != "Alpha" {
		t.Errorf("order not preserved: %v", []string{got[0].SeriesTitle, got[1].SeriesTitle})
	}
	if got[1].SXE != "S01E05" {
		t.Errorf("SXE = %q, want S01E05", got[1].SXE)
	}
	if got[1].Kind != "tv" {
		t.Errorf("Kind = %q, want tv", got[1].Kind)
	}
	if got[1].AirDate == nil || !got[1].AirDate.Equal(time.Date(2026, 5, 18, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("AirDate not passed through: %v", got[1].AirDate)
	}
}

func TestBuild_EmptyWindowIsNonNil(t *testing.T) {
	start, end := testWindow()
	got := Build(nil, start, end, testNow)
	if got == nil {
		t.Fatal("Build(nil) must return a non-nil (empty) slice for the [] contract")
	}
	if len(got) != 0 {
		t.Errorf("expected empty, got %d", len(got))
	}
}

func TestRenderICal_Structure(t *testing.T) {
	entries := []Entry{
		{
			Kind: "tv", SeriesID: 10, SeriesTitle: "Severance", Season: 1, Episode: 5,
			EpisodeTitle: "Pilot", SXE: "S01E05",
			AirStart: time.Date(2026, 5, 15, 21, 0, 0, 0, time.UTC),
			AirEnd:   time.Date(2026, 5, 15, 21, 42, 0, 0, time.UTC),
		},
		{
			Kind: "tv", SeriesID: 20, SeriesTitle: "Slow Horses", Season: 2, Episode: 1,
			EpisodeTitle: "Episode 1", SXE: "S02E01",
			AirStart: time.Date(2026, 5, 16, 21, 0, 0, 0, time.UTC),
			AirEnd:   time.Date(2026, 5, 16, 21, 42, 0, 0, time.UTC),
		},
	}
	out := RenderICal(entries, time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC))

	for _, want := range []string{
		"BEGIN:VCALENDAR",
		"VERSION:2.0",
		"PRODID:-//Mediarr//Calendar 1.0//EN",
		"END:VCALENDAR",
		"BEGIN:VEVENT",
		"DTSTAMP:20260510T120000Z",
		"DTSTART:20260515T210000Z",
		"DTEND:20260515T214200Z",
		"SUMMARY:Severance - Pilot",
		"UID:mediarr-tv-10-s01-e05@mediarr",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("ICS missing %q\n%s", want, out)
		}
	}
	if n := strings.Count(out, "BEGIN:VEVENT"); n != 2 {
		t.Errorf("expected 2 VEVENTs, got %d", n)
	}
	if n := strings.Count(out, "END:VEVENT"); n != 2 {
		t.Errorf("expected 2 END:VEVENT, got %d", n)
	}
	if !strings.Contains(out, "\r\n") {
		t.Error("ICS should use CRLF line endings")
	}
}

func TestRenderICal_EscapesText(t *testing.T) {
	e := Entry{
		Kind: "tv", SeriesID: 1, SeriesTitle: "Show, Inc.", Season: 1, Episode: 1,
		EpisodeTitle: "A; B", SXE: "S01E01",
		AirStart: time.Date(2026, 5, 15, 21, 0, 0, 0, time.UTC),
		AirEnd:   time.Date(2026, 5, 15, 21, 42, 0, 0, time.UTC),
	}
	out := RenderICal([]Entry{e}, time.Time{})
	if !strings.Contains(out, "SUMMARY:Show\\, Inc. - A\\; B") {
		t.Errorf("SUMMARY not RFC5545-escaped:\n%s", out)
	}
}

func TestRenderICal_Empty(t *testing.T) {
	out := RenderICal(nil, time.Time{})
	if !strings.Contains(out, "BEGIN:VCALENDAR") || !strings.Contains(out, "END:VCALENDAR") {
		t.Errorf("empty feed must still be a valid VCALENDAR:\n%s", out)
	}
	if strings.Contains(out, "BEGIN:VEVENT") {
		t.Errorf("empty feed must have no VEVENT:\n%s", out)
	}
}

func TestReleaseRange_AllDaySpan(t *testing.T) {
	start, end := ReleaseRange(time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC))
	if !start.Equal(time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("start = %v", start)
	}
	if !end.Equal(time.Date(2026, 5, 20, 23, 59, 0, 0, time.UTC)) {
		t.Errorf("end = %v", end)
	}
}

func TestBuildMovies_FiltersAndShape(t *testing.T) {
	future := time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC)
	past := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	unmon := time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC)
	start, end, now := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC), time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC), time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	out := BuildMovies([]MovieRow{
		{ID: 1, Title: "In", Year: 2026, ReleaseDate: &future, Monitored: true, QualityProfile: "HD-1080p"},
		{ID: 2, Title: "Past", Year: 2020, ReleaseDate: &past, Monitored: true},
		{ID: 3, Title: "Unmon", Year: 2026, ReleaseDate: &unmon, Monitored: false},
		{ID: 4, Title: "NoDate", Year: 2026, ReleaseDate: nil, Monitored: true},
	}, start, end, now)
	if len(out) != 1 {
		t.Fatalf("expected 1 entry (monitored + in-window + future release), got %d: %+v", len(out), out)
	}
	e := out[0]
	if e.Kind != "movie" || e.SeriesID != 1 || e.SeriesTitle != "In" {
		t.Errorf("wrong entry: %+v", e)
	}
	if !e.AirStart.Equal(time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC)) ||
		!e.AirEnd.Equal(time.Date(2026, 5, 20, 23, 59, 0, 0, time.UTC)) {
		t.Errorf("wrong all-day range: %v .. %v", e.AirStart, e.AirEnd)
	}
	if e.Year != 2026 || e.Status != "unreleased" || e.QualityProfile != "HD-1080p" {
		t.Errorf("wrong extras: %+v", e)
	}
	if e.Season != 0 || e.Episode != 0 || e.SXE != "" || e.EpisodeTitle != "" {
		t.Errorf("movie entries must not carry episode fields: %+v", e)
	}
}

func TestBuildMovies_TodayReleaseIsNotPast(t *testing.T) {
	today := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)
	start, end, now := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC), time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC), time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	out := BuildMovies([]MovieRow{
		{ID: 1, Title: "Today", Year: 2026, ReleaseDate: &today, Monitored: true},
	}, start, end, now)
	if len(out) != 1 {
		t.Fatalf("a release on 'today' is not in the past and must appear, got %+v", out)
	}
	if out[0].Status != "unreleased" {
		t.Errorf("status = %q, want unreleased", out[0].Status)
	}
}

func TestBuildMovies_DownloadedAndQueuedStatuses(t *testing.T) {
	future := time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC)
	start, end, now := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC), time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC), time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	out := BuildMovies([]MovieRow{
		{ID: 1, Title: "Done", Year: 2026, ReleaseDate: &future, Monitored: true, WantedSatisfied: true},
		{ID: 2, Title: "Queued", Year: 2026, ReleaseDate: &future, Monitored: true, InQueue: true},
	}, start, end, now)
	if len(out) != 2 {
		t.Fatalf("expected 2 entries, got %+v", out)
	}
	if out[0].Status != "downloaded:monitored" {
		t.Errorf("status = %q, want downloaded:monitored", out[0].Status)
	}
	if out[1].Status != "queued" {
		t.Errorf("status = %q, want queued", out[1].Status)
	}
}

func TestRenderICal_MovieAllDay(t *testing.T) {
	start, end := ReleaseRange(time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC))
	ics := RenderICal([]Entry{{
		Kind: "movie", SeriesID: 7, SeriesTitle: "Dune 2", Year: 2026,
		AirStart: start, AirEnd: end,
	}}, time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC))
	for _, want := range []string{
		"UID:mediarr-movie-7@mediarr",
		"DTSTART;VALUE=DATE:20260520",
		"DTEND;VALUE=DATE:20260521",
		"SUMMARY:Dune 2 (2026)",
		"DTSTAMP:20260515T120000Z",
	} {
		if !strings.Contains(ics, want) {
			t.Errorf("ICS missing %q\n%s", want, ics)
		}
	}
}

func TestRenderICal_TVUnchanged(t *testing.T) {
	as, ae := AirRange(time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC))
	ics := RenderICal([]Entry{{
		Kind: "tv", SeriesID: 42, SeriesTitle: "Severance", Season: 1, Episode: 5,
		EpisodeTitle: "Episode Title", SXE: "S01E05", AirStart: as, AirEnd: ae,
	}}, time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC))
	for _, want := range []string{
		"UID:mediarr-tv-42-s01-e05@mediarr",
		"DTSTART:20260520T210000Z",
		"SUMMARY:Severance - Episode Title",
	} {
		if !strings.Contains(ics, want) {
			t.Errorf("ICS missing %q\n%s", want, ics)
		}
	}
	if strings.Contains(ics, "VALUE=DATE") {
		t.Errorf("TV feed must not use all-day DATE values\n%s", ics)
	}
}

func TestBuildMusic_FiltersAndShape(t *testing.T) {
	future := time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC)
	past := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	unmon := time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC)
	start, end, now := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC), time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC), time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	out := BuildMusic([]MusicRow{
		{ArtistID: 1, ArtistTitle: "Nirvana", AlbumID: 11, AlbumTitle: "In", AlbumType: "album", Year: 2026, ReleaseDate: &future, Monitored: true, QualityProfile: "Lossless"},
		{ArtistID: 1, ArtistTitle: "Nirvana", AlbumID: 12, AlbumTitle: "Past", Year: 2020, ReleaseDate: &past, Monitored: true},
		{ArtistID: 2, ArtistTitle: "Rage", AlbumID: 13, AlbumTitle: "Unmon", Year: 2026, ReleaseDate: &unmon, Monitored: false},
		{ArtistID: 2, ArtistTitle: "Rage", AlbumID: 14, AlbumTitle: "NoDate", Year: 2026, ReleaseDate: nil, Monitored: true},
	}, start, end, now)
	if len(out) != 1 {
		t.Fatalf("expected 1 entry (monitored + in-window + future release), got %d: %+v", len(out), out)
	}
	e := out[0]
	if e.Kind != "music" || e.SeriesID != 11 || e.SeriesTitle != "In" {
		t.Errorf("wrong entry: %+v", e)
	}
	if !e.AirStart.Equal(time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC)) ||
		!e.AirEnd.Equal(time.Date(2026, 5, 20, 23, 59, 0, 0, time.UTC)) {
		t.Errorf("wrong all-day range: %v .. %v", e.AirStart, e.AirEnd)
	}
	if e.Year != 2026 || e.Status != "unreleased" || e.QualityProfile != "Lossless" {
		t.Errorf("wrong extras: %+v", e)
	}
	if e.ArtistID != 1 || e.ArtistTitle != "Nirvana" || e.AlbumID != 11 || e.AlbumTitle != "In" || e.AlbumType != "album" {
		t.Errorf("wrong music extras: %+v", e)
	}
	if e.Season != 0 || e.Episode != 0 || e.SXE != "" || e.EpisodeTitle != "" {
		t.Errorf("music entries must not carry episode fields: %+v", e)
	}
}

func TestBuildMusic_TodayReleaseIsNotPast(t *testing.T) {
	today := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)
	start, end, now := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC), time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC), time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	out := BuildMusic([]MusicRow{
		{ArtistID: 1, ArtistTitle: "Nirvana", AlbumID: 11, AlbumTitle: "Today", Year: 2026, ReleaseDate: &today, Monitored: true},
	}, start, end, now)
	if len(out) != 1 {
		t.Fatalf("a release on 'today' is not in the past and must appear, got %+v", out)
	}
	if out[0].Status != "unreleased" {
		t.Errorf("status = %q, want unreleased", out[0].Status)
	}
}

func TestRenderICal_MusicAllDay(t *testing.T) {
	start, end := ReleaseRange(time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC))
	ics := RenderICal([]Entry{{
		Kind: "music", SeriesID: 11, SeriesTitle: "Nevermind", Year: 1991,
		ArtistID: 1, ArtistTitle: "Nirvana", AlbumID: 11, AlbumTitle: "Nevermind",
		AirStart: start, AirEnd: end,
	}}, time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC))
	for _, want := range []string{
		"UID:mediarr-music-11@mediarr",
		"DTSTART;VALUE=DATE:20260520",
		"DTEND;VALUE=DATE:20260521",
		"SUMMARY:Nirvana - Nevermind (1991)",
		"DTSTAMP:20260515T120000Z",
	} {
		if !strings.Contains(ics, want) {
			t.Errorf("ICS missing %q\n%s", want, ics)
		}
	}
}
