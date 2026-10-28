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

// TestL3_MusicLevelValidation covers the music level rules: the TV rules
// apply, scoped to the music_levels array.
func TestL3_MusicLevelValidation(t *testing.T) {
	// Defaults must validate (levels, movie_levels, music_levels).
	if msg := DefaultQuality().Validate(); msg != "" {
		t.Fatalf("defaults must validate: %s", msg)
	}
	cases := []struct {
		name string
		mut  func(*QualitySettings)
		want string
	}{
		{
			name: "duplicate music id",
			mut: func(q *QualitySettings) {
				q.MusicLevels = append(q.MusicLevels, QualityLevel{ID: MusicLevelWAV, Name: "WAV dup"})
			},
			want: "music_levels[38]: duplicate id 213",
		},
		{
			name: "missing music name",
			mut: func(q *QualitySettings) {
				q.MusicLevels[0].Name = ""
			},
			want: "music_levels[0]: name is required",
		},
		{
			name: "negative music size limit",
			mut: func(q *QualitySettings) {
				q.MusicLevels[0].SizeLimitMB = -1
			},
			want: "music_levels[0]: size_limit_mb must be >= 0",
		},
		{
			name: "movie level rules still scoped to movie_levels",
			mut: func(q *QualitySettings) {
				q.MovieLevels = append(q.MovieLevels, QualityLevel{ID: 100, Name: "480p", Source: "dvd", Resolution: "480p"})
			},
			want: "movie_levels[13]: duplicate id 100",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			q := DefaultQuality()
			c.mut(&q)
			if got := q.Validate(); got != c.want {
				t.Errorf("Validate() = %q, want %q", got, c.want)
			}
		})
	}
}

// TestL3_MusicProfileValidation covers the music profile rules: duplicate
// ids/names per array, and items/cutoffs that reference unknown music level
// ids (including a movie id, which is unknown in the music space).
func TestL3_MusicProfileValidation(t *testing.T) {
	if msg := DefaultProfiles().Validate(); msg != "" {
		t.Fatalf("defaults must validate: %s", msg)
	}
	cases := []struct {
		name string
		mut  func(*ProfilesSettings)
		want string
	}{
		{
			name: "duplicate music profile id",
			mut: func(p *ProfilesSettings) {
				p.MusicProfiles = append(p.MusicProfiles, p.MusicProfiles[0])
			},
			want: "music_profiles[4]: duplicate id 200",
		},
		{
			name: "duplicate music profile name",
			mut: func(p *ProfilesSettings) {
				p.MusicProfiles = append(p.MusicProfiles, QualityProfile{
					ID: 999, Name: "WAV", Cutoff: MusicLevelWAV,
					Items: []QualityProfileItem{{Quality: MusicLevelWAV, Approved: true}},
				})
			},
			want: `music_profiles[4]: duplicate name "WAV"`,
		},
		{
			name: "cutoff not a music level (movie id)",
			mut: func(p *ProfilesSettings) {
				p.MusicProfiles[0].Cutoff = 100
			},
			want: "music_profiles[0]: cutoff must reference a music quality level id",
		},
		{
			name: "item not a music level (TV id)",
			mut: func(p *ProfilesSettings) {
				p.MusicProfiles[3].Items[0].Quality = 1
			},
			want: "music_profiles[3].items[0]: unknown music quality level id 1",
		},
		{
			name: "empty music profile items",
			mut: func(p *ProfilesSettings) {
				p.MusicProfiles[3].Items = nil
			},
			want: "music_profiles[3]: at least one quality item is required",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := DefaultProfiles()
			c.mut(&p)
			if got := p.Validate(); got != c.want {
				t.Errorf("Validate() = %q, want %q", got, c.want)
			}
		})
	}
}

// TestL3_MetadataProfileValidation covers the metadata profile id/name
// rules via the shared helper.
func TestL3_MetadataProfileValidation(t *testing.T) {
	p := DefaultProfiles()
	if msg := p.Validate(); msg != "" {
		t.Fatalf("defaults must validate: %s", msg)
	}
	dup := p
	dup.MetadataProfiles = append(dup.MetadataProfiles, MetadataProfile{ID: 1, Name: "Standard"})
	if got := dup.Validate(); got != "metadata_profiles[3]: duplicate id 1" {
		t.Errorf("Validate() = %q, want the duplicate-id error", got)
	}
}

// TestL3_MusicNamingValidation covers the music naming rules: both format
// templates required, colon replacement at most one rune (multi-byte ok).
func TestL3_MusicNamingValidation(t *testing.T) {
	if err := DefaultMediaManagement().Validate(); err != "" {
		t.Fatalf("defaults must validate: %s", err)
	}
	cases := []struct {
		name string
		mut  func(*MediaManagementSettings)
		want string
	}{
		{
			name: "empty standard format",
			mut: func(m *MediaManagementSettings) {
				m.MusicNaming.StandardFormat = ""
			},
			want: "music_naming.standard_format is required",
		},
		{
			name: "empty multi-disc format",
			mut: func(m *MediaManagementSettings) {
				m.MusicNaming.MultiDiscFormat = ""
			},
			want: "music_naming.multi_disc_format is required",
		},
		{
			name: "two-char colon replacement",
			mut: func(m *MediaManagementSettings) {
				m.MusicNaming.ColonReplacement = "sl"
			},
			want: "music_naming.colon_replacement must be a single character or empty",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := DefaultMediaManagement()
			c.mut(&m)
			if got := m.Validate(); got != c.want {
				t.Errorf("Validate() = %q, want %q", got, c.want)
			}
		})
	}
	// A single multi-byte rune is fine.
	m := DefaultMediaManagement()
	m.MusicNaming.ColonReplacement = "/"
	if err := m.Validate(); err != "" {
		t.Errorf("single-rune colon replacement must validate: %s", err)
	}
}

// TestL3_MetadataConsumerValidation covers the consumer id/name rules.
func TestL3_MetadataConsumerValidation(t *testing.T) {
	if err := DefaultMetadata().Validate(); err != "" {
		t.Fatalf("defaults must validate: %s", err)
	}
	noName := DefaultMetadata()
	noName.Consumers[0].Name = ""
	if got := noName.Validate(); got != "consumers[0]: name is required" {
		t.Errorf("Validate() = %q, want the required-name error", got)
	}
	dup := DefaultMetadata()
	dup.Consumers[1].Name = "WDTV"
	if got := dup.Validate(); got != `consumers[2]: duplicate name "WDTV"` {
		t.Errorf("Validate() = %q, want the duplicate-name error", got)
	}
}

// TestL3_ThemeValidation covers the UI theme rule: auto|dark|light only.
func TestL3_ThemeValidation(t *testing.T) {
	base := DefaultUI()
	if msg := base.Validate(); msg != "" {
		t.Fatalf("defaults must validate: %s", msg)
	}
	bad := base
	bad.Theme = "blue"
	if got := bad.Validate(); got != "theme must be one of auto, dark, light" {
		t.Errorf("Validate() = %q, want the theme error", got)
	}
	for _, ok := range []string{"auto", "dark", "light"} {
		g := base
		g.Theme = ok
		if msg := g.Validate(); msg != "" {
			t.Errorf("theme %q must validate: %s", ok, msg)
		}
	}
}
