package secrets

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func newTestVault(t *testing.T) *Vault {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("rand: %v", err)
	}
	v, err := NewVaultFromBytes(key)
	if err != nil {
		t.Fatalf("NewVaultFromBytes: %v", err)
	}
	return v
}

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	v := newTestVault(t)
	secret := "sk-super-secret-indexer-key-12345"

	ct, err := v.EncryptString(secret)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if ct == secret || ct == "" {
		t.Fatalf("ciphertext must be opaque, got %q", ct)
	}
	pt, err := v.DecryptString(ct)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if pt != secret {
		t.Fatalf("round-trip = %q, want %q", pt, secret)
	}
}

func TestEncrypt_NonceUniqueness(t *testing.T) {
	v := newTestVault(t)
	a, err := v.Encrypt([]byte("same"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := v.Encrypt([]byte("same"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a, b) {
		t.Fatal("two encryptions of the same plaintext must differ (fresh nonce)")
	}
}

func TestDecrypt_TamperDetected(t *testing.T) {
	v := newTestVault(t)
	ct, err := v.Encrypt([]byte("payload"))
	if err != nil {
		t.Fatal(err)
	}
	ct[len(ct)-1] ^= 0x01 // flip a tag bit
	if _, err := v.Decrypt(ct); err == nil {
		t.Fatal("tampered ciphertext must fail to decrypt")
	}
}

func TestDecrypt_WrongKey(t *testing.T) {
	v1 := newTestVault(t)
	v2 := newTestVault(t)
	ct, err := v1.Encrypt([]byte("top secret"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v2.Decrypt(ct); err == nil {
		t.Fatal("decryption with the wrong key must fail")
	}
}

func TestDecrypt_TooShort(t *testing.T) {
	v := newTestVault(t)
	if _, err := v.Decrypt([]byte("short")); err == nil {
		t.Fatal("short ciphertext must be rejected")
	}
}

func TestDecrypt_NonHexInput(t *testing.T) {
	v := newTestVault(t)
	if _, err := v.DecryptString("not-hex!!"); err == nil {
		t.Fatal("non-hex ciphertext must be rejected")
	}
}

func TestNewVault_InvalidKey(t *testing.T) {
	if _, err := NewVault("tooshort"); err == nil {
		t.Fatal("short hex key must be rejected")
	}
	if _, err := NewVault("zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"); err == nil {
		t.Fatal("non-hex key must be rejected")
	}
	if _, err := NewVaultFromBytes([]byte("tiny")); err == nil {
		t.Fatal("short raw key must be rejected")
	}
}
