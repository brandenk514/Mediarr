#!/usr/bin/env bash
#
# ci-preflight.sh — run the gates CI runs, locally, in one pass.
#
# Usage:
#   scripts/ci-preflight.sh            fast, pure-local gates
#   scripts/ci-preflight.sh --full     + Docker-dependent gates (integration,
#                                      Go e2e); needs a running Docker daemon
#   scripts/ci-preflight.sh --load     + 10k-item load test (slow, 2-4 min)
#   scripts/ci-preflight.sh --only=g1,g2   (debug) run only the named gates
#
# Every selected gate runs even if an earlier one fails; the summary at the
# end lists ALL failures. Exits 1 if any gate failed, 0 if all passed.
#
# Mirrors .github/workflows/ci.yml:
#   gofmt / go vet / golangci-lint / unit tests + 60% coverage floor
#                                       ... "lint-and-test" job
#   govulncheck ............................ "lint-and-test" job (if installed)
#   npm ci / npm audit / oxlint / tsc build / vitest ... "ui" job
#   semgrep p/golang + p/typescript ......... "semgrep" job (if installed)
#   integration + e2e (testcontainers) ...... "lint-and-test" job (--full)
#   load test ................................ "load" job (--load)
# Not mirrored (CI-only): Docker image build, Trivy scan, compose smoke test.
#
# A gate whose tool is missing locally (semgrep, govulncheck) or whose
# prerequisite is unavailable (Docker for --full) is reported as skipped,
# not failed — CI still runs those gates on the push.

set -u

# Go tool binaries (govulncheck, ...) live in ~/go/bin, which is not always
# on the default PATH.
export PATH="$HOME/go/bin:$PATH"

FULL=0
LOAD=0
ONLY=""
for arg in "$@"; do
  case "$arg" in
    --full) FULL=1 ;;
    --load) LOAD=1 ;;
    --only=*) ONLY="${arg#--only=}" ;;
    -h|--help) grep '^#' "$0" | sed 's/^#//'; exit 0 ;;
    *) echo "unknown flag: $arg (try --help)" >&2; exit 2 ;;
  esac
done

cd "$(dirname "$0")/.."

RESULTS=()
FAILURES=()

selected() {
  [ -z "$ONLY" ] && return 0
  case ",$ONLY," in *,"$1",*) return 0 ;; *) return 1 ;; esac
}

# skip_if_missing TOOL — sets SKIP_REASON and returns 1 if TOOL is not in
# PATH (the gate is skipped, not failed: CI always has the toolchain).
skip_if_missing() {
  if ! command -v "$1" >/dev/null 2>&1; then
    SKIP_REASON="$1 not in PATH; CI runs this"
    echo "    skipping: $SKIP_REASON"
    return 1
  fi
  return 0
}

# gate NAME FUNC — run FUNC, record PASS/SKIP/FAIL, keep going either way.
# A gate function that skips (missing tool / Docker) sets SKIP_REASON.
gate() {
  local name="$1" func="$2" rc
  if ! selected "$name"; then return 0; fi
  local t0=$SECONDS
  echo
  echo "==> $name"
  SKIP_REASON=""
  if "$func"; then
    if [ -n "$SKIP_REASON" ]; then
      RESULTS+=("SKIP  $name ($SKIP_REASON)")
    else
      RESULTS+=("PASS  $name ($((SECONDS - t0))s)")
    fi
  else
    rc=$?
    RESULTS+=("FAIL  $name (exit $rc, $((SECONDS - t0))s)")
    FAILURES+=("$name")
  fi
}

gate_gofmt() {
  skip_if_missing go || return 0
  local out
  out=$(gofmt -l .)
  [ -z "$out" ] && return 0
  echo "$out" | sed 's/^/    unformatted: /'
  return 1
}

gate_vet() {
  skip_if_missing go || return 0
  go vet ./...
}

gate_golangci() {
  # gosec/errcheck/govet/unused — same .golangci.yml the CI step uses.
  # Missing locally? Skipped, like the govulncheck gate (CI pins v2.15.0).
  skip_if_missing go || return 0
  skip_if_missing golangci-lint || return 0
  golangci-lint run ./...
}

