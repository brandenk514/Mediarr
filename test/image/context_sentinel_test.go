package image

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestBuildContextSentinelIsolation proves the build context is isolated
// from secrets and toolchain contamination RECURSIVELY — both at the context
// root and nested (ui/node_modules, nested .env files, .git at any depth).
//
// It is a real build-context test, run twice through `docker build` on a
// synthetic context (marker files only — never real secrets):
//
//  1. RED build: the context's .dockerignore lacks the node_modules /
//     .env / .git / dist rules. The sentinels must LEAK into the image —
//     this proves the test is sensitive: without it, a missing rule would
//     go unnoticed.
//  2. GREEN build: the context carries the repository's real .dockerignore.
//     Every sentinel must be absent (root AND nested), while the genuine
//     build inputs (ui sources, package-lock.json, ui/.npmrc, go.mod) must
//     still be present — the rules exclude the right things and nothing
//     more.
//
// The context is a temporary directory; the image is FROM scratch, so no
// external base image is pulled. Only the Docker daemon is required; if it
// is unavailable the test skips (CI always has it).
func TestBuildContextSentinelIsolation(t *testing.T) {
	ctx := context.Background()
	if _, err := runDocker(ctx, dockerTimeout, "info"); err != nil {
		t.Skip("Docker daemon not available; the build-context sentinel runs in CI (the docker job builds the real image)")
	}

	tmp := t.TempDir()

	// Real build inputs that the .dockerignore must RETAIN.
	inputs := map[string]string{
		"go.mod":               "module example.com/sentinel\n\ngo 1.27\n",
		"deploy/Dockerfile":    "# project Dockerfile marker\nFROM scratch\n",
		"ui/package.json":      `{"name":"ui","private":true}` + "\n",
		"ui/package-lock.json": `{"name":"ui","lockfileVersion":3}` + "\n",
		"ui/.npmrc":            "legacy-peer-deps=true\n",
		"ui/index.html":        "<html><body></body></html>\n",
		"ui/vite.config.ts":    "export default {}\n",
		"ui/src/main.tsx":      "export {}\n",
	}

	// Sentinels: synthetic markers, never real secrets.
	sentinels := map[string]string{
		"node_modules/left-pad/index.js":    "ROOT_NODE_MODULES_SENTINEL\n",
		"ui/node_modules/left-pad/index.js": "NESTED_NODE_MODULES_SENTINEL\n",
		".env":                              "FAKE_ROOT_ENV_SENTINEL\n",
		".env.production":                   "FAKE_ROOT_ENV_PROD_SENTINEL\n",
		"ui/.env":                           "FAKE_UI_ENV_SENTINEL\n",
		"internal/.env.local":               "FAKE_NESTED_ENV_SENTINEL\n",
		".git/HEAD":                         "FAKE_GIT_METADATA_SENTINEL\n",
		"ui/dist/index.html":                "FAKE_HOST_BUILD_OUTPUT_SENTINEL\n",
	}

	// The sentinel image is FROM scratch, and `docker create` refuses an
	// image with no command at all — give it one (it is never executed).
	sentinelDockerfile := "FROM scratch\nCOPY . /sentinel\nCMD [\"sentinel\"]\n"

	materialize := func(dir, dockerignoreContent string) {
		t.Helper()
		all := make(map[string]string, len(inputs)+len(sentinels)+2)
		for k, v := range inputs {
			all[k] = v
		}
		for k, v := range sentinels {
			all[k] = v
		}
		all["Dockerfile"] = sentinelDockerfile
		all[".dockerignore"] = dockerignoreContent
		for rel, content := range all {
			full := filepath.Join(dir, rel)
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				t.Fatalf("create %s: %v", full, err)
			}
			if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
				t.Fatalf("write %s: %v", full, err)
			}
		}
	}

	redDir := filepath.Join(tmp, "red")
	// A .dockerignore with none of the isolation rules of interest.
	materialize(redDir, "sentinel-red.dockerignore\n")
	redTag := fmt.Sprintf("mediarr-sentinel-%d-red", time.Now().UnixNano())
	buildSentinelImage(ctx, t, redDir, redTag)

	greenDir := filepath.Join(tmp, "green")
	realIgnore, err := os.ReadFile("../../.dockerignore")
	if err != nil {
		t.Fatalf("repo .dockerignore not found (test runs from test/image/): %v", err)
	}
	materialize(greenDir, string(realIgnore))
	greenTag := fmt.Sprintf("mediarr-sentinel-%d-green", time.Now().UnixNano())
	buildSentinelImage(ctx, t, greenDir, greenTag)

	// --- RED: without the rules, every sentinel leaks. If it does NOT leak,
	// the mechanism under test is not real (e.g. BuildKit changed context
	// filtering) and this test would be vacuous.
	redFS := exportImageFS(ctx, t, redTag)
	for _, p := range []string{
		"sentinel/node_modules/left-pad/index.js",
		"sentinel/ui/node_modules/left-pad/index.js",
		"sentinel/.env",
		"sentinel/.env.production",
		"sentinel/ui/.env",
		"sentinel/internal/.env.local",
		"sentinel/.git/HEAD",
		"sentinel/ui/dist/index.html",
	} {
		if _, ok := redFS[p]; !ok {
			t.Fatalf("RED build: sentinel %q did not leak into the context — the isolation test would be vacuous", p)
		}
	}
	t.Log("RED: all 8 sentinels leaked without the isolation rules (test sensitivity proven)")

	// --- GREEN: with the repository's .dockerignore, nothing leaks — root
	// and nested — and the required inputs survive.
	greenFS := exportImageFS(ctx, t, greenTag)
	for _, p := range []string{
		"sentinel/node_modules/left-pad/index.js",    // root node_modules
		"sentinel/ui/node_modules/left-pad/index.js", // nested node_modules
		"sentinel/.env",                // root env file
		"sentinel/.env.production",     // root env file
		"sentinel/ui/.env",             // nested env file
		"sentinel/internal/.env.local", // nested env file
		"sentinel/.git/HEAD",           // VCS metadata
		"sentinel/ui/dist/index.html",  // host build output
	} {
		if _, ok := greenFS[p]; ok {
			t.Errorf("GREEN build: %q leaked into the build context — .dockerignore must exclude it recursively", p)
		}
	}
	for _, p := range []string{
		"sentinel/go.mod",
		"sentinel/deploy/Dockerfile",
		"sentinel/ui/package.json",
		"sentinel/ui/package-lock.json",
		"sentinel/ui/.npmrc",
		"sentinel/ui/index.html",
		"sentinel/ui/vite.config.ts",
		"sentinel/ui/src/main.tsx",
	} {
		if _, ok := greenFS[p]; !ok {
			t.Errorf("GREEN build: required input %q was excluded — .dockerignore is too broad", p)
		}
	}
	t.Log("GREEN: no sentinel leaked (root and nested) and all required inputs retained")
}

// buildSentinelImage runs `docker build` in the given context (FROM scratch,
// so no base image is pulled) and removes the temporary image on cleanup.
func buildSentinelImage(ctx context.Context, t *testing.T, ctxDir, tag string) {
	t.Helper()
	out, err := runDocker(ctx, 3*time.Minute, "build", "-f", filepath.Join(ctxDir, "Dockerfile"), "-t", tag, ctxDir)
	if err != nil {
		t.Fatalf("docker build (tag %s): %v\n%s", tag, err, out)
	}
	t.Cleanup(func() {
		// Remove only the image this test created.
		_, _ = runDocker(context.Background(), dockerTimeout, "rmi", "-f", tag)
	})
}
