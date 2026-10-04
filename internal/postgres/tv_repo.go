package postgres

import (
	"context"
	"database/sql"
	"fmt"

	tv "github.com/brandenk514/mediarr/internal/domains/tv"
)

// TVRepo is the Postgres-backed implementation of tv.Repo.
type TVRepo struct {
	db *sql.DB
}

// NewTVRepo builds a TV repository over the given pool.
func NewTVRepo(db *sql.DB) *TVRepo { return &TVRepo{db: db} }

var _ tv.Repo = (*TVRepo)(nil)

// --- Series ---

// CreateSeries inserts a series and returns its id.
func (r *TVRepo) CreateSeries(ctx context.Context, s tv.Series) (int64, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO tv_series (tvdb_id, tmdb_id, title, year, quality_profile, monitored)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id`,
		s.TVDBID, s.TMDBID, s.Title, s.Year, s.QualityProfile, s.Monitored,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("tv: create series: %w", err)
	}
	return id, nil
}

// GetSeries loads a series by id.
func (r *TVRepo) GetSeries(ctx context.Context, id int64) (*tv.Series, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, tvdb_id, tmdb_id, title, year, quality_profile, monitored, added_at
		FROM tv_series WHERE id = $1`, id)
	s := &tv.Series{}
	err := row.Scan(&s.ID, &s.TVDBID, &s.TMDBID, &s.Title, &s.Year,
		&s.QualityProfile, &s.Monitored, &s.AddedAt)
	if err == sql.ErrNoRows {
		return nil, tv.ErrSeriesNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("tv: get series: %w", err)
	}
	return s, nil
}

// ListSeries returns all series, oldest first.
func (r *TVRepo) ListSeries(ctx context.Context) ([]tv.Series, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tvdb_id, tmdb_id, title, year, quality_profile, monitored, added_at
		FROM tv_series ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("tv: list series: %w", err)
	}
	defer rows.Close()
	var out []tv.Series
	for rows.Next() {
		var s tv.Series
		if err := rows.Scan(&s.ID, &s.TVDBID, &s.TMDBID, &s.Title, &s.Year,
			&s.QualityProfile, &s.Monitored, &s.AddedAt); err != nil {
			return nil, fmt.Errorf("tv: scan series: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SetSeriesMonitored toggles a series' monitored flag.
func (r *TVRepo) SetSeriesMonitored(ctx context.Context, id int64, monitored bool) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE tv_series SET monitored = $2 WHERE id = $1`, id, monitored)
	if err != nil {
		return fmt.Errorf("tv: set series monitored: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return tv.ErrSeriesNotFound
	}
	return nil
}

// --- Episodes ---

// CreateEpisode inserts an episode and returns its id.
func (r *TVRepo) CreateEpisode(ctx context.Context, e tv.Episode) (int64, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO tv_episodes (series_id, season, episode, title, monitored)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id`,
		e.SeriesID, e.Season, e.Episode, e.Title, e.Monitored,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("tv: create episode: %w", err)
	}
	return id, nil
}

// GetEpisode loads a single episode by (series, season, episode).
func (r *TVRepo) GetEpisode(ctx context.Context, seriesID int64, season, episode int) (*tv.Episode, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, series_id, season, episode, title, monitored, added_at
		FROM tv_episodes WHERE series_id = $1 AND season = $2 AND episode = $3`,
		seriesID, season, episode)
	e := &tv.Episode{}
	err := row.Scan(&e.ID, &e.SeriesID, &e.Season, &e.Episode, &e.Title,
		&e.Monitored, &e.AddedAt)
	if err == sql.ErrNoRows {
		return nil, tv.ErrEpisodeNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("tv: get episode: %w", err)
	}
	return e, nil
}

// ListEpisodes returns all episodes for a series, ordered by season then episode.
func (r *TVRepo) ListEpisodes(ctx context.Context, seriesID int64) ([]tv.Episode, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, series_id, season, episode, title, monitored, added_at
		FROM tv_episodes WHERE series_id = $1
		ORDER BY season, episode`, seriesID)
	if err != nil {
		return nil, fmt.Errorf("tv: list episodes: %w", err)
	}
	defer rows.Close()
	var out []tv.Episode
	for rows.Next() {
		var e tv.Episode
		if err := rows.Scan(&e.ID, &e.SeriesID, &e.Season, &e.Episode, &e.Title,
			&e.Monitored, &e.AddedAt); err != nil {
			return nil, fmt.Errorf("tv: scan episode: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// SetEpisodeMonitored toggles a single episode's monitored flag.
func (r *TVRepo) SetEpisodeMonitored(ctx context.Context, seriesID int64, season, episode int, monitored bool) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE tv_episodes SET monitored = $4
		WHERE series_id = $1 AND season = $2 AND episode = $3`,
		seriesID, season, episode, monitored)
	if err != nil {
		return fmt.Errorf("tv: set episode monitored: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return tv.ErrEpisodeNotFound
	}
	return nil
}

// --- Wanted (episode granularity) ---

// EnsureWanted inserts a pending wanted row for the episode if one does not
// already exist. Idempotent: re-adding a monitored episode does not create a
// second row.
func (r *TVRepo) EnsureWanted(ctx context.Context, seriesID int64, season, episode int) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO tv_wanted (series_id, season, episode, status)
		VALUES ($1, $2, $3, 'pending')
		ON CONFLICT (series_id, season, episode) DO NOTHING`, seriesID, season, episode)
	if err != nil {
		return fmt.Errorf("tv: ensure wanted: %w", err)
	}
	return nil
}

// GetWanted loads a single episode's wanted row.
func (r *TVRepo) GetWanted(ctx context.Context, seriesID int64, season, episode int) (*tv.Wanted, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT series_id, season, episode, status, release_title, created_at, satisfied_at
		FROM tv_wanted WHERE series_id = $1 AND season = $2 AND episode = $3`,
		seriesID, season, episode)
	w := &tv.Wanted{}
	err := row.Scan(&w.SeriesID, &w.Season, &w.Episode, &w.Status,
		&w.ReleaseTitle, &w.CreatedAt, &w.SatisfiedAt)
	if err == sql.ErrNoRows {
		return nil, sql.ErrNoRows
	}
	if err != nil {
		return nil, fmt.Errorf("tv: get wanted: %w", err)
	}
	return w, nil
}

// ListWanted returns all wanted rows for a series, ordered by season then episode.
func (r *TVRepo) ListWanted(ctx context.Context, seriesID int64) ([]tv.Wanted, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT series_id, season, episode, status, release_title, created_at, satisfied_at
		FROM tv_wanted WHERE series_id = $1
		ORDER BY season, episode`, seriesID)
	if err != nil {
		return nil, fmt.Errorf("tv: list wanted: %w", err)
	}
	defer rows.Close()
	var out []tv.Wanted
	for rows.Next() {
		var w tv.Wanted
		if err := rows.Scan(&w.SeriesID, &w.Season, &w.Episode, &w.Status,
			&w.ReleaseTitle, &w.CreatedAt, &w.SatisfiedAt); err != nil {
			return nil, fmt.Errorf("tv: scan wanted: %w", err)
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// MarkWantedSatisfied flips an episode's wanted row to satisfied and records
// the release that satisfied it.
func (r *TVRepo) MarkWantedSatisfied(ctx context.Context, seriesID int64, season, episode int, releaseTitle string) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE tv_wanted
		SET status = 'satisfied', release_title = $4, satisfied_at = now()
		WHERE series_id = $1 AND season = $2 AND episode = $3`,
		seriesID, season, episode, releaseTitle)
	if err != nil {
		return fmt.Errorf("tv: satisfy wanted: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("tv: no wanted row for series %d S%02dE%02d", seriesID, season, episode)
	}
	return nil
}

// --- Queue ---

// CreateQueue inserts a queue entry and returns its id.
func (r *TVRepo) CreateQueue(ctx context.Context, e tv.QueueEntry) (int64, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO tv_queue (series_id, season, episode, release_title, indexer, download_client, state, progress)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id`,
		e.SeriesID, e.Season, e.Episode, e.ReleaseTitle, e.Indexer,
		e.DownloadClient, e.State, e.Progress,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("tv: create queue: %w", err)
	}
	return id, nil
}

// UpdateQueue updates a queue entry's state/progress.
func (r *TVRepo) UpdateQueue(ctx context.Context, e tv.QueueEntry) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE tv_queue
		SET state = $2, progress = $3, updated_at = now()
		WHERE id = $1`, e.ID, e.State, e.Progress)
	if err != nil {
		return fmt.Errorf("tv: update queue: %w", err)
	}
	return nil
}

// GetQueue loads a queue entry by id.
func (r *TVRepo) GetQueue(ctx context.Context, id int64) (*tv.QueueEntry, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, series_id, season, episode, release_title, indexer, download_client,
		       state, progress, created_at, updated_at
		FROM tv_queue WHERE id = $1`, id)
	e := &tv.QueueEntry{}
	err := row.Scan(&e.ID, &e.SeriesID, &e.Season, &e.Episode, &e.ReleaseTitle,
		&e.Indexer, &e.DownloadClient, &e.State, &e.Progress, &e.CreatedAt, &e.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, sql.ErrNoRows
	}
	if err != nil {
		return nil, fmt.Errorf("tv: get queue: %w", err)
	}
	return e, nil
}

