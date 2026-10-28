package calendar

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	dom "github.com/brandenk514/mediarr/internal/domains/calendar"
)

// fakeRepo implements dom.Repo in memory for service tests.
type fakeRepo struct {
	rows   []dom.EpisodeRow
	movies []dom.MovieRow
	music  []dom.MusicRow
	err    error
}

func (f *fakeRepo) Episodes(ctx context.Context) ([]dom.EpisodeRow, error) {
	return f.rows, f.err
}

func (f *fakeRepo) MovieRows(ctx context.Context) ([]dom.MovieRow, error) {
	return f.movies, f.err
}

func (f *fakeRepo) MusicRows(ctx context.Context) ([]dom.MusicRow, error) {
	return f.music, f.err
}

// fixedNow is the injected "current" time (2026-05-15 noon).
var fixedNow = time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)

func window() (time.Time, time.Time) {
	return time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC), time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC)
}

func newTestService(rows []dom.EpisodeRow) *Service {
	svc := New(&fakeRepo{rows: rows})
	svc.now = func() time.Time { return fixedNow }
	return svc
}

// newTestMovieService builds a service over movie rows for R4 tests.
func newTestMovieService(movies []dom.MovieRow) *Service {
	svc := New(&fakeRepo{movies: movies})
	svc.now = func() time.Time { return fixedNow }
	return svc
}

func tvRow(title string, season, episode int, d time.Time, monitored bool) dom.EpisodeRow {
	t := d
	return dom.EpisodeRow{
		SeriesID: int64(100 + len(title)), SeriesTitle: title, Season: season, Episode: episode,
		EpisodeTitle: "Ep " + title, AirDate: &t, Monitored: monitored,
	}
}

func movieRow(id int64, title string, year int, release *time.Time, monitored, wantedSatisfied, inQueue bool) dom.MovieRow {
	return dom.MovieRow{
		ID: id, Title: title, Year: year, ReleaseDate: release, Monitored: monitored,
		WantedSatisfied: wantedSatisfied, InQueue: inQueue, QualityProfile: "HD-1080p",
	}
}

func TestService_Calendar_Filters(t *testing.T) {
	svc := newTestService([]dom.EpisodeRow{
		tvRow("Future", 1, 1, time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC), true),
		tvRow("Past", 1, 1, time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), true),
		tvRow("Unmon", 1, 1, time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC), false),
	})
	start, end := window()
	entries, err := svc.Calendar(context.Background(), dom.KindTV, start, end)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry (monitored + in-window + future), got %d: %v", len(entries), entries)
	}
	if entries[0].SeriesTitle != "Future" {
		t.Errorf("wrong entry: %q", entries[0].SeriesTitle)
	}
	if entries[0].SXE != "S01E01" {
		t.Errorf("SXE = %q, want S01E01", entries[0].SXE)
	}
	if !entries[0].AirStart.Equal(time.Date(2026, 5, 20, 21, 0, 0, 0, time.UTC)) {
		t.Errorf("AirStart = %v", entries[0].AirStart)
	}
	if !entries[0].AirEnd.Equal(time.Date(2026, 5, 20, 21, 42, 0, 0, time.UTC)) {
		t.Errorf("AirEnd = %v", entries[0].AirEnd)
	}
}

// TestService_Calendar_TVUnchanged pins the P4 regression: an empty kind
// (the "kind param omitted" wire case) behaves exactly like kind=tv.
func TestService_Calendar_TVUnchanged(t *testing.T) {
	rows := []dom.EpisodeRow{
		tvRow("Future", 1, 1, time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC), true),
	}
	start, end := window()
	for _, kind := range []dom.Kind{dom.KindTV, ""} {
		svc := newTestService(rows)
		entries, err := svc.Calendar(context.Background(), kind, start, end)
		if err != nil {
			t.Fatalf("kind %q: %v", kind, err)
		}
		if len(entries) != 1 || entries[0].SeriesTitle != "Future" || entries[0].Kind != "tv" {
			t.Fatalf("kind %q: expected the single TV entry, got %+v", kind, entries)
		}
		if entries[0].Year != 0 || entries[0].Status != "" || entries[0].QualityProfile != "" {
			t.Errorf("kind %q: TV entries must not carry movie extras: %+v", kind, entries[0])
		}
	}
}

func TestService_Calendar_EmptyIsNonNil(t *testing.T) {
	svc := newTestService(nil)
	start, end := window()
	entries, err := svc.Calendar(context.Background(), dom.KindTV, start, end)
	if err != nil {
		t.Fatal(err)
	}
	if entries == nil || len(entries) != 0 {
		t.Errorf("expected empty non-nil slice, got %v", entries)
	}
}

func TestService_Calendar_ReposError(t *testing.T) {
	svc := New(&fakeRepo{err: errors.New("db down")})
	if _, err := svc.Calendar(context.Background(), dom.KindTV, time.Time{}, time.Time{}); err == nil {
		t.Fatal("expected repo error to propagate")
	}
}

