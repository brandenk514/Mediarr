package movies

import (
	"fmt"
	"path"
	"strings"
	"unicode"
)

// Import is the plan for importing one completed download: the source file on
// disk (from the download client) and the destination under the media root
// (renamed to a clean, template-driven name).
type Import struct {
	// SourcePath is the full path to the downloaded file (as named by the
	// download client).
	SourcePath string
	// DestPath is the full path to import the file to.
	DestPath string
	// DestName is the cleaned filename at the destination.
	DestName string
	// Extension is the file extension including the leading dot (e.g. ".mkv").
	Extension string
}

// PlanImport computes where a downloaded file should land.
//
// layout: root/<Title (Year)>/<Title (Year)>.<ext>
//
// The destination is always normalised and must remain under root — any
// attempt to escape the root (e.g. a release name containing "../") is
// rejected. This is the path-traversal hardening required by the security
// plan: the resolved path must stay under the configured media root.
func PlanImport(root, title string, year int, sourcePath string) (*Import, error) {
	if root == "" {
		return nil, fmt.Errorf("movies: import: media root is empty")
	}
	root = strings.TrimRight(root, "/")

	ext := path.Ext(sourcePath)
	base := strings.TrimSuffix(path.Base(sourcePath), ext)

	// Derive a clean name from the parsed title; if a title is supplied we use
	// it, otherwise fall back to the source basename.
	name := title
	if name == "" {
		name = base
	}
	if year > 0 {
		name = fmt.Sprintf("%s (%d)", name, year)
	}
	name = cleanPathSegment(name)
	if name == "" {
		name = "movie"
	}

	dir := path.Join(root, name)

	destPath := path.Join(dir, name+ext)
	// cleanPathSegment already strips path separators and control chars from
	// the name, so it cannot escape the root; withinRoot is the final guard.
	if !withinRoot(root, destPath) {
		return nil, fmt.Errorf("movies: import: destination escapes media root: %q", destPath)
	}
	return &Import{
		SourcePath: sourcePath,
		DestPath:   destPath,
		DestName:   name + ext,
		Extension:  ext,
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
			// Replace anything else (e.g. "?", "*", "#", unicode symbols)
			// with nothing to keep names portable.
			b.WriteRune('_')
		}
	}
	out := strings.Join(strings.Fields(b.String()), " ") // collapse whitespace
	return strings.Trim(out, " _")
}
