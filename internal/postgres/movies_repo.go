package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
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

// CreateMovie inserts a movie and returns its id. It is idempotent (M6 #42):
// a movie with the same title + year already in the library is returned
// unchanged, so the add flow re-selecting a provider result never creates a
// duplicate.
func (r *MovieRepo) CreateMovie(ctx context.Context, m movies.Movie) (int64, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, `
		SELECT id FROM movies WHERE title = $1 AND year = $2`, m.Title, m.Year).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return 0, fmt.Errorf("movies: create: %w", err)
	}
	err = r.db.QueryRowContext(ctx, `
		INSERT INTO movies (title, year, quality_profile, monitored,
		                   tags, root_folder, minimum_availability)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id`,
		m.Title, m.Year, m.QualityProfile, m.Monitored,
		nullSafeTags(m.Tags), m.RootFolder, m.MinimumAvailability,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("movies: create: %w", err)
	}
	// Radarr parity (R1): a freshly added movie gets one 'Theatrical' edition
	// with the movie's quality profile; its monitored state follows the R2
	// edition-aware monitor plan (default: monitored).
	seeded := true
	if m.SeedEditionMonitored != nil {
		seeded = *m.SeedEditionMonitored
	}
	if _, err := r.db.ExecContext(ctx, `
		INSERT INTO editions (movie_id, label, monitored, quality)
		VALUES ($1, 'Theatrical', $2, $3)
		ON CONFLICT (movie_id, label) DO NOTHING`, id, seeded, m.QualityProfile); err != nil {
		return 0, fmt.Errorf("movies: seed edition: %w", err)
	}
	return id, nil
}

// GetMovie loads a movie by id.
func (r *MovieRepo) GetMovie(ctx context.Context, id int64) (*movies.Movie, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, title, year, quality_profile, monitored, added_at,
		       tags, root_folder, minimum_availability,
		       collection_id, collection_name
		FROM movies WHERE id = $1`, id)
	m := &movies.Movie{}
	var collectionName sql.NullString
	err := row.Scan(&m.ID, &m.Title, &m.Year, &m.QualityProfile, &m.Monitored, &m.AddedAt,
		&m.Tags, &m.RootFolder, &m.MinimumAvailability,
		&m.CollectionID, &collectionName)
	if err == sql.ErrNoRows {
		return nil, movies.ErrMovieNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("movies: get: %w", err)
	}
	if collectionName.Valid && collectionName.String != "" {
		m.CollectionName = &collectionName.String
	}
	return m, nil
}

// ListMovies returns all movies, oldest first.
func (r *MovieRepo) ListMovies(ctx context.Context) ([]movies.Movie, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, title, year, quality_profile, monitored, added_at,
		       tags, root_folder, minimum_availability,
		       collection_id, collection_name
		FROM movies ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("movies: list: %w", err)
	}
	defer func() { _ = rows.Close() }() // rows consumed by the loop above
	var out []movies.Movie
	for rows.Next() {
		var m movies.Movie
		var collectionName sql.NullString
		if err := rows.Scan(&m.ID, &m.Title, &m.Year, &m.QualityProfile, &m.Monitored, &m.AddedAt,
			&m.Tags, &m.RootFolder, &m.MinimumAvailability,
			&m.CollectionID, &collectionName); err != nil {
			return nil, fmt.Errorf("movies: scan: %w", err)
		}
		if collectionName.Valid && collectionName.String != "" {
			m.CollectionName = &collectionName.String
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SetMovieMonitored toggles a movie's monitored flag.
func (r *MovieRepo) SetMovieMonitored(ctx context.Context, id int64, monitored bool) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE movies SET monitored = $2 WHERE id = $1`, id, monitored)
	if err != nil {
		return fmt.Errorf("movies: set monitored: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return movies.ErrMovieNotFound
	}
	return nil
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
	defer func() { _ = rows.Close() }() // rows consumed by the loop above
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
	defer func() { _ = rows.Close() }() // rows consumed by the loop above
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
	defer func() { _ = rows.Close() }() // rows consumed by the loop above
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

// CreateEdition inserts an edition for a movie and returns its id. The
// (movie_id, label) unique key makes a re-create a no-op.
func (r *MovieRepo) CreateEdition(ctx context.Context, e movies.Edition) (int64, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO editions (movie_id, label, monitored, quality, size)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (movie_id, label) DO NOTHING
		RETURNING id`,
		e.MovieID, e.Label, e.Monitored, e.Quality, e.Size,
	).Scan(&id)
	if err == sql.ErrNoRows {
		// A same-named edition already exists: return it.
		err = r.db.QueryRowContext(ctx, `
			SELECT id FROM editions WHERE movie_id = $1 AND label = $2`,
			e.MovieID, e.Label).Scan(&id)
		if err != nil {
			return 0, fmt.Errorf("movies: create edition: %w", err)
		}
		return id, nil
	}
	if err != nil {
		return 0, fmt.Errorf("movies: create edition: %w", err)
	}
	return id, nil
}

// GetEdition loads an edition by id.
func (r *MovieRepo) GetEdition(ctx context.Context, id int64) (*movies.Edition, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, movie_id, label, monitored, quality, size, created_at
		FROM editions WHERE id = $1`, id)
	e := &movies.Edition{}
	err := row.Scan(&e.ID, &e.MovieID, &e.Label, &e.Monitored, &e.Quality, &e.Size, &e.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, movies.ErrEditionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("movies: get edition: %w", err)
	}
	return e, nil
}