func TestService_ICal(t *testing.T) {
	svc := newTestService([]dom.EpisodeRow{
		tvRow("Severance", 1, 5, time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC), true),
	})
	start, end := window()
	ics, err := svc.ICal(context.Background(), dom.KindTV, start, end)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"BEGIN:VCALENDAR",
		"PRODID:-//Mediarr//Calendar 1.0//EN",
		"SUMMARY:Severance - Ep Severance",
	} {
		if !strings.Contains(ics, want) {
			t.Errorf("ICS missing %q\n%s", want, ics)
		}
	}
	if n := strings.Count(ics, "BEGIN:VEVENT"); n != 1 {
		t.Errorf("expected 1 VEVENT, got %d\n%s", n, ics)
	}
}

func TestService_ICal_ReposError(t *testing.T) {
	svc := New(&fakeRepo{err: errors.New("db down")})
	if _, err := svc.ICal(context.Background(), dom.KindTV, time.Time{}, time.Time{}); err == nil {
		t.Fatal("expected repo error to propagate")
	}
}

// TestService_Calendar_MovieFilters is the R4 core case: only monitored
// movies with a known release date inside the window and not in the past
// appear; everything else is excluded.
func TestService_Calendar_MovieFilters(t *testing.T) {
	future := time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC)
	past := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	unmon := time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC)
	outside := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	svc := newTestMovieService([]dom.MovieRow{
		movieRow(1, "Dune 2", 2026, &future, true, false, false),
		movieRow(2, "Old Film", 2020, &past, true, false, false),
		movieRow(3, "Ignored", 2026, &unmon, false, false, false),
		movieRow(4, "No Date", 2026, nil, true, false, false),
		movieRow(5, "Too Far", 2027, &outside, true, false, false),
	})
	start, end := window()
	entries, err := svc.Calendar(context.Background(), dom.KindMovie, start, end)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry (monitored + in-window + future release), got %d: %+v", len(entries), entries)
	}
	e := entries[0]
	if e.Kind != "movie" || e.SeriesID != 1 || e.SeriesTitle != "Dune 2" {
		t.Errorf("wrong entry: %+v", e)
	}
	if e.Year != 2026 || e.Status != "unreleased" || e.QualityProfile != "HD-1080p" {
		t.Errorf("movie extras wrong: %+v", e)
	}
	// All-day range on the release date.
	if !e.AirStart.Equal(time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("AirStart = %v", e.AirStart)
	}
	if !e.AirEnd.Equal(time.Date(2026, 5, 20, 23, 59, 0, 0, time.UTC)) {
		t.Errorf("AirEnd = %v", e.AirEnd)
	}
	if e.Season != 0 || e.Episode != 0 || e.SXE != "" || e.EpisodeTitle != "" {
		t.Errorf("movie entries must not carry episode fields: %+v", e)
	}
}

// TestService_Calendar_MovieBoundaries pins the movie calendar's boundary
// cases: the window end is inclusive (a release ON the end day appears), a
// release on "today" is not in the past (appears), and a release yesterday
// (in the past) or beyond the window end is excluded.
func TestService_Calendar_MovieBoundaries(t *testing.T) {
	today := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)
	yesterday := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC)
	edgeEnd := time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC)
	afterEnd := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	svc := newTestMovieService([]dom.MovieRow{
		movieRow(1, "Today", 2026, &today, true, false, false),
		movieRow(2, "Yesterday", 2026, &yesterday, true, false, false),
		movieRow(3, "EdgeEnd", 2026, &edgeEnd, true, false, false),
		movieRow(4, "AfterEnd", 2027, &afterEnd, true, false, false),
	})
	start, end := window()
	entries, err := svc.Calendar(context.Background(), dom.KindMovie, start, end)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries (today + window end), got %d: %+v", len(entries), entries)
	}
	if entries[0].SeriesTitle != "Today" || entries[1].SeriesTitle != "EdgeEnd" {
		t.Errorf("wrong entries: %+v", entries)
	}
	// A release on today derives 'unreleased', not 'missing'.
	if entries[0].Status != "unreleased" {
		t.Errorf("today status = %q, want unreleased", entries[0].Status)
	}
}

// TestService_Calendar_MovieEmptyIsNonNil pins the empty movie calendar
// (keyless dev: no release dates) as a valid empty non-nil slice.
func TestService_Calendar_MovieEmptyIsNonNil(t *testing.T) {
	svc := newTestMovieService(nil)
	start, end := window()
	entries, err := svc.Calendar(context.Background(), dom.KindMovie, start, end)
	if err != nil {
		t.Fatal(err)
	}
	if entries == nil || len(entries) != 0 {
		t.Errorf("expected empty non-nil slice, got %v", entries)
	}
}

func TestService_Calendar_MovieReposError(t *testing.T) {
	svc := New(&fakeRepo{err: errors.New("db down")})
	if _, err := svc.Calendar(context.Background(), dom.KindMovie, time.Time{}, time.Time{}); err == nil {
		t.Fatal("expected repo error to propagate")
	}
}

