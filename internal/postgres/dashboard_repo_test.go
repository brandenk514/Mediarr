package postgres

import (
	"context"
	"testing"

	"github.com/brandenk514/mediarr/internal/dashboard"
)

// TestDashboardRepo_Snapshot is the #40 dashboard aggregation contract against
// a real Postgres container: seed every kind with a known mix of libraries,
// wants (pending + satisfied), and queue jobs (in-flight + terminal), and
// assert the rollup, the cross-kind totals, and the unified recent-history /
// recent-queue feeds.
func TestDashboardRepo_Snapshot(t *testing.T) {
	db := newTestPostgres(t)
	ctx := context.Background()

	st := NewWithDB(db)
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := NewDashboardRepo(db)

	apply := func(s string) {
		if _, err := db.ExecContext(ctx, s); err != nil {
			t.Fatalf("seed %q: %v", s, err)
		}
	}

	// Movies: 2 movies, 1 pending + 1 satisfied want, 2 queue jobs (1 in
	// flight, 1 complete).
	apply(`INSERT INTO movies (title, year) VALUES
		('Inception', 2010), ('Interstellar', 2014)`)
	apply(`INSERT INTO wanted (movie_id, status) VALUES (1, 'pending'), (2, 'satisfied')`)
	apply(`INSERT INTO queue (movie_id, release_title, indexer, state, progress) VALUES
		(1, 'Inception.2010.1080p.WEB.x264', 'nyaa', 'downloading', 42),
		(2, 'Interstellar.2014.2160p.UHD.HDR', 'rarbg', 'complete', 100)`)
	apply(`INSERT INTO history (movie_id, event, detail) VALUES
		(1, 'imported', 'imported to /media/movies/Inception (2010)/Inception.mkv')`)

	// TV: 1 series, 2 pending episode wants, 1 queued job.
	apply(`INSERT INTO tv_series (title, year) VALUES ('The Wire', 2002)`)
	apply(`INSERT INTO tv_wanted (series_id, season, episode, status) VALUES
		(1, 1, 1, 'pending'), (1, 1, 2, 'pending')`)
	apply(`INSERT INTO tv_queue (series_id, season, episode, release_title, indexer, state, progress) VALUES
		(1, 1, 0, 'The.Wire.S01.COMPLETE.1080p.WEB.x264', 'fake', 'queued', 0)`)
	apply(`INSERT INTO tv_history (series_id, event, detail) VALUES
		(1, 'added', 'series added to library')`)

	// Music: 1 artist + 1 album, 1 pending album want, 1 failed job.
	apply(`INSERT INTO music_artists (name) VALUES ('Nirvana')`)
	apply(`INSERT INTO music_albums (artist_id, name, year) VALUES (1, 'Nevermind', 1991)`)
	apply(`INSERT INTO music_wanted (album_id, track_id, status) VALUES (1, 0, 'pending')`)
	apply(`INSERT INTO music_queue (artist_id, album_id, release_title, indexer, state, progress) VALUES
		(1, 1, 'Nirvana (1991) - Nevermind [FLAC 1411kbps]', 'fake', 'failed', 0)`)
	apply(`INSERT INTO music_history (artist_id, event, detail) VALUES
		(1, 'added', 'artist added to library')`)

	// Books: 1 author + 1 title, 1 pending title want, no queue jobs.
	apply(`INSERT INTO book_authors (name) VALUES ('Ursula K. Le Guin')`)
	apply(`INSERT INTO book_titles (author_id, name) VALUES (1, 'The Dispossessed')`)
	apply(`INSERT INTO book_wanted (title_id, edition_id, status) VALUES (1, 0, 'pending')`)
	apply(`INSERT INTO book_history (author_id, event, detail) VALUES
		(1, 'added', 'author added to library')`)

	snap, err := repo.Snapshot(ctx, 10)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	byKind := map[dashboard.Kind]dashboard.Library{}
	for _, l := range snap.Libraries {
		byKind[l.Kind] = l
	}
	if len(byKind) != 4 {
		t.Fatalf("expected 4 kinds, got %d: %+v", len(byKind), snap.Libraries)
	}

	checks := []struct {
		kind      dashboard.Kind
		label     string
		count     int64
		wants     int64
		inflight  int64
	}{
		{dashboard.KindMovie, "Movies", 2, 1, 1},
		{dashboard.KindTV, "TV", 1, 2, 1},
		{dashboard.KindMusic, "Music", 1, 1, 0}, // failed job is not in flight
		{dashboard.KindBook, "Books", 1, 1, 0},
	}
	for _, c := range checks {
		l, ok := byKind[c.kind]
		if !ok {
			t.Fatalf("missing kind %s", c.kind)
		}
		if l.Label != c.label {
			t.Errorf("%s: label = %q, want %q", c.kind, l.Label, c.label)
		}
		if l.Count != c.count {
			t.Errorf("%s: count = %d, want %d", c.kind, l.Count, c.count)
		}
		if l.Wants != c.wants {
			t.Errorf("%s: wants = %d, want %d", c.kind, l.Wants, c.wants)
		}
		if l.Queue != c.inflight {
			t.Errorf("%s: in-flight queue = %d, want %d", c.kind, l.Queue, c.inflight)
		}
	}

	// Cross-library totals: 5 libraries, 5 active wants, 2 in-flight jobs.
	if snap.Totals.Libraries != 5 {
		t.Errorf("totals.libraries = %d, want 5", snap.Totals.Libraries)
	}
	if snap.Totals.Wants != 5 {
		t.Errorf("totals.wants = %d, want 5", snap.Totals.Wants)
	}
	if snap.Totals.Queue != 2 {
		t.Errorf("totals.queue = %d, want 2", snap.Totals.Queue)
	}

	// Unified recent history: all four kinds, newest-first (4 entries seeded:
	// one per kind — timestamps may be equal, so assert membership + order by
	// kind set rather than exact ordering).
	if len(snap.RecentHistory) != 4 {
		t.Fatalf("recent_history = %d entries, want 4: %+v", len(snap.RecentHistory), snap.RecentHistory)
	}
	histKinds := map[dashboard.Kind]bool{}
	for _, h := range snap.RecentHistory {
		histKinds[h.Kind] = true
		if h.Name == "" {
			t.Errorf("history for kind %s has empty parent name", h.Kind)
		}
	}
	for _, k := range []dashboard.Kind{dashboard.KindMovie, dashboard.KindTV, dashboard.KindMusic, dashboard.KindBook} {
		if !histKinds[k] {
			t.Errorf("recent_history missing kind %s", k)
		}
	}

	// Unified recent queue: 4 jobs (movies 2, tv 1, music 1 — book has none),
	// newest-first.
	if len(snap.RecentQueue) != 4 {
		t.Fatalf("recent_queue = %d entries, want 4: %+v", len(snap.RecentQueue), snap.RecentQueue)
	}
	qKinds := map[dashboard.Kind]bool{}
	for _, q := range snap.RecentQueue {
		qKinds[q.Kind] = true
	}
	for _, k := range []dashboard.Kind{dashboard.KindMovie, dashboard.KindTV, dashboard.KindMusic} {
		if !qKinds[k] {
			t.Errorf("recent_queue missing kind %s", k)
		}
	}

	if snap.GeneratedAt.IsZero() {
		t.Error("generated_at is zero")
	}
}

