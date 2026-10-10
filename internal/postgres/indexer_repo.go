package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/brandenk514/mediarr/internal/indexers"
	"github.com/brandenk514/mediarr/internal/secrets"
)

// IndexersRepo is the Postgres-backed implementation of indexers.IndexerRepo
// (migration 0007). It is the single layer responsible for the at-rest
// encryption of indexer API keys (PLAN §7): keys are encrypted with the app
// vault before they touch the indexers table and decrypted on the way out. No
// other code path in the repo reads or writes the ciphertext column.
type IndexersRepo struct {
	db    *sql.DB
	vault *secrets.Vault
}

// NewIndexersRepo builds an indexer repository over the given pool using the
// given vault for key encryption. The vault is required: there is no
// unencrypted mode, so a caller cannot accidentally store a raw API key.
func NewIndexersRepo(db *sql.DB, vault *secrets.Vault) *IndexersRepo {
	return &IndexersRepo{db: db, vault: vault}
}

var _ indexers.IndexerRepo = (*IndexersRepo)(nil)

const indexerColumns = `id, name, kind, base_url, api_key_encrypted, settings_json, enabled, last_test`

// encryptKey stores the ciphertext for a plaintext key, or NULL for an empty
// key (keyless indexers). An empty plaintext never round-trips through the
// vault, so a NULL column always means "no key".
func (r *IndexersRepo) encryptKey(plaintext string) (any, error) {
	if plaintext == "" {
		return nil, nil
	}
	ct, err := r.vault.EncryptString(plaintext)
	if err != nil {
		return nil, fmt.Errorf("indexers: encrypt api key: %w", err)
	}
	return ct, nil
}

// decryptKey turns a stored ciphertext (or NULL) back into a plaintext key.
func (r *IndexersRepo) decryptKey(ciphertext *string) (string, error) {
	if ciphertext == nil {
		return "", nil
	}
	pt, err := r.vault.DecryptString(*ciphertext)
	if err != nil {
		return "", fmt.Errorf("indexers: decrypt api key: %w", err)
	}
	return pt, nil
}

// scanIndexer reads one row into a Definition, decrypting the key.
func (r *IndexersRepo) scanIndexer(row interface {
	Scan(...any) error
}) (*indexers.Definition, error) {
	var def indexers.Definition
	var key *string
	var lastTest *time.Time
	err := row.Scan(&def.ID, &def.Name, &def.Kind, &def.BaseURL, &key,
		&def.SettingsJSON, &def.Enabled, &lastTest)
	if err != nil {
		return nil, err
	}
	def.APIKey, err = r.decryptKey(key)
	if err != nil {
		return nil, err
	}
	def.LastTest = lastTest
	return &def, nil
}

// Create inserts a new indexer definition and returns its id.
func (r *IndexersRepo) Create(ctx context.Context, def indexers.Definition) (int64, error) {
	enc, err := r.encryptKey(def.APIKey)
	if err != nil {
		return 0, err
	}
	var id int64
	err = r.db.QueryRowContext(ctx, `
		INSERT INTO indexers (name, kind, base_url, api_key_encrypted, settings_json, enabled)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id`,
		def.Name, def.Kind, def.BaseURL, enc, def.SettingsJSON, def.Enabled,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("indexers: create %q: %w", def.Name, err)
	}
	return id, nil
}

// Get loads a definition by id.
func (r *IndexersRepo) Get(ctx context.Context, id int64) (*indexers.Definition, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+indexerColumns+` FROM indexers WHERE id = $1`, id)
	def, err := r.scanIndexer(row)
	if err == sql.ErrNoRows {
		return nil, sql.ErrNoRows
	}
	if err != nil {
		return nil, fmt.Errorf("indexers: get %d: %w", id, err)
	}
	return def, nil
}

// GetByName loads a definition by its unique name.
func (r *IndexersRepo) GetByName(ctx context.Context, name string) (*indexers.Definition, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+indexerColumns+` FROM indexers WHERE name = $1`, name)
	def, err := r.scanIndexer(row)
	if err == sql.ErrNoRows {
		return nil, sql.ErrNoRows
	}
	if err != nil {
		return nil, fmt.Errorf("indexers: get by name %q: %w", name, err)
	}
	return def, nil
}

// List returns all definitions, ordered by id.
func (r *IndexersRepo) List(ctx context.Context) ([]indexers.Definition, error) {
	return r.listByQuery(ctx, `SELECT `+indexerColumns+` FROM indexers ORDER BY id`)
}

// ListEnabled returns only enabled definitions, ordered by id. This is the set
// the search fan-out is built from.
func (r *IndexersRepo) ListEnabled(ctx context.Context) ([]indexers.Definition, error) {
	return r.listByQuery(ctx,
		`SELECT `+indexerColumns+` FROM indexers WHERE enabled = true ORDER BY id`)
}

func (r *IndexersRepo) listByQuery(ctx context.Context, query string) ([]indexers.Definition, error) {
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("indexers: list: %w", err)
	}
	defer rows.Close()
	var out []indexers.Definition
	for rows.Next() {
		def, err := r.scanIndexer(rows)
		if err != nil {
			return nil, fmt.Errorf("indexers: scan row: %w", err)
		}
		out = append(out, *def)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("indexers: list rows: %w", err)
	}
	return out, nil
}

// Update replaces a definition's mutable fields, re-encrypting the API key.
// It updates by id and errors if no row matches.
func (r *IndexersRepo) Update(ctx context.Context, def indexers.Definition) error {
	enc, err := r.encryptKey(def.APIKey)
	if err != nil {
		return err
	}
	res, err := r.db.ExecContext(ctx, `
		UPDATE indexers
		SET name = $2, kind = $3, base_url = $4, api_key_encrypted = $5,
		    settings_json = $6, enabled = $7, updated_at = now()
		WHERE id = $1`,
		def.ID, def.Name, def.Kind, def.BaseURL, enc, def.SettingsJSON, def.Enabled)
	if err != nil {
		return fmt.Errorf("indexers: update %d: %w", def.ID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("indexers: no indexer with id %d", def.ID)
	}
	return nil
}

// Delete removes a definition by id.
func (r *IndexersRepo) Delete(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM indexers WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("indexers: delete %d: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("indexers: no indexer with id %d", id)
	}
	return nil
}

// SetEnabled toggles a definition's enabled flag.
func (r *IndexersRepo) SetEnabled(ctx context.Context, id int64, enabled bool) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE indexers SET enabled = $2, updated_at = now() WHERE id = $1`,
		id, enabled)
	if err != nil {
		return fmt.Errorf("indexers: set enabled %d: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("indexers: no indexer with id %d", id)
	}
	return nil
}

// SetLastTest records the timestamp of a health probe (for #34). A nil time
// clears it back to NULL.
func (r *IndexersRepo) SetLastTest(ctx context.Context, id int64, at *time.Time) error {
	var val any
	if at != nil {
		val = *at
	}
	res, err := r.db.ExecContext(ctx,
		`UPDATE indexers SET last_test = $2, updated_at = now() WHERE id = $1`,
		id, val)
	if err != nil {
		return fmt.Errorf("indexers: set last_test %d: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("indexers: no indexer with id %d", id)
	}
	return nil
}
