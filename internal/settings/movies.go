package settings

import "fmt"

// ---------------------------------------------------------------------------
// Radarr R5a: movie quality definitions + movie/delay/release profiles
//
// All of this is additive on top of the TV parity documents: the TV quality
// levels (ids 1-10) and TV quality profiles are untouched. Movie levels and
// movie profile ids live in separate spaces starting at 100 so a profile item
// reference can't collide across the two kinds.
// ---------------------------------------------------------------------------

// DefaultMovieQualityProfile is the Radarr v6 factory default movie quality
// profile name; an empty quality profile on an add resolves to it (R5a).
const DefaultMovieQualityProfile = "4K Best Effort"

// Movie quality level ids (a separate space from the TV levels, which end at
// 10). The Radarr v6 movie quality set.
const (
	MovieLevel480p       = 100
	MovieLevel720pWeb    = 101
	MovieLevel720p       = 102
	MovieLevel720pRemux  = 103
	MovieLevel1080pWeb   = 104
	MovieLevel1080p      = 105
	MovieLevel1080pRemux = 106
	MovieLevel2160pWeb   = 107
	MovieLevel2160p      = 108
	MovieLevel2160pRemux = 109
	MovieLevelBRDisk     = 110
	MovieLevelRawHD      = 111
	MovieLevelUnknown    = 112
)

// DefaultMovieLevels returns the Radarr v6 movie quality set. Size limits
// mirror the TV matrix where the tiers line up; 0 = unlimited.
func DefaultMovieLevels() []QualityLevel {
	return []QualityLevel{
		{ID: MovieLevel480p, Name: "480p", Source: "dvd", Resolution: "480p", SizeLimitMB: 1024},
		{ID: MovieLevel720pWeb, Name: "720p WEB", Source: "webrip", Resolution: "720p", SizeLimitMB: 1536},
		{ID: MovieLevel720p, Name: "720p", Source: "blu-ray", Resolution: "720p", SizeLimitMB: 2560},
		{ID: MovieLevel720pRemux, Name: "720p REMUX", Source: "remux", Resolution: "720p", SizeLimitMB: 7168},
		{ID: MovieLevel1080pWeb, Name: "1080p WEB", Source: "webrip", Resolution: "1080p", SizeLimitMB: 1536},
		{ID: MovieLevel1080p, Name: "1080p", Source: "blu-ray", Resolution: "1080p", SizeLimitMB: 4096},
		{ID: MovieLevel1080pRemux, Name: "1080p REMUX", Source: "remux", Resolution: "1080p", SizeLimitMB: 11264},
		{ID: MovieLevel2160pWeb, Name: "2160p WEB", Source: "webrip", Resolution: "2160p", SizeLimitMB: 5120},
		{ID: MovieLevel2160p, Name: "2160p", Source: "blu-ray", Resolution: "2160p", SizeLimitMB: 8192},
		{ID: MovieLevel2160pRemux, Name: "2160p REMUX", Source: "remux", Resolution: "2160p", SizeLimitMB: 20480},
		{ID: MovieLevelBRDisk, Name: "BR-DISK", Source: "br-disk", Resolution: "2160p", SizeLimitMB: 25600},
		{ID: MovieLevelRawHD, Name: "Raw-HD", Source: "raw-hd", Resolution: "2160p", SizeLimitMB: 51200},
		{ID: MovieLevelUnknown, Name: "Unknown", Source: "unknown", Resolution: "", SizeLimitMB: 0},
	}
}

// movieProfileSpec is one seeded movie profile: id (100+ space), name, the
// approved movie level ids, and the cutoff level id.
type movieProfileSpec struct {
	id      int
	name    string
	approve []int
	cutoff  int
}

