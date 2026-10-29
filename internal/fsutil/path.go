// Package fsutil holds small filesystem helpers shared across services.
package fsutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrEmptyPath is a confined-path request with no path.
var ErrEmptyPath = errors.New("fsutil: path is required")

// ErrPathOutsideRoot is a confined-path request whose target escapes root.
var ErrPathOutsideRoot = errors.New("fsutil: path is outside the root")

// ResolveConfined validates that p points at a file located strictly inside
// root and returns its canonical (absolute, cleaned) form. It is the
// defense against path injection on endpoints that accept user-supplied
// file paths (the manual-import endpoints): p is user-supplied, so before
// any filesystem access it is cleaned, resolved to an absolute path, and
// required to stay inside root (a relative input is rooted there; an
// absolute input must already be under it). Symlinks whose target escapes
// root are rejected.
func ResolveConfined(root, p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", ErrEmptyPath
	}
	base, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("fsutil: resolve root: %w", err)
	}
	cand := filepath.Clean(p)
	if !filepath.IsAbs(cand) {
		cand = filepath.Join(base, cand)
	}
	if cand, err = filepath.Abs(cand); err != nil {
		return "", fmt.Errorf("fsutil: resolve path: %w", err)
	}
	if !IsUnder(base, cand) {
		return "", ErrPathOutsideRoot
	}
	// A symlink anywhere in the path could point outside the root; reject
	// it if its resolved target escapes.
	if resolved, rerr := filepath.EvalSymlinks(cand); rerr == nil {
		if !IsUnder(base, resolved) {
			return "", ErrPathOutsideRoot
		}
	}
	return cand, nil
}

// IsUnder reports whether candidate is inside (or equal to) base.
func IsUnder(base, candidate string) bool {
	rel, err := filepath.Rel(base, candidate)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)))
}
