package tv

import (
	"path"
	"regexp"
	"strconv"
	"strings"
)

// ImportCandidate is one series folder found while scanning a root folder
// (library import, phase 1: scan -> match -> confirm).
type ImportCandidate struct {
	Path     string // absolute path of the scanned folder
	Title    string // parsed series title (folder name without the year)
	Year     int    // parsed year; 0 when the folder name has none
	Seasons  int    // number of "Season NN" subfolders
	Files    int    // video files found under the folder
	Size     int64  // total bytes of those video files
	Matched  bool   // a tracked series was found for this folder
	SeriesID int64  // the matched series' id (0 when not matched)
}

// ImportReport is the scan + match result for a root folder.
type ImportReport struct {
	Root       string
	Scanned    int // number of candidate folders
	Candidates []ImportCandidate
}

// videoExtensions are the file extensions recognized as media during a
// library scan.
var videoExtensions = []string{".mkv", ".mp4", ".avi", ".mov", ".m4v", ".wmv", ".ts"}

// IsVideoFile reports whether a file name looks like a media file.
func IsVideoFile(name string) bool {
	ext := strings.ToLower(path.Ext(name))
	for _, v := range videoExtensions {
		if ext == v {
			return true
		}
	}
	return false
}

var folderYearRe = regexp.MustCompile(`^(.*?)\s*\((\d{4})\)$`)

// ParseFolderName splits a series folder name into title and year. Both
// "Breaking Bad" and "Breaking Bad (2008)" are accepted; folders without a
// year parse to year 0.
func ParseFolderName(name string) (title string, year int) {
	name = strings.TrimSpace(name)
	if m := folderYearRe.FindStringSubmatch(name); m != nil {
		year, _ = strconv.Atoi(m[2])
		return strings.TrimSpace(m[1]), year
	}
	return strings.TrimSpace(name), 0
}

// MatchesSeries reports whether a scanned candidate matches a tracked series:
// the titles are equal (case-insensitive, trimmed) and, when the folder name
// carries a year, it must agree with the series' year.
func MatchesSeries(candidateTitle string, candidateYear int, s Series) bool {
	if !strings.EqualFold(strings.TrimSpace(candidateTitle), strings.TrimSpace(s.Title)) {
		return false
	}
	return candidateYear == 0 || s.Year == candidateYear
}
