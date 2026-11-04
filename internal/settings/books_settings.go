// Package settings — Readarr B5: the Books-specific settings layer.
//
// This file holds the Books-specific portion of the settings documents that
// was not part of the shared (P5) sections: the book media-management block
// (naming + file management), the Calibre integration config (feature-flagged,
// off by default), and the book metadata tag-writing options. The book
// quality / metadata PROFILES themselves are the B3 data in books.go.
//
// Layering: this file only adds data types + defaults + validation; it imports
// nothing outside the standard library (settings stays a leaf package). The
// naming engine that renders the book naming templates lives in
// domains/books (which imports settings); the service layer maps a
// BookNaming config into the engine's NamingOptions.
package settings

import (
	"fmt"
	"strings"
)

// ---------------------------------------------------------------------------
// Media management (books) — naming block
// ---------------------------------------------------------------------------

// BookNaming is the Readarr book naming block (B5). standard_format is the
// book's file-naming template and author_format the author-folder template,
// both in the same brace-token style as the movie/music naming blocks but
// using the Readarr BOOK tag set (see domains/books/naming.go). The engine
// that renders them is domains/books; this struct is the config surface the
// UI reads/edits and the service maps into NamingOptions.
type BookNaming struct {
	// RenameBooks, when false, keeps the original release filename (no
	// template is applied) — mirrors Radarr R5a's rename_movies.
	RenameBooks              bool `json:"rename_books"`
	ReplaceIllegalCharacters bool `json:"replace_illegal_characters"`
	// ColonReplacement is the single character (or empty = remove) used for
	// ':' in titles when ReplaceIllegalCharacters is on.
	ColonReplacement string `json:"colon_replacement"`
	// StandardFormat is the book file-naming template (brace tokens), e.g.
	// "{Author Name} - {Book Title} ({Release Year})".
	StandardFormat string `json:"standard_format"`
	// AuthorFormat is the author-folder template (brace tokens), e.g.
	// "{Author Name}".
	AuthorFormat string `json:"author_format"`
	// SpaceHandling: "space" (default), "period", "underscore", "dash".
	SpaceHandling string `json:"space_handling"`
	// CaseHandling: "default" (default), "uppercase", "lowercase".
	CaseHandling string `json:"case_handling"`
}

// DefaultBookStandardFormat / DefaultBookAuthorFormat are the Readarr book
// naming defaults (the plan doc §2 Media-Management naming bullet).
const (
	DefaultBookStandardFormat = "{Author Name} - {Book Title} ({Release Year})"
	DefaultBookAuthorFormat   = "{Author Name}"
)

// DefaultBookNaming returns the factory-default book naming block.
func DefaultBookNaming() BookNaming {
	return BookNaming{
		RenameBooks:              true,
		ReplaceIllegalCharacters: true,
		ColonReplacement:         "",
		StandardFormat:           DefaultBookStandardFormat,
		AuthorFormat:             DefaultBookAuthorFormat,
		SpaceHandling:            "space",
		CaseHandling:             "default",
	}
}

// Validate checks the book naming block: the two templates are required and
// the space/case handling values must be in their (small, engine-defined)
// vocabularies.
func (b BookNaming) Validate() string {
	if strings.TrimSpace(b.StandardFormat) == "" {
		return "book_naming: standard_format is required"
	}
	if strings.TrimSpace(b.AuthorFormat) == "" {
		return "book_naming: author_format is required"
	}
	if !containsString(bookSpaceHandlings, b.SpaceHandling) {
		return fmt.Sprintf("book_naming: unknown space_handling %q", b.SpaceHandling)
	}
	if !containsString(bookCaseHandlings, b.CaseHandling) {
		return fmt.Sprintf("book_naming: unknown case_handling %q", b.CaseHandling)
	}
	return ""
}

var (
	bookSpaceHandlings = []string{"space", "period", "underscore", "dash"}
	bookCaseHandlings  = []string{"default", "uppercase", "lowercase"}
)

// ---------------------------------------------------------------------------
// Media management (books) — file management block
// ---------------------------------------------------------------------------

