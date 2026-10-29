package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/brandenk514/mediarr/internal/auth"
	cdom "github.com/brandenk514/mediarr/internal/domains/calendar"
	"github.com/brandenk514/mediarr/internal/health"
	calsvc "github.com/brandenk514/mediarr/internal/services/calendar"
)

// memCalendarRepo is an in-memory calendar.Repo for the endpoint contract tests.
type memCalendarRepo struct {
	rows   []cdom.EpisodeRow
	movies []cdom.MovieRow
	music  []cdom.MusicRow
}

func (m *memCalendarRepo) Episodes(_ context.Context) ([]cdom.EpisodeRow, error) {
	return m.rows, nil
}

func (m *memCalendarRepo) MovieRows(_ context.Context) ([]cdom.MovieRow, error) {
	return m.movies, nil
}

func (m *memCalendarRepo) MusicRows(_ context.Context) ([]cdom.MusicRow, error) {
	return m.music, nil
}

// newCalendarTestServer wires a fresh server with a minted bearer token and a
// calendar service over the in-memory repo. A new server per call avoids the
// mux double-register panic when the same route is registered twice.
func newCalendarTestServer(t *testing.T, repo *memCalendarRepo) (*Server, string) {
	t.Helper()
	store := newFakeStore()
	u, _ := store.CreateUser(context.Background(), "admin", "", "admin", "hash")
	pair, err := auth.NewToken("ma_live_")
	if err != nil {
		t.Fatalf("new token: %v", err)
	}
	if _, err := store.CreateToken(context.Background(), u, "test", pair, nil); err != nil {
		t.Fatalf("create token: %v", err)
	}
	s := NewServer(health.NewChecker(nil, nil))
	s.SetAuth(&AuthDeps{Repo: store})
	s.SetCalendar(&CalendarDeps{Svc: calsvc.New(repo)})
	return s, pair.Raw
}

// calendarRows returns rows anchored to the real current time so the handler's
// future cutoff (time.Now) is deterministic.
func calendarRows() []cdom.EpisodeRow {
	now := time.Now().UTC()
	air := func(d int) *time.Time {
		t := now.AddDate(0, 0, d)
		return &t
	}
	return []cdom.EpisodeRow{
		{SeriesID: 1, SeriesTitle: "ShowA", Season: 1, Episode: 1, EpisodeTitle: "Pilot", AirDate: air(1), Monitored: true},
		{SeriesID: 2, SeriesTitle: "ShowB", Season: 2, Episode: 3, EpisodeTitle: "Two", AirDate: air(2), Monitored: true},
		{SeriesID: 3, SeriesTitle: "ShowC", Season: 1, Episode: 1, EpisodeTitle: "Past", AirDate: air(-1), Monitored: true},
		{SeriesID: 4, SeriesTitle: "ShowD", Season: 1, Episode: 1, EpisodeTitle: "Unmon", AirDate: air(1), Monitored: false},
	}
}

func TestCalendar_List(t *testing.T) {
	now := time.Now().UTC()
	q := "?" + "start=" + now.Format(time.RFC3339) + "&end=" + now.AddDate(0, 0, 30).Format(time.RFC3339)
	s, token := newCalendarTestServer(t, &memCalendarRepo{rows: calendarRows()})
	rec := doReq(t, s.Handler(), http.MethodGet, "/api/v1/calendar"+q, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d; body %s", rec.Code, rec.Body.String())
	}
	var entries []cdom.Entry
	if err := json.Unmarshal(rec.Body.Bytes(), &entries); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries (monitored + in-window + future), got %d: %v", len(entries), entries)
	}
	if entries[0].SeriesTitle != "ShowA" || entries[0].SXE != "S01E01" {
		t.Errorf("first entry = %+v, want ShowA / S01E01", entries[0])
	}
	if entries[0].AirStart.IsZero() || entries[0].AirEnd.IsZero() || !entries[0].AirEnd.After(entries[0].AirStart) {
		t.Errorf("air range not populated: %v .. %v", entries[0].AirStart, entries[0].AirEnd)
	}
	if entries[0].Kind != "tv" {
		t.Errorf("kind = %q, want tv", entries[0].Kind)
	}
}

