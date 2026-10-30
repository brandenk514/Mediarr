package settings

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Section names one of the eleven settings-section documents served by the
// generic per-section endpoints (P5 settings parity). The settings shell's
// 13 sections are these eleven plus indexers and download-clients, which keep
// their dedicated endpoints (the indexer CRUD + test surface and the
// download-client config / per-library overrides).
//
// A section document is a JSON value stored in the settings key/value table
// under the key "section.<slug>". Get returns the stored document merged over
// the section defaults (a fresh install reads complete documents); Put is a
// full-document replace. Secrets carried by a document (connect tokens,
// import-list API keys) are masked at the API boundary — the stored document
// keeps the plaintext secret and the read shape only carries *_set booleans.
type Section string

const (
	SectionGeneral         Section = "general"
	SectionUI              Section = "ui"
	SectionQuality         Section = "quality"
	SectionProfiles        Section = "profiles"
	SectionTags            Section = "tags"
	SectionMediaManagement Section = "media-management"
	SectionCustomFormats   Section = "custom-formats"
	SectionConnect         Section = "connect"
	SectionMetadata        Section = "metadata"
	SectionMetadataSource  Section = "metadata-source"
	SectionImportLists     Section = "import-lists"
)

// AllSections is the fixed, ordered set of generic section slugs (the order
// the UI renders its nav, matching the shell).
var AllSections = []Section{
	SectionGeneral,
	SectionUI,
	SectionQuality,
	SectionProfiles,
	SectionTags,
	SectionMediaManagement,
	SectionCustomFormats,
	SectionConnect,
	SectionMetadata,
	SectionMetadataSource,
	SectionImportLists,
}

// ParseSection reports whether slug is a generic section slug. indexers and
// download-clients are deliberately NOT generic slugs — they have dedicated
// endpoints and must 404/400 on the generic route, not be swallowed.
func ParseSection(slug string) (Section, bool) {
	s := Section(slug)
	for _, v := range AllSections {
		if v == s {
			return s, true
		}
	}
	return "", false
}

// Key is the settings-table storage key for a section document.
func (s Section) Key() string { return "section." + string(s) }

// ---------------------------------------------------------------------------
// general
// ---------------------------------------------------------------------------

// GeneralSettings is the app-level document. It is reconciled with the
// env-based boot config: host/port/base_url/ssl are persisted for display and
// for the next boot (boot config still wins at startup), and auth flags
// likewise take effect at the next boot.
type GeneralSettings struct {
	Host        string `json:"host"`
	Port        int    `json:"port"`
	BaseURL     string `json:"base_url"`
	SSL         bool   `json:"ssl"`
	AuthEnabled bool   `json:"auth_enabled"`
	AuthPinning bool   `json:"auth_pinning"`
	// Prowlarr PR4 additions. These are savable now; log_level and http_proxy
	// take effect at the next boot (a restart actually applying them is out of
	// scope this phase). AllowedHosts is a comma-separated host allow-list
	// ("" = allow all). CertValidation disables TLS certificate validation
	// when false. Analytics is the analytics opt-out flag (false = analytics
	// OFF, Prowlarr's default).
	AllowedHosts   string `json:"allowed_hosts"`
	CertValidation bool   `json:"cert_validation"`
	HTTPProxy      string `json:"http_proxy"`
	LogLevel       string `json:"log_level"`
	Analytics      bool   `json:"analytics"`
}

// GeneralLogLevels enumerates the valid log_level values (PR4).
var GeneralLogLevels = []string{"debug", "info", "warn", "error"}

func DefaultGeneral() GeneralSettings {
	return GeneralSettings{
		Host:           "0.0.0.0",
		Port:           8080,
		BaseURL:        "http://localhost:8080",
		AuthEnabled:    true,
		AuthPinning:    true,
		AllowedHosts:   "",
		CertValidation: true,
		HTTPProxy:      "",
		LogLevel:       "info",
		Analytics:      false,
	}
}

func (g GeneralSettings) Validate() string {
	if g.Port < 1 || g.Port > 65535 {
		return "port must be between 1 and 65535"
	}
	if len(g.Host) > 64 {
		return "host is too long"
	}
	if g.BaseURL != "" && !isHTTPURLValue(g.BaseURL) {
		return "base_url must be an http/https URL"
	}
	if !containsString(GeneralLogLevels, g.LogLevel) {
		return "log_level must be one of " + strings.Join(GeneralLogLevels, ", ")
	}
	return ""
}

// ---------------------------------------------------------------------------
// ui
// ---------------------------------------------------------------------------

// UISettings holds the UI options (default landing view + date/time format +
// list density).
type UISettings struct {
	DefaultView  string `json:"default_view"`
	DateFormat   string `json:"date_format"`
	TimeFormat   string `json:"time_format"`
	ItemsPerPage int    `json:"items_per_page"`
	// Lidarr L3: the artist/album detail-page expand toggles. Albums ship
	// expanded (matching the L1 detail page's album-group behavior); the
	// rest start collapsed.
	ExpandAlbums    bool `json:"expand_albums"`
	ExpandEPs       bool `json:"expand_eps"`
	ExpandSingles   bool `json:"expand_singles"`
	ExpandBroadcast bool `json:"expand_broadcast"`
	ExpandOther     bool `json:"expand_other"`
	// Theme is the UI theme (auto|dark|light) and ColorImpaired boosts
	// contrast for color-impaired users (L3).
	Theme         string `json:"theme"`
	ColorImpaired bool   `json:"color_impaired"`
	// Language is the UI language (PR4): a BCP-47 tag, defaulting to "en".
	Language string `json:"language"`
}

// UIDefaultsView enumerates the valid default_view values.
var UIViews = []string{"dashboard", "calendar", "queue", "wanted", "series", "movies", "artists", "authors"}

