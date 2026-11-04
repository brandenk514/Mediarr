package settings

import (
	"testing"
)

// TestB3_BookLevelDefaults covers the unified book quality definitions:
// 41 rows — the 3 e-book format-preference entries followed by the a-book
// ladder, which shares its seed values 1:1 with the music ladder (L3) in
// table order (same names, groups, size limits; book id = music id + 110).
func TestB3_BookLevelDefaults(t *testing.T) {
	levels := DefaultBookLevels()
	if len(levels) != 3+len(DefaultMusicLevels()) {
		t.Fatalf("want %d book levels, got %d", 3+len(DefaultMusicLevels()), len(levels))
	}
	wantEBook := []struct {
		id   int
		name string
	}{
		{BookLevelEPUB, "EPUB"},
		{BookLevelAZW3, "AZW3"},
		{BookLevelMOBI, "MOBI"},
	}
	for i, l := range levels[:3] {
		if l.ID != wantEBook[i].id || l.Name != wantEBook[i].name {
			t.Errorf("book level %d = (id=%d name=%q), want (id=%d name=%q)", i, l.ID, l.Name, wantEBook[i].id, wantEBook[i].name)
		}
		if l.Group != "E-book" {
			t.Errorf("e-book level %q: group = %q, want E-book", l.Name, l.Group)
		}
	}
	// The a-book ladder: 1:1 with the music ladder, re-based +110.
	for i, m := range DefaultMusicLevels() {
		b := levels[3+i]
		want := m.ID + bookABookOffset
		if b.ID != want {
			t.Errorf("a-book level %d: id = %d, want %d (music %d + 110)", i, b.ID, want, m.ID)
		}
		if b.Name != m.Name || b.Group != m.Group || b.SizeLimitMB != m.SizeLimitMB {
			t.Errorf("a-book level %d = (name=%q group=%q limit=%d), want music twin (name=%q group=%q limit=%d)",
				i, b.Name, b.Group, b.SizeLimitMB, m.Name, m.Group, m.SizeLimitMB)
		}
	}
	// Unknown stays last, as in the music ladder.
	last := levels[len(levels)-1]
	if last.ID != BookLevelUnknown || last.Name != "Unknown" {
		t.Errorf("last book level = (id=%d name=%q), want Unknown", last.ID, last.Name)
	}
}

// TestB3_BookLevelIDSpace covers the id-space contract: every book level id
// is 300+ and unique; a-book ids live in 310-347 (music 200-237 re-based),
// e-book ids in 300-302.
func TestB3_BookLevelIDSpace(t *testing.T) {
	seen := map[int]bool{}
	for _, l := range DefaultBookLevels() {
		if l.ID < 300 {
			t.Errorf("book level %q: id %d outside the 300+ space", l.Name, l.ID)
		}
		if seen[l.ID] {
			t.Errorf("duplicate book level id %d", l.ID)
		}
		seen[l.ID] = true
		if l.ID >= 310 && l.ID > 347 {
			t.Errorf("a-book level %q: id %d outside 310-347", l.Name, l.ID)
		}
	}
	if len(seen) != 41 {
		t.Errorf("expected 41 distinct book level ids, got %d", len(seen))
	}
}

// TestB3_BookABookConstants pins the named a-book level constants to their
// music twins (+110) — the drift guard for the 1:1 seed sharing.
func TestB3_BookABookConstants(t *testing.T) {
	twins := []struct {
		book  int
		music int
	}{
		{BookLevelUnknown, MusicLevelUnknown},
		{BookLevelMP3VBRV0, MusicLevelMP3VBRV0},
		{BookLevelMP3320, MusicLevelMP3320},
		{BookLevelFLAC, MusicLevelFLAC},
		{BookLevelALAC, MusicLevelALAC},
		{BookLevelAAC320, MusicLevelAAC320},
		{BookLevelAACVBR, MusicLevelAACVBR},
		{BookLevelWAV, MusicLevelWAV},
		{BookLevelOGGQ10, MusicLevelOGGQ10},
		{BookLevelOGGQ9, MusicLevelOGGQ9},
		{BookLevelWMA, MusicLevelWMA},
		{BookLevelFLAC24, MusicLevelFLAC24},
		{BookLevelAPE, MusicLevelAPE},
		{BookLevelWavPack, MusicLevelWavPack},
		{BookLevelALAC24, MusicLevelALAC24},
		{BookLevelMP38, MusicLevelMP38},
	}
	for _, tw := range twins {
		if tw.book != tw.music+bookABookOffset {
			t.Errorf("book level %d != music level %d + 110", tw.book, tw.music)
		}
	}
}