// BookFileManagement is the Readarr book media-management file block (B5):
// the unmonitor/ignore toggles, propers/repacks handling, author-folder and
// empty-folder management, the file-date / library-watch / rescan /
// fingerprinting options, the recycle bin, the Linux permissions, and the
// import free-space / hard-link / extra-file options. Field names mirror the
// archived Readarr MediaManagementConfigResource.
type BookFileManagement struct {
	// AutoUnmonitorDownloaded stops monitoring a book once a file for it has
	// been imported (Readarr autoUnmonitorPreviouslyDownloadedBooks).
	AutoUnmonitorDownloaded bool `json:"unmonitor_downloaded"`
	// IgnoreDeletedFiles ignores book files that are deleted on disk.
	IgnoreDeletedFiles bool `json:"ignore_deleted_files"`
	// DownloadPropersAndRepacks: "ignore" | "include" | "upgrade".
	DownloadPropersAndRepacks string `json:"download_propers_and_repacks"`

	// CreateEmptyAuthorFolders creates an author's folder even when it has no
	// imported books yet.
	CreateEmptyAuthorFolders bool `json:"create_empty_author_folders"`
	// DeleteEmptyFolders removes author folders that end up empty.
	DeleteEmptyFolders bool `json:"delete_empty_folders"`

	// FileDate: "original" | "release" (set the file mtime to the book's
	// release date on import).
	FileDate string `json:"file_date"`
	// WatchLibraryForChanges makes the scanner rescan a root folder for
	// changes (Readarr watchLibraryForChanges).
	WatchLibraryForChanges bool `json:"watch_library_for_changes"`
	// RescanAfterRefresh: "no" | "changed" | "all".
	RescanAfterRefresh string `json:"rescan_after_refresh"`
	// AllowFingerprinting: "no" | "yes" (write the file-fingerprint tag).
	AllowFingerprinting string `json:"allow_fingerprinting"`

	// RecycleBin is the path to a recycle bin for deleted files ("" = direct
	// delete); RecycleBinCleanupDays is how long items stay before purge.
	RecycleBin            string `json:"recycle_bin"`
	RecycleBinCleanupDays int    `json:"recycle_bin_cleanup_days"`

	// SetPermissionsLinux + the chown/chmod group are applied to imported
	// files when enabled (Linux only).
	SetPermissionsLinux bool   `json:"set_permissions_linux"`
	ChmodFolder         string `json:"chmod_folder"`
	ChownUser           string `json:"chown_user"`
	ChownGroup          string `json:"chown_group"`

	// SkipFreeSpaceCheckWhenImporting skips the destination free-space check;
	// MinimumFreeSpaceWhenImportingGB is the minimum free space (GB) required.
	SkipFreeSpaceCheckWhenImporting bool `json:"skip_free_space_check"`
	MinimumFreeSpaceWhenImportingGB int  `json:"min_free_space_gb"`
	// CopyUsingHardlinks hard-links instead of copies when possible (B5:
	// auto-disabled per root when that root's Calibre is enabled).
	CopyUsingHardlinks bool `json:"copy_using_hardlinks"`
	// ImportExtraFiles copies extra (non-book) files; ExtraFileExtensions
	// lists the allowed extensions (comma/newline separated, "" = all).
	ImportExtraFiles    bool   `json:"import_extra_files"`
	ExtraFileExtensions string `json:"extra_file_extensions"`
}

// DefaultBookManagement returns the factory-default book file-management
// block. Defaults follow Readarr's factory: unmonitor on, ignore deletions
// off, propers/repacks "include", empty-folder creation on, file date
// "original", rescan "no", fingerprinting "no", no recycle bin, permissions
// off, hard links on (Calibre overrides per root), extra files off.
func DefaultBookManagement() BookFileManagement {
	return BookFileManagement{
		AutoUnmonitorDownloaded:         true,
		IgnoreDeletedFiles:              false,
		DownloadPropersAndRepacks:       "include",
		CreateEmptyAuthorFolders:        true,
		DeleteEmptyFolders:              false,
		FileDate:                        "original",
		WatchLibraryForChanges:          true,
		RescanAfterRefresh:              "no",
		AllowFingerprinting:             "no",
		RecycleBin:                      "",
		RecycleBinCleanupDays:           14,
		SetPermissionsLinux:             false,
		ChmodFolder:                     "",
		ChownUser:                       "",
		ChownGroup:                      "",
		SkipFreeSpaceCheckWhenImporting: false,
		MinimumFreeSpaceWhenImportingGB: 0,
		CopyUsingHardlinks:              true,
		ImportExtraFiles:                false,
		ExtraFileExtensions:             "",
	}
}

