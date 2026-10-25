package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	wd "github.com/brandenk514/mediarr/internal/domains/wanted"
)

// WantedRepo is the Postgres adapter for wanted.Repo (P3 global Wanted). It
// reads across the tv_* tables (episodes + series + the per-episode wanted
// rows, which carry the file state: a satisfied wanted row means the
// episode has a file, and its quality is the satisfying release's quality)
// and writes the monitor toggle, the global history, and the wanted
// request/satisfaction transitions the P3 actions need.
type WantedRepo struct {
	db *sql.DB
}

// NewWantedRepo wraps a *sql.DB as a wanted.Repo.
func NewWantedRepo(db *sql.DB) *WantedRepo { return &WantedRepo{db: db} }

var _ wd.Repo = (*WantedRepo)(nil)

// EpisodeRows implements wanted.Repo: every episode of every series,
// ordered by series title, season, episode, each with its file state.
func (r *WantedRepo) EpisodeRows(ctx context.Context) ([]wd.EpisodeRow, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT e.series_id, s.title, e.season, e.episode, e.title, e.air_date,
		       e.monitored,
		       CASE WHEN w.status = 'satisfied' THEN 1 ELSE 0 END,
		       COALESCE(w.quality, '')
		FROM tv_episodes e
		JOIN tv_series s ON s.id = e.series_id
		LEFT JOIN tv_wanted w
			ON w.series_id = e.series_id AND w.season = e.season AND w.episode = e.episode
		ORDER BY s.title, e.season, e.episode`)
	if err != nil {
		return nil, fmt.Errorf("wanted: query episodes: %w", err)
	}
	defer func() { _ = rows.Close() }() // rows consumed by the loop below

	var out []wd.EpisodeRow
	for rows.Next() {
		var (
			seriesID    int64
			seriesTitle string
			season      int
			episode     int
			episodeName string
			airDate     *time.Time
			monitored   bool
			hasFile     int
			fileQuality string
		)
		if err := rows.Scan(&seriesID, &seriesTitle, &season, &episode, &episodeName,
			&airDate, &monitored, &hasFile, &fileQuality); err != nil {
			return nil, fmt.Errorf("wanted: scan episode: %w", err)
		}
		out = append(out, wd.EpisodeRow{
			SeriesID:     seriesID,
			SeriesTitle:  seriesTitle,
			Season:       season,
			Episode:      episode,
			EpisodeTitle: episodeName,
			AirDate:      airDate,
			Monitored:    monitored,
			HasFile:      hasFile == 1,
			FileQuality:  fileQuality,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("wanted: iterate episodes: %w", err)
	}
	if out == nil {
		out = []wd.EpisodeRow{}
	}
	return out, nil
}

// SeriesProfiles implements wanted.Repo: series id to quality-profile name.
func (r *WantedRepo) SeriesProfiles(ctx context.Context) (map[int64]string, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, quality_profile FROM tv_series`)
	if err != nil {
		return nil, fmt.Errorf("wanted: query series profiles: %w", err)
	}
	defer func() { _ = rows.Close() }() // rows consumed by the loop below

	out := make(map[int64]string)
	for rows.Next() {
		var (
			id      int64
			profile string
		)
		if err := rows.Scan(&id, &profile); err != nil {
			return nil, fmt.Errorf("wanted: scan series: %w", err)
		}
		out[id] = profile
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("wanted: iterate series: %w", err)
	}
	return out, nil
}

// EnsureWantedPending implements wanted.Repo: inserts a pending wanted row
// for the episode, or resets an existing satisfied row back to pending
// (re-requesting the search). Pending rows are left untouched.
func (r *WantedRepo) EnsureWantedPending(ctx context.Context, seriesID int64, season, episode int) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO tv_wanted (series_id, season, episode, status)
		VALUES ($1, $2, $3, 'pending')
		ON CONFLICT (series_id, season, episode)
		DO UPDATE SET status = 'pending', satisfied_at = NULL`,
		seriesID, season, episode)
	if err != nil {
		return fmt.Errorf("wanted: ensure pending: %w", err)
	}
	return nil
}

// SetEpisodeMonitoredOff implements wanted.Repo: turns monitoring off for
// one episode and reports whether a row was updated.
func (r *WantedRepo) SetEpisodeMonitoredOff(ctx context.Context, seriesID int64, season, episode int) (bool, error) {
	res, err := r.db.ExecContext(ctx, `
		UPDATE tv_episodes SET monitored = false
		WHERE series_id = $1 AND season = $2 AND episode = $3`,
		seriesID, season, episode)
	if err != nil {
		return false, fmt.Errorf("wanted: unmonitor: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("wanted: unmonitor rows affected: %w", err)
	}
	return n > 0, nil
}

// GetSeries implements wanted.Repo: one series (nil when it does not exist).
func (r *WantedRepo) GetSeries(ctx context.Context, id int64) (*wd.SeriesRef, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, title, quality_profile FROM tv_series WHERE id = $1`, id)
	s := &wd.SeriesRef{}
	if err := row.Scan(&s.ID, &s.Title, &s.QualityProfile); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("wanted: get series: %w", err)
	}
	return s, nil
}

// GetEpisode implements wanted.Repo: one episode (nil when it does not
// exist). Only the wire shape (series, season, episode) is carried back —
// the service uses it for the existence check.
func (r *WantedRepo) GetEpisode(ctx context.Context, seriesID int64, season, episode int) (*wd.EpisodeRef, error) {
	var id int64
	var title string
	var monitored bool
	err := r.db.QueryRowContext(ctx, `
		SELECT id, title, monitored
		FROM tv_episodes WHERE series_id = $1 AND season = $2 AND episode = $3`,
		seriesID, season, episode).Scan(&id, &title, &monitored)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("wanted: get episode: %w", err)
	}
	return &wd.EpisodeRef{SeriesID: seriesID, Season: season, Episode: episode}, nil
}

// AddHistory implements wanted.Repo: appends a global (TV) history event.
func (r *WantedRepo) AddHistory(ctx context.Context, kind string, parentID int64, event, detail string) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO tv_history (series_id, event, detail)
		VALUES ($1, $2, $3)`, parentID, event, detail)
	if err != nil {
		return fmt.Errorf("wanted: add history: %w", err)
	}
	return nil
}

// SatisfyWanted implements wanted.Repo: marks the episode's wanted row
// satisfied by the given release + quality. It is a no-op when no row
// exists (the episode was unmonitored): the import still lands, it just
// does not retire a request.
func (r *WantedRepo) SatisfyWanted(ctx context.Context, seriesID int64, season, episode int, releaseTitle, quality string) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO tv_wanted (series_id, season, episode, status, release_title, quality, satisfied_at)
		VALUES ($1, $2, $3, 'satisfied', $4, $5, now())
		ON CONFLICT (series_id, season, episode)
		DO UPDATE SET status = 'satisfied',
		              release_title = EXCLUDED.release_title,
		              quality = EXCLUDED.quality,
		              satisfied_at = now()`,
		seriesID, season, episode, releaseTitle, quality)
	if err != nil {
		return fmt.Errorf("wanted: satisfy: %w", err)
	}
	return nil
}