// ListEditions returns every edition, ordered by movie_id then id.
func (r *MovieRepo) ListEditions(ctx context.Context) ([]movies.Edition, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, movie_id, label, monitored, quality, size, created_at
		FROM editions ORDER BY movie_id, id`)
	if err != nil {
		return nil, fmt.Errorf("movies: list editions: %w", err)
	}
	defer func() { _ = rows.Close() }() // rows consumed by the loop above
	var out []movies.Edition
	for rows.Next() {
		var e movies.Edition
		if err := rows.Scan(&e.ID, &e.MovieID, &e.Label, &e.Monitored, &e.Quality, &e.Size, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("movies: scan edition: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// SetEditionMonitored toggles an edition's monitored flag.
func (r *MovieRepo) SetEditionMonitored(ctx context.Context, id int64, monitored bool) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE editions SET monitored = $2 WHERE id = $1`, id, monitored)
	if err != nil {
		return fmt.Errorf("movies: set edition monitored: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return movies.ErrEditionNotFound
	}
	return nil
}

// CreateMovieFile records an imported movie file and returns its id.
// Languages and formats are stored as JSON text (the columns' '[]' default).
func (r *MovieRepo) CreateMovieFile(ctx context.Context, f movies.MovieFile) (int64, error) {
	languages, err := json.Marshal(f.Languages)
	if err != nil {
		return 0, fmt.Errorf("movies: create file: %w", err)
	}
	formats, err := json.Marshal(f.Formats)
	if err != nil {
		return 0, fmt.Errorf("movies: create file: %w", err)
	}
	var editionID any
	if f.EditionID != nil {
		editionID = *f.EditionID
	}
	var id int64
	err = r.db.QueryRowContext(ctx, `
		INSERT INTO movie_files
			(movie_id, edition_id, relative_path, video_codec, audio_info,
			 size, languages, quality, release_group, formats)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id`,
		f.MovieID, editionID, f.RelativePath, f.VideoCodec, f.AudioInfo,
		f.Size, string(languages), f.Quality, f.ReleaseGroup, string(formats),
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("movies: create file: %w", err)
	}
	return id, nil
}

// ListMovieFiles returns every movie file, ordered by movie_id then id.
func (r *MovieRepo) ListMovieFiles(ctx context.Context) ([]movies.MovieFile, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, movie_id, edition_id, relative_path, video_codec, audio_info,
			size, languages, quality, release_group, formats
		FROM movie_files ORDER BY movie_id, id`)
	if err != nil {
		return nil, fmt.Errorf("movies: list files: %w", err)
	}
	defer func() { _ = rows.Close() }() // rows consumed by the loop above
	var out []movies.MovieFile
	for rows.Next() {
		var (
			f  movies.MovieFile
			ed sql.NullInt64
			la string
			fo string
		)
		if err := rows.Scan(&f.ID, &f.MovieID, &ed, &f.RelativePath, &f.VideoCodec, &f.AudioInfo,
			&f.Size, &la, &f.Quality, &f.ReleaseGroup, &fo); err != nil {
			return nil, fmt.Errorf("movies: scan file: %w", err)
		}
		if ed.Valid {
			f.EditionID = &ed.Int64
		}
		if err := json.Unmarshal([]byte(la), &f.Languages); err != nil {
			f.Languages = []string{}
		}
		if err := json.Unmarshal([]byte(fo), &f.Formats); err != nil {
			f.Formats = []string{}
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// GetMovieStats computes the Movies-page stats bar in one query.
func (r *MovieRepo) GetMovieStats(ctx context.Context) (movies.MovieStats, error) {
	var s movies.MovieStats
	err := r.db.QueryRowContext(ctx, `
		SELECT
			(SELECT COUNT(*) FROM movies),
			(SELECT COUNT(*) FROM movie_files),
			(SELECT COUNT(*) FROM movies WHERE monitored),
			(SELECT COUNT(*) FROM movies WHERE NOT monitored),
			(COALESCE((SELECT SUM(size) FROM movie_files), 0))`).
		Scan(&s.MovieCount, &s.FileCount, &s.MonitoredCount, &s.UnmonitoredCount, &s.TotalSize)
	if err != nil {
		return s, fmt.Errorf("movies: stats: %w", err)
	}
	return s, nil
}

// SetMovieCollection persists the movie's TMDB collection (Radarr R3). The
// columns are NOT NULL with ” defaults, so "unknown" is stored as ”/”.
func (r *MovieRepo) SetMovieCollection(ctx context.Context, id int64, collectionID, collectionName string) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE movies
		SET collection_id = $2, collection_name = $3
		WHERE id = $1`, id, collectionID, collectionName)
	if err != nil {
		return fmt.Errorf("movies: set collection: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return movies.ErrMovieNotFound
	}
	return nil
}
