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
	rows []dom.EpisodeRow
	err  error
}

func (f *fakeRepo) Episodes(ctx context.Context) ([]dom.EpisodeRow, error) { return f.rows, f.err }

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

func tvRow(title string, season, episode int, d time.Time, monitored bool) dom.EpisodeRow {
	t := d
	return dom.EpisodeRow{
		SeriesID: int64(100 + len(title)), SeriesTitle: title, Season: season, Episode: episode,
		EpisodeTitle: "Ep " + title, AirDate: &t, Monitored: monitored,
	}
}

func TestService_Calendar_Filters(t *testing.T) {
	svc := newTestService([]dom.EpisodeRow{
		tvRow("Future", 1, 1, time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC), true),
		tvRow("Past", 1, 1, time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), true),
		tvRow("Unmon", 1, 1, time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC), false),
	})
	start, end := window()
	entries, err := svc.Calendar(context.Background(), start, end)
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

func TestService_Calendar_EmptyIsNonNil(t *testing.T) {
	svc := newTestService(nil)
	start, end := window()
	entries, err := svc.Calendar(context.Background(), start, end)
	if err != nil {
		t.Fatal(err)
	}
	if entries == nil || len(entries) != 0 {
		t.Errorf("expected empty non-nil slice, got %v", entries)
	}
}

func TestService_Calendar_ReposError(t *testing.T) {
	svc := New(&fakeRepo{err: errors.New("db down")})
	if _, err := svc.Calendar(context.Background(), time.Time{}, time.Time{}); err == nil {
		t.Fatal("expected repo error to propagate")
	}
}

func TestService_ICal(t *testing.T) {
	svc := newTestService([]dom.EpisodeRow{
		tvRow("Severance", 1, 5, time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC), true),
	})
	start, end := window()
	ics, err := svc.ICal(context.Background(), start, end)
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
	if _, err := svc.ICal(context.Background(), time.Time{}, time.Time{}); err == nil {
		t.Fatal("expected repo error to propagate")
	}
}