gate_unit() {
  skip_if_missing go || return 0
  local total
  go test ./internal/api/... ./internal/auth/... ./internal/config/... \
    ./internal/health/... ./internal/domains/... ./internal/services/... \
    ./internal/secrets/... ./internal/indexers/... ./internal/downloads/... \
    ./internal/paging/... \
    -count=1 -cover -coverprofile=unit.coverage.out || return 1
  total=$(go tool cover -func unit.coverage.out | awk '/^total/ {gsub("%","",$3); print $3}')
  if awk -v v="${total:-0}" 'BEGIN { exit !(v < 60) }'; then
    echo "    overall unit coverage ${total}% is below the 60% floor (CI will reject this)"
    return 1
  fi
  echo "    overall unit coverage: ${total}% (floor 60%)"
}

gate_ui() {
  skip_if_missing npm || return 0
  # --include=dev --include=optional: shells with NODE_ENV=production make
  # npm silently skip devDependencies (and write a production-only
  # lockfile). These flags force the full tree regardless of the environment.
  ( cd ui \
    && npm ci --no-audit --no-fund --include=dev --include=optional \
    && npm audit --audit-level=high \
    && npm run lint \
    && npm run build \
    && npm run test )
}

gate_semgrep() {
  if ! command -v semgrep >/dev/null 2>&1; then
    SKIP_REASON="semgrep not installed locally; CI pins semgrep==1.181.0"
    echo "    skipping: $SKIP_REASON"
    return 0
  fi
  semgrep scan --config p/golang --config p/typescript --error
}

gate_govulncheck() {
  # Needs both the govulncheck binary AND the go toolchain it shells out to.
  skip_if_missing go || return 0
  skip_if_missing govulncheck || return 0
  govulncheck ./...
}

docker_up() { docker info >/dev/null 2>&1; }

gate_integration() {
  skip_if_missing go || return 0
  if ! docker_up; then
    SKIP_REASON="Docker daemon not running; CI runs this"
    echo "    skipping: $SKIP_REASON"
    return 0
  fi
  go test ./internal/postgres/... ./internal/auth/... -count=1 -timeout 300s
}

gate_e2e() {
  skip_if_missing go || return 0
  if ! docker_up; then
    SKIP_REASON="Docker daemon not running; CI runs this"
    echo "    skipping: $SKIP_REASON"
    return 0
  fi
  go test ./test/e2e/... -count=1 -timeout 300s
}

gate_load() {
  skip_if_missing go || return 0
  if ! docker_up; then
    SKIP_REASON="Docker daemon not running"
    echo "    skipping: $SKIP_REASON"
    return 0
  fi
  go test ./test/loadtest/... -count=1 -timeout 600s
}

echo "=== ci-preflight ==="
if [ "$FULL" -eq 1 ]; then
  echo "mode: full (local + Docker-dependent gates)"
elif [ -n "$ONLY" ]; then
  echo "mode: debug (gates: $ONLY)"
else
  echo "mode: local (Docker-dependent gates off; add --full)"
fi

gate "gofmt"                          gate_gofmt
gate "go vet"                         gate_vet
gate "golangci-lint (gosec, errcheck, unused)" gate_golangci
gate "unit tests + coverage"          gate_unit
gate "ui (ci, audit, lint, build, test)" gate_ui
gate "semgrep"                        gate_semgrep
gate "govulncheck"                    gate_govulncheck
if [ "$FULL" -eq 1 ]; then
  gate "integration (Postgres + Redis)"  gate_integration
  gate "e2e pipeline (qBittorrent + SABnzbd)" gate_e2e
fi
if [ "$LOAD" -eq 1 ]; then
  gate "load test (10k items)"         gate_load
fi

echo
echo "========================================"
echo " preflight results"
for r in "${RESULTS[@]}"; do echo "  $r"; done
echo "========================================"
if [ "${#FAILURES[@]}" -gt 0 ]; then
  echo "  ${#FAILURES[@]} gate(s) FAILED: ${FAILURES[*]}"
  exit 1
fi
echo "  all ${#RESULTS[@]} gate(s) passed"
exit 0
