// Package image is a regression suite for the Docker image that
// deploy/Dockerfile builds. It is opt-in: the tests run only when the
// MEDIARR_IMAGE environment variable explicitly names the image under test.
// The suite fails closed: if MEDIARR_IMAGE is set but the Docker daemon or
// the named image is unavailable, the run fails (non-zero exit) instead of
// skipping. A plain `go test ./...` with no explicit image skips every test,
// so the suite never runs by default and never picks a tag on its own.
//
// The runtime image has no shell (distroless), so its filesystem is
// inspected with `docker export` and the tar is parsed IN MEMORY — nothing
// is extracted to disk. Entry names are validated against path traversal
// (..) and absolute paths before any use, the total bytes read from the
// export are bounded, and the export subprocess runs under a wall-clock
// context deadline with a single Wait.
//
// What the suite asserts (issue #88):
//   - runtime invariants are unchanged: nonroot uid 65532, /mediarr
//     entrypoint, the healthcheck subcommand, exposed port 8080, and the
//     /mediarr binary itself;
//   - the built UI is present: /home/nonroot/ui/index.html exists and is
//     non-empty, and every JS/CSS/favicon asset it references exists and is
//     non-empty. The set of legitimate extra outputs (public/ files,
//     dynamic chunks) is deliberately NOT restricted — over-strict
//     allowlists have been a review finding before; contamination is proven
//     by the build-context sentinel test instead;
//   - no node/npm/node_modules/toolchain payload ever reaches the runtime;
//   - a stalled export subprocess is killed at its own deadline and reaped
//     (deterministic, no Docker required).
package image

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strings"
	"testing"
	"time"
)

const (
	dockerBin      = "docker"
	dockerTimeout  = 60 * time.Second
	exportTimeout  = 5 * time.Minute
	maxExportBytes = 512 << 20 // hard cap on bytes read from `docker export`
)

// runCommand runs a subprocess under a wall-clock deadline and returns its
// combined stdout+stderr. exec.CommandContext plus exactly ONE cmd.Wait: a
// double Wait would panic, and a missing Wait would leak the child.
func runCommand(ctx context.Context, timeout time.Duration, name string, args ...string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, name, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Start(); err != nil {
		return out.String(), fmt.Errorf("start %s: %w", name, err)
	}
	if err := cmd.Wait(); err != nil {
		return out.String(), fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, out.String())
	}
	return out.String(), nil
}

func runDocker(ctx context.Context, timeout time.Duration, args ...string) (string, error) {
	return runCommand(ctx, timeout, dockerBin, args...)
}

// TestExportStallIsReaped is a fast, deterministic guard for the subprocess
// runner used by docker export: a 30s sleep under a 2s deadline must return
// with an error well before 30s, proving the context deadline kills the
// child and the single Wait reaps it. No real Docker is involved.
func TestExportStallIsReaped(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	start := time.Now()
	_, err := runCommand(ctx, 30*time.Second, "sleep", "30")
	elapsed := time.Since(start)
	if err == nil {
		t.Fatalf("expected the 30s sleep to be killed by the 2s deadline, got success")
	}
	if elapsed > 15*time.Second {
		t.Fatalf("stalled subprocess was not killed at the deadline: Wait took %s", elapsed)
	}
	t.Logf("stalled subprocess killed and reaped after %s (deadline 2s)", elapsed)
}

// requireImage implements the opt-in, fail-closed gate for the whole suite:
//   - MEDIARR_IMAGE unset  -> plain run: skip (the suite is opt-in and must
//     never default to a feature-specific tag);
//   - MEDIARR_IMAGE set    -> fail closed: if the daemon or the image is
//     unavailable, Fatal — never a silent skip.
func requireImage(t *testing.T) string {
	t.Helper()
	tag := os.Getenv("MEDIARR_IMAGE")
	if tag == "" {
		t.Skip("MEDIARR_IMAGE not set: image regression suite is opt-in (MEDIARR_IMAGE=<image:tag> go test ./test/image/...)")
	}
	ctx := context.Background()
	if _, err := runDocker(ctx, dockerTimeout, "info"); err != nil {
		t.Fatalf("MEDIARR_IMAGE=%s was explicitly requested but the Docker daemon is unavailable: %v", tag, err)
	}
	if _, err := runDocker(ctx, dockerTimeout, "image", "inspect", tag); err != nil {
		t.Fatalf("MEDIARR_IMAGE=%s was explicitly requested but the image is not in the daemon: %v", tag, err)
	}
	return tag
}

