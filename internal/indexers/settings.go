package indexers

import "encoding/json"

// IndexerSettings are the per-indexer search-behavior toggles (Radarr R5a:
// RSS / Automatic Search / Interactive Search per indexer). They live in the
// indexer's settings_json bag — no schema change — and every read defaults
// a missing bag to all-true, so pre-R5a indexers read with the factory
// behavior unless a toggle was explicitly stored false.
type IndexerSettings struct {
	RSS               bool `json:"rss"`
	AutoSearch        bool `json:"auto_search"`
	InteractiveSearch bool `json:"interactive_search"`
}

// DefaultIndexerSettings is the factory value: every toggle on.
func DefaultIndexerSettings() IndexerSettings {
	return IndexerSettings{RSS: true, AutoSearch: true, InteractiveSearch: true}
}

// ParseIndexerSettings decodes a stored settings_json into IndexerSettings.
// A missing or unparseable bag yields the factory value (the bag is written
// by this codebase, so a corrupt value degrades to defaults rather than an
// error).
func ParseIndexerSettings(raw string) IndexerSettings {
	out := DefaultIndexerSettings()
	if raw == "" {
		return out
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return DefaultIndexerSettings()
	}
	return out
}

// WithIndexerToggles folds the given toggles into a settings_json bag,
// preserving any other keys the bag already carries (adapter-specific
// config written elsewhere).
func WithIndexerToggles(raw string, t IndexerSettings) string {
	var bag map[string]any
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &bag); err != nil {
			bag = map[string]any{}
		}
	} else {
		bag = map[string]any{}
	}
	bag["rss"] = t.RSS
	bag["auto_search"] = t.AutoSearch
	bag["interactive_search"] = t.InteractiveSearch
	out, err := json.Marshal(bag)
	if err != nil {
		return "{}"
	}
	return string(out)
}
