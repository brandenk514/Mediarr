package tv

import (
	"fmt"
	"os"
	"path"
	"strings"
	"unicode"
)

// Import is the plan for importing one completed TV download: the source file
// on disk (from the download client) and the destination under the media root
// (renamed to a clean, template-driven name and placed in the season folder).
type Import struct {
	// SourcePath is the full path to the downloaded file.
	SourcePath string
	// DestPath is the full path to import the file to.
	DestPath string
	// DestName is the cleaned filename at the destination.
	DestName string
	// Extension is the file extension including the leading dot (e.g. ".mkv").
	Extension string
	// Season/Episode the file is for.
	Season  int
	Episode int
}

// PlanImport computes where a downloaded TV episode should land.
//
// layout (PLAN §6, per-series template):
//
//	root/<Series (Year)>/Season <NN>/<Series> S<xx>E<yy>[ <EpisodeTitle>].<ext>
//
// e.g.
//
//	/media/tv/Breaking Bad (2008)/Season 01/Breaking Bad S01E01 Piloto.mkv
//
// Collisions are detected, not overwritten: if the destination file already
// exists, ErrCollision is returned. The destination is always normalised and
// must remain under the media root — any attempt to escape it (a release name
// containing "../") is rejected (PLAN §8 path-traversal hardening). No shell
// is executed here; this is pure path computation.
func PlanImport(root, seriesTitle string, seriesYear, season, episode int, epTitle, sourcePath string) (*Import, error) {
	if root == "" {
		return nil, fmt.Errorf("tv: import: media root is empty")
	}
	if season < 0 || episode < 0 {
		return nil, fmt.Errorf("tv: import: invalid season/episode %d/%d", season, episode)
	}
	root = strings.TrimRight(root, "/")

	ext := path.Ext(sourcePath)
	name := seriesTitle
	if name == "" {
		name = "series"
	}
	name = cleanPathSegment(name)
	if name == "" {
		name = "series"
	}
	seriesDir := name
	if seriesYear > 0 {
		seriesDir = fmt.Sprintf("%s (%d)", name, seriesYear)
	}

	seasonDir := fmt.Sprintf("Season %02d", season)

	// Per-episode file name: <Series> S<xx>E<yy>[ <EpisodeTitle>].
	fileName := fmt.Sprintf("%s S%02dE%02d", name, season, episode)
	if t := cleanPathSegment(epTitle); t != "" {
		fileName += " " + t
	}

	seriesAbs := path.Join(root, seriesDir)
	dir := path.Join(seriesAbs, seasonDir)
	destPath := path.Join(dir, fileName+ext)

	// Final guard: the resolved path must stay under the media root.
	if !withinRoot(root, destPath) {
		return nil, fmt.Errorf("tv: import: destination escapes media root: %q", destPath)
	}

	// Collisions are detected, not overwritten (PLAN §6).
	if _, err := os.Stat(destPath); err == nil {
		return nil, fmt.Errorf("%w: %s", ErrCollision, destPath)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("tv: import: stat %s: %w", destPath, err)
	}

	return &Import{
		SourcePath: sourcePath,
		DestPath:   destPath,
		DestName:   fileName + ext,
		Extension:  ext,
		Season:     season,
		Episode:    episode,
	}, nil
}

// withinRoot reports whether p resolves to a path inside (or equal to) root.
func withinRoot(root, p string) bool {
	absRoot := path.Clean(root)
	absP := path.Clean(p)
	if absRoot == "" {
		return false
	}
	return absP == absRoot || strings.HasPrefix(absP, absRoot+"/")
}

// cleanPathSegment removes path separators and unsafe characters so a parsed
// title can safely be used as a single directory/file name component.
func cleanPathSegment(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '/' || r == '\\' || r == '\x00':
			continue
		case r < 0x20: // control chars
			continue
		case unicode.IsLetter(r) || unicode.IsNumber(r) ||
			r == ' ' || r == '-' || r == '_' || r == '.' || r == '(' || r == ')':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := strings.Join(strings.Fields(b.String()), " ") // collapse whitespace
	return strings.Trim(out, " _")
}
