package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	movies "github.com/brandenk514/mediarr/internal/domains/movies"
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

// --- L4 music (kind=music) ---

// MusicRows implements wanted.Repo: one album-level row per
// effectively-monitored album (artist AND album monitored) plus one row per
// monitored track of such an album, ordered by artist name, album (year,
// name), disc, number. The enriched catalog's release date is a text
// YYYY-MM-DD column ("" = unknown), parsed to the wire shape's *time.Time.
func (r *WantedRepo) MusicRows(ctx context.Context) ([]wd.MusicRow, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT ar.id, ar.name,
		       al.id, al.name, COALESCE(al.album_type, ''), al.year, COALESCE(al.release_date, ''),
		       COALESCE(al.quality_profile, ''),
		       (ar.monitored AND al.monitored),
		       0, 0, 0, '', false,
		       (SELECT COUNT(*) FROM music_tracks t WHERE t.album_id = al.id),
		       (SELECT COUNT(*) FROM music_tracks t WHERE t.album_id = al.id AND t.status <> ''),
		       COALESCE((SELECT array_agg(t.status) FROM music_tracks t
		                 WHERE t.album_id = al.id AND t.status <> ''), '{}')
		FROM music_artists ar
		JOIN music_albums al ON al.artist_id = ar.id
		WHERE ar.monitored AND al.monitored
		UNION ALL
		SELECT ar.id, ar.name,
		       al.id, al.name, COALESCE(al.album_type, ''), al.year, COALESCE(al.release_date, ''),
		       COALESCE(al.quality_profile, ''),
		       (ar.monitored AND al.monitored),
		       t.disc, t.number, t.id, t.title, t.monitored,
		       0, 0, '{}'::text[]
		FROM music_artists ar
		JOIN music_albums al ON al.artist_id = ar.id
		JOIN music_tracks t ON t.album_id = al.id
		WHERE ar.monitored AND al.monitored AND t.monitored
		ORDER BY 2, 6, 4, 10, 11`)
	if err != nil {
		return nil, fmt.Errorf("wanted: music rows: %w", err)
	}
	defer func() { _ = rows.Close() }() // rows consumed by the loop below
	var out []wd.MusicRow
	for rows.Next() {
		var (
			m    wd.MusicRow
			rd   string
			prof string
		)
		if err := rows.Scan(&m.ArtistID, &m.ArtistTitle,
			&m.AlbumID, &m.AlbumTitle, &m.AlbumType, &m.Year, &rd,
			&prof, &m.Monitored,
			&m.Disc, &m.Number, &m.TrackID, &m.TrackTitle, &m.TrackMonitored,
			&m.TrackCount, &m.FileCount, &m.ImportedQualities); err != nil {
			return nil, fmt.Errorf("wanted: scan music row: %w", err)
		}
		m.ReleaseDate = movies.ParseReleaseDate(rd)
		m.ProfileName = prof
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("wanted: iterate music rows: %w", err)
	}
	return out, nil
}

// EnsureMusicWanted implements wanted.Repo: inserts a pending music_wanted
// row for the ref (whole album when disc/number are zero, else the track),
// or resets an existing satisfied row back to pending (re-requesting the
// search). Pending rows are left untouched. An unknown track falls back to
// the whole-album row so the search still has something to bite on.
func (r *WantedRepo) EnsureMusicWanted(ctx context.Context, albumID int64, disc, number int) error {
	trackID := int64(0)
	if disc > 0 || number > 0 {
		err := r.db.QueryRowContext(ctx, `
			SELECT id FROM music_tracks
			WHERE album_id = $1 AND disc = $2 AND number = $3`,
			albumID, disc, number).Scan(&trackID)
		if err != nil && err != sql.ErrNoRows {
			return fmt.Errorf("wanted: music track lookup: %w", err)
		}
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO music_wanted (album_id, track_id, status)
		VALUES ($1, $2, 'pending')
		ON CONFLICT (album_id, track_id)
		DO UPDATE SET status = 'pending', release_title = '', satisfied_at = NULL`,
		albumID, trackID)
	if err != nil {
		return fmt.Errorf("wanted: ensure music wanted: %w", err)
	}
	return nil
}

// SetMusicMonitoredOff implements wanted.Repo: turns monitoring off for the
// music ref (whole album when disc/number are zero, else the one track) and
// reports whether a row was updated.
func (r *WantedRepo) SetMusicMonitoredOff(ctx context.Context, albumID int64, disc, number int) (bool, error) {
	var (
		res sql.Result
		err error
	)
	if disc > 0 || number > 0 {
		res, err = r.db.ExecContext(ctx, `
			UPDATE music_tracks SET monitored = false
			WHERE album_id = $1 AND disc = $2 AND number = $3`,
			albumID, disc, number)
	} else {
		res, err = r.db.ExecContext(ctx, `
			UPDATE music_albums SET monitored = false WHERE id = $1`, albumID)
	}
	if err != nil {
		return false, fmt.Errorf("wanted: unmonitor music: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("wanted: unmonitor music rows affected: %w", err)
	}
	return n > 0, nil
}

// AddMusicHistory implements wanted.Repo: appends a music_history event —
// the kind "music" source of the global Activity feed (the music service's
// own per-artist feed is the same table).
func (r *WantedRepo) AddMusicHistory(ctx context.Context, artistID int64, event, detail string) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO music_history (artist_id, event, detail)
		VALUES ($1, $2, $3)`, artistID, event, detail)
	if err != nil {
		return fmt.Errorf("wanted: add music history: %w", err)
	}
	return nil
}
