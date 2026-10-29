package music

import (
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// L5 (Lidarr "Unmapped Files" page): list the audio files that live under the
// music media root but are not mapped to a monitored artist/album. Like the
// L4 library-import scan (scan.go), this is a filesystem-only report — it
// matches scanned folders against the tracked artists and stops at the report
// (no file moves, no artist creation from the endpoint).

// UnmappedFile is one audio file under the music media root that is not
// mapped to a monitored artist/album.
type UnmappedFile struct {
	Path      string    `json:"path"`
	Size      int64     `json:"size"`
	DateAdded time.Time `json:"date_added"`
	Quality   string    `json:"quality"`
}

// ScanUnmappedFiles walks root and returns every audio file that is NOT under
// a top-level folder whose name (case-insensitive, trimmed) is in
// monitoredTitles. A file directly under root (no artist folder) or one under
// a folder matching no monitored artist is unmapped. matchedTitles is the set
// of monitored artist names. The result is sorted by path for stable output.
//
// Matching is at the artist-folder level (the standard music layout is
// <root>/<artist>/<album>/<file>): a file is "mapped" when its top-level
// folder names a monitored tracked artist. Files under a non-monitored artist
// folder are reported unmapped, so unmonitoring an artist surfaces its files
// here (the Lidarr semantic).
func ScanUnmappedFiles(root string, monitoredTitles map[string]bool) ([]UnmappedFile, error) {
	root = filepath.Clean(root)
	mon := make(map[string]bool, len(monitoredTitles))
	for k := range monitoredTitles {
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
		if !IsAudioFile(d.Name()) {
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
			return nil // mapped to a monitored artist
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		out = append(out, UnmappedFile{
			Path:      p,
			Size:      info.Size(),
			DateAdded: info.ModTime(),
			Quality:   CodecFromExtension(filepath.Ext(d.Name())),
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
