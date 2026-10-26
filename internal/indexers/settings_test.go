package indexers

import (
	"encoding/json"
	"testing"
)

// R5a: the per-indexer toggles live in the settings_json bag. A missing or
// empty bag (every pre-R5a row) must read as the factory defaults (all
// true) instead of all-false — and the same for a corrupted bag.
func TestR5a_IndexerSettings_Parse(t *testing.T) {
	cases := []struct {
		raw string
		t   IndexerSettings
	}{
		{"", DefaultIndexerSettings()},
		{`{}`, DefaultIndexerSettings()},
		{`{"rss":false}`, IndexerSettings{RSS: false, AutoSearch: true, InteractiveSearch: true}},
		{`{"auto_search":false,"interactive_search":false}`, IndexerSettings{RSS: true, AutoSearch: false, InteractiveSearch: false}},
		{`{"category":50,"rss":false}`, IndexerSettings{RSS: false, AutoSearch: true, InteractiveSearch: true}},
		{`{corrupt`, DefaultIndexerSettings()},
	}
	for _, c := range cases {
		if got := ParseIndexerSettings(c.raw); got != c.t {
			t.Errorf("ParseIndexerSettings(%q) = %+v, want %+v", c.raw, got, c.t)
		}
	}
}

// R5a: WithIndexerToggles must write all three keys and preserve any other
// bag keys (an indexer's provider-specific settings survive a toggle save).
func TestR5a_IndexerSettings_With(t *testing.T) {
	out := WithIndexerToggles(`{"category":50}`, IndexerSettings{RSS: false, AutoSearch: true, InteractiveSearch: false})
	var bag map[string]any
	if err := json.Unmarshal([]byte(out), &bag); err != nil {
		t.Fatalf("output is not a JSON object: %v: %s", err, out)
	}
	if bag["category"] != 50.0 {
		t.Errorf("existing key must be preserved: %v", bag)
	}
	if bag["rss"] != false || bag["auto_search"] != true || bag["interactive_search"] != false {
		t.Errorf("toggles not written: %v", bag)
	}

	out = WithIndexerToggles("", DefaultIndexerSettings())
	var all map[string]any
	if err := json.Unmarshal([]byte(out), &all); err != nil {
		t.Fatalf("empty-bag output is not JSON: %v: %s", err, out)
	}
	if all["rss"] != true || all["auto_search"] != true || all["interactive_search"] != true {
		t.Errorf("factory defaults not written: %v", all)
	}
}
