package music

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestHashFile_Deterministic confirms HashFile returns a stable 64-hex sha256
// for a given file and a different hash for different contents.
func TestHashFile_Deterministic(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.flac")
	os.WriteFile(p, []byte("hello music"), 0o644)

	h1, err := HashFile(p)
	if err != nil {
		t.Fatalf("HashFile: %v", err)
	}
	if len(h1) != 64 || !isHex(h1) {
		t.Errorf("hash = %q, want 64 hex chars", h1)
	}

	h2, err := HashFile(p)
	if err != nil {
		t.Fatalf("HashFile (2nd): %v", err)
	}
	if h1 != h2 {
		t.Errorf("hash not deterministic: %q vs %q", h1, h2)
	}

	// Different content → different hash.
	p2 := filepath.Join(dir, "b.flac")
	os.WriteFile(p2, []byte("different bytes"), 0o644)
	h3, err := HashFile(p2)
	if err != nil {
		t.Fatalf("HashFile (2): %v", err)
	}
	if h1 == h3 {
		t.Error("different content should hash differently")
	}
}

// TestHashFile_Missing confirms a missing file is a wrapped error.
func TestHashFile_Missing(t *testing.T) {
	if _, err := HashFile(filepath.Join(t.TempDir(), "nope.flac")); err == nil {
		t.Fatal("expected an error for a missing file")
	}
}

// TestVerifyChecksum_Match confirms a matching checksum returns nil.
func TestVerifyChecksum_Match(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.flac")
	os.WriteFile(p, []byte("the file"), 0o644)
	sum, _ := HashFile(p)

	if err := VerifyChecksum(p, sum); err != nil {
		t.Errorf("matching checksum: %v", err)
	}
	// Case-insensitive comparison.
	if err := VerifyChecksum(p, strings.ToUpper(sum)); err != nil {
		t.Errorf("upper-case matching checksum: %v", err)
	}
}

// TestVerifyChecksum_Mismatch confirms a wrong checksum returns
// ErrChecksumMismatch.
func TestVerifyChecksum_Mismatch(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.flac")
	os.WriteFile(p, []byte("the file"), 0o644)
	if err := VerifyChecksum(p, strings.Repeat("0", 64)); !errors.Is(err, ErrChecksumMismatch) {
		t.Errorf("expected ErrChecksumMismatch, got %v", err)
	}
}

// TestVerifyChecksum_EmptyExpected confirms an empty expected checksum is a
// no-op (the call site decides whether to verify, per the per-library opt-out).
func TestVerifyChecksum_EmptyExpected(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.flac")
	os.WriteFile(p, []byte("any bytes"), 0o644)
	if err := VerifyChecksum(p, ""); err != nil {
		t.Errorf("empty expected checksum should be a no-op, got %v", err)
	}
}

func isHex(s string) bool {
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
