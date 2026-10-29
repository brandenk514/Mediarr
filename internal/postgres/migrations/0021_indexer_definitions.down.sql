ALTER TABLE indexers
    DROP COLUMN IF EXISTS config_fields_json,
    DROP COLUMN IF EXISTS definition_name;

DROP INDEX IF EXISTS idx_indexer_definitions_name;
DROP INDEX IF EXISTS idx_indexer_definitions_privacy;
DROP INDEX IF EXISTS idx_indexer_definitions_protocol;
DROP TABLE IF EXISTS indexer_definitions;
