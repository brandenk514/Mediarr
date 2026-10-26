-- Radarr R1: movie editions + imported movie files.
--
-- editions: one row per distinct edition of a movie (Radarr v6 editions:
-- theatrical, extended, ...). A freshly added movie is seeded with one
-- 'Theatrical' edition by the create path.
CREATE TABLE IF NOT EXISTS editions (
    id         BIGSERIAL PRIMARY KEY,
    movie_id   BIGINT NOT NULL REFERENCES movies(id) ON DELETE CASCADE,
    label      TEXT NOT NULL,
    monitored  BOOLEAN NOT NULL DEFAULT true,
    quality    TEXT NOT NULL DEFAULT '',
    size       BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (movie_id, label)
);
CREATE INDEX IF NOT EXISTS idx_editions_movie ON editions (movie_id);

-- movie_files: imported files (one or more per movie; edition_id links the
-- file to the edition it satisfies, null = unassigned).
CREATE TABLE IF NOT EXISTS movie_files (
    id            BIGSERIAL PRIMARY KEY,
    movie_id      BIGINT NOT NULL REFERENCES movies(id) ON DELETE CASCADE,
    edition_id    BIGINT REFERENCES editions(id) ON DELETE SET NULL,
    relative_path TEXT NOT NULL,
    video_codec   TEXT NOT NULL DEFAULT '',
    audio_info    TEXT NOT NULL DEFAULT '',
    size          BIGINT NOT NULL DEFAULT 0,
    languages     TEXT NOT NULL DEFAULT '[]',
    quality       TEXT NOT NULL DEFAULT '',
    release_group TEXT NOT NULL DEFAULT '',
    formats       TEXT NOT NULL DEFAULT '[]'
);
CREATE INDEX IF NOT EXISTS idx_movie_files_movie ON movie_files (movie_id);