func TestCalendar_List_EmptyIs200Array(t *testing.T) {
	// A window entirely in the past: everything is excluded by the future
	// filter, so the response is a valid 200 [] (not null, not 4xx).
	now := time.Now().UTC()
	q := "?start=" + now.AddDate(0, 0, -60).Format(time.RFC3339) + "&end=" + now.AddDate(0, 0, -30).Format(time.RFC3339)
	s, token := newCalendarTestServer(t, &memCalendarRepo{rows: calendarRows()})
	rec := doReq(t, s.Handler(), http.MethodGet, "/api/v1/calendar"+q, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d; body %s", rec.Code, rec.Body.String())
	}
	body := strings.TrimSpace(rec.Body.String())
	if body != "[]" {
		t.Fatalf("empty window body = %q, want []", body)
	}
}

func TestCalendar_List_DefaultWindow(t *testing.T) {
	// No start/end -> today .. today+30d. ShowA/ShowB (future) are in it.
	s, token := newCalendarTestServer(t, &memCalendarRepo{rows: calendarRows()})
	rec := doReq(t, s.Handler(), http.MethodGet, "/api/v1/calendar", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d; body %s", rec.Code, rec.Body.String())
	}
	var entries []cdom.Entry
	if err := json.Unmarshal(rec.Body.Bytes(), &entries); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("default window: expected 2 entries, got %d", len(entries))
	}
}

func TestCalendar_List_RequiresAuth(t *testing.T) {
	s, _ := newCalendarTestServer(t, &memCalendarRepo{rows: calendarRows()})
	// No token.
	rec := doReq(t, s.Handler(), http.MethodGet, "/api/v1/calendar", "")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: got %d, want 401", rec.Code)
	}
	// Invalid token.
	rec = doReq(t, s.Handler(), http.MethodGet, "/api/v1/calendar", "ma_invalid")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("invalid token: got %d, want 401", rec.Code)
	}
}

func TestCalendar_Feed_QueryToken(t *testing.T) {
	s, token := newCalendarTestServer(t, &memCalendarRepo{rows: calendarRows()})
	rec := doReq(t, s.Handler(), http.MethodGet, "/calendar.ics?token="+token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d; body %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/calendar") {
		t.Errorf("Content-Type = %q, want text/calendar", ct)
	}
	body := rec.Body.String()
	for _, want := range []string{"BEGIN:VCALENDAR", "BEGIN:VEVENT", "PRODID:-//Mediarr//Calendar 1.0//EN"} {
		if !strings.Contains(body, want) {
			t.Errorf("feed missing %q:\n%s", want, body)
		}
	}
	// Only the 2 monitored + future rows become events.
	if n := strings.Count(body, "BEGIN:VEVENT"); n != 2 {
		t.Errorf("expected 2 VEVENTs, got %d\n%s", n, body)
	}
}

func TestCalendar_Feed_HeaderToken(t *testing.T) {
	s, token := newCalendarTestServer(t, &memCalendarRepo{rows: calendarRows()})
	rec := doReq(t, s.Handler(), http.MethodGet, "/calendar.ics", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("header token: got %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "BEGIN:VCALENDAR") {
		t.Errorf("feed missing VCALENDAR:\n%s", rec.Body.String())
	}
}

func TestCalendar_Feed_RequiresAuth(t *testing.T) {
	s, _ := newCalendarTestServer(t, &memCalendarRepo{rows: calendarRows()})
	// No header token and no query token.
	rec := doReq(t, s.Handler(), http.MethodGet, "/calendar.ics", "")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: got %d, want 401", rec.Code)
	}
	// Invalid query token.
	rec = doReq(t, s.Handler(), http.MethodGet, "/calendar.ics?token=ma_invalid", "")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("invalid query token: got %d, want 401", rec.Code)
	}
}

