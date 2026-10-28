package wanted

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/brandenk514/mediarr/internal/settings"
)

func ts(s string) *time.Time {
	t, _ := time.Parse("2006-01-02", s)
	return &t
}

func TestMusicCutoffLevel(t *testing.T) {
	// Known profile names resolve to their seeded cutoffs.
	if got := MusicCutoffLevel("Lossless Best Effort"); got != settings.MusicLevelFLAC24 {
		t.Errorf("Lossless Best Effort cutoff = %d, want %d (FLAC 24bit)", got, settings.MusicLevelFLAC24)
	}
	if got := MusicCutoffLevel("High Quality Lossy"); got != settings.MusicLevelOGGQ10 {
		t.Errorf("High Quality Lossy cutoff = %d, want %d (OGG Q10)", got, settings.MusicLevelOGGQ10)
	}
	if got := MusicCutoffLevel("WAV"); got != settings.MusicLevelWAV {
		t.Errorf("WAV cutoff = %d, want %d", got, settings.MusicLevelWAV)
	}
	// The L3 fallback: an empty or unknown profile name resolves to the
	// DefaultMusicQualityProfile profile (Lossless Best Effort -> FLAC 24bit).
	for _, name := range []string{"", "   ", "Not A Real Profile"} {
		if got := MusicCutoffLevel(name); got != settings.MusicLevelFLAC24 {
			t.Errorf("cutoff for %q = %d, want %d (default profile fallback)", name, got, settings.MusicLevelFLAC24)
		}
	}
}

func TestBelowMusicCutoff(t *testing.T) {
	// Cutoffs are seeded level ids; file qualities are codec names (the
	// Track.Status strings) resolved by QualityLevelID.
	oggCut := settings.MusicLevelOGGQ10 // 214
	lbeCut := settings.MusicLevelFLAC24 // 221
	cases := []struct {
		quality string
		cutoff  int
		want    bool
	}{
		{"OGG", oggCut, false}, // at cutoff: not below
		{"APE", oggCut, false}, // better than cutoff
		{"MP3", oggCut, true},  // far below
		{"FLAC", lbeCut, true}, // FLAC (206) is below FLAC 24bit (221)
		{"", lbeCut, true},     // unknown is below every cutoff
		{"Gibberish", lbeCut, true},
		{"FLAC", 0, false}, // unevaluable cutoff
	}
	for _, c := range cases {
		if got := BelowMusicCutoff(c.quality, c.cutoff); got != c.want {
			t.Errorf("BelowMusicCutoff(%q, %d) = %v, want %v", c.quality, c.cutoff, got, c.want)
		}
	}
}

func TestMusicLevelName(t *testing.T) {
	if got := MusicLevelName(settings.MusicLevelFLAC24); got != "FLAC 24bit" {
		t.Errorf("MusicLevelName(221) = %q, want FLAC 24bit", got)
	}
	if got := MusicLevelName(999); got != "unknown" {
		t.Errorf("MusicLevelName(999) = %q, want unknown", got)
	}
}

// missingRows is the shared fixture for the missing-tab tests: a monitored
// file-less album, a monitored imported album, an unmonitored file-less
// album, and three track rows (monitored file-less, monitored imported,
// unmonitored).
func missingRows() []MusicRow {
	return []MusicRow{
		{ArtistID: 1, ArtistTitle: "Bob Marley", AlbumID: 10, AlbumTitle: "Legend", AlbumType: "album", ReleaseDate: ts("1977-06-13"), Monitored: true, TrackCount: 2},
		{ArtistID: 1, ArtistTitle: "Bob Marley", AlbumID: 11, AlbumTitle: "Uprising", Monitored: true, TrackCount: 2, FileCount: 1, ImportedQualities: []string{"FLAC"}},
		{ArtistID: 2, ArtistTitle: "Jimi Hendrix", AlbumID: 12, AlbumTitle: "Axis", Monitored: false, TrackCount: 2},
		{ArtistID: 1, ArtistTitle: "Bob Marley", AlbumID: 10, AlbumTitle: "Legend", Monitored: true, Disc: 1, Number: 1, TrackID: 101, TrackTitle: "No Woman, No Man", TrackMonitored: true},
		{ArtistID: 1, ArtistTitle: "Bob Marley", AlbumID: 10, AlbumTitle: "Legend", Monitored: true, Disc: 1, Number: 2, TrackID: 102, TrackTitle: "Three Little Birds", TrackMonitored: true, TrackStatus: "FLAC"},
		{ArtistID: 1, ArtistTitle: "Bob Marley", AlbumID: 10, AlbumTitle: "Legend", Monitored: true, Disc: 1, Number: 3, TrackID: 103, TrackTitle: "Is This Love", TrackMonitored: false},
	}
}

