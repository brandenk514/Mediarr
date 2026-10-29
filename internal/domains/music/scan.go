package music

// L4 (Lidarr library import, phase 1): the scan -> match -> report types,
// mirroring the TV import (internal/domains/tv/scan.go). Phase 1 ends at
// the report: the API scan endpoint reports candidates and their matches,
// and the UI confirm step is client-side selection — no file moves, no
// artist creation from the endpoint.

import (
	"path"
	"regexp"
	"strconv"
	"strings"
)

// MusicImportCandidate is one artist folder found while scanning a root
// folder (library import, phase 1: scan -> match -> confirm).
type MusicImportCandidate struct {
	Path     string // absolute path of the scanned folder
	Title    string // parsed artist title (folder name without a trailing year in parens)
	Files    int    // audio files found under the folder
	Size     int64  // total bytes of those audio files
	Matched  bool   // a tracked artist was found for this folder
	ArtistID int64  // the matched artist's id (0 when not matched)
}

// MusicImportReport is the scan + match result for a root folder.
type MusicImportReport struct {
	Root       string
	Scanned    int // number of candidate folders
	Candidates []MusicImportCandidate
}

// audioExtensions are the file extensions recognized as audio during a
// library scan.
var audioExtensions = []string{".flac", ".mp3", ".m4a", ".aac", ".ogg", ".oga", ".opus", ".wav", ".wv", ".ape", ".alac", ".wma"}

// IsAudioFile reports whether a file name looks like an audio file.
func IsAudioFile(name string) bool {
	ext := strings.ToLower(path.Ext(name))
	for _, v := range audioExtensions {
		if ext == v {
			return true
		}
	}
	return false
}

var folderYearRe = regexp.MustCompile(`^(.*?)\s*\((\d{4})\)$`)

// ParseFolderName splits an artist folder name into title and year. Both
// "Radiohead" and "Radiohead (1995)" are accepted; folders without a year
// parse to year 0 (mirrors the TV ParseFolderName).
func ParseFolderName(name string) (title string, year int) {
	name = strings.TrimSpace(name)
	if m := folderYearRe.FindStringSubmatch(name); m != nil {
		year, _ = strconv.Atoi(m[2])
		return strings.TrimSpace(m[1]), year
	}
	return strings.TrimSpace(name), 0
}

// CandidateMatchesArtist reports whether a scanned candidate matches a
// tracked artist: the titles are equal (case-insensitive, trimmed). Unlike
// the TV case, a year in parens does not constrain the match — artists
// have no year of their own to agree with.
func CandidateMatchesArtist(candidateTitle string, a Artist) bool {
	return strings.EqualFold(strings.TrimSpace(candidateTitle), strings.TrimSpace(a.Name))
}
