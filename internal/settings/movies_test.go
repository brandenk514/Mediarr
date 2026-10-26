package settings

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestR5a_MovieLevelDefaults covers the seeded Radarr movie set: 13 levels in
// their own 100+ id space, sources per the Radarr v6 vocabulary.
func TestR5a_MovieLevelDefaults(t *testing.T) {
	levels := DefaultMovieLevels()
	if len(levels) != 13 {
		t.Fatalf("want 13 movie levels, got %d", len(levels))
	}
	want := []struct {
		id      int
		name    string
		source  string
		res     string
		limitMB int
	}{
		{100, "480p", "dvd", "480p", 1024},
		{101, "720p WEB", "webrip", "720p", 1536},
		{102, "720p", "blu-ray", "720p", 2560},
		{103, "720p REMUX", "remux", "720p", 7168},
		{104, "1080p WEB", "webrip", "1080p", 1536},
		{105, "1080p", "blu-ray", "1080p", 4096},
		{106, "1080p REMUX", "remux", "1080p", 11264},
		{107, "2160p WEB", "webrip", "2160p", 5120},
		{108, "2160p", "blu-ray", "2160p", 8192},
		{109, "2160p REMUX", "remux", "2160p", 20480},
		{110, "BR-DISK", "br-disk", "2160p", 25600},
		{111, "Raw-HD", "raw-hd", "2160p", 51200},
		{112, "Unknown", "unknown", "", 0},
	}
	for i, l := range levels {
		w := want[i]
		if l.ID != w.id || l.Name != w.name || l.Source != w.source || l.Resolution != w.res || l.SizeLimitMB != w.limitMB {
			t.Errorf("movie level %d = %+v, want %+v", i, l, w)
		}
	}
}

// TestR5a_TVLevelsRegression is the TV contract: the TV matrix must stay
// byte-identical (P5) — the Radarr movie set is additive only.
func TestR5a_TVLevelsRegression(t *testing.T) {
	got, err := json.Marshal(DefaultQuality().Levels)
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"id":1,"name":"SD","source":"dvd","resolution":"480p","size_limit_mb":1024},{"id":2,"name":"720p WEB","source":"webrip","resolution":"720p","size_limit_mb":1536},{"id":3,"name":"720p","source":"blu-ray","resolution":"720p","size_limit_mb":2560},{"id":4,"name":"720p REMUX","source":"remux","resolution":"720p","size_limit_mb":7168},{"id":5,"name":"1080p WEB","source":"webrip","resolution":"1080p","size_limit_mb":1536},{"id":6,"name":"1080p","source":"blu-ray","resolution":"1080p","size_limit_mb":4096},{"id":7,"name":"1080p REMUX","source":"remux","resolution":"1080p","size_limit_mb":11264},{"id":8,"name":"2160p WEB","source":"webrip","resolution":"2160p","size_limit_mb":5120},{"id":9,"name":"2160p","source":"blu-ray","resolution":"2160p","size_limit_mb":8192},{"id":10,"name":"2160p REMUX","source":"remux","resolution":"2160p","size_limit_mb":20480}]`
	if string(got) != want {
		t.Errorf("TV levels drifted:\ngot  %s\nwant %s", got, want)
	}
}

// TestR5a_TVProfilesRegression is the TV contract for the profiles document:
// the TV profile array stays byte-identical.
func TestR5a_TVProfilesRegression(t *testing.T) {
	got, err := json.Marshal(DefaultProfiles().Profiles)
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"id":1,"name":"Default","items":[{"quality":1,"approved":true},{"quality":2,"approved":true},{"quality":3,"approved":true},{"quality":4,"approved":true},{"quality":5,"approved":true},{"quality":6,"approved":true},{"quality":7,"approved":true},{"quality":8,"approved":true},{"quality":9,"approved":true},{"quality":10,"approved":true}],"cutoff":10}]`
	if string(got) != want {
		t.Errorf("TV profiles drifted:\ngot  %s\nwant %s", got, want)
	}
}