// exportImageFS creates a disposable container from the image, exports its
// filesystem, and returns the regular-file contents keyed by relative path.
// The archive is parsed in memory (never extracted to disk), tar entry names
// are validated against traversal and absolute paths before use, the bytes
// read are bounded by maxExportBytes, and the export subprocess runs under a
// wall-clock deadline with a single Wait.
func exportImageFS(ctx context.Context, t *testing.T, image string) map[string][]byte {
	t.Helper()
	out, err := runDocker(ctx, dockerTimeout, "create", image)
	if err != nil {
		t.Fatalf("docker create %s: %v", image, err)
	}
	cid := strings.TrimSpace(out)
	t.Cleanup(func() {
		// Remove only the container this test created, with its (anonymous)
		// volumes, so nothing leaks.
		_, _ = runDocker(context.Background(), dockerTimeout, "rm", "-f", "-v", cid)
	})

	cctx, cancel := context.WithTimeout(ctx, exportTimeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, dockerBin, "export", cid)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe for docker export: %v", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start docker export: %v", err)
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, io.LimitReader(stdout, maxExportBytes+1)); err != nil {
		t.Fatalf("read docker export: %v", err)
	}
	if err := cmd.Wait(); err != nil { // single Wait
		t.Fatalf("docker export: %v: %s", err, stderr.String())
	}
	if buf.Len() > maxExportBytes {
		t.Fatalf("docker export output is %d bytes, exceeding the %d-byte bound", buf.Len(), maxExportBytes)
	}
	files, err := parseTarInMemory(buf.Bytes())
	if err != nil {
		t.Fatalf("parse exported filesystem tar: %v", err)
	}
	return files
}

// parseTarInMemory reads a tar archive fully in memory and returns the
// regular-file contents keyed by their validated relative path. Entry names
// that are absolute or traverse upward (..) are rejected before the entry is
// used, so a malicious or corrupted archive can never write outside the map.
func parseTarInMemory(data []byte) (map[string][]byte, error) {
	tr := tar.NewReader(bytes.NewReader(data))
	files := make(map[string][]byte)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return files, nil
		}
		if err != nil {
			return nil, fmt.Errorf("read tar entry: %w", err)
		}
		name := hdr.Name
		if strings.HasPrefix(name, "/") {
			return nil, fmt.Errorf("absolute tar entry name %q", name)
		}
		clean := path.Clean(name)
		if clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(name, "/../") {
			return nil, fmt.Errorf("path-traversal tar entry name %q", name)
		}
		if name == "" || clean == "." {
			continue
		}
		if hdr.Typeflag == tar.TypeReg {
			content, err := io.ReadAll(tr)
			if err != nil {
				return nil, fmt.Errorf("read tar file %q: %w", clean, err)
			}
			files[clean] = content
		}
	}
}

// TestRuntimeInvariants checks that #88 left the runtime contract intact:
// nonroot uid 65532, the /mediarr entrypoint, the healthcheck subcommand,
// and port 8080 — all unchanged from the pre-#88 image.
func TestRuntimeInvariants(t *testing.T) {
	tag := requireImage(t)
	out, err := runDocker(context.Background(), dockerTimeout, "image", "inspect", tag)
	if err != nil {
		t.Fatalf("docker image inspect: %v", err)
	}
	var imgs []struct {
		Config struct {
			User         string         `json:"User"`
			Entrypoint   []string       `json:"Entrypoint"`
			ExposedPorts map[string]any `json:"ExposedPorts"`
			Healthcheck  *struct {
				Test []string `json:"Test"`
			} `json:"Healthcheck"`
		} `json:"Config"`
	}
	if err := json.Unmarshal([]byte(out), &imgs); err != nil {
		t.Fatalf("parse docker image inspect: %v", err)
	}
	if len(imgs) != 1 {
		t.Fatalf("expected 1 image in inspect output, got %d", len(imgs))
	}
	cfg := imgs[0].Config
	if cfg.User != "65532" {
		t.Errorf("Config.User = %q, want 65532 (nonroot)", cfg.User)
	}
	if len(cfg.Entrypoint) != 1 || cfg.Entrypoint[0] != "/mediarr" {
		t.Errorf("Config.Entrypoint = %v, want [\"/mediarr\"]", cfg.Entrypoint)
	}
	if cfg.Healthcheck == nil {
		t.Error("Config.Healthcheck is missing; want the /mediarr healthcheck subcommand")
	} else if joined := strings.Join(cfg.Healthcheck.Test, " "); !strings.Contains(joined, "/mediarr") || !strings.Contains(joined, "healthcheck") {
		t.Errorf("Config.Healthcheck.Test = %v, want [\"/mediarr\" \"healthcheck\"]", cfg.Healthcheck.Test)
	}
	if _, ok := cfg.ExposedPorts["8080/tcp"]; !ok {
		t.Errorf("Config.ExposedPorts = %v, want 8080/tcp to be exposed", cfg.ExposedPorts)
	}
}

