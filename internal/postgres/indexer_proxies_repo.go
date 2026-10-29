package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/brandenk514/mediarr/internal/indexers"
	"github.com/brandenk514/mediarr/internal/secrets"
)

// IndexerProxiesRepo is the Postgres-backed implementation of
// indexers.ProxyRepo (migration 0024). It is the single layer that owns the
// at-rest encryption of FlareSolverr basic-auth passwords: password is
// vault-encrypted before it touches the indexer_proxies table and decrypted
// on the way out, the same convention as indexers.api_key_encrypted.
// Nothing outside this repo ever sees the ciphertext.
type IndexerProxiesRepo struct {
	db    *sql.DB
	vault *secrets.Vault
}

var _ indexers.ProxyRepo = (*IndexerProxiesRepo)(nil)

const indexerProxyColumns = `id, name, url, username, password, tags, created_at`

// NewIndexerProxiesRepo constructs the repo over an open pool. vault must be
// the app vault (main.go builds one from the config encryption key).
func NewIndexerProxiesRepo(db *sql.DB, vault *secrets.Vault) *IndexerProxiesRepo {
	return &IndexerProxiesRepo{db: db, vault: vault}
}

// encryptPassword maps a plaintext password to its at-rest form: the vault
// ciphertext, or NULL for an empty password.
func (r *IndexerProxiesRepo) encryptPassword(plaintext string) (any, error) {
	if plaintext == "" {
		return nil, nil
	}
	ct, err := r.vault.EncryptString(plaintext)
	if err != nil {
		return nil, fmt.Errorf("encrypt proxy password: %w", err)
	}
	return ct, nil
}

// decryptPassword maps the at-rest form back to the plaintext password.
func (r *IndexerProxiesRepo) decryptPassword(ciphertext *string) (string, error) {
	if ciphertext == nil {
		return "", nil
	}
	pt, err := r.vault.DecryptString(*ciphertext)
	if err != nil {
		return "", fmt.Errorf("decrypt proxy password: %w", err)
	}
	return pt, nil
}

// scanProxyRow scans one row; password is the raw (still-encrypted or NULL)
// column value the caller decrypts with the vault.
func scanProxyRow(row interface{ Scan(...any) error }) (*indexers.Proxy, *string, []byte, time.Time, error) {
	var (
		p        indexers.Proxy
		password *string
		tags     []byte
		created  time.Time
	)
	if err := row.Scan(&p.ID, &p.Name, &p.URL, &p.Username, &password, &tags, &created); err != nil {
		return nil, nil, nil, time.Time{}, err
	}
	return &p, password, tags, created, nil
}

// openProxy decrypts the row's password and tags, ready to return.
func (r *IndexerProxiesRepo) openProxy(p *indexers.Proxy, password *string, tags []byte, created time.Time) (*indexers.Proxy, error) {
	pt, err := r.decryptPassword(password)
	if err != nil {
		return nil, err
	}
	var list []string
	if len(tags) > 0 {
		if err := json.Unmarshal(tags, &list); err != nil {
			return nil, fmt.Errorf("decode tags: %w", err)
		}
	}
	if list == nil {
		list = []string{}
	}
	p.Password = pt
	p.Tags = list
	p.CreatedAt = created
	return p, nil
}

// List returns every saved proxy in creation order (id ascending).
func (r *IndexerProxiesRepo) List(ctx context.Context) ([]indexers.Proxy, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+indexerProxyColumns+` FROM indexer_proxies ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []indexers.Proxy
	for rows.Next() {
		p, password, tags, created, err := scanProxyRow(rows)
		if err != nil {
			return nil, err
		}
		opened, err := r.openProxy(p, password, tags, created)
		if err != nil {
			return nil, err
		}
		out = append(out, *opened)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// Create stores the proxy and returns the DB-assigned id and created_at.
func (r *IndexerProxiesRepo) Create(ctx context.Context, p indexers.Proxy) (int64, time.Time, error) {
	password, err := r.encryptPassword(p.Password)
	if err != nil {
		return 0, time.Time{}, err
	}
	tagJSON, err := encodeTags(p.Tags)
	if err != nil {
		return 0, time.Time{}, err
	}
	err = r.db.QueryRowContext(ctx, `
		INSERT INTO indexer_proxies (name, url, username, password, tags)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at`,
		p.Name, p.URL, p.Username, password, tagJSON,
	).Scan(&p.ID, &p.CreatedAt)
	if err != nil {
		return 0, time.Time{}, err
	}
	return p.ID, p.CreatedAt, nil
}

// Get returns one proxy by id, or sql.ErrNoRows for an unknown id.
func (r *IndexerProxiesRepo) Get(ctx context.Context, id int64) (*indexers.Proxy, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+indexerProxyColumns+` FROM indexer_proxies WHERE id = $1`, id)
	p, password, tags, created, err := scanProxyRow(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, sql.ErrNoRows
		}
		return nil, err
	}
	return r.openProxy(p, password, tags, created)
}

// Update fully replaces the proxy's fields (the caller has already merged in
// any secret or tags that must be kept).
func (r *IndexerProxiesRepo) Update(ctx context.Context, p indexers.Proxy) error {
	password, err := r.encryptPassword(p.Password)
	if err != nil {
		return err
	}
	tagJSON, err := encodeTags(p.Tags)
	if err != nil {
		return err
	}
	res, err := r.db.ExecContext(ctx, `
		UPDATE indexer_proxies
		SET name = $1, url = $2, username = $3, password = $4, tags = $5
		WHERE id = $6`,
		p.Name, p.URL, p.Username, password, tagJSON, p.ID,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// Delete removes the proxy by id. A missing row is not an error.
func (r *IndexerProxiesRepo) Delete(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM indexer_proxies WHERE id = $1`, id)
	return err
}
