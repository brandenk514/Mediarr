-- 0003_tv.up.sql
-- M2: TV domain tables. A series, its known episodes, per-episode "want"
-- requests (so monitoring works at episode and season granularity), the
-- download queue, and a per-series history. Postgres is the source of truth;
-- everything here is re-derivable (nothing lives only in Redis).
--
-- Table names are prefixed (tv_*) so the TV domain does not collide with the
-- M1 movie tables (movies/wanted/queue/history) in the same database.

CREATE TABLE IF NOT EXISTS tv_series (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    tvdb_id         INTEGER     NOT NULL DEFAULT 0,   -- nullable provider id (0 = unset)
    tmdb_id         INTEGER     NOT NULL DEFAULT 0,   -- nullable provider id (0 = unset)
    title           TEXT        NOT NULL,
    year            INTEGER     NOT NULL DEFAULT 0,
    quality_profile TEXT        NOT NULL DEFAULT 'HD-1080p',
    monitored       BOOLEAN     NOT NULL DEFAULT TRUE,
    added_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (title, year)
);
CREATE INDEX IF NOT EXISTS idx_tv_series_monitored ON tv_series(monitored);

-- tv_episodes: a single known episode within a series, keyed by (series,
-- season, episode).
CREATE TABLE IF NOT EXISTS tv_episodes (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    series_id   BIGINT      NOT NULL REFERENCES tv_series(id) ON DELETE CASCADE,
    season      INTEGER     NOT NULL,
    episode     INTEGER     NOT NULL,
    title       TEXT        NOT NULL DEFAULT '',
    monitored   BOOLEAN     NOT NULL DEFAULT TRUE,
    added_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (series_id, season, episode)
);
CREATE INDEX IF NOT EXISTS idx_tv_episodes_series ON tv_episodes(series_id);

-- tv_wanted: a per-episode "we want a file" request. One row per (series,
-- season, episode) — the episode granularity the plan requires for TV
-- monitoring (PLAN §4).
CREATE TABLE IF NOT EXISTS tv_wanted (
    series_id     BIGINT      NOT NULL REFERENCES tv_series(id) ON DELETE CASCADE,
    season        INTEGER     NOT NULL,
    episode       INTEGER     NOT NULL,
    status        TEXT        NOT NULL DEFAULT 'pending'
                  CHECK (status IN ('pending', 'satisfied')),
    release_title TEXT        NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    satisfied_at  TIMESTAMPTZ,
    PRIMARY KEY (series_id, season, episode)
);
CREATE INDEX IF NOT EXISTS idx_tv_wanted_pending ON tv_wanted(status);

-- tv_queue: one download job for an episode (or season pack).
CREATE TABLE IF NOT EXISTS tv_queue (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    series_id       BIGINT      NOT NULL REFERENCES tv_series(id) ON DELETE CASCADE,
    season          INTEGER     NOT NULL,
    episode         INTEGER     NOT NULL DEFAULT 0,   -- 0 for a season-pack job
    release_title   TEXT        NOT NULL,
    indexer         TEXT        NOT NULL DEFAULT '',
    download_client TEXT        NOT NULL DEFAULT '',
    state           TEXT        NOT NULL DEFAULT 'queued'
                    CHECK (state IN ('queued', 'downloading', 'complete', 'failed')),
    progress        INTEGER     NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_tv_queue_series ON tv_queue(series_id);
CREATE INDEX IF NOT EXISTS idx_tv_queue_state ON tv_queue(state);

-- tv_history: append-only audit trail of a series' lifecycle.
CREATE TABLE IF NOT EXISTS tv_history (
    id       BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    series_id BIGINT      NOT NULL REFERENCES tv_series(id) ON DELETE CASCADE,
    event    TEXT        NOT NULL,
    detail   TEXT        NOT NULL DEFAULT '',
    at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_tv_history_series ON tv_history(series_id, at DESC);