// TestRuntimeBinaryPresent checks that the /mediarr binary is in the image
// (the entrypoint target the healthcheck subcommand runs).
func TestRuntimeBinaryPresent(t *testing.T) {
	tag := requireImage(t)
	files := exportImageFS(context.Background(), t, tag)
	if _, ok := files["mediarr"]; !ok {
		t.Fatal("missing /mediarr binary in the image")
	}
}

const uiRoot = "home/nonroot/ui/"

// TestBuiltUIAssets is the core #88 acceptance check: the complete built
// Vite dist is at the documented read-only path. index.html exists and is
// non-empty, and every local asset it references (JS/CSS/favicon) exists
// and is non-empty. Extra legitimate outputs (public/ files, dynamic
// chunks) are allowed — see the package comment.
func TestBuiltUIAssets(t *testing.T) {
	tag := requireImage(t)
	files := exportImageFS(context.Background(), t, tag)

	index, ok := files[uiRoot+"index.html"]
	if !ok {
		t.Fatalf("missing %sindex.html: the image does not contain the built UI (deploy/Dockerfile must ship the dist to /home/nonroot/ui)", uiRoot)
	}
	if len(index) == 0 {
		t.Fatal("index.html is empty")
	}

	refs := referencedAssets(string(index))
	if len(refs) == 0 {
		t.Fatalf("index.html references no local assets; expected script/link refs:\n%s", string(index))
	}
	for _, ref := range refs {
		full := uiRoot + strings.TrimPrefix(ref, "/")
		data, ok := files[full]
		if !ok {
			t.Errorf("index.html references %q but %s is absent from the image", ref, full)
			continue
		}
		if len(data) == 0 {
			t.Errorf("referenced asset %s is empty", full)
		}
	}
}

// referencedAssets extracts the local asset references (src/href attribute
// values) from the built index.html. Remote URLs, data: URIs, and fragments
// are ignored; relative refs resolve against the UI root.
func referencedAssets(html string) []string {
	var refs []string
	reAttr := regexp.MustCompile(`(?i)\b(?:src|href)\s*=\s*"([^"]+)"`)
	for _, m := range reAttr.FindAllStringSubmatch(html, -1) {
		v := m[1]
		if v == "" || strings.Contains(v, "://") || strings.HasPrefix(v, "//") ||
			strings.HasPrefix(v, "data:") || strings.HasPrefix(v, "#") {
			continue
		}
		refs = append(refs, v)
	}
	return refs
}

// TestNoToolchainPayload checks that no Node toolchain payload (node, npm,
// node_modules, npm cache) reached the runtime image — the UI is built in
// the ui-build stage and only the dist crosses into the runtime.
func TestNoToolchainPayload(t *testing.T) {
	tag := requireImage(t)
	files := exportImageFS(context.Background(), t, tag)

	var offenders []string
	for name := range files {
		base := path.Base(name)
		switch base {
		case "node", "npm", "npx", "corepack", "node_modules":
			offenders = append(offenders, name)
		}
		if strings.Contains(name, "node_modules/") || strings.Contains(name, ".npm/") {
			offenders = append(offenders, name)
		}
	}
	if len(offenders) > 0 {
		first := offenders
		if len(first) > 10 {
			first = first[:10]
		}
		t.Errorf("Node toolchain payload found in the runtime image (%d entries, first %d): %v", len(offenders), len(first), first)
	}
}
