package auth

import (
	"strings"
	"testing"
)

func TestHashVerifyPassword(t *testing.T) {
	pw := "correct-horse-battery-staple"
	enc, err := HashPassword(pw)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !strings.HasPrefix(enc, "$argon2id$") {
		t.Errorf("expected argon2id encoding, got %q", enc)
	}
	if !VerifyPassword(pw, enc) {
		t.Error("VerifyPassword should accept correct password")
	}
	if VerifyPassword("wrong-password", enc) {
		t.Error("VerifyPassword should reject wrong password")
	}
}

func TestHashPassword_IsRandomised(t *testing.T) {
	// Same password, two calls → different salts → different encodings.
	a, err := HashPassword("same")
	if err != nil {
		t.Fatal(err)
	}
	b, err := HashPassword("same")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Error("two hashes of the same password should differ (random salt)")
	}
	if !VerifyPassword("same", a) || !VerifyPassword("same", b) {
		t.Error("both hashes should verify")
	}
}

func TestVerifyPassword_Malformed(t *testing.T) {
	if VerifyPassword("x", "not-an-argon2-hash") {
		t.Error("malformed hash must not verify")
	}
}

func TestNewToken_Format(t *testing.T) {
	tp, err := NewToken("ma_")
	if err != nil {
		t.Fatalf("NewToken: %v", err)
	}
	if !strings.HasPrefix(tp.Raw, "ma_") {
		t.Errorf("raw should start with ma_, got %q", tp.Raw)
	}
	if len(tp.Raw) < 20 {
		t.Errorf("raw too short: %q", tp.Raw)
	}
	if !strings.HasPrefix(tp.Prefix, "ma_") {
		t.Errorf("prefix should start with ma_, got %q", tp.Prefix)
	}
	if len(tp.Fingerprint) != 64 {
		t.Errorf("fingerprint should be 64 hex chars, got %d", len(tp.Fingerprint))
	}
	if !ValidateTokenFormat(tp.Raw) {
		t.Error("ValidateTokenFormat should accept a freshly generated token")
	}
}

func TestNewToken_Uniqueness(t *testing.T) {
	a, err := NewToken("ma_")
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewToken("ma_")
	if err != nil {
		t.Fatal(err)
	}
	if a.Raw == b.Raw {
		t.Error("two generated tokens must differ")
	}
	if a.Fingerprint == b.Fingerprint {
		t.Error("two fingerprints must differ (different salts/secrets)")
	}
}

func TestFingerprint_Deterministic(t *testing.T) {
	salt := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	a := Fingerprint(salt, "ma_secret")
	b := Fingerprint(salt, "ma_secret")
	if a != b {
		t.Error("fingerprint must be deterministic for the same salt+raw")
	}
	c := Fingerprint(salt, "ma_other")
	if a == c {
		t.Error("fingerprint must differ for a different raw token")
	}
}

func TestValidateTokenFormat(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"ma_abcdef1234567890", true},
		{"ma_", false},
		{"xx_abcdef1234567890", false},
		{"", false},
	}
	for _, c := range cases {
		if got := ValidateTokenFormat(c.in); got != c.want {
			t.Errorf("ValidateTokenFormat(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
