package settings

// ---------------------------------------------------------------------------
// Lidarr L3: music quality definitions (the lossless quality model)
//
// Design decision (L3 brief): DATA, NOT TYPES. The music quality model is a
// seeded data table — QualityLevel rows and QualityProfile documents — not
// per-codec Go types. The release-MATCHING engine (domains/music/quality.go)
// is untouched; L3 adds the settings-side definitions the UI renders and
// add-artist validation checks against.
//
// ID space: TV levels are 1-10, movie levels 100-112 (R5a), music levels
// 200+ — so a music level id can never collide with a TV or movie level id.
// Music level id = 200 + Lidarr quality id (Lidarr Unknown(0) -> 200 ...
// ALAC 24bit(37) -> 237).
// ---------------------------------------------------------------------------

// Music quality level ids (200+ space). Lidarr id = id - 200.
const (
	MusicLevelUnknown  = 200 // Lidarr 0: Unknown
	MusicLevelMP3192   = 201 // Lidarr 1
	MusicLevelMP3VBRV0 = 202 // Lidarr 2
	MusicLevelMP3256   = 203 // Lidarr 3
	MusicLevelMP3320   = 204 // Lidarr 4
	MusicLevelMP3160   = 205 // Lidarr 5
	MusicLevelFLAC     = 206 // Lidarr 6
	MusicLevelALAC     = 207 // Lidarr 7
	MusicLevelMP3VBRV2 = 208 // Lidarr 8
	MusicLevelAAC192   = 209 // Lidarr 9
	MusicLevelAAC256   = 210 // Lidarr 10
	MusicLevelAAC320   = 211 // Lidarr 11
	MusicLevelAACVBR   = 212 // Lidarr 12
	MusicLevelWAV      = 213 // Lidarr 13
	MusicLevelOGGQ10   = 214 // Lidarr 14: OGG Vorbis Q10
	MusicLevelOGGQ9    = 215 // Lidarr 15
	MusicLevelOGGQ8    = 216 // Lidarr 16
	MusicLevelOGGQ7    = 217 // Lidarr 17
	MusicLevelOGGQ6    = 218 // Lidarr 18
	MusicLevelOGGQ5    = 219 // Lidarr 19
	MusicLevelWMA      = 220 // Lidarr 20
	MusicLevelFLAC24   = 221 // Lidarr 21: FLAC 24bit
	MusicLevelMP3128   = 222 // Lidarr 22
	MusicLevelMP396    = 223 // Lidarr 23
	MusicLevelMP380    = 224 // Lidarr 24
	MusicLevelMP364    = 225 // Lidarr 25
	MusicLevelMP356    = 226 // Lidarr 26
	MusicLevelMP348    = 227 // Lidarr 27
	MusicLevelMP340    = 228 // Lidarr 28
	MusicLevelMP332    = 229 // Lidarr 29
	MusicLevelMP324    = 230 // Lidarr 30
	MusicLevelMP316    = 231 // Lidarr 31
	MusicLevelMP38     = 232 // Lidarr 32
	MusicLevelMP3112   = 233 // Lidarr 33
	MusicLevelMP3224   = 234 // Lidarr 34
	MusicLevelAPE      = 235 // Lidarr 35
	MusicLevelWavPack  = 236 // Lidarr 36
	MusicLevelALAC24   = 237 // Lidarr 37: ALAC 24bit
)

