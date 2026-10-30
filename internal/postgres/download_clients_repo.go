package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/brandenk514/mediarr/internal/downloadclients"
	"github.com/brandenk514/mediarr/internal/secrets"
)

// DownloadClientsRepo is the Postgres-backed implementation of
// downloadclients.Repo (Prowlarr PR5, migration 0026). Like IndexersRepo and
// NotificationsRepo it is the single layer that owns the at-rest encryption of
// client secrets: the API key is vault-encrypted before it touches the
// download_clients table and decrypted on the way out, the same convention as
// indexers.api_key_encrypted. Nothing outside this repo sees the ciphertext,
// and the REST layer never echoes a key back.
type DownloadClientsRepo struct {
	db    *sql.DB
	vault *secrets.Vault
}

var _ downloadclients.Repo = (*DownloadClientsRepo)(nil)

const downloadClientColumns = `id, name, kind, enabled, position, base_url, username, api_key, complete_dir, created_at, updated_at`

// NewDownloadClientsRepo constructs the repo over an open pool. vault must be
// the app vault (main.go builds one from the config encryption key).
func NewDownloadClientsRepo(db *sql.DB, vault *secrets.Vault) *DownloadClientsRepo {
	return &DownloadClientsRepo{db: db, vault: vault}
}

// encryptKey maps a plaintext API key to its at-rest form: the vault
// ciphertext, or "" for no key.
func (r *DownloadClientsRepo) encryptKey(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	ct, err := r.vault.EncryptString(plaintext)
	if err != nil {
		return "", fmt.Errorf("encrypt api key: %w", err)
	}
	return ct, nil
}

// decryptKey maps the at-rest form back to the plaintext API key.
func (r *DownloadClientsRepo) decryptKey(ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}
	pt, err := r.vault.DecryptString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("decrypt api key: %w", err)
	}
	return pt, nil
}

// scanDownloadClientRow scans one row; apiKey is the raw (still-encrypted)
// column value the caller decrypts with the vault.
func scanDownloadClientRow(row interface{ Scan(...any) error }) (*downloadclients.Record, string, error) {
	var (
		rec  downloadclients.Record
		kind string
		key  string
	)
	if err := row.Scan(&rec.ID, &rec.Name, &kind, &rec.Enabled, &rec.Position, &rec.BaseURL, &rec.Username, &key, &rec.CompleteDir, &rec.CreatedAt, &rec.UpdatedAt); err != nil {
		return nil, "", err
	}
	rec.Kind = downloadclients.Kind(kind)
	return &rec, key, nil
}

// Create stores r and returns the DB-assigned id and created_at.
func (r *DownloadClientsRepo) Create(ctx context.Context, rec downloadclients.Record) (int64, time.Time, error) {
	encKey, err := r.encryptKey(rec.APIKey)
	if err != nil {
		return 0, time.Time{}, err
	}
	var id int64
	var createdAt time.Time
	err = r.db.QueryRowContext(ctx, `
		INSERT INTO download_clients (name, kind, enabled, position, base_url, username, api_key, complete_dir)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, created_at`,
		rec.Name, string(rec.Kind), rec.Enabled, rec.Position, rec.BaseURL, rec.Username, encKey, rec.CompleteDir,
	).Scan(&id, &createdAt)
	if err != nil {
		return 0, time.Time{}, err
	}
	return id, createdAt, nil
}

// Get returns the client with id, or sql.ErrNoRows.
func (r *DownloadClientsRepo) Get(ctx context.Context, id int64) (*downloadclients.Record, error) {
	rec, key, err := scanDownloadClientRow(r.db.QueryRowContext(ctx,
		`SELECT `+downloadClientColumns+` FROM download_clients WHERE id = $1`, id))
	if err != nil {
		return nil, err
	}
	plain, err := r.decryptKey(key)
	if err != nil {
		return nil, err
	}
	rec.APIKey = plain
	return rec, nil
}

// GetByName returns the client named name, or sql.ErrNoRows when none.
func (r *DownloadClientsRepo) GetByName(ctx context.Context, name string) (*downloadclients.Record, error) {
	rec, key, err := scanDownloadClientRow(r.db.QueryRowContext(ctx,
		`SELECT `+downloadClientColumns+` FROM download_clients WHERE name = $1`, name))
	if err != nil {
		return nil, err
	}
	plain, err := r.decryptKey(key)
	if err != nil {
		return nil, err
	}
	rec.APIKey = plain
	return rec, nil
}

