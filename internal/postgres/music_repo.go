package postgres

import (
	"context"
	"database/sql"
	"fmt"

	music "github.com/brandenk514/mediarr/internal/domains/music"
)

// MusicRepo is the Postgres-backed implementation of music.Repo.
type MusicRepo struct {
	db *sql.DB
}

// NewMusicRepo builds a music repository over the given pool.
func NewMusicRepo(db *sql.DB) *MusicRepo { return &MusicRepo{db: db} }

var _ music.Repo = (*MusicRepo)(nil)

// --- Artists ---

// CreateArtist inserts an artist and returns its id.
func (r *MusicRepo) CreateArtist(ctx context.Context, a music.Artist) (int64, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO music_artists (mbid, name, monitored)
		VALUES ($1, $2, $3)
		RETURNING id`,
		a.MBID, a.Name, a.Monitored,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("music: create artist: %w", err)
	}
	return id, nil
}

// GetArtist loads an artist by id.
func (r *MusicRepo) GetArtist(ctx context.Context, id int64) (*music.Artist, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, mbid, name, monitored, added_at
		FROM music_artists WHERE id = $1`, id)
	a := &music.Artist{}
	err := row.Scan(&a.ID, &a.MBID, &a.Name, &a.Monitored, &a.AddedAt)
	if err == sql.ErrNoRows {
		return nil, music.ErrArtistNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("music: get artist: %w", err)
	}
	return a, nil
}

