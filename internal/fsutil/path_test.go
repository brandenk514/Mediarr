package fsutil

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestResolveConfined pins the path-injection defense: in-root targets
// (absolute, relative, the root itself) are accepted; absolute paths
// outside the root, ".." traversal, and symlink escapes are rejected.
func TestResolveConfined(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "media")
	if err := os.MkdirAll(filepath.Join(root, "series"), 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(base, "elsewhere")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}

	cases := map[string]struct {
		root, p string
		wantErr error
	}{
		"absolute in-root":        {root, filepath.Join(root, "series"), nil},
		"relative in-root":        {root, filepath.Join("series", "..", "series"), nil},
		"the root itself":         {root, root, nil},
		"empty path":              {root, "  ", ErrEmptyPath},
		"absolute outside root":   {root, outside, ErrPathOutsideRoot},
		"dotdot traversal out":    {root, filepath.Join(root, "..", "elsewhere"), ErrPathOutsideRoot},
		"relative dotdot out":     {root, filepath.Join("..", "elsewhere"), ErrPathOutsideRoot},
		"absolute outside prefix": {root, filepath.Join(base, "elsewhere-2"), ErrPathOutsideRoot},
	}
	for name, tc := range cases {
		got, err := ResolveConfined(tc.root, tc.p)
		if !errors.Is(err, tc.wantErr) {
			t.Errorf("%s: err = %v, want %v", name, err, tc.wantErr)
			continue
		}
		if tc.wantErr == nil {
			if !filepath.IsAbs(got) {
				t.Errorf("%s: result %q not absolute", name, got)
			}
			if !IsUnder(tc.root, got) {
				t.Errorf("%s: result %q not under root %q", name, got, tc.root)
			}
		}
	}
}

// TestResolveConfined_SymlinkEscape pins that a symlink inside the root
// whose target points outside the root is rejected.
func TestResolveConfined_SymlinkEscape(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "media")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(base, "elsewhere")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	if _, err := ResolveConfined(root, link); !errors.Is(err, ErrPathOutsideRoot) {
		t.Errorf("symlink escape: err = %v, want ErrPathOutsideRoot", err)
	}
	// A symlink inside the root that stays inside is fine.
	inner := filepath.Join(root, "inner")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	link2 := filepath.Join(root, "innertwo")
	if err := os.Symlink(inner, link2); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	if _, err := ResolveConfined(root, link2); err != nil {
		t.Errorf("in-root symlink: err = %v, want nil", err)
	}
}

// TestResolveConfined_SymlinkedRootMount pins the macOS-shaped case: the
// root itself lives behind a symlinked mount point (/var -> /private/var),
// and in-root paths must still be accepted (the containment check compares
// resolved-to-resolved).
func TestResolveConfined_SymlinkedRootMount(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real-media")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	mount := filepath.Join(base, "mnt")
	if err := os.MkdirAll(mount, 0o755); err != nil {
		t.Fatal(err)
	}
	rootLink := filepath.Join(mount, "media")
	if err := os.Symlink(real, rootLink); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	// The root, reached through the symlink, confines in-root paths
	// (existing and not-yet-existing alike: the lexical check is what
	// binds, the symlink check applies when the target exists).
	for _, p := range []string{rootLink, filepath.Join(rootLink, "not-created-yet")} {
		if _, err := ResolveConfined(rootLink, p); err != nil {
			t.Errorf("symlinked root, path %q: err = %v, want nil", p, err)
		}
	}
	// An in-root symlink reached through the symlinked root is accepted.
	inner := filepath.Join(rootLink, "inner")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	// A symlink inside the (symlinked) root that escapes is rejected.
	outside := filepath.Join(base, "elsewhere")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	escape := filepath.Join(rootLink, "escape")
	if err := os.Symlink(outside, escape); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	if _, err := ResolveConfined(rootLink, escape); !errors.Is(err, ErrPathOutsideRoot) {
		t.Errorf("symlinked root, escape: err = %v, want ErrPathOutsideRoot", err)
	}
}