// DefaultMusicLevels returns the full Lidarr music quality ladder (38 rows):
// the lossy + lossless set with per-level size limits (SizeLimitMB; 0 =
// "Unlimited" for every lossless row and WAV).
//
// Order: best-first, mirroring Lidarr's UI ordering — WAV, APE, WavPack,
// the lossless group [FLAC 24bit, ALAC 24bit, FLAC, ALAC], then the High /
// Mid / Low / Poor / Trash Quality Lossy groups, and Unknown last. Within a
// lossy group the rows are ordered by size limit (descending), then name
// (ascending), so the order is stable across runs.
//
// Source and Resolution are the TV/movie columns and have no audio
// equivalent: music rows carry "".
//
// (Lidarr also shows a "preferred size" column; it is deliberately NOT a
// QualityLevel field — SizeLimitMB is the max size and the preferred sizes
// are a display-only concern the UI leaf can hardcode if it renders them.)
func DefaultMusicLevels() []QualityLevel {
	return []QualityLevel{
		// Lossless (bit-exact) — best first. WAV has no group in Lidarr.
		{ID: MusicLevelWAV, Name: "WAV", Group: "", SizeLimitMB: 0},
		{ID: MusicLevelAPE, Name: "APE", Group: "Lossless", SizeLimitMB: 0},
		{ID: MusicLevelWavPack, Name: "WavPack", Group: "Lossless", SizeLimitMB: 0},
		{ID: MusicLevelFLAC24, Name: "FLAC 24bit", Group: "Lossless", SizeLimitMB: 0},
		{ID: MusicLevelALAC24, Name: "ALAC 24bit", Group: "Lossless", SizeLimitMB: 0},
		{ID: MusicLevelFLAC, Name: "FLAC", Group: "Lossless", SizeLimitMB: 0},
		{ID: MusicLevelALAC, Name: "ALAC", Group: "Lossless", SizeLimitMB: 0},
		// High Quality Lossy.
		{ID: MusicLevelOGGQ10, Name: "OGG Vorbis Q10", Group: "High Quality Lossy", SizeLimitMB: 550},
		{ID: MusicLevelAAC320, Name: "AAC-320", Group: "High Quality Lossy", SizeLimitMB: 350},
		{ID: MusicLevelAACVBR, Name: "AAC-VBR", Group: "High Quality Lossy", SizeLimitMB: 350},
		{ID: MusicLevelMP3320, Name: "MP3-320", Group: "High Quality Lossy", SizeLimitMB: 350},
		{ID: MusicLevelMP3VBRV0, Name: "MP3-VBR-V0", Group: "High Quality Lossy", SizeLimitMB: 350},
		{ID: MusicLevelOGGQ9, Name: "OGG Vorbis Q9", Group: "High Quality Lossy", SizeLimitMB: 350},
		// Mid Quality Lossy.
		{ID: MusicLevelAAC256, Name: "AAC-256", Group: "Mid Quality Lossy", SizeLimitMB: 280},
		{ID: MusicLevelMP3256, Name: "MP3-256", Group: "Mid Quality Lossy", SizeLimitMB: 280},
		{ID: MusicLevelMP3VBRV2, Name: "MP3-VBR-V2", Group: "Mid Quality Lossy", SizeLimitMB: 280},
		{ID: MusicLevelOGGQ8, Name: "OGG Vorbis Q8", Group: "Mid Quality Lossy", SizeLimitMB: 280},
		{ID: MusicLevelOGGQ7, Name: "OGG Vorbis Q7", Group: "Mid Quality Lossy", SizeLimitMB: 245},
		// Low Quality Lossy.
		{ID: MusicLevelWMA, Name: "WMA", Group: "Low Quality Lossy", SizeLimitMB: 350},
		{ID: MusicLevelMP3224, Name: "MP3-224", Group: "Low Quality Lossy", SizeLimitMB: 245},
		{ID: MusicLevelAAC192, Name: "AAC-192", Group: "Low Quality Lossy", SizeLimitMB: 210},
		{ID: MusicLevelMP3192, Name: "MP3-192", Group: "Low Quality Lossy", SizeLimitMB: 210},
		{ID: MusicLevelOGGQ6, Name: "OGG Vorbis Q6", Group: "Low Quality Lossy", SizeLimitMB: 210},
		// Poor Quality Lossy.
		{ID: MusicLevelMP3160, Name: "MP3-160", Group: "Poor Quality Lossy", SizeLimitMB: 175},
		{ID: MusicLevelOGGQ5, Name: "OGG Vorbis Q5", Group: "Poor Quality Lossy", SizeLimitMB: 175},
		{ID: MusicLevelMP3128, Name: "MP3-128", Group: "Poor Quality Lossy", SizeLimitMB: 140},
		{ID: MusicLevelMP3112, Name: "MP3-112", Group: "Poor Quality Lossy", SizeLimitMB: 125},
		{ID: MusicLevelMP396, Name: "MP3-96", Group: "Poor Quality Lossy", SizeLimitMB: 110},
		// Trash Quality Lossy.
		{ID: MusicLevelMP380, Name: "MP3-80", Group: "Trash Quality Lossy", SizeLimitMB: 95},
		{ID: MusicLevelMP364, Name: "MP3-64", Group: "Trash Quality Lossy", SizeLimitMB: 75},
		{ID: MusicLevelMP356, Name: "MP3-56", Group: "Trash Quality Lossy", SizeLimitMB: 65},
		{ID: MusicLevelMP348, Name: "MP3-48", Group: "Trash Quality Lossy", SizeLimitMB: 55},
		{ID: MusicLevelMP340, Name: "MP3-40", Group: "Trash Quality Lossy", SizeLimitMB: 45},
		{ID: MusicLevelMP332, Name: "MP3-32", Group: "Trash Quality Lossy", SizeLimitMB: 40},
		{ID: MusicLevelMP324, Name: "MP3-24", Group: "Trash Quality Lossy", SizeLimitMB: 30},
		{ID: MusicLevelMP316, Name: "MP3-16", Group: "Trash Quality Lossy", SizeLimitMB: 20},
		{ID: MusicLevelMP38, Name: "MP3-8", Group: "Trash Quality Lossy", SizeLimitMB: 10},
		// Unknown last.
		{ID: MusicLevelUnknown, Name: "Unknown", Group: "", SizeLimitMB: 350},
	}
}

