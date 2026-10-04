package music

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
)

// checksum.go carries the music-specific checksum verification (PLAN §15.5:
// "Music files are verified by checksum (sha256), on by default, opt-out per
// library"). This is the one I/O touchpoint in the pure domain: reading the
// file bytes to hash them. It is self-contained so the import pipeline can call
// it without dragging in a database or http import.

// HashFile returns the hex-encoded sha256 of the file at path.
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("music: checksum: open %s: %w", path, err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("music: checksum: read %s: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// VerifyChecksum compares the file at path against an expected sha256 (hex).
// It returns nil when they match, ErrChecksumMismatch when they differ, and a
// wrapped error when the file could not be read. An empty expectedChecksum
// means "no checksum to check" and always returns nil (the call site decides
// whether to verify at all, per the per-library opt-out).
func VerifyChecksum(path, expectedChecksum string) error {
	if strings.TrimSpace(expectedChecksum) == "" {
		return nil
	}
	got, err := HashFile(path)
	if err != nil {
		return err
	}
	if !strings.EqualFold(got, expectedChecksum) {
		return fmt.Errorf("%w: got %s, want %s", ErrChecksumMismatch, got, expectedChecksum)
	}
	return nil
}
