package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/brandenk514/mediarr/internal/indexers"
	indexerdefinitions "github.com/brandenk514/mediarr/internal/indexers/definitions"
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

const indexerColumns = `id, name, kind, base_url, api_key_encrypted, settings_json, enabled, last_test, privacy, priority, sync_profile, added, config_fields_json, definition_name`

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

// scanIndexer reads one row into a Definition, decrypting the key and the
// config-contract password fields.
func (r *IndexersRepo) scanIndexer(row interface {
	Scan(...any) error
}) (*indexers.Definition, error) {
	var def indexers.Definition
	var key *string
	var lastTest *time.Time
	var cfgJSON *string
	var defName *string
	err := row.Scan(&def.ID, &def.Name, &def.Kind, &def.BaseURL, &key,
		&def.SettingsJSON, &def.Enabled, &lastTest,
		&def.Privacy, &def.Priority, &def.SyncProfile, &def.Added,
		&cfgJSON, &defName)
	if err != nil {
		return nil, err
	}
	def.APIKey, err = r.decryptKey(key)
	if err != nil {
		return nil, err
	}
	def.LastTest = lastTest
	def.ConfigFields, err = r.decodeConfigFields(defName, cfgJSON)
	if err != nil {
		return nil, err
	}
	if defName != nil {
		def.DefinitionName = *defName
	}
	return &def, nil
}

// encodeConfigFields serializes the definition's config-contract values for
// at-rest storage: password field values (per the named library definition's
// contract) are encrypted with the app vault before they touch the column —
// this repo is the only layer that touches ciphertext, mirroring encryptKey.
// A nil map is the legacy path and encodes to "{}" with no definition name.
func (r *IndexersRepo) encodeConfigFields(def indexers.Definition) (cfgJSON string, defName *string, err error) {
	if len(def.ConfigFields) == 0 {
		return "{}", nil, nil
	}
	if def.DefinitionName == "" {
		return "", nil, fmt.Errorf("indexers: config_fields without a definition_name cannot be stored (secrets would be unencrypted)")
	}
	contract := indexerdefinitions.Default().ResolveDefinition(def.DefinitionName)
	m := make(map[string]any, len(def.ConfigFields))
	for k, v := range def.ConfigFields {
		if contract != nil && contract.IsSecretField(k) {
			s, ok := v.(string)
			if !ok {
				return "", nil, fmt.Errorf("indexers: secret config field %q must be a string", k)
			}
			if s == "" {
				m[k] = s
				continue
			}
			ct, err := r.vault.EncryptString(s)
			if err != nil {
				return "", nil, fmt.Errorf("indexers: encrypt config field %q: %w", k, err)
			}
			m[k] = ct
			continue
		}
		m[k] = v
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return "", nil, fmt.Errorf("indexers: marshal config fields: %w", err)
	}
	name := def.DefinitionName
	return string(raw), &name, nil
}

// decodeConfigFields parses the stored config-contract JSON and decrypts the
// password field values (the inverse of encodeConfigFields). An empty/blank
// column is the legacy path: nil.
func (r *IndexersRepo) decodeConfigFields(defName, cfgJSON *string) (map[string]any, error) {
	if cfgJSON == nil || *cfgJSON == "" || *cfgJSON == "{}" {
		return nil, nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(*cfgJSON), &m); err != nil {
		return nil, fmt.Errorf("indexers: unmarshal config fields: %w", err)
	}
	if len(m) == 0 {
		return nil, nil
	}
	if defName != nil && *defName != "" {
		if contract := indexerdefinitions.Default().ResolveDefinition(*defName); contract != nil {
			for k, v := range m {
				if !contract.IsSecretField(k) {
					continue
				}
				s, ok := v.(string)
				if !ok || s == "" {
					continue // never a ciphertext
				}
				pt, err := r.vault.DecryptString(s)
				if err != nil {
					return nil, fmt.Errorf("indexers: decrypt config field %q: %w", k, err)
				}
				m[k] = pt
			}
		}
	}
	return m, nil
}

// Create inserts a new indexer definition and returns its id. The PR1 hub
// fields (privacy / priority / sync_profile / added) fall back to the wire
// defaults when the caller leaves them zero, so a create that omits them still
// lands a row the hub can render.
func (r *IndexersRepo) Create(ctx context.Context, def indexers.Definition) (int64, error) {
	enc, err := r.encryptKey(def.APIKey)
	if err != nil {
		return 0, err
	}
	cfgJSON, defName, err := r.encodeConfigFields(def)
	if err != nil {
		return 0, err
	}
	privacy := def.Privacy
	if privacy == "" {
		privacy = "public"
	}
	syncProfile := def.SyncProfile
	if syncProfile == "" {
		syncProfile = "standard"
	}
	priority := def.Priority
	if priority == 0 {
		priority = 25
	}
	added := def.Added
	if added.IsZero() {
		added = time.Now().UTC()
	}
	var id int64
	err = r.db.QueryRowContext(ctx, `
		INSERT INTO indexers (name, kind, base_url, api_key_encrypted, settings_json, enabled,
		                               privacy, priority, sync_profile, added,
		                               config_fields_json, definition_name)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING id`,
		def.Name, def.Kind, def.BaseURL, enc, def.SettingsJSON, def.Enabled,
		privacy, priority, syncProfile, added,
		cfgJSON, defName,
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
	defer func() { _ = rows.Close() }() // rows consumed by the loop above
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
// It updates by id and errors if no row matches. The PR1 hub fields
// (privacy / priority / sync_profile) are updated alongside the core fields;
// `added` is immutable and is left untouched here.
func (r *IndexersRepo) Update(ctx context.Context, def indexers.Definition) error {
	enc, err := r.encryptKey(def.APIKey)
	if err != nil {
		return err
	}
	cfgJSON, defName, err := r.encodeConfigFields(def)
	if err != nil {
		return err
	}
	res, err := r.db.ExecContext(ctx, `
		UPDATE indexers
		SET name = $2, kind = $3, base_url = $4, api_key_encrypted = $5,
		    settings_json = $6, enabled = $7,
		    privacy = $8, priority = $9, sync_profile = $10,
		    config_fields_json = $11, definition_name = $12,
		    updated_at = now()
		WHERE id = $1`,
		def.ID, def.Name, def.Kind, def.BaseURL, enc, def.SettingsJSON, def.Enabled,
		def.Privacy, def.Priority, def.SyncProfile,
		cfgJSON, defName)
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
