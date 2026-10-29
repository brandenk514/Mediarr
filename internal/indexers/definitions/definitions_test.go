package definitions

import (
	"testing"
)

func TestLoad_EmbeddedSeed(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(c.Definitions) < 500 {
		t.Fatalf("expected at least 500 definitions in seed, got %d", len(c.Definitions))
	}
	if c.Revision == "" {
		t.Fatal("seed revision is empty")
	}

	// Well-known entries must be present.
	for _, id := range []string{"1337x", "generic-newznab", "generic-torznab"} {
		if c.GetByID(id) == nil {
			t.Errorf("seed missing required definition %q", id)
		}
	}

	// Protocol distribution must be sane: the community set is all torrent,
	// plus the hand-written built-ins contribute usenet.
	var torrents, usenet int
	for i := range c.Definitions {
		switch c.Definitions[i].Protocol {
		case "torrent":
			torrents++
		case "usenet":
			usenet++
		default:
			t.Errorf("definition %q has unexpected protocol %q", c.Definitions[i].ID, c.Definitions[i].Protocol)
		}
	}
	if torrents < 500 {
		t.Errorf("expected at least 500 torrent definitions, got %d", torrents)
	}
	if usenet < 2 {
		t.Errorf("expected at least 2 built-in usenet definitions, got %d", usenet)
	}

	// Privacy values must be from the known set.
	for i := range c.Definitions {
		p := c.Definitions[i].Privacy
		if p != "public" && p != "private" && p != "semi-private" {
			t.Errorf("definition %q has unexpected privacy %q", c.Definitions[i].ID, p)
		}
	}
}

func TestLoad_BuiltinFields(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	d := c.GetByID("generic-newznab")
	if d == nil {
		t.Fatal("generic-newznab missing")
	}
	if d.Protocol != "usenet" {
		t.Errorf("generic-newznab protocol = %q, want usenet", d.Protocol)
	}
	baseurl := d.Field("baseurl")
	if baseurl == nil || !baseurl.Required || baseurl.Type != FieldTypeTextbox {
		t.Errorf("generic-newznab baseurl field wrong: %+v", baseurl)
	}
	apikey := d.Field("apikey")
	if apikey == nil || apikey.Type != FieldTypePassword {
		t.Errorf("generic-newznab apikey field wrong: %+v", apikey)
	}
}

func TestResolveDefinition(t *testing.T) {
	c := Default()
	if c.ResolveDefinition("1337x") == nil {
		t.Error("ResolveDefinition by id failed")
	}
	if c.ResolveDefinition("GENERIC-NEWZNAB") != nil {
		t.Error("case-insensitive match should not apply to ids")
	}
	if c.ResolveDefinition("Generic Newznab") == nil {
		t.Error("ResolveDefinition by display name failed")
	}
	if c.ResolveDefinition("generic newznab") == nil {
		t.Error("ResolveDefinition by lower-cased display name failed")
	}
	if c.ResolveDefinition("") != nil {
		t.Error("ResolveDefinition(\"\") should be nil")
	}
	if c.ResolveDefinition("no-such-definition") != nil {
		t.Error("ResolveDefinition unknown should be nil")
	}
}

func TestParseSeed_RejectsBadSeeds(t *testing.T) {
	tests := []struct {
		name string
		json string
	}{
		{"not json", `{`},
		{"empty list", `{"revision":"x","definitions":[]}`},
		{"missing id", `{"revision":"x","definitions":[{"name":"a"}]}`},
		{"duplicate id", `{"revision":"x","definitions":[{"id":"a"},{"id":"a"}]}`},
	}
	for _, tc := range tests {
		if _, err := ParseSeed([]byte(tc.json)); err == nil {
			t.Errorf("%s: expected error", tc.name)
		}
	}
}