// movieCalendarRows returns R4 movie rows anchored to the real current time
// (the handler's future cutoff is time.Now).
func movieCalendarRows() []cdom.MovieRow {
	now := time.Now().UTC()
	rel := func(d int) *time.Time {
		t := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, d)
		return &t
	}
	return []cdom.MovieRow{
		{ID: 1, Title: "MovieA", Year: 2026, ReleaseDate: rel(1), Monitored: true, QualityProfile: "HD-1080p"},
		{ID: 2, Title: "MovieB", Year: 2026, ReleaseDate: rel(2), Monitored: true, QualityProfile: "HD-1080p"},
		{ID: 3, Title: "MoviePast", Year: 2020, ReleaseDate: rel(-1), Monitored: true},
		{ID: 4, Title: "MovieUnmon", Year: 2026, ReleaseDate: rel(1), Monitored: false},
		{ID: 5, Title: "MovieNoDate", Year: 2026, ReleaseDate: nil, Monitored: true},
	}
}

func TestCalendar_List_Movie(t *testing.T) {
	now := time.Now().UTC()
	q := "?kind=movie&start=" + now.Format(time.RFC3339) + "&end=" + now.AddDate(0, 0, 30).Format(time.RFC3339)
	s, token := newCalendarTestServer(t, &memCalendarRepo{movies: movieCalendarRows()})
	rec := doReq(t, s.Handler(), http.MethodGet, "/api/v1/calendar"+q, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d; body %s", rec.Code, rec.Body.String())
	}
	var entries []cdom.Entry
	if err := json.Unmarshal(rec.Body.Bytes(), &entries); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries (monitored + in-window + future release), got %d: %+v", len(entries), entries)
	}
	if entries[0].Kind != "movie" {
		t.Errorf("kind = %q, want movie", entries[0].Kind)
	}
	if entries[0].SeriesTitle != "MovieA" || entries[0].SeriesID != 1 {
		t.Errorf("first entry = %+v, want MovieA / 1", entries[0])
	}
	if entries[0].Year != 2026 || entries[0].Status != "unreleased" || entries[0].QualityProfile != "HD-1080p" {
		t.Errorf("movie extras = %+v, want year 2026 / unreleased / HD-1080p", entries[0])
	}
	if !entries[0].AirStart.Equal(entries[0].AirEnd.Add(-23*time.Hour - 59*time.Minute)) {
		t.Errorf("all-day range not populated: %v .. %v", entries[0].AirStart, entries[0].AirEnd)
	}
	if entries[0].Season != 0 || entries[0].Episode != 0 || entries[0].SXE != "" {
		t.Errorf("movie entries must not carry episode fields: %+v", entries[0])
	}
}

func TestCalendar_List_MovieEmptyIs200Array(t *testing.T) {
	// No release dates at all (keyless dev) -> a valid 200 [].
	s, token := newCalendarTestServer(t, &memCalendarRepo{})
	rec := doReq(t, s.Handler(), http.MethodGet, "/api/v1/calendar?kind=movie", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d; body %s", rec.Code, rec.Body.String())
	}
	body := strings.TrimSpace(rec.Body.String())
	if body != "[]" {
		t.Fatalf("empty movie calendar body = %q, want []", body)
	}
}

// TestCalendar_List_TVUnchanged pins the P4 wire regression at the endpoint:
// an explicit kind=tv is byte-for-byte the pre-R4 episode calendar.
func TestCalendar_List_TVUnchanged(t *testing.T) {
	now := time.Now().UTC()
	q := "?kind=tv&start=" + now.Format(time.RFC3339) + "&end=" + now.AddDate(0, 0, 30).Format(time.RFC3339)
	s, token := newCalendarTestServer(t, &memCalendarRepo{rows: calendarRows()})
	rec := doReq(t, s.Handler(), http.MethodGet, "/api/v1/calendar"+q, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d; body %s", rec.Code, rec.Body.String())
	}
	var entries []cdom.Entry
	if err := json.Unmarshal(rec.Body.Bytes(), &entries); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("kind=tv: expected the 2 episode entries, got %d", len(entries))
	}
	for _, e := range entries {
		if e.Kind != "tv" || e.Year != 0 || e.Status != "" || e.QualityProfile != "" {
			t.Errorf("TV entries must not carry movie extras: %+v", e)
		}
	}
}

