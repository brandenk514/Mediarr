-- 0015_music_l1.up.sql
-- Lidarr L1: artist library grid + artist detail display fields.
-- Additive/backward-compatible: existing rows get sensible defaults.
--
-- links is a JSON array of {"Name": ..., "URL": ...} objects stored as TEXT
-- (the movie_files languages/formats convention, 0011: TEXT NOT NULL
-- DEFAULT '[]' + json.Unmarshal in the repo); '[]' = no links.
--
-- Album TrackCount/FileCount and the library summary are computed in SQL
-- over the tracks at read time — deliberately NO denormalized counters
-- (L1 plan: keep the single source of truth in music_tracks).

-- music_artists: grid card footer + detail pills.
ALTER TABLE music_artists ADD COLUMN IF NOT EXISTS status          TEXT            NOT NULL DEFAULT 'continuing'; -- continuing / ended / inactive
ALTER TABLE music_artists ADD COLUMN IF NOT EXISTS quality_profile TEXT            NOT NULL DEFAULT '';  -- per-artist profile name; '' = none (L2 assigns)
ALTER TABLE music_artists ADD COLUMN IF NOT EXISTS path            TEXT            NOT NULL DEFAULT '';  -- subfolder under the root; '' = use the artist name
ALTER TABLE music_artists ADD COLUMN IF NOT EXISTS rating          DOUBLE PRECISION NOT NULL DEFAULT 0;  -- 0 = unknown
ALTER TABLE music_artists ADD COLUMN IF NOT EXISTS genre           TEXT            NOT NULL DEFAULT '';  -- '' = unknown
ALTER TABLE music_artists ADD COLUMN IF NOT EXISTS links           TEXT            NOT NULL DEFAULT '[]'; -- JSON array of {Name, URL}

-- music_albums: accordion group type + row/sub-header fields.
ALTER TABLE music_albums ADD COLUMN IF NOT EXISTS album_type   TEXT            NOT NULL DEFAULT 'album'; -- album / ep / single / compilation / live / remix / broadcast / demo / djmix / other
ALTER TABLE music_albums ADD COLUMN IF NOT EXISTS release_date TEXT            NOT NULL DEFAULT '';  -- YYYY-MM-DD; '' = unknown (year stays as-is)
ALTER TABLE music_albums ADD COLUMN IF NOT EXISTS rating       DOUBLE PRECISION NOT NULL DEFAULT 0;  -- 0 = unknown
ALTER TABLE music_albums ADD COLUMN IF NOT EXISTS duration     INTEGER         NOT NULL DEFAULT 0;  -- seconds; 0 = unknown
ALTER TABLE music_albums ADD COLUMN IF NOT EXISTS size         BIGINT          NOT NULL DEFAULT 0;  -- bytes; 0 = unknown

-- music_tracks: per-track audio info (the track table's Audio Info + Status
-- columns). Populated by the file tracker — deferred, L1 leaves these at
-- zero/empty for imported files.
ALTER TABLE music_tracks ADD COLUMN IF NOT EXISTS duration    INTEGER NOT NULL DEFAULT 0;  -- seconds; 0 = unknown
ALTER TABLE music_tracks ADD COLUMN IF NOT EXISTS codec       TEXT    NOT NULL DEFAULT '';  -- e.g. FLAC, MP3
ALTER TABLE music_tracks ADD COLUMN IF NOT EXISTS channels    INTEGER NOT NULL DEFAULT 0;  -- 0 = unknown
ALTER TABLE music_tracks ADD COLUMN IF NOT EXISTS kbps        INTEGER NOT NULL DEFAULT 0;  -- 0 = unknown
ALTER TABLE music_tracks ADD COLUMN IF NOT EXISTS sample_rate INTEGER NOT NULL DEFAULT 0;  -- kHz; 0 = unknown
ALTER TABLE music_tracks ADD COLUMN IF NOT EXISTS bit_depth   INTEGER NOT NULL DEFAULT 0;  -- 0 = unknown
ALTER TABLE music_tracks ADD COLUMN IF NOT EXISTS size        BIGINT  NOT NULL DEFAULT 0;  -- bytes; 0 = unknown
ALTER TABLE music_tracks ADD COLUMN IF NOT EXISTS status      TEXT    NOT NULL DEFAULT '';  -- '' = missing/no file; else imported file's quality string
-- FileCount / summary file counts key off status (non-empty = has a file).
CREATE INDEX IF NOT EXISTS idx_music_tracks_status ON music_tracks (status);
