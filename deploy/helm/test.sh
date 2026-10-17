#!/usr/bin/env bash
# Helm chart lint + unit tests for deploy/helm/mediarr.
#
# "Unit tests" here = render assertions: `helm template` with realistic values
# and checks that the security model (PLAN §9) and wiring actually made it
# into the rendered manifests. A real `helm install` on a test cluster is
# covered by the release job / kind (see docs/RELEASE.md), not by this script.
set -euo pipefail

CHART_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/mediarr"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

fail() { echo "FAIL: $*" >&2; exit 1; }

have_helm() { command -v helm >/dev/null 2>&1; }
if ! have_helm; then
  echo "helm not found on PATH — installing into $WORK (one-time)..." >&2
  GOBIN="$WORK/bin" go install helm.sh/helm/v3/cmd/helm@v3.15.4 2>/dev/null \
    || { echo "cannot install helm (no go either?)" >&2; exit 1; }
  export PATH="$WORK/bin:$PATH"
fi

ENC_KEY="$(head -c32 /dev/urandom | od -An -tx1 | tr -d ' \n')"
PG_PASS="chart-test-password"

echo "==> helm lint"
helm lint "$CHART_DIR" \
  --set secrets.encryptionKey="$ENC_KEY" \
  --set secrets.postgresPassword="$PG_PASS"

echo "==> render (default values + required secrets)"
helm template chart-test "$CHART_DIR" --kube-version 1.30.4 \
  --set secrets.encryptionKey="$ENC_KEY" \
  --set secrets.postgresPassword="$PG_PASS" \
  > "$WORK/render.yaml"
[ -s "$WORK/render.yaml" ] || fail "render produced no output"

check() { grep -qE "$1" "$WORK/render.yaml" || fail "missing in render: $2"; }

echo "==> render assertions"
check 'kind: Deployment'                    "app Deployment"
check 'kind: StatefulSet'                   "Postgres StatefulSet"
check 'readOnlyRootFilesystem: true'        "read-only rootfs"
check 'drop:'                               "capabilities dropped"
check 'app.kubernetes.io/component: app'    "app component selector (no cross-workload match)"
check 'runAsNonRoot: true'                  "runAsNonRoot"
check 'allowPrivilegeEscalation: false'     "no privilege escalation"
check 'type: RuntimeDefault'                "seccomp RuntimeDefault"
check 'path: /healthz'                      "liveness /healthz"
check 'path: /readyz'                       "readiness /readyz"
check 'MEDIARR_POSTGRES_DSN'                "PG DSN env"
check 'MEDIARR_REDIS_URL'                   "Redis URL env"
check 'MEDIARR_ENCRYPTION_KEY'              "encryption key env"
check 'kind: Secret'                        "chart-managed Secret"
check 'kind: Service'                       "Services"
check 'emptyDir: \{\}'                      "emptyDir volumes (media/downloads/tmp)"
# Without secrets.redisPassword the chart must not reference the redis password.
! grep -q 'key: REDIS_PASSWORD' "$WORK/render.yaml" \
  || fail "REDIS_PASSWORD key referenced without secrets.redisPassword set"

echo "==> render (with redis password)"
helm template chart-test "$CHART_DIR" --kube-version 1.30.4 \
  --set secrets.encryptionKey="$ENC_KEY" \
  --set secrets.postgresPassword="$PG_PASS" \
  --set secrets.redisPassword="redis-pass" \
  > "$WORK/render-redis.yaml"
grep -q -- '--requirepass' "$WORK/render-redis.yaml" \
  || fail "redis not configured with requirepass"
grep -q 'key: REDIS_PASSWORD' "$WORK/render-redis.yaml" \
  || fail "redis password not referenced"

echo "==> render (postgres persistence disabled)"
helm template chart-test "$CHART_DIR" --kube-version 1.30.4 \
  --set secrets.encryptionKey="$ENC_KEY" \
  --set secrets.postgresPassword="$PG_PASS" \
  --set postgres.persistence.enabled=false \
  > "$WORK/render-nopvc.yaml"
! grep -q 'volumeClaimTemplates' "$WORK/render-nopvc.yaml" \
  || fail "volumeClaimTemplates should be absent with persistence disabled"

echo "==> existingSecret path"
helm template chart-test "$CHART_DIR" --kube-version 1.30.4 \
  --set existingSecret=external-creds \
  > "$WORK/render-ext.yaml"
! grep -q 'kind: Secret' "$WORK/render-ext.yaml" \
  || fail "chart must not create a Secret when existingSecret is set"
grep -q 'name: external-creds' "$WORK/render-ext.yaml" \
  || fail "existingSecret not used"

echo "==> required-secrets guard"
if helm template chart-test "$CHART_DIR" >/dev/null 2>&1; then
  fail "chart rendered without required secrets (required() guard missing)"
fi

echo "ALL HELM TESTS PASSED"