// Validate checks the book file-management block's enum-style fields and the
// recycle-bin cleanup days.
func (m BookFileManagement) Validate() string {
	if !containsString(propersRepacksOptions, m.DownloadPropersAndRepacks) {
		return fmt.Sprintf("book_management: unknown download_propers_and_repacks %q", m.DownloadPropersAndRepacks)
	}
	if !containsString(fileDateOptions, m.FileDate) {
		return fmt.Sprintf("book_management: unknown file_date %q", m.FileDate)
	}
	if !containsString(rescanOptions, m.RescanAfterRefresh) {
		return fmt.Sprintf("book_management: unknown rescan_after_refresh %q", m.RescanAfterRefresh)
	}
	if !containsString(fingerprintOptions, m.AllowFingerprinting) {
		return fmt.Sprintf("book_management: unknown allow_fingerprinting %q", m.AllowFingerprinting)
	}
	if m.RecycleBinCleanupDays < 0 {
		return "book_management: recycle_bin_cleanup_days must be >= 0"
	}
	if m.MinimumFreeSpaceWhenImportingGB < 0 {
		return "book_management: min_free_space_gb must be >= 0"
	}
	return ""
}

var (
	propersRepacksOptions = []string{"ignore", "include", "upgrade"}
	fileDateOptions       = []string{"original", "release"}
	rescanOptions         = []string{"no", "changed", "all"}
	fingerprintOptions    = []string{"no", "yes"}
)

// ---------------------------------------------------------------------------
// Calibre (B5) — the Readarr signature, feature-flagged, OFF by default
// ---------------------------------------------------------------------------

// CalibreEnabled is the global Calibre feature flag (B5). It is a kill-switch
// for the whole Calibre integration: when false, no root folder's Calibre
// config is applied and the UI hides the Calibre surface. A per-root folder
// additionally carries its own Calibre config (see rootfolders), which only
// takes effect when this flag is on. Off by default.
const CalibreEnabledFlag = "calibre_enabled"

// CalibreSettings is the per-root-folder Calibre Content Server + book config
// (B5). It is the config surface for the Calibre integration: the Content
// Server connection (host/port/url_base/username/password/library), the
// conversion + output profile, the connection toggles, and the per-root
// monitoring / profile / tag overrides. The password is masked on read and
// blank on write keeps the stored value (the indexer masked-edit convention);
// it is vault-encrypted at rest by the Postgres root-folder repo.
type CalibreSettings struct {
	// Enabled is this root folder's "Use Calibre" toggle (Readarr's
	// IsCalibreLibrary; the per-root flag, also gated by the global
	// CalibreEnabled feature flag).
	Enabled bool `json:"enabled"`

	// Content Server connection.
	Host        string `json:"host"`
	Port        int    `json:"port"`
	URLBase     string `json:"url_base"`
	Username    string `json:"username"`
	Password    string `json:"password"` // write-only; masked on read
	PasswordSet bool   `json:"password_set"`
	Library     string `json:"library"`
	// UseSSL uses an HTTPS Content Server connection.
	UseSSL bool `json:"use_ssl"`

	// OutputFormat is the comma-separated list of Calibre output formats to
	// convert to (the "Convert to" formats: MOBI, EPUB, AZW3, ...); "" = no
	// conversion (just import). OutputProfile is the Calibre output (device)
	// profile index to convert with (0 = default).
	OutputFormat  string `json:"output_format"`
	OutputProfile int    `json:"output_profile"`

	// Monitor, when true, auto-imports this root's books into Calibre on
	// import. QualityProfile / MetadataProfile are the NAMEs of the seeded
	// book profiles to apply to this root's books ("" = the app default).
	// DefaultTags is the set of tag ids written to this root's books.
	Monitor         bool   `json:"monitor"`
	QualityProfile  string `json:"quality_profile"`
	MetadataProfile string `json:"metadata_profile"`
	DefaultTags     []int  `json:"default_tags"`
}

