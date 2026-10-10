package downloads

import (
	"testing"
)

// TestNewClient_RequiresDownloadsDir is the boot contract: every client kind
// needs a downloads directory (the mock's dir, or the real client's default
// save/complete dir). Missing = error, not a silent no-op.
func TestNewClient_RequiresDownloadsDir(t *testing.T) {
	for _, kind := range []ClientKind{"", KindMock, KindQB, KindSAB} {
		if _, err := NewClient(ClientOptions{Kind: kind}); err == nil {
			t.Errorf("kind %q: expected error for empty downloads dir", kind)
		}
	}
}

// TestNewClient_SelectsKind verifies the config-driven swap (#36/#37): the
// right concrete type is built for each kind, and defaults flow through
// (QB save path / SAB complete dir default to the downloads dir).
func TestNewClient_SelectsKind(t *testing.T) {
	dir := t.TempDir()
	if c, err := NewClient(ClientOptions{Kind: "", MockDir: dir}); err != nil {
		t.Fatalf("mock: %v", err)
	} else if _, ok := c.(*MockClient); !ok {
		t.Errorf("kind '' -> %T, want *MockClient", c)
	}
	if c, err := NewClient(ClientOptions{Kind: KindMock, MockDir: dir}); err != nil {
		t.Fatalf("mock: %v", err)
	} else if _, ok := c.(*MockClient); !ok {
		t.Errorf("kind mock -> %T, want *MockClient", c)
	}
	qb, err := NewClient(ClientOptions{Kind: KindQB, MockDir: dir, QB: QBittorrentConfig{Base: "http://q", APIKey: "k"}})
	if err != nil {
		t.Fatalf("qb: %v", err)
	}
	if c, ok := qb.(*QBittorrentClient); !ok {
		t.Errorf("kind qbittorrent -> %T, want *QBittorrentClient", qb)
	} else if c.savePath != dir {
		t.Errorf("qb default savePath = %q, want %q (downloads dir)", c.savePath, dir)
	}
	sab, err := NewClient(ClientOptions{Kind: KindSAB, MockDir: dir, SAB: SABnzbdConfig{Base: "http://s", APIKey: "k"}})
	if err != nil {
		t.Fatalf("sab: %v", err)
	}
	if c, ok := sab.(*SABnzbdClient); !ok {
		t.Errorf("kind sabnzbd -> %T, want *SABnzbdClient", sab)
	} else if c.completeDir != dir {
		t.Errorf("sab default completeDir = %q, want %q (downloads dir)", c.completeDir, dir)
	}
}

// TestNewClient_UnknownKindFailsFast is the misconfiguration guard: an unknown
// MEDIARR_DOWNLOAD_CLIENT must error, never fall back to a mock (which would
// "download" into the wrong place).
func TestNewClient_UnknownKindFailsFast(t *testing.T) {
	_, err := NewClient(ClientOptions{Kind: "deluge", MockDir: t.TempDir()})
	if err == nil {
		t.Fatal("unknown kind must fail, not fall back to mock")
	}
}

// TestNewClient_ForwardsRealConfig ensures the real-client settings the factory
// is given are what the client gets (not clobbered by the defaults).
func TestNewClient_ForwardsRealConfig(t *testing.T) {
	qb, err := NewClient(ClientOptions{
		Kind:    KindQB,
		MockDir: "ignored",
		QB:      QBittorrentConfig{Base: "http://q:8080", APIKey: "k3y", SavePath: "/custom"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if c := qb.(*QBittorrentClient); c.base != "http://q:8080" || c.savePath != "/custom" {
		t.Errorf("qb config not forwarded: base=%q savePath=%q", c.base, c.savePath)
	}
	sab, err := NewClient(ClientOptions{
		Kind:    KindSAB,
		MockDir: "ignored",
		SAB:     SABnzbdConfig{Base: "http://s:8080", APIKey: "sabk", CompleteDir: "/custom"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if c := sab.(*SABnzbdClient); c.base != "http://s:8080" || c.completeDir != "/custom" {
		t.Errorf("sab config not forwarded: base=%q completeDir=%q", c.base, c.completeDir)
	}
}
