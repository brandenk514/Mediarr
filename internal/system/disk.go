package system

import (
	"os"
	"syscall"
)

// DiskRoot is the filesystem usage of one configured root.
type DiskRoot struct {
	Path       string  `json:"path"`
	Exists     bool    `json:"exists"`
	TotalBytes int64   `json:"total_bytes"`
	UsedBytes  int64   `json:"used_bytes"`
	FreeBytes  int64   `json:"free_bytes"`
	UsagePct   float64 `json:"usage_pct"`
}

// SystemDisk is the disk report for the configured media roots.
type SystemDisk struct {
	Roots        []DiskRoot `json:"roots"`
	MediaRoot    string     `json:"media_root"`
	DownloadsDir string     `json:"downloads_dir"`
}

// StatFs is the filesystem-stat seam so tests can inject usage without
// touching the real filesystem. The default implementation uses syscall.
type StatFs func(path string) (total, used, free int64, err error)

// syscallStatfs is the default StatFs (POSIX statfs).
func syscallStatfs(path string) (total, used, free int64, err error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, 0, err
	}
	bsize := int64(st.Bsize)
	total = int64(st.Blocks) * bsize
	free = int64(st.Bavail) * bsize
	used = total - int64(st.Bfree)*bsize
	return total, used, free, nil
}

// BuildDisk assembles the disk report for the given configured paths,
// deduped and preserving order (media root, downloads dir, then any
// registered root folders). A missing path is reported with Exists=false and
// zero usage so the UI can show a "not present" row.
func BuildDisk(mediaRoot, downloadsDir string, roots []string, stat StatFs) SystemDisk {
	if stat == nil {
		stat = syscallStatfs
	}
	report := SystemDisk{
		MediaRoot:    mediaRoot,
		DownloadsDir: downloadsDir,
		Roots:        []DiskRoot{},
	}
	seen := map[string]bool{}
	add := func(p string) {
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		dr := DiskRoot{Path: p}
		if _, err := os.Stat(p); err != nil {
			report.Roots = append(report.Roots, dr)
			return
		}
		dr.Exists = true
		total, used, free, err := stat(p)
		if err == nil {
			dr.TotalBytes, dr.UsedBytes, dr.FreeBytes = total, used, free
			if total > 0 {
				dr.UsagePct = float64(used) / float64(total)
			}
		}
		report.Roots = append(report.Roots, dr)
	}
	add(mediaRoot)
	add(downloadsDir)
	for _, r := range roots {
		add(r)
	}
	return report
}