// DefaultMusicQualityProfile is the Lidarr factory default music quality
// profile name; an empty quality profile on an add-artist resolves to it
// (L3).
const DefaultMusicQualityProfile = "Lossless Best Effort"

// musicProfileSpec is one seeded music profile: id (the 200+ music space),
// name, the approved music level ids, and the cutoff level id.
type musicProfileSpec struct {
	id      int
	name    string
	approve []int
	cutoff  int
}

// defaultMusicProfileSpecs are the 4 Lidarr default music quality profiles.
// Best-effort profiles approve everything up to and including their tier;
// the others approve only their own tier. Cutoffs sit at the profile's
// highest approved level of its top tier (the TV/movie convention) — for
// Lossless Best Effort that is FLAC 24bit, the highest lossless level (the
// high-quality lossy items are approved, but the cutoff stays lossless);
// for High Quality Lossy it is OGG Vorbis Q10, the group's highest.
var defaultMusicProfileSpecs = []musicProfileSpec{
	{200, "Lossless Best Effort", []int{
		MusicLevelWAV, MusicLevelAPE, MusicLevelWavPack,
		MusicLevelFLAC24, MusicLevelALAC24, MusicLevelFLAC, MusicLevelALAC,
		MusicLevelOGGQ10, MusicLevelAAC320, MusicLevelAACVBR,
		MusicLevelMP3320, MusicLevelMP3VBRV0, MusicLevelOGGQ9,
	}, MusicLevelFLAC24},
	{201, "High Quality Lossy", []int{
		MusicLevelOGGQ10, MusicLevelAAC320, MusicLevelAACVBR,
		MusicLevelMP3320, MusicLevelMP3VBRV0, MusicLevelOGGQ9,
	}, MusicLevelOGGQ10},
	{202, "Lossless", []int{
		MusicLevelWAV, MusicLevelAPE, MusicLevelWavPack,
		MusicLevelFLAC24, MusicLevelALAC24, MusicLevelFLAC, MusicLevelALAC,
	}, MusicLevelFLAC24},
	{203, "WAV", []int{MusicLevelWAV}, MusicLevelWAV},
}

