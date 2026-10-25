-- 0010: P3 Activity + Wanted parity (part of the Sonarr P1-P7 plan, #2).
--
-- 1. blocklist — the blocked (rejected) releases Activity > Blocklist lists:
--    a rejected release with its media kind + parent, indexer, and reason.
CREATE TABLE IF NOT EXISTS blocklist (
    id            BIGSERIAL PRIMARY KEY,
    kind          TEXT        NOT NULL,
    parent_id     BIGINT      NOT NULL,
    release_title TEXT        NOT NULL,
    indexer       TEXT        NOT NULL DEFAULT '',
    reason        TEXT        NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS blocklist_kind_idx ON blocklist (kind);
CREATE INDEX IF NOT EXISTS blocklist_parent_idx ON blocklist (kind, parent_id);

-- 2. tv_episodes.air_date — the first air date, shown in the global Wanted
--    table (Activity IA: air-date column). Nullable: episodes can exist
--    without a known air date.
ALTER TABLE tv_episodes ADD COLUMN IF NOT EXISTS air_date DATE;

-- 3. tv_wanted.quality — the quality of the release that satisfied the
--    episode. The Cutoff Unmet tab compares a file's quality against the
--    series' cutoff, so the satisfying quality has to be remembered.
ALTER TABLE tv_wanted ADD COLUMN IF NOT EXISTS quality TEXT NOT NULL DEFAULT '';