// UIThemes enumerates the valid theme values (L3).
var UIThemes = []string{"auto", "dark", "light"}

func DefaultUI() UISettings {
	return UISettings{
		DefaultView:  "dashboard",
		DateFormat:   "YYYY-MM-DD",
		TimeFormat:   "24h",
		ItemsPerPage: 50,
		ExpandAlbums: true,
		Theme:        "auto",
		Language:     "en",
	}
}

func (u UISettings) Validate() string {
	if !containsString(UIViews, u.DefaultView) {
		return "default_view must be one of " + strings.Join(UIViews, ", ")
	}
	if u.TimeFormat != "24h" && u.TimeFormat != "12h" {
		return "time_format must be 24h or 12h"
	}
	if u.DateFormat == "" || len(u.DateFormat) > 32 {
		return "date_format is required (max 32 chars)"
	}
	if u.ItemsPerPage < 1 || u.ItemsPerPage > 500 {
		return "items_per_page must be between 1 and 500"
	}
	if !containsString(UIThemes, u.Theme) {
		return "theme must be one of " + strings.Join(UIThemes, ", ")
	}
	if u.Language == "" || len(u.Language) > 16 {
		return "language is required (max 16 chars, BCP-47 tag)"
	}
	return ""
}

// ---------------------------------------------------------------------------
// quality
// ---------------------------------------------------------------------------

// QualityLevel is one row of the quality matrix (id is a stable reference the
// quality profiles use).
type QualityLevel struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	// Group is the level's quality category (Lidarr L3 "Title" column): the
	// lossy/lossless group for music levels ("Lossless", "High Quality
	// Lossy", ...); "" for TV and movie rows, where the category concept does
	// not apply.
	Group       string `json:"group"`
	Source      string `json:"source"`
	Resolution  string `json:"resolution"`
	SizeLimitMB int    `json:"size_limit_mb"`
}

// QualitySettings is the quality-levels document. Levels is the TV matrix
// (P5, ids 1-10); MovieLevels is the additive Radarr movie set (R5a, ids
// 100+); MusicLevels is the additive Lidarr music set (L3, ids 200+), each
// in its own id space.
type QualitySettings struct {
	Levels      []QualityLevel `json:"levels"`
	MovieLevels []QualityLevel `json:"movie_levels"`
	MusicLevels []QualityLevel `json:"music_levels"`
}

// DefaultQuality returns the TV-parity default matrix (Sonarr's standard
// levels, with size limits in MB; 0 = unlimited) plus the seeded Radarr
// movie levels (R5a) and the seeded Lidarr music levels (L3).
func DefaultQuality() QualitySettings {
	return QualitySettings{
		Levels: []QualityLevel{
			{ID: 1, Name: "SD", Source: "dvd", Resolution: "480p", SizeLimitMB: 1024},
			{ID: 2, Name: "720p WEB", Source: "webrip", Resolution: "720p", SizeLimitMB: 1536},
			{ID: 3, Name: "720p", Source: "blu-ray", Resolution: "720p", SizeLimitMB: 2560},
			{ID: 4, Name: "720p REMUX", Source: "remux", Resolution: "720p", SizeLimitMB: 7168},
			{ID: 5, Name: "1080p WEB", Source: "webrip", Resolution: "1080p", SizeLimitMB: 1536},
			{ID: 6, Name: "1080p", Source: "blu-ray", Resolution: "1080p", SizeLimitMB: 4096},
			{ID: 7, Name: "1080p REMUX", Source: "remux", Resolution: "1080p", SizeLimitMB: 11264},
			{ID: 8, Name: "2160p WEB", Source: "webrip", Resolution: "2160p", SizeLimitMB: 5120},
			{ID: 9, Name: "2160p", Source: "blu-ray", Resolution: "2160p", SizeLimitMB: 8192},
			{ID: 10, Name: "2160p REMUX", Source: "remux", Resolution: "2160p", SizeLimitMB: 20480},
		},
		MovieLevels: DefaultMovieLevels(),
		MusicLevels: DefaultMusicLevels(),
	}
}

// validateQualityLevels applies the per-level rules of a quality array and
// returns a prefixed error message ("" when valid).
func validateQualityLevels(prefix string, levels []QualityLevel) string {
	seen := map[int]bool{}
	for i, l := range levels {
		if l.ID < 1 {
			return fmt.Sprintf("%s[%d]: id must be >= 1", prefix, i)
		}
		if seen[l.ID] {
			return fmt.Sprintf("%s[%d]: duplicate id %d", prefix, i, l.ID)
		}
		seen[l.ID] = true
		if l.Name == "" {
			return fmt.Sprintf("%s[%d]: name is required", prefix, i)
		}
		if l.SizeLimitMB < 0 {
			return fmt.Sprintf("%s[%d]: size_limit_mb must be >= 0", prefix, i)
		}
	}
	return ""
}

func (q QualitySettings) Validate() string {
	if msg := validateQualityLevels("levels", q.Levels); msg != "" {
		return msg
	}
	// Movie levels: the same rules, scoped to their own array (R5a).
	if msg := validateQualityLevels("movie_levels", q.MovieLevels); msg != "" {
		return msg
	}
	// Music levels: the same rules, scoped to their own array (L3).
	return validateQualityLevels("music_levels", q.MusicLevels)
}

// ---------------------------------------------------------------------------
// profiles
// ---------------------------------------------------------------------------

// QualityProfileItem is one approved/not-approved quality in a profile.
type QualityProfileItem struct {
	Quality  int  `json:"quality"`
	Approved bool `json:"approved"`
}

// QualityProfile is a named quality profile with an approval set and a
// cutoff level (re-download trigger). Used by both the TV profiles and the
// movie profiles (R5a), whose items/cutoff reference the respective level id
// spaces.
type QualityProfile struct {
	ID     int                  `json:"id"`
	Name   string               `json:"name"`
	Items  []QualityProfileItem `json:"items"`
	Cutoff int                  `json:"cutoff"`
}

