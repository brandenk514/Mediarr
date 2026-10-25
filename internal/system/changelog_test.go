package system

import (
	"testing"
)

func TestChangelogHighlightsCurrentVersion(t *testing.T) {
	svc := NewChangelogService("0.6.0")
	upd := svc.Get()
	if upd.Current != "0.6.0" {
		t.Fatalf("current = %q, want 0.6.0", upd.Current)
	}
	if len(upd.Changelog) == 0 {
		t.Fatal("expected a non-empty changelog")
	}
	// Newest release first, and the current version is present.
	if upd.Changelog[0].Version != "0.6.0" {
		t.Errorf("first entry = %q, want 0.6.0", upd.Changelog[0].Version)
	}
	found := false
	for _, e := range upd.Changelog {
		if e.Version == "0.6.0" && len(e.Entries) > 0 {
			found = true
		}
	}
	if !found {
		t.Errorf("current version 0.6.0 not present with entries")
	}
	// Dates are in the YYYY-MM-DD shape.
	if upd.Changelog[0].Date != "2026-10-25" {
		t.Errorf("first date = %q, want 2026-10-25", upd.Changelog[0].Date)
	}
}

func TestChangelogEmptyVersionStillServes(t *testing.T) {
	svc := NewChangelogService("")
	upd := svc.Get()
	if upd.Current != "" {
		t.Errorf("current = %q, want empty", upd.Current)
	}
	if len(upd.Changelog) == 0 {
		t.Error("expected the default changelog even with an empty version")
	}
}
