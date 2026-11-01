package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/brandenk514/mediarr/internal/secrets"
	"github.com/brandenk514/mediarr/internal/settings"
)

// SettingsRepo is the Postgres-backed implementation of settings.Store (#44).
// It backs the settings table (migration 0001: key/value_json) with the
// per-library override documents (key "library.<kind>") and, read-only since
// Prowlarr PR5, the legacy single download-client document (key
// "download_client"), which SeedLegacySingleClient in
// download_clients_repo.go migrates into the plural download_clients table.
//
// Like IndexersRepo, it is the single layer responsible for at-rest
// encryption of secrets: values are sealed with the app vault before they
// touch the settings table and opened on the way out. Nothing above this
// layer sees ciphertext, and the REST layer never echoes a secret back.
type SettingsRepo struct {
	db    *sql.DB
	vault *secrets.Vault
}

// NewSettingsRepo builds a settings repository over the given pool using the
// given vault for secret encryption. The vault is required: there is no
// unencrypted mode, so a caller cannot accidentally store a raw credential.
func NewSettingsRepo(db *sql.DB, vault *secrets.Vault) *SettingsRepo {
	return &SettingsRepo{db: db, vault: vault}
}

var _ settings.Store = (*SettingsRepo)(nil)

// Storage keys in the settings table.
const (
	dcStorageKey = "download_client"
)

// libraryKey is the settings-table key for a per-library override.
func libraryKey(k settings.LibraryKind) string { return "library." + k.String() }

// dcStored is the on-wire (JSONB) shape of the download-client document. The
// three secret fields hold hex ciphertext (or "" when unset); every other
// field is plaintext. It exists only in this package so the at-rest form can
// differ from the in-memory settings.DownloadClient (which carries plaintext).
type dcStored struct {
	Kind           string `json:"kind"`
	QBBase         string `json:"qb_base"`
	QBAPIKey       string `json:"qb_api_key"`
	QBUsername     string `json:"qb_username"`
	QBPassword     string `json:"qb_password"`
	SABBase        string `json:"sab_base"`
	SABAPIKey      string `json:"sab_api_key"`
	SABCompleteDir string `json:"sab_complete_dir"`
}

// encryptField stores ciphertext for a plaintext secret, or "" when empty.
func (r *SettingsRepo) encryptField(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	ct, err := r.vault.EncryptString(plaintext)
	if err != nil {
		return "", fmt.Errorf("settings: encrypt secret: %w", err)
	}
	return ct, nil
}

// decryptField turns a stored ciphertext (or "") back into a plaintext secret.
func (r *SettingsRepo) decryptField(ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}
	pt, err := r.vault.DecryptString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("settings: decrypt secret: %w", err)
	}
	return pt, nil
}

// getJSON reads the value_json for key, or ("", false) when absent.
func (r *SettingsRepo) getJSON(ctx context.Context, key string) (string, bool, error) {
	var raw string
	err := r.db.QueryRowContext(ctx,
		`SELECT value_json FROM settings WHERE key = $1`, key,
	).Scan(&raw)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("settings: read %q: %w", key, err)
	}
	return raw, true, nil
}

// putJSON upserts value_json for key.
func (r *SettingsRepo) putJSON(ctx context.Context, key, json string) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO settings (key, value_json) VALUES ($1, $2::jsonb)
		ON CONFLICT (key) DO UPDATE SET value_json = $2::jsonb, updated_at = now()`,
		key, json)
	if err != nil {
		return fmt.Errorf("settings: write %q: %w", key, err)
	}
	return nil
}

// deleteKey removes the row for key (a no-op when absent).
func (r *SettingsRepo) deleteKey(ctx context.Context, key string) error {
	if _, err := r.db.ExecContext(ctx,
		`DELETE FROM settings WHERE key = $1`, key); err != nil {
		return fmt.Errorf("settings: delete %q: %w", key, err)
	}
	return nil
}

// GetLibrary returns the saved override for k, or a zero override when none
// has been saved.
func (r *SettingsRepo) GetLibrary(ctx context.Context, k settings.LibraryKind) (*settings.LibraryOverride, error) {
	if !k.IsValid() {
		return nil, fmt.Errorf("%w: %q", settings.ErrInvalidLibraryKind, k)
	}
	raw, ok, err := r.getJSON(ctx, libraryKey(k))
	if err != nil {
		return nil, err
	}
	if !ok {
		return &settings.LibraryOverride{}, nil
	}
	var o settings.LibraryOverride
	if err := json.Unmarshal([]byte(raw), &o); err != nil {
		return nil, fmt.Errorf("settings: parse library %q: %w", k, err)
	}
	return &o, nil
}

// PutLibrary upserts the override for k. A zero override clears it (the row is
// removed so a later read yields a fresh default).
func (r *SettingsRepo) PutLibrary(ctx context.Context, k settings.LibraryKind, o *settings.LibraryOverride) error {
	if !k.IsValid() {
		return fmt.Errorf("%w: %q", settings.ErrInvalidLibraryKind, k)
	}
	if o == nil || o.IsZero() {
		return r.deleteKey(ctx, libraryKey(k))
	}
	raw, err := json.Marshal(o)
	if err != nil {
		return fmt.Errorf("settings: marshal library %q: %w", k, err)
	}
	return r.putJSON(ctx, libraryKey(k), string(raw))
}
