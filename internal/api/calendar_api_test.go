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
type memCalendarRepo struct{ rows []cdom.EpisodeRow }

func (m *memCalendarRepo) Episodes(_ context.Context) ([]cdom.EpisodeRow, error) { return m.rows, nil }

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
