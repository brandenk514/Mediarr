package postgres

import (
	"context"
	"testing"

	"github.com/brandenk514/mediarr/internal/queue"
)

// TestQueueRepo_List is the #43 dedicated-queue-view contract against a real
// Postgres container: seed every kind with parents + queue jobs and assert the
// unified, unbounded list is joined to each parent's human name and is not
// truncated (unlike the dashboard's recent tail).
func TestQueueRepo_List(t *testing.T) {
	db := newTestPostgres(t)
	ctx := context.Background()

	st := NewWithDB(db)
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := NewQueueRepo(db)

	apply := func(s string) {
		if _, err := db.ExecContext(ctx, s); err != nil {
			t.Fatalf("seed %q: %v", s, err)
		}
	}

	// Movies: 2 jobs.
	apply(`INSERT INTO movies (title, year) VALUES ('Inception', 2010), ('Interstellar', 2014)`)
	apply(`INSERT INTO queue (movie_id, release_title, indexer, state, progress) VALUES
		(1, 'Inception.2010.1080p.WEB.x264', 'nyaa', 'downloading', 42),
		(2, 'Interstellar.2014.2160p.UHD.HDR', 'rarbg', 'queued', 0)`)
	// TV: 1 job.
	apply(`INSERT INTO tv_series (title, year) VALUES ('The Wire', 2002)`)
	apply(`INSERT INTO tv_queue (series_id, season, episode, release_title, indexer, state, progress) VALUES
		(1, 1, 0, 'The.Wire.S01.COMPLETE.1080p', 'fake', 'complete', 100)`)
	// Music: 1 job.
	apply(`INSERT INTO music_artists (name) VALUES ('Nirvana')`)
	apply(`INSERT INTO music_albums (artist_id, name, year) VALUES (1, 'Nevermind', 1991)`)
	apply(`INSERT INTO music_queue (artist_id, album_id, release_title, indexer, state, progress,
        album_title, quality, formats, track_title) VALUES
	(1, 1, 'Nirvana (1991) - Nevermind [FLAC 1411kbps]', 'fake', 'failed', 0,
	 'Nevermind', 'FLAC', 'FLAC', '')`)
	// Books: 1 job.
	apply(`INSERT INTO book_authors (name) VALUES ('Ursula K. Le Guin')`)
	apply(`INSERT INTO book_titles (author_id, name) VALUES (1, 'The Dispossessed')`)
	apply(`INSERT INTO book_queue (author_id, title_id, release_title, indexer, state, progress) VALUES
		(1, 1, 'The.Dispossessed.epub', 'zlib', 'queued', 0)`)

	items, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	// 2 movies + 1 tv + 1 music + 1 book = 5 (unbounded, no tail truncation).
	if len(items) != 5 {
		t.Fatalf("len(items) = %d, want 5: %+v", len(items), items)
	}

	byParent := map[string]queue.Item{}
	for _, it := range items {
		byParent[it.ParentName] = it
	}
	if it, ok := byParent["Inception"]; !ok || it.Kind != queue.KindMovie || it.State != "downloading" || it.Progress != 42 {
		t.Errorf("Inception row wrong: %+v", it)
	}
	if it, ok := byParent["The Wire"]; !ok || it.Kind != queue.KindTV || it.ParentID != 1 {
		t.Errorf("The Wire row wrong: %+v", it)
	}
	if it, ok := byParent["Nirvana"]; !ok || it.Kind != queue.KindMusic {
		t.Errorf("Nirvana row wrong: %+v", it)
	} else {
		// L4 Activity: the music row carries its display columns (Lidarr
		// queue row: Album Title / Quality / Formats / Track Title).
		if it.AlbumTitle != "Nevermind" || it.Quality != "FLAC" || it.Formats != "FLAC" || it.TrackTitle != "" {
			t.Errorf("Nirvana music display columns wrong: %+v", it)
		}
	}
	if it, ok := byParent["Ursula K. Le Guin"]; !ok || it.Kind != queue.KindBook {
		t.Errorf("Le Guin row wrong: %+v", it)
	} else if it.AlbumTitle != "" || it.Quality != "" || it.Formats != "" || it.TrackTitle != "" {
		t.Errorf("book row must keep empty music columns: %+v", it)
	}
}

// TestQueueRepo_History is the #43 per-parent drill-down contract: history is
// scoped to one parent, newest-first, capped by limit, and an empty parent
// yields a non-nil empty slice (not an error).
func TestQueueRepo_History(t *testing.T) {
	db := newTestPostgres(t)
	ctx := context.Background()

	st := NewWithDB(db)
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := NewQueueRepo(db)

	apply := func(s string) {
		if _, err := db.ExecContext(ctx, s); err != nil {
			t.Fatalf("seed %q: %v", s, err)
		}
	}
	apply(`INSERT INTO movies (title, year) VALUES ('Inception', 2010), ('Interstellar', 2014)`)
	// Distinct at values: the default now() would give every row the same
	// second, making ORDER BY at DESC tie and non-deterministic.
	apply(`INSERT INTO history (movie_id, event, detail, at) VALUES
		(1, 'added_to_queue', 'from nyaa', now() - interval '3 hours'),
		(1, 'downloading', '42%', now() - interval '2 hours'),
		(1, 'imported', 'to /media/movies/Inception', now() - interval '1 hour'),
		(2, 'added_to_queue', 'from rarbg', now() - interval '30 minutes')`)

	// limit 2 -> only the newest two for movie 1.
	ev, err := repo.History(ctx, queue.KindMovie, 1, 2)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(ev) != 2 {
		t.Fatalf("limit 2: got %d events, want 2: %+v", len(ev), ev)
	}
	// Newest first.
	if ev[0].Event != "imported" || ev[1].Event != "downloading" {
		t.Errorf("order = %v, %v; want imported then downloading", ev[0].Event, ev[1].Event)
	}

	// A different parent is isolated.
	ev2, err := repo.History(ctx, queue.KindMovie, 2, 0)
	if err != nil {
		t.Fatalf("history(2): %v", err)
	}
	if len(ev2) != 1 || ev2[0].Event != "added_to_queue" {
		t.Errorf("movie 2 history = %+v, want 1 added_to_queue", ev2)
	}

	// A parent with no history (and an unknown kind) is empty, not an error.
	empty, err := repo.History(ctx, queue.KindMusic, 1, 0)
	if err != nil {
		t.Fatalf("history empty: %v", err)
	}
	if len(empty) != 0 || empty == nil {
		t.Errorf("empty history = %+v, want [] non-nil", empty)
	}
}

// TestQueueRepo_Empty is the #43 empty-state contract: a fresh database yields
// an empty (non-nil) queue list, so the UI renders an empty state, not an error.
func TestQueueRepo_Empty(t *testing.T) {
	db := newTestPostgres(t)
	ctx := context.Background()

	st := NewWithDB(db)
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := NewQueueRepo(db)

	items, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 0 || items == nil {
		t.Errorf("empty list = %+v, want [] non-nil", items)
	}
}
