package imageproxy

import "testing"

// TestParseEntryName_ClampsCorruptMaxAge pins the CodeQL fix from the
// run-5 #149 CI triage: an entry filename (shared disk, not fully
// trusted) can carry an out-of-range max-age; parseEntryName must clamp
// it so the later int64 -> int conversion cannot truncate.
func TestParseEntryName_ClampsCorruptMaxAge(t *testing.T) {
	maxAge, expireAtMs, ext := parseEntryName("9223372036854775807.1234567890.jpg.png")
	if maxAge != int(maxCacheMaxAgeSeconds) {
		t.Fatalf("corrupt maxAge = %d, want the clamp %d", maxAge, maxCacheMaxAgeSeconds)
	}
	if expireAtMs != 1234567890 {
		t.Fatalf("expireAtMs = %d, want 1234567890", expireAtMs)
	}
	if ext != "png" {
		t.Fatalf("ext = %q, want png", ext)
	}
}
