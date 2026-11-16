# Image regression tests (`test/image`)

Regression suite for the image that `deploy/Dockerfile` builds (issue #88:
the image carries the complete built UI at the read-only path
`/home/nonroot/ui`).

## Opt-in, fail-closed

The suite is **opt-in** via the `MEDIARR_IMAGE` environment variable, which
must name the exact image to inspect. It never defaults to a tag and never
runs against whatever happens to be in the daemon.

- `MEDIARR_IMAGE` **unset** → every test skips. A plain `go test ./...` is
  unaffected and stays green on machines without Docker.
- `MEDIARR_IMAGE` **set** but the Docker daemon or the named image is
  unavailable → the run **fails** (non-zero exit). It never silently skips:
  an explicitly requested verification that can't run is a broken gate.

```sh
# Build the image (make image is the documented build path):
make image IMAGE=mediarr TAG=local VERSION=0.0.0-local COMMIT=$(git rev-parse HEAD) \
    BUILD_DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ)

# Run the suite against it (zero skips expected):
MEDIARR_IMAGE=mediarr:local go test ./test/image/ -count=1 -v
```

## What it asserts

- **Runtime invariants unchanged:** user uid 65532 (nonroot), entrypoint
  `/mediarr`, `healthcheck` subcommand, port 8080, and the `/mediarr`
  binary itself.
- **Built UI present:** `/home/nonroot/ui/index.html` exists and is
  non-empty, and every local asset it references (JS/CSS/favicon) exists
  and is non-empty. The legitimate extra outputs of a Vite build
  (`public/` files, dynamic chunks) are allowed; build-context
  contamination is proven separately by the sentinel test
  (`context_sentinel_test.go`), not by over-strict allowlists.
- **No toolchain payload:** no `node`/`npm`/`node_modules`/npm-cache
  entries in the runtime image.
- **Stalled-export guard:** a synthetic stalled subprocess is killed at its
  own context deadline and reaped (deterministic, no Docker needed).

## How the image is inspected

Distroless has no shell, so the tests `docker create` a disposable
container, `docker export` its filesystem, and parse the tar **in memory**
(nothing is extracted to disk). Tar entry names are validated against path
traversal (`..`) and absolute paths before use, total bytes read are
bounded (512 MiB), and the export subprocess runs under a wall-clock
deadline (`context` + `exec.CommandContext`, single `Wait`). Every
container/image the suite creates is removed on cleanup (`docker rm -f -v`,
`docker rmi -f`); it never touches pre-existing Docker resources.