// ListQueue returns all queue entries for a series, ordered by id.
func (r *TVRepo) ListQueue(ctx context.Context, seriesID int64) ([]tv.QueueEntry, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, series_id, season, episode, release_title, indexer, download_client,
		       state, progress, created_at, updated_at
		FROM tv_queue WHERE series_id = $1 ORDER BY id`, seriesID)
	if err != nil {
		return nil, fmt.Errorf("tv: list queue: %w", err)
	}
	defer rows.Close()
	var out []tv.QueueEntry
	for rows.Next() {
		var e tv.QueueEntry
		if err := rows.Scan(&e.ID, &e.SeriesID, &e.Season, &e.Episode, &e.ReleaseTitle,
			&e.Indexer, &e.DownloadClient, &e.State, &e.Progress, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, fmt.Errorf("tv: scan queue: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// --- History ---

// AddHistory appends a history event for a series.
func (r *TVRepo) AddHistory(ctx context.Context, e tv.HistoryEntry) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO tv_history (series_id, event, detail)
		VALUES ($1, $2, $3)`, e.SeriesID, e.Event, e.Detail)
	if err != nil {
		return fmt.Errorf("tv: add history: %w", err)
	}
	return nil
}

// ListHistory returns the most recent history events for a series (newest first).
func (r *TVRepo) ListHistory(ctx context.Context, seriesID int64, limit int) ([]tv.HistoryEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, series_id, event, detail, at
		FROM tv_history WHERE series_id = $1
		ORDER BY at DESC, id DESC LIMIT $2`, seriesID, limit)
	if err != nil {
		return nil, fmt.Errorf("tv: list history: %w", err)
	}
	defer rows.Close()
	var out []tv.HistoryEntry
	for rows.Next() {
		var e tv.HistoryEntry
		if err := rows.Scan(&e.ID, &e.SeriesID, &e.Event, &e.Detail, &e.At); err != nil {
			return nil, fmt.Errorf("tv: scan history: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
