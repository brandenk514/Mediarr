-- 0002_movies.up.sql
-- M1: movie pipeline tables. A movie, its "want" request, the download
-- queue, and the per-movie history. Postgres is the source of truth; everything
-- here is re-derivable (nothing lives only in Redis).

CREATE TABLE IF NOT EXISTS movies (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    title           TEXT        NOT NULL,
    year            INTEGER     NOT NULL DEFAULT 0,
    quality_profile TEXT        NOT NULL DEFAULT 'HD-1080p',
    monitored       BOOLEAN     NOT NULL DEFAULT TRUE,
    added_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (title, year)
);
CREATE INDEX IF NOT EXISTS idx_movies_monitored ON movies(monitored);

-- wanted: a per-movie "we want a file" request. One row per movie.
CREATE TABLE IF NOT EXISTS wanted (
    movie_id       BIGINT      NOT NULL REFERENCES movies(id) ON DELETE CASCADE,
    status         TEXT        NOT NULL DEFAULT 'pending'
                  CHECK (status IN ('pending', 'satisfied')),
    release_title  TEXT        NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    satisfied_at   TIMESTAMPTZ,
    PRIMARY KEY (movie_id)
);

-- queue: one download job. state advances queued -> downloading -> complete.
CREATE TABLE IF NOT EXISTS queue (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    movie_id        BIGINT      NOT NULL REFERENCES movies(id) ON DELETE CASCADE,
    release_title   TEXT        NOT NULL,
    indexer         TEXT        NOT NULL DEFAULT '',
    download_client TEXT        NOT NULL DEFAULT '',
    state           TEXT        NOT NULL DEFAULT 'queued'
                    CHECK (state IN ('queued', 'downloading', 'complete', 'failed')),
    progress        INTEGER     NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_queue_movie ON queue(movie_id);
CREATE INDEX IF NOT EXISTS idx_queue_state ON queue(state);

-- history: append-only audit trail of a movie's lifecycle.
CREATE TABLE IF NOT EXISTS history (
    id       BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    movie_id BIGINT      NOT NULL REFERENCES movies(id) ON DELETE CASCADE,
    event    TEXT        NOT NULL,
    detail   TEXT        NOT NULL DEFAULT '',
    at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_history_movie ON history(movie_id, at DESC);