// TestB3_BookProfileDefaults covers the 4 seeded book quality profiles:
// ids in the 300+ space, exact names ("Best" first), exact approved items
// (book level ids, all approved) and exact cutoffs.
func TestB3_BookProfileDefaults(t *testing.T) {
	profiles := DefaultBookProfiles()
	if len(profiles) != 4 {
		t.Fatalf("want 4 book profiles, got %d", len(profiles))
	}
	if profiles[0].Name != DefaultBookQualityProfile {
		t.Errorf("first book profile = %q, want the default %q", profiles[0].Name, DefaultBookQualityProfile)
	}
	// Expected approve sets: the seeded level table order (e-book entries
	// first, then the a-book ladder), minus Unknown for "Best".
	var all, allButUnknown []int
	for _, l := range DefaultBookLevels() {
		all = append(all, l.ID)
		if l.ID != BookLevelUnknown {
			allButUnknown = append(allButUnknown, l.ID)
		}
	}
	type wantProfile struct {
		id      int
		name    string
		approve []int
		cutoff  int
	}
	want := []wantProfile{
		{300, "Best", allButUnknown, BookLevelEPUB},
		{301, "EPUB", []int{BookLevelEPUB}, BookLevelEPUB},
		{302, "Kindle", []int{BookLevelAZW3, BookLevelMOBI}, BookLevelAZW3},
		{303, "Any", all, BookLevelUnknown},
	}
	for i, p := range profiles {
		w := want[i]
		if p.ID != w.id || p.Name != w.name || p.Cutoff != w.cutoff {
			t.Errorf("book profile %d = (id=%d name=%q cutoff=%d), want (id=%d name=%q cutoff=%d)",
				i, p.ID, p.Name, p.Cutoff, w.id, w.name, w.cutoff)
		}
		if len(p.Items) != len(w.approve) {
			t.Errorf("book profile %q: %d items, want %d", p.Name, len(p.Items), len(w.approve))
			continue
		}
		for j, it := range p.Items {
			if it.Quality != w.approve[j] || !it.Approved {
				t.Errorf("book profile %q item %d = (quality=%d approved=%v), want (quality=%d approved=true)",
					p.Name, j, it.Quality, it.Approved, w.approve[j])
			}
		}
	}
	// Every item and cutoff references a seeded book level id.
	seeded := seedBookLevelIDs()
	for _, p := range profiles {
		if !seeded[p.Cutoff] {
			t.Errorf("book profile %q: cutoff %d is not a seeded book level id", p.Name, p.Cutoff)
		}
		for _, it := range p.Items {
			if !seeded[it.Quality] {
				t.Errorf("book profile %q: item %d is not a seeded book level id", p.Name, it.Quality)
			}
		}
	}
}

// TestB3_BookProfileNames covers the seeded book profile name set and the
// default profile name (compatibility with author rows that store "Best").
func TestB3_BookProfileNames(t *testing.T) {
	names := BookProfileNames()
	want := map[string]bool{"Best": true, "EPUB": true, "Kindle": true, "Any": true}
	if len(names) != len(want) {
		t.Fatalf("want %d book profile names, got %d: %v", len(want), len(names), names)
	}
	for n := range want {
		if !names[n] {
			t.Errorf("book profile names missing %q", n)
		}
	}
	if DefaultBookQualityProfile != "Best" {
		t.Errorf("DefaultBookQualityProfile = %q, want Best", DefaultBookQualityProfile)
	}
}

