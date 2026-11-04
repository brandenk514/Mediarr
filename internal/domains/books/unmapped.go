package books

import (
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// B3 (Readarr "Unmapped Files" page): list the book files that live under the
// books media root but are not mapped to a monitored author. Like the music
// L5 twin (domains/music/unmapped.go), this is a pure, filesystem-only
// report: it matches scanned folders against the monitored authors and stops
// at the report (no file moves, no author creation from the endpoint).
// Nothing here imports database, redis, or http (the domain dependency rule).

// UnmappedFile is one book file under the books media root that is not
// mapped to a monitored author.
type UnmappedFile struct {
	Path      string    `json:"path"`
	Size      int64     `json:"size"`
	DateAdded time.Time `json:"date_added"`
	Quality   string    `json:"quality"`
}

// IsBookFile reports whether name is a book file by extension: a PLAN §15.4
// e-book format (epub / mobi / azw3), case-insensitive. Extension-based only
// (ParseFormat also matches format tokens inside names, which is wrong for a
// directory walk — a .txt with "mobi" in its name is not a book file).
func IsBookFile(name string) bool {
	return IsSupportedFormat(strings.ToLower(strings.TrimPrefix(filepath.Ext(name), ".")))
}

// ScanUnmappedFiles walks root and returns every book file that is NOT under
// a top-level folder whose name (case-insensitive, trimmed) is in
// monitoredAuthors. A file directly under root (no author folder) or one
// under a folder matching no monitored author is unmapped. The result is
// sorted by path for stable output.
//
// Matching is at the author-folder level (the standard books layout is
// <root>/<author>/<book>.epub): a file is "mapped" when its top-level folder
// names a monitored tracked author. Files under a non-monitored author
// folder are reported unmapped, so unmonitoring an author surfaces its files
// here (the Readarr semantic). Quality is the book format inferred from the
// file extension (EPUB / AZW3 / MOBI, upper case — the books analogue of
// music's codec-from-extension).
func ScanUnmappedFiles(root string, monitoredAuthors map[string]bool) ([]UnmappedFile, error) {
	root = filepath.Clean(root)
	mon := make(map[string]bool, len(monitoredAuthors))
	for k := range monitoredAuthors {
		mon[strings.ToLower(strings.TrimSpace(k))] = true
	}
	var out []UnmappedFile
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			// The root itself missing is a real error (the API pre-checks it);
			// a bad entry deeper in the tree is skipped so one unreadable file
			// never aborts the whole report.
			if p == root {
				return err
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !IsBookFile(d.Name()) {
			return nil
		}
		// Top-level folder = the first path element relative to root. A file
		// directly under root has none (top stays "") and is unmapped.
		top := ""
		if rel, rerr := filepath.Rel(root, p); rerr == nil {
			if i := strings.IndexAny(rel, `/\`); i > 0 {
				top = rel[:i]
			}
		}
		if mon[strings.ToLower(strings.TrimSpace(top))] {
			return nil // mapped to a monitored author
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		out = append(out, UnmappedFile{
			Path:      p,
			Size:      info.Size(),
			DateAdded: info.ModTime(),
			Quality:   strings.ToUpper(ParseFormat(d.Name()).Format),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	if out == nil {
		out = []UnmappedFile{}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}