// DefaultMusicProfiles returns the seeded music quality profiles (ids in the
// 200+ music space, items referencing music level ids). The Lidarr factory
// default, Lossless Best Effort, is first — the same convention as the
// seeded movie profiles.
func DefaultMusicProfiles() []QualityProfile {
	out := make([]QualityProfile, 0, len(defaultMusicProfileSpecs))
	for _, s := range defaultMusicProfileSpecs {
		prof := QualityProfile{ID: s.id, Name: s.name, Cutoff: s.cutoff}
		prof.Items = make([]QualityProfileItem, 0, len(s.approve))
		for _, q := range s.approve {
			prof.Items = append(prof.Items, QualityProfileItem{Quality: q, Approved: true})
		}
		out = append(out, prof)
	}
	return out
}

// MusicProfileNames returns the seeded music profile names — the set an
// add-artist quality_profile parameter (and a root folder's default quality
// profile) is validated against (L3).
func MusicProfileNames() map[string]bool {
	out := map[string]bool{}
	for _, p := range DefaultMusicProfiles() {
		out[p.Name] = true
	}
	return out
}

// seedMusicLevelIDs returns the seeded music level ids — the reference set
// music profile items/cutoffs are validated against.
func seedMusicLevelIDs() map[int]bool {
	out := map[int]bool{}
	for _, l := range DefaultMusicLevels() {
		out[l.ID] = true
	}
	return out
}

// ---------------------------------------------------------------------------
// Lidarr L3: metadata profiles (Standard / Album / Studio)
// ---------------------------------------------------------------------------

// MetadataProfile is one named set of metadata-file kinds (Lidarr's metadata
// profiles). It is deliberately lightweight and data-driven: a profile is
// just id + name + description + the item list the UI renders (read-mostly).
type MetadataProfile struct {
	ID          int      `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Items       []string `json:"items"`
}

// MetadataProfileItemVocabulary is the full set of metadata-file kinds a
// profile may name (Lidarr's consumer vocabulary, kept as stable strings).
// The seeded profiles below use a subset of it.
var MetadataProfileItemVocabulary = []string{
	"Embedded", "Music XML", "Music Image", "Album TXT", "Artist TXT", "M4A Tags", "CUE Sheet",
}

// DefaultMetadataProfiles returns the three seeded Lidarr metadata profiles.
// Seeded item lists (documented per the L3 brief):
//
//	Standard -> [Embedded, Music XML, Music Image]  (the Lidarr standard set)
//	Album    -> [Embedded, Album TXT]              (album-scoped files)
//	Studio   -> [Embedded, Artist TXT]             (artist/label-scoped files)
func DefaultMetadataProfiles() []MetadataProfile {
	return []MetadataProfile{
		{ID: 1, Name: "Standard",
			Description: "The standard metadata set: embedded tags, Music XML and cover art.",
			Items:       []string{"Embedded", "Music XML", "Music Image"}},
		{ID: 2, Name: "Album",
			Description: "Album-scoped metadata: embedded tags plus the album notes file.",
			Items:       []string{"Embedded", "Album TXT"}},
		{ID: 3, Name: "Studio",
			Description: "Artist/label-scoped metadata: embedded tags plus the artist notes file.",
			Items:       []string{"Embedded", "Artist TXT"}},
	}
}

// MetadataProfileNames returns the seeded metadata profile names — the set a
// root folder's default metadata profile is validated against (L3).
func MetadataProfileNames() map[string]bool {
	out := map[string]bool{}
	for _, p := range DefaultMetadataProfiles() {
		out[p.Name] = true
	}
	return out
}
