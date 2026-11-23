package api

// #107: the TV and music library-import endpoints accept an ad-hoc
// filesystem path (the "unmapped" import case). Import by path stays
// available, but only for locations inside the service's configured media
// root: this is the shared confinement for that option.

import (
	"errors"
	"fmt"
	"strings"

	"github.com/brandenk514/mediarr/internal/fsutil"
)

// confineImportPath confines the ad-hoc import path to the service's
// configured media root (#107). Absolute paths outside the root, ".."
// traversal, and symlinks that point out of the root are rejected before
// any filesystem access (fsutil.ResolveConfined); the fsutil sentinels are
// mapped onto transport-ready messages the handlers turn into 400s. A
// service with no configured media root fails closed: there is no location
// to confine to, so the path option is unavailable.
func confineImportPath(mediaRoot, p string) (string, error) {
	if strings.TrimSpace(mediaRoot) == "" {
		return "", errors.New("media root is not configured; use a registered root folder or configure the media root")
	}
	confined, err := fsutil.ResolveConfined(mediaRoot, p)
	if err != nil {
		switch {
		case errors.Is(err, fsutil.ErrEmptyPath):
			return "", errors.New("path is required")
		case errors.Is(err, fsutil.ErrPathOutsideRoot):
			return "", fmt.Errorf("path must be inside the media root %s", mediaRoot)
		}
		return "", err
	}
	return confined, nil
}
