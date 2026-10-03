// Package secrets provides authenticated encryption for values that must be
// stored at rest (indexer API keys, download-client credentials).
//
// Algorithm: AES-256-GCM. The key is a 32-byte value supplied via
// MEDIARR_ENCRYPTION_KEY (64 hex chars, validated by the config package).
// The key is never persisted, never logged, and never leaves this package.
//
// Ciphertext layout: [12-byte nonce][ciphertext][16-byte GCM tag] — all
// concatenated, so Encrypt/Decrypt are self-contained round-trips.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
)

const nonceSize = 12 // GCM standard nonce size

// ErrCiphertextTooShort is returned when the input is not a valid sealed
// value (shorter than nonce+tag).
var ErrCiphertextTooShort = errors.New("secrets: ciphertext too short")

// Vault encrypts and decrypts secret values with a fixed 256-bit key.
type Vault struct {
	aead cipher.AEAD
}

// NewVault builds a Vault from a 64-char hex key (32 bytes).
func NewVault(hexKey string) (*Vault, error) {
	if len(hexKey) != 64 {
		return nil, fmt.Errorf("secrets: key must be 64 hex chars (32 bytes), got %d", len(hexKey))
	}
	key, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, fmt.Errorf("secrets: decode key: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secrets: new cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secrets: new gcm: %w", err)
	}
	return &Vault{aead: aead}, nil
}

// NewVaultFromBytes builds a Vault from raw key bytes (test convenience).
func NewVaultFromBytes(key []byte) (*Vault, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("secrets: key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Vault{aead: aead}, nil
}

// Encrypt seals a plaintext value. The result is safe to store (e.g. in
// Postgres) and opaque — it embeds a fresh random nonce per call.
func (v *Vault) Encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, nonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("secrets: nonce: %w", err)
	}
	return v.aead.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt opens a sealed value. Returns an error on tamper or bad key.
func (v *Vault) Decrypt(ciphertext []byte) ([]byte, error) {
	if len(ciphertext) < v.aead.NonceSize()+v.aead.Overhead() {
		return nil, ErrCiphertextTooShort
	}
	nonce, ct := ciphertext[:nonceSize], ciphertext[nonceSize:]
	return v.aead.Open(nil, nonce, ct, nil)
}

// EncryptString / DecryptString are convenience wrappers for string secrets
// (API keys) — the common case.
func (v *Vault) EncryptString(s string) (string, error) {
	ct, err := v.Encrypt([]byte(s))
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(ct), nil
}

func (v *Vault) DecryptString(hexCT string) (string, error) {
	ct, err := hex.DecodeString(hexCT)
	if err != nil {
		return "", fmt.Errorf("secrets: ciphertext is not hex: %w", err)
	}
	pt, err := v.Decrypt(ct)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}
