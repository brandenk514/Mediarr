package music

import (
	"fmt"
	"os"
	"path"
	"strings"
	"unicode"
)

// Import is the plan for importing one completed music download: the source
// file on disk (from the download client) and the destination under the media
// root (renamed to a clean, template-driven name and placed in the artist/album
// folders).
type Import struct {
	// SourcePath is the full path to the downloaded file.
	SourcePath string
	// DestPath is the full path to import the file to.
	DestPath string
	// DestName is the cleaned filename at the destination.
	DestName string
	// Extension is the file extension including the leading dot (e.g. ".flac").
	Extension string
	// Disc/Number the file is for (0/0 for a whole-album file).
	Disc   int
	Number int
}

// PlanTrackImport computes where a downloaded single track should land.
//
// layout (PLAN §6, per-artist template):
//
//	root/<Artist>/<Album>/<NN>-<NN> <Title>.<ext>
//
// e.g.
//
//	/media/music/Bob Marley (1977)/Legend/01-01 No Woman, No Man.flac
//
// Collisions are detected, not overwritten: if the destination file already
// exists, ErrCollision is returned. The destination must stay under the media
// root — a release name containing "../" is rejected (PLAN §8 hardening). No
// shell is executed; this is pure path computation.
func PlanTrackImport(root, artist, album string, disc, number int, title, sourcePath string) (*Import, error) {
	if root == "" {
		return nil, fmt.Errorf("music: import: media root is empty")
	}
	if disc < 0 || number < 0 {
		return nil, fmt.Errorf("music: import: invalid disc/track %d/%d", disc, number)
	}
	if disc == 0 {
		disc = 1
	}
	ext := path.Ext(sourcePath)

	aDir := cleanPathSegment(artist)
	if aDir == "" {
		aDir = "artist"
	}
	alDir := cleanPathSegment(album)
	if alDir == "" {
		alDir = "album"
	}

	fileName := fmt.Sprintf("%02d-%02d", disc, number)
	if t := cleanPathSegment(title); t != "" {
		fileName += " " + t
	}

	dir := path.Join(root, aDir, alDir)
	destPath := path.Join(dir, fileName+ext)

	if !withinRoot(root, destPath) {
		return nil, fmt.Errorf("music: import: destination escapes media root: %q", destPath)
	}
	if err := checkCollision(destPath); err != nil {
		return nil, err
	}
	return &Import{
		SourcePath: sourcePath,
		DestPath:   destPath,
		DestName:   fileName + ext,
		Extension:  ext,
		Disc:       disc,
		Number:     number,
	}, nil
}

// AlbumDestPath returns the destination path a whole-album file would land at
// under root, without performing a collision check. It is the path-only
// companion to PlanAlbumImport, for call sites (like the import pipeline) that
// need to decide collision/copy behaviour themselves.
func AlbumDestPath(root, artist, album, sourcePath string) (string, error) {
	if root == "" {
		return "", fmt.Errorf("music: import: media root is empty")
	}
	ext := path.Ext(sourcePath)
	aDir := cleanPathSegment(artist)
	if aDir == "" {
		aDir = "artist"
	}
	alDir := cleanPathSegment(album)
	if alDir == "" {
		alDir = "album"
	}
	dest := path.Join(root, aDir, alDir, alDir+ext)
	if !withinRoot(root, dest) {
		return "", fmt.Errorf("music: import: destination escapes media root: %q", dest)
	}
	return dest, nil
}

// PlanAlbumImport computes where a downloaded whole-album file should land
// (a single file representing the entire album, e.g. one .flac).
//
// layout:
//
//	root/<Artist>/<Album>/<Album>.<ext>
func PlanAlbumImport(root, artist, album, sourcePath string) (*Import, error) {
	if root == "" {
		return nil, fmt.Errorf("music: import: media root is empty")
	}
	ext := path.Ext(sourcePath)

	aDir := cleanPathSegment(artist)
	if aDir == "" {
		aDir = "artist"
	}
	alDir := cleanPathSegment(album)
	if alDir == "" {
		alDir = "album"
	}
	fileName := alDir

	dir := path.Join(root, aDir)
	destPath := path.Join(dir, alDir, fileName+ext)

	if !withinRoot(root, destPath) {
		return nil, fmt.Errorf("music: import: destination escapes media root: %q", destPath)
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

// checkCollision returns ErrCollision if destPath exists, nil if it does not.
func checkCollision(destPath string) error {
	if _, err := os.Stat(destPath); err == nil {
		return fmt.Errorf("%w: %s", ErrCollision, destPath)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("music: import: stat %s: %w", destPath, err)
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
