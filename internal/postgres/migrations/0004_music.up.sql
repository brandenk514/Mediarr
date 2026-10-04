-- 0004_music.up.sql
-- M3: Music domain tables. The artist → album → track hierarchy (PLAN §4/§6),
-- per-album/track "want" requests (so monitoring works at album and track
-- granularity), the download queue, and a per-artist history. Postgres is the
-- source of truth; everything here is re-derivable (nothing lives only in
-- Redis).
--
-- Table names are prefixed (music_*) so the music domain does not collide with
-- the movie (movies/wanted/queue/history) or TV (tv_*) tables in the same
-- database.

CREATE TABLE IF NOT EXISTS music_artists (
    id                BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    mbid              TEXT        NOT NULL DEFAULT '',  -- MusicBrainz artist id ('' = unset; provider lands in M5)
    name              TEXT        NOT NULL,
    monitored         BOOLEAN     NOT NULL DEFAULT TRUE,
    added_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (name)
);
CREATE INDEX IF NOT EXISTS idx_music_artists_monitored ON music_artists(monitored);

-- music_albums: a known album within an artist, keyed by (artist, name, year).
-- verify_checksums is the per-library opt-out for checksum verification
-- (PLAN §15.5: on by default, opt-out per library) — default TRUE.
CREATE TABLE IF NOT EXISTS music_albums (
    id                 BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    artist_id          BIGINT      NOT NULL REFERENCES music_artists(id) ON DELETE CASCADE,
    mbid               TEXT        NOT NULL DEFAULT '',  -- MusicBrainz release id ('' = unset)
    name               TEXT        NOT NULL,
    year               INTEGER     NOT NULL DEFAULT 0,
    quality_profile    TEXT        NOT NULL DEFAULT 'Lossless',
    monitored          BOOLEAN     NOT NULL DEFAULT FALSE,
    verify_checksums   BOOLEAN     NOT NULL DEFAULT TRUE,
    checksum           TEXT        NOT NULL DEFAULT '',  -- expected sha256 of the whole-album file, when known
    added_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (artist_id, name, year)
);
CREATE INDEX IF NOT EXISTS idx_music_albums_artist ON music_albums(artist_id);

-- music_tracks: a single known track within an album, keyed by
-- (album, disc, number). Track number is the 1-based position on its disc.
CREATE TABLE IF NOT EXISTS music_tracks (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    album_id    BIGINT      NOT NULL REFERENCES music_albums(id) ON DELETE CASCADE,
    mbid        TEXT        NOT NULL DEFAULT '',  -- MusicBrainz track id ('' = unset)
    disc        INTEGER     NOT NULL DEFAULT 1,
    number      INTEGER     NOT NULL,
    title       TEXT        NOT NULL DEFAULT '',
    checksum    TEXT        NOT NULL DEFAULT '',  -- expected sha256 of the file, when known
    monitored   BOOLEAN     NOT NULL DEFAULT FALSE,
    added_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (album_id, disc, number)
);
CREATE INDEX IF NOT EXISTS idx_music_tracks_album ON music_tracks(album_id);

-- music_wanted: a "we want a file" request at album or track granularity.
-- track_id = 0 is the whole-album request; track_id > 0 is a specific track.
CREATE TABLE IF NOT EXISTS music_wanted (
    album_id      BIGINT      NOT NULL REFERENCES music_albums(id) ON DELETE CASCADE,
    track_id      BIGINT      NOT NULL DEFAULT 0,  -- 0 = whole album
    status        TEXT        NOT NULL DEFAULT 'pending'
                  CHECK (status IN ('pending', 'satisfied')),
    release_title TEXT        NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    satisfied_at  TIMESTAMPTZ,
    PRIMARY KEY (album_id, track_id)
);
CREATE INDEX IF NOT EXISTS idx_music_wanted_pending ON music_wanted(status);

-- music_queue: one download job for an album release (or a single track).
CREATE TABLE IF NOT EXISTS music_queue (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    artist_id       BIGINT      NOT NULL REFERENCES music_artists(id) ON DELETE CASCADE,
    album_id        BIGINT      NOT NULL REFERENCES music_albums(id) ON DELETE CASCADE,
    release_title   TEXT        NOT NULL,
    indexer         TEXT        NOT NULL DEFAULT '',
    download_client TEXT        NOT NULL DEFAULT '',
    state           TEXT        NOT NULL DEFAULT 'queued'
                    CHECK (state IN ('queued', 'downloading', 'complete', 'failed')),
    progress        INTEGER     NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_music_queue_artist ON music_queue(artist_id);
CREATE INDEX IF NOT EXISTS idx_music_queue_state ON music_queue(state);

-- music_history: append-only audit trail of an artist's lifecycle.
CREATE TABLE IF NOT EXISTS music_history (
    id        BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    artist_id BIGINT      NOT NULL REFERENCES music_artists(id) ON DELETE CASCADE,
    event     TEXT        NOT NULL,
    detail    TEXT        NOT NULL DEFAULT '',
    at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_music_history_artist ON music_history(artist_id, at DESC);