// ProfilesSettings is the quality-profiles document. Profiles is the TV
// document (P5); the Radarr movie/delay/release arrays are additive (R5a);
// the music + metadata arrays are the additive Lidarr set (L3).
type ProfilesSettings struct {
	Profiles         []QualityProfile  `json:"profiles"`
	MovieProfiles    []QualityProfile  `json:"movie_profiles"`
	MusicProfiles    []QualityProfile  `json:"music_profiles"`
	MetadataProfiles []MetadataProfile `json:"metadata_profiles"`
	DelayProfiles    []DelayProfile    `json:"delay_profiles"`
	ReleaseProfiles  []ReleaseProfile  `json:"release_profiles"`
}

// DefaultProfiles returns the single default TV profile (every TV level
// approved, cutoff at the highest TV level) plus the seeded movie documents:
// the 14 Radarr v6 movie profiles, one empty delay profile, one all-inclusive
// release profile (R5a), and the seeded Lidarr documents: the 4 default
// music profiles and the 3 metadata profiles (L3).
func DefaultProfiles() ProfilesSettings {
	levels := DefaultQuality().Levels
	prof := QualityProfile{ID: 1, Name: "Default", Cutoff: levels[len(levels)-1].ID}
	prof.Items = make([]QualityProfileItem, 0, len(levels))
	for _, l := range levels {
		prof.Items = append(prof.Items, QualityProfileItem{Quality: l.ID, Approved: true})
	}
	return ProfilesSettings{
		Profiles:         []QualityProfile{prof},
		MovieProfiles:    DefaultMovieProfiles(),
		MusicProfiles:    DefaultMusicProfiles(),
		MetadataProfiles: DefaultMetadataProfiles(),
		DelayProfiles:    DefaultDelayProfiles(),
		ReleaseProfiles:  DefaultReleaseProfiles(),
	}
}

func (p ProfilesSettings) Validate() string {
	names := map[string]bool{}
	for i, prof := range p.Profiles {
		if prof.ID < 1 {
			return fmt.Sprintf("profiles[%d]: id must be >= 1", i)
		}
		if prof.Name == "" {
			return fmt.Sprintf("profiles[%d]: name is required", i)
		}
		if names[prof.Name] {
			return fmt.Sprintf("profiles[%d]: duplicate name %q", i, prof.Name)
		}
		names[prof.Name] = true
		if prof.Cutoff < 1 {
			return fmt.Sprintf("profiles[%d]: cutoff must reference a quality level id >= 1", i)
		}
		if len(prof.Items) == 0 {
			return fmt.Sprintf("profiles[%d]: at least one quality item is required", i)
		}
		seen := map[int]bool{}
		for j, it := range prof.Items {
			if it.Quality < 1 {
				return fmt.Sprintf("profiles[%d].items[%d]: quality must be a level id >= 1", i, j)
			}
			if seen[it.Quality] {
				return fmt.Sprintf("profiles[%d].items[%d]: duplicate quality %d", i, j, it.Quality)
			}
			seen[it.Quality] = true
		}
	}

	// Radarr R5a: movie profiles — duplicate ids/names within the array,
	// items/cutoff must reference the seeded movie level ids (movie space).
	validMovieLevels := seedMovieLevelIDs()
	movieIDs := map[int]bool{}
	movieNames := map[string]bool{}
	for i, prof := range p.MovieProfiles {
		if prof.ID < 1 {
			return fmt.Sprintf("movie_profiles[%d]: id must be >= 1", i)
		}
		if movieIDs[prof.ID] {
			return fmt.Sprintf("movie_profiles[%d]: duplicate id %d", i, prof.ID)
		}
		movieIDs[prof.ID] = true
		if prof.Name == "" {
			return fmt.Sprintf("movie_profiles[%d]: name is required", i)
		}
		if movieNames[prof.Name] {
			return fmt.Sprintf("movie_profiles[%d]: duplicate name %q", i, prof.Name)
		}
		movieNames[prof.Name] = true
		if !validMovieLevels[prof.Cutoff] {
			return fmt.Sprintf("movie_profiles[%d]: cutoff must reference a movie quality level id", i)
		}
		if len(prof.Items) == 0 {
			return fmt.Sprintf("movie_profiles[%d]: at least one quality item is required", i)
		}
		seen := map[int]bool{}
		for j, it := range prof.Items {
			if !validMovieLevels[it.Quality] {
				return fmt.Sprintf("movie_profiles[%d].items[%d]: unknown movie quality level id %d", i, j, it.Quality)
			}
			if seen[it.Quality] {
				return fmt.Sprintf("movie_profiles[%d].items[%d]: duplicate quality %d", i, j, it.Quality)
			}
			seen[it.Quality] = true
		}
	}

	// Lidarr L3: music profiles — same rules as the movie profiles, but
	// items/cutoff must reference the seeded MUSIC level ids (music space),
	// and ids/names are unique within the music array.
	validMusicLevels := seedMusicLevelIDs()
	musicIDs := map[int]bool{}
	musicNames := map[string]bool{}
	for i, prof := range p.MusicProfiles {
		if prof.ID < 1 {
			return fmt.Sprintf("music_profiles[%d]: id must be >= 1", i)
		}
		if musicIDs[prof.ID] {
			return fmt.Sprintf("music_profiles[%d]: duplicate id %d", i, prof.ID)
		}
		musicIDs[prof.ID] = true
		if prof.Name == "" {
			return fmt.Sprintf("music_profiles[%d]: name is required", i)
		}
		if musicNames[prof.Name] {
			return fmt.Sprintf("music_profiles[%d]: duplicate name %q", i, prof.Name)
		}
		musicNames[prof.Name] = true
		if !validMusicLevels[prof.Cutoff] {
			return fmt.Sprintf("music_profiles[%d]: cutoff must reference a music quality level id", i)
		}
		if len(prof.Items) == 0 {
			return fmt.Sprintf("music_profiles[%d]: at least one quality item is required", i)
		}
		seen := map[int]bool{}
		for j, it := range prof.Items {
			if !validMusicLevels[it.Quality] {
				return fmt.Sprintf("music_profiles[%d].items[%d]: unknown music quality level id %d", i, j, it.Quality)
			}
			if seen[it.Quality] {
				return fmt.Sprintf("music_profiles[%d].items[%d]: duplicate quality %d", i, j, it.Quality)
			}
			seen[it.Quality] = true
		}
	}

	// Lidarr L3: metadata profiles — id/name rules via the shared helper
	// (item lists are free-form strings the UI renders read-mostly).
	if msg := validateNamedIDs("metadata_profiles", len(p.MetadataProfiles), func(i int) (int, string) {
		return p.MetadataProfiles[i].ID, p.MetadataProfiles[i].Name
	}); msg != "" {
		return msg
	}

	// Delay/release profiles: id/name rules per array (booleans need none).
	if msg := validateNamedIDs("delay_profiles", len(p.DelayProfiles), func(i int) (int, string) {
		return p.DelayProfiles[i].ID, p.DelayProfiles[i].Name
	}); msg != "" {
		return msg
	}
	for i, d := range p.DelayProfiles {
		if d.Webrip < 0 || d.Bluray < 0 || d.Remux < 0 || d.BrDisk < 0 || d.RawHD < 0 || d.HDTV < 0 {
			return fmt.Sprintf("delay_profiles[%d]: hours must be >= 0", i)
		}
	}
	return validateNamedIDs("release_profiles", len(p.ReleaseProfiles), func(i int) (int, string) {
		return p.ReleaseProfiles[i].ID, p.ReleaseProfiles[i].Name
	})
}

