package books

import (
	"fmt"
	"os"
	"path"
	"strings"
	"unicode"
)

// Import is the plan for importing one completed book download: the source file
// on disk (from the download client) and the destination under the media root
// (renamed to a clean, template-driven name and placed in the author/title
// folders).
type Import struct {
	// SourcePath is the full path to the downloaded file.
	SourcePath string
	// DestPath is the full path to import the file to.
	DestPath string
	// DestName is the cleaned filename at the destination.
	DestName string
	// Extension is the file extension including the leading dot (e.g. ".epub").
	Extension string
}

// PlanImport computes where a downloaded e-book file should land.
//
// layout (PLAN §6, per-author template):
//
//	root/<Author>/<Title>/<Title>.<ext>
//
// e.g.
//
//	/media/books/Ursula K. Le Guin/The Dispossessed/The Dispossessed.epub
//
// A book is a single file (an e-book), so — like music's whole-album file — the
// file is named after the title. Collisions are detected, not overwritten: if
// the destination file already exists, ErrCollision is returned. The
// destination must stay under the media root — a release name containing
// "../" is rejected (PLAN §8 hardening). No shell is executed; this is pure
// path computation.
func PlanImport(root, author, title, sourcePath string) (*Import, error) {
	if root == "" {
		return nil, fmt.Errorf("books: import: media root is empty")
	}
	ext := path.Ext(sourcePath)

	aDir := cleanPathSegment(author)
	if aDir == "" {
		aDir = "author"
	}
	tDir := cleanPathSegment(title)
	if tDir == "" {
		tDir = "title"
	}

	fileName := tDir

	dir := path.Join(root, aDir)
	destPath := path.Join(dir, tDir, fileName+ext)

	if !withinRoot(root, destPath) {
		return nil, fmt.Errorf("books: import: destination escapes media root: %q", destPath)
	}
	if err := checkCollision(destPath); err != nil {
		return nil, err
	}
	return &Import{
		SourcePath: sourcePath,
		DestPath:   destPath,
		DestName:   fileName + ext,
		Extension:  ext,
	}, nil
}

// DestPath returns the destination path a book file would land at under root,
// without performing a collision check. It is the path-only companion to
// PlanImport, for call sites (like the import pipeline) that need to decide
// collision/copy behaviour themselves.
func DestPath(root, author, title, sourcePath string) (string, error) {
	if root == "" {
		return "", fmt.Errorf("books: import: media root is empty")
	}
	ext := path.Ext(sourcePath)

	aDir := cleanPathSegment(author)
	if aDir == "" {
		aDir = "author"
	}
	tDir := cleanPathSegment(title)
	if tDir == "" {
		tDir = "title"
	}
	dest := path.Join(root, aDir, tDir, tDir+ext)
	if !withinRoot(root, dest) {
		return "", fmt.Errorf("books: import: destination escapes media root: %q", dest)
	}
	return dest, nil
}

// checkCollision returns ErrCollision if destPath exists, nil if it does not.
func checkCollision(destPath string) error {
	if _, err := os.Stat(destPath); err == nil {
		return fmt.Errorf("%w: %s", ErrCollision, destPath)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("books: import: stat %s: %w", destPath, err)
	}
	return nil
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
			r == ' ' || r == '-' || r == '_' || r == '.' || r == '(' || r == ')' || r == ',':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := strings.Join(strings.Fields(b.String()), " ") // collapse whitespace
	return strings.Trim(out, " _")
}
