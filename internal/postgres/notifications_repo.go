package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/brandenk514/mediarr/internal/notify"
	"github.com/brandenk514/mediarr/internal/secrets"
)

// NotificationsRepo is the Postgres-backed implementation of
// notify.SinkRepo (migration 0023). It is the single layer that owns the
// at-rest encryption of sink tokens: token is vault-encrypted before it
// touches the notifications table and decrypted on the way out, the same
// convention as indexers.api_key_encrypted. Nothing outside this repo ever
// sees the ciphertext.
type NotificationsRepo struct {
	db    *sql.DB
	vault *secrets.Vault
}

var _ notify.SinkRepo = (*NotificationsRepo)(nil)

const notificationColumns = `id, name, kind, enabled, url, section_id, token, user_agent, created_at`

// NewNotificationsRepo constructs the repo over an open pool. vault must be
// the app vault (main.go builds one from the config encryption key).
func NewNotificationsRepo(db *sql.DB, vault *secrets.Vault) *NotificationsRepo {
	return &NotificationsRepo{db: db, vault: vault}
}

// encryptToken maps a plaintext token to its at-rest form: the vault
// ciphertext, or NULL for an empty token.
func (r *NotificationsRepo) encryptToken(plaintext string) (any, error) {
	if plaintext == "" {
		return nil, nil
	}
	ct, err := r.vault.EncryptString(plaintext)
	if err != nil {
		return nil, fmt.Errorf("encrypt token: %w", err)
	}
	return ct, nil
}

// decryptToken maps the at-rest form back to the plaintext token.
func (r *NotificationsRepo) decryptToken(ciphertext *string) (string, error) {
	if ciphertext == nil {
		return "", nil
	}
	pt, err := r.vault.DecryptString(*ciphertext)
	if err != nil {
		return "", fmt.Errorf("decrypt token: %w", err)
	}
	return pt, nil
}

// scanNotificationRow scans one row; token is the raw (still-encrypted or
// NULL) column value the caller decrypts with the vault.
func scanNotificationRow(row interface{ Scan(...any) error }) (*notify.SinkRecord, *string, error) {
	var (
		rec     notify.SinkRecord
		kind    string
		token   *string
		created time.Time
	)
	if err := row.Scan(&rec.ID, &rec.Name, &kind, &rec.Enabled, &rec.URL, &rec.SectionID, &token, &rec.UserAgent, &created); err != nil {
		return nil, nil, err
	}
	rec.Kind = notify.SinkKind(kind)
	rec.CreatedAt = created
	return &rec, token, nil
}

// List returns every saved sink in creation order (id ascending).
func (r *NotificationsRepo) List(ctx context.Context) ([]notify.SinkRecord, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+notificationColumns+` FROM notifications ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []notify.SinkRecord
	for rows.Next() {
		rec, token, err := scanNotificationRow(rows)
		if err != nil {
			return nil, err
		}
		pt, err := r.decryptToken(token)
		if err != nil {
			return nil, err
		}
		rec.Token = pt
		out = append(out, *rec)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// Create stores the sink and returns the DB-assigned id and created_at.
func (r *NotificationsRepo) Create(ctx context.Context, rec notify.SinkRecord) (int64, time.Time, error) {
	token, err := r.encryptToken(rec.Token)
	if err != nil {
		return 0, time.Time{}, err
	}
	err = r.db.QueryRowContext(ctx, `
		INSERT INTO notifications (name, kind, enabled, url, section_id, token, user_agent)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at`,
		rec.Name, string(rec.Kind), rec.Enabled, rec.URL, rec.SectionID, token, rec.UserAgent,
	).Scan(&rec.ID, &rec.CreatedAt)
	if err != nil {
		return 0, time.Time{}, err
	}
	return rec.ID, rec.CreatedAt, nil
}

// Get returns one sink by id, or sql.ErrNoRows for an unknown id.
func (r *NotificationsRepo) Get(ctx context.Context, id int64) (*notify.SinkRecord, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+notificationColumns+` FROM notifications WHERE id = $1`, id)
	rec, token, err := scanNotificationRow(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, sql.ErrNoRows
		}
		return nil, err
	}
	pt, err := r.decryptToken(token)
	if err != nil {
		return nil, err
	}
	rec.Token = pt
	return rec, nil
}

// Update fully replaces the sink's fields (the caller has already merged in
// any secret that must be kept).
func (r *NotificationsRepo) Update(ctx context.Context, rec notify.SinkRecord) error {
	token, err := r.encryptToken(rec.Token)
	if err != nil {
		return err
	}
	res, err := r.db.ExecContext(ctx, `
		UPDATE notifications
		SET name = $1, kind = $2, enabled = $3, url = $4, section_id = $5, token = $6, user_agent = $7
		WHERE id = $8`,
		rec.Name, string(rec.Kind), rec.Enabled, rec.URL, rec.SectionID, token, rec.UserAgent, rec.ID,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// Delete removes the sink by id. A missing row is not an error.
func (r *NotificationsRepo) Delete(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM notifications WHERE id = $1`, id)
	return err
}
