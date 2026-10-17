# Mediarr Helm chart

Deploys the full dev-parity stack from `deploy/compose.dev.yml` onto
Kubernetes: the Mediarr server, Postgres 16, and Redis 7, in one namespace.

## Quick start

```sh
# one-time: generate a 64-hex encryption key (32 bytes)
export MEDIARR_ENC_KEY=$(head -c32 /dev/urandom | od -An -tx1 | tr -d ' \n')

helm install mediarr ./mediarr \
  --set secrets.encryptionKey="$MEDIARR_ENC_KEY" \
  --set secrets.postgresPassword="$(openssl rand -hex 16)" \
  --set image.tag=0.1.0

kubectl get pods -w
kubectl port-forward svc/mediarr-mediarr 8080:8080   # then http://localhost:8080
```

## Secrets

Two modes:

- **Chart-managed (dev default):** `secrets.encryptionKey` +
  `secrets.postgresPassword` (and optional `secrets.redisPassword`) are turned
  into the chart's Secret. `secrets.encryptionKey` must be 64 hex chars.
- **`existingSecret: <name>`:** the chart creates no Secret and reads
  `MEDIARR_POSTGRES_DSN`, `MEDIARR_REDIS_URL`, `MEDIARR_ENCRYPTION_KEY` (plus
  `POSTGRES_PASSWORD` / `REDIS_PASSWORD` if the bundled datastores are used)
  from your pre-created Secret. Caveat: if that Secret was previously created
  by THIS chart, `helm upgrade` into existingSecret mode prunes it (three-way
  merge sees it as "no longer rendered"). Pre-create it yourself, or keep
  chart-managed; a pruned copy is recoverable via
  `helm get manifest <rel> --revision <n>`.

## Security model (PLAN §9)

| Control | App | Redis | Postgres |
|---|---|---|---|
| nonroot user | 65532 (distroless) | 999 (redis) | 70 (postgres) |
| read-only rootfs | yes | yes | yes |
| writable volumes | `/media`, `/downloads`, `/tmp` (emptyDir) | `/data`, `/tmp` | `/var/lib/postgresql/data` (PVC), `/var/run/postgresql`, `/tmp` |
| capabilities | drop ALL | drop ALL | drop ALL (init container: drop ALL + CHOWN only) |
| no-new-privs / allowPrivilegeEscalation | false | false | false |
| seccomp | RuntimeDefault | RuntimeDefault | RuntimeDefault |
| probes | http `/healthz` `/readyz` | `redis-cli ping` | `pg_isready` |

Postgres runs directly as the postgres user (uid 70) with a read-only
rootfs; the image pre-owns `/var/run/postgresql`, `fsGroup: 70` makes the
PVC group-writable, and a rootful init container
(`fix-data-permissions`, capabilities: CHOWN only) chowns the
provisioner-created volume dir once so `initdb` and restarts work.

## Persistence

Postgres gets a `volumeClaimTemplates` PVC (1 Gi default,
`postgres.persistence.*`). Redis uses an emptyDir (cache/queue only) by
default; set `redis.persistence.enabled=true` for a PVC. The app's `/media`
and `/downloads` are emptyDir in this dev chart — point them at PVCs or
`hostPath` for real media retention (see `templates/deployment.yaml`).

## Tests

```sh
deploy/helm/test.sh     # helm lint + render assertions (security model, wiring)
```

A live `helm install` on a kind cluster is part of the M7 release validation
(see `docs/RELEASE.md`).
