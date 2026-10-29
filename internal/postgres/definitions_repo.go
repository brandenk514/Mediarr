package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	indexerdefinitions "github.com/brandenk514/mediarr/internal/indexers/definitions"
)

// IndexerDefinitionsRepo is the Postgres-backed store for the indexer
// definition library (migration 0021). Rows are seeded from the embedded
// seed (internal/indexers/definitions/definitions.json) at boot when the
// table is empty; each row records the seed revision that produced it so the
// definition-update task (Prowlarr PR6) can compare stored revisions against
// the current seed, detect stale rows, and re-upsert them.
type IndexerDefinitionsRepo struct {
	db *sql.DB
}

// NewIndexerDefinitionsRepo builds the definition-library repository over the
// given pool.
func NewIndexerDefinitionsRepo(db *sql.DB) *IndexerDefinitionsRepo {
	return &IndexerDefinitionsRepo{db: db}
}

// StoredDefinition is a library definition plus the seed revision that
// produced its row (PR6 compares revisions to detect stale rows).
type StoredDefinition struct {
	indexerdefinitions.Definition
	SeedRevision string
}

const definitionColumns = `id, name, protocol, privacy, language, categories, urls, legacy_urls, fields, raw, seed_revision`

// scanDefinitionRow reads one library row.
func (r *IndexerDefinitionsRepo) scanDefinitionRow(row interface {
	Scan(...any) error
}) (*StoredDefinition, error) {
	var d StoredDefinition
	var (
		categories []byte
		urls       []byte
		legacy     []byte
		fields     []byte
	)
	if err := row.Scan(&d.ID, &d.Name, &d.Protocol, &d.Privacy, &d.Language,
		&categories, &urls, &legacy, &fields, &d.Raw, &d.SeedRevision); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(categories, &d.Categories); err != nil {
		return nil, fmt.Errorf("definitions: %s categories: %w", d.ID, err)
	}
	if err := json.Unmarshal(urls, &d.URLs); err != nil {
		return nil, fmt.Errorf("definitions: %s urls: %w", d.ID, err)
	}
	if err := json.Unmarshal(legacy, &d.LegacyURLs); err != nil {
		return nil, fmt.Errorf("definitions: %s legacy_urls: %w", d.ID, err)
	}
	if err := json.Unmarshal(fields, &d.Fields); err != nil {
		return nil, fmt.Errorf("definitions: %s fields: %w", d.ID, err)
	}
	return &d, nil
}

// List returns every library definition ordered by display name.
func (r *IndexerDefinitionsRepo) List(ctx context.Context) ([]indexerdefinitions.Definition, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+definitionColumns+` FROM indexer_definitions ORDER BY lower(name), id`)
	if err != nil {
		return nil, fmt.Errorf("definitions: list: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []indexerdefinitions.Definition
	for rows.Next() {
		d, err := r.scanDefinitionRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d.Definition)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("definitions: list rows: %w", err)
	}
	return out, nil
}

// Get loads a single library definition by id.
func (r *IndexerDefinitionsRepo) Get(ctx context.Context, id string) (*indexerdefinitions.Definition, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+definitionColumns+` FROM indexer_definitions WHERE id = $1`, id)
	d, err := r.scanDefinitionRow(row)
	if err == sql.ErrNoRows {
		return nil, sql.ErrNoRows
	}
	if err != nil {
		return nil, fmt.Errorf("definitions: get %q: %w", id, err)
	}
	return &d.Definition, nil
}

// SeedIfEmpty seeds the table from the given definitions when it is empty.
// It is idempotent: a non-empty table (however it got there, including a
// PR6 update) is left untouched, and the returned count is the number of
// rows inserted (0 when the table already had data).
func (r *IndexerDefinitionsRepo) SeedIfEmpty(ctx context.Context, defs []indexerdefinitions.Definition, revision string) (int, error) {
	var n int
	if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM indexer_definitions`).Scan(&n); err != nil {
		return 0, fmt.Errorf("definitions: count: %w", err)
	}
	if n > 0 {
		return 0, nil
	}
	if len(defs) == 0 {
		return 0, fmt.Errorf("definitions: refusing to seed an empty seed")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("definitions: begin seed: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after Commit
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO indexer_definitions
		(id, name, protocol, privacy, language, categories, urls, legacy_urls, fields, raw, seed_revision)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`)
	if err != nil {
		return 0, fmt.Errorf("definitions: prepare seed: %w", err)
	}
	defer func() { _ = stmt.Close() }()
	for i := range defs {
		d := &defs[i]
		cats, err := json.Marshal(d.Categories)
		if err != nil {
			return 0, fmt.Errorf("definitions: %s categories: %w", d.ID, err)
		}
		urls, err := json.Marshal(d.URLs)
		if err != nil {
			return 0, fmt.Errorf("definitions: %s urls: %w", d.ID, err)
		}
		legacy, err := json.Marshal(d.LegacyURLs)
		if err != nil {
			return 0, fmt.Errorf("definitions: %s legacy_urls: %w", d.ID, err)
		}
		fields, err := json.Marshal(d.Fields)
		if err != nil {
			return 0, fmt.Errorf("definitions: %s fields: %w", d.ID, err)
		}
		if _, err := stmt.ExecContext(ctx, d.ID, d.Name, d.Protocol, d.Privacy,
			d.Language, cats, urls, legacy, fields, d.Raw, revision); err != nil {
			return 0, fmt.Errorf("definitions: seed %s: %w", d.ID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("definitions: commit seed: %w", err)
	}
	return len(defs), nil
}