// defaultMovieProfileSpecs are the 14 Radarr v6 default movie profiles.
// Best-effort profiles approve everything up to and including their tier
// (the tier's web + main sources); the others approve only their own level.
// Cutoffs sit at the profile's highest approved level.
var defaultMovieProfileSpecs = []movieProfileSpec{
	{100, "4K Best Effort", []int{MovieLevel480p, MovieLevel720pWeb, MovieLevel720p, MovieLevel720pRemux,
		MovieLevel1080pWeb, MovieLevel1080p, MovieLevel1080pRemux, MovieLevel2160pWeb, MovieLevel2160p}, MovieLevel2160p},
	{101, "4K", []int{MovieLevel2160p}, MovieLevel2160p},
	{102, "4K Remux", []int{MovieLevel2160pRemux}, MovieLevel2160pRemux},
	{103, "4K BR-DISK", []int{MovieLevelBRDisk}, MovieLevelBRDisk},
	{104, "1080p Best Effort", []int{MovieLevel480p, MovieLevel720pWeb, MovieLevel720p,
		MovieLevel720pRemux, MovieLevel1080pWeb, MovieLevel1080p}, MovieLevel1080p},
	{105, "1080p", []int{MovieLevel1080p}, MovieLevel1080p},
	{106, "1080p Remux", []int{MovieLevel1080pRemux}, MovieLevel1080pRemux},
	{107, "720p Best Effort", []int{MovieLevel480p, MovieLevel720pWeb, MovieLevel720p}, MovieLevel720p},
	{108, "720p", []int{MovieLevel720p}, MovieLevel720p},
	{109, "720p Remux", []int{MovieLevel720pRemux}, MovieLevel720pRemux},
	{110, "BR-DISK", []int{MovieLevelBRDisk}, MovieLevelBRDisk},
	{111, "Raw-HD", []int{MovieLevelRawHD}, MovieLevelRawHD},
	{112, "Unknown", []int{MovieLevelUnknown}, MovieLevelUnknown},
	{113, "HDTV", []int{MovieLevel480p}, MovieLevel480p},
}

// DefaultMovieProfiles returns the seeded movie quality profiles (ids in
// their own 100+ space, items referencing movie level ids).
func DefaultMovieProfiles() []QualityProfile {
	out := make([]QualityProfile, 0, len(defaultMovieProfileSpecs))
	for _, s := range defaultMovieProfileSpecs {
		prof := QualityProfile{ID: s.id, Name: s.name, Cutoff: s.cutoff}
		prof.Items = make([]QualityProfileItem, 0, len(s.approve))
		for _, q := range s.approve {
			prof.Items = append(prof.Items, QualityProfileItem{Quality: q, Approved: true})
		}
		out = append(out, prof)
	}
	return out
}

// MovieProfileNames returns the seeded movie profile names — the set an
// add-movie quality_profile parameter is validated against (R5a).
func MovieProfileNames() map[string]bool {
	out := map[string]bool{}
	for _, p := range DefaultMovieProfiles() {
		out[p.Name] = true
	}
	return out
}

// seedMovieLevelIDs returns the seeded movie level ids — the reference set
// movie profile items/cutoffs are validated against.
func seedMovieLevelIDs() map[int]bool {
	out := map[int]bool{}
	for _, l := range DefaultMovieLevels() {
		out[l.ID] = true
	}
	return out
}

// DelayProfile is a search delay per release source, in hours (Radarr's
// delay profiles; 0 = no delay).
type DelayProfile struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Webrip      int    `json:"webrip"`
	Bluray      int    `json:"bluray"`
	Remux       int    `json:"remux"`
	BrDisk      int    `json:"brdisk"`
	RawHD       int    `json:"rawhd"`
	HDTV        int    `json:"hdtv"`
}

// DefaultDelayProfiles returns the single empty default profile.
func DefaultDelayProfiles() []DelayProfile {
	return []DelayProfile{{ID: 1, Name: "Default"}}
}

// ReleaseProfile decides which release sources may be considered (Radarr's
// release profiles; every flag defaults to true).
type ReleaseProfile struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Webrip      bool   `json:"webrip"`
	Bluray      bool   `json:"bluray"`
	Remux       bool   `json:"remux"`
	BrDisk      bool   `json:"brdisk"`
	RawHD       bool   `json:"rawhd"`
	HDTV        bool   `json:"hdtv"`
}

// DefaultReleaseProfiles returns the single all-inclusive default profile.
func DefaultReleaseProfiles() []ReleaseProfile {
	return []ReleaseProfile{{
		ID: 1, Name: "Default",
		Webrip: true, Bluray: true, Remux: true, BrDisk: true, RawHD: true, HDTV: true,
	}}
}

// validateNamedIDs checks the id/name rules shared by every named list
// (id >= 1, unique id, non-empty unique name) and reports a prefixed message.
func validateNamedIDs(prefix string, n int, get func(int) (int, string)) string {
	ids := map[int]bool{}
	names := map[string]bool{}
	for i := 0; i < n; i++ {
		id, name := get(i)
		if id < 1 {
			return fmt.Sprintf("%s[%d]: id must be >= 1", prefix, i)
		}
		if ids[id] {
			return fmt.Sprintf("%s[%d]: duplicate id %d", prefix, i, id)
		}
		ids[id] = true
		if name == "" {
			return fmt.Sprintf("%s[%d]: name is required", prefix, i)
		}
		if names[name] {
			return fmt.Sprintf("%s[%d]: duplicate name %q", prefix, i, name)
		}
		names[name] = true
	}
	return ""
}
