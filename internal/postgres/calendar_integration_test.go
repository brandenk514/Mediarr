package postgres

import (
	"context"
	"testing"
	"time"
)

// TestP4_Calendar_Episodes is the P4 repo contract against a real Postgres
// container: Episodes() joins each episode with its series title, returns the
// monitor flag and air date, excludes episodes with no air date, and orders
// chronologically (air date, then show, season, episode).
func TestP4_Calendar_Episodes(t *testing.T) {
	db := newTestPostgres(t)
	ctx := context.Background()
	st := NewWithDB(db)
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := NewCalendarRepo(db)

	apply := func(s string) {
		if _, err := db.ExecContext(ctx, s); err != nil {
			t.Fatalf("seed %q: %v", s, err)
		}
	}
	apply(`INSERT INTO tv_series (title, year) VALUES ('The Wire', 2002)`)
	apply(`INSERT INTO tv_series (title, year) VALUES ('Slow Horses', 2022)`)
	apply(`INSERT INTO tv_episodes (series_id, season, episode, title, air_date, monitored) VALUES
		(1, 1, 1, 'Pilot',  '2026-06-01', TRUE),
		(1, 1, 2, 'Second', '2026-06-08', TRUE),
		(1, 2, 1, 'S2E1',   '2026-07-01', FALSE),
		(2, 1, 1, 'First',  '2026-06-15', TRUE),
		(2, 1, 2, 'NoDate', NULL,         TRUE)`)

	rows, err := repo.Episodes(ctx)
	if err != nil {
		t.Fatalf("episodes: %v", err)
	}
	// The NULL-air-date row (Slow Horses 1x2) is excluded -> 4 rows.
	if len(rows) != 4 {
		t.Fatalf("len = %d, want 4: %+v", len(rows), rows)
	}

	// Chronological order: Wire 1x1 (06-01), Wire 1x2 (06-08), Slow 1x1 (06-15),
	// Wire 2x1 (07-01).
	wantOrder := []struct {
		title   string
		season  int
		episode int
	}{
		{"The Wire", 1, 1},
		{"The Wire", 1, 2},
		{"Slow Horses", 1, 1},
		{"The Wire", 2, 1},
	}
	for i, w := range wantOrder {
		if rows[i].SeriesTitle != w.title || rows[i].Season != w.season || rows[i].Episode != w.episode {
			t.Errorf("row %d = %s %dx%d, want %s %dx%d",
				i, rows[i].SeriesTitle, rows[i].Season, rows[i].Episode, w.title, w.season, w.episode)
		}
	}

	// Join brought in the title; air date scanned as a real date; monitor flag
	// passed through.
	if rows[0].AirDate == nil {
		t.Fatal("row 0 air date nil")
	}
	y, m, d := rows[0].AirDate.Date()
	if y != 2026 || m != time.June || d != 1 {
		t.Errorf("row 0 air date = %v, want 2026-06-01", rows[0].AirDate)
	}
	// The unmonitored episode is still returned (the domain filters it); the
	// flag must be false.
	if rows[3].Monitored {
		t.Errorf("row 3 (Wire 2x1) should be unmonitored, got monitored")
	}
	if !rows[0].Monitored || !rows[1].Monitored || !rows[2].Monitored {
		t.Errorf("rows 0-2 should be monitored")
	}
}
