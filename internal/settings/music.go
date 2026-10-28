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