// ListArtists returns all artists, oldest first.
func (r *MusicRepo) ListArtists(ctx context.Context) ([]music.Artist, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, mbid, name, monitored, added_at
		FROM music_artists ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("music: list artists: %w", err)
	}
	defer rows.Close()
	var out []music.Artist
	for rows.Next() {
		var a music.Artist
		if err := rows.Scan(&a.ID, &a.MBID, &a.Name, &a.Monitored, &a.AddedAt); err != nil {
			return nil, fmt.Errorf("music: scan artist: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// SetArtistMonitored toggles an artist's monitored flag.
func (r *MusicRepo) SetArtistMonitored(ctx context.Context, id int64, monitored bool) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE music_artists SET monitored = $2 WHERE id = $1`, id, monitored)
	if err != nil {
		return fmt.Errorf("music: set artist monitored: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return music.ErrArtistNotFound
	}
	return nil
}

// --- Albums ---

// CreateAlbum inserts an album and returns its id.
func (r *MusicRepo) CreateAlbum(ctx context.Context, a music.Album) (int64, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO music_albums
			(artist_id, mbid, name, year, quality_profile, monitored, verify_checksums, checksum)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id`,
		a.ArtistID, a.MBID, a.Name, a.Year, a.QualityProfile, a.Monitored,
		a.VerifyChecksums, a.Checksum,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("music: create album: %w", err)
	}
	return id, nil
}

// GetAlbum loads an album by id.
func (r *MusicRepo) GetAlbum(ctx context.Context, id int64) (*music.Album, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, artist_id, mbid, name, year, quality_profile, monitored,
		       verify_checksums, checksum, added_at
		FROM music_albums WHERE id = $1`, id)
	a := &music.Album{}
	err := row.Scan(&a.ID, &a.ArtistID, &a.MBID, &a.Name, &a.Year,
		&a.QualityProfile, &a.Monitored, &a.VerifyChecksums, &a.Checksum, &a.AddedAt)
	if err == sql.ErrNoRows {
		return nil, music.ErrAlbumNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("music: get album: %w", err)
	}
	return a, nil
}

// ListAlbums returns all albums for an artist, oldest first.
func (r *MusicRepo) ListAlbums(ctx context.Context, artistID int64) ([]music.Album, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, artist_id, mbid, name, year, quality_profile, monitored,
		       verify_checksums, checksum, added_at
		FROM music_albums WHERE artist_id = $1
		ORDER BY year DESC, name, id`, artistID)
	if err != nil {
		return nil, fmt.Errorf("music: list albums: %w", err)
	}
	defer rows.Close()
	var out []music.Album
	for rows.Next() {
		var a music.Album
		if err := rows.Scan(&a.ID, &a.ArtistID, &a.MBID, &a.Name, &a.Year,
			&a.QualityProfile, &a.Monitored, &a.VerifyChecksums, &a.Checksum, &a.AddedAt); err != nil {
			return nil, fmt.Errorf("music: scan album: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// SetAlbumMonitored toggles a single album's monitored flag.
func (r *MusicRepo) SetAlbumMonitored(ctx context.Context, id int64, monitored bool) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE music_albums SET monitored = $2 WHERE id = $1`, id, monitored)
	if err != nil {
		return fmt.Errorf("music: set album monitored: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return music.ErrAlbumNotFound
	}
	return nil
}

// --- Tracks ---

// CreateTrack inserts a track and returns its id.
func (r *MusicRepo) CreateTrack(ctx context.Context, t music.Track) (int64, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO music_tracks (album_id, mbid, disc, number, title, checksum, monitored)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id`,
		t.AlbumID, t.MBID, t.Disc, t.Number, t.Title, t.Checksum, t.Monitored,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("music: create track: %w", err)
	}
	return id, nil
}

// GetTrack loads a single track by (album, disc, number).
func (r *MusicRepo) GetTrack(ctx context.Context, albumID int64, disc, number int) (*music.Track, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, album_id, mbid, disc, number, title, checksum, monitored, added_at
		FROM music_tracks WHERE album_id = $1 AND disc = $2 AND number = $3`,
		albumID, disc, number)
	t := &music.Track{}
	err := row.Scan(&t.ID, &t.AlbumID, &t.MBID, &t.Disc, &t.Number, &t.Title,
		&t.Checksum, &t.Monitored, &t.AddedAt)
	if err == sql.ErrNoRows {
		return nil, music.ErrTrackNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("music: get track: %w", err)
	}
	return t, nil
}

// ListTracks returns all tracks for an album, ordered by disc then number.
func (r *MusicRepo) ListTracks(ctx context.Context, albumID int64) ([]music.Track, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, album_id, mbid, disc, number, title, checksum, monitored, added_at
		FROM music_tracks WHERE album_id = $1
		ORDER BY disc, number`, albumID)
	if err != nil {
		return nil, fmt.Errorf("music: list tracks: %w", err)
	}
	defer rows.Close()
	var out []music.Track
	for rows.Next() {
		var t music.Track
		if err := rows.Scan(&t.ID, &t.AlbumID, &t.MBID, &t.Disc, &t.Number,
			&t.Title, &t.Checksum, &t.Monitored, &t.AddedAt); err != nil {
			return nil, fmt.Errorf("music: scan track: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// SetTrackMonitored toggles a single track's monitored flag.
func (r *MusicRepo) SetTrackMonitored(ctx context.Context, albumID int64, disc, number int, monitored bool) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE music_tracks SET monitored = $4
		WHERE album_id = $1 AND disc = $2 AND number = $3`,
		albumID, disc, number, monitored)
	if err != nil {
		return fmt.Errorf("music: set track monitored: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return music.ErrTrackNotFound
	}
	return nil
}

// --- Wanted (album or track granularity) ---

// EnsureWantedAlbum inserts a pending whole-album wanted row (track_id = 0) if
// one does not already exist. Idempotent.
func (r *MusicRepo) EnsureWantedAlbum(ctx context.Context, albumID int64) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO music_wanted (album_id, track_id, status)
		VALUES ($1, 0, 'pending')
		ON CONFLICT (album_id, track_id) DO NOTHING`, albumID)
	if err != nil {
		return fmt.Errorf("music: ensure wanted album: %w", err)
	}
	return nil
}

// EnsureWantedTrack inserts a pending track-level wanted row if one does not
// already exist. Idempotent.
func (r *MusicRepo) EnsureWantedTrack(ctx context.Context, albumID, trackID int64) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO music_wanted (album_id, track_id, status)
		VALUES ($1, $2, 'pending')
		ON CONFLICT (album_id, track_id) DO NOTHING`, albumID, trackID)
	if err != nil {
		return fmt.Errorf("music: ensure wanted track: %w", err)
	}
	return nil
}

// GetWanted loads a single wanted row by (album, track) — track 0 is the
// whole-album request.
func (r *MusicRepo) GetWanted(ctx context.Context, albumID, trackID int64) (*music.Wanted, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT album_id, track_id, status, release_title, created_at, satisfied_at
		FROM music_wanted WHERE album_id = $1 AND track_id = $2`,
		albumID, trackID)
	w := &music.Wanted{}
	err := row.Scan(&w.AlbumID, &w.TrackID, &w.Status, &w.ReleaseTitle,
		&w.CreatedAt, &w.SatisfiedAt)
	if err == sql.ErrNoRows {
		return nil, sql.ErrNoRows
	}
	if err != nil {
		return nil, fmt.Errorf("music: get wanted: %w", err)
	}
	return w, nil
}

// ListWanted returns all wanted rows for an album (album-level first, then
// track-level), ordered by track id.
func (r *MusicRepo) ListWanted(ctx context.Context, albumID int64) ([]music.Wanted, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT album_id, track_id, status, release_title, created_at, satisfied_at
		FROM music_wanted WHERE album_id = $1
		ORDER BY track_id`, albumID)
	if err != nil {
		return nil, fmt.Errorf("music: list wanted: %w", err)
	}
	defer rows.Close()
	var out []music.Wanted
	for rows.Next() {
		var w music.Wanted
		if err := rows.Scan(&w.AlbumID, &w.TrackID, &w.Status, &w.ReleaseTitle,
			&w.CreatedAt, &w.SatisfiedAt); err != nil {
			return nil, fmt.Errorf("music: scan wanted: %w", err)
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// MarkWantedSatisfied flips a wanted row to satisfied and records the release
// that satisfied it.
func (r *MusicRepo) MarkWantedSatisfied(ctx context.Context, albumID, trackID int64, releaseTitle string) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE music_wanted
		SET status = 'satisfied', release_title = $3, satisfied_at = now()
		WHERE album_id = $1 AND track_id = $2`,
		albumID, trackID, releaseTitle)
	if err != nil {
		return fmt.Errorf("music: satisfy wanted: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("music: no wanted row for album %d track %d", albumID, trackID)
	}
	return nil
}

// --- Queue ---

// CreateQueue inserts a queue entry and returns its id.
func (r *MusicRepo) CreateQueue(ctx context.Context, e music.QueueEntry) (int64, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO music_queue
			(artist_id, album_id, release_title, indexer, download_client, state, progress)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id`,
		e.ArtistID, e.AlbumID, e.ReleaseTitle, e.Indexer,
		e.DownloadClient, e.State, e.Progress,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("music: create queue: %w", err)
	}
	return id, nil
}

// UpdateQueue updates a queue entry's state/progress.
func (r *MusicRepo) UpdateQueue(ctx context.Context, e music.QueueEntry) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE music_queue
		SET state = $2, progress = $3, updated_at = now()
		WHERE id = $1`, e.ID, e.State, e.Progress)
	if err != nil {
		return fmt.Errorf("music: update queue: %w", err)
	}
	return nil
}