// TestR5a_MovieProfilesDefaults covers the 14 seeded Radarr v6 movie
// profiles: ids in their own 100+ space, best-effort approval sets, and
// cutoffs at each profile's highest approved level.
func TestR5a_MovieProfilesDefaults(t *testing.T) {
	profs := DefaultMovieProfiles()
	if len(profs) != 14 {
		t.Fatalf("want 14 movie profiles, got %d", len(profs))
	}
	want := []struct {
		id      int
		name    string
		approve []int
		cutoff  int
	}{
		{100, "4K Best Effort", []int{100, 101, 102, 103, 104, 105, 106, 107, 108}, 108},
		{101, "4K", []int{108}, 108},
		{102, "4K Remux", []int{109}, 109},
		{103, "4K BR-DISK", []int{110}, 110},
		{104, "1080p Best Effort", []int{100, 101, 102, 103, 104, 105}, 105},
		{105, "1080p", []int{105}, 105},
		{106, "1080p Remux", []int{106}, 106},
		{107, "720p Best Effort", []int{100, 101, 102}, 102},
		{108, "720p", []int{102}, 102},
		{109, "720p Remux", []int{103}, 103},
		{110, "BR-DISK", []int{110}, 110},
		{111, "Raw-HD", []int{111}, 111},
		{112, "Unknown", []int{112}, 112},
		{113, "HDTV", []int{100}, 100},
	}
	for i, prof := range profs {
		w := want[i]
		if prof.ID != w.id || prof.Name != w.name || prof.Cutoff != w.cutoff {
			t.Errorf("movie profile %d: got (id=%d name=%q cutoff=%d), want (id=%d name=%q cutoff=%d)",
				i, prof.ID, prof.Name, prof.Cutoff, w.id, w.name, w.cutoff)
		}
		got := make([]int, 0, len(prof.Items))
		for _, it := range prof.Items {
			if !it.Approved {
				t.Errorf("movie profile %q: item %d must be approved", prof.Name, it.Quality)
			}
			got = append(got, it.Quality)
		}
		if len(got) != len(w.approve) {
			t.Errorf("movie profile %q: approved levels %v, want %v", prof.Name, got, w.approve)
			continue
		}
		for j, q := range got {
			if q != w.approve[j] {
				t.Errorf("movie profile %q: approved levels %v, want %v", prof.Name, got, w.approve)
				break
			}
		}
	}
	// The Radarr v6 factory default is first.
	if profs[0].Name != DefaultMovieQualityProfile {
		t.Errorf("first movie profile = %q, want %q", profs[0].Name, DefaultMovieQualityProfile)
	}
}

// TestR5a_DelayReleaseDefaults covers the single seeded delay profile (empty,
// id 1) and release profile (all-inclusive, id 1).
func TestR5a_DelayReleaseDefaults(t *testing.T) {
	d := DefaultDelayProfiles()
	if len(d) != 1 || d[0].ID != 1 || d[0].Name != "Default" || d[0].Webrip != 0 || d[0].Bluray != 0 ||
		d[0].Remux != 0 || d[0].BrDisk != 0 || d[0].RawHD != 0 || d[0].HDTV != 0 {
		t.Errorf("delay defaults wrong: %+v", d)
	}
	r := DefaultReleaseProfiles()
	if len(r) != 1 || r[0].ID != 1 || r[0].Name != "Default" ||
		!r[0].Webrip || !r[0].Bluray || !r[0].Remux || !r[0].BrDisk || !r[0].RawHD || !r[0].HDTV {
		t.Errorf("release defaults wrong: %+v", r)
	}
}

// TestR5a_Decode_MergesMovieDefaults covers the fresh-install/upgrade read:
// a stored pre-R5a document (no movie arrays) reads with the seeded movie
// documents default-filled.
func TestR5a_Decode_MergesMovieDefaults(t *testing.T) {
	storedQ := []byte(`{"levels": []}`)
	dq, err := SectionQuality.DecodeDocument(storedQ)
	if err != nil {
		t.Fatal(err)
	}
	q := dq.(*QualitySettings)
	if len(q.MovieLevels) != 13 {
		t.Errorf("stored quality doc must default-fill movie_levels: %+v", q.MovieLevels)
	}

	storedP := []byte(`{"profiles": [{"id": 1, "name": "Default", "cutoff": 10, "items": [{"quality": 1, "approved": true}]}]}`)
	dp, err := SectionProfiles.DecodeDocument(storedP)
	if err != nil {
		t.Fatal(err)
	}
	p := dp.(*ProfilesSettings)
	if len(p.MovieProfiles) != 14 || len(p.DelayProfiles) != 1 || len(p.ReleaseProfiles) != 1 {
		t.Errorf("stored profiles doc must default-fill movie documents: %+v", p)
	}
}

