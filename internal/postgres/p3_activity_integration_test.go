package postgres

import (
	"context"
	"testing"
	"time"

	blkdom "github.com/brandenk514/mediarr/internal/domains/blocklist"
	hfdom "github.com/brandenk514/mediarr/internal/domains/historyfeed"
	wd "github.com/brandenk514/mediarr/internal/domains/wanted"
)

// TestP3_HistoryFeed_List is the P3 global Activity > History contract
// against a real Postgres container: events are seeded into all four
// per-kind history tables and the feed must unify them (with each media
// item's name joined in), honoring since / event / limit and ordering
// newest first.
func TestP3_HistoryFeed_List(t *testing.T) {
	db := newTestPostgres(t)
	ctx := context.Background()
	st := NewWithDB(db)
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := NewHistoryRepo(db)

	apply := func(s string) {
		if _, err := db.ExecContext(ctx, s); err != nil {
			t.Fatalf("seed %q: %v", s, err)
		}
	}
	apply(`INSERT INTO movies (title, year) VALUES ('Inception', 2010)`)
	apply(`INSERT INTO tv_series (title, year) VALUES ('The Wire', 2002)`)
	apply(`INSERT INTO music_artists (name) VALUES ('Nirvana')`)
	apply(`INSERT INTO book_authors (name) VALUES ('Ursula K. Le Guin')`)
	apply(`INSERT INTO history (movie_id, event, detail, at) VALUES
		(1, 'imported', 'Inception.2010.1080p.mkv', '2026-10-11T12:00:00Z')`)
	apply(`INSERT INTO tv_history (series_id, event, detail, at) VALUES
		(1, 'search-requested', 'S01E01', '2026-10-11T11:00:00Z'),
		(1, 'added', '', '2026-10-10T11:00:00Z')`)
	apply(`INSERT INTO music_history (artist_id, event, detail, at) VALUES
		(1, 'downloaded', 'Nevermind [FLAC]', '2026-10-09T11:00:00Z')`)
	apply(`INSERT INTO book_history (author_id, event, detail, at) VALUES
		(1, 'imported', 'The.Dispossessed.epub', '2026-10-08T11:00:00Z')`)

	// All four kinds, newest first.
	items, err := repo.List(ctx, hfdom.Filter{Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 5 {
		t.Fatalf("len = %d, want 5: %+v", len(items), items)
	}
	if items[0].Kind != "movie" || items[0].Name != "Inception" {
		t.Fatalf("newest = %+v, want movie/Inception", items[0])
	}
	// Names resolved per kind.
	byKind := map[string]string{}
	for _, it := range items {
		byKind[it.Kind] = it.Name
	}
	if byKind["tv"] != "The Wire" || byKind["music"] != "Nirvana" || byKind["book"] != "Ursula K. Le Guin" {
		t.Fatalf("names = %+v", byKind)
	}

	// since is an inclusive lower bound.
	since := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	items, _ = repo.List(ctx, hfdom.Filter{Since: &since, Limit: 10})
	if len(items) != 2 {
		t.Fatalf("since: len = %d, want 2", len(items))
	}

	// event is case-insensitive.
	items, err = repo.List(ctx, hfdom.Filter{Event: "IMPORTED", Limit: 10})
	if err != nil {
		t.Fatalf("event filter err: %v", err)
	}
	if len(items) != 2 { // movie imported + book imported
		t.Fatalf("event: len = %d, want 2: %+v", len(items), items)
	}

	// limit caps the result.
	items, err = repo.List(ctx, hfdom.Filter{Limit: 2})
	if err != nil {
		t.Fatalf("limit err: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("limit: len = %d, want 2", len(items))
	}

	// No match -> non-nil empty.
	items, _ = repo.List(ctx, hfdom.Filter{Event: "nope"})
	if items == nil || len(items) != 0 {
		t.Fatalf("no match: got %v, want non-nil empty", items)
	}
}

// TestP3_Blocklist is the P3 Activity > Blocklist CRUD contract: an entry
// stores the release + indexer + reason + media, the parent name is joined
// in at read time, filters narrow the list, and unblock removes it.
func TestP3_Blocklist(t *testing.T) {
	db := newTestPostgres(t)
	ctx := context.Background()
	st := NewWithDB(db)
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := NewBlocklistRepo(db)

	apply := func(s string) {
		if _, err := db.ExecContext(ctx, s); err != nil {
			t.Fatalf("seed %q: %v", s, err)
		}
	}
	apply(`INSERT INTO tv_series (title, year) VALUES ('Severance', 2022)`)
	apply(`INSERT INTO movies (title, year) VALUES ('Dune', 2021)`)

	id, err := repo.Add(ctx, blkdom.Entry{
		Kind: blkdom.KindTV, ParentID: 1, ReleaseTitle: "Severance.S01E01.720p", Indexer: "nyaa", Reason: "low quality",
	})
	if err != nil {
		t.Fatalf("add tv: %v", err)
	}
	id2, err := repo.Add(ctx, blkdom.Entry{
		Kind: blkdom.KindMovie, ParentID: 1, ReleaseTitle: "Dune.2160p.WEB", Indexer: "torrentle",
	})
	if err != nil {
		t.Fatalf("add movie: %v", err)
	}

	items, err := repo.List(ctx, blkdom.Filter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len = %d, want 2: %+v", len(items), items)
	}
	// Parent names resolved per kind.
	var tvName, movieName string
	for _, it := range items {
		switch it.Kind {
		case blkdom.KindTV:
			tvName = it.ParentName
		case blkdom.KindMovie:
			movieName = it.ParentName
		}
	}
	if tvName != "Severance" || movieName != "Dune" {
		t.Fatalf("names = %q/%q, want Severance/Dune", tvName, movieName)
	}

	// Kind filter.
	items, _ = repo.List(ctx, blkdom.Filter{Kind: blkdom.KindMovie})
	if len(items) != 1 || items[0].ID != id2 {
		t.Fatalf("kind filter: %+v", items)
	}
	// Parent filter.
	items, _ = repo.List(ctx, blkdom.Filter{Kind: blkdom.KindTV, ParentID: 1})
	if len(items) != 1 || items[0].ReleaseTitle != "Severance.S01E01.720p" {
		t.Fatalf("parent filter: %+v", items)
	}

	// Unblock removes; a repeat reports false.
	ok, err := repo.Unblock(ctx, id)
	if err != nil || !ok {
		t.Fatalf("unblock: ok=%v err=%v", ok, err)
	}
	ok, err = repo.Unblock(ctx, id)
	if err != nil || ok {
		t.Fatalf("unblock again: ok=%v err=%v (want false, nil)", ok, err)
	}
	items, _ = repo.List(ctx, blkdom.Filter{})
	if len(items) != 1 {
		t.Fatalf("after unblock: len = %d, want 1", len(items))
	}
}

// TestP3_WantedRepo is the P3 global Wanted data-layer contract: EpisodeRows
// joins each episode to its series title + file state, SeriesProfiles maps
// series to their profile, and the write paths (pending wanted, unmonitor,
// satisfy, history) round-trip.
func TestP3_WantedRepo(t *testing.T) {
	db := newTestPostgres(t)
	ctx := context.Background()
	st := NewWithDB(db)
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := NewWantedRepo(db)

	apply := func(s string) {
		if _, err := db.ExecContext(ctx, s); err != nil {
			t.Fatalf("seed %q: %v", s, err)
		}
	}
	apply(`INSERT INTO tv_series (title, year, quality_profile) VALUES
		('Alpha', 2020, 'HD-1080p'), ('Beta', 2021, 'HD-720p')`)
	apply(`INSERT INTO tv_episodes (series_id, season, episode, title, monitored) VALUES
		(1, 1, 1, 'Pilot', true),
		(1, 1, 2, 'Two', true),
		(2, 1, 1, 'First', false)`)
	// A satisfied wanted row on 1x1 (gives it a file + quality).
	apply(`INSERT INTO tv_wanted (series_id, season, episode, status, release_title, quality, satisfied_at) VALUES
		(1, 1, 1, 'satisfied', 'Alpha.S01E01.720p', '720p', '2026-10-11T10:00:00Z')`)

	// EpisodeRows: 3 episodes, each joined to its series + file state.
	rows, err := repo.EpisodeRows(ctx)
	if err != nil {
		t.Fatalf("episode rows: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("len = %d, want 3: %+v", len(rows), rows)
	}
	bySxE := map[[3]int64]wd.EpisodeRow{}
	for _, r := range rows {
		bySxE[[3]int64{r.SeriesID, int64(r.Season), int64(r.Episode)}] = r
	}
	r11 := bySxE[[3]int64{1, 1, 1}]
	if r11.SeriesTitle != "Alpha" || !r11.HasFile || r11.FileQuality != "720p" {
		t.Fatalf("1x1 = %+v, want Alpha/file/720p", r11)
	}
	r12 := bySxE[[3]int64{1, 1, 2}]
	if r12.SeriesTitle != "Alpha" || r12.HasFile {
		t.Fatalf("1x2 = %+v, want Alpha/no file", r12)
	}
	// Beta 1x1 is unmonitored in the seed.
	betaRow := bySxE[[3]int64{2, 1, 1}]
	if betaRow.SeriesTitle != "Beta" || betaRow.Monitored {
		t.Fatalf("beta 1x1 = %+v, want unmonitored Beta", betaRow)
	}

	// SeriesProfiles maps each series to its profile name.
	profiles, err := repo.SeriesProfiles(ctx)
	if err != nil {
		t.Fatalf("profiles: %v", err)
	}
	if profiles[1] != "HD-1080p" || profiles[2] != "HD-720p" {
		t.Fatalf("profiles = %+v", profiles)
	}

	// EnsureWantedPending inserts a pending row for a new episode.
	if err := repo.EnsureWantedPending(ctx, 1, 1, 2); err != nil {
		t.Fatalf("ensure pending: %v", err)
	}
	var status string
	if err := db.QueryRowContext(ctx,
		`SELECT status FROM tv_wanted WHERE series_id=1 AND season=1 AND episode=2`).Scan(&status); err != nil {
		t.Fatalf("scan 1x2 status: %v", err)
	}
	if status != "pending" {
		t.Fatalf("1x2 status = %q, want pending", status)
	}
	// EnsureWantedPending resets a satisfied row back to pending.
	if err := repo.EnsureWantedPending(ctx, 1, 1, 1); err != nil {
		t.Fatalf("ensure pending 1x1: %v", err)
	}
	if err := db.QueryRowContext(ctx,
		`SELECT status FROM tv_wanted WHERE series_id=1 AND season=1 AND episode=1`).Scan(&status); err != nil {
		t.Fatalf("scan 1x1 status: %v", err)
	}
	if status != "pending" {
		t.Fatalf("1x1 status = %q, want pending (reset from satisfied)", status)
	}

	// SetEpisodeMonitoredOff flips a monitored episode off (true) and
	// reports false for an unknown episode.
	ok, err := repo.SetEpisodeMonitoredOff(ctx, 1, 1, 1)
	if err != nil || !ok {
		t.Fatalf("unmonitor 1x1: ok=%v err=%v", ok, err)
	}
	ok, err = repo.SetEpisodeMonitoredOff(ctx, 9, 1, 1)
	if err != nil || ok {
		t.Fatalf("unmonitor unknown: ok=%v err=%v (want false, nil)", ok, err)
	}

	// GetSeries / GetEpisode.
	s, err := repo.GetSeries(ctx, 1)
	if err != nil || s == nil || s.Title != "Alpha" || s.QualityProfile != "HD-1080p" {
		t.Fatalf("get series = %+v err=%v", s, err)
	}
	if s, _ = repo.GetSeries(ctx, 99); s != nil {
		t.Fatalf("get unknown series = %+v, want nil", s)
	}
	e, err := repo.GetEpisode(ctx, 1, 1, 2)
	if err != nil || e == nil {
		t.Fatalf("get episode = %+v err=%v", e, err)
	}
	if e, _ = repo.GetEpisode(ctx, 1, 9, 9); e != nil {
		t.Fatalf("get unknown episode = %+v, want nil", e)
	}

	// AddHistory appends a global TV event.
	if err := repo.AddHistory(ctx, "tv", 1, "search-requested", "S01E02"); err != nil {
		t.Fatalf("add history: %v", err)
	}
	var hcount int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM tv_history WHERE series_id=1 AND event='search-requested'`).Scan(&hcount); err != nil {
		t.Fatalf("scan history count: %v", err)
	}
	if hcount != 1 {
		t.Fatalf("history count = %d, want 1", hcount)
	}

	// SatisfyWanted upserts a satisfied row with quality.
	if err := repo.SatisfyWanted(ctx, 1, 1, 2, "Alpha.S01E02.1080p", "1080p"); err != nil {
		t.Fatalf("satisfy: %v", err)
	}
	var qs string
	if err := db.QueryRowContext(ctx,
		`SELECT quality FROM tv_wanted WHERE series_id=1 AND season=1 AND episode=2`).Scan(&qs); err != nil {
		t.Fatalf("scan quality: %v", err)
	}
	if qs != "1080p" {
		t.Fatalf("satisfied quality = %q, want 1080p", qs)
	}
}
