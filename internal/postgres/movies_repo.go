package postgres

import (
	"context"
	"database/sql"
	"fmt"

	movies "github.com/brandenk514/mediarr/internal/domains/movies"
)

// MovieRepo is the Postgres-backed implementation of movies.Repo.
type MovieRepo struct {
	db *sql.DB
}

// NewMovieRepo builds a movies repository over the given pool.
func NewMovieRepo(db *sql.DB) *MovieRepo { return &MovieRepo{db: db} }

var _ movies.Repo = (*MovieRepo)(nil)

// CreateMovie inserts a movie and returns its id.
func (r *MovieRepo) CreateMovie(ctx context.Context, m movies.Movie) (int64, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO movies (title, year, quality_profile, monitored)
		VALUES ($1, $2, $3, $4)
		RETURNING id`,
		m.Title, m.Year, m.QualityProfile, m.Monitored,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("movies: create: %w", err)
	}
	return id, nil
}

// GetMovie loads a movie by id.
func (r *MovieRepo) GetMovie(ctx context.Context, id int64) (*movies.Movie, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, title, year, quality_profile, monitored, added_at
		FROM movies WHERE id = $1`, id)
	m := &movies.Movie{}
	err := row.Scan(&m.ID, &m.Title, &m.Year, &m.QualityProfile, &m.Monitored, &m.AddedAt)
	if err == sql.ErrNoRows {
		return nil, movies.ErrMovieNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("movies: get: %w", err)
	}
	return m, nil
}

// ListMovies returns all movies, oldest first.
func (r *MovieRepo) ListMovies(ctx context.Context) ([]movies.Movie, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, title, year, quality_profile, monitored, added_at
		FROM movies ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("movies: list: %w", err)
	}
	defer rows.Close()
	var out []movies.Movie
	for rows.Next() {
		var m movies.Movie
		if err := rows.Scan(&m.ID, &m.Title, &m.Year, &m.QualityProfile, &m.Monitored, &m.AddedAt); err != nil {
			return nil, fmt.Errorf("movies: scan: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// EnsureWanted inserts a pending wanted row if one does not already exist.
// Idempotent: re-adding a monitored movie does not create a second row.
func (r *MovieRepo) EnsureWanted(ctx context.Context, movieID int64) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO wanted (movie_id, status)
		VALUES ($1, 'pending')
		ON CONFLICT (movie_id) DO NOTHING`, movieID)
	if err != nil {
		return fmt.Errorf("movies: ensure wanted: %w", err)
	}
	return nil
}

// GetWanted loads a movie's wanted row.
func (r *MovieRepo) GetWanted(ctx context.Context, movieID int64) (*movies.Wanted, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT movie_id, status, release_title, created_at, satisfied_at
		FROM wanted WHERE movie_id = $1`, movieID)
	w := &movies.Wanted{}
	err := row.Scan(&w.MovieID, &w.Status, &w.ReleaseTitle, &w.CreatedAt, &w.SatisfiedAt)
	if err == sql.ErrNoRows {
		return nil, sql.ErrNoRows
	}
	if err != nil {
		return nil, fmt.Errorf("movies: get wanted: %w", err)
	}
	return w, nil
}

// ListWanted returns all wanted rows ordered by id.
func (r *MovieRepo) ListWanted(ctx context.Context) ([]movies.Wanted, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT movie_id, status, release_title, created_at, satisfied_at
		FROM wanted ORDER BY movie_id`)
	if err != nil {
		return nil, fmt.Errorf("movies: list wanted: %w", err)
	}
	defer rows.Close()
	var out []movies.Wanted
	for rows.Next() {
		var w movies.Wanted
		if err := rows.Scan(&w.MovieID, &w.Status, &w.ReleaseTitle, &w.CreatedAt, &w.SatisfiedAt); err != nil {
			return nil, fmt.Errorf("movies: scan wanted: %w", err)
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// MarkWantedSatisfied flips a wanted row to satisfied and records the release.
func (r *MovieRepo) MarkWantedSatisfied(ctx context.Context, movieID int64, releaseTitle string) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE wanted
		SET status = 'satisfied', release_title = $2, satisfied_at = now()
		WHERE movie_id = $1`, movieID, releaseTitle)
	if err != nil {
		return fmt.Errorf("movies: satisfy wanted: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("movies: no wanted row for movie %d", movieID)
	}
	return nil
}

// CreateQueue inserts a queue entry and returns its id.
func (r *MovieRepo) CreateQueue(ctx context.Context, e movies.QueueEntry) (int64, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO queue (movie_id, release_title, indexer, download_client, state, progress)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id`,
		e.MovieID, e.ReleaseTitle, e.Indexer, e.DownloadClient, e.State, e.Progress,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("movies: create queue: %w", err)
	}
	return id, nil
}

// UpdateQueue updates a queue entry's state/progress.
func (r *MovieRepo) UpdateQueue(ctx context.Context, e movies.QueueEntry) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE queue
		SET state = $2, progress = $3, updated_at = now()
		WHERE id = $1`, e.ID, e.State, e.Progress)
	if err != nil {
		return fmt.Errorf("movies: update queue: %w", err)
	}
	return nil
}

// GetQueue loads a queue entry by id.
func (r *MovieRepo) GetQueue(ctx context.Context, id int64) (*movies.QueueEntry, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, movie_id, release_title, indexer, download_client, state, progress, created_at, updated_at
		FROM queue WHERE id = $1`, id)
	e := &movies.QueueEntry{}
	err := row.Scan(&e.ID, &e.MovieID, &e.ReleaseTitle, &e.Indexer, &e.DownloadClient,
		&e.State, &e.Progress, &e.CreatedAt, &e.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, sql.ErrNoRows
	}
	if err != nil {
		return nil, fmt.Errorf("movies: get queue: %w", err)
	}
	return e, nil
}

// ListQueue returns all queue entries ordered by id.
func (r *MovieRepo) ListQueue(ctx context.Context) ([]movies.QueueEntry, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, movie_id, release_title, indexer, download_client, state, progress, created_at, updated_at
		FROM queue ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("movies: list queue: %w", err)
	}
	defer rows.Close()
	var out []movies.QueueEntry
	for rows.Next() {
		var e movies.QueueEntry
		if err := rows.Scan(&e.ID, &e.MovieID, &e.ReleaseTitle, &e.Indexer, &e.DownloadClient,
			&e.State, &e.Progress, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, fmt.Errorf("movies: scan queue: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// AddHistory appends a history event.
func (r *MovieRepo) AddHistory(ctx context.Context, e movies.HistoryEntry) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO history (movie_id, event, detail)
		VALUES ($1, $2, $3)`, e.MovieID, e.Event, e.Detail)
	if err != nil {
		return fmt.Errorf("movies: add history: %w", err)
	}
	return nil
}

// ListHistory returns the most recent history events (newest first).
func (r *MovieRepo) ListHistory(ctx context.Context, limit int) ([]movies.HistoryEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, movie_id, event, detail, at
		FROM history ORDER BY at DESC, id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("movies: list history: %w", err)
	}
	defer rows.Close()
	var out []movies.HistoryEntry
	for rows.Next() {
		var e movies.HistoryEntry
		if err := rows.Scan(&e.ID, &e.MovieID, &e.Event, &e.Detail, &e.At); err != nil {
			return nil, fmt.Errorf("movies: scan history: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