// TestR5a_MovieLevelValidation covers the movie level rules: the TV rules
// apply, scoped to the movie_levels array.
func TestR5a_MovieLevelValidation(t *testing.T) {
	valid := DefaultQuality()
	if msg := valid.Validate(); msg != "" {
		t.Fatalf("defaults must validate: %s", msg)
	}
	cases := []struct {
		name string
		mut  func(*QualitySettings)
		want string
	}{
		{
			name: "duplicate movie id",
			mut: func(q *QualitySettings) {
				q.MovieLevels = append(q.MovieLevels, QualityLevel{ID: 100, Name: "480p", Source: "dvd", Resolution: "480p"})
			},
			want: "movie_levels[13]: duplicate id 100",
		},
		{
			name: "missing movie name",
			mut: func(q *QualitySettings) {
				q.MovieLevels[0].Name = ""
			},
			want: "movie_levels[0]: name is required",
		},
		{
			name: "negative movie size limit",
			mut: func(q *QualitySettings) {
				q.MovieLevels[0].SizeLimitMB = -1
			},
			want: "movie_levels[0]: size_limit_mb must be >= 0",
		},
		{
			name: "tv level rules still scoped to levels",
			mut: func(q *QualitySettings) {
				q.Levels = append(q.Levels, QualityLevel{ID: 1})
			},
			want: "levels[10]: duplicate id 1",
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

// TestR5a_MovieProfileValidation covers the movie profile rules: duplicate
// ids/names per array, and items/cutoffs that reference unknown movie level
// ids (including a TV id, which is unknown in the movie space).
func TestR5a_MovieProfileValidation(t *testing.T) {
	valid := DefaultProfiles()
	if msg := valid.Validate(); msg != "" {
		t.Fatalf("defaults must validate: %s", msg)
	}
	cases := []struct {
		name string
		mut  func(*ProfilesSettings)
		want string
	}{
		{
			name: "duplicate movie profile id",
			mut: func(p *ProfilesSettings) {
				p.MovieProfiles = append(p.MovieProfiles, p.MovieProfiles[0])
			},
			want: "movie_profiles[14]: duplicate id 100",
		},
		{
			name: "duplicate movie profile name",
			mut: func(p *ProfilesSettings) {
				p.MovieProfiles = append(p.MovieProfiles, QualityProfile{
					ID: 999, Name: "4K", Cutoff: 108,
					Items: []QualityProfileItem{{Quality: 108, Approved: true}},
				})
			},
			want: `movie_profiles[14]: duplicate name "4K"`,
		},
		{
			name: "unknown movie quality ref in items",
			mut: func(p *ProfilesSettings) {
				p.MovieProfiles[0].Items = append(p.MovieProfiles[0].Items, QualityProfileItem{Quality: 999, Approved: true})
			},
			want: "movie_profiles[0].items[9]: unknown movie quality level id 999",
		},
		{
			name: "tv level id is not a movie level id",
			mut: func(p *ProfilesSettings) {
				p.MovieProfiles[1].Items = []QualityProfileItem{{Quality: 9, Approved: true}}
			},
			want: "movie_profiles[1].items[0]: unknown movie quality level id 9",
		},
		{
			name: "cutoff must be a movie level id",
			mut: func(p *ProfilesSettings) {
				p.MovieProfiles[0].Cutoff = 10
			},
			want: "movie_profiles[0]: cutoff must reference a movie quality level id",
		},
		{
			name: "duplicate delay id",
			mut: func(p *ProfilesSettings) {
				p.DelayProfiles = append(p.DelayProfiles, DelayProfile{ID: 1, Name: "X"})
			},
			want: "delay_profiles[1]: duplicate id 1",
		},
		{
			name: "negative delay hours",
			mut: func(p *ProfilesSettings) {
				p.DelayProfiles[0].Webrip = -2
			},
			want: "delay_profiles[0]: hours must be >= 0",
		},
		{
			name: "duplicate release name",
			mut: func(p *ProfilesSettings) {
				p.ReleaseProfiles = append(p.ReleaseProfiles, ReleaseProfile{ID: 2, Name: "Default"})
			},
			want: `release_profiles[1]: duplicate name "Default"`,
		},
		{
			name: "release name required",
			mut: func(p *ProfilesSettings) {
				p.ReleaseProfiles[0].Name = ""
			},
			want: "release_profiles[0]: name is required",
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
	// A movie profile may legally share its name with a TV profile (the
	// duplicate check is per array).
	p := DefaultProfiles()
	p.MovieProfiles = append(p.MovieProfiles, QualityProfile{
		ID: 999, Name: "Default", Cutoff: 108,
		Items: []QualityProfileItem{{Quality: 108, Approved: true}},
	})
	if msg := p.Validate(); msg != "" {
		t.Errorf("cross-array name overlap must pass: %s", msg)
	}
	// Every movie profile name is in the seeded name set used by add-movie
	// validation.
	names := MovieProfileNames()
	if len(names) != 14 {
		t.Errorf("MovieProfileNames() = %d names, want 14", len(names))
	}
	for _, prof := range DefaultMovieProfiles() {
		if !names[prof.Name] {
			t.Errorf("MovieProfileNames() missing %q", prof.Name)
		}
	}
}

// TestR5a_MovieQualityProfileJSON sanity-checks the wire shapes of the new
// delay/release profile schemas (field names are the API contract).
func TestR5a_MovieQualityProfileJSON(t *testing.T) {
	b, err := json.Marshal(DelayProfile{ID: 1, Name: "d", Description: "x", Webrip: 1, Bluray: 2, Remux: 3, BrDisk: 4, RawHD: 5, HDTV: 6})
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, frag := range []string{`"webrip":1`, `"bluray":2`, `"remux":3`, `"brdisk":4`, `"rawhd":5`, `"hdtv":6`} {
		if !strings.Contains(got, frag) {
			t.Errorf("DelayProfile JSON missing %s: %s", frag, got)
		}
	}
	rb, err := json.Marshal(DefaultReleaseProfiles()[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rb), `"webrip":true`) || !strings.Contains(string(rb), `"hdtv":true`) {
		t.Errorf("ReleaseProfile JSON wrong: %s", rb)
	}
}

// R5a: the movie naming + unmonitor block on the media-management document.
func TestR5a_MovieNamingDefaults(t *testing.T) {
	mm := DefaultMediaManagement()
	if !mm.MovieNaming.RenameMovies || !mm.MovieNaming.ReplaceIllegalCharacters {
		t.Errorf("movie_naming booleans must default true: %+v", mm.MovieNaming)
	}
	if mm.MovieNaming.ColonReplacement != "" {
		t.Errorf("colon_replacement must default empty: %q", mm.MovieNaming.ColonReplacement)
	}
	if mm.MovieNaming.StandardFormat != DefaultMovieStandardFormat {
		t.Errorf("standard_format = %q, want %q", mm.MovieNaming.StandardFormat, DefaultMovieStandardFormat)
	}
	if !mm.Management.UnmonitorDeletedMovies {
		t.Error("unmonitor_deleted_movies must default true")
	}
	if err := SectionMediaManagement.ValidateDocument(mm); err != "" {
		t.Errorf("defaults must validate: %s", err)
	}
}

// R5a: a pre-R5a stored document (no movie_naming / unmonitor fields) reads
// the defaults — the decode-over-defaults path.
func TestR5a_MediaManagementLegacyStoredDoc(t *testing.T) {
	stored := []byte(`{"naming":{"series":"s","episode":"e","movie":"m","album":"a","track":"t","author":"a2","title":"tt"},"file_management":{"min_file_age_hours":0,"remove_empty_dirs":true,"import_extra_files":true,"rename_series":true},"remote_path_mappings":[]}`)
	doc, err := SectionMediaManagement.DecodeDocument(stored)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	mm, ok := doc.(*MediaManagementSettings)
	if !ok {
		t.Fatalf("decoded type %T", doc)
	}
	if !mm.MovieNaming.RenameMovies || mm.MovieNaming.StandardFormat != DefaultMovieStandardFormat {
		t.Errorf("movie_naming defaults not merged on read: %+v", mm.MovieNaming)
	}
	if !mm.Management.UnmonitorDeletedMovies {
		t.Error("unmonitor_deleted_movies default not merged on read")
	}
}

// R5a: movie naming validation — empty standard_format and a multi-rune
// colon replacement are rejected; a single (possibly multi-byte) character
// is accepted.
func TestR5a_MovieNamingValidation(t *testing.T) {
	base := DefaultMediaManagement()

	base.MovieNaming.StandardFormat = ""
	if err := SectionMediaManagement.ValidateDocument(base); err != "movie_naming.standard_format is required" {
		t.Errorf("empty standard_format: got %q", err)
	}

	base = DefaultMediaManagement()
	base.MovieNaming.ColonReplacement = "--"
	if err := SectionMediaManagement.ValidateDocument(base); err != "movie_naming.colon_replacement must be a single character or empty" {
		t.Errorf("two-char colon: got %q", err)
	}

	base = DefaultMediaManagement()
	base.MovieNaming.ColonReplacement = "→" // one rune, two bytes — Radarr allows a single character
	if err := SectionMediaManagement.ValidateDocument(base); err != "" {
		t.Errorf("single-rune colon must validate: %q", err)
	}
}