// TestService_ICal_Movie is the R4 feed case: all-day VEVENTs with the
// "Title (Year)" summary and a movie UID; the TV feed shape is untouched.
func TestService_ICal_Movie(t *testing.T) {
	future := time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC)
	svc := newTestMovieService([]dom.MovieRow{
		movieRow(1, "Dune 2", 2026, &future, true, false, false),
	})
	start, end := window()
	ics, err := svc.ICal(context.Background(), dom.KindMovie, start, end)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"BEGIN:VCALENDAR",
		"UID:mediarr-movie-1@mediarr",
		"DTSTART;VALUE=DATE:20260520",
		"DTEND;VALUE=DATE:20260521",
		"SUMMARY:Dune 2 (2026)",
	} {
		if !strings.Contains(ics, want) {
			t.Errorf("ICS missing %q\n%s", want, ics)
		}
	}
	if n := strings.Count(ics, "BEGIN:VEVENT"); n != 1 {
		t.Errorf("expected 1 VEVENT, got %d\n%s", n, ics)
	}
}

// newTestMusicService builds a service over music rows for L4 tests.
func newTestMusicService(music []dom.MusicRow) *Service {
	svc := New(&fakeRepo{music: music})
	svc.now = func() time.Time { return fixedNow }
	return svc
}

func musicRows() []dom.MusicRow {
	future := time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC)
	past := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	unmon := time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC)
	return []dom.MusicRow{
		{ArtistID: 1, ArtistTitle: "Nirvana", AlbumID: 11, AlbumTitle: "AlbumA", AlbumType: "album", Year: 2026, ReleaseDate: &future, Monitored: true, QualityProfile: "Lossless"},
		{ArtistID: 1, ArtistTitle: "Nirvana", AlbumID: 12, AlbumTitle: "AlbumB", Year: 2026, ReleaseDate: &future, Monitored: true, QualityProfile: "Lossless"},
		{ArtistID: 2, ArtistTitle: "Rage", AlbumID: 13, AlbumTitle: "Past", Year: 2020, ReleaseDate: &past, Monitored: true},
		{ArtistID: 2, ArtistTitle: "Rage", AlbumID: 14, AlbumTitle: "Unmon", Year: 2026, ReleaseDate: &unmon, Monitored: false},
		{ArtistID: 2, ArtistTitle: "Rage", AlbumID: 15, AlbumTitle: "NoDate", Year: 2026, ReleaseDate: nil, Monitored: true},
	}
}

// TestService_Calendar_MusicFilters verifies kind=music returns the monitored
// albums with a future release date in the window (L4 music calendar).
func TestService_Calendar_MusicFilters(t *testing.T) {
	svc := newTestMusicService(musicRows())
	start, end := window()
	entries, err := svc.Calendar(context.Background(), dom.KindMusic, start, end)
	if err != nil {
		t.Fatalf("calendar: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries (monitored + in-window + future), got %d: %+v", len(entries), entries)
	}
	for _, e := range entries {
		if e.Kind != "music" || e.Status != "unreleased" {
			t.Errorf("entry kind/status = %q/%q, want music/unreleased: %+v", e.Kind, e.Status, e)
		}
		if e.SeriesID != e.AlbumID || e.SeriesTitle != e.AlbumTitle {
			t.Errorf("entry must carry the album as its primary identity: %+v", e)
		}
	}
}

// TestService_Calendar_MusicEmptyIsNonNil pins the empty-window contract.
func TestService_Calendar_MusicEmptyIsNonNil(t *testing.T) {
	svc := newTestMusicService(nil)
	start, end := window()
	entries, err := svc.Calendar(context.Background(), dom.KindMusic, start, end)
	if err != nil {
		t.Fatalf("calendar: %v", err)
	}
	if entries == nil {
		t.Fatal("empty music calendar must be a non-nil slice")
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(entries))
	}
}

// TestService_ICal_Music verifies the iCal feed for kind=music renders
// all-day events with the artist-qualified summary.
func TestService_ICal_Music(t *testing.T) {
	svc := newTestMusicService(musicRows())
	start, end := window()
	ics, err := svc.ICal(context.Background(), dom.KindMusic, start, end)
	if err != nil {
		t.Fatalf("ical: %v", err)
	}
	for _, want := range []string{
		"BEGIN:VCALENDAR",
		"UID:mediarr-music-11@mediarr",
		"SUMMARY:Nirvana - AlbumA (2026)",
		"DTSTART;VALUE=DATE:",
	} {
		if !strings.Contains(ics, want) {
			t.Errorf("music feed missing %q:\n%s", want, ics)
		}
	}
	if n := strings.Count(ics, "BEGIN:VEVENT"); n != 2 {
		t.Errorf("expected 2 VEVENTs, got %d\n%s", n, ics)
	}
}
