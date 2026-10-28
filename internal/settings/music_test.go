package settings

import (
	"testing"
)

// TestL3_MusicLevelDefaults covers the seeded Lidarr music quality ladder:
// 38 rows in their own 200+ id space, best-first, with exact ids, names,
// groups, and size limits (0 = unlimited for the lossless rows + WAV).
func TestL3_MusicLevelDefaults(t *testing.T) {
	levels := DefaultMusicLevels()
	if len(levels) != 38 {
		t.Fatalf("want 38 music levels, got %d", len(levels))
	}
	// (id, name, group, sizeLimitMB) in the exact seeded (best-first) order.
	want := []struct {
		id      int
		name    string
		group   string
		limitMB int
	}{
		{213, "WAV", "", 0},
		{235, "APE", "Lossless", 0},
		{236, "WavPack", "Lossless", 0},
		{221, "FLAC 24bit", "Lossless", 0},
		{237, "ALAC 24bit", "Lossless", 0},
		{206, "FLAC", "Lossless", 0},
		{207, "ALAC", "Lossless", 0},
		{214, "OGG Vorbis Q10", "High Quality Lossy", 550},
		{211, "AAC-320", "High Quality Lossy", 350},
		{212, "AAC-VBR", "High Quality Lossy", 350},
		{204, "MP3-320", "High Quality Lossy", 350},
		{202, "MP3-VBR-V0", "High Quality Lossy", 350},
		{215, "OGG Vorbis Q9", "High Quality Lossy", 350},
		{210, "AAC-256", "Mid Quality Lossy", 280},
		{203, "MP3-256", "Mid Quality Lossy", 280},
		{208, "MP3-VBR-V2", "Mid Quality Lossy", 280},
		{216, "OGG Vorbis Q8", "Mid Quality Lossy", 280},
		{217, "OGG Vorbis Q7", "Mid Quality Lossy", 245},
		{220, "WMA", "Low Quality Lossy", 350},
		{234, "MP3-224", "Low Quality Lossy", 245},
		{209, "AAC-192", "Low Quality Lossy", 210},
		{201, "MP3-192", "Low Quality Lossy", 210},
		{218, "OGG Vorbis Q6", "Low Quality Lossy", 210},
		{205, "MP3-160", "Poor Quality Lossy", 175},
		{219, "OGG Vorbis Q5", "Poor Quality Lossy", 175},
		{222, "MP3-128", "Poor Quality Lossy", 140},
		{233, "MP3-112", "Poor Quality Lossy", 125},
		{223, "MP3-96", "Poor Quality Lossy", 110},
		{224, "MP3-80", "Trash Quality Lossy", 95},
		{225, "MP3-64", "Trash Quality Lossy", 75},
		{226, "MP3-56", "Trash Quality Lossy", 65},
		{227, "MP3-48", "Trash Quality Lossy", 55},
		{228, "MP3-40", "Trash Quality Lossy", 45},
		{229, "MP3-32", "Trash Quality Lossy", 40},
		{230, "MP3-24", "Trash Quality Lossy", 30},
		{231, "MP3-16", "Trash Quality Lossy", 20},
		{232, "MP3-8", "Trash Quality Lossy", 10},
		{200, "Unknown", "", 350},
	}
	for i, l := range levels {
		w := want[i]
		if l.ID != w.id || l.Name != w.name || l.Group != w.group || l.SizeLimitMB != w.limitMB {
			t.Errorf("music level %d = (id=%d name=%q group=%q limit=%d), want (id=%d name=%q group=%q limit=%d)",
				i, l.ID, l.Name, l.Group, l.SizeLimitMB, w.id, w.name, w.group, w.limitMB)
		}
		if l.Source != "" || l.Resolution != "" {
			t.Errorf("music level %q: source/resolution must be empty (no audio equivalent): %+v", l.Name, l)
		}
	}
}

// TestL3_MusicLevelIDSpace covers the id-space contract: every music level id
// is 200+, unique, and equal to 200 + the Lidarr quality id (Lidarr
// Unknown(0) -> 200 ... ALAC 24bit(37) -> 237).
func TestL3_MusicLevelIDSpace(t *testing.T) {
	seen := map[int]bool{}
	for _, l := range DefaultMusicLevels() {
		if l.ID < 200 || l.ID > 237 {
			t.Errorf("music level %q: id %d outside the 200+ space", l.Name, l.ID)
		}
		if seen[l.ID] {
			t.Errorf("duplicate music level id %d", l.ID)
		}
		seen[l.ID] = true
	}
	// Lidarr's full 0-37 id range is present (38 distinct levels).
	if len(seen) != 38 {
		t.Errorf("expected 38 distinct music level ids, got %d", len(seen))
	}
}

