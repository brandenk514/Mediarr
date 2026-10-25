package postgres

import (
	"context"
	"database/sql"
	"time"

	cdom "github.com/brandenk514/mediarr/internal/domains/calendar"
)

// CalendarRepo is the P4 repository: it selects the episodes the calendar
// renders over — every TV episode with a known air date, joined with its
// series title and monitor flag. The pure window/future filtering and iCal
// rendering live in the domain, not here.
type CalendarRepo struct {
	db *sql.DB
}

var _ cdom.Repo = (*CalendarRepo)(nil)

func NewCalendarRepo(db *sql.DB) *CalendarRepo { return &CalendarRepo{db: db} }

// Episodes selects every episode with a non-null air date, ordered by air
// date then show, season, episode (a stable, chronological calendar order).
// The episode's own monitor flag is returned as-is; the domain decides which
// rows count as "monitored".
func (r *CalendarRepo) Episodes(ctx context.Context) ([]cdom.EpisodeRow, error) {
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

	var out []cdom.EpisodeRow
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
		out = append(out, cdom.EpisodeRow{
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
