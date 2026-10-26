-- Movie add-flow fields (Radarr R2): tags, root folder, minimum availability.
-- Additive/backward-compatible: existing rows get sensible defaults.
-- Tags follow the Series convention (0009: TEXT[]).
ALTER TABLE movies ADD COLUMN IF NOT EXISTS tags                  TEXT[] NOT NULL DEFAULT '{}';
ALTER TABLE movies ADD COLUMN IF NOT EXISTS root_folder           TEXT   NOT NULL DEFAULT '';
ALTER TABLE movies ADD COLUMN IF NOT EXISTS minimum_availability  TEXT   NOT NULL DEFAULT '';