// TestB3_BookMetadataProfileDefaults covers the seeded book metadata
// profile: a single "Default" with no filters (every book eligible) and a
// non-nil allowed_languages (the required JSON field).
func TestB3_BookMetadataProfileDefaults(t *testing.T) {
	profiles := DefaultBookMetadataProfiles()
	if len(profiles) != 1 {
		t.Fatalf("want 1 book metadata profile, got %d", len(profiles))
	}
	p := profiles[0]
	if p.ID != 1 || p.Name != "Default" {
		t.Errorf("book metadata profile = (id=%d name=%q), want (id=1 name=Default)", p.ID, p.Name)
	}
	if p.MinimumPopularity != 0 || p.SkipMissingReleaseDate || p.SkipNoISBNASIN ||
		p.SkipBrokenParts || p.SkipSecondarySeries {
		t.Errorf("Default book metadata profile must carry no filters: %+v", p)
	}
	if p.AllowedLanguages == nil {
		t.Error("Default book metadata profile: allowed_languages must be non-nil (required JSON field)")
	}
	names := BookMetadataProfileNames()
	if len(names) != 1 || !names["Default"] {
		t.Errorf("BookMetadataProfileNames = %v, want {Default:true}", names)
	}
}

// TestB3_BookProfilesValidation covers the book-side rules of
// ProfilesSettings.Validate: the seeded default document is valid, and each
// book-profile / book-metadata-profile violation is rejected.
func TestB3_BookProfilesValidation(t *testing.T) {
	if msg := DefaultProfiles().Validate(); msg != "" {
		t.Fatalf("DefaultProfiles().Validate() = %q, want empty", msg)
	}
	if msg := DefaultQuality().Validate(); msg != "" {
		t.Fatalf("DefaultQuality().Validate() = %q, want empty", msg)
	}

	cases := []struct {
		name string
		mut  func(*ProfilesSettings)
		want string
	}{
		{
			name: "cutoff references a TV level id",
			mut: func(p *ProfilesSettings) {
				p.BookProfiles[0].Cutoff = 1
			},
			want: "book_profiles[0]: cutoff must reference a book quality level id",
		},
		{
			name: "cutoff references a music level id",
			mut: func(p *ProfilesSettings) {
				p.BookProfiles[0].Cutoff = MusicLevelFLAC
			},
			want: "book_profiles[0]: cutoff must reference a book quality level id",
		},
		{
			name: "item references an unknown book level id",
			mut: func(p *ProfilesSettings) {
				p.BookProfiles[1].Items[0].Quality = 999
			},
			want: "book_profiles[1].items[0]: unknown book quality level id 999",
		},
		{
			name: "duplicate book profile name",
			mut: func(p *ProfilesSettings) {
				p.BookProfiles[1].Name = p.BookProfiles[0].Name
			},
			want: `book_profiles[1]: duplicate name "Best"`,
		},
		{
			name: "duplicate book profile id",
			mut: func(p *ProfilesSettings) {
				p.BookProfiles[1].ID = p.BookProfiles[0].ID
			},
			want: "book_profiles[1]: duplicate id 300",
		},
		{
			name: "book profile with no items",
			mut: func(p *ProfilesSettings) {
				p.BookProfiles[2].Items = nil
			},
			want: "book_profiles[2]: at least one quality item is required",
		},
		{
			name: "negative minimum popularity",
			mut: func(p *ProfilesSettings) {
				p.BookMetadataProfiles[0].MinimumPopularity = -1
			},
			want: "book_metadata_profiles[0]: minimum_popularity must be >= 0",
		},
		{
			name: "duplicate book metadata profile name",
			mut: func(p *ProfilesSettings) {
				dup := p.BookMetadataProfiles[0]
				dup.ID = 2
				p.BookMetadataProfiles = append(p.BookMetadataProfiles, dup)
			},
			want: `book_metadata_profiles[1]: duplicate name "Default"`,
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

// TestB3_QualitySettingsBookLevelsValidation covers the book_levels side of
// QualitySettings.Validate (the shared per-level rules, scoped to the book
// array).
func TestB3_QualitySettingsBookLevelsValidation(t *testing.T) {
	q := DefaultQuality()
	if q.BookLevels == nil || len(q.BookLevels) != 41 {
		t.Fatalf("DefaultQuality().BookLevels = %d rows, want 41", len(q.BookLevels))
	}
	dup := DefaultQuality()
	dup.BookLevels = append(dup.BookLevels, dup.BookLevels[0])
	if got := dup.Validate(); got != "book_levels[41]: duplicate id 300" {
		t.Errorf("Validate() = %q, want duplicate id 300", got)
	}
	empty := DefaultQuality()
	empty.BookLevels = nil
	// An empty array passes the per-row rules (the TV/movie/music arrays
	// behave the same).
	if got := empty.Validate(); got != "" {
		t.Errorf("Validate() of empty book_levels = %q, want empty", got)
	}
}
