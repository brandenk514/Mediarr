-- 0001_initial_schema.down.sql
DROP TABLE IF EXISTS schema_migrations;
DROP TABLE IF EXISTS settings;
DROP TABLE IF EXISTS api_tokens;
DROP TABLE IF EXISTS users;
DROP EXTENSION IF EXISTS pgcrypto;
