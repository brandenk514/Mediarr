// Package auth implements user credentials and API token management.
//
// Design (single-user v1, multi-user schema present):
//   - Passwords are hashed with Argon2id; the hash is stored, never the
//     plaintext.
//   - API tokens are opaque, revocable, and scoped. They are stored as a
//     salted SHA-256 fingerprint (the raw token is shown once at creation
//     and never persisted).
//   - The auth package is pure — it takes and returns data, it does no I/O.
//     Persistence is handled by the store adapter that owns the tables.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
)

// Errors
var (
	ErrInvalidCredentials = errors.New("auth: invalid username or password")
	ErrUnknownUser        = errors.New("auth: user not found")
	ErrInvalidToken       = errors.New("auth: invalid or revoked token")
	ErrUserExists         = errors.New("auth: user already exists")
	ErrUserNotFound       = errors.New("auth: user not found")
	ErrTokenRevoked       = errors.New("auth: token has been revoked")
)

// Argon2id parameters. Memory 64 MiB, iterations 3, parallelism 4, 32-byte
// tag — matches OWASP's "moderate" baseline and is comfortable on modest
// hardware.
const (
	argon2Memory      = 64 * 1024
	argon2Iterations  = 3
	argon2Parallelism = 4
	argon2KeyLen      = 32
	argon2SaltLen     = 16
)

// User is a persistent user record (credentials excluded).
type User struct {
	ID           int64
	Username     string
	Role         string // "admin" | "user"
	Email        string
	Enabled      bool
	PasswordHash string // argon2-encoded; never serialized to API responses
	LastLoginAt  *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Token is a live API token (the raw secret is not stored).
type Token struct {
	ID          int64
	UserID      int64
	Name        string
	Prefix      string // first 12 chars of the raw token, for identification
	Fingerprint string // sha256(salt || rawToken) hex
	LastUsedAt  *time.Time
	ExpiresAt   *time.Time
	RevokedAt   *time.Time
	CreatedAt   time.Time
}

// HashPassword produces an Argon2id-encoded hash of the plaintext password.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argon2SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: generate salt: %w", err)
	}
	hash := argon2.IDKey([]byte(password), salt, argon2Iterations, argon2Memory, argon2Parallelism, argon2KeyLen)
	// Encode as: $argon2id$v=19$m=65536,t=3,p=4$<salt b64>$<hash b64>
	return encodeArgon2(salt, hash), nil
}

// VerifyPassword checks a plaintext password against a stored encoded hash.
func VerifyPassword(password, encoded string) bool {
	salt, hash, err := decodeArgon2(encoded)
	if err != nil {
		return false
	}
	candidate := argon2.IDKey([]byte(password), salt, argon2Iterations, argon2Memory, argon2Parallelism, argon2KeyLen)
	return subtleEqual(hash, candidate)
}

func subtleEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := range a {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}

// encodeArgon2 / decodeArgon2 implement the PHC string format.
func encodeArgon2(salt, hash []byte) string {
	b64Salt := base64URLNoPad(salt)
	b64Hash := base64URLNoPad(hash)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		argon2Memory, argon2Iterations, argon2Parallelism, b64Salt, b64Hash)
}

func decodeArgon2(encoded string) (salt, hash []byte, err error) {
	parts := strings.Split(encoded, "$")
	// Format: $argon2id$v=19$m=...,t=...,p=...$<salt>$<hash>
	// Splitting on "$" yields 6 parts: ["", "argon2id", "v=19", "params", salt, hash]
	if len(parts) != 6 || parts[1] != "argon2id" {
		return nil, nil, errors.New("auth: malformed argon2 encoding")
	}
	salt, err = base64URLOnPad(parts[4])
	if err != nil {
		return nil, nil, err
	}
	hash, err = base64URLOnPad(parts[5])
	if err != nil {
		return nil, nil, err
	}
	return salt, hash, nil
}

func base64URLNoPad(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

func base64URLOnPad(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}

// TokenPair is returned when a new token is created. Raw is shown once.
type TokenPair struct {
	Raw         string // e.g. "ma_live_abcdef0123456789abcdef0123456789"
	Prefix      string // "ma_live_abcd"
	Salt        []byte
	Fingerprint string
}

// NewToken generates a fresh raw API token and its storage representation.
// The raw value is returned to the caller exactly once.
func NewToken(prefix string) (TokenPair, error) {
	const tokenLen = 32 // 256 bits of entropy
	secret := make([]byte, tokenLen)
	if _, err := rand.Read(secret); err != nil {
		return TokenPair{}, fmt.Errorf("auth: generate token: %w", err)
	}
	raw := prefix + hex.EncodeToString(secret)

	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return TokenPair{}, fmt.Errorf("auth: generate token salt: %w", err)
	}
	fp := fingerprint(salt, raw)
	return TokenPair{
		Raw:         raw,
		Prefix:      prefix + hex.EncodeToString(secret)[:12],
		Salt:        salt,
		Fingerprint: fp,
	}, nil
}

// Fingerprint computes the stored sha256(salt || rawToken) hex digest.
func Fingerprint(salt []byte, raw string) string {
	return fingerprint(salt, raw)
}

func fingerprint(salt []byte, raw string) string {
	h := sha256.New()
	h.Write(salt)
	h.Write([]byte(raw))
	return hex.EncodeToString(h.Sum(nil))
}

// ValidateTokenFormat is a fast-path check that a bearer token could be one
// of ours (right prefix, hex body, sane length) so obviously-garbage tokens
// are rejected without a database lookup. Full verification (fingerprint +
// revocation) always goes through the store.
func ValidateTokenFormat(token string) bool {
	if !strings.HasPrefix(token, "ma_") {
		return false
	}
	body := token[len("ma_"):]
	if len(body) < 8 || len(body)%2 != 0 {
		return false
	}
	for _, c := range body {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}
