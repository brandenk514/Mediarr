package api

import (
	"net/http"
	"strings"
	"time"

	calDom "github.com/brandenk514/mediarr/internal/domains/calendar"
	calSvc "github.com/brandenk514/mediarr/internal/services/calendar"
)

// CalendarDeps wires the P4 calendar routes (the JSON list + the iCal feed).
type CalendarDeps struct {
	Svc *calSvc.Service
}

// SetCalendar registers the P4 calendar endpoints.
//
//	GET /api/v1/calendar  -> JSON list of upcoming monitored air dates (bearer)
//	GET /calendar.ics     -> RFC 5545 iCal feed (bearer header or ?token=)
func (s *Server) SetCalendar(deps *CalendarDeps) {
	s.calendarDeps = deps
	s.register("GET /api/v1/calendar", s.requireAuth(s.ListCalendar))
	s.register("GET /calendar.ics", s.calendarFeedAuth(s.CalendarFeed))
}

// calendarDefaultDays is the fallback window length when start/end are missing
// or invalid (today .. today+30d).
const calendarDefaultDays = 30

// parseCalendarWindow sanitizes and clamps the start/end query params into a
// concrete window: a missing or invalid start falls back to today (start of
// day, UTC); a missing or invalid end falls back to start + calendarDefaultDays;
// an end that precedes the start is clamped back to start + calendarDefaultDays.
func parseCalendarWindow(r *http.Request, now time.Time) (start, end time.Time) {
	now = now.UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	q := r.URL.Query()

	start = today
	if v := q.Get("start"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			start = t.UTC()
		}
	}
	end = start.Add(calendarDefaultDays * 24 * time.Hour)
	if v := q.Get("end"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			end = t.UTC()
		}
	}
	if end.Before(start) {
		end = start.Add(calendarDefaultDays * 24 * time.Hour)
	}
	return start, end
}

// ListCalendar handles GET /api/v1/calendar: the upcoming monitored air dates
// in the requested window. An empty window is a valid 200 [].
func (s *Server) ListCalendar(w http.ResponseWriter, r *http.Request) {
	start, end := parseCalendarWindow(r, time.Now())
	entries, err := s.calendarDeps.Svc.Calendar(r.Context(), start, end)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "loading calendar")
		return
	}
	if entries == nil {
		entries = []calDom.Entry{}
	}
	writeJSON(w, http.StatusOK, entries)
}

// CalendarFeed handles GET /calendar.ics: the RFC 5545 iCal document for the
// window, served as a download (Content-Disposition: attachment).
func (s *Server) CalendarFeed(w http.ResponseWriter, r *http.Request) {
	start, end := parseCalendarWindow(r, time.Now())
	ics, err := s.calendarDeps.Svc.ICal(r.Context(), start, end)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "building calendar feed")
		return
	}
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="mediarr-calendar.ics"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(ics)) //nolint:gosec // G705: this is a text/calendar data feed, not an HTML document — calendar clients parse it as structured iCalendar, so there is no HTML/JS injection surface
}

// calendarFeedAuth authenticates a file-download route that a browser fetches
// as a plain navigation (no Authorization header on an <a>/calendar-client
// link). The token may arrive as a standard bearer header or a ?token= query
// param — the same trade-off documented on /api/v1/events.
func (s *Server) calendarFeedAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw := bearerToken(r)
		if raw == "" {
			raw = strings.TrimSpace(r.URL.Query().Get("token"))
		}
		if raw == "" {
			writeError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		if _, _, err := s.authDeps.Repo.TokenUser(r.Context(), raw); err != nil {
			writeError(w, http.StatusUnauthorized, "invalid token")
			return
		}
		next(w, r)
	}
}
