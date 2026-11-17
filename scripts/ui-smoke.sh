#!/usr/bin/env bash
#
# ui-smoke.sh — browser-grade UI-surface probes for the served compose stack.
#
# The Go server serves the built SPA same-origin on :8080 (issue #87/#89).
# These curl probes assert the *served* UI surface, independent of a real
# browser, so both the push-only `smoke` job and the pull_request `ui-e2e`
# job can run them against their own stack (issue #90):
#
#   1. GET /                     -> 200 and the body is the app's HTML root
#   2. GET /movies (deep link)   -> 200 (SPA fallback serves index.html, not 404)
#   3. a hashed asset referenced by index.html -> 200 (the dist copy is intact)
#   4. GET /api/v1/auth/me (no token)          -> 401 (the fallback must not
#                                                 swallow API routes)
#   5. GET /api/v1/events (no token)           -> 401 (a live WebSocket route,
#                                                 never treated as a static path)
#
# Usage:
#   BASE_URL=http://localhost:8080 scripts/ui-smoke.sh
#
# Exits non-zero on the first failed probe; each probe prints an ok:/FAIL line.

set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"
BASE_URL="${BASE_URL%/}"  # tolerate a trailing slash

echo "=== ui-smoke: probing ${BASE_URL} ==="

# probe METHOD PATH WANT_STATUS [WANT_BODY_SUBSTRING]
# Fetches PATH, checks the status code (and, if given, a substring of the
# body). Prints the body on failure for log context.
probe() {
  local method="$1" path="$2" want="$3" want_body="${4:-}"
  local code body
  if [ "$method" = "HEAD" ]; then
    code="$(curl -s -o /dev/null -I -X HEAD -w '%{http_code}' "${BASE_URL}${path}")"
    body=""
  else
    body="$(curl -s -X "$method" -w '\n%{http_code}' "${BASE_URL}${path}")" || true
    code="${body##*$'\n'}"
    body="${body%$'\n'*}"
  fi
  if [ "$code" != "$want" ]; then
    echo "FAIL: ${method} ${path} -> ${code} (want ${want})"
    [ -n "$body" ] && printf '      body: %s\n' "$(printf '%s' "$body" | head -c 400)"
    exit 1
  fi
  if [ -n "$want_body" ] && ! printf '%s' "$body" | grep -qF "$want_body"; then
    echo "FAIL: ${method} ${path} -> 200 but body is missing ${want_body@Q}"
    printf '      body: %s\n' "$(printf '%s' "$body" | head -c 400)"
    exit 1
  fi
  echo "ok: ${method} ${path} -> ${code}${want_body:+ (body has the expected marker)}"
}

# 1. / must be the SPA (200 + the app's HTML root element).
probe GET / 200 'id="root"'

# 2. A client-routing deep link falls back to index.html (200, not 404).
probe GET /movies 200 'id="root"'

# 3. Every hashed asset index.html references must be served (200). A broken
#    dist copy (missing /home/nonroot/ui, wrong asset hash) fails here.
root_html="$(curl -fsS "${BASE_URL}/")"
assets="$(printf '%s' "$root_html" | grep -oE '(src|href)="/assets/[^"]+"' | sed -E 's/^(src|href)="//; s/"$//' || true)"
if [ -z "$assets" ]; then
  echo "FAIL: index.html references no /assets/ files (expected at least one hashed asset)"
  exit 1
fi
while IFS= read -r asset; do
  [ -n "$asset" ] || continue
  code="$(curl -s -o /dev/null -w '%{http_code}' "${BASE_URL}${asset}")"
  if [ "$code" != "200" ]; then
    echo "FAIL: asset ${asset} -> ${code} (want 200)"
    exit 1
  fi
  echo "ok: GET ${asset} -> ${code}"
done <<< "$assets"

# 4. API routes stay API routes: the SPA fallback must not swallow them.
probe GET /api/v1/auth/me 401

# 5. The events WebSocket is reserved and auth-gated, never a static path.
probe GET /api/v1/events 401

echo "=== ui-smoke: all probes passed ==="