// GetQueue loads a queue entry by id.
func (r *MusicRepo) GetQueue(ctx context.Context, id int64) (*music.QueueEntry, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, artist_id, album_id, release_title, indexer, download_client,
		       state, progress, created_at, updated_at
		FROM music_queue WHERE id = $1`, id)
	e := &music.QueueEntry{}
	err := row.Scan(&e.ID, &e.ArtistID, &e.AlbumID, &e.ReleaseTitle,
		&e.Indexer, &e.DownloadClient, &e.State, &e.Progress, &e.CreatedAt, &e.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, sql.ErrNoRows
	}
	if err != nil {
		return nil, fmt.Errorf("music: get queue: %w", err)
	}
	return e, nil
}

// ListQueue returns all queue entries for an artist, ordered by id.
func (r *MusicRepo) ListQueue(ctx context.Context, artistID int64) ([]music.QueueEntry, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, artist_id, album_id, release_title, indexer, download_client,
		       state, progress, created_at, updated_at
		FROM music_queue WHERE artist_id = $1 ORDER BY id`, artistID)
	if err != nil {
		return nil, fmt.Errorf("music: list queue: %w", err)
	}
	defer rows.Close()
	var out []music.QueueEntry
	for rows.Next() {
		var e music.QueueEntry
		if err := rows.Scan(&e.ID, &e.ArtistID, &e.AlbumID, &e.ReleaseTitle,
			&e.Indexer, &e.DownloadClient, &e.State, &e.Progress, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, fmt.Errorf("music: scan queue: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// --- History ---

// AddHistory appends a history event for an artist.
func (r *MusicRepo) AddHistory(ctx context.Context, e music.HistoryEntry) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO music_history (artist_id, event, detail)
		VALUES ($1, $2, $3)`, e.ArtistID, e.Event, e.Detail)
	if err != nil {
		return fmt.Errorf("music: add history: %w", err)
	}
	return nil
}

// ListHistory returns the most recent history events for an artist (newest
// first).
func (r *MusicRepo) ListHistory(ctx context.Context, artistID int64, limit int) ([]music.HistoryEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, artist_id, event, detail, at
		FROM music_history WHERE artist_id = $1
		ORDER BY at DESC, id DESC LIMIT $2`, artistID, limit)
	if err != nil {
		return nil, fmt.Errorf("music: list history: %w", err)
	}
	defer rows.Close()
	var out []music.HistoryEntry
	for rows.Next() {
		var e music.HistoryEntry
		if err := rows.Scan(&e.ID, &e.ArtistID, &e.Event, &e.Detail, &e.At); err != nil {
			return nil, fmt.Errorf("music: scan history: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
