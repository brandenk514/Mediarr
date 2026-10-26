package settings

import (
	"strings"
	"testing"
)

// R5a: the radarr import-list kind validates host+port (no URL) while the
// trakt/sonarr URL rules are unchanged.
func TestR5a_ImportListRadarrValidation(t *testing.T) {
	valid := func(host string, port int) ImportListsSettings {
		return ImportListsSettings{Lists: []ImportList{{
			ID: 1, Name: "My Radarr", Kind: "radarr", Host: host, Port: port, Enabled: true,
		}}}
	}

	if err := valid("radarr.example", 7878).Validate(); err != "" {
		t.Errorf("valid radarr list must validate: %q", err)
	}

	if err := valid("", 7878).Validate(); !strings.Contains(err, "host is required") {
		t.Errorf("missing host: got %q", err)
	}
	if err := valid("radarr.example", 0).Validate(); !strings.Contains(err, "port") {
		t.Errorf("port 0: got %q", err)
	}
	if err := valid("radarr.example", 70000).Validate(); !strings.Contains(err, "port") {
		t.Errorf("port 70000: got %q", err)
	}
}

// R5a: unknown kinds are still rejected, and trakt/sonarr keep their URL
// rules (a radarr-shaped entry with a URL but no host is still invalid).
func TestR5a_ImportListOtherKindsUnchanged(t *testing.T) {
	if err := (ImportListsSettings{Lists: []ImportList{{
		ID: 1, Name: "x", Kind: "nzb", URL: "https://x.example",
	}}}).Validate(); !strings.Contains(err, "unknown kind") {
		t.Errorf("unknown kind: got %q", err)
	}
	if err := (ImportListsSettings{Lists: []ImportList{{
		ID: 1, Name: "x", Kind: "sonarr", URL: "not-a-url",
	}}}).Validate(); !strings.Contains(err, "url must be an http/https URL") {
		t.Errorf("sonarr bad url: got %q", err)
	}
}