// CalibreConvertFormats is the set of Calibre output formats a root can
// convert to (the plan doc §2 Calibre bullet).
var CalibreConvertFormats = []string{
	"MOBI", "EPUB", "AZW3", "DOCX", "FB2", "TXT", "RTF", "LIT",
	"CHM", "HTML", "AZW", "KFX", "SNB", "TTS",
}

// DefaultCalibre returns the factory-default (disabled, empty) per-root
// Calibre config. Off by default: Enabled false, no Content Server, no
// conversion.
func DefaultCalibre() CalibreSettings {
	return CalibreSettings{
		Enabled:       false,
		OutputFormat:  "MOBI",
		OutputProfile: 0,
		Monitor:       false,
		DefaultTags:   []int{},
	}
}

// Validate checks a per-root Calibre config. When disabled the connection
// fields are not required (a half-configured-but-off root is legal); when
// enabled the Content Server host/port/username are required. OutputFormat
// (when set) must be a comma-separated list of known Calibre formats and
// OutputProfile must be non-negative.
func (c CalibreSettings) Validate() string {
	if c.OutputFormat != "" {
		for _, f := range strings.Split(c.OutputFormat, ",") {
			f = strings.TrimSpace(f)
			if !containsString(CalibreConvertFormats, f) {
				return fmt.Sprintf("calibre: unknown output format %q", f)
			}
		}
	}
	if c.OutputProfile < 0 {
		return "calibre: output_profile must be >= 0"
	}
	if !c.Enabled {
		return ""
	}
	if c.Host == "" {
		return "calibre: host is required when enabled"
	}
	if c.Port < 1 || c.Port > 65535 {
		return "calibre: port must be between 1 and 65535 when enabled"
	}
	if c.Username == "" {
		return "calibre: username is required when enabled"
	}
	return ""
}

// HardlinksDisabled reports whether Calibre being enabled for a root folder
// forces hard-links OFF for that root (Calibre's requirement: it needs real
// files, not hard links, or its library gets corrupted). This is the B5
// "when Calibre is enabled for a root, hardlinks are auto-disabled for that
// root" rule. A nil/disabled Calibre config does not disable hard links.
func (c CalibreSettings) HardlinksDisabled() bool {
	return c.Enabled
}

// ---------------------------------------------------------------------------
// Metadata (books) — write tags to files (B5)
// ---------------------------------------------------------------------------

// BookTagWrite is the "write tags to files" config for books (B5): the tag
// mode for e-book files and for audio (a-book) files. "no" disables tagging
// for that file kind; "all" writes every applicable tag; "book"/"audio"
// write the book- / audio-specific subset.
type BookTagWrite struct {
	BookTagType  string `json:"book_tag_type"`
	AudioTagType string `json:"audio_tag_type"`
}

// DefaultBookTagWrite returns the default tag-writing config (tag e-books
// with all tags; audio is off by default since e-books have no audio tags).
func DefaultBookTagWrite() BookTagWrite {
	return BookTagWrite{BookTagType: "all", AudioTagType: "no"}
}

// Validate checks the book tag-writing modes.
func (b BookTagWrite) Validate() string {
	if !containsString(bookTagOptions, b.BookTagType) {
		return fmt.Sprintf("metadata: unknown book_tag_type %q", b.BookTagType)
	}
	if !containsString(audioTagOptions, b.AudioTagType) {
		return fmt.Sprintf("metadata: unknown audio_tag_type %q", b.AudioTagType)
	}
	return ""
}

var (
	bookTagOptions  = []string{"no", "all", "book"}
	audioTagOptions = []string{"no", "all", "audio"}
)