// TestL3_MusicProfileDefaults covers the 4 seeded Lidarr music quality
// profiles: ids in the 200+ space, exact names (default first), exact
// approved items (music level ids, all approved), and exact cutoffs.
func TestL3_MusicProfileDefaults(t *testing.T) {
	profiles := DefaultMusicProfiles()
	if len(profiles) != 4 {
		t.Fatalf("want 4 music profiles, got %d", len(profiles))
	}
	type wantProfile struct {
		id      int
		name    string
		approve []int
		cutoff  int
	}
	want := []wantProfile{
		{200, "Lossless Best Effort", []int{
			213, 235, 236, 221, 237, 206, 207,
			214, 211, 212, 204, 202, 215,
		}, 221},
		{201, "High Quality Lossy", []int{214, 211, 212, 204, 202, 215}, 214},
		{202, "Lossless", []int{213, 235, 236, 221, 237, 206, 207}, 221},
		{203, "WAV", []int{213}, 213},
	}
	for i, p := range profiles {
		w := want[i]
		if p.ID != w.id || p.Name != w.name || p.Cutoff != w.cutoff {
			t.Errorf("music profile %d = (id=%d name=%q cutoff=%d), want (id=%d name=%q cutoff=%d)",
				i, p.ID, p.Name, p.Cutoff, w.id, w.name, w.cutoff)
		}
		if len(p.Items) != len(w.approve) {
			t.Fatalf("music profile %q: %d items, want %d", p.Name, len(p.Items), len(w.approve))
		}
		for j, item := range p.Items {
			if !item.Approved || item.Quality != w.approve[j] {
				t.Errorf("music profile %q item %d = (quality=%d approved=%v), want (quality=%d approved=true)",
					p.Name, j, item.Quality, item.Approved, w.approve[j])
			}
		}
	}
}

// TestL3_MusicProfileNames covers the 4 music profile names and the default.
func TestL3_MusicProfileNames(t *testing.T) {
	names := MusicProfileNames()
	for _, n := range []string{"Lossless Best Effort", "High Quality Lossy", "Lossless", "WAV"} {
		if !names[n] {
			t.Errorf("missing music profile name %q", n)
		}
	}
	if len(names) != 4 {
		t.Errorf("expected 4 music profile names, got %d", len(names))
	}
	if DefaultMusicQualityProfile != "Lossless Best Effort" {
		t.Errorf("default music quality profile = %q, want Lossless Best Effort", DefaultMusicQualityProfile)
	}
}

// TestL3_MetadataProfileDefaults covers the 3 seeded metadata profiles:
// exact names, the documented item lists, and the item vocabulary.
func TestL3_MetadataProfileDefaults(t *testing.T) {
	profiles := DefaultMetadataProfiles()
	if len(profiles) != 3 {
		t.Fatalf("want 3 metadata profiles, got %d", len(profiles))
	}
	want := map[string][]string{
		"Standard": {"Embedded", "Music XML", "Music Image"},
		"Album":    {"Embedded", "Album TXT"},
		"Studio":   {"Embedded", "Artist TXT"},
	}
	for _, p := range profiles {
		items, ok := want[p.Name]
		if !ok {
			t.Errorf("unexpected metadata profile %q", p.Name)
			continue
		}
		delete(want, p.Name)
		if p.ID < 1 || p.Description == "" {
			t.Errorf("metadata profile %q: id/description missing: %+v", p.Name, p)
		}
		if len(p.Items) != len(items) {
			t.Errorf("metadata profile %q items = %v, want %v", p.Name, p.Items, items)
			continue
		}
		for i, it := range p.Items {
			if it != items[i] {
				t.Errorf("metadata profile %q item %d = %q, want %q", p.Name, i, it, items[i])
			}
		}
	}
	if got := MetadataProfileNames(); len(got) != 3 {
		t.Errorf("expected 3 metadata profile names, got %d", len(got))
	}
}