// TestDashboardRepo_LimitBounded is the #40 "bounded recent feed" contract:
// a non-positive limit falls back to the default, and a small limit is honored
// (the feed is a tail, never the full table).
func TestDashboardRepo_LimitBounded(t *testing.T) {
	db := newTestPostgres(t)
	ctx := context.Background()

	st := NewWithDB(db)
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := NewDashboardRepo(db)

	// Seed 5 movies each with 2 queue jobs = 10 queue rows.
	if _, err := db.ExecContext(ctx, `INSERT INTO movies (title, year)
		VALUES ('M1', 2001), ('M2', 2002), ('M3', 2003), ('M4', 2004), ('M5', 2005)`); err != nil {
		t.Fatalf("seed movies: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO queue (movie_id, release_title, state)
		SELECT id, 'rel-' || id || '-a', 'complete' FROM movies
		UNION ALL
		SELECT id, 'rel-' || id || '-b', 'complete' FROM movies`); err != nil {
		t.Fatalf("seed queue: %v", err)
	}

	snap, err := repo.Snapshot(ctx, 3)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if len(snap.RecentQueue) != 3 {
		t.Errorf("limit 3: got %d queue entries, want 3", len(snap.RecentQueue))
	}

	// Non-positive limit uses the implementation default, not the full table.
	snap2, err := repo.Snapshot(ctx, 0)
	if err != nil {
		t.Fatalf("snapshot (default limit): %v", err)
	}
	if len(snap2.RecentQueue) > 10 {
		t.Errorf("default limit: got %d queue entries, want <= 10", len(snap2.RecentQueue))
	}
}

// TestDashboardRepo_EmptyLibrary is the #40 empty-state contract: a fresh
// database yields zeroed libraries and empty (nil-safe) feeds, not an error.
func TestDashboardRepo_EmptyLibrary(t *testing.T) {
	db := newTestPostgres(t)
	ctx := context.Background()

	st := NewWithDB(db)
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := NewDashboardRepo(db)

	snap, err := repo.Snapshot(ctx, 10)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if len(snap.Libraries) != 4 {
		t.Fatalf("expected 4 (zero) kinds, got %d", len(snap.Libraries))
	}
	for _, l := range snap.Libraries {
		if l.Count != 0 || l.Wants != 0 || l.Queue != 0 {
			t.Errorf("kind %s: expected zero counts, got %+v", l.Kind, l)
		}
	}
	if snap.Totals.Libraries != 0 || snap.Totals.Wants != 0 || snap.Totals.Queue != 0 {
		t.Errorf("totals not zero on empty db: %+v", snap.Totals)
	}
	if len(snap.RecentHistory) != 0 || len(snap.RecentQueue) != 0 {
		t.Errorf("expected empty feeds on empty db: hist=%d queue=%d",
			len(snap.RecentHistory), len(snap.RecentQueue))
	}
}