func TestMusicFilter_Missing(t *testing.T) {
	items := MusicFilter{Tab: TabMissing}.Apply(missingRows(), nil)
	if len(items) != 2 {
		t.Fatalf("missing items = %d, want 2 (file-less album + file-less track); %+v", len(items), items)
	}
	// The album row: the columns carry the enriched catalog fields.
	if items[0].Kind != "music" || items[0].AlbumTitle != "Legend" ||
		items[0].ArtistTitle != "Bob Marley" || items[0].AlbumType != "album" ||
		items[0].ReleaseDate == nil || !items[0].ReleaseDate.Equal(time.Date(1977, 6, 13, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("album row = %+v", items[0])
	}
	if items[0].Status != "missing:no file" {
		t.Errorf("album status = %q", items[0].Status)
	}
	// The track row: the episode-shaped fields carry the track identity.
	if items[1].Season != 1 || items[1].Episode != 1 || items[1].EpisodeTitle != "No Woman, No Man" {
		t.Errorf("track row = %+v", items[1])
	}
}

func TestMusicFilter_MissingSearch(t *testing.T) {
	items := MusicFilter{Tab: TabMissing, Q: "uprising"}.Apply(missingRows(), nil)
	// "Uprising" is imported (FileCount 1), so the search matches nothing.
	if len(items) != 0 {
		t.Fatalf("search missing: got %d items, want 0; %+v", len(items), items)
	}
	items = MusicFilter{Tab: TabMissing, Q: "bob"}.Apply(missingRows(), nil)
	// Both missing rows are Bob Marley's.
	if len(items) != 2 {
		t.Fatalf("search artist: got %d items, want 2; %+v", len(items), items)
	}
}

func TestMusicFilter_Cutoff(t *testing.T) {
	rows := []MusicRow{
		// Monitored, imported in FLAC/MP3, no profile -> default cutoff
		// (FLAC 24bit): both below -> cutoff-unmet.
		{ArtistID: 1, ArtistTitle: "Bob Marley", AlbumID: 10, AlbumTitle: "Legend", Monitored: true, FileCount: 2, ImportedQualities: []string{"FLAC", "MP3"}},
		// Imported at the WAV cutoff: not unmet.
		{ArtistID: 2, ArtistTitle: "Jimi Hendrix", AlbumID: 11, AlbumTitle: "Axis", Monitored: true, ProfileName: "WAV", FileCount: 1, ImportedQualities: []string{"WAV"}},
		// Imported above the Lossless Best Effort cutoff: not unmet.
		{ArtistID: 3, ArtistTitle: "Miles Davis", AlbumID: 12, AlbumTitle: "Kind of Blue", Monitored: true, ProfileName: "Lossless Best Effort", FileCount: 1, ImportedQualities: []string{"APE"}},
		// A file-less monitored album is a missing row, not a cutoff row.
		{ArtistID: 3, ArtistTitle: "Miles Davis", AlbumID: 13, AlbumTitle: "Sketches", Monitored: true, ProfileName: "Lossless Best Effort"},
		// Track rows drop out of the cutoff tab.
		{ArtistID: 1, ArtistTitle: "Bob Marley", AlbumID: 10, AlbumTitle: "Legend", Monitored: true, Disc: 1, Number: 1, TrackID: 101, TrackTitle: "x", TrackMonitored: true, TrackStatus: "FLAC"},
		// Unknown profile name -> the L3 fallback resolves the default
		// profile (FLAC 24bit cutoff), so the MP3 file IS below it.
		{ArtistID: 4, ArtistTitle: "Prince", AlbumID: 14, AlbumTitle: "1999", Monitored: true, ProfileName: "Not A Profile", FileCount: 1, ImportedQualities: []string{"MP3"}},
		// Unmonitored: excluded.
		{ArtistID: 5, ArtistTitle: "Fleetwood Mac", AlbumID: 15, AlbumTitle: "Rumours", Monitored: false, FileCount: 1, ImportedQualities: []string{"MP3"}},
	}
	items := MusicFilter{Tab: TabCutoff}.Apply(rows, map[int64]int{
		10: MusicCutoffLevel(""),
		11: MusicCutoffLevel("WAV"),
		12: MusicCutoffLevel("Lossless Best Effort"),
		13: MusicCutoffLevel("Lossless Best Effort"),
		14: MusicCutoffLevel("Not A Profile"),
		15: MusicCutoffLevel(""),
	})
	if len(items) != 2 {
		t.Fatalf("cutoff items = %d, want 2 (Legend + 1999 via the default fallback); %+v", len(items), items)
	}
	if items[0].AlbumTitle != "Legend" || items[0].Quality != "FLAC" {
		t.Errorf("first cutoff row = %+v, want Legend at worst quality FLAC", items[0])
	}
	if items[0].Status != "cutoff:FLAC below cutoff FLAC 24bit" {
		t.Errorf("cutoff status = %q", items[0].Status)
	}
	if items[1].AlbumTitle != "1999" || items[1].Quality != "MP3" {
		t.Errorf("second cutoff row = %+v, want 1999 at MP3 (unknown profile falls back to the default)", items[1])
	}
}

func TestMusicFilter_InvalidTab(t *testing.T) {
	items := MusicFilter{Tab: Tab("bogus")}.Apply(missingRows(), nil)
	if len(items) != 0 {
		t.Fatalf("bogus tab: got %d items, want 0", len(items))
	}
}

func TestMusicFilter_EmptyResult(t *testing.T) {
	items := MusicFilter{Tab: TabMissing}.Apply(nil, nil)
	if items == nil {
		t.Fatal("Apply(nil) returned nil, want non-nil empty slice")
	}
	if len(items) != 0 {
		t.Fatalf("Apply(nil) = %+v, want empty", items)
	}
}

// TestMusicRef_JSON checks the wire shape the Unmonitor endpoint accepts.
func TestMusicRef_JSON(t *testing.T) {
	r := MusicRef{AlbumID: 7, Disc: 1, Number: 3}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var back MusicRef
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back != r {
		t.Fatalf("round-trip = %+v, want %+v (%s)", back, r, b)
	}
	// A whole-album ref omits disc/number.
	b, _ = json.Marshal(MusicRef{AlbumID: 7})
	if string(b) != `{"album_id":7}` {
		t.Fatalf("whole-album ref JSON = %s, want {\"album_id\":7}", b)
	}
}