// List returns every saved client in display order (position ascending, then
// id ascending).
func (r *DownloadClientsRepo) List(ctx context.Context) ([]downloadclients.Record, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+downloadClientColumns+` FROM download_clients ORDER BY position, id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []downloadclients.Record
	for rows.Next() {
		rec, key, err := scanDownloadClientRow(rows)
		if err != nil {
			return nil, err
		}
		plain, err := r.decryptKey(key)
		if err != nil {
			return nil, err
		}
		rec.APIKey = plain
		out = append(out, *rec)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// Update fully replaces the row (callers merge kept secrets before calling).
// sql.ErrNoRows when no row has rec.ID.
func (r *DownloadClientsRepo) Update(ctx context.Context, rec downloadclients.Record) error {
	encKey, err := r.encryptKey(rec.APIKey)
	if err != nil {
		return err
	}
	res, err := r.db.ExecContext(ctx, `
		UPDATE download_clients
		SET name = $1, kind = $2, enabled = $3, position = $4, base_url = $5,
		    username = $6, api_key = $7, complete_dir = $8, updated_at = now()
		WHERE id = $9`,
		rec.Name, string(rec.Kind), rec.Enabled, rec.Position, rec.BaseURL, rec.Username, encKey, rec.CompleteDir, rec.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// Delete removes the client. sql.ErrNoRows when no row has id.
func (r *DownloadClientsRepo) Delete(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM download_clients WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// SeedLegacySingleClient migrates the pre-PR5 single download-client config
// (the "download_client" key of the settings table, written by the old
// settings store) into the plural download_clients table at boot. Raw SQL
// cannot call the vault, so the seed runs in Go — main wires it right after
// the migrations. Two consequences follow from the legacy storage shape:
//
//   - The legacy document stores its secrets vault-ENCRYPTED (the old store
//     sealed them with the same app vault), so the seed copies the ciphertexts
//     verbatim into the new rows: no plaintext ever materializes and nothing
//     is re-encrypted.
//   - One row is inserted per configured part: a qBittorrent row (name
//     "qBittorrent") when the legacy client was a qBittorrent one, a SABnzbd
//     row (name "SABnzbd") when it was SABnzbd. A "mock" config seeds
//     nothing.
//
// Idempotent: each name is only inserted when no row of that name exists
// yet, so a restart never duplicates the seeded clients. It returns the
// number of rows inserted.
func (r *DownloadClientsRepo) SeedLegacySingleClient(ctx context.Context) (int, error) {
	var raw string
	err := r.db.QueryRowContext(ctx,
		`SELECT value_json FROM settings WHERE key = $1`, dcStorageKey).Scan(&raw)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("download clients: read legacy config: %w", err)
	}
	var st dcStored
	if err := json.Unmarshal([]byte(raw), &st); err != nil {
		return 0, fmt.Errorf("download clients: parse legacy config: %w", err)
	}

	seeded := 0
	if st.Kind == "qbittorrent" {
		n, err := r.insertSeededIfAbsent(ctx, "qBittorrent", downloadclients.KindQB, st.QBBase, st.QBUsername, st.QBAPIKey, "")
		if err != nil {
			return seeded, err
		}
		seeded += n
	}
	if st.Kind == "sabnzbd" {
		n, err := r.insertSeededIfAbsent(ctx, "SABnzbd", downloadclients.KindSAB, st.SABBase, "", st.SABAPIKey, st.SABCompleteDir)
		if err != nil {
			return seeded, err
		}
		seeded += n
	}
	return seeded, nil
}

// insertSeededIfAbsent inserts one seeded legacy row (ciphertexts carried
// verbatim, enabled, position 1) when no client of that name exists and that
// part was configured (a non-empty base URL). Returns the number of rows it
// inserted (0 or 1).
func (r *DownloadClientsRepo) insertSeededIfAbsent(ctx context.Context, name string, kind downloadclients.Kind, base, username, apiKeyCiphertext, completeDir string) (int, error) {
	var exists int
	err := r.db.QueryRowContext(ctx,
		`SELECT 1 FROM download_clients WHERE name = $1`, name).Scan(&exists)
	if err != nil && err != sql.ErrNoRows {
		return 0, err
	}
	if err == nil {
		return 0, nil // already seeded (or created by the user)
	}
	if base == "" {
		return 0, nil // that part was not configured
	}
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO download_clients (name, kind, base_url, username, api_key, complete_dir)
		VALUES ($1, $2, $3, $4, $5, $6)`, name, string(kind), base, username, apiKeyCiphertext, completeDir)
	if err != nil {
		return 0, err
	}
	return 1, nil
}
