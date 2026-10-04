-- 0002_movies.down.sql
-- Reverts the M1 movie pipeline tables. Drop children before parents so the
-- foreign keys (wanted/queue/history -> movies) are removed first. These are the
-- unprefixed M1 tables; the TV domain uses tv_* names (0003) and is untouched.
DROP TABLE IF EXISTS history;
DROP TABLE IF EXISTS queue;
DROP TABLE IF EXISTS wanted;
DROP TABLE IF EXISTS movies;
