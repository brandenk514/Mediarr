package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	caldomain "github.com/brandenk514/mediarr/internal/domains/calendar"
	movies "github.com/brandenk514/mediarr/internal/domains/movies"
)

// CalendarRepo is the P4 repository: it selects the episodes the calendar
// renders over — every TV episode with a known air date, joined with its
// series title and monitor flag. The pure window/future filtering and iCal
// rendering live in the domain, not here.
type CalendarRepo struct {
	db *sql.DB
}

var _ caldomain.Repo = (*CalendarRepo)(nil)

func NewCalendarRepo(db *sql.DB) *CalendarRepo { return &CalendarRepo{db: db} }

// Episodes selects every episode with a non-null air date, ordered by air
// date then show, season, episode (a stable, chronological calendar order).
// The episode's own monitor flag is returned as-is; the domain decides which
// rows count as "monitored".
func (r *CalendarRepo) Episodes(ctx context.Context) ([]caldomain.EpisodeRow, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT
    e.series_id,
    s.title,
    e.season,
    e.episode,
    e.title,
    e.air_date,
    e.monitored
FROM tv_episodes e
JOIN tv_series s ON s.id = e.series_id
WHERE e.air_date IS NOT NULL
ORDER BY e.air_date, s.title, e.season, e.episode`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }() // rows consumed by the loop below

	var out []caldomain.EpisodeRow
	for rows.Next() {
		var (
			seriesID    int64
			seriesTitle string
			season      int
			episode     int
			episodeName string
			airDate     *time.Time
			monitored   bool
		)
		if err := rows.Scan(&seriesID, &seriesTitle, &season, &episode, &episodeName, &airDate, &monitored); err != nil {
			return nil, err
		}
		out = append(out, caldomain.EpisodeRow{
			SeriesID:     seriesID,
			SeriesTitle:  seriesTitle,
			Season:       season,
			Episode:      episode,
			EpisodeTitle: episodeName,
			AirDate:      airDate,
			Monitored:    monitored,
		})
	}
	return out, rows.Err()
}

// MovieRows returns the R4 movie-calendar rows: every movie with a known
// (non-empty) release date, with its monitor flag and the state the status
// derivation needs (a satisfied wanted row — at most one per movie — plus an
// active queue entry), ordered by release date then title.
func (r *CalendarRepo) MovieRows(ctx context.Context) ([]caldomain.MovieRow, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT m.id, m.title, m.year, m.release_date, m.monitored, m.quality_profile,
		       EXISTS (SELECT 1 FROM wanted w
		               WHERE w.movie_id = m.id AND w.status = 'satisfied') AS wanted_satisfied,
		       EXISTS (SELECT 1 FROM queue q
		               WHERE q.movie_id = m.id AND q.state IN ('queued', 'downloading')) AS in_queue
		FROM movies m
		WHERE m.release_date <> ''
		ORDER BY m.release_date, m.title`)
	if err != nil {
		return nil, fmt.Errorf("calendar: movie rows: %w", err)
	}
	defer func() { _ = rows.Close() }() // rows consumed by the loop above
	var out []caldomain.MovieRow
	for rows.Next() {
		var (
			row caldomain.MovieRow
			rd  string
		)
		if err := rows.Scan(&row.ID, &row.Title, &row.Year, &rd, &row.Monitored,
			&row.QualityProfile, &row.WantedSatisfied, &row.InQueue); err != nil {
			return nil, fmt.Errorf("calendar: scan movie row: %w", err)
		}
		row.ReleaseDate = movies.ParseReleaseDate(rd)
		out = append(out, row)
	}
	return out, rows.Err()
}
