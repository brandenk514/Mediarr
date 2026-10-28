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
