package auth

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// Repo persists users and API tokens. It wraps a *sql.DB so it can be
// tested against real Postgres (testcontainers) or a fake.
type Repo struct {
	db *sql.DB
}

// NewRepo builds an auth repository over the given pool.
func NewRepo(db *sql.DB) *Repo { return &Repo{db: db} }

// UserRow is a database user record (hash included, never the plaintext).
type UserRow struct {
	ID           int64
	Username     string
	Role         string
	Email        string
	Enabled      bool
	PasswordHash string
	LastLoginAt  *time.Time
	CreatedAt    time.Time
}

// CountUsers returns the number of user rows.
func (r *Repo) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("auth: count users: %w", err)
	}
	return n, nil
}

// CreateUser inserts a new user. Returns the new row ID.
func (r *Repo) CreateUser(ctx context.Context, username, email, role, passwordHash string) (int64, error) {
	if role == "" {
		role = "admin"
	}
	var id int64
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO users (username, email, role, password_hash)
		VALUES ($1, $2, $3, $4)
		RETURNING id`,
		username, email, role, passwordHash,
	).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			return 0, ErrUserExists
		}
		return 0, fmt.Errorf("auth: create user: %w", err)
	}
	return id, nil
}

// GetUserByUsername loads a user by username.
func (r *Repo) GetUserByUsername(ctx context.Context, username string) (*UserRow, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, username, role, email, enabled, password_hash, last_login_at, created_at
		FROM users WHERE username = $1`, username)
	u := &UserRow{}
	err := row.Scan(&u.ID, &u.Username, &u.Role, &u.Email, &u.Enabled,
		&u.PasswordHash, &u.LastLoginAt, &u.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("auth: get user: %w", err)
	}
	return u, nil
}

// TouchLastLogin updates the last_login_at timestamp.
func (r *Repo) TouchLastLogin(ctx context.Context, userID int64) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE users SET last_login_at = now(), updated_at = now() WHERE id = $1`,
		userID)
	return err
}

// CreateToken inserts a new API token row. The raw token is NOT stored.
func (r *Repo) CreateToken(ctx context.Context, userID int64, name string, tp TokenPair, expiresAt *time.Time) (int64, error) {
	saltHex := hex.EncodeToString(tp.Salt)
	var id int64
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO api_tokens (user_id, name, prefix, fingerprint, salt, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id`,
		userID, name, tp.Prefix, tp.Fingerprint, saltHex, expiresAt,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("auth: create token: %w", err)
	}
	return id, nil
}

// TokenUser resolves a raw bearer token to its owning user. Returns
// ErrInvalidToken when unknown, revoked, or expired.
func (r *Repo) TokenUser(ctx context.Context, rawToken string) (*UserRow, int64, error) {
	if !ValidateTokenFormat(rawToken) {
		return nil, 0, ErrInvalidToken
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT t.id, t.salt, t.fingerprint, t.revoked_at, t.expires_at,
		       u.id, u.username, u.role, u.email, u.enabled, u.password_hash, u.last_login_at, u.created_at
		FROM api_tokens t
		JOIN users u ON u.id = t.user_id
		WHERE t.prefix = $1
		ORDER BY t.id DESC`,
		tokenPrefix(rawToken),
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var (
		tokenID int64
		u       UserRow
	)
	var found bool
	for rows.Next() {
		var (
			saltHex   string
			fp        string
			revokedAt *time.Time
			expiresAt *time.Time
		)
		if err := rows.Scan(&tokenID, &saltHex, &fp, &revokedAt, &expiresAt,
			&u.ID, &u.Username, &u.Role, &u.Email, &u.Enabled,
			&u.PasswordHash, &u.LastLoginAt, &u.CreatedAt); err != nil {
			return nil, 0, err
		}
		salt, err := hex.DecodeString(saltHex)
		if err != nil {
			continue
		}
		if Fingerprint(salt, rawToken) != fp {
			continue
		}
		found = true
		break
	}
	if !found {
		return nil, 0, ErrInvalidToken
	}
	// Re-read status columns for the matched row (they were scanned above).
	// (Revoked/expiry were checked below via the scan values — re-derive.)
	if err := r.checkTokenState(ctx, tokenID); err != nil {
		return nil, 0, err
	}
	if !u.Enabled {
		return nil, 0, ErrInvalidToken
	}

	// Best-effort last-used update; ignore errors to keep the auth path fast.
	_, _ = r.db.ExecContext(ctx,
		`UPDATE api_tokens SET last_used_at = now() WHERE id = $1`, tokenID)

	return &u, tokenID, nil
}

func (r *Repo) checkTokenState(ctx context.Context, tokenID int64) error {
	var revokedAt *time.Time
	var expiresAt *time.Time
	err := r.db.QueryRowContext(ctx, `
		SELECT revoked_at, expires_at FROM api_tokens WHERE id = $1`,
		tokenID,
	).Scan(&revokedAt, &expiresAt)
	if err != nil {
		return ErrInvalidToken
	}
	if revokedAt != nil {
		return ErrTokenRevoked
	}
	if expiresAt != nil && time.Now().After(*expiresAt) {
		return ErrInvalidToken
	}
	return nil
}

// tokenPrefix returns the stored prefix column value for a raw token:
// the "ma_" tag plus the first 12 hex chars of the secret.
func tokenPrefix(raw string) string {
	const tag = "ma_"
	if len(raw) < len(tag)+12 {
		return raw
	}
	return raw[:len(tag)+12]
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, sub := range []string{"duplicate key", "unique constraint"} {
		if containsSub(msg, sub) {
			return true
		}
	}
	return false
}

func containsSub(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// UserStore is the persistence surface the API needs for auth. Repo
// implements it over Postgres; tests can use a fake.
type UserStore interface {
	CountUsers(ctx context.Context) (int, error)
	CreateUser(ctx context.Context, username, email, role, passwordHash string) (int64, error)
	GetUserByUsername(ctx context.Context, username string) (*UserRow, error)
	TouchLastLogin(ctx context.Context, userID int64) error
	CreateToken(ctx context.Context, userID int64, name string, tp TokenPair, expiresAt *time.Time) (int64, error)
	TokenUser(ctx context.Context, rawToken string) (*UserRow, int64, error)
}

var _ UserStore = (*Repo)(nil)

var _ = errors.New