// ---------------------------------------------------------------------------
// tags
// ---------------------------------------------------------------------------

// TagInfo is one tag (label + free-text description). TV series are the only
// tagged entity today; the tag label is what is stored in tv_series.tags.
type TagInfo struct {
	ID          int    `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

// TagsSettings is the tags document. Defaults to an empty (non-nil) list.
type TagsSettings struct {
	Tags []TagInfo `json:"tags"`
}

func DefaultTags() TagsSettings { return TagsSettings{Tags: []TagInfo{}} }

func (t TagsSettings) Validate() string {
	ids := map[int]bool{}
	labels := map[string]bool{}
	for i, tag := range t.Tags {
		if tag.ID < 1 {
			return fmt.Sprintf("tags[%d]: id must be >= 1", i)
		}
		if ids[tag.ID] {
			return fmt.Sprintf("tags[%d]: duplicate id %d", i, tag.ID)
		}
		ids[tag.ID] = true
		if tag.Label == "" {
			return fmt.Sprintf("tags[%d]: label is required", i)
		}
		if labels[tag.Label] {
			return fmt.Sprintf("tags[%d]: duplicate label %q", i, tag.Label)
		}
		labels[tag.Label] = true
	}
	return ""
}

// ---------------------------------------------------------------------------
// media management
// ---------------------------------------------------------------------------

// NamingTemplate is the per-kind naming templates (TV parity first — the
// episode/series templates use Sonarr's {token} syntax).
type NamingTemplate struct {
	Series  string `json:"series"`
	Episode string `json:"episode"`
	Movie   string `json:"movie"`
	Album   string `json:"album"`
	Track   string `json:"track"`
	Author  string `json:"author"`
	Title   string `json:"title"`
}

// FileManagement is the file-management options block.
type FileManagement struct {
	MinFileAgeHours  int  `json:"min_file_age_hours"`
	RemoveEmptyDirs  bool `json:"remove_empty_dirs"`
	ImportExtraFiles bool `json:"import_extra_files"`
	RenameSeries     bool `json:"rename_series"`
	// UnmonitorDeletedMovies (Radarr R5a, "Unmonitor Deleted Movies"): unmonitor
	// a movie when its files are deleted.
	UnmonitorDeletedMovies bool `json:"unmonitor_deleted_movies"`
}

// MovieNaming is the Radarr movie naming block (R5a): rename +
// illegal-character/colon options and the movie's standard format template,
// in the same brace-token style as the rest of the naming document (it
// renders, for example, to "Title (2010) Bluray-1080p Proper").
type MovieNaming struct {
	RenameMovies             bool   `json:"rename_movies"`
	ReplaceIllegalCharacters bool   `json:"replace_illegal_characters"`
	ColonReplacement         string `json:"colon_replacement"`
	StandardFormat           string `json:"standard_format"`
}

// DefaultMovieStandardFormat is the Radarr standard movie naming template
// (R5a): movie title, year, quality and edition tokens.
const DefaultMovieStandardFormat = "{movie-title} ({year}) {quality} {edition}"

// MusicNaming is the Lidarr music naming block (L3): rename +
// illegal-character/colon options and the track's standard/multi-disc format
// templates, in the same brace-token style as the rest of the naming document
// (it renders, for example, to "Artist - 07 - Track Title").
type MusicNaming struct {
	RenameTracks             bool   `json:"rename_tracks"`
	ReplaceIllegalCharacters bool   `json:"replace_illegal_characters"`
	ColonReplacement         string `json:"colon_replacement"`
	StandardFormat           string `json:"standard_format"`
	MultiDiscFormat          string `json:"multi_disc_format"`
}

// DefaultMusicStandardFormat / DefaultMusicMultiDiscFormat are the Lidarr
// default track naming templates (L3) — the brace tokens are exactly
// Lidarr's conventions.
const (
	DefaultMusicStandardFormat  = "{artist} - {track:0} - {track-title}"
	DefaultMusicMultiDiscFormat = "{artist} - {album} - CD {disc:0} - {track:0} - {track-title}"
)

// RemotePathMapping maps a source path (on the download client) to a
// destination path (on the Mediarr host) for the import step.
type RemotePathMapping struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
}

// MediaManagementSettings is the media-management document. Root folders are
// NOT part of this document — they live in the root-folders registry
// (P2 add-flow domain) and the section's UI reads/writes that registry.
type MediaManagementSettings struct {
	Naming             NamingTemplate      `json:"naming"`
	MovieNaming        MovieNaming         `json:"movie_naming"`
	MusicNaming        MusicNaming         `json:"music_naming"`
	Management         FileManagement      `json:"file_management"`
	RemotePathMappings []RemotePathMapping `json:"remote_path_mappings"`
}

func DefaultMediaManagement() MediaManagementSettings {
	return MediaManagementSettings{
		Naming: NamingTemplate{
			Series:  "{series-title} ({year})",
			Episode: "{series-title} S{season:00}E{episode:00} - {episode-title}",
			Movie:   "{movie-title} ({year})",
			Album:   "{artist} - {album}",
			Track:   "{disc:0} - {track:0} - {track-title}",
			Author:  "{author}",
			Title:   "{title}",
		},
		MovieNaming: MovieNaming{
			RenameMovies:             true,
			ReplaceIllegalCharacters: true,
			ColonReplacement:         "",
			StandardFormat:           DefaultMovieStandardFormat,
		},
		MusicNaming: MusicNaming{
			RenameTracks:             true,
			ReplaceIllegalCharacters: true,
			ColonReplacement:         "",
			StandardFormat:           DefaultMusicStandardFormat,
			MultiDiscFormat:          DefaultMusicMultiDiscFormat,
		},
		Management: FileManagement{
			MinFileAgeHours:        0,
			RemoveEmptyDirs:        true,
			ImportExtraFiles:       true,
			RenameSeries:           true,
			UnmonitorDeletedMovies: true,
		},
		RemotePathMappings: []RemotePathMapping{},
	}
}

func (m MediaManagementSettings) Validate() string {
	for name, tpl := range map[string]string{
		"series": m.Naming.Series, "episode": m.Naming.Episode, "movie": m.Naming.Movie,
		"album": m.Naming.Album, "track": m.Naming.Track, "author": m.Naming.Author,
		"title": m.Naming.Title,
	} {
		if tpl == "" {
			return "naming." + name + " is required"
		}
	}
	// Radarr R5a movie naming: the standard format must be set and the colon
	// replacement is a single character (or empty).
	if m.MovieNaming.StandardFormat == "" {
		return "movie_naming.standard_format is required"
	}
	if utf8.RuneCountInString(m.MovieNaming.ColonReplacement) > 1 {
		return "movie_naming.colon_replacement must be a single character or empty"
	}
	// Lidarr L3 music naming: both format templates must be set and the
	// colon replacement is a single character (or empty).
	if m.MusicNaming.StandardFormat == "" {
		return "music_naming.standard_format is required"
	}
	if m.MusicNaming.MultiDiscFormat == "" {
		return "music_naming.multi_disc_format is required"
	}
	if utf8.RuneCountInString(m.MusicNaming.ColonReplacement) > 1 {
		return "music_naming.colon_replacement must be a single character or empty"
	}
	if m.Management.MinFileAgeHours < 0 {
		return "file_management.min_file_age_hours must be >= 0"
	}
	srcs := map[string]bool{}
	for i, mp := range m.RemotePathMappings {
		if mp.Source == "" || mp.Destination == "" {
			return fmt.Sprintf("remote_path_mappings[%d]: source and destination are required", i)
		}
		if srcs[mp.Source] {
			return fmt.Sprintf("remote_path_mappings[%d]: duplicate source %q", i, mp.Source)
		}
		srcs[mp.Source] = true
	}
	return ""
}

// ---------------------------------------------------------------------------
// custom formats
// ---------------------------------------------------------------------------

// CFRule is one custom-format rule. A format matches when ALL of its rules
// match (Sonarr CF engine parity), and contributes its score when it matches.
type CFRule struct {
	Field    string `json:"field"`
	Operator string `json:"operator"`
	Value    string `json:"value"`
}

// CFFields / CFOperators are the rule vocabulary.
var (
	CFFields    = []string{"title", "quality", "source", "kind", "tag", "size_mb"}
	CFOperators = []string{"contains", "not_contains", "equals", "regex", "exists", "gt", "lt"}
)

// CustomFormat is one named, scored, enabled-able rule group.
type CustomFormat struct {
	ID      int      `json:"id"`
	Name    string   `json:"name"`
	Enabled bool     `json:"enabled"`
	Score   int      `json:"score"`
	Rules   []CFRule `json:"rules"`
}

// CustomFormatsSettings is the custom-formats document.
type CustomFormatsSettings struct {
	Formats []CustomFormat `json:"formats"`
}

func DefaultCustomFormats() CustomFormatsSettings {
	return CustomFormatsSettings{Formats: []CustomFormat{}}
}

func (c CustomFormatsSettings) Validate() string {
	ids := map[int]bool{}
	for i, f := range c.Formats {
		if f.ID < 1 {
			return fmt.Sprintf("formats[%d]: id must be >= 1", i)
		}
		if ids[f.ID] {
			return fmt.Sprintf("formats[%d]: duplicate id %d", i, f.ID)
		}
		ids[f.ID] = true
		if f.Name == "" {
			return fmt.Sprintf("formats[%d]: name is required", i)
		}
		if len(f.Rules) == 0 {
			return fmt.Sprintf("formats[%d]: at least one rule is required", i)
		}
		for j, r := range f.Rules {
			if !containsString(CFFields, r.Field) {
				return fmt.Sprintf("formats[%d].rules[%d]: unknown field %q", i, j, r.Field)
			}
			if !containsString(CFOperators, r.Operator) {
				return fmt.Sprintf("formats[%d].rules[%d]: unknown operator %q", i, j, r.Operator)
			}
			if r.Operator != "exists" && r.Value == "" {
				return fmt.Sprintf("formats[%d].rules[%d]: value is required for operator %q", i, j, r.Operator)
			}
			if r.Operator == "regex" {
				if _, err := regexp.Compile(r.Value); err != nil {
					return fmt.Sprintf("formats[%d].rules[%d]: invalid regex: %v", i, j, err)
				}
			}
			if (r.Operator == "gt" || r.Operator == "lt") && r.Field != "size_mb" {
				return fmt.Sprintf("formats[%d].rules[%d]: gt/lt only apply to size_mb", i, j)
			}
			if r.Operator == "gt" || r.Operator == "lt" {
				if _, err := strconv.ParseFloat(r.Value, 64); err != nil {
					return fmt.Sprintf("formats[%d].rules[%d]: value must be a number for %q", i, j, r.Operator)
				}
			}
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// connect
// ---------------------------------------------------------------------------

// NotificationOptions is one notification sink's options. Token is the
// sink's credential (Plex token / webhook secret); it crosses the API masked
// — reads return Token="" + TokenSet, writes with an empty Token keep the
// stored value.
type NotificationOptions struct {
	URL       string `json:"url"`
	SectionID string `json:"section_id"`
	Token     string `json:"token"`
	TokenSet  bool   `json:"token_set"`
}

// NotificationKinds are the supported sink implementations.
var NotificationKinds = []string{"webhook", "plex", "jellyfin", "emby"}

// Notification is one configured sink.
type Notification struct {
	ID      int                 `json:"id"`
	Name    string              `json:"name"`
	Kind    string              `json:"kind"`
	Enabled bool                `json:"enabled"`
	Options NotificationOptions `json:"options"`
}

// ConnectSettings is the connect document (the notification feed).
type ConnectSettings struct {
	Notifications []Notification `json:"notifications"`
}

func DefaultConnect() ConnectSettings { return ConnectSettings{Notifications: []Notification{}} }

func (c ConnectSettings) Validate() string {
	ids := map[int]bool{}
	for i, n := range c.Notifications {
		if n.ID < 1 {
			return fmt.Sprintf("notifications[%d]: id must be >= 1", i)
		}
		if ids[n.ID] {
			return fmt.Sprintf("notifications[%d]: duplicate id %d", i, n.ID)
		}
		ids[n.ID] = true
		if n.Name == "" {
			return fmt.Sprintf("notifications[%d]: name is required", i)
		}
		if !containsString(NotificationKinds, n.Kind) {
			return fmt.Sprintf("notifications[%d]: unknown kind %q", i, n.Kind)
		}
		if !isHTTPURLValue(n.Options.URL) {
			return fmt.Sprintf("notifications[%d]: url must be an http/https URL", i)
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// metadata
// ---------------------------------------------------------------------------

// MetadataSettings is the metadata-generation document (the metadata file
// writer's options): whether .nfo sidecars and subtitles are written on
// import/refresh, in which language, and for which kinds. The music fields
// (L3) cover tagging audio files and the metadata consumers.
type MetadataSettings struct {
	GenerateNFO       bool            `json:"generate_nfo"`
	GenerateSubtitles bool            `json:"generate_subtitles"`
	Language          string          `json:"language"`
	Kinds             map[string]bool `json:"kinds"`
	// Lidarr L3 music metadata: tag audio files with metadata on import and
	// scrub existing tags from the files.
	TagAudioFiles     bool               `json:"tag_audio_files"`
	ScrubExistingTags bool               `json:"scrub_existing_tags"`
	Consumers         []MetadataConsumer `json:"consumers"`
}

// MetadataConsumer is one music metadata consumer (L3): an app that reads
// the generated metadata files, with a name + enabled flag.
type MetadataConsumer struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

func DefaultMetadata() MetadataSettings {
	return MetadataSettings{
		GenerateNFO:       true,
		GenerateSubtitles: false,
		Language:          "en",
		Kinds:             map[string]bool{"movie": true, "tv": true, "music": true, "book": true},
		// Lidarr L3: tag audio files on import by default; don't scrub
		// existing tags. Consumers default to DISABLED (no consumers run
		// unless the user opts in).
		TagAudioFiles:     true,
		ScrubExistingTags: false,
		Consumers: []MetadataConsumer{
			// Lidarr groups Kodi/Emby under one consumer label.
			{Name: "Kodi (XBMC)/Emby", Enabled: false},
			{Name: "Roksbox", Enabled: false},
			{Name: "WDTV", Enabled: false},
		},
	}
}

func (m MetadataSettings) Validate() string {
	if len(m.Language) < 2 || len(m.Language) > 8 {
		return "language must be a 2-8 char language code"
	}
	for k := range m.Kinds {
		if !settingsLibraryKinds[k] {
			return "kinds: unknown kind " + strconv.Quote(k)
		}
	}
	// Lidarr L3: consumers carry a required name (enabled is a plain bool).
	seenConsumers := map[string]bool{}
	for i, c := range m.Consumers {
		if c.Name == "" {
			return fmt.Sprintf("consumers[%d]: name is required", i)
		}
		if seenConsumers[c.Name] {
			return fmt.Sprintf("consumers[%d]: duplicate name %q", i, c.Name)
		}
		seenConsumers[c.Name] = true
	}
	return ""
}

var settingsLibraryKinds = map[string]bool{
	"movie": true, "tv": true, "music": true, "book": true,
}

// ---------------------------------------------------------------------------
// metadata source (P5.7 per-kind source lists)
// ---------------------------------------------------------------------------

// MetadataSourceSettings is the P5.7 document: the ordered provider chain
// per library kind. The fallback chain tries sources in order, skipping any
// that are not configured (missing API key), so a zero-key install still
// searches through the keyless adapters.
type MetadataSourceSettings struct {
	Sources map[string][]string `json:"sources"`
}

// ProviderNames is the full vocabulary of metadata providers a kind's source
// list may name (keyed: tmdb, tvdb; keyless: tvmaze, wikidata, musicbrainz,
// openlibrary).
var ProviderNames = []string{"tmdb", "tvdb", "tvmaze", "wikidata", "musicbrainz", "openlibrary"}

// KeyedProviders are the providers that need an API key; keyless ones are
// ProviderNames minus these.
var KeyedProviders = map[string]bool{"tmdb": true, "tvdb": true}

// DefaultMetadataSource returns the default per-kind chains: keyed primary
// first, keyless fallbacks after.
func DefaultMetadataSource() MetadataSourceSettings {
	return MetadataSourceSettings{Sources: map[string][]string{
		"tv":    {"tvdb", "tvmaze", "tmdb"},
		"movie": {"tmdb"},
		"music": {"musicbrainz"},
		"book":  {"openlibrary"},
	}}
}

func (m MetadataSourceSettings) Validate() string {
	for kind, list := range m.Sources {
		if !settingsLibraryKinds[kind] {
			return "sources: unknown kind " + strconv.Quote(kind)
		}
		if len(list) == 0 {
			return "sources." + kind + " must name at least one provider"
		}
		seen := map[string]bool{}
		for _, p := range list {
			if !containsString(ProviderNames, p) {
				return "sources." + kind + ": unknown provider " + strconv.Quote(p)
			}
			if seen[p] {
				return "sources." + kind + ": duplicate provider " + strconv.Quote(p)
			}
			seen[p] = true
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// import lists
// ---------------------------------------------------------------------------

// ImportListKinds are the supported import-list sources.
var ImportListKinds = []string{"trakt", "sonarr", "radarr"}

// ImportList is one configured list. For trakt, URL is a Trakt list URL
// (https://trakt.tv/users/<user>/lists/<slug>); for sonarr, URL is the base
// URL of a Sonarr instance and APIKey its API key (masked on read). For
// radarr (R5a drop-in bridge), the list points at a Radarr instance by
// Host+Port and APIKey is its API key (masked on read, sent as a query
// parameter to the instance's /api/v1/movies).
type ImportList struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
	URL  string `json:"url"`
	// Radarr R5a (kind=radarr only): the Radarr instance to import from.
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Enabled   bool   `json:"enabled"`
	APIKey    string `json:"api_key"`
	APIKeySet bool   `json:"api_key_set"`
}

// ImportListsSettings is the import-lists document.
type ImportListsSettings struct {
	Lists []ImportList `json:"lists"`
}

func DefaultImportLists() ImportListsSettings { return ImportListsSettings{Lists: []ImportList{}} }

func (i ImportListsSettings) Validate() string {
	ids := map[int]bool{}
	for n, l := range i.Lists {
		if l.ID < 1 {
			return fmt.Sprintf("lists[%d]: id must be >= 1", n)
		}
		if ids[l.ID] {
			return fmt.Sprintf("lists[%d]: duplicate id %d", n, l.ID)
		}
		ids[l.ID] = true
		if l.Name == "" {
			return fmt.Sprintf("lists[%d]: name is required", n)
		}
		if !containsString(ImportListKinds, l.Kind) {
			return fmt.Sprintf("lists[%d]: unknown kind %q", n, l.Kind)
		}
		if l.Kind == "radarr" {
			// R5a drop-in bridge: the list names a Radarr instance by
			// host+port (no URL); the fetch builds the endpoint from them.
			if l.Host == "" {
				return fmt.Sprintf("lists[%d]: host is required for radarr lists", n)
			}
			if l.Port < 1 || l.Port > 65535 {
				return fmt.Sprintf("lists[%d]: port must be between 1 and 65535", n)
			}
		} else {
			if !isHTTPURLValue(l.URL) {
				return fmt.Sprintf("lists[%d]: url must be an http/https URL", n)
			}
			if l.Kind == "trakt" && !strings.Contains(l.URL, "trakt.tv") {
				return fmt.Sprintf("lists[%d]: trakt lists must use a trakt.tv URL", n)
			}
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// generic per-section codecs
// ---------------------------------------------------------------------------

// DecodeDocument turns stored (or nil, for unsaved) section JSON into the
// typed document, decoded over the section defaults so a partial stored
// document still reads complete.
func (s Section) DecodeDocument(b []byte) (any, error) {
	var out any
	switch s {
	case SectionGeneral:
		d := DefaultGeneral()
		out = &d
	case SectionUI:
		d := DefaultUI()
		out = &d
	case SectionQuality:
		d := DefaultQuality()
		out = &d
	case SectionProfiles:
		d := DefaultProfiles()
		out = &d
	case SectionTags:
		d := DefaultTags()
		out = &d
	case SectionMediaManagement:
		d := DefaultMediaManagement()
		out = &d
	case SectionCustomFormats:
		d := DefaultCustomFormats()
		out = &d
	case SectionConnect:
		d := DefaultConnect()
		out = &d
	case SectionMetadata:
		d := DefaultMetadata()
		out = &d
	case SectionMetadataSource:
		d := DefaultMetadataSource()
		out = &d
	case SectionImportLists:
		d := DefaultImportLists()
		out = &d
	default:
		return nil, fmt.Errorf("settings: unknown section %q", s)
	}
	if len(b) > 0 {
		if err := json.Unmarshal(b, out); err != nil {
			return nil, fmt.Errorf("settings: decode %s: %w", s, err)
		}
	}
	return out, nil
}

// ValidateDocument type-switches to the document's Validate (accepts either
// the value or pointer form, so callers can pass the result of DefaultX()).
func (s Section) ValidateDocument(doc any) string {
	switch v := doc.(type) {
	case GeneralSettings, *GeneralSettings:
		switch g := any(v).(type) {
		case GeneralSettings:
			return g.Validate()
		case *GeneralSettings:
			return g.Validate()
		}
	case UISettings, *UISettings:
		switch u := any(v).(type) {
		case UISettings:
			return u.Validate()
		case *UISettings:
			return u.Validate()
		}
	case QualitySettings, *QualitySettings:
		switch q := any(v).(type) {
		case QualitySettings:
			return q.Validate()
		case *QualitySettings:
			return q.Validate()
		}
	case ProfilesSettings, *ProfilesSettings:
		switch p := any(v).(type) {
		case ProfilesSettings:
			return p.Validate()
		case *ProfilesSettings:
			return p.Validate()
		}
	case TagsSettings, *TagsSettings:
		switch t := any(v).(type) {
		case TagsSettings:
			return t.Validate()
		case *TagsSettings:
			return t.Validate()
		}
	case MediaManagementSettings, *MediaManagementSettings:
		switch m := any(v).(type) {
		case MediaManagementSettings:
			return m.Validate()
		case *MediaManagementSettings:
			return m.Validate()
		}
	case CustomFormatsSettings, *CustomFormatsSettings:
		switch c := any(v).(type) {
		case CustomFormatsSettings:
			return c.Validate()
		case *CustomFormatsSettings:
			return c.Validate()
		}
	case ConnectSettings, *ConnectSettings:
		switch c := any(v).(type) {
		case ConnectSettings:
			return c.Validate()
		case *ConnectSettings:
			return c.Validate()
		}
	case MetadataSettings, *MetadataSettings:
		switch m := any(v).(type) {
		case MetadataSettings:
			return m.Validate()
		case *MetadataSettings:
			return m.Validate()
		}
	case MetadataSourceSettings, *MetadataSourceSettings:
		switch m := any(v).(type) {
		case MetadataSourceSettings:
			return m.Validate()
		case *MetadataSourceSettings:
			return m.Validate()
		}
	case ImportListsSettings, *ImportListsSettings:
		switch i := any(v).(type) {
		case ImportListsSettings:
			return i.Validate()
		case *ImportListsSettings:
			return i.Validate()
		}
	}
	return fmt.Sprintf("settings: cannot validate %T", doc)
}

// MaskDocument returns a copy of the document with secrets blanked (connect
// tokens, import-list API keys) — the read shape that goes over the wire.
// The copy is deep enough that masking never mutates the caller's document.
func (s Section) MaskDocument(doc any) any {
	switch v := doc.(type) {
	case *ConnectSettings:
		out := *v
		out.Notifications = make([]Notification, len(v.Notifications))
		copy(out.Notifications, v.Notifications)
		for i := range out.Notifications {
			out.Notifications[i].Options.Token = ""
		}
		return &out
	case *ImportListsSettings:
		out := *v
		out.Lists = make([]ImportList, len(v.Lists))
		copy(out.Lists, v.Lists)
		for i := range out.Lists {
			out.Lists[i].APIKey = ""
		}
		return &out
	case ConnectSettings:
		out := v
		out.Notifications = make([]Notification, len(v.Notifications))
		copy(out.Notifications, v.Notifications)
		for i := range out.Notifications {
			out.Notifications[i].Options.Token = ""
		}
		return out
	case ImportListsSettings:
		out := v
		out.Lists = make([]ImportList, len(v.Lists))
		copy(out.Lists, v.Lists)
		for i := range out.Lists {
			out.Lists[i].APIKey = ""
		}
		return out
	default:
		return doc
	}
}

// EncodeDocument marshals a typed document to its stored JSON form.
func (s Section) EncodeDocument(doc any) ([]byte, error) {
	b, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("settings: encode %s: %w", s, err)
	}
	return b, nil
}

// MergeSecrets copies stored secrets into an incoming document whose secret
// fields are empty (the masked-password round-trip), per section.
func (s Section) MergeSecrets(stored, incoming any) {
	switch s {
	case SectionConnect:
		st, ok := stored.(*ConnectSettings)
		if !ok {
			return
		}
		in, ok := incoming.(*ConnectSettings)
		if !ok {
			return
		}
		tokens := map[int]string{}
		for _, n := range st.Notifications {
			tokens[n.ID] = n.Options.Token
		}
		for i := range in.Notifications {
			t := in.Notifications[i]
			if t.Options.Token != "" {
				t.Options.TokenSet = true
			} else if old, ok := tokens[t.ID]; ok {
				t.Options.Token = old
				t.Options.TokenSet = true
			}
			in.Notifications[i] = t
		}
	case SectionImportLists:
		st, ok := stored.(*ImportListsSettings)
		if !ok {
			return
		}
		in, ok := incoming.(*ImportListsSettings)
		if !ok {
			return
		}
		keys := map[int]string{}
		for _, l := range st.Lists {
			keys[l.ID] = l.APIKey
		}
		for i := range in.Lists {
			l := in.Lists[i]
			if l.APIKey != "" {
				l.APIKeySet = true
			} else if old, ok := keys[l.ID]; ok {
				l.APIKey = old
				l.APIKeySet = true
			}
			in.Lists[i] = l
		}
	}
}

// ---- small helpers ----

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func isHTTPURLValue(s string) bool {
	if s == "" {
		return false
	}
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}