func TestCalendar_Feed_Movie(t *testing.T) {
	s, token := newCalendarTestServer(t, &memCalendarRepo{movies: movieCalendarRows()})
	rec := doReq(t, s.Handler(), http.MethodGet, "/calendar.ics?kind=movie&token="+token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d; body %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"BEGIN:VCALENDAR",
		"UID:mediarr-movie-1@mediarr",
		"SUMMARY:MovieA (2026)",
		"DTSTART;VALUE=DATE:",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("movie feed missing %q:\n%s", want, body)
		}
	}
	// Only the 2 monitored + future releases become all-day events.
	if n := strings.Count(body, "BEGIN:VEVENT"); n != 2 {
		t.Errorf("expected 2 VEVENTs, got %d\n%s", n, body)
	}
}

// TestCalendar_Feed_TVUnchanged pins the P4 feed regression: kind=tv (and
// the omitted-kind default) still renders the dated episode events.
func TestCalendar_Feed_TVUnchanged(t *testing.T) {
	s, token := newCalendarTestServer(t, &memCalendarRepo{rows: calendarRows()})
	rec := doReq(t, s.Handler(), http.MethodGet, "/calendar.ics?kind=tv&token="+token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d; body %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"UID:mediarr-tv-1-s01-e01@mediarr", "SUMMARY:ShowA - Pilot", "DTSTART:2"} {
		if !strings.Contains(body, want) {
			t.Errorf("TV feed missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "VALUE=DATE") {
		t.Errorf("TV feed must not use all-day DATE values:\n%s", body)
	}
}

// musicCalendarRows returns music rows anchored to the real current time so
// the handler's future cutoff (time.Now) is deterministic.
func musicCalendarRows() []cdom.MusicRow {
	now := time.Now().UTC()
	rel := func(d int) *time.Time {
		t := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, d)
		return &t
	}
	return []cdom.MusicRow{
		{ArtistID: 1, ArtistTitle: "Nirvana", AlbumID: 11, AlbumTitle: "AlbumA", AlbumType: "album", Year: 2026, ReleaseDate: rel(1), Monitored: true, QualityProfile: "Lossless"},
		{ArtistID: 1, ArtistTitle: "Nirvana", AlbumID: 12, AlbumTitle: "AlbumB", Year: 2026, ReleaseDate: rel(2), Monitored: true, QualityProfile: "Lossless"},
		{ArtistID: 2, ArtistTitle: "Rage", AlbumID: 13, AlbumTitle: "AlbumPast", Year: 2020, ReleaseDate: rel(-1), Monitored: true},
		{ArtistID: 2, ArtistTitle: "Rage", AlbumID: 14, AlbumTitle: "AlbumUnmon", Year: 2026, ReleaseDate: rel(1), Monitored: false},
		{ArtistID: 2, ArtistTitle: "Rage", AlbumID: 15, AlbumTitle: "AlbumNoDate", Year: 2026, ReleaseDate: nil, Monitored: true},
	}
}

// TestCalendar_List_Music is the L4 endpoint contract: kind=music returns
// the upcoming monitored-album releases with the music entry shape (album as
// the primary identity, artist in the music extras).
func TestCalendar_List_Music(t *testing.T) {
	now := time.Now().UTC()
	q := "?kind=music&start=" + now.Format(time.RFC3339) + "&end=" + now.AddDate(0, 0, 30).Format(time.RFC3339)
	s, token := newCalendarTestServer(t, &memCalendarRepo{music: musicCalendarRows()})
	rec := doReq(t, s.Handler(), http.MethodGet, "/api/v1/calendar"+q, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d; body %s", rec.Code, rec.Body.String())
	}
	var entries []cdom.Entry
	if err := json.Unmarshal(rec.Body.Bytes(), &entries); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries (monitored + in-window + future release), got %d: %+v", len(entries), entries)
	}
	if entries[0].Kind != "music" {
		t.Errorf("kind = %q, want music", entries[0].Kind)
	}
	if entries[0].SeriesID != 11 || entries[0].SeriesTitle != "AlbumA" {
		t.Errorf("first entry = %+v, want AlbumA / 11", entries[0])
	}
	if entries[0].ArtistID != 1 || entries[0].ArtistTitle != "Nirvana" || entries[0].AlbumID != 11 || entries[0].AlbumTitle != "AlbumA" || entries[0].AlbumType != "album" {
		t.Errorf("music extras = %+v, want Nirvana / AlbumA / album", entries[0])
	}
	if entries[0].Year != 2026 || entries[0].Status != "unreleased" || entries[0].QualityProfile != "Lossless" {
		t.Errorf("music entry extras = %+v, want year 2026 / unreleased / Lossless", entries[0])
	}
	if !entries[0].AirStart.Equal(entries[0].AirEnd.Add(-23*time.Hour - 59*time.Minute)) {
		t.Errorf("all-day range not populated: %v .. %v", entries[0].AirStart, entries[0].AirEnd)
	}
}

