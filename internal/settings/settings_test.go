package settings

import "testing"

// TestLibraryKinds is the #44 contract: the four libraries are the fixed,
// ordered set the UI iterates, and IsValid accepts exactly those tokens.
func TestLibraryKinds(t *testing.T) {
	want := []string{"movie", "tv", "music", "book"}
	if len(LibraryKinds) != len(want) {
		t.Fatalf("len(LibraryKinds) = %d, want %d", len(LibraryKinds), len(want))
	}
	for i, k := range LibraryKinds {
		if k.String() != want[i] {
			t.Errorf("LibraryKinds[%d].String() = %q, want %q", i, k.String(), want[i])
		}
		if !k.IsValid() {
			t.Errorf("LibraryKinds[%d] should be valid", i)
		}
	}
	for _, bad := range []string{"", "anime", "MOVIE", "books"} {
		if LibraryKind(bad).IsValid() {
			t.Errorf("LibraryKind(%q) should not be valid", bad)
		}
	}
}

// TestLibraryOverride_IsZero is the #44 contract: an override is zero only when
// every field is empty and the checksum tristate is nil (so an explicit opt-out
// is *not* zero).
func TestLibraryOverride_IsZero(t *testing.T) {
	if !(new(LibraryOverride)).IsZero() {
		t.Error("a nil override should be zero")
	}
	if !(&LibraryOverride{}).IsZero() {
		t.Error("an all-empty override should be zero")
	}
	if (&LibraryOverride{Path: "/x"}).IsZero() {
		t.Error("an override with a path should not be zero")
	}
	if (&LibraryOverride{QualityProfile: "HQ"}).IsZero() {
		t.Error("an override with a profile should not be zero")
	}
	off := false
	if (&LibraryOverride{ChecksumVerify: &off}).IsZero() {
		t.Error("an override with an explicit checksum opt-out should not be zero")
	}
	on := true
	if (&LibraryOverride{ChecksumVerify: &on}).IsZero() {
		t.Error("an override with an explicit checksum opt-in should not be zero")
	}
}

// TestClientKinds is the #44 contract: the download-client kinds are the fixed
// set {mock, qbittorrent, sabnzbd}, mirroring the downloads factory.
func TestClientKinds(t *testing.T) {
	want := []string{"mock", "qbittorrent", "sabnzbd"}
	if len(ClientKinds) != len(want) {
		t.Fatalf("len(ClientKinds) = %d, want %d", len(ClientKinds), len(want))
	}
	for i, k := range ClientKinds {
		if k != want[i] {
			t.Errorf("ClientKinds[%d] = %q, want %q", i, k, want[i])
		}
		if !IsValidClientKind(k) {
			t.Errorf("IsValidClientKind(%q) should be true", k)
		}
	}
	for _, bad := range []string{"", "aria2", "QBITTORRENT", "sabcord"} {
		if IsValidClientKind(bad) {
			t.Errorf("IsValidClientKind(%q) should be false", bad)
		}
	}
}