// TestCalendar_List_MusicEmptyIs200Array: no release dates at all (keyless
// dev) -> a valid 200 [].
func TestCalendar_List_MusicEmptyIs200Array(t *testing.T) {
	s, token := newCalendarTestServer(t, &memCalendarRepo{})
	rec := doReq(t, s.Handler(), http.MethodGet, "/api/v1/calendar?kind=music", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d; body %s", rec.Code, rec.Body.String())
	}
	body := strings.TrimSpace(rec.Body.String())
	if body != "[]" {
		t.Fatalf("empty music calendar body = %q, want []", body)
	}
}

// TestCalendar_List_TVUnchanged_L4 pins the P4 wire regression against the
// L4 additions: TV entries must not carry the music extras.
func TestCalendar_List_TVNoMusicExtras(t *testing.T) {
	now := time.Now().UTC()
	q := "?kind=tv&start=" + now.Format(time.RFC3339) + "&end=" + now.AddDate(0, 0, 30).Format(time.RFC3339)
	s, token := newCalendarTestServer(t, &memCalendarRepo{rows: calendarRows()})
	rec := doReq(t, s.Handler(), http.MethodGet, "/api/v1/calendar"+q, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d; body %s", rec.Code, rec.Body.String())
	}
	var entries []cdom.Entry
	if err := json.Unmarshal(rec.Body.Bytes(), &entries); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, e := range entries {
		if e.ArtistID != 0 || e.ArtistTitle != "" || e.AlbumID != 0 || e.AlbumTitle != "" || e.AlbumType != "" {
			t.Errorf("TV entries must not carry music extras: %+v", e)
		}
	}
}

func TestCalendar_Feed_Music(t *testing.T) {
	s, token := newCalendarTestServer(t, &memCalendarRepo{music: musicCalendarRows()})
	rec := doReq(t, s.Handler(), http.MethodGet, "/calendar.ics?kind=music&token="+token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d; body %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"BEGIN:VCALENDAR",
		"UID:mediarr-music-11@mediarr",
		"SUMMARY:Nirvana - AlbumA (2026)",
		"DTSTART;VALUE=DATE:",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("music feed missing %q:\n%s", want, body)
		}
	}
	// Only the 2 monitored + future releases become all-day events.
	if n := strings.Count(body, "BEGIN:VEVENT"); n != 2 {
		t.Errorf("expected 2 VEVENTs, got %d\n%s", n, body)
	}
}
